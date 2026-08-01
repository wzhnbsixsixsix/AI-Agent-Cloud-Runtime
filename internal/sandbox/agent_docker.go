package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// AgentDockerDriver executes tools in the persistent container created for an
// Agent by the Control Plane. Acquire receives an agent ID, resolves the
// container by its labels, and Release intentionally leaves it running so its
// named /workspace volume survives across tool calls and runs.
type AgentDockerDriver struct {
	cli      *client.Client
	hard     time.Duration
	closed   atomic.Bool
	closeOne sync.Once
}

func NewAgentDockerDriver(ctx context.Context, hardTimeout time.Duration) (*AgentDockerDriver, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	if _, err := cli.Ping(ctx); err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("docker ping: %w", err)
	}
	if hardTimeout <= 0 {
		hardTimeout = 60 * time.Second
	}
	return &AgentDockerDriver{cli: cli, hard: hardTimeout}, nil
}

func (d *AgentDockerDriver) Acquire(ctx context.Context, agentID string) (Sandbox, error) {
	if agentID == "" {
		return nil, errors.New("empty agent_id")
	}
	if d.closed.Load() {
		return nil, errors.New("driver closing")
	}
	containers, err := d.cli.ContainerList(ctx, types.ContainerListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", "agentforge.role=persistent-agent"),
			filters.Arg("label", "agentforge.agent_id="+agentID),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("list persistent agent container: %w", err)
	}
	if len(containers) == 0 {
		return nil, fmt.Errorf("persistent container for agent %q not found", agentID)
	}
	if len(containers) > 1 {
		return nil, fmt.Errorf("multiple persistent containers found for agent %q", agentID)
	}
	if containers[0].State != "running" {
		return nil, fmt.Errorf("persistent container for agent %q is %s", agentID, containers[0].State)
	}
	inspect, err := d.cli.ContainerInspect(ctx, containers[0].ID)
	if err != nil {
		return nil, fmt.Errorf("inspect persistent agent container: %w", err)
	}
	user := ""
	if inspect.Config != nil {
		user = inspect.Config.User
	}
	return &agentDockerSandbox{
		cli:         d.cli,
		containerID: containers[0].ID,
		agentID:     agentID,
		user:        user,
		hard:        d.hard,
	}, nil
}

func (d *AgentDockerDriver) Release(context.Context, Sandbox) error { return nil }
func (d *AgentDockerDriver) Stats() Stats                           { return Stats{} }
func (d *AgentDockerDriver) Close(context.Context) error {
	var err error
	d.closeOne.Do(func() {
		d.closed.Store(true)
		err = d.cli.Close()
	})
	return err
}

type agentDockerSandbox struct {
	cli         *client.Client
	containerID string
	agentID     string
	user        string
	hard        time.Duration
}

func (s *agentDockerSandbox) ID() string                 { return s.containerID }
func (s *agentDockerSandbox) RunID() string              { return s.agentID }
func (s *agentDockerSandbox) WorkspaceHost() string      { return "" }
func (s *agentDockerSandbox) WorkspaceContainer() string { return "/workspace" }

func (s *agentDockerSandbox) Exec(ctx context.Context, req ExecRequest) (ExecResult, error) {
	if len(req.Cmd) == 0 {
		return ExecResult{}, errors.New("empty cmd")
	}
	timeout := req.Timeout
	if timeout <= 0 || timeout > s.hard {
		timeout = s.hard
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	workdir := req.Dir
	if workdir == "" {
		workdir = s.WorkspaceContainer()
	}

	created, err := s.cli.ContainerExecCreate(execCtx, s.containerID, types.ExecConfig{
		User:         s.user,
		WorkingDir:   workdir,
		AttachStdin:  len(req.Stdin) > 0,
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          req.Cmd,
		Env:          req.Env,
	})
	if err != nil {
		return ExecResult{}, fmt.Errorf("exec create: %w", err)
	}
	attach, err := s.cli.ContainerExecAttach(execCtx, created.ID, types.ExecStartCheck{Tty: false})
	if err != nil {
		return ExecResult{}, fmt.Errorf("exec attach: %w", err)
	}
	defer attach.Close()
	start := time.Now()
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
	var stdout, stderr bytes.Buffer
	var truncated atomic.Bool
	copyDone := make(chan error, 1)
	go func() {
		_, copyErr := stdcopy.StdCopy(
			&capWriter{buf: &stdout, max: maxOut, truncated: &truncated},
			&capWriter{buf: &stderr, max: maxOut, truncated: &truncated},
			attach.Reader,
		)
		copyDone <- copyErr
	}()

	select {
	case <-execCtx.Done():
		return ExecResult{ExitCode: -1, Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), Truncated: truncated.Load(), Elapsed: time.Since(start)}, fmt.Errorf("exec timeout: %w", execCtx.Err())
	case copyErr := <-copyDone:
		if copyErr != nil && !errors.Is(copyErr, io.EOF) {
			return ExecResult{}, fmt.Errorf("exec copy: %w", copyErr)
		}
	}
	result, err := s.cli.ContainerExecInspect(context.Background(), created.ID)
	if err != nil {
		return ExecResult{}, fmt.Errorf("exec inspect: %w", err)
	}
	return ExecResult{
		ExitCode:  result.ExitCode,
		Stdout:    stdout.Bytes(),
		Stderr:    stderr.Bytes(),
		Truncated: truncated.Load(),
		Elapsed:   time.Since(start),
	}, nil
}
