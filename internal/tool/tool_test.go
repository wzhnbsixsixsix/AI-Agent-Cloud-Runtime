package tool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wzhnbsixsixsix/agentforge/internal/sandbox"
)

// fakeSandbox 满足 sandbox.Sandbox 接口，把 Exec 委托给一个回调。
// 用于 tool 单测，无需真起 docker 或 os/exec。
type fakeSandbox struct {
	id        string
	runID     string
	wsHost    string
	wsCont    string
	onExec    func(req sandbox.ExecRequest) (sandbox.ExecResult, error)
	execCalls atomic.Int64
}

func (s *fakeSandbox) ID() string                 { return s.id }
func (s *fakeSandbox) RunID() string              { return s.runID }
func (s *fakeSandbox) WorkspaceHost() string      { return s.wsHost }
func (s *fakeSandbox) WorkspaceContainer() string { return s.wsCont }
func (s *fakeSandbox) Exec(_ context.Context, req sandbox.ExecRequest) (sandbox.ExecResult, error) {
	s.execCalls.Add(1)
	return s.onExec(req)
}

func newFakeSandbox(onExec func(sandbox.ExecRequest) (sandbox.ExecResult, error)) *fakeSandbox {
	return &fakeSandbox{
		id: "fake-1", runID: "run-1",
		wsHost: "/tmp/agentforge/run-1", wsCont: "/workspace/runs/run-1",
		onExec: onExec,
	}
}

// ---------- bash ----------

func TestBashTool_Happy(t *testing.T) {
	sb := newFakeSandbox(func(r sandbox.ExecRequest) (sandbox.ExecResult, error) {
		if r.Cmd[0] != "sh" || r.Cmd[1] != "-c" || r.Cmd[2] != "echo hi" {
			t.Errorf("unexpected cmd: %v", r.Cmd)
		}
		return sandbox.ExecResult{ExitCode: 0, Stdout: []byte("hi\n"), Elapsed: 5 * time.Millisecond}, nil
	})
	res, err := (&BashTool{}).Invoke(context.Background(), sb, json.RawMessage(`{"command":"echo hi"}`))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if res.IsError || !strings.Contains(res.Content, "hi") {
		t.Fatalf("unexpected: %+v", res)
	}
}

func TestBashTool_NonZeroExit(t *testing.T) {
	sb := newFakeSandbox(func(r sandbox.ExecRequest) (sandbox.ExecResult, error) {
		return sandbox.ExecResult{ExitCode: 7, Stdout: []byte("out"), Stderr: []byte("err")}, nil
	})
	res, _ := (&BashTool{}).Invoke(context.Background(), sb, json.RawMessage(`{"command":"false"}`))
	if !res.IsError {
		t.Fatal("want IsError=true")
	}
	if !strings.Contains(res.Content, "out") || !strings.Contains(res.Content, "err") {
		t.Fatalf("content missing stdout/stderr: %q", res.Content)
	}
	if got := res.Metadata["exit_code"]; got != 7 {
		t.Fatalf("exit_code meta=%v", got)
	}
}

func TestBashTool_BadArgs(t *testing.T) {
	_, err := (&BashTool{}).Invoke(context.Background(), newFakeSandbox(nil), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("want err for missing command")
	}
}

// ---------- safePath ----------

func TestSafePath(t *testing.T) {
	sb := newFakeSandbox(nil)
	cases := []struct {
		in   string
		want string
		bad  bool
	}{
		{"a/b.txt", "/workspace/runs/run-1/a/b.txt", false},
		{"./x", "/workspace/runs/run-1/x", false},
		{".", "/workspace/runs/run-1", false},
		{"/workspace/runs/run-1/y", "/workspace/runs/run-1/y", false},
		{"/etc/passwd", "", true},
		{"../../../etc/passwd", "", true},
	}
	for _, c := range cases {
		got, err := safePath(sb, c.in)
		if c.bad {
			if err == nil {
				t.Errorf("safePath(%q) want err, got %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("safePath(%q) err: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("safePath(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

// ---------- fs_write ----------

func TestFsWriteTool_HappyOverwrite(t *testing.T) {
	var seenStdin []byte
	var seenCmd string
	sb := newFakeSandbox(func(r sandbox.ExecRequest) (sandbox.ExecResult, error) {
		seenStdin = r.Stdin
		seenCmd = strings.Join(r.Cmd, " ")
		return sandbox.ExecResult{ExitCode: 0}, nil
	})
	args := json.RawMessage(`{"path":"out/x.txt","content":"hello\n"}`)
	res, err := (&FsWriteTool{}).Invoke(context.Background(), sb, args)
	if err != nil || res.IsError {
		t.Fatalf("invoke: err=%v res=%+v", err, res)
	}
	if string(seenStdin) != "hello\n" {
		t.Fatalf("stdin=%q", seenStdin)
	}
	if !strings.Contains(seenCmd, "/workspace/runs/run-1/out/x.txt") || strings.Contains(seenCmd, ">>") {
		t.Fatalf("unexpected cmd: %s", seenCmd)
	}
}

func TestFsWriteTool_Append(t *testing.T) {
	var seenCmd string
	sb := newFakeSandbox(func(r sandbox.ExecRequest) (sandbox.ExecResult, error) {
		seenCmd = strings.Join(r.Cmd, " ")
		return sandbox.ExecResult{ExitCode: 0}, nil
	})
	_, _ = (&FsWriteTool{}).Invoke(context.Background(), sb,
		json.RawMessage(`{"path":"a.txt","content":"x","append":true}`))
	if !strings.Contains(seenCmd, ">>") {
		t.Fatalf("expected append redirect, cmd=%s", seenCmd)
	}
}

// ---------- fs_read ----------

func TestFsReadTool_Truncated(t *testing.T) {
	sb := newFakeSandbox(func(r sandbox.ExecRequest) (sandbox.ExecResult, error) {
		return sandbox.ExecResult{ExitCode: 0, Stdout: []byte("partial"), Truncated: true}, nil
	})
	res, _ := (&FsReadTool{}).Invoke(context.Background(), sb, json.RawMessage(`{"path":"big.bin"}`))
	if res.IsError {
		t.Fatalf("res=%+v", res)
	}
	if got := res.Metadata["truncated"]; got != true {
		t.Fatalf("truncated meta=%v", got)
	}
}

// ---------- http_fetch ----------

func TestHTTPFetchTool_Allowlist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello-world"))
	}))
	defer srv.Close()

	tool := &HTTPFetchTool{AllowList: []string{"example.com"}, MaxBytes: 1024}
	res, err := tool.Invoke(context.Background(), nil, json.RawMessage(`{"url":"`+srv.URL+`"}`))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "not in allowlist") {
		t.Fatalf("expected blocked, got %+v", res)
	}

	tool.AllowList = nil // 全允许
	res, err = tool.Invoke(context.Background(), nil, json.RawMessage(`{"url":"`+srv.URL+`"}`))
	if err != nil || res.IsError {
		t.Fatalf("invoke2: err=%v res=%+v", err, res)
	}
	if !strings.Contains(res.Content, "hello-world") {
		t.Fatalf("body=%q", res.Content)
	}
	if got := res.Metadata["status"]; got != 200 {
		t.Fatalf("status=%v", got)
	}
}

func TestHTTPFetchTool_Truncate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 5000)))
	}))
	defer srv.Close()
	tool := &HTTPFetchTool{MaxBytes: 100}
	res, _ := tool.Invoke(context.Background(), nil, json.RawMessage(`{"url":"`+srv.URL+`"}`))
	if !res.Metadata["truncated"].(bool) {
		t.Fatalf("expected truncated, meta=%v", res.Metadata)
	}
	if len(res.Content) != 100 {
		t.Fatalf("body=%d", len(res.Content))
	}
}

// ---------- registry ----------

func TestBuiltinsRegistry(t *testing.T) {
	r := Builtins(BuiltinsConfig{})
	want := []string{"bash", "fs_list", "fs_read", "fs_write", "http_fetch"}
	got := r.List()
	if len(got) != len(want) {
		t.Fatalf("count=%d want=%d", len(got), len(want))
	}
	for i, d := range got {
		if d.Name != want[i] {
			t.Fatalf("[%d] %s != %s", i, d.Name, want[i])
		}
		// Schema 必须是合法 JSON
		var any map[string]any
		if err := json.Unmarshal(d.Schema, &any); err != nil {
			t.Fatalf("%s: schema not valid json: %v", d.Name, err)
		}
	}
}
