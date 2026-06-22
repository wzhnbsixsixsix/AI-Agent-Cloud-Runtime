package tool

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/wzhnbsixsixsix/agentforge/internal/hook"
	"github.com/wzhnbsixsixsix/agentforge/internal/obs"
	"github.com/wzhnbsixsixsix/agentforge/internal/queue"
	"github.com/wzhnbsixsixsix/agentforge/internal/sandbox"
)

// Runner 在 worker 侧消费 ToolTask 并把结果回写到 ToolBus。
//
// 一次 ToolTask 的生命周期：
//  1. queue.ToolDelivery → r.Handle
//  2. Acquire sandbox（per-call，runID = call_id）
//  3. registry.Get(toolName).Invoke(ctx, sb, args)
//  4. Release sandbox（异步销毁 + 补位）
//  5. ToolBus.Publish 把 ToolResultEvent 发回 gateway
//
// 失败也会发布一条 result，gateway 不会傻等。
type Runner struct {
	Registry *Registry
	Driver   sandbox.Driver
	Bus      *queue.ToolBus
	Log      *slog.Logger

	// HardTimeout 任意一次 tool 调用的兜底超时（保护 sandbox 池不被卡住）。
	// 0 → 60s。Task 自带的 TimeoutMS 与之取较小。
	HardTimeout    time.Duration
	HookClient     hook.Client
	HookFailClosed bool
}

// Handle 是 queue.ToolHandler 兼容的回调。返回 nil 即 ack。
func (r *Runner) Handle(ctx context.Context, d queue.ToolDelivery) error {
	t := d.Task
	ev, err := r.Execute(ctx, t.CallID, t.TraceID, t.Tool, []byte(t.ArgsJSON), t.TimeoutMS)
	if err != nil {
		return r.publishError(ctx, t, "execute", err.Error(), time.Now())
	}
	if err := r.Bus.Publish(ctx, t.CallID, ev); err != nil {
		if r.Log != nil {
			r.Log.Warn("publish tool result", "err", err)
		}
		return err
	}
	if r.Log != nil {
		r.Log.Info("tool done",
			"call_id", t.CallID,
			"tool", t.Tool,
			"is_error", ev.IsError,
			"elapsed_ms", ev.ElapsedMS,
			"container", ev.ContainerID)
	}
	return nil
}

// Execute 直接执行一次 tool，供 queue consumer 与 agent function-calling loop 复用。
func (r *Runner) Execute(ctx context.Context, callID, traceID, toolName string, argsJSON []byte, timeoutMS int) (queue.ToolResultEvent, error) {
	ctx = obs.WithTraceID(ctx, traceID)
	ctx, span := obs.StartSpan(ctx, "tool.execute", obs.Attr("tool.name", toolName))
	var execErr error
	defer func() {
		obs.EndSpan(span, execErr)
	}()
	log := slog.Default()
	if r.Log != nil {
		log = r.Log.With("call_id", callID, "tool", toolName, "trace", traceID)
	}
	start := time.Now()

	// 取 tool 描述
	if r.Registry == nil {
		execErr = errors.New("tool registry is nil")
		obs.ToolTotal.WithLabelValues(obs.ServiceName(), toolName, "error").Inc()
		return queue.ToolResultEvent{}, execErr
	}
	if r.Driver == nil {
		execErr = errors.New("sandbox driver is nil")
		obs.ToolTotal.WithLabelValues(obs.ServiceName(), toolName, "error").Inc()
		return queue.ToolResultEvent{}, execErr
	}
	tool, ok := r.Registry.Get(toolName)
	if !ok {
		obs.ToolTotal.WithLabelValues(obs.ServiceName(), toolName, "error").Inc()
		obs.ToolDuration.WithLabelValues(obs.ServiceName(), toolName, "error").Observe(time.Since(start).Seconds())
		return queue.ToolResultEvent{
			CallID:    callID,
			TraceID:   traceID,
			IsError:   true,
			Content:   "unknown tool: " + toolName,
			ErrorCode: "tool_not_found",
			ErrorMsg:  "unknown tool: " + toolName,
			ElapsedMS: time.Since(start).Milliseconds(),
		}, nil
	}

	// 计算执行超时
	hard := r.HardTimeout
	if hard <= 0 {
		hard = 60 * time.Second
	}
	timeout := hard
	if timeoutMS > 0 {
		if want := time.Duration(timeoutMS) * time.Millisecond; want < timeout {
			timeout = want
		}
	}
	tCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Acquire sandbox（即使是 http_fetch 也走一遍，保持调用接口一致；
	// http_fetch 不会用到 sandbox.Exec 但拿到 workspace 仍有意义）。
	acquireStart := time.Now()
	sb, err := r.Driver.Acquire(tCtx, callID)
	if err != nil {
		execErr = err
		obs.SandboxAcquireDuration.WithLabelValues(obs.ServiceName(), "error").Observe(time.Since(acquireStart).Seconds())
		obs.ToolTotal.WithLabelValues(obs.ServiceName(), toolName, "error").Inc()
		obs.ToolDuration.WithLabelValues(obs.ServiceName(), toolName, "error").Observe(time.Since(start).Seconds())
		return queue.ToolResultEvent{}, err
	}
	obs.SandboxAcquireDuration.WithLabelValues(obs.ServiceName(), "ok").Observe(time.Since(acquireStart).Seconds())
	stats := r.Driver.Stats()
	obs.SandboxPoolReady.WithLabelValues(obs.ServiceName(), "docker").Set(float64(stats.PoolReady))
	obs.SandboxInFlight.WithLabelValues(obs.ServiceName(), "docker").Set(float64(stats.InFlight))
	defer func() {
		// Release 用 background ctx；上层 ctx 取消不影响销毁
		_ = r.Driver.Release(context.Background(), sb)
	}()

	if len(argsJSON) == 0 {
		argsJSON = []byte("{}")
	}
	argsJSON, denied, reason, err := r.executePreToolHook(tCtx, callID, traceID, toolName, argsJSON)
	if err != nil {
		execErr = err
		obs.ToolTotal.WithLabelValues(obs.ServiceName(), toolName, "error").Inc()
		obs.ToolDuration.WithLabelValues(obs.ServiceName(), toolName, "error").Observe(time.Since(start).Seconds())
		return queue.ToolResultEvent{}, err
	}
	if denied {
		obs.ToolTotal.WithLabelValues(obs.ServiceName(), toolName, "denied").Inc()
		obs.ToolDuration.WithLabelValues(obs.ServiceName(), toolName, "denied").Observe(time.Since(start).Seconds())
		return queue.ToolResultEvent{
			CallID:    callID,
			TraceID:   traceID,
			IsError:   true,
			Content:   reason,
			ErrorCode: "hook_denied",
			ErrorMsg:  reason,
			ElapsedMS: time.Since(start).Milliseconds(),
		}, nil
	}
	res, err := tool.Invoke(tCtx, sb, argsJSON)
	if err != nil {
		log.Warn("tool invoke internal error", "err", err)
		execErr = err
		obs.ToolTotal.WithLabelValues(obs.ServiceName(), toolName, "error").Inc()
		obs.ToolDuration.WithLabelValues(obs.ServiceName(), toolName, "error").Observe(time.Since(start).Seconds())
		return queue.ToolResultEvent{}, err
	}
	if patched, ok, err := r.executePostToolHook(tCtx, callID, traceID, toolName, res); err != nil {
		execErr = err
		obs.ToolTotal.WithLabelValues(obs.ServiceName(), toolName, "error").Inc()
		obs.ToolDuration.WithLabelValues(obs.ServiceName(), toolName, "error").Observe(time.Since(start).Seconds())
		return queue.ToolResultEvent{}, err
	} else if ok {
		res = patched
	}

	// 序列化 metadata
	var metaJSON string
	if len(res.Metadata) > 0 {
		if b, e := json.Marshal(res.Metadata); e == nil {
			metaJSON = string(b)
		}
	}

	// 取 exit_code（来自 metadata，便于 gateway 直接读）
	exit := 0
	if v, ok := res.Metadata["exit_code"].(int); ok {
		exit = v
	}

	status := "ok"
	if res.IsError {
		status = "error"
	}
	obs.ToolTotal.WithLabelValues(obs.ServiceName(), toolName, status).Inc()
	obs.ToolDuration.WithLabelValues(obs.ServiceName(), toolName, status).Observe(time.Since(start).Seconds())
	return queue.ToolResultEvent{
		CallID:      callID,
		TraceID:     traceID,
		ContainerID: sb.ID(),
		ExitCode:    exit,
		Content:     res.Content,
		IsError:     res.IsError,
		MetaJSON:    metaJSON,
		ElapsedMS:   time.Since(start).Milliseconds(),
	}, nil
}

func (r *Runner) executePreToolHook(ctx context.Context, callID, traceID, toolName string, argsJSON []byte) ([]byte, bool, string, error) {
	if r.HookClient == nil {
		return argsJSON, false, "", nil
	}
	start := time.Now()
	ctx, span := obs.StartSpan(ctx, "hook.pre_tool", obs.Attr("hook.event", string(hook.EventPreToolUse)), obs.Attr("tool.name", toolName))
	var hookErr error
	defer func() {
		status := "ok"
		if hookErr != nil {
			status = "error"
		}
		obs.HookTotal.WithLabelValues(obs.ServiceName(), string(hook.EventPreToolUse), status).Inc()
		obs.HookDuration.WithLabelValues(obs.ServiceName(), string(hook.EventPreToolUse), status).Observe(time.Since(start).Seconds())
		obs.EndSpan(span, hookErr)
	}()
	payload, _ := json.Marshal(map[string]any{
		"call_id":   callID,
		"tool":      toolName,
		"args_json": json.RawMessage(argsJSON),
	})
	resp, err := r.HookClient.Execute(ctx, hook.Request{
		Event:       hook.EventPreToolUse,
		TraceID:     traceID,
		PayloadJSON: payload,
	})
	if err != nil {
		if r.HookFailClosed {
			hookErr = err
			return nil, false, "", err
		}
		return argsJSON, false, "", nil
	}
	if !resp.Allowed {
		obs.HookTotal.WithLabelValues(obs.ServiceName(), string(hook.EventPreToolUse), "denied").Inc()
		return argsJSON, true, resp.Reason, nil
	}
	if len(resp.PayloadJSON) == 0 {
		return argsJSON, false, "", nil
	}
	var patched struct {
		ArgsJSON json.RawMessage `json:"args_json"`
	}
	if err := json.Unmarshal(resp.PayloadJSON, &patched); err == nil && len(patched.ArgsJSON) > 0 {
		return patched.ArgsJSON, false, "", nil
	}
	return argsJSON, false, "", nil
}

func (r *Runner) executePostToolHook(ctx context.Context, callID, traceID, toolName string, res Result) (Result, bool, error) {
	if r.HookClient == nil {
		return res, false, nil
	}
	start := time.Now()
	ctx, span := obs.StartSpan(ctx, "hook.post_tool", obs.Attr("hook.event", string(hook.EventPostToolUse)), obs.Attr("tool.name", toolName))
	var hookErr error
	defer func() {
		status := "ok"
		if hookErr != nil {
			status = "error"
		}
		obs.HookTotal.WithLabelValues(obs.ServiceName(), string(hook.EventPostToolUse), status).Inc()
		obs.HookDuration.WithLabelValues(obs.ServiceName(), string(hook.EventPostToolUse), status).Observe(time.Since(start).Seconds())
		obs.EndSpan(span, hookErr)
	}()
	payload, _ := json.Marshal(map[string]any{
		"call_id":  callID,
		"tool":     toolName,
		"content":  res.Content,
		"is_error": res.IsError,
		"metadata": res.Metadata,
	})
	resp, err := r.HookClient.Execute(ctx, hook.Request{
		Event:       hook.EventPostToolUse,
		TraceID:     traceID,
		PayloadJSON: payload,
	})
	if err != nil {
		if r.HookFailClosed {
			hookErr = err
			return res, false, err
		}
		return res, false, nil
	}
	if len(resp.PayloadJSON) == 0 {
		return res, false, nil
	}
	var patched struct {
		Content  string         `json:"content"`
		IsError  bool           `json:"is_error"`
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal(resp.PayloadJSON, &patched); err != nil {
		return res, false, nil
	}
	res.Content = patched.Content
	res.IsError = patched.IsError
	if patched.Metadata != nil {
		res.Metadata = patched.Metadata
	}
	return res, true, nil
}

func (r *Runner) publishError(ctx context.Context, t queue.ToolTask, code, msg string, start time.Time) error {
	if r.Log != nil {
		r.Log.Warn("tool error", "call_id", t.CallID, "tool", t.Tool, "code", code, "msg", msg)
	}
	ev := queue.ToolResultEvent{
		CallID:    t.CallID,
		TraceID:   t.TraceID,
		IsError:   true,
		Content:   msg,
		ErrorCode: code,
		ErrorMsg:  msg,
		ElapsedMS: time.Since(start).Milliseconds(),
	}
	if err := r.Bus.Publish(ctx, t.CallID, ev); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	// 即使发布失败也返回 nil，让 stream ack；不要让一条任务永远卡 PEL
	return nil
}
