// Package hook implements W8 hook execution contracts and a deterministic
// built-in engine used by hookd.
package hook

import (
	"context"
	"encoding/json"
	"time"
)

type Event string

const (
	EventPreLLM      Event = "PreLLM"
	EventPostLLM     Event = "PostLLM"
	EventPreToolUse  Event = "PreToolUse"
	EventPostToolUse Event = "PostToolUse"
)

type Request struct {
	Event       Event           `json:"event"`
	RunID       string          `json:"run_id,omitempty"`
	TraceID     string          `json:"trace_id,omitempty"`
	PayloadJSON json.RawMessage `json:"payload_json,omitempty"`
}

type Response struct {
	Allowed              bool            `json:"allowed"`
	Reason               string          `json:"reason,omitempty"`
	PayloadJSON          json.RawMessage `json:"payload_json,omitempty"`
	AppendSystemMessages []string        `json:"append_system_messages,omitempty"`
	MatchedHooks         []string        `json:"matched_hooks,omitempty"`
}

type Info struct {
	ID          string `json:"id"`
	Event       Event  `json:"event"`
	Enabled     bool   `json:"enabled"`
	Source      string `json:"source"`
	Description string `json:"description,omitempty"`
}

type Client interface {
	Execute(ctx context.Context, req Request) (Response, error)
	List(ctx context.Context) ([]Info, error)
}

type Config struct {
	Root           string
	Timeout        time.Duration
	FailClosed     bool
	MaxStdoutBytes int
}
