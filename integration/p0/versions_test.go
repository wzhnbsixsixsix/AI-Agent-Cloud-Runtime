package p0

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestPinnedProfiles(t *testing.T) {
	read := func(path string) []byte {
		t.Helper()
		b, err := os.ReadFile("../../" + path)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var lock struct {
		Go            string
		GoImageTag    string `json:"go_image_tag"`
		GoImageDigest string `json:"go_image_digest"`
		WorkloadImage string `json:"workload_image"`
		Images        map[string]string
		OpenSandbox   struct{ SDK string }
		SDKs          map[string]string `json:"validation_sdks"`
	}
	if err := json.Unmarshal(read("deploy/opensandbox/versions.lock.json"), &lock); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(read("go.mod")), "go "+lock.Go+"\n") || !strings.Contains(string(read("integration/p0/go.mod")), "go "+lock.Go+"\n") {
		t.Fatal("Go version drift")
	}
	if !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(lock.GoImageDigest) ||
		!regexp.MustCompile(`^docker.io/library/python:[a-zA-Z0-9._-]+@sha256:[a-f0-9]{64}$`).MatchString(lock.WorkloadImage) {
		t.Fatal("Go or workload digest missing or malformed")
	}
	for _, path := range []string{"Makefile", "build/Dockerfile"} {
		if !strings.Contains(string(read(path)), lock.GoImageTag+"@"+lock.GoImageDigest) {
			t.Fatalf("Go image drift: %s", path)
		}
	}
	for name, image := range lock.Images {
		if !regexp.MustCompile(`^docker.io/opensandbox/[a-z-]+@sha256:[a-f0-9]{64}$`).MatchString(image) {
			t.Fatalf("image not pinned: %s", name)
		}
	}
	module := string(read("integration/p0/go.mod"))
	lock.SDKs["opensandbox"] = lock.OpenSandbox.SDK
	for name, version := range lock.SDKs {
		pair := strings.Split(version, "@")
		if len(pair) != 2 || !strings.Contains(module, pair[0]+" "+pair[1]+"\n") {
			t.Fatalf("SDK lock drift: %s", name)
		}
	}
	for _, profile := range []string{"gvisor", "kata"} {
		var config struct {
			Server struct {
				Host   string
				Port   int
				APIKey string `toml:"api_key"`
			}
			Runtime struct {
				Type       string
				ExecdImage string `toml:"execd_image"`
			}
			Secure struct {
				Type          string
				DockerRuntime string `toml:"docker_runtime"`
			} `toml:"secure_runtime"`
			Docker struct {
				Network         string `toml:"network_mode"`
				NoNewPrivileges bool   `toml:"no_new_privileges"`
			}
			Egress struct {
				Image, Mode string
				DisableIPv6 bool `toml:"disable_ipv6"`
			}
		}
		if err := toml.Unmarshal(read("deploy/opensandbox/"+profile+".toml"), &config); err != nil {
			t.Fatal(err)
		}
		if config.Server.Host != "127.0.0.1" || config.Server.APIKey != "" || config.Runtime.Type != "docker" || config.Runtime.ExecdImage != lock.Images["execd"] || !config.Docker.NoNewPrivileges {
			t.Fatalf("unsafe or unpinned %s configuration", profile)
		}
		if profile == "gvisor" {
			if config.Secure.Type != "gvisor" || config.Secure.DockerRuntime != "runsc" || config.Docker.Network != "agentforge-p0-offline" || config.Egress.Image != "" {
				t.Fatal("gVisor isolation profile drift")
			}
		} else if config.Secure.Type != "kata" || config.Secure.DockerRuntime != "kata-qemu" || config.Docker.Network != "bridge" || config.Egress.Image != lock.Images["egress"] || config.Egress.Mode != "dns+nft" || !config.Egress.DisableIPv6 {
			t.Fatal("Kata isolation profile drift")
		}
	}
}
