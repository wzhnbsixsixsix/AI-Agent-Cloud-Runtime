package llm

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMockStream(t *testing.T) {
	p := NewMock([]string{"a", "b", "c"}, time.Millisecond)
	ch, err := p.Stream(context.Background(), Req{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	var b strings.Builder
	done := false
	for ev := range ch {
		if ev.Err != nil {
			t.Fatalf("err: %v", ev.Err)
		}
		if ev.Done {
			done = true
			break
		}
		b.WriteString(ev.Token)
	}
	if !done {
		t.Fatalf("not done")
	}
	if !strings.HasSuffix(b.String(), "abc") {
		t.Fatalf("want suffix abc, got %q", b.String())
	}
}
