package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/wzhnbsixsixsix/agentforge/internal/sandbox"
)

// BashTool 在 sandbox 内通过 sh -c 执行任意 shell 命令。
type BashTool struct{}

const bashSchema = `{
  "type": "object",
  "properties": {
    "command": {"type":"string","description":"shell command, run via 'sh -c'"},
    "timeout_ms": {"type":"integer","minimum":1,"maximum":60000,"description":"max execution time, default 15000"}
  },
  "required": ["command"]
}`

func (t *BashTool) Descriptor() Descriptor {
	return Descriptor{
		Name:        "bash",
		Description: "Execute a shell command inside the run sandbox (alpine + sh). Sandbox has NO network access. Stdout/stderr are truncated at 64KiB.",
		Schema:      json.RawMessage(bashSchema),
	}
}

func (t *BashTool) Invoke(ctx context.Context, sb sandbox.Sandbox, args json.RawMessage) (Result, error) {
	var a struct {
		Command   string `json:"command"`
		TimeoutMS int    `json:"timeout_ms"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return Result{}, fmt.Errorf("bad args: %w", err)
	}
	if a.Command == "" {
		return Result{}, errors.New("command is required")
	}
	timeout := time.Duration(a.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	res, err := sb.Exec(ctx, sandbox.ExecRequest{
		Cmd:     []string{"sh", "-c", a.Command},
		Timeout: timeout,
	})
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	out := string(res.Stdout)
	if len(res.Stderr) > 0 {
		if out != "" {
			out += "\n--- stderr ---\n"
		}
		out += string(res.Stderr)
	}
	return Result{
		Content: out,
		IsError: res.ExitCode != 0,
		Metadata: map[string]any{
			"exit_code":  res.ExitCode,
			"elapsed_ms": res.Elapsed.Milliseconds(),
			"truncated":  res.Truncated,
		},
	}, nil
}
