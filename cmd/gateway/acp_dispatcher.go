package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	internalacp "github.com/wzhnbsixsixsix/agentforge/internal/acp"
	"github.com/wzhnbsixsixsix/agentforge/internal/obs"
	"github.com/wzhnbsixsixsix/agentforge/internal/queue"
)

// acpDispatcher 把 ACP RUN 帧适配到 Redis Stream + Pub/Sub 业务链路。
//
// 流程与 gRPC 入口完全一致，差异只在最外层的"如何把事件回送给客户端"。
type acpDispatcher struct {
	q          *queue.StreamQueue
	ps         *queue.PubSub
	log        *slog.Logger
	runTimeout time.Duration
}

// idGenAdapter 让 obs 的 ULID 工具实现 internal/acp.IDGenerator。
type idGenAdapter struct{}

func (idGenAdapter) NewRunID() string   { return obs.NewRunID() }
func (idGenAdapter) NewTraceID() string { return obs.NewTraceID() }

// Run 实现 internal/acp.Dispatcher。
func (d *acpDispatcher) Run(ctx context.Context, in internalacp.RunInput, emit internalacp.EmitFunc) error {
	ctx = obs.WithRunID(obs.WithTraceID(ctx, in.TraceID), in.RunID)
	log := obs.LoggerFromContext(ctx)

	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	evCh, evCancel, err := d.ps.Subscribe(subCtx, in.RunID)
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	defer evCancel()

	if _, err := d.q.Publish(ctx, queue.Task{
		RunID:   in.RunID,
		UserID:  in.UserID,
		Prompt:  in.Prompt,
		Model:   in.Model,
		TraceID: in.TraceID,
	}); err != nil {
		return fmt.Errorf("publish task: %w", err)
	}
	log.Info("acp task dispatched", "user", in.UserID)

	timeout := d.runTimeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	hard, hardCancel := context.WithTimeout(ctx, timeout)
	defer hardCancel()

	for {
		select {
		case <-hard.Done():
			_ = emit(internalacp.BusinessEvent{Kind: "error", Code: "timeout", Message: "run timeout"})
			return nil
		case ev, ok := <-evCh:
			if !ok {
				return nil
			}
			be := mapQueueEventACP(ev)
			if be.Kind == "" {
				continue
			}
			if err := emit(be); err != nil {
				log.Warn("acp emit", "err", err)
				return nil
			}
			if be.Kind == "done" || be.Kind == "error" {
				return nil
			}
		}
	}
}

func mapQueueEventACP(ev queue.Event) internalacp.BusinessEvent {
	switch ev.Kind {
	case queue.EventToken:
		return internalacp.BusinessEvent{Kind: "token", Text: ev.Text, Index: ev.Index}
	case queue.EventState:
		return internalacp.BusinessEvent{Kind: "state", From: ev.From, State: ev.State}
	case queue.EventDone:
		return internalacp.BusinessEvent{Kind: "done", Total: ev.Total}
	case queue.EventError:
		return internalacp.BusinessEvent{Kind: "error", Code: ev.Code, Message: ev.Message}
	}
	return internalacp.BusinessEvent{}
}
