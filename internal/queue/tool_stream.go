package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	redisstore "github.com/wzhnbsixsixsix/agentforge/internal/storage/redis"

	"github.com/redis/go-redis/v9"
)

// ToolTask gateway → worker 的 tool 调用任务。
type ToolTask struct {
	CallID    string `json:"call_id"`
	UserID    string `json:"user_id"`
	Tool      string `json:"tool"`
	ArgsJSON  string `json:"args_json"` // raw JSON
	TimeoutMS int    `json:"timeout_ms,omitempty"`
	TraceID   string `json:"trace_id"`
	Attempt   int    `json:"attempt,omitempty"`
}

// ToolResultEvent worker → gateway 的 tool 调用结果。
type ToolResultEvent struct {
	CallID      string `json:"call_id"`
	TraceID     string `json:"trace_id,omitempty"`
	ContainerID string `json:"container_id,omitempty"`
	ExitCode    int    `json:"exit_code"`
	Content     string `json:"content"`
	IsError     bool   `json:"is_error,omitempty"`
	MetaJSON    string `json:"meta_json,omitempty"`
	ElapsedMS   int64  `json:"elapsed_ms"`
	ErrorCode   string `json:"error_code,omitempty"`
	ErrorMsg    string `json:"error_message,omitempty"`
}

// ToolStreamQueue 与 StreamQueue 同思路，独立 stream + DLQ，用于 ExecTool。
type ToolStreamQueue struct {
	cli    *redis.Client
	stream string
	dlq    string
}

func NewToolStream(cli *redis.Client) *ToolStreamQueue {
	return &ToolStreamQueue{
		cli:    cli,
		stream: redisstore.Keys.QueueToolTasks,
		dlq:    redisstore.Keys.QueueToolTasksDLQ,
	}
}

func (q *ToolStreamQueue) EnsureGroup(ctx context.Context, group string) error {
	err := q.cli.XGroupCreateMkStream(ctx, q.stream, group, "0").Err()
	if err != nil && !isBusyGroup(err) {
		return fmt.Errorf("create tool group: %w", err)
	}
	return nil
}

func (q *ToolStreamQueue) Publish(ctx context.Context, t ToolTask) (string, error) {
	payload, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	return q.cli.XAdd(ctx, &redis.XAddArgs{
		Stream: q.stream,
		Values: map[string]any{"task": string(payload)},
	}).Result()
}

// ToolDelivery 投递给 handler 的封装。
type ToolDelivery struct {
	ID   string
	Task ToolTask
}

// ToolHandler 处理一条 tool 任务。
type ToolHandler func(ctx context.Context, d ToolDelivery) error

// Consume 阻塞消费 tool 任务流。tool 调用一般不重试（结果已通过 pubsub 返回给 caller）。
func (q *ToolStreamQueue) Consume(ctx context.Context, group, consumer string, maxRetry int, h ToolHandler) error {
	if maxRetry <= 0 {
		maxRetry = 1
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		res, err := q.cli.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    group,
			Consumer: consumer,
			Streams:  []string{q.stream, ">"},
			Count:    1,
			Block:    2 * time.Second,
		}).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) || errors.Is(err, context.Canceled) {
				continue
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
			continue
		}
		for _, st := range res {
			for _, msg := range st.Messages {
				rawAny, ok := msg.Values["task"]
				if !ok {
					_ = q.cli.XAck(ctx, q.stream, group, msg.ID).Err()
					continue
				}
				raw, _ := rawAny.(string)
				var t ToolTask
				if err := json.Unmarshal([]byte(raw), &t); err != nil {
					_ = q.deadLetter(ctx, group, msg.ID, raw, err)
					continue
				}
				if err := h(ctx, ToolDelivery{ID: msg.ID, Task: t}); err == nil {
					_ = q.cli.XAck(ctx, q.stream, group, msg.ID).Err()
					continue
				} else {
					t.Attempt++
					if t.Attempt >= maxRetry {
						_ = q.deadLetter(ctx, group, msg.ID, raw, err)
						continue
					}
					newPayload, _ := json.Marshal(t)
					_, _ = q.cli.XAdd(ctx, &redis.XAddArgs{
						Stream: q.stream,
						Values: map[string]any{"task": string(newPayload)},
					}).Result()
					_ = q.cli.XAck(ctx, q.stream, group, msg.ID).Err()
				}
			}
		}
	}
}

func (q *ToolStreamQueue) deadLetter(ctx context.Context, group, id, raw string, hErr error) error {
	_, addErr := q.cli.XAdd(ctx, &redis.XAddArgs{
		Stream: q.dlq,
		Values: map[string]any{"task": raw, "error": hErr.Error(), "from": id},
	}).Result()
	_ = q.cli.XAck(ctx, q.stream, group, id).Err()
	return addErr
}

// ToolBus 是 worker → gateway 的 tool 结果回传通道（基于 redis pubsub）。
type ToolBus struct {
	cli *redis.Client
}

func NewToolBus(cli *redis.Client) *ToolBus { return &ToolBus{cli: cli} }

// Publish 发布到 tool_results:{call_id}。
func (b *ToolBus) Publish(ctx context.Context, callID string, ev ToolResultEvent) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	return b.cli.Publish(ctx, redisstore.Keys.ToolResultsTopic(callID), payload).Err()
}

// Subscribe 订阅 tool_results:{call_id}；返回 channel + cancel。
func (b *ToolBus) Subscribe(ctx context.Context, callID string) (<-chan ToolResultEvent, func(), error) {
	sub := b.cli.Subscribe(ctx, redisstore.Keys.ToolResultsTopic(callID))
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return nil, nil, err
	}
	out := make(chan ToolResultEvent, 4)
	done := make(chan struct{})
	go func() {
		defer close(out)
		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var ev ToolResultEvent
				if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
					continue
				}
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				case <-done:
					return
				}
			}
		}
	}()
	cancel := func() {
		close(done)
		_ = sub.Close()
	}
	return out, cancel, nil
}
