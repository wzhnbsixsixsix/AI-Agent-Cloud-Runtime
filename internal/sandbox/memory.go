package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// MemoryDriver 进程内 sandbox（不带容器隔离），仅用于：
//   - 单测 / CI 没有 docker
//   - 本地开发快速联调
//
// Exec 走 os/exec 真实执行命令；workspace 在 host 文件系统。
// 不要在生产用，没有任何 cgroup/cap/network 隔离。
type MemoryDriver struct {
	root      string
	poolSize  int
	pool      chan *memSlot
	closing   chan struct{}
	closeOnce sync.Once

	mu       sync.Mutex
	inFlight map[string]*memSlot

	spawnCnt   atomic.Int64
	destroyCnt atomic.Int64
	waitMicros atomic.Int64
}

type memSlot struct {
	id            string
	workspaceHost string
	runID         string
}

// NewMemoryDriver 创建一个内存型 Driver。
func NewMemoryDriver(root string, poolSize int) (*MemoryDriver, error) {
	if root == "" {
		root = filepath.Join(os.TempDir(), "agentforge-mem")
	}
	if poolSize <= 0 {
		poolSize = 2
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir root: %w", err)
	}
	d := &MemoryDriver{
		root:     root,
		poolSize: poolSize,
		pool:     make(chan *memSlot, poolSize),
		closing:  make(chan struct{}),
		inFlight: map[string]*memSlot{},
	}
	for i := 0; i < poolSize; i++ {
		s, err := d.spawn()
		if err != nil {
			return nil, err
		}
		d.pool <- s
	}
	return d, nil
}

func (d *MemoryDriver) spawn() (*memSlot, error) {
	id := fmt.Sprintf("mem-%d-%d", time.Now().UnixNano(), d.spawnCnt.Add(1))
	p := filepath.Join(d.root, id)
	if err := os.MkdirAll(p, 0o755); err != nil {
		return nil, err
	}
	return &memSlot{id: id, workspaceHost: p}, nil
}

// Acquire 阻塞取 slot。
func (d *MemoryDriver) Acquire(ctx context.Context, runID string) (Sandbox, error) {
	if runID == "" {
		return nil, errors.New("empty run_id")
	}
	select {
	case <-d.closing:
		return nil, errors.New("driver closing")
	default:
	}
	start := time.Now()
	var slot *memSlot
	select {
	case slot = <-d.pool:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-d.closing:
		return nil, errors.New("driver closing")
	}
	d.waitMicros.Add(time.Since(start).Microseconds())
	slot.runID = runID
	runDir := filepath.Join(slot.workspaceHost, "runs", runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		go d.destroyAndRefill(slot)
		return nil, fmt.Errorf("mkdir run workspace: %w", err)
	}
	d.mu.Lock()
	d.inFlight[slot.id] = slot
	d.mu.Unlock()
	return &memorySandbox{slot: slot, drv: d}, nil
}

// Release 释放并触发异步补位。
func (d *MemoryDriver) Release(_ context.Context, sb Sandbox) error {
	ms, ok := sb.(*memorySandbox)
	if !ok {
		return errors.New("foreign sandbox")
	}
	d.mu.Lock()
	delete(d.inFlight, ms.slot.id)
	d.mu.Unlock()
	go d.destroyAndRefill(ms.slot)
	return nil
}

func (d *MemoryDriver) destroyAndRefill(slot *memSlot) {
	_ = os.RemoveAll(slot.workspaceHost)
	d.destroyCnt.Add(1)
	select {
	case <-d.closing:
		return
	default:
	}
	s, err := d.spawn()
	if err != nil {
		return
	}
	select {
	case d.pool <- s:
	case <-d.closing:
		_ = os.RemoveAll(s.workspaceHost)
	}
}

// Stats 当前池状态。
func (d *MemoryDriver) Stats() Stats {
	d.mu.Lock()
	inf := len(d.inFlight)
	d.mu.Unlock()
	return Stats{
		PoolSize:          d.poolSize,
		PoolReady:         len(d.pool),
		InFlight:          inf,
		SpawnCount:        d.spawnCnt.Load(),
		DestroyCount:      d.destroyCnt.Load(),
		AcquireWaitMicros: d.waitMicros.Load(),
	}
}

// Close drain pool 并等 in-flight 释放。
func (d *MemoryDriver) Close(ctx context.Context) error {
	d.closeOnce.Do(func() { close(d.closing) })
drainLoop:
	for {
		select {
		case s := <-d.pool:
			_ = os.RemoveAll(s.workspaceHost)
		default:
			break drainLoop
		}
	}
	for {
		d.mu.Lock()
		n := len(d.inFlight)
		d.mu.Unlock()
		if n == 0 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return os.RemoveAll(d.root)
}

// ---------- memorySandbox ----------

type memorySandbox struct {
	slot *memSlot
	drv  *MemoryDriver
}

func (s *memorySandbox) ID() string                 { return s.slot.id }
func (s *memorySandbox) RunID() string              { return s.slot.runID }
func (s *memorySandbox) WorkspaceHost() string      { return filepath.Join(s.slot.workspaceHost, "runs", s.slot.runID) }
func (s *memorySandbox) WorkspaceContainer() string { return s.WorkspaceHost() }

func (s *memorySandbox) Exec(ctx context.Context, req ExecRequest) (ExecResult, error) {
	if len(req.Cmd) == 0 {
		return ExecResult{}, errors.New("empty cmd")
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	eCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	workdir := req.Dir
	if workdir == "" {
		workdir = s.WorkspaceContainer()
	}

	cmd := exec.CommandContext(eCtx, req.Cmd[0], req.Cmd[1:]...)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), req.Env...)

	maxOut := req.MaxOut
	if maxOut <= 0 {
		maxOut = 64 * 1024
	}
	var so, se bytes.Buffer
	var truncated atomic.Bool
	cmd.Stdout = &capWriter{buf: &so, max: maxOut, truncated: &truncated}
	cmd.Stderr = &capWriter{buf: &se, max: maxOut, truncated: &truncated}
	if len(req.Stdin) > 0 {
		cmd.Stdin = bytes.NewReader(req.Stdin)
	}

	start := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(start)
	res := ExecResult{Stdout: so.Bytes(), Stderr: se.Bytes(), Truncated: truncated.Load(), Elapsed: elapsed}
	if runErr == nil {
		return res, nil
	}
	if eCtx.Err() != nil {
		res.ExitCode = -1
		return res, fmt.Errorf("exec timeout: %w", eCtx.Err())
	}
	if ee, ok := runErr.(*exec.ExitError); ok {
		res.ExitCode = ee.ExitCode()
		return res, nil
	}
	// 启动失败（如二进制不存在）等
	return res, runErr
}
