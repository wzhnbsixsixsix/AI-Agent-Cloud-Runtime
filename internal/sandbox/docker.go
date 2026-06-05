package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/oklog/ulid/v2"
)

// DockerOptions 控制 DockerDriver 的所有可调参数。
type DockerOptions struct {
	// Image 默认 alpine:3.19；首次启动如本地无该镜像会自动 pull。
	Image string
	// PoolSize 预热池常驻 idle 容器数；默认 4。
	PoolSize int
	// AcquireTimeout 取容器最长等待时间，超时返回错误；默认 30s。
	AcquireTimeout time.Duration
	// WorkspaceRoot host 侧根目录；默认 ${TMPDIR}/agentforge。
	WorkspaceRoot string
	// MemoryBytes 单容器 memory 上限；默认 256MiB。
	MemoryBytes int64
	// CPUQuota / CPUPeriod 控制 CPU 上限；默认 50000/100000 = 0.5 CPU。
	CPUQuota  int64
	CPUPeriod int64
	// PidsLimit 默认 256。
	PidsLimit int64
	// ExecTimeoutHard 任意 ExecRequest.Timeout 都不能超过此值（兜底）；默认 60s。
	ExecTimeoutHard time.Duration
	// User 运行用户；默认 65534:65534 (nobody)。
	User string
	// TmpfsSize tmpfs /tmp 大小；默认 64m。
	TmpfsSize string
	// Logger 默认 slog.Default()。
	Logger *slog.Logger
}

func (o *DockerOptions) defaults() {
	if o.Image == "" {
		o.Image = "alpine:3.19"
	}
	if o.PoolSize <= 0 {
		o.PoolSize = 4
	}
	if o.AcquireTimeout <= 0 {
		o.AcquireTimeout = 30 * time.Second
	}
	if o.WorkspaceRoot == "" {
		o.WorkspaceRoot = filepath.Join(os.TempDir(), "agentforge")
	}
	if o.MemoryBytes <= 0 {
		o.MemoryBytes = 256 * 1024 * 1024
	}
	if o.CPUQuota <= 0 {
		o.CPUQuota = 50000
	}
	if o.CPUPeriod <= 0 {
		o.CPUPeriod = 100000
	}
	if o.PidsLimit <= 0 {
		o.PidsLimit = 256
	}
	if o.ExecTimeoutHard <= 0 {
		o.ExecTimeoutHard = 60 * time.Second
	}
	if o.User == "" {
		o.User = "65534:65534"
	}
	if o.TmpfsSize == "" {
		o.TmpfsSize = "64m"
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
}

// DockerDriver 基于 Docker Engine API 的 Driver 实现。
type DockerDriver struct {
	cli *client.Client
	opt DockerOptions
	log *slog.Logger

	pool      chan *warmSlot
	closing   chan struct{}
	closeOnce sync.Once

	mu       sync.Mutex
	inFlight map[string]*warmSlot

	spawnCount   atomic.Int64
	destroyCount atomic.Int64
	waitMicros   atomic.Int64
}

type warmSlot struct {
	containerID   string
	name          string
	workspaceHost string // host path bound to /workspace
	runID         string // 仅在 Acquire 后设置
}

// NewDockerDriver 与 docker daemon 建立连接，预热 PoolSize 个 idle 容器。
//
// 调用方需保证：
//   - 进程内能访问到 docker socket（通过 DOCKER_HOST 或 /var/run/docker.sock）
//   - WorkspaceRoot 与 docker daemon 的视角一致（典型场景：worker 跑在容器内时，
//     需要把宿主机 /tmp/agentforge bind mount 到 worker 容器同路径，否则 daemon 找不到 source）
func NewDockerDriver(ctx context.Context, opt DockerOptions) (*DockerDriver, error) {
	opt.defaults()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	if _, err := cli.Ping(ctx); err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("docker ping: %w", err)
	}
	if err := os.MkdirAll(opt.WorkspaceRoot, 0o755); err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("workspace root: %w", err)
	}
	if err := ensureImage(ctx, cli, opt.Image, opt.Logger); err != nil {
		_ = cli.Close()
		return nil, err
	}
	d := &DockerDriver{
		cli:      cli,
		opt:      opt,
		log:      opt.Logger.With("comp", "sandbox.docker"),
		pool:     make(chan *warmSlot, opt.PoolSize),
		closing:  make(chan struct{}),
		inFlight: map[string]*warmSlot{},
	}
	// 预热池：失败的不计入，先把能起的塞进去
	for i := 0; i < opt.PoolSize; i++ {
		slot, err := d.spawnSlot(ctx)
		if err != nil {
			d.log.Warn("warm spawn failed", "i", i, "err", err)
			continue
		}
		d.pool <- slot
	}
	d.log.Info("docker driver ready",
		"image", opt.Image, "pool_size", opt.PoolSize,
		"pool_ready", len(d.pool), "workspace_root", opt.WorkspaceRoot)
	return d, nil
}

func ensureImage(ctx context.Context, cli *client.Client, ref string, log *slog.Logger) error {
	if _, _, err := cli.ImageInspectWithRaw(ctx, ref); err == nil {
		return nil
	}
	log.Info("pulling sandbox image", "image", ref)
	rc, err := cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("image pull: %w", err)
	}
	defer rc.Close()
	if _, err := io.Copy(io.Discard, rc); err != nil {
		return fmt.Errorf("image pull drain: %w", err)
	}
	return nil
}

// spawnSlot 创建并启动一个新的 warm 容器。
func (d *DockerDriver) spawnSlot(ctx context.Context) (*warmSlot, error) {
	id := ulid.Make().String()
	name := "agentforge-sb-" + id[len(id)-12:]
	hostWorkspace := filepath.Join(d.opt.WorkspaceRoot, name)
	if err := os.MkdirAll(hostWorkspace, 0o777); err != nil {
		return nil, fmt.Errorf("mkdir host workspace: %w", err)
	}

	cfg := &container.Config{
		Image:      d.opt.Image,
		User:       d.opt.User,
		Cmd:        []string{"sleep", "infinity"},
		WorkingDir: "/workspace",
		Tty:        false,
		Labels: map[string]string{
			"agentforge.role": "sandbox-l1",
			"agentforge.slot": id,
		},
		NetworkDisabled: true,
	}
	pids := d.opt.PidsLimit
	hostCfg := &container.HostConfig{
		AutoRemove:     false,
		ReadonlyRootfs: true,
		NetworkMode:    "none",
		SecurityOpt:    []string{"no-new-privileges:true"},
		CapDrop:        []string{"ALL"},
		Tmpfs: map[string]string{
			"/tmp": "rw,nosuid,nodev,size=" + d.opt.TmpfsSize,
		},
		Mounts: []mount.Mount{{
			Type:   mount.TypeBind,
			Source: hostWorkspace,
			Target: "/workspace",
		}},
		Resources: container.Resources{
			Memory:    d.opt.MemoryBytes,
			CPUQuota:  d.opt.CPUQuota,
			CPUPeriod: d.opt.CPUPeriod,
			PidsLimit: &pids,
		},
	}
	netCfg := &network.NetworkingConfig{}

	resp, err := d.cli.ContainerCreate(ctx, cfg, hostCfg, netCfg, nil, name)
	if err != nil {
		_ = os.RemoveAll(hostWorkspace)
		return nil, fmt.Errorf("container create: %w", err)
	}
	if err := d.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		_ = d.cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		_ = os.RemoveAll(hostWorkspace)
		return nil, fmt.Errorf("container start: %w", err)
	}
	d.spawnCount.Add(1)
	return &warmSlot{
		containerID:   resp.ID,
		name:          name,
		workspaceHost: hostWorkspace,
	}, nil
}

// Acquire 取一个 warm slot 并绑定 runID。
func (d *DockerDriver) Acquire(ctx context.Context, runID string) (Sandbox, error) {
	if runID == "" {
		return nil, errors.New("empty run_id")
	}
	select {
	case <-d.closing:
		return nil, errors.New("driver closing")
	default:
	}
	aCtx := ctx
	if d.opt.AcquireTimeout > 0 {
		var cancel context.CancelFunc
		aCtx, cancel = context.WithTimeout(ctx, d.opt.AcquireTimeout)
		defer cancel()
	}
	start := time.Now()
	var slot *warmSlot
	select {
	case slot = <-d.pool:
	case <-aCtx.Done():
		return nil, fmt.Errorf("acquire: %w", aCtx.Err())
	case <-d.closing:
		return nil, errors.New("driver closing")
	}
	d.waitMicros.Add(time.Since(start).Microseconds())

	slot.runID = runID
	runDir := filepath.Join(slot.workspaceHost, "runs", runID)
	if err := os.MkdirAll(runDir, 0o777); err != nil {
		go d.destroyAndRefill(slot)
		return nil, fmt.Errorf("mkdir run workspace: %w", err)
	}
	d.mu.Lock()
	d.inFlight[slot.containerID] = slot
	d.mu.Unlock()
	return &dockerSandbox{slot: slot, drv: d}, nil
}

// Release 销毁该容器，并异步补位。
func (d *DockerDriver) Release(_ context.Context, sb Sandbox) error {
	ds, ok := sb.(*dockerSandbox)
	if !ok {
		return errors.New("foreign sandbox")
	}
	d.mu.Lock()
	delete(d.inFlight, ds.slot.containerID)
	d.mu.Unlock()
	go d.destroyAndRefill(ds.slot)
	return nil
}

// destroyAndRefill 销毁旧容器并发起一个新 slot 进入 pool。
//
// 用独立 background context，避免 Release 上层 ctx cancel 后 docker rm 失败。
func (d *DockerDriver) destroyAndRefill(slot *warmSlot) {
	rmCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := d.cli.ContainerRemove(rmCtx, slot.containerID, container.RemoveOptions{
		Force: true, RemoveVolumes: true,
	}); err != nil {
		d.log.Warn("container remove", "id", slot.containerID, "err", err)
	}
	_ = os.RemoveAll(slot.workspaceHost)
	d.destroyCount.Add(1)

	select {
	case <-d.closing:
		return
	default:
	}
	spawnCtx, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel2()
	ns, err := d.spawnSlot(spawnCtx)
	if err != nil {
		d.log.Warn("refill spawn failed", "err", err)
		return
	}
	select {
	case d.pool <- ns:
	case <-d.closing:
		_ = d.cli.ContainerRemove(context.Background(), ns.containerID, container.RemoveOptions{Force: true})
		_ = os.RemoveAll(ns.workspaceHost)
	}
}

// Stats 当前池状态。
func (d *DockerDriver) Stats() Stats {
	d.mu.Lock()
	inflight := len(d.inFlight)
	d.mu.Unlock()
	return Stats{
		PoolSize:          d.opt.PoolSize,
		PoolReady:         len(d.pool),
		InFlight:          inflight,
		SpawnCount:        d.spawnCount.Load(),
		DestroyCount:      d.destroyCount.Load(),
		AcquireWaitMicros: d.waitMicros.Load(),
	}
}

// Close drain 池，等 in-flight 释放后关闭 docker client。
func (d *DockerDriver) Close(ctx context.Context) error {
	d.closeOnce.Do(func() { close(d.closing) })

drainLoop:
	for {
		select {
		case slot := <-d.pool:
			rmCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = d.cli.ContainerRemove(rmCtx, slot.containerID, container.RemoveOptions{Force: true})
			cancel()
			_ = os.RemoveAll(slot.workspaceHost)
		default:
			break drainLoop
		}
	}
	for {
		d.mu.Lock()
		n := len(d.inFlight)
		d.mu.Unlock()
		if n == 0 {
			return d.cli.Close()
		}
		select {
		case <-ctx.Done():
			return d.cli.Close()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// ---------- dockerSandbox ----------

type dockerSandbox struct {
	slot *warmSlot
	drv  *DockerDriver
}

func (s *dockerSandbox) ID() string                 { return s.slot.containerID }
func (s *dockerSandbox) RunID() string              { return s.slot.runID }
func (s *dockerSandbox) WorkspaceHost() string      { return filepath.Join(s.slot.workspaceHost, "runs", s.slot.runID) }
func (s *dockerSandbox) WorkspaceContainer() string { return "/workspace/runs/" + s.slot.runID }

func (s *dockerSandbox) Exec(ctx context.Context, req ExecRequest) (ExecResult, error) {
	if len(req.Cmd) == 0 {
		return ExecResult{}, errors.New("empty cmd")
	}
	timeout := req.Timeout
	if timeout <= 0 || timeout > s.drv.opt.ExecTimeoutHard {
		timeout = s.drv.opt.ExecTimeoutHard
	}
	eCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	workdir := req.Dir
	if workdir == "" {
		workdir = s.WorkspaceContainer()
	}

	cfg := types.ExecConfig{
		User:         s.drv.opt.User,
		WorkingDir:   workdir,
		AttachStdin:  len(req.Stdin) > 0,
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          req.Cmd,
		Env:          req.Env,
	}
	cresp, err := s.drv.cli.ContainerExecCreate(eCtx, s.slot.containerID, cfg)
	if err != nil {
		return ExecResult{}, fmt.Errorf("exec create: %w", err)
	}

	attach, err := s.drv.cli.ContainerExecAttach(eCtx, cresp.ID, types.ExecStartCheck{Tty: false})
	if err != nil {
		return ExecResult{}, fmt.Errorf("exec attach: %w", err)
	}
	defer attach.Close()

	start := time.Now()

	// stdin: 写完立刻 CloseWrite，避免子进程一直等待
	if len(req.Stdin) > 0 {
		go func() {
			_, _ = attach.Conn.Write(req.Stdin)
			_ = attach.CloseWrite()
		}()
	}

	maxOut := req.MaxOut
	if maxOut <= 0 {
		maxOut = 64 * 1024
	}
	var stdoutBuf, stderrBuf bytes.Buffer
	var truncated atomic.Bool
	stdoutW := &capWriter{buf: &stdoutBuf, max: maxOut, truncated: &truncated}
	stderrW := &capWriter{buf: &stderrBuf, max: maxOut, truncated: &truncated}

	copyDone := make(chan error, 1)
	go func() {
		_, err := stdcopy.StdCopy(stdoutW, stderrW, attach.Reader)
		copyDone <- err
	}()

	select {
	case <-eCtx.Done():
		// 超时：HijackedResponse.Close() 会触发 reader EOF；返回部分输出 + ctx err
		return ExecResult{
			ExitCode:  -1,
			Stdout:    stdoutBuf.Bytes(),
			Stderr:    stderrBuf.Bytes(),
			Truncated: truncated.Load(),
			Elapsed:   time.Since(start),
		}, fmt.Errorf("exec timeout: %w", eCtx.Err())
	case err := <-copyDone:
		if err != nil && !errors.Is(err, io.EOF) {
			return ExecResult{}, fmt.Errorf("exec copy: %w", err)
		}
	}

	// inspect 用独立 ctx，避免 eCtx 已超时
	inspect, err := s.drv.cli.ContainerExecInspect(context.Background(), cresp.ID)
	if err != nil {
		return ExecResult{}, fmt.Errorf("exec inspect: %w", err)
	}
	return ExecResult{
		ExitCode:  inspect.ExitCode,
		Stdout:    stdoutBuf.Bytes(),
		Stderr:    stderrBuf.Bytes(),
		Truncated: truncated.Load(),
		Elapsed:   time.Since(start),
	}, nil
}
