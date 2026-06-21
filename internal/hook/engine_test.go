package hook

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnginePreLLMAppendSystem(t *testing.T) {
	eng, err := Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	resp, err := eng.Execute(context.Background(), Request{Event: EventPreLLM})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !resp.Allowed || len(resp.AppendSystemMessages) == 0 {
		t.Fatalf("missing system message: %+v", resp)
	}
}

func TestEngineDenyDangerousBash(t *testing.T) {
	eng, _ := Load("")
	args, _ := json.Marshal(map[string]any{"command": "rm -rf /"})
	payload, _ := json.Marshal(map[string]any{"tool": "bash", "args_json": json.RawMessage(args)})
	resp, err := eng.Execute(context.Background(), Request{Event: EventPreToolUse, PayloadJSON: payload})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resp.Allowed || !strings.Contains(resp.Reason, "denied") {
		t.Fatalf("want denied, got %+v", resp)
	}
}

func TestEngineRedactPostTool(t *testing.T) {
	eng, _ := Load("")
	payload, _ := json.Marshal(map[string]any{"content": "token sk-demo-secret"})
	resp, err := eng.Execute(context.Background(), Request{Event: EventPostToolUse, PayloadJSON: payload})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(string(resp.PayloadJSON), "sk-demo-secret") || !strings.Contains(string(resp.PayloadJSON), "[REDACTED]") {
		t.Fatalf("not redacted: %s", resp.PayloadJSON)
	}
}

func TestEngineExecutesWASMHook(t *testing.T) {
	root := filepath.Join("..", "..", "hooks")
	eng, err := LoadWithConfig(Config{Root: root, MaxStdoutBytes: 65536})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	resp, err := eng.Execute(context.Background(), Request{Event: EventPreLLM})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	joined := strings.Join(resp.AppendSystemMessages, "\n")
	if !strings.Contains(joined, "WASM enterprise hook") {
		t.Fatalf("missing wasm system message: %+v", resp)
	}
}
