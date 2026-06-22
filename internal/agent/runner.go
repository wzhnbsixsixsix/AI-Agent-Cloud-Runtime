package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wzhnbsixsixsix/agentforge/internal/history"
	"github.com/wzhnbsixsixsix/agentforge/internal/hook"
	"github.com/wzhnbsixsixsix/agentforge/internal/llm"
	"github.com/wzhnbsixsixsix/agentforge/internal/obs"
	"github.com/wzhnbsixsixsix/agentforge/internal/orchestrator"
	"github.com/wzhnbsixsixsix/agentforge/internal/queue"
	"github.com/wzhnbsixsixsix/agentforge/internal/rag"
	"github.com/wzhnbsixsixsix/agentforge/internal/skill"
	"github.com/wzhnbsixsixsix/agentforge/internal/tool"
)

// Runner 是 worker 内执行单个 Run 的内核。
type Runner struct {
	History             history.Store
	Provider            llm.Provider
	Events              *queue.PubSub
	ToolRunner          *tool.Runner
	ToolMaxSteps        int
	SkillSelector       skill.Selector
	SkillRenderer       skill.Renderer
	RAGRetriever        rag.Retriever
	RAGTenantID         string
	RAGTopK             int
	RAGMinScore         float64
	MultiAgentEnabled   bool
	SubagentMaxDepth    int
	SubagentMaxChildren int
	SubagentTimeout     time.Duration
	Depth               int
	SubagentChildren    map[string]int
	CompactPolicy       orchestrator.CompactPolicy
	HookClient          hook.Client
	HookFailClosed      bool
}

// NewRunner constructor。
func NewRunner(h history.Store, p llm.Provider, ev *queue.PubSub) *Runner {
	return &Runner{History: h, Provider: p, Events: ev}
}

// Run 完整执行流程：状态机 → 写 user 历史 → LLM 流式 → 透传 token → 落 assistant 历史 → 终态事件。
func (r *Runner) Run(ctx context.Context, t queue.Task) (runErr error) {
	if t.RunID == "" {
		return errors.New("empty run id")
	}
	ctx = obs.WithTraceID(obs.WithRunID(ctx, t.RunID), t.TraceID)
	ctx, span := obs.StartSpan(ctx, "agent.run",
		obs.Attr("agentforge.user_id", t.UserID),
		obs.Attr("llm.model", t.Model),
	)
	runStart := time.Now()
	defer func() {
		status := "ok"
		if runErr != nil {
			status = "error"
		}
		obs.RunsTotal.WithLabelValues(obs.ServiceName(), status).Inc()
		obs.RunDuration.WithLabelValues(obs.ServiceName(), status).Observe(time.Since(runStart).Seconds())
		obs.EndSpan(span, runErr)
	}()
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
	if ok, err := r.compactHistory(ctx, t, cur, prior); err != nil {
		log.Warn("history compact failed", "err", err)
	} else if ok {
		prior, err = r.History.Render(ctx, t.RunID)
		if err != nil {
			r.fail(ctx, t, cur, "history_render_after_compact", err)
			return err
		}
		if err := r.publishState(ctx, t, StateCompacting, StateRunning); err != nil {
			log.Warn("publish state running after compact failed", "err", err)
		}
		cur = StateRunning
	}
	msgs := make([]llm.Message, 0, len(prior)+2)
	msgs = append(msgs, llm.Message{
		Role:    llm.RoleSystem,
		Content: "You are AgentForge runtime, a helpful assistant. Answer concisely.",
	})
	if r.SkillSelector != nil {
		selectStart := time.Now()
		selectCtx, selectSpan := obs.StartSpan(ctx, "skill.select")
		selected, err := r.SkillSelector.Select(selectCtx, t.Prompt)
		selectStatus := "ok"
		if err != nil {
			selectStatus = "error"
			log.Warn("skill select failed", "err", err)
		} else if rendered := r.SkillRenderer.RenderSystemMessage(selected); rendered != "" {
			msgs = append(msgs, llm.Message{
				Role:    llm.RoleSystem,
				Content: rendered,
			})
			log.Info("skills loaded", "count", len(selected))
		}
		obs.SkillDuration.WithLabelValues(obs.ServiceName(), selectStatus).Observe(time.Since(selectStart).Seconds())
		obs.SkillSelected.WithLabelValues(obs.ServiceName()).Observe(float64(len(selected)))
		obs.EndSpan(selectSpan, err)
	}
	if r.RAGRetriever != nil {
		tenantID := r.RAGTenantID
		if tenantID == "" {
			tenantID = "default"
		}
		ragStart := time.Now()
		ragCtx, ragSpan := obs.StartSpan(ctx, "rag.retrieve", obs.Attr("rag.tenant_id", tenantID))
		results, err := r.RAGRetriever.Retrieve(ragCtx, tenantID, t.Prompt, r.RAGTopK, r.RAGMinScore)
		ragStatus := "ok"
		if err != nil {
			ragStatus = "error"
			log.Warn("rag retrieve failed", "err", err)
		} else if rendered := rag.RenderSystemMessage(results); rendered != "" {
			msgs = append(msgs, llm.Message{
				Role:    llm.RoleSystem,
				Content: rendered,
			})
			log.Info("rag context loaded", "tenant", tenantID, "chunks", len(results))
		}
		obs.RAGDuration.WithLabelValues(obs.ServiceName(), ragStatus).Observe(time.Since(ragStart).Seconds())
		obs.RAGResults.WithLabelValues(obs.ServiceName()).Observe(float64(len(results)))
		obs.EndSpan(ragSpan, err)
	}
	if r.HookClient != nil {
		hookStart := time.Now()
		hookCtx, hookSpan := obs.StartSpan(ctx, "hook.pre_llm", obs.Attr("hook.event", string(hook.EventPreLLM)))
		resp, err := r.HookClient.Execute(hookCtx, hook.Request{
			Event:   hook.EventPreLLM,
			RunID:   t.RunID,
			TraceID: t.TraceID,
		})
		hookStatus := "ok"
		if err != nil {
			hookStatus = "error"
			log.Warn("pre-llm hook failed", "err", err)
			if r.HookFailClosed {
				obs.HookTotal.WithLabelValues(obs.ServiceName(), string(hook.EventPreLLM), hookStatus).Inc()
				obs.HookDuration.WithLabelValues(obs.ServiceName(), string(hook.EventPreLLM), hookStatus).Observe(time.Since(hookStart).Seconds())
				obs.EndSpan(hookSpan, err)
				r.fail(ctx, t, cur, "hook_pre_llm", err)
				return err
			}
		} else if resp.Allowed {
			for _, content := range resp.AppendSystemMessages {
				if strings.TrimSpace(content) != "" {
					msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: content})
				}
			}
		}
		obs.HookTotal.WithLabelValues(obs.ServiceName(), string(hook.EventPreLLM), hookStatus).Inc()
		obs.HookDuration.WithLabelValues(obs.ServiceName(), string(hook.EventPreLLM), hookStatus).Observe(time.Since(hookStart).Seconds())
		obs.EndSpan(hookSpan, err)
	}
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
			r.executePostLLMHook(ctx, t, assistantText, toolCalls)
			break
		}
		r.executePostLLMHook(ctx, t, assistantText, toolCalls)
		if (r.ToolRunner == nil || r.ToolRunner.Registry == nil) && !(r.MultiAgentEnabled && hasOnlyDispatchSubagent(toolCalls)) {
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
			ev, err := r.executeToolCall(ctx, t, callID, tc)
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
	obs.RunTokens.WithLabelValues(obs.ServiceName()).Add(float64(tokenCnt))
	log.Info("run done", "tokens", tokenCnt, "tool_rounds", toolRounds, "elapsed_ms", time.Since(startTime).Milliseconds())
	return nil
}

func (r *Runner) executePostLLMHook(ctx context.Context, t queue.Task, text string, toolCalls []llm.ToolCall) {
	if r.HookClient == nil {
		return
	}
	start := time.Now()
	hookCtx, span := obs.StartSpan(ctx, "hook.post_llm", obs.Attr("hook.event", string(hook.EventPostLLM)))
	payload := map[string]any{"assistant_text": text, "tool_calls": toolCalls}
	raw, _ := json.Marshal(payload)
	_, err := r.HookClient.Execute(hookCtx, hook.Request{
		Event:       hook.EventPostLLM,
		RunID:       t.RunID,
		TraceID:     t.TraceID,
		PayloadJSON: raw,
	})
	status := "ok"
	if err != nil {
		status = "error"
	}
	obs.HookTotal.WithLabelValues(obs.ServiceName(), string(hook.EventPostLLM), status).Inc()
	obs.HookDuration.WithLabelValues(obs.ServiceName(), string(hook.EventPostLLM), status).Observe(time.Since(start).Seconds())
	obs.EndSpan(span, err)
}

func (r *Runner) streamOnce(ctx context.Context, t queue.Task, msgs []llm.Message, tools []llm.ToolDefinition, idx *int64) (string, []llm.ToolCall, int64, error) {
	start := time.Now()
	streamCtx, span := obs.StartSpan(ctx, "llm.stream",
		obs.Attr("llm.provider", r.Provider.Name()),
		obs.Attr("llm.model", t.Model),
	)
	var streamErr error
	defer func() {
		status := "ok"
		if streamErr != nil {
			status = "error"
		}
		obs.LLMStreamDuration.WithLabelValues(obs.ServiceName(), r.Provider.Name(), status).Observe(time.Since(start).Seconds())
		obs.EndSpan(span, streamErr)
	}()
	streamCtx, cancel := context.WithCancel(streamCtx)
	defer cancel()
	ch, err := r.Provider.Stream(streamCtx, llm.Req{
		Model:    t.Model,
		Messages: msgs,
		Tools:    tools,
	})
	if err != nil {
		streamErr = err
		return "", nil, 0, err
	}

	var (
		buf       []byte
		toolCalls []llm.ToolCall
		tokenCnt  int64
	)
	for ev := range ch {
		if ev.Err != nil {
			streamErr = ev.Err
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
	var out []llm.ToolDefinition
	if r.ToolRunner != nil && r.ToolRunner.Registry != nil {
		descs := r.ToolRunner.Registry.List()
		out = make([]llm.ToolDefinition, 0, len(descs)+1)
		for _, d := range descs {
			out = append(out, llm.ToolDefinition{
				Name:        d.Name,
				Description: d.Description,
				Parameters:  d.Schema,
			})
		}
	}
	if r.MultiAgentEnabled {
		out = append(out, dispatchSubagentTool())
	}
	return out
}

func (r *Runner) compactHistory(ctx context.Context, t queue.Task, from State, prior []history.Message) (compacted bool, compactErr error) {
	ctx, span := obs.StartSpan(ctx, "history.compact")
	defer func() { obs.EndSpan(span, compactErr) }()
	if !r.CompactPolicy.Enabled {
		return false, nil
	}
	maxChars := r.CompactPolicy.MaxChars
	if maxChars <= 0 {
		maxChars = 24000
	}
	var total int
	for _, m := range prior {
		total += len([]rune(m.Content))
	}
	keepHead := r.CompactPolicy.KeepHead
	if keepHead < 0 {
		keepHead = 0
	}
	keepTail := r.CompactPolicy.KeepTail
	if keepTail <= 0 {
		keepTail = 8
	}
	if total <= maxChars || len(prior) <= keepHead+keepTail+1 {
		return false, nil
	}
	if err := r.publishState(ctx, t, from, StateCompacting); err != nil {
		return false, err
	}
	return r.CompactPolicy.CompactIfNeeded(ctx, r.History, t.RunID, prior)
}

func (r *Runner) executeToolCall(ctx context.Context, t queue.Task, callID string, tc llm.ToolCall) (ev queue.ToolResultEvent, err error) {
	ctx, span := obs.StartSpan(ctx, "agent.tool_call", obs.Attr("tool.name", tc.Name))
	defer func() { obs.EndSpan(span, err) }()
	if tc.Name == "dispatch_subagent" && r.MultiAgentEnabled {
		return r.dispatchSubagent(ctx, t, callID, tc)
	}
	return r.ToolRunner.Execute(ctx, callID, t.TraceID, tc.Name, []byte(tc.Arguments), 0)
}

func (r *Runner) dispatchSubagent(ctx context.Context, t queue.Task, callID string, tc llm.ToolCall) (queue.ToolResultEvent, error) {
	var req orchestrator.SubagentRequest
	if err := json.Unmarshal([]byte(tc.Arguments), &req); err != nil {
		return queue.ToolResultEvent{
			CallID:    callID,
			TraceID:   t.TraceID,
			Content:   orchestrator.SubagentResult{Status: "error", Error: err.Error()}.Marshal(),
			IsError:   true,
			ErrorCode: "bad_subagent_args",
		}, nil
	}
	if r.SubagentChildren == nil {
		r.SubagentChildren = map[string]int{}
	}
	sup := &orchestrator.Supervisor{
		Runner:      r,
		MaxDepth:    r.SubagentMaxDepth,
		MaxChildren: r.SubagentMaxChildren,
		Timeout:     r.SubagentTimeout,
		Children:    r.SubagentChildren,
	}
	res, err := sup.Dispatch(ctx, t.RunID, t.TraceID, t.UserID, t.Model, r.Depth, req)
	ev := queue.ToolResultEvent{
		CallID:   callID,
		TraceID:  t.TraceID,
		Content:  res.Marshal(),
		MetaJSON: fmt.Sprintf(`{"child_run_id":%q,"role":%q}`, res.ChildRunID, res.Role),
	}
	if err != nil {
		ev.IsError = true
		ev.ErrorCode = "subagent_error"
	}
	return ev, nil
}

// RunChild executes a child run locally with isolated history and no tool fanout.
func (r *Runner) RunChild(ctx context.Context, req orchestrator.ChildRunRequest) (orchestrator.SubagentResult, error) {
	childTask := queue.Task{
		RunID:   req.ChildRunID,
		UserID:  req.UserID,
		Model:   req.Model,
		TraceID: req.TraceID,
	}
	_ = r.publishState(ctx, childTask, StatePending, StateRunning)

	childPrompt := strings.TrimSpace(req.Task)
	if req.Role != "" {
		childPrompt = "You are a subagent with role: " + req.Role + ".\n\nTask:\n" + childPrompt
	}
	if _, err := r.History.Append(ctx, req.ChildRunID, history.Message{
		Role:    history.RoleUser,
		Content: childPrompt,
		Tags: map[string]string{
			"parent_run_id": req.ParentRunID,
			"subagent_role": req.Role,
		},
	}); err != nil {
		r.fail(ctx, childTask, StateRunning, "child_history_user", err)
		return orchestrator.SubagentResult{}, err
	}
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: "You are an AgentForge subagent. Complete the delegated task and answer with a concise structured summary."},
		{Role: llm.RoleUser, Content: childPrompt},
	}
	var idx int64
	text, _, tokens, err := r.streamOnce(ctx, childTask, msgs, nil, &idx)
	if err != nil {
		r.fail(ctx, childTask, StateRunning, "child_llm_chunk", err)
		return orchestrator.SubagentResult{}, err
	}
	if _, err := r.History.Append(ctx, req.ChildRunID, history.Message{
		Role:    history.RoleAssistant,
		Content: text,
		Tags: map[string]string{
			"parent_run_id": req.ParentRunID,
			"subagent_role": req.Role,
		},
	}); err != nil {
		r.fail(ctx, childTask, StateRunning, "child_history_assistant", err)
		return orchestrator.SubagentResult{}, err
	}
	_ = r.publishState(ctx, childTask, StateRunning, StateDone)
	_ = r.Events.Publish(ctx, req.ChildRunID, queue.Event{
		RunID:   req.ChildRunID,
		TraceID: req.TraceID,
		Kind:    queue.EventDone,
		Total:   tokens,
	})
	return orchestrator.SubagentResult{
		ChildRunID: req.ChildRunID,
		Role:       req.Role,
		Summary:    strings.TrimSpace(text),
		Status:     "ok",
	}, nil
}

func hasOnlyDispatchSubagent(calls []llm.ToolCall) bool {
	if len(calls) == 0 {
		return false
	}
	for _, tc := range calls {
		if tc.Name != "dispatch_subagent" {
			return false
		}
	}
	return true
}

func dispatchSubagentTool() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "dispatch_subagent",
		Description: "Dispatch a local isolated subagent to complete a focused task and return a structured summary.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"role":{"type":"string","description":"Subagent role, such as reviewer, researcher, planner"},"task":{"type":"string","description":"Focused task for the child agent"},"output_schema":{"type":"string","description":"Optional JSON schema the child should follow"}},"required":["role","task"],"additionalProperties":false}`),
	}
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
