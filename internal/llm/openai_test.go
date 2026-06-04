package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
