// Package scheduler W1 仅做 worker 注册/心跳；Pick 留接口给 W8 Raft 实现。
package scheduler

import (
	"context"
	"errors"
	"time"
)

// ErrNotImplemented Pick 在 W1 阶段未实现。
var ErrNotImplemented = errors.New("scheduler: pick not implemented in W1")

// WorkerInfo 注册信息。
type WorkerInfo struct {
	WorkerID    string
	Addr        string
	Concurrency int32
	Labels      map[string]string
	UpdatedAt   time.Time
	InFlight    int32
	Load        float64
}

// Scheduler 调度器抽象。
type Scheduler interface {
	Register(ctx context.Context, info WorkerInfo, ttl time.Duration) error
	Heartbeat(ctx context.Context, workerID string, inFlight int32, load float64, ttl time.Duration) error
	Pick(ctx context.Context, runID string) (workerID string, err error)
	List(ctx context.Context) ([]WorkerInfo, error)
}
