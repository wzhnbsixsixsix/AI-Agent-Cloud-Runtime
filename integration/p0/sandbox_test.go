package p0

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	osb "github.com/alibaba/OpenSandbox/sdks/sandbox/go"
)

type dockerInfo struct {
	OSType   string
	Runtimes map[string]json.RawMessage
}

// No fallback path exists: neither runc nor the memory driver is acceptable.
func requireRuntime(info dockerInfo, runtime string) error {
	if runtime != "runsc" && runtime != "kata-qemu" {
		return fmt.Errorf("unsupported secure runtime %q", runtime)
	}
	if info.OSType != "linux" {
		return errors.New("Linux Docker daemon required")
	}
	if _, ok := info.Runtimes[runtime]; !ok {
		return fmt.Errorf("required runtime %s missing; fallback forbidden", runtime)
	}
	return nil
}

func TestRuntimeFailClosed(t *testing.T) {
	for _, runtime := range []string{"runsc", "kata-qemu"} {
		if requireRuntime(dockerInfo{OSType: "linux", Runtimes: map[string]json.RawMessage{"runc": nil}}, runtime) == nil {
			t.Fatal("missing runtime accepted")
		}
		if requireRuntime(dockerInfo{OSType: "darwin", Runtimes: map[string]json.RawMessage{runtime: nil}}, runtime) == nil {
			t.Fatal("non-Linux accepted")
		}
		if err := requireRuntime(dockerInfo{OSType: "linux", Runtimes: map[string]json.RawMessage{runtime: nil}}, runtime); err != nil {
			t.Fatal(err)
		}
	}
	if requireRuntime(dockerInfo{OSType: "linux"}, "runc") == nil {
		t.Fatal("runc accepted")
	}
}

func TestSDKCreateFailureIsNotRetried(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/v1/sandboxes" {
			t.Error("unexpected API operation")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(503)
		fmt.Fprint(w, `{"code":"RUNTIME_UNAVAILABLE","message":"secure runtime unavailable"}`)
	}))
	defer server.Close()
	_, err := osb.CreateSandbox(context.Background(), osb.ConnectionConfig{Domain: server.URL, DisableMetrics: true}, osb.SandboxCreateOptions{Image: "test-only", SkipHealthCheck: true})
	if err == nil || calls != 1 {
		t.Fatalf("expected one failed create, got error=%v calls=%d", err, calls)
	}
}

// Commands return data only; never echo Docker inspection (it can contain keys).
func dockerJSON(ctx context.Context, target any, args ...string) error {
	out, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return fmt.Errorf("Docker %s failed; details suppressed", args[0])
	}
	if err := json.Unmarshal(out, target); err != nil {
		return errors.New("invalid Docker JSON")
	}
	return nil
}

type inspectedContainer struct {
	ID     string
	Config struct {
		Image  string
		Labels map[string]string
	}
	HostConfig struct {
		Runtime, NetworkMode string
		Privileged           bool
	}
	State struct{ Running bool }
}

func inspect(ctx context.Context, id string) (inspectedContainer, error) {
	var data []inspectedContainer
	err := dockerJSON(ctx, &data, "inspect", id)
	if err != nil {
		return inspectedContainer{}, err
	}
	if len(data) != 1 {
		return inspectedContainer{}, errors.New("expected exactly one container")
	}
	return data[0], nil
}

// A real server-startup negative test, separate from the local guard unit test.
// Only run on the dedicated Linux Docker host; --pull=never avoids confusing
// registry errors with successful fail-closed validation.
func testMissingRuntimeStartup(t *testing.T, image, execdImage string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	name := fmt.Sprintf("agentforge-p0-missing-%d", time.Now().UnixNano())
	config := "[server]\nhost = \"127.0.0.1\"\n[runtime]\ntype = \"docker\"\nexecd_image = \"" + execdImage + "\"\n[secure_runtime]\ntype = \"gvisor\"\ndocker_runtime = \"" + name + "\"\n"
	path := filepath.Join(t.TempDir(), "missing.toml")
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		// This unique test-owned container may already have been removed by --rm.
		_ = exec.CommandContext(cleanup, "docker", "rm", "-f", name).Run()
	})
	output, err := exec.CommandContext(ctx, "docker", "run", "--rm", "--pull=never", "--name", name, "--network", "none",
		"-v", "/var/run/docker.sock:/var/run/docker.sock", "-v", path+":/etc/opensandbox/config.toml:ro",
		"-e", "SANDBOX_CONFIG_PATH=/etc/opensandbox/config.toml", "-e", "OPENSANDBOX_SERVER_API_KEY=synthetic-startup-test-only", image).CombinedOutput()
	message := strings.ToLower(string(output))
	if ctx.Err() != nil || err == nil || !strings.Contains(message, name) || !strings.Contains(message, "not available") {
		t.Fatal("missing-runtime startup refusal not proven; output suppressed (image/config/startup error or timeout is not a pass)")
	}
}

func TestLiveSandbox(t *testing.T) {
	if os.Getenv("AGENTFORGE_LIVE_SANDBOX") != "1" {
		t.Skip("live sandbox not requested")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	var info dockerInfo
	if err := dockerJSON(ctx, &info, "info", "--format", "{{json .}}"); err != nil {
		t.Fatal(err)
	}
	var lock struct {
		Images        map[string]string
		WorkloadImage string `json:"workload_image"`
	}
	data, err := os.ReadFile("../../deploy/opensandbox/versions.lock.json")
	if err != nil || json.Unmarshal(data, &lock) != nil {
		t.Fatal("cannot read image lock")
	}
	t.Run("missing_runtime_startup", func(t *testing.T) { testMissingRuntimeStartup(t, lock.Images["server"], lock.Images["execd"]) })
	image := lock.WorkloadImage
	if !regexp.MustCompile(`^[a-zA-Z0-9./:_-]+@sha256:[a-f0-9]{64}$`).MatchString(image) {
		t.Fatal("workload_image in versions.lock.json must be frozen to an immutable Python 3 image with bash")
	}
	for _, profile := range []struct{ name, runtime, url, key string }{
		{"gvisor", "runsc", "http://127.0.0.1:18081", "P0_GVISOR_API_KEY"},
		{"kata", "kata-qemu", "http://127.0.0.1:18082", "P0_KATA_API_KEY"},
	} {
		t.Run(profile.name, func(t *testing.T) {
			if err := requireRuntime(info, profile.runtime); err != nil {
				t.Fatal(err)
			}
			if os.Getenv(profile.key) == "" {
				t.Fatalf("missing %s", profile.key)
			}
			server, err := inspect(ctx, "agentforge-p0-"+profile.name)
			if err != nil {
				t.Fatal(err)
			}
			if !server.State.Running || server.Config.Image != lock.Images["server"] {
				t.Fatal("server not running locked image")
			}
			// Compare the mounted config to the version-controlled profile, without
			// printing its contents. This is still not a production attestation.
			actual, err := exec.CommandContext(ctx, "docker", "exec", server.ID, "cat", "/etc/opensandbox/config.toml").Output()
			want, readErr := os.ReadFile("../../deploy/opensandbox/" + profile.name + ".toml")
			if err != nil || readErr != nil || string(actual) != string(want) {
				t.Fatal("server configuration differs from locked profile")
			}
			if profile.name == "gvisor" {
				var networks []struct{ Internal, EnableIPv6 bool }
				if err := dockerJSON(ctx, &networks, "network", "inspect", "agentforge-p0-offline"); err != nil {
					t.Fatal(err)
				}
				if len(networks) != 1 || !networks[0].Internal || networks[0].EnableIPv6 {
					t.Fatal("offline bridge must be internal and IPv4-only")
				}
			}
			config := osb.ConnectionConfig{Domain: profile.url, APIKey: os.Getenv(profile.key), UseServerProxy: true, DisableMetrics: true, RequestTimeout: 30 * time.Second}
			lc := osb.NewLifecycleClient(profile.url+"/v1", os.Getenv(profile.key), osb.WithTimeout(60*time.Second))
			ttl := 300
			req := osb.CreateSandboxRequest{Image: &osb.ImageSpec{URI: image}, Entrypoint: []string{"/bin/sh", "-c", "sleep 300"}, Timeout: &ttl, ResourceLimits: osb.ResourceLimits{"cpu": "1", "memory": "512Mi"}, Metadata: map[string]string{"agentforge-p0": "true"}}
			if profile.name == "kata" {
				req.NetworkPolicy = &osb.NetworkPolicy{DefaultAction: "deny", Egress: []osb.NetworkRule{{Action: "allow", Target: "example.com"}}}
			}
			created, err := lc.CreateSandbox(ctx, req)
			if err != nil {
				t.Fatal("sandbox create failed; no retry or fallback attempted")
			}
			t.Logf("created %s sandbox %s", profile.name, created.ID)
			t.Cleanup(func() {
				cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
				defer stop()
				if err := lc.DeleteSandbox(cleanup, created.ID); err != nil {
					t.Errorf("cleanup failed for sandbox %s; inspect and remove explicitly", created.ID)
					return
				}
				_, err := lc.GetSandbox(cleanup, created.ID)
				var apiErr *osb.APIError
				if !errors.As(err, &apiErr) || apiErr.StatusCode != 404 {
					t.Error("sandbox deletion not confirmed")
				}
			})
			// Inspect the actual workload BEFORE executing any probe.
			ids, err := exec.CommandContext(ctx, "docker", "ps", "-q", "--filter", "label=opensandbox.io/id="+created.ID).Output()
			if err != nil {
				t.Fatal("cannot inspect sandbox identity")
			}
			workloads := 0
			for _, id := range strings.Fields(string(ids)) {
				container, err := inspect(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				if container.Config.Image != image {
					continue
				}
				workloads++
				if container.HostConfig.Runtime != profile.runtime || container.HostConfig.Privileged {
					t.Fatal("actual workload runtime mismatch or privileged container; refusing execution")
				}
				if profile.name == "gvisor" && container.HostConfig.NetworkMode != "agentforge-p0-offline" {
					t.Fatal("offline workload on wrong network")
				}
			}
			if workloads != 1 {
				t.Fatal("could not uniquely identify workload; refusing execution")
			}
			sb, err := osb.ConnectSandbox(ctx, config, created.ID)
			if err != nil {
				t.Fatal("SDK connect/readiness failed")
			}
			run := func(command string) string {
				t.Helper()
				result, err := sb.RunCommandWithOpts(ctx, osb.RunCommandRequest{Command: command, Timeout: 30000}, nil)
				if err != nil || result == nil || result.Error != nil || result.Complete == nil || (result.ExitCode != nil && *result.ExitCode != 0) {
					t.Fatal("sandbox command failed or did not complete")
				}
				return result.Text()
			}
			if !strings.Contains(run("command -v bash && python3 --version && printf P0_EXEC_OK"), "P0_EXEC_OK") {
				t.Fatal("command stdout mismatch")
			}
			if err := sb.UploadFile(ctx, strings.NewReader("P0_FILE_OK"), osb.UploadFileOptions{FileName: "agentforge-p0.txt", Metadata: osb.FileMetadata{Path: "/tmp/agentforge-p0.txt", Mode: 600}}); err != nil {
				t.Fatal("file upload failed")
			}
			file, err := sb.DownloadFile(ctx, "/tmp/agentforge-p0.txt", "")
			if err != nil {
				t.Fatal("file download failed")
			}
			content, err := io.ReadAll(io.LimitReader(file, 1024))
			file.Close()
			if err != nil || string(content) != "P0_FILE_OK" {
				t.Fatal("file round trip failed")
			}
			// Python distinguishes successful denial assertions from a broken command.
			// No proxies, redirects or provider credentials are used by these probes.
			probe := `python3 - <<'PY'
import socket, urllib.request
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
def reachable(url):
    try:
        with opener.open(url, timeout=5) as r:
            return 200 <= r.status < 400
    except Exception:
        return False
def tcp(host, port):
    try:
        with socket.create_connection((host, port), timeout=3):
            return True
    except OSError:
        return False
`
			if profile.name == "gvisor" {
				probe += "assert not tcp('example.com',443), 'external domain reachable'\n"
			} else {
				policy, err := sb.GetEgressPolicy(ctx)
				if err != nil || policy == nil || policy.Policy == nil || policy.Policy.DefaultAction != "deny" || policy.EnforcementMode != "dns+nft" {
					t.Fatal("egress deny policy/dns+nft mode not active")
				}
				probe += "assert reachable('https://example.com'), 'allowed domain unreachable'\n"
				probe += "assert not tcp('example.org',443), 'denied domain reachable'\n"
			}
			probe += "assert not tcp('1.1.1.1', 443), 'direct IP bypass'\nassert not tcp('2606:4700:4700::1111',443), 'IPv6 bypass'\nprint('P0_NETWORK_OK')\nPY"
			if !strings.Contains(run(probe), "P0_NETWORK_OK") {
				t.Fatal("network probe incomplete")
			}
			t.Log("PASS: locked server/config, actual secure runtime, command/file round trip and network probes; not a full security audit")
		})
	}
}
