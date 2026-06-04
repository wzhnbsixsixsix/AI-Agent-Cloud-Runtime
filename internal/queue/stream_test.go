package queue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestStreamPublishConsume(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	cli := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer cli.Close()

	q := NewStream(cli)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := q.EnsureGroup(ctx, "g1"); err != nil {
		t.Fatalf("ensure group: %v", err)
	}

	if _, err := q.Publish(ctx, Task{RunID: "r1", Prompt: "hi", TraceID: "t"}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	var got int32
	cctx, ccancel := context.WithCancel(ctx)
	defer ccancel()
	done := make(chan struct{})
	go func() {
		_ = q.Consume(cctx, "g1", "c1", 3, func(ctx context.Context, d Delivery) error {
			if d.Task.RunID == "r1" {
				atomic.StoreInt32(&got, 1)
				ccancel()
				close(done)
			}
			return nil
		})
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting message")
	}
	if atomic.LoadInt32(&got) != 1 {
		t.Fatalf("not received")
	}
}
