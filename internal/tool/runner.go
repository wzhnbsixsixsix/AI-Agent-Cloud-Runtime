package tool

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

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
	HardTimeout time.Duration
}

// Handle 是 queue.ToolHandler 兼容的回调。返回 nil 即 ack。
func (r *Runner) Handle(ctx context.Context, d queue.ToolDelivery) error {
	t := d.Task
	log := r.Log.With("call_id", t.CallID, "tool", t.Tool, "trace", t.TraceID)
	start := time.Now()

	// 取 tool 描述
	tool, ok := r.Registry.Get(t.Tool)
	if !ok {
		return r.publishError(ctx, t, "tool_not_found", "unknown tool: "+t.Tool, start)
	}

	// 计算执行超时
	hard := r.HardTimeout
	if hard <= 0 {
		hard = 60 * time.Second
	}
	timeout := hard
	if t.TimeoutMS > 0 {
		if want := time.Duration(t.TimeoutMS) * time.Millisecond; want < timeout {
			timeout = want
		}
	}
	tCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Acquire sandbox（即使是 http_fetch 也走一遍，保持调用接口一致；
	// http_fetch 不会用到 sandbox.Exec 但拿到 workspace 仍有意义）。
	sb, err := r.Driver.Acquire(tCtx, t.CallID)
	if err != nil {
		return r.publishError(ctx, t, "acquire", err.Error(), start)
	}
	defer func() {
		// Release 用 background ctx；上层 ctx 取消不影响销毁
		_ = r.Driver.Release(context.Background(), sb)
	}()

	args := []byte(t.ArgsJSON)
	if len(args) == 0 {
		args = []byte("{}")
	}
	res, err := tool.Invoke(tCtx, sb, args)
	if err != nil {
		log.Warn("tool invoke internal error", "err", err)
		return r.publishError(ctx, t, "invoke", err.Error(), start)
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

	ev := queue.ToolResultEvent{
		CallID:      t.CallID,
		TraceID:     t.TraceID,
		ContainerID: sb.ID(),
		ExitCode:    exit,
		Content:     res.Content,
		IsError:     res.IsError,
		MetaJSON:    metaJSON,
		ElapsedMS:   time.Since(start).Milliseconds(),
	}
	if err := r.Bus.Publish(ctx, t.CallID, ev); err != nil {
		log.Warn("publish tool result", "err", err)
		return err
	}
	log.Info("tool done",
		"is_error", res.IsError, "elapsed_ms", ev.ElapsedMS, "container", sb.ID())
	return nil
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
