package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wzhnbsixsixsix/agentforge/internal/history"
)

type fakeChildRunner struct {
	reqs []ChildRunRequest
	err  error
}

func (r *fakeChildRunner) RunChild(_ context.Context, req ChildRunRequest) (SubagentResult, error) {
	r.reqs = append(r.reqs, req)
	if r.err != nil {
		return SubagentResult{}, r.err
	}
	return SubagentResult{
		ChildRunID: req.ChildRunID,
		Role:       req.Role,
		Summary:    "done: " + req.Task,
		Status:     "ok",
	}, nil
}

func TestSupervisorDispatchLimits(t *testing.T) {
	child := &fakeChildRunner{}
	sup := &Supervisor{Runner: child, MaxDepth: 2, MaxChildren: 1, Timeout: time.Second}
	res, err := sup.Dispatch(context.Background(), "parent", "trace", "user", "model", 0, SubagentRequest{
		Role: "reviewer",
		Task: "check README",
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if res.ChildRunID == "" || res.Role != "reviewer" || res.Status != "ok" {
		t.Fatalf("bad result: %+v", res)
	}
	if _, err := sup.Dispatch(context.Background(), "parent", "trace", "user", "model", 0, SubagentRequest{Task: "again"}); !errors.Is(err, ErrMaxChildren) {
		t.Fatalf("want max children, got %v", err)
	}
	if _, err := sup.Dispatch(context.Background(), "other", "trace", "user", "model", 2, SubagentRequest{Task: "too deep"}); !errors.Is(err, ErrMaxDepth) {
		t.Fatalf("want max depth, got %v", err)
	}
}

func TestParsePipelineAndOrder(t *testing.T) {
	p, err := ParsePipeline(`
name: readme-review
steps:
  - id: summarize
    role: summarizer
    task: summarize README
  - id: critique
    role: reviewer
    task: critique summary
    depends_on: [summarize]
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ordered, err := TopologicalOrder(p)
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	if len(ordered) != 2 || ordered[0].ID != "summarize" || ordered[1].ID != "critique" {
		t.Fatalf("bad order: %+v", ordered)
	}
}

func TestPipelineValidation(t *testing.T) {
	if _, err := ParsePipeline("steps:\n  - id: a\n    task: one\n  - id: a\n    task: two\n"); !errors.Is(err, ErrDuplicateStep) {
		t.Fatalf("want duplicate, got %v", err)
	}
	if _, err := ParsePipeline("steps:\n  - id: a\n    task: one\n    depends_on: [missing]\n"); !errors.Is(err, ErrMissingStep) {
		t.Fatalf("want missing dep, got %v", err)
	}
	if _, err := ParsePipeline("steps:\n  - id: a\n    task: one\n    depends_on: [b]\n  - id: b\n    task: two\n    depends_on: [a]\n"); !errors.Is(err, ErrPipelineCycle) {
		t.Fatalf("want cycle, got %v", err)
	}
}

func TestRunPipelineInjectsDependencies(t *testing.T) {
	p, err := ParsePipeline(`
name: demo
steps:
  - id: a
    role: first
    task: produce first
  - id: b
    role: second
    task: consume output
    depends_on: [a]
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	child := &fakeChildRunner{}
	sup := &Supervisor{Runner: child, MaxChildren: 4}
	res, err := RunPipeline(context.Background(), sup, p, "parent", "trace", "user", "model")
	if err != nil {
		t.Fatalf("run pipeline: %v", err)
	}
	if res.Status != "ok" || len(res.Results) != 2 {
		t.Fatalf("bad result: %+v", res)
	}
	if len(child.reqs) != 2 || !strings.Contains(child.reqs[1].Task, "Dependency outputs") {
		t.Fatalf("dependency output not injected: %+v", child.reqs)
	}
}

type memoryHistory struct {
	msgs []history.Message
}

func (m *memoryHistory) Append(context.Context, string, history.Message) (string, error) {
	return "", nil
}
func (m *memoryHistory) Patch(context.Context, string, string, string) error { return nil }
func (m *memoryHistory) Hide(context.Context, string, string) error          { return nil }
func (m *memoryHistory) Render(context.Context, string) ([]history.Message, error) {
	return m.msgs, nil
}
func (m *memoryHistory) Fold(_ context.Context, _ string, fromID, toID, summary string) (string, error) {
	for i := range m.msgs {
		if m.msgs[i].ID >= fromID && m.msgs[i].ID <= toID {
			m.msgs[i].Visible = false
		}
	}
	m.msgs = append(m.msgs, history.Message{
		ID:      "folded",
		Role:    history.RoleAssistant,
		Content: summary,
		Visible: true,
		Tags:    map[string]string{"compacted": "true"},
	})
	return "folded", nil
}

func TestCompactPolicy(t *testing.T) {
	var msgs []history.Message
	for i := 0; i < 8; i++ {
		msgs = append(msgs, history.Message{
			ID:      string(rune('a' + i)),
			Role:    history.RoleUser,
			Content: strings.Repeat("x", 20),
			Visible: true,
		})
	}
	store := &memoryHistory{msgs: msgs}
	ok, err := (CompactPolicy{Enabled: true, MaxChars: 60, KeepHead: 1, KeepTail: 2}).CompactIfNeeded(context.Background(), store, "run", msgs)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if !ok || store.msgs[len(store.msgs)-1].Tags["compacted"] != "true" {
		t.Fatalf("compaction not applied: %+v", store.msgs)
	}
}
