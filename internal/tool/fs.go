package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/wzhnbsixsixsix/agentforge/internal/sandbox"
)

// safePath 把用户传的相对/绝对路径安全地映射到 sandbox 容器内 workspace 之下。
//
// 规则：
//   - 绝对路径必须以 sandbox.WorkspaceContainer() 为前缀，否则报错
//   - 相对路径以 WorkspaceContainer 为根做 path.Clean，禁止 ../ 跳出
func safePath(sb sandbox.Sandbox, p string) (string, error) {
	root := sb.WorkspaceContainer()
	if p == "" || p == "." {
		return root, nil
	}
	if strings.HasPrefix(p, "/") {
		cleaned := path.Clean(p)
		if cleaned != root && !strings.HasPrefix(cleaned, root+"/") {
			return "", fmt.Errorf("path %q is outside workspace %q", p, root)
		}
		return cleaned, nil
	}
	cleaned := path.Clean(path.Join(root, p))
	if cleaned != root && !strings.HasPrefix(cleaned, root+"/") {
		return "", fmt.Errorf("path %q escapes workspace", p)
	}
	return cleaned, nil
}

// ---------- fs_read ----------

type FsReadTool struct{}

const fsReadSchema = `{
  "type":"object",
  "properties":{
    "path":{"type":"string","description":"path relative to /workspace, or absolute under /workspace"},
    "max_bytes":{"type":"integer","minimum":1,"maximum":1048576,"description":"default 65536"}
  },
  "required":["path"]
}`

func (t *FsReadTool) Descriptor() Descriptor {
	return Descriptor{
		Name:        "fs_read",
		Description: "Read a file from the run workspace. Path must stay inside /workspace. Capped at max_bytes (default 64KiB).",
		Schema:      json.RawMessage(fsReadSchema),
	}
}

func (t *FsReadTool) Invoke(ctx context.Context, sb sandbox.Sandbox, args json.RawMessage) (Result, error) {
	var a struct {
		Path     string `json:"path"`
		MaxBytes int64  `json:"max_bytes"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return Result{}, fmt.Errorf("bad args: %w", err)
	}
	if a.Path == "" {
		return Result{}, errors.New("path required")
	}
	p, err := safePath(sb, a.Path)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	n := a.MaxBytes
	if n <= 0 {
		n = 64 * 1024
	}
	res, err := sb.Exec(ctx, sandbox.ExecRequest{
		Cmd:    []string{"sh", "-c", fmt.Sprintf("head -c %d %q", n, p)},
		MaxOut: n + 4096,
	})
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	if res.ExitCode != 0 {
		return Result{
			Content: string(res.Stderr),
			IsError: true,
			Metadata: map[string]any{
				"exit_code": res.ExitCode,
				"path":      p,
			},
		}, nil
	}
	return Result{
		Content: string(res.Stdout),
		Metadata: map[string]any{
			"path":      p,
			"bytes":     len(res.Stdout),
			"truncated": res.Truncated,
		},
	}, nil
}

// ---------- fs_write ----------

type FsWriteTool struct{}

const fsWriteSchema = `{
  "type":"object",
  "properties":{
    "path":{"type":"string"},
    "content":{"type":"string"},
    "append":{"type":"boolean","default":false}
  },
  "required":["path","content"]
}`

func (t *FsWriteTool) Descriptor() Descriptor {
	return Descriptor{
		Name:        "fs_write",
		Description: "Write a text file inside /workspace. Default mode is overwrite. Parent dirs are created automatically.",
		Schema:      json.RawMessage(fsWriteSchema),
	}
}

func (t *FsWriteTool) Invoke(ctx context.Context, sb sandbox.Sandbox, args json.RawMessage) (Result, error) {
	var a struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Append  bool   `json:"append"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return Result{}, fmt.Errorf("bad args: %w", err)
	}
	if a.Path == "" {
		return Result{}, errors.New("path required")
	}
	p, err := safePath(sb, a.Path)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	redirect := ">"
	if a.Append {
		redirect = ">>"
	}
	// 用 stdin 注入内容，避免 shell quoting 噩梦
	cmd := fmt.Sprintf("mkdir -p %q && cat %s %q", path.Dir(p), redirect, p)
	res, err := sb.Exec(ctx, sandbox.ExecRequest{
		Cmd:   []string{"sh", "-c", cmd},
		Stdin: []byte(a.Content),
	})
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	if res.ExitCode != 0 {
		return Result{
			Content: string(res.Stderr),
			IsError: true,
			Metadata: map[string]any{
				"exit_code": res.ExitCode,
				"path":      p,
			},
		}, nil
	}
	return Result{
		Content: fmt.Sprintf("wrote %d bytes to %s", len(a.Content), p),
		Metadata: map[string]any{
			"path":     p,
			"bytes":    len(a.Content),
			"appended": a.Append,
		},
	}, nil
}

// ---------- fs_list ----------

type FsListTool struct{}

const fsListSchema = `{
  "type":"object",
  "properties":{"path":{"type":"string","default":"."}}
}`

func (t *FsListTool) Descriptor() Descriptor {
	return Descriptor{
		Name:        "fs_list",
		Description: "List entries in a workspace directory (ls -la equivalent). Default path is workspace root.",
		Schema:      json.RawMessage(fsListSchema),
	}
}

func (t *FsListTool) Invoke(ctx context.Context, sb sandbox.Sandbox, args json.RawMessage) (Result, error) {
	var a struct {
		Path string `json:"path"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &a)
	}
	if a.Path == "" {
		a.Path = "."
	}
	p, err := safePath(sb, a.Path)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	res, err := sb.Exec(ctx, sandbox.ExecRequest{
		Cmd: []string{"ls", "-la", p},
	})
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	if res.ExitCode != 0 {
		return Result{
			Content:  string(res.Stderr),
			IsError:  true,
			Metadata: map[string]any{"exit_code": res.ExitCode, "path": p},
		}, nil
	}
	return Result{
		Content:  string(res.Stdout),
		Metadata: map[string]any{"path": p},
	}, nil
}
