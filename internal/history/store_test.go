package history

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestStore(t *testing.T) (*RedisStore, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	cli := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = cli.Close() })
	return NewRedis(cli), mr
}

func TestAppendRender(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	runID := "run1"

	if _, err := s.Append(ctx, runID, Message{Role: RoleUser, Content: "hi"}); err != nil {
		t.Fatalf("append1: %v", err)
	}
	if _, err := s.Append(ctx, runID, Message{Role: RoleAssistant, Content: "hello"}); err != nil {
		t.Fatalf("append2: %v", err)
	}

	msgs, err := s.Render(ctx, runID)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("want 2 msgs, got %d", len(msgs))
	}
	if msgs[0].Role != RoleUser || msgs[1].Role != RoleAssistant {
		t.Fatalf("order broken: %+v", msgs)
	}
}

func TestPatchHide(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	runID := "run2"

	id, err := s.Append(ctx, runID, Message{Role: RoleAssistant, Content: "v1"})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := s.Patch(ctx, runID, id, "v2"); err != nil {
		t.Fatalf("patch: %v", err)
	}
	msgs, _ := s.Render(ctx, runID)
	if len(msgs) != 1 || msgs[0].Content != "v2" || msgs[0].Version < 2 {
		t.Fatalf("patch failed: %+v", msgs)
	}

	if err := s.Hide(ctx, runID, id); err != nil {
		t.Fatalf("hide: %v", err)
	}
	msgs, _ = s.Render(ctx, runID)
	if len(msgs) != 0 {
		t.Fatalf("expect hidden, got %d", len(msgs))
	}
}

func TestPatchNotFound(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.Patch(context.Background(), "rx", "missing", "x"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestFold(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	runID := "run-fold"

	first, err := s.Append(ctx, runID, Message{Role: RoleUser, Content: "one"})
	if err != nil {
		t.Fatalf("append first: %v", err)
	}
	second, err := s.Append(ctx, runID, Message{Role: RoleAssistant, Content: "two"})
	if err != nil {
		t.Fatalf("append second: %v", err)
	}
	if _, err := s.Append(ctx, runID, Message{Role: RoleUser, Content: "three"}); err != nil {
		t.Fatalf("append third: %v", err)
	}

	foldID, err := s.Fold(ctx, runID, first, second, "summary")
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	msgs, err := s.Render(ctx, runID)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("want 2 visible messages, got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Content != "three" || msgs[1].Content != "summary" {
		t.Fatalf("unexpected render after fold: %+v", msgs)
	}
	folded, err := s.load(ctx, runID, foldID)
	if err != nil {
		t.Fatalf("load folded: %v", err)
	}
	if folded.Tags["compacted"] != "true" || folded.Tags["fold_from"] != first || folded.Tags["fold_to"] != second {
		t.Fatalf("bad fold tags: %+v", folded.Tags)
	}
}

func TestFoldNotFound(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	runID := "run-fold-missing"
	first, err := s.Append(ctx, runID, Message{Role: RoleUser, Content: "one"})
	if err != nil {
		t.Fatalf("append first: %v", err)
	}
	if _, err := s.Fold(ctx, runID, first, "missing", "summary"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
