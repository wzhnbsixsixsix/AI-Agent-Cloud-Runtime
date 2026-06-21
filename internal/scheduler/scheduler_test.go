package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisSchedulerPickLowestLoad(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()
	s := NewRedis(redis.NewClient(&redis.Options{Addr: mr.Addr()}))
	ctx := context.Background()
	for _, w := range []WorkerInfo{
		{WorkerID: "busy", Concurrency: 4, Load: 0.9, InFlight: 3},
		{WorkerID: "idle", Concurrency: 4, Load: 0.1, InFlight: 1},
	} {
		if err := s.Register(ctx, w, time.Minute); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	got, err := s.Pick(ctx, "run")
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if got != "idle" {
		t.Fatalf("want idle, got %s", got)
	}
}
