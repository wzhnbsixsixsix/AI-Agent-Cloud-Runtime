package agent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/wzhnbsixsixsix/agentforge/internal/history"
	"github.com/wzhnbsixsixsix/agentforge/internal/llm"
	"github.com/wzhnbsixsixsix/agentforge/internal/queue"
	"github.com/wzhnbsixsixsix/agentforge/internal/sandbox"
	"github.com/wzhnbsixsixsix/agentforge/internal/tool"
)

type scriptedProvider struct {
	mu       sync.Mutex
	streams  [][]llm.TokenEvent
	requests []llm.Req
}

func (p *scriptedProvider) Name() string { return "scripted" }

func (p *scriptedProvider) Stream(ctx context.Context, req llm.Req) (<-chan llm.TokenEvent, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	var events []llm.TokenEvent
	if len(p.streams) > 0 {
		events = p.streams[0]
		p.streams = p.streams[1:]
	} else {
		events = []llm.TokenEvent{{Done: true}}
	}
	p.mu.Unlock()

	out := make(chan llm.TokenEvent, len(events))
	go func() {
		defer close(out)
		for _, ev := range events {
			select {
			case <-ctx.Done():
				return
			case out <- ev:
			}
		}
	}()
	return out, nil
}

func (p *scriptedProvider) reqs() []llm.Req {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]llm.Req(nil), p.requests...)
}

type fakeTool struct{}

func (fakeTool) Descriptor() tool.Descriptor {
	return tool.Descriptor{
		Name:        "fake_tool",
		Description: "fake tool for runner tests",
		Schema:      json.RawMessage(`{"type":"object"}`),
	}
}

func (fakeTool) Invoke(context.Context, sandbox.Sandbox, json.RawMessage) (tool.Result, error) {
	return tool.Result{
		Content:  "tool says hello",
		Metadata: map[string]any{"exit_code": 0},
	}, nil
}

func newRunnerTest(t *testing.T, provider llm.Provider) (*Runner, history.Store, *queue.PubSub) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	cli := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = cli.Close() })
	store := history.NewRedis(cli)
	ps := queue.NewPubSub(cli)
	return NewRunner(store, provider, ps), store, ps
}

func collectRunEvents(t *testing.T, ps *queue.PubSub, runID string, run func(context.Context) error) ([]queue.Event, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	evCh, evCancel, err := ps.Subscribe(ctx, runID)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer evCancel()

	runErr := run(ctx)
	var events []queue.Event
	for {
		select {
		case ev, ok := <-evCh:
			if !ok {
				t.Fatalf("event channel closed before terminal event; collected=%+v runErr=%v", events, runErr)
			}
			events = append(events, ev)
			if ev.Kind == queue.EventDone || ev.Kind == queue.EventError {
				return events, runErr
			}
		case <-ctx.Done():
			t.Fatalf("timed out waiting terminal event; collected=%+v runErr=%v", events, runErr)
		}
	}
}

func TestRunnerTextOnly(t *testing.T) {
	provider := &scriptedProvider{streams: [][]llm.TokenEvent{{
		{Token: "hello"},
		{Token: " world"},
		{Done: true, StopReason: llm.StopReasonStop},
	}}}
	r, store, ps := newRunnerTest(t, provider)

	events, err := collectRunEvents(t, ps, "run-text", func(ctx context.Context) error {
		return r.Run(ctx, queue.Task{RunID: "run-text", Prompt: "hi", TraceID: "trace-text"})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if events[len(events)-1].Kind != queue.EventDone {
		t.Fatalf("want done, got %+v", events)
	}
	msgs, err := store.Render(context.Background(), "run-text")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(msgs) != 2 || msgs[1].Content != "hello world" {
		t.Fatalf("bad history: %+v", msgs)
	}
	if len(provider.reqs()) != 1 || len(provider.reqs()[0].Tools) != 0 {
		t.Fatalf("text-only run should not send tools: %+v", provider.reqs())
	}
}

func TestRunnerToolCallingLoop(t *testing.T) {
	provider := &scriptedProvider{streams: [][]llm.TokenEvent{
		{
			{ToolCalls: []llm.ToolCall{{ID: "call_1", Type: "function", Name: "fake_tool", Arguments: `{}`}}, StopReason: llm.StopReasonToolCalls},
			{Done: true, StopReason: llm.StopReasonToolCalls},
		},
		{
			{Token: "final"},
			{Done: true, StopReason: llm.StopReasonStop},
		},
	}}
	r, store, ps := newRunnerTest(t, provider)
	reg := tool.NewRegistry()
	if err := reg.Register(fakeTool{}); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	driver, err := sandbox.NewMemoryDriver(t.TempDir(), 1)
	if err != nil {
		t.Fatalf("memory driver: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = driver.Close(ctx)
	})
	r.ToolRunner = &tool.Runner{Registry: reg, Driver: driver, HardTimeout: time.Second}
	r.ToolMaxSteps = 2

	events, err := collectRunEvents(t, ps, "run-tool", func(ctx context.Context) error {
		return r.Run(ctx, queue.Task{RunID: "run-tool", Prompt: "use tool", TraceID: "trace-tool"})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	seenWaiting := false
	for _, ev := range events {
		if ev.Kind == queue.EventState && ev.State == string(StateWaitingTool) {
			seenWaiting = true
		}
	}
	if !seenWaiting {
		t.Fatalf("WAITING_TOOL state not observed: %+v", events)
	}
	reqs := provider.reqs()
	if len(reqs) != 2 {
		t.Fatalf("want 2 LLM calls, got %d", len(reqs))
	}
	if len(reqs[0].Tools) != 1 || reqs[0].Tools[0].Name != "fake_tool" {
		t.Fatalf("first request missing tool defs: %+v", reqs[0].Tools)
	}
	lastMsgs := reqs[1].Messages
	if len(lastMsgs) == 0 || lastMsgs[len(lastMsgs)-1].Role != llm.RoleTool || lastMsgs[len(lastMsgs)-1].Content != "tool says hello" {
		t.Fatalf("second request missing tool result: %+v", lastMsgs)
	}
	msgs, err := store.Render(context.Background(), "run-tool")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(msgs) < 4 || msgs[len(msgs)-1].Content != "final" {
		t.Fatalf("bad history after tool loop: %+v", msgs)
	}
}

func TestRunnerToolLoopLimit(t *testing.T) {
	provider := &scriptedProvider{streams: [][]llm.TokenEvent{
		{
			{ToolCalls: []llm.ToolCall{{ID: "call_1", Type: "function", Name: "fake_tool", Arguments: `{}`}}},
			{Done: true, StopReason: llm.StopReasonToolCalls},
		},
		{
			{ToolCalls: []llm.ToolCall{{ID: "call_2", Type: "function", Name: "fake_tool", Arguments: `{}`}}},
			{Done: true, StopReason: llm.StopReasonToolCalls},
		},
	}}
	r, _, ps := newRunnerTest(t, provider)
	reg := tool.NewRegistry()
	if err := reg.Register(fakeTool{}); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	driver, err := sandbox.NewMemoryDriver(t.TempDir(), 1)
	if err != nil {
		t.Fatalf("memory driver: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = driver.Close(ctx)
	})
	r.ToolRunner = &tool.Runner{Registry: reg, Driver: driver, HardTimeout: time.Second}
	r.ToolMaxSteps = 1

	events, err := collectRunEvents(t, ps, "run-limit", func(ctx context.Context) error {
		return r.Run(ctx, queue.Task{RunID: "run-limit", Prompt: "loop", TraceID: "trace-limit"})
	})
	if err == nil {
		t.Fatalf("want loop limit error")
	}
	last := events[len(events)-1]
	if last.Kind != queue.EventError || last.Code != "tool_loop_limit" {
		t.Fatalf("want tool_loop_limit event, got %+v", last)
	}
}
