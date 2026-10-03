package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAdversarialContainer attempts common escapes from inside the sandbox.
// Every probe must fail. Set BC_TEST_DOCKER_IMAGE to run.
func TestAdversarialContainer(t *testing.T) {
	image := os.Getenv("BC_TEST_DOCKER_IMAGE")
	if image == "" || testing.Short() {
		t.Skip("set BC_TEST_DOCKER_IMAGE")
	}
	home, _ := os.UserHomeDir()
	ws, err := os.MkdirTemp(filepath.Join(home, ".cache"), "bc-adv-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(ws) })
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(ws, ".env"), []byte("DB_PASSWORD=hunter2-canary\n"), 0o600))
	must(os.MkdirAll(filepath.Join(ws, "deploy", "secrets"), 0o700))
	must(os.WriteFile(filepath.Join(ws, "deploy", "secrets", "key.pem"), []byte("PRIVATE-canary"), 0o600))
	must(os.WriteFile(filepath.Join(ws, "main.go"), []byte("package main\n"), 0o644))
	t.Setenv("BC_CANARY_API_KEY", "env-canary")

	c := &Container{Engine: "docker", Image: image, Network: "none", Memory: "512m", UID: os.Getuid(), GID: os.Getgid()}
	run := func(script string) (string, error) {
		cmd, err := c.Command(context.Background(), Spec{
			Argv: []string{"sh", "-c", script}, Workdir: ws,
			Mounts: []Mount{{Host: ws, Target: ws}},
			Masks:  []string{filepath.Join(ws, ".env"), filepath.Join(ws, "deploy", "secrets")},
		})
		if err != nil {
			return "", err
		}
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	probes := []struct {
		name, script, canary string
	}{
		{"read masked .env", "cat .env 2>&1; ls -la .env 2>&1", "hunter2-canary"},
		{"read masked secrets dir", "cat deploy/secrets/key.pem 2>&1; ls deploy/secrets 2>&1", "PRIVATE-canary"},
		{"host env secrets", "env", "env-canary"},
		{"host home", "ls -a " + home + " 2>&1; cat " + filepath.Join(home, ".ssh", "id_ed25519") + " 2>&1", ".bashrc"},
		{"network egress", "python3 -c \"import urllib.request;print(urllib.request.urlopen('http://1.1.1.1',timeout=3).status)\" 2>&1 || echo NO_NET", "200"},
		{"docker socket", "ls -la /var/run/docker.sock 2>&1", "srw"},
	}
	for _, p := range probes {
		out, _ := run(p.script)
		if strings.Contains(out, p.canary) {
			t.Errorf("%s: sandbox leaked %q:\n%s", p.name, p.canary, out)
		}
	}
	// Privilege: no root, no new privileges, no capabilities.
	out, _ := run("id -u; grep -E '^(CapEff|NoNewPrivs)' /proc/self/status")
	if !strings.Contains(out, "CapEff:\t0000000000000000") || !strings.Contains(out, "NoNewPrivs:\t1") || strings.HasPrefix(out, "0\n") {
		t.Errorf("privilege hardening missing:\n%s", out)
	}
	// Writes land only in the worktree; the root filesystem is the image's.
	out, err = run("echo ok > written.txt && cat written.txt")
	if err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("worktree not writable: %v %s", err, out)
	}
	if b, err := os.ReadFile(filepath.Join(ws, "written.txt")); err != nil || strings.TrimSpace(string(b)) != "ok" {
		t.Fatalf("worktree write not visible on host: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(ws, "written.txt")); err == nil {
		if st, ok := fileOwner(fi); ok && st != os.Getuid() {
			t.Errorf("file owned by uid %d, want %d", st, os.Getuid())
		}
	}
	// The masked .env must be unchanged on the host even if the agent writes to it.
	_, _ = run("echo pwned > .env 2>/dev/null; echo pwned > deploy/secrets/key.pem 2>/dev/null")
	if b, _ := os.ReadFile(filepath.Join(ws, ".env")); !strings.Contains(string(b), "hunter2-canary") {
		t.Error("masked .env was modified through the sandbox")
	}
}
