package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOpenAIStreamSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		write := func(s string) {
			fmt.Fprint(w, s)
			if flusher != nil {
				flusher.Flush()
			}
		}
		write("data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n\n")
		write("data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n")
		write("data: {\"choices\":[{\"delta\":{\"content\":\"!\"},\"finish_reason\":\"stop\"}]}\n\n")
		write("data: [DONE]\n\n")
	}))
	defer srv.Close()

	p := NewOpenAI(srv.URL, "test-key", "gpt-test", 5*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ch, err := p.Stream(ctx, Req{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	var got strings.Builder
	doneSeen := false
	for ev := range ch {
		if ev.Err != nil {
			t.Fatalf("event err: %v", ev.Err)
		}
		if ev.Done {
			doneSeen = true
			break
		}
		got.WriteString(ev.Token)
	}
	if !doneSeen {
		t.Fatalf("done not seen")
	}
	if got.String() != "Hello!" {
		t.Fatalf("want Hello!, got %q", got.String())
	}
}

func TestOpenAIStreamHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	p := NewOpenAI(srv.URL, "bad", "m", time.Second)
	_, err := p.Stream(context.Background(), Req{Messages: []Message{{Role: RoleUser, Content: "x"}}})
	if err == nil {
		t.Fatalf("want error")
	}
}

func TestOpenAIStreamToolCallFragments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		write := func(s string) {
			fmt.Fprint(w, s)
			if flusher != nil {
				flusher.Flush()
			}
		}
		write("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"fs_write\",\"arguments\":\"{\\\"path\\\":\\\"he\"}}]}}]}\n\n")
		write("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"llo.txt\\\",\\\"content\\\":\\\"hi\\\"}\"}}]}}]}\n\n")
		write("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
		write("data: [DONE]\n\n")
	}))
	defer srv.Close()

	p := NewOpenAI(srv.URL, "test-key", "gpt-test", 5*time.Second)
	ch, err := p.Stream(context.Background(), Req{Messages: []Message{{Role: RoleUser, Content: "write"}}})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	var calls []ToolCall
	for ev := range ch {
		if ev.Err != nil {
			t.Fatalf("event err: %v", ev.Err)
		}
		if len(ev.ToolCalls) > 0 {
			calls = append(calls, ev.ToolCalls...)
		}
	}
	if len(calls) != 1 {
		t.Fatalf("want 1 tool call, got %d: %+v", len(calls), calls)
	}
	if calls[0].ID != "call_1" || calls[0].Name != "fs_write" {
		t.Fatalf("bad tool call identity: %+v", calls[0])
	}
	if calls[0].Arguments != `{"path":"hello.txt","content":"hi"}` {
		t.Fatalf("bad arguments: %q", calls[0].Arguments)
	}
}

func TestOpenAIRequestToolsOnlyWhenConfigured(t *testing.T) {
	var (
		mu         sync.Mutex
		requests   []map[string]any
		requestErr error
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			mu.Lock()
			requestErr = err
			mu.Unlock()
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, payload)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	p := NewOpenAI(srv.URL, "test-key", "gpt-test", 5*time.Second)
	for _, req := range []Req{
		{Messages: []Message{{Role: RoleUser, Content: "no tools"}}},
		{
			Messages: []Message{{Role: RoleUser, Content: "with tools"}},
			Tools: []ToolDefinition{{
				Name:        "fs_read",
				Description: "read a file",
				Parameters:  json.RawMessage(`{"type":"object"}`),
			}},
		},
	} {
		ch, err := p.Stream(context.Background(), req)
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		for range ch {
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if requestErr != nil {
		t.Fatalf("bad request json: %v", requestErr)
	}
	if len(requests) != 2 {
		t.Fatalf("want 2 requests, got %d", len(requests))
	}
	if _, ok := requests[0]["tools"]; ok {
		t.Fatalf("tools should be omitted when request has no tools: %+v", requests[0])
	}
	tools, ok := requests[1]["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools should be present: %+v", requests[1]["tools"])
	}
}
