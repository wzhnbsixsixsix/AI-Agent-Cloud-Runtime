package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wzhnbsixsixsix/agentforge/internal/history"
	"github.com/wzhnbsixsixsix/agentforge/internal/llm"
	"github.com/wzhnbsixsixsix/agentforge/internal/obs"
	"github.com/wzhnbsixsixsix/agentforge/internal/queue"
)

// Runner 是 worker 内执行单个 Run 的内核。
type Runner struct {
	History  history.Store
	Provider llm.Provider
	Events   *queue.PubSub
}

// NewRunner constructor。
func NewRunner(h history.Store, p llm.Provider, ev *queue.PubSub) *Runner {
	return &Runner{History: h, Provider: p, Events: ev}
}

// Run 完整执行流程：状态机 → 写 user 历史 → LLM 流式 → 透传 token → 落 assistant 历史 → 终态事件。
func (r *Runner) Run(ctx context.Context, t queue.Task) error {
	if t.RunID == "" {
		return errors.New("empty run id")
	}
	ctx = obs.WithTraceID(obs.WithRunID(ctx, t.RunID), t.TraceID)
	log := obs.LoggerFromContext(ctx)
	log.Info("run start", "user", t.UserID, "model", t.Model)

	cur := StatePending
	if err := r.publishState(ctx, t, cur, StateRunning); err != nil {
		log.Warn("publish state running failed", "err", err)
	}
	cur = StateRunning

	// 1) 写 user 消息
	if _, err := r.History.Append(ctx, t.RunID, history.Message{
		Role:    history.RoleUser,
		Content: t.Prompt,
	}); err != nil {
		r.fail(ctx, t, cur, "history_user", err)
		return err
	}

	// 2) 取上下文
	prior, err := r.History.Render(ctx, t.RunID)
	if err != nil {
		r.fail(ctx, t, cur, "history_render", err)
		return err
	}
	msgs := make([]llm.Message, 0, len(prior)+1)
	msgs = append(msgs, llm.Message{
		Role:    llm.RoleSystem,
		Content: "You are AgentForge runtime, a helpful assistant. Answer concisely.",
	})
	for _, m := range prior {
		msgs = append(msgs, llm.Message{Role: llm.Role(m.Role), Content: m.Content})
	}

	// 3) 调 LLM 流式
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch, err := r.Provider.Stream(streamCtx, llm.Req{
		Model:    t.Model,
		Messages: msgs,
	})
	if err != nil {
		r.fail(ctx, t, cur, "llm_stream", err)
		return err
	}

	var (
		buf       []byte
		idx       int64
		tokenCnt  int64
		startTime = time.Now()
	)
	for ev := range ch {
		if ev.Err != nil {
			r.fail(ctx, t, cur, "llm_chunk", ev.Err)
			return ev.Err
		}
		if ev.Done {
			break
		}
		tokenCnt++
		buf = append(buf, ev.Token...)
		_ = r.Events.Publish(ctx, t.RunID, queue.Event{
			RunID:   t.RunID,
			TraceID: t.TraceID,
			Kind:    queue.EventToken,
			Text:    ev.Token,
			Index:   idx,
		})
		idx++
	}

	// 4) 落 assistant 完整消息
	if len(buf) > 0 {
		if _, err := r.History.Append(ctx, t.RunID, history.Message{
			Role:    history.RoleAssistant,
			Content: string(buf),
		}); err != nil {
			log.Warn("history assistant append", "err", err)
		}
	}

	// 5) DONE 事件
	_ = r.publishState(ctx, t, cur, StateDone)
	_ = r.Events.Publish(ctx, t.RunID, queue.Event{
		RunID:   t.RunID,
		TraceID: t.TraceID,
		Kind:    queue.EventDone,
		Total:   tokenCnt,
	})
	log.Info("run done", "tokens", tokenCnt, "elapsed_ms", time.Since(startTime).Milliseconds())
	return nil
}

func (r *Runner) publishState(ctx context.Context, t queue.Task, from, to State) error {
	if !CanTransit(from, to) {
		return fmt.Errorf("invalid transition %s -> %s", from, to)
	}
	return r.Events.Publish(ctx, t.RunID, queue.Event{
		RunID:   t.RunID,
		TraceID: t.TraceID,
		Kind:    queue.EventState,
		From:    string(from),
		State:   string(to),
	})
}

func (r *Runner) fail(ctx context.Context, t queue.Task, from State, code string, err error) {
	log := obs.LoggerFromContext(ctx)
	log.Error("run failed", "code", code, "err", err)
	_ = r.publishState(ctx, t, from, StateFailed)
	_ = r.Events.Publish(ctx, t.RunID, queue.Event{
		RunID:   t.RunID,
		TraceID: t.TraceID,
		Kind:    queue.EventError,
		Code:    code,
		Message: err.Error(),
	})
}
