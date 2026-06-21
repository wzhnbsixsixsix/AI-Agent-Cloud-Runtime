// Package orchestrator implements W7 local multi-agent orchestration.
package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wzhnbsixsixsix/agentforge/internal/history"
	"github.com/wzhnbsixsixsix/agentforge/internal/llm"
	"github.com/wzhnbsixsixsix/agentforge/internal/obs"
)

var (
	ErrMaxDepth      = errors.New("subagent max depth exceeded")
	ErrMaxChildren   = errors.New("subagent max children exceeded")
	ErrMissingStep   = errors.New("pipeline missing step")
	ErrDuplicateStep = errors.New("pipeline duplicate step id")
	ErrPipelineCycle = errors.New("pipeline contains a cycle")
)

// SubagentRequest is the dispatch_subagent input schema.
type SubagentRequest struct {
	Role         string `json:"role"`
	Task         string `json:"task"`
	OutputSchema string `json:"output_schema,omitempty"`
}

// SubagentResult is returned to the parent as a tool result.
type SubagentResult struct {
	ChildRunID string `json:"child_run_id"`
	Role       string `json:"role"`
	Summary    string `json:"summary"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}

// Marshal returns a stable JSON string for tool results.
func (r SubagentResult) Marshal() string {
	b, err := json.Marshal(r)
	if err != nil {
		return `{"status":"error","error":"marshal subagent result"}`
	}
	return string(b)
}

// ChildRunner executes a child task with isolated history.
type ChildRunner interface {
	RunChild(ctx context.Context, req ChildRunRequest) (SubagentResult, error)
}

// ChildRunRequest describes one local child run.
type ChildRunRequest struct {
	ParentRunID string
	ChildRunID  string
	TraceID     string
	UserID      string
	Model       string
	Role        string
	Task        string
	Depth       int
}

// Supervisor controls local child dispatch limits.
type Supervisor struct {
	Runner      ChildRunner
	MaxDepth    int
	MaxChildren int
	Timeout     time.Duration

	Children map[string]int
}

// Dispatch validates limits, creates a child run id, and executes the child locally.
func (s *Supervisor) Dispatch(ctx context.Context, parentRunID, traceID, userID, model string, depth int, req SubagentRequest) (SubagentResult, error) {
	if s == nil || s.Runner == nil {
		return SubagentResult{Status: "error", Error: "supervisor disabled"}, nil
	}
	maxDepth := s.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 2
	}
	if depth >= maxDepth {
		return SubagentResult{Role: req.Role, Status: "error", Error: ErrMaxDepth.Error()}, ErrMaxDepth
	}
	maxChildren := s.MaxChildren
	if maxChildren <= 0 {
		maxChildren = 4
	}
	if s.Children == nil {
		s.Children = map[string]int{}
	}
	if s.Children[parentRunID] >= maxChildren {
		return SubagentResult{Role: req.Role, Status: "error", Error: ErrMaxChildren.Error()}, ErrMaxChildren
	}
	s.Children[parentRunID]++

	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	childCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	childRunID := obs.NewRunID()
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = "subagent"
	}
	task := strings.TrimSpace(req.Task)
	if req.OutputSchema != "" {
		task += "\n\nReturn output matching this JSON schema:\n" + req.OutputSchema
	}
	res, err := s.Runner.RunChild(childCtx, ChildRunRequest{
		ParentRunID: parentRunID,
		ChildRunID:  childRunID,
		TraceID:     traceID,
		UserID:      userID,
		Model:       model,
		Role:        role,
		Task:        task,
		Depth:       depth + 1,
	})
	if err != nil {
		return SubagentResult{ChildRunID: childRunID, Role: role, Status: "error", Error: err.Error()}, err
	}
	if res.ChildRunID == "" {
		res.ChildRunID = childRunID
	}
	if res.Role == "" {
		res.Role = role
	}
	if res.Status == "" {
		res.Status = "ok"
	}
	return res, nil
}

// CompactPolicy decides when to fold older history.
type CompactPolicy struct {
	Enabled  bool
	MaxChars int
	KeepHead int
	KeepTail int
}

// CompactIfNeeded folds the middle of visible history when it exceeds MaxChars.
func (p CompactPolicy) CompactIfNeeded(ctx context.Context, store history.Store, runID string, msgs []history.Message) (bool, error) {
	if !p.Enabled || store == nil {
		return false, nil
	}
	maxChars := p.MaxChars
	if maxChars <= 0 {
		maxChars = 24000
	}
	var total int
	for _, m := range msgs {
		total += len([]rune(m.Content))
	}
	if total <= maxChars {
		return false, nil
	}
	keepHead := p.KeepHead
	if keepHead < 0 {
		keepHead = 0
	}
	keepTail := p.KeepTail
	if keepTail <= 0 {
		keepTail = 8
	}
	if len(msgs) <= keepHead+keepTail+1 {
		return false, nil
	}
	from := msgs[keepHead]
	to := msgs[len(msgs)-keepTail-1]
	summary := SummarizeMessages(msgs[keepHead : len(msgs)-keepTail])
	if _, err := store.Fold(ctx, runID, from.ID, to.ID, summary); err != nil {
		return false, err
	}
	return true, nil
}

// SummarizeMessages creates a deterministic local compaction summary.
func SummarizeMessages(msgs []history.Message) string {
	var b strings.Builder
	b.WriteString("Compacted history summary:\n")
	for _, m := range msgs {
		content := strings.Join(strings.Fields(m.Content), " ")
		if len([]rune(content)) > 180 {
			content = string([]rune(content)[:180]) + "..."
		}
		b.WriteString(fmt.Sprintf("- %s: %s\n", m.Role, content))
	}
	return strings.TrimSpace(b.String())
}

// ExtractAssistantSummary returns a short summary from child LLM messages.
func ExtractAssistantSummary(msgs []llm.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == llm.RoleAssistant && strings.TrimSpace(msgs[i].Content) != "" {
			return strings.TrimSpace(msgs[i].Content)
		}
	}
	return ""
}
