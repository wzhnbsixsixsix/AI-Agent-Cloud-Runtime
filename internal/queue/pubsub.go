package queue

import (
	"context"
	"encoding/json"

	redisstore "github.com/wzhnbsixsixsix/agentforge/internal/storage/redis"

	"github.com/redis/go-redis/v9"
)

// EventKind 事件类型。
type EventKind string

const (
	EventState EventKind = "state"
	EventToken EventKind = "token"
	EventDone  EventKind = "done"
	EventError EventKind = "error"
)

// Event worker → gateway 的事件载体。
type Event struct {
	RunID   string    `json:"run_id"`
	TraceID string    `json:"trace_id"`
	Kind    EventKind `json:"kind"`
	// 联合字段，按 Kind 取
	State   string `json:"state,omitempty"`
	From    string `json:"from,omitempty"`
	Text    string `json:"text,omitempty"`  // token
	Index   int64  `json:"index,omitempty"` // token index
	Code    string `json:"code,omitempty"`  // error code
	Message string `json:"message,omitempty"`
	Total   int64  `json:"total,omitempty"`
}

// PubSub 简单封装 Redis Pub/Sub。
type PubSub struct {
	cli *redis.Client
}

// NewPubSub constructor。
func NewPubSub(cli *redis.Client) *PubSub { return &PubSub{cli: cli} }

// Publish 发到 events:{run_id}。
func (p *PubSub) Publish(ctx context.Context, runID string, ev Event) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	return p.cli.Publish(ctx, redisstore.Keys.EventsTopic(runID), payload).Err()
}

// Subscribe 返回 Event channel + cancel func；使用方关闭 cancel 释放订阅。
func (p *PubSub) Subscribe(ctx context.Context, runID string) (<-chan Event, func(), error) {
	sub := p.cli.Subscribe(ctx, redisstore.Keys.EventsTopic(runID))
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return nil, nil, err
	}
	out := make(chan Event, 256)
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
				var ev Event
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
