package openhands

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/agent"
	"github.com/akynte/boundedcode/internal/sandbox"
)

func TestToolchainMounts(t *testing.T) {
	mc, gc := t.TempDir(), filepath.Join(t.TempDir(), "agent-gocache")
	env := map[string]string{}
	ms := toolchainMounts(agent.Toolchain{GoModCache: mc, GoCache: gc}, env)
	if len(ms) != 2 || !ms[0].ReadOnly || ms[0].Host != mc || ms[1].ReadOnly || ms[1].Host != gc {
		t.Fatalf("mounts = %+v", ms)
	}
	if env["GOMODCACHE"] != mc || env["GOPROXY"] != "off" || env["GOCACHE"] != gc || !strings.Contains(env["GOFLAGS"], "-mod=mod") {
		t.Fatalf("env = %v", env)
	}
	// A missing module cache is skipped, not mounted or advertised.
	env = map[string]string{}
	if ms := toolchainMounts(agent.Toolchain{GoModCache: filepath.Join(mc, "missing")}, env); len(ms) != 0 || env["GOMODCACHE"] != "" {
		t.Fatalf("missing cache: %+v %v", ms, env)
	}
}

// TestAgentCanBuildWithDependenciesOffline is the regression test for the
// 2026-10-05 failure analysis: the agent sandbox had no module cache, so on
// gin, caddy and prometheus the agent could not build or run a single test
// (82 of 104 commands in one attempt went to looking for dependencies), while
// verification could. With the toolchain mounts, a module with an external
// dependency builds offline in the sandbox.
func TestAgentCanBuildWithDependenciesOffline(t *testing.T) {
	image := os.Getenv("BC_TEST_DOCKER_IMAGE")
	if image == "" || testing.Short() {
		t.Skip("set BC_TEST_DOCKER_IMAGE")
	}
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Skip("go toolchain required")
	}
	mc := strings.TrimSpace(string(out))
	if _, err := os.Stat(filepath.Join(mc, "github.com", "spf13", "cobra@v1.10.2")); err != nil {
		t.Skip("cobra v1.10.2 not in the module cache")
	}
	home, _ := os.UserHomeDir()
	root, err := os.MkdirTemp(filepath.Join(home, ".cache"), "bc-toolchain-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	ws := filepath.Join(root, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	gomod := "module example.com/x\n\ngo 1.22\n\nrequire github.com/spf13/cobra v1.10.2\n"
	main := "package main\n\nimport \"github.com/spf13/cobra\"\n\nfunc main() { _ = (&cobra.Command{}).Execute() }\n"
	for name, c := range map[string]string{"go.mod": gomod, "main.go": main} {
		if err := os.WriteFile(filepath.Join(ws, name), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	env := map[string]string{"HOME": "/tmp"}
	mounts := append([]sandbox.Mount{{Host: ws, Target: ws}}, toolchainMounts(agent.Toolchain{GoModCache: mc, GoCache: filepath.Join(root, "gocache")}, env)...)
	sb := &sandbox.Container{Engine: "docker", Image: image, Network: "none", PIDs: 4096, UID: os.Getuid(), GID: os.Getgid()}
	cmd, err := sb.Command(context.Background(), sandbox.Spec{Argv: []string{"go", "build", "./..."}, Workdir: ws, Mounts: mounts, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("offline build with dependencies failed: %v\n%s", err, b)
	}
	// The module cache stays read-only for the agent.
	cmd, _ = sb.Command(context.Background(), sandbox.Spec{Argv: []string{"sh", "-c", "touch " + mc + "/x"}, Workdir: ws, Mounts: mounts, Env: env})
	if err := cmd.Run(); err == nil {
		t.Fatal("agent could write the module cache")
	}
}
