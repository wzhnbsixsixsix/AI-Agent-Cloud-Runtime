package controlplane

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

type DockerManager struct{ cli *client.Client }

func NewDockerManager() (*DockerManager, error) {
	c, e := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if e != nil {
		return nil, e
	}
	return &DockerManager{c}, nil
}
func (m *DockerManager) Close() error { return m.cli.Close() }
func (m *DockerManager) Create(ctx context.Context, a AgentSpec) (string, error) {
	if _, err := m.cli.VolumeCreate(ctx, volume.CreateOptions{Name: a.VolumeName, Labels: map[string]string{"agentforge.agent_id": a.ID}}); err != nil {
		return "", fmt.Errorf("create workspace volume: %w", err)
	}
	pids := a.PidsLimit
	resp, err := m.cli.ContainerCreate(ctx, &container.Config{Image: a.Image, Cmd: []string{"sleep", "infinity"}, WorkingDir: "/workspace", Labels: map[string]string{"agentforge.role": "persistent-agent", "agentforge.agent_id": a.ID}}, &container.HostConfig{ReadonlyRootfs: true, NetworkMode: "none", SecurityOpt: []string{"no-new-privileges:true"}, CapDrop: []string{"ALL"}, Mounts: []mount.Mount{{Type: mount.TypeVolume, Source: a.VolumeName, Target: "/workspace"}}, Resources: container.Resources{Memory: a.MemoryMB * 1024 * 1024, CPUQuota: a.CPUQuotaUS, CPUPeriod: 100000, PidsLimit: &pids}}, nil, nil, "agentforge-agent-"+a.ID)
	if err != nil {
		return "", err
	}
	if err := m.cli.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
		return "", err
	}
	return resp.ID, nil
}
func (m *DockerManager) Start(ctx context.Context, id string) error {
	return m.cli.ContainerStart(ctx, id, types.ContainerStartOptions{})
}
func (m *DockerManager) Stop(ctx context.Context, id string) error {
	t := 10
	return m.cli.ContainerStop(ctx, id, container.StopOptions{Timeout: &t})
}
func (m *DockerManager) Delete(ctx context.Context, a AgentSpec) error {
	if a.ContainerID != "" {
		_ = m.cli.ContainerRemove(ctx, a.ContainerID, types.ContainerRemoveOptions{Force: true})
	}
	if a.WorkspacePolicy == "delete" {
		return m.cli.VolumeRemove(ctx, a.VolumeName, true)
	}
	return nil
}
func safeWorkspacePath(p string) (string, error) {
	for _, segment := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if segment == ".." {
			return "", fmt.Errorf("invalid workspace path")
		}
	}
	p = strings.TrimPrefix(path.Clean("/"+p), "/")
	if p == "." {
		p = ""
	}
	return p, nil
}

type WorkspaceEntry struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Directory bool   `json:"directory"`
	Size      int64  `json:"size"`
}

func (m *DockerManager) ListWorkspace(ctx context.Context, a AgentSpec, dir string) ([]WorkspaceEntry, error) {
	p, e := safeWorkspacePath(dir)
	if e != nil {
		return nil, e
	}
	rc, _, e := m.cli.CopyFromContainer(ctx, a.ContainerID, path.Join("/workspace", p))
	if e != nil {
		return nil, e
	}
	defer rc.Close()
	tr := tar.NewReader(rc)
	out := []WorkspaceEntry{}
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		name := strings.TrimPrefix(path.Clean(h.Name), "./")
		if name == "" {
			continue
		}
		rel := strings.TrimPrefix(name, strings.TrimSuffix(p, "/")+"/")
		if p != "" && !strings.HasPrefix(name, p+"/") {
			continue
		}
		if strings.Contains(rel, "/") {
			continue
		}
		out = append(out, WorkspaceEntry{Path: path.Join(p, rel), Name: rel, Directory: h.Typeflag == tar.TypeDir, Size: h.Size})
	}
	return out, nil
}
func (m *DockerManager) ReadWorkspaceFile(ctx context.Context, a AgentSpec, file string) (string, error) {
	p, e := safeWorkspacePath(file)
	if e != nil || p == "" {
		return "", fmt.Errorf("invalid workspace file")
	}
	rc, _, e := m.cli.CopyFromContainer(ctx, a.ContainerID, path.Join("/workspace", p))
	if e != nil {
		return "", e
	}
	defer rc.Close()
	tr := tar.NewReader(rc)
	h, e := tr.Next()
	if e != nil {
		return "", e
	}
	if h.Size > 256*1024 {
		return "", fmt.Errorf("file exceeds 256 KiB preview limit")
	}
	b := bytes.NewBuffer(nil)
	if _, e = io.CopyN(b, tr, h.Size); e != nil {
		return "", e
	}
	if bytes.IndexByte(b.Bytes(), 0) >= 0 {
		return "", fmt.Errorf("binary files cannot be previewed")
	}
	return b.String(), nil
}
