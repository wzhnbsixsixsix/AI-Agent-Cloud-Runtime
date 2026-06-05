//go:build integration_docker

package sandbox

import (
	"context"
	"strings"
	"testing"
	"time"
)

// 此文件需要 docker daemon 可用，并通过 build tag 启用：
//
//	go test -tags=integration_docker ./internal/sandbox -run Docker -v
func TestDockerDriver_HelloWorld(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	d, err := NewDockerDriver(ctx, DockerOptions{
		PoolSize:      1,
		WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("new docker driver: %v", err)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = d.Close(c)
	})

	sb, err := d.Acquire(ctx, "run-int-1")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer d.Release(ctx, sb)

	res, err := sb.Exec(ctx, ExecRequest{Cmd: []string{"echo", "hello"}})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if res.ExitCode != 0 || !strings.HasPrefix(string(res.Stdout), "hello") {
		t.Fatalf("unexpected: code=%d out=%q err=%q", res.ExitCode, res.Stdout, res.Stderr)
	}

	// 验证网络隔离：wget 应失败
	res, _ = sb.Exec(ctx, ExecRequest{Cmd: []string{"sh", "-c", "wget -q -O- http://1.1.1.1 || echo NETBLOCKED"}})
	if !strings.Contains(string(res.Stdout), "NETBLOCKED") {
		t.Fatalf("expected network blocked, got %q", res.Stdout)
	}

	// 验证 read-only rootfs
	res, _ = sb.Exec(ctx, ExecRequest{Cmd: []string{"sh", "-c", "touch /etc/foo 2>&1 || echo READONLY"}})
	if !strings.Contains(string(res.Stdout), "READONLY") {
		t.Fatalf("expected readonly, got %q", res.Stdout)
	}

	// 验证 workspace 可写
	res, err = sb.Exec(ctx, ExecRequest{Cmd: []string{"sh", "-c", "echo ok > t.txt && cat t.txt"}})
	if err != nil || res.ExitCode != 0 || !strings.Contains(string(res.Stdout), "ok") {
		t.Fatalf("workspace write failed: code=%d out=%q err=%v", res.ExitCode, res.Stdout, err)
	}
}
