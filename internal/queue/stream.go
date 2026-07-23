// Package queue 封装 Redis Stream 任务队列与 Pub/Sub 事件广播。
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wzhnbsixsixsix/agentforge/internal/obs"
	redisstore "github.com/wzhnbsixsixsix/agentforge/internal/storage/redis"

	"github.com/redis/go-redis/v9"
)

// Task 投递给 worker 的任务体。
type Task struct {
	RunID        string `json:"run_id"`
	UserID       string `json:"user_id"`
	AgentID      string `json:"agent_id,omitempty"`
	Prompt       string `json:"prompt"`
	Model        string `json:"model,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	TraceID      string `json:"trace_id"`
	Attempt      int    `json:"attempt,omitempty"`
}

// StreamQueue 基于 Redis Stream + Consumer Group 的任务队列。
type StreamQueue struct {
	cli    *redis.Client
	stream string
	dlq    string
}

// NewStream 构造 StreamQueue。
func NewStream(cli *redis.Client) *StreamQueue {
	return &StreamQueue{
		cli:    cli,
		stream: redisstore.Keys.QueueTasks,
		dlq:    redisstore.Keys.QueueTasksDLQ,
	}
}

// EnsureGroup 幂等创建 consumer group。
func (q *StreamQueue) EnsureGroup(ctx context.Context, group string) error {
	err := q.cli.XGroupCreateMkStream(ctx, q.stream, group, "0").Err()
	if err != nil && !isBusyGroup(err) {
		return fmt.Errorf("create group: %w", err)
	}
	return nil
}

func isBusyGroup(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "BUSYGROUP")
}

// Publish 投递一个 Task。返回 stream entry id。
func (q *StreamQueue) Publish(ctx context.Context, t Task) (string, error) {
	payload, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	id, err := q.cli.XAdd(ctx, &redis.XAddArgs{
		Stream: q.stream,
		Values: map[string]any{"task": string(payload)},
	}).Result()
	if err != nil {
		obs.QueueEvents.WithLabelValues(obs.ServiceName(), "task_publish", "error").Inc()
		return "", fmt.Errorf("xadd: %w", err)
	}
	obs.QueueEvents.WithLabelValues(obs.ServiceName(), "task_publish", "ok").Inc()
	return id, nil
}

// Delivery 是 worker 处理函数收到的任务封装。
type Delivery struct {
	ID   string
	Task Task
}

// Handler 处理一条任务；返回 nil 表示成功（会自动 XACK）。
type Handler func(ctx context.Context, d Delivery) error

// Consume 阻塞消费，直到 ctx 取消。
//   - group: consumer group 名
//   - consumer: 当前 worker 实例 id
//   - maxRetry: 处理失败累计达到后转 DLQ
func (q *StreamQueue) Consume(ctx context.Context, group, consumer string, maxRetry int, h Handler) error {
	if maxRetry <= 0 {
		maxRetry = 3
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
			// 短暂退避，避免 redis 抖动后 busy loop
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
				var t Task
				if err := json.Unmarshal([]byte(raw), &t); err != nil {
					_ = q.deadLetter(ctx, group, msg.ID, raw, err)
					continue
				}
				err := h(ctx, Delivery{ID: msg.ID, Task: t})
				if err == nil {
					obs.QueueEvents.WithLabelValues(obs.ServiceName(), "task_consume", "ok").Inc()
					_ = q.cli.XAck(ctx, q.stream, group, msg.ID).Err()
					continue
				}
				obs.QueueEvents.WithLabelValues(obs.ServiceName(), "task_consume", "error").Inc()
				t.Attempt++
				if t.Attempt >= maxRetry {
					_ = q.deadLetter(ctx, group, msg.ID, raw, err)
					continue
				}
				// 简单重试：重新投递新条目（保留 attempt），ack 老条目
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

func (q *StreamQueue) deadLetter(ctx context.Context, group, id, raw string, hErr error) error {
	_, addErr := q.cli.XAdd(ctx, &redis.XAddArgs{
		Stream: q.dlq,
		Values: map[string]any{"task": raw, "error": hErr.Error(), "from": id},
	}).Result()
	_ = q.cli.XAck(ctx, q.stream, group, id).Err()
	return addErr
}
