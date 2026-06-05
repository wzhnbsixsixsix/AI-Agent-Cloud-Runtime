package sandbox

import (
	"context"
	"testing"
	"time"
)

func TestMemoryDriver_AcquireRelease(t *testing.T) {
	d, err := NewMemoryDriver(t.TempDir(), 2)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = d.Close(ctx)
	})

	ctx := context.Background()
	a, err := d.Acquire(ctx, "run-1")
	if err != nil {
		t.Fatalf("acquire1: %v", err)
	}
	if a.RunID() != "run-1" {
		t.Fatalf("run id mismatch: %q", a.RunID())
	}
	b, err := d.Acquire(ctx, "run-2")
	if err != nil {
		t.Fatalf("acquire2: %v", err)
	}
	st := d.Stats()
	if st.InFlight != 2 || st.PoolReady != 0 {
		t.Fatalf("after 2 acquire stats=%+v", st)
	}

	if err := d.Release(ctx, a); err != nil {
		t.Fatalf("release: %v", err)
	}
	// 等异步补位
	if !waitFor(time.Second, func() bool {
		s := d.Stats()
		return s.InFlight == 1 && s.PoolReady >= 1
	}) {
		t.Fatalf("refill timeout, stats=%+v", d.Stats())
	}
	_ = d.Release(ctx, b)
}

func TestMemoryDriver_AcquireBlocksThenSucceeds(t *testing.T) {
	d, err := NewMemoryDriver(t.TempDir(), 1)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = d.Close(ctx)
	})
	ctx := context.Background()
	a, err := d.Acquire(ctx, "r1")
	if err != nil {
		t.Fatalf("acquire1: %v", err)
	}

	got := make(chan error, 1)
	go func() {
		ctx2, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := d.Acquire(ctx2, "r2")
		got <- err
	}()

	// 短暂等待，确认第二个 Acquire 还在阻塞
	select {
	case err := <-got:
		t.Fatalf("expected blocking, got immediate: %v", err)
	case <-time.After(80 * time.Millisecond):
	}

	_ = d.Release(ctx, a)
	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("acquire2 after release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("acquire2 didn't unblock after release")
	}
}

func TestMemoryDriver_AcquireCtxCancel(t *testing.T) {
	d, err := NewMemoryDriver(t.TempDir(), 1)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	t.Cleanup(func() { _ = d.Close(context.Background()) })

	a, _ := d.Acquire(context.Background(), "r1")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = d.Acquire(ctx, "r2")
	if err == nil {
		t.Fatal("expected ctx err, got nil")
	}
	_ = d.Release(context.Background(), a)
}

func TestMemoryDriver_Close(t *testing.T) {
	d, err := NewMemoryDriver(t.TempDir(), 3)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ctx := context.Background()
	a, _ := d.Acquire(ctx, "r1")

	closeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	go func() {
		time.Sleep(80 * time.Millisecond)
		_ = d.Release(ctx, a)
	}()
	if err := d.Close(closeCtx); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func waitFor(timeout time.Duration, ok func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return ok()
}
