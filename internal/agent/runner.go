package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/wzhnbsixsixsix/agentforge/internal/history"
	"github.com/wzhnbsixsixsix/agentforge/internal/llm"
	"github.com/wzhnbsixsixsix/agentforge/internal/obs"
	"github.com/wzhnbsixsixsix/agentforge/internal/queue"
	"github.com/wzhnbsixsixsix/agentforge/internal/tool"
)

// Runner 是 worker 内执行单个 Run 的内核。
type Runner struct {
	History      history.Store
	Provider     llm.Provider
	Events       *queue.PubSub
	ToolRunner   *tool.Runner
	ToolMaxSteps int
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

	var (
		idx        int64
		tokenCnt   int64
		toolRounds int
		startTime  = time.Now()
	)
	tools := r.llmTools()
	maxToolSteps := r.ToolMaxSteps
	if maxToolSteps <= 0 {
		maxToolSteps = 5
	}

	for {
		assistantText, toolCalls, emitted, err := r.streamOnce(ctx, t, msgs, tools, &idx)
		tokenCnt += emitted
		if err != nil {
			r.fail(ctx, t, cur, "llm_chunk", err)
			return err
		}

		assistantMsg := llm.Message{
			Role:      llm.RoleAssistant,
			Content:   assistantText,
			ToolCalls: toolCalls,
		}
		msgs = append(msgs, assistantMsg)

		var assistantMsgID string
		if assistantText != "" || len(toolCalls) > 0 {
			tags := map[string]string{}
			if len(toolCalls) > 0 {
				tags["tool_calls"] = marshalToolCallsTag(toolCalls)
			}
			assistantMsgID, err = r.History.Append(ctx, t.RunID, history.Message{
				Role:    history.RoleAssistant,
				Content: assistantText,
				Tags:    tags,
			})
			if err != nil {
				log.Warn("history assistant append", "err", err)
			}
		}

		if len(toolCalls) == 0 {
			break
		}
		if r.ToolRunner == nil || r.ToolRunner.Registry == nil {
			err := errors.New("model requested tool calls but tool runtime is disabled")
			r.fail(ctx, t, cur, "tool_unavailable", err)
			return err
		}
		if toolRounds >= maxToolSteps {
			err := fmt.Errorf("tool call loop exceeded max steps (%d)", maxToolSteps)
			r.fail(ctx, t, cur, "tool_loop_limit", err)
			return err
		}
		toolRounds++

		if err := r.publishState(ctx, t, cur, StateWaitingTool); err != nil {
			log.Warn("publish state waiting_tool failed", "err", err)
		}
		cur = StateWaitingTool
		for _, tc := range toolCalls {
			callID := tc.ID
			if callID == "" {
				callID = obs.NewRunID()
			}
			ev, err := r.ToolRunner.Execute(ctx, callID, t.TraceID, tc.Name, []byte(tc.Arguments), 0)
			if err != nil {
				r.fail(ctx, t, cur, "tool_execute", err)
				return err
			}
			msgs = append(msgs, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: callID,
				Name:       tc.Name,
				Content:    ev.Content,
			})
			tags := map[string]string{
				"tool_call_id": callID,
				"tool_name":    tc.Name,
				"is_error":     fmt.Sprintf("%v", ev.IsError),
			}
			if ev.ErrorCode != "" {
				tags["error_code"] = ev.ErrorCode
			}
			if ev.MetaJSON != "" {
				tags["metadata"] = ev.MetaJSON
			}
			if _, err := r.History.Append(ctx, t.RunID, history.Message{
				Role:     history.RoleTool,
				Content:  ev.Content,
				ParentID: assistantMsgID,
				Tags:     tags,
			}); err != nil {
				log.Warn("history tool append", "err", err)
			}
		}
		if err := r.publishState(ctx, t, cur, StateRunning); err != nil {
			log.Warn("publish state running failed", "err", err)
		}
		cur = StateRunning
	}

	// 4) DONE 事件
	_ = r.publishState(ctx, t, cur, StateDone)
	_ = r.Events.Publish(ctx, t.RunID, queue.Event{
		RunID:   t.RunID,
		TraceID: t.TraceID,
		Kind:    queue.EventDone,
		Total:   tokenCnt,
	})
	log.Info("run done", "tokens", tokenCnt, "tool_rounds", toolRounds, "elapsed_ms", time.Since(startTime).Milliseconds())
	return nil
}

func (r *Runner) streamOnce(ctx context.Context, t queue.Task, msgs []llm.Message, tools []llm.ToolDefinition, idx *int64) (string, []llm.ToolCall, int64, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch, err := r.Provider.Stream(streamCtx, llm.Req{
		Model:    t.Model,
		Messages: msgs,
		Tools:    tools,
	})
	if err != nil {
		return "", nil, 0, err
	}

	var (
		buf       []byte
		toolCalls []llm.ToolCall
		tokenCnt  int64
	)
	for ev := range ch {
		if ev.Err != nil {
			return "", nil, tokenCnt, ev.Err
		}
		if len(ev.ToolCalls) > 0 {
			toolCalls = append(toolCalls, ev.ToolCalls...)
		}
		if ev.Token != "" {
			tokenCnt++
			buf = append(buf, ev.Token...)
			_ = r.Events.Publish(ctx, t.RunID, queue.Event{
				RunID:   t.RunID,
				TraceID: t.TraceID,
				Kind:    queue.EventToken,
				Text:    ev.Token,
				Index:   *idx,
			})
			*idx = *idx + 1
		}
		if ev.Done {
			break
		}
	}
	return string(buf), toolCalls, tokenCnt, nil
}

func (r *Runner) llmTools() []llm.ToolDefinition {
	if r.ToolRunner == nil || r.ToolRunner.Registry == nil {
		return nil
	}
	descs := r.ToolRunner.Registry.List()
	out := make([]llm.ToolDefinition, 0, len(descs))
	for _, d := range descs {
		out = append(out, llm.ToolDefinition{
			Name:        d.Name,
			Description: d.Description,
			Parameters:  d.Schema,
		})
	}
	return out
}

func marshalToolCallsTag(calls []llm.ToolCall) string {
	b, err := json.Marshal(calls)
	if err != nil {
		return "[]"
	}
	return string(b)
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
