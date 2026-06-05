// Package sandbox 提供 Agent 工具执行所需的隔离运行环境。
//
// W3 阶段实现 L1（Docker driver + 预热池），后续会按 PROJECT_DESIGN §4.4 增加：
//   - L2: gVisor (runsc) — W8 之后
//   - L3: Firecracker microVM — W9 之后
//
// 关键抽象：
//   - Driver 提供 Acquire/Release，背后维护预热池
//   - Sandbox 是一次 Acquire 拿到的独占资源，提供 Exec
//   - ExecRequest/ExecResult 是 stateless 的命令执行接口，方便后续 LLM function-calling 接入
package sandbox

import (
	"bytes"
	"context"
	"sync/atomic"
	"time"
)

// Sandbox 是一次 Acquire 拿到的隔离执行环境。
//
// 同一时刻一个 Sandbox 只能被一个 goroutine 使用；并发请用 Driver.Acquire 拿多个。
// Run 结束必须调用 Driver.Release，否则会泄漏容器与 host 工作目录。
type Sandbox interface {
	// ID 返回容器/进程标识，用于日志与审计。
	ID() string
	// RunID 返回此 Sandbox 绑定的 run_id（Acquire 时传入）。
	RunID() string
	// WorkspaceHost 返回宿主机侧 run workspace 绝对路径（仅在同机调试/测试时有用）。
	WorkspaceHost() string
	// WorkspaceContainer 返回容器内 workspace 的绝对路径，传给 Exec.Dir 默认值。
	WorkspaceContainer() string
	// Exec 执行一条命令。返回 ExecResult；若发生超时返回 ExitCode=-1 及 ctx 错误。
	Exec(ctx context.Context, req ExecRequest) (ExecResult, error)
}

// ExecRequest 一次命令执行的入参。
type ExecRequest struct {
	// Cmd argv，长度必须 > 0。
	Cmd []string
	// Stdin 可选；driver 会把它喂给进程 stdin 后立即 CloseWrite。
	Stdin []byte
	// Env 形如 "K=V"，附加到容器/进程默认环境。
	Env []string
	// Dir 工作目录；空则使用 Sandbox.WorkspaceContainer()。
	Dir string
	// Timeout 0 表示按 driver 全局 hardLimit 兜底。
	Timeout time.Duration
	// MaxOut 单路（stdout 或 stderr）最大字节数；超过截断并置 Truncated。
	// 0 → 默认 64KiB。
	MaxOut int64
}

// ExecResult 命令执行的出参。
type ExecResult struct {
	ExitCode  int
	Stdout    []byte
	Stderr    []byte
	Truncated bool
	Elapsed   time.Duration
}

// Stats 描述一个 Driver 当前池状态，用于 obs / 健康检查。
type Stats struct {
	PoolSize          int   // 配置的常驻 idle 数
	PoolReady         int   // 当前池中 idle 数
	InFlight          int   // 已 Acquire 未 Release 的数量
	SpawnCount        int64 // 累计创建数
	DestroyCount      int64 // 累计销毁数
	AcquireWaitMicros int64 // 累计 Acquire 等待 us（拨快了说明池不够大）
}

// Driver 提供 Sandbox 的获取/释放，内部维护预热池。
type Driver interface {
	// Acquire 取一个 sandbox，会阻塞至 ctx.Done 或 pool 有可用 slot。
	Acquire(ctx context.Context, runID string) (Sandbox, error)
	// Release 销毁 sandbox 并触发 pool 异步补位。
	// 即使 Release 返回 error，pool 也会继续工作。
	Release(ctx context.Context, sb Sandbox) error
	// Stats 当前池状态。
	Stats() Stats
	// Close 阻塞直到所有 in-flight sandbox 释放且 pool 清空（受 ctx 兜底）。
	Close(ctx context.Context) error
}

// capWriter 在写入达到 max 后丢弃后续字节并置位 Truncated 标志。
// 在 docker.go / memory.go 中被复用，避免重复实现。
type capWriter struct {
	buf       *bytes.Buffer
	max       int64
	truncated *atomic.Bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	remain := w.max - int64(w.buf.Len())
	if remain <= 0 {
		w.truncated.Store(true)
		return len(p), nil
	}
	if int64(len(p)) > remain {
		w.buf.Write(p[:remain])
		w.truncated.Store(true)
		return len(p), nil
	}
	return w.buf.Write(p)
}
