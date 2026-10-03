package cbm

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCLIAndSession indexes a tiny repo and queries it both via one-shot CLI
// and via a persistent MCP session. Requires codebase-memory-mcp in PATH.
func TestCLIAndSession(t *testing.T) {
	bin, err := exec.LookPath("codebase-memory-mcp")
	if err != nil || testing.Short() {
		t.Skip("codebase-memory-mcp not installed")
	}
	home, _ := os.UserHomeDir()
	base, err := os.MkdirTemp(filepath.Join(home, ".cache"), "bc-cbm-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	repo := filepath.Join(base, "repo")
	_ = os.MkdirAll(repo, 0o755)
	_ = os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n\nfunc Charge() int { return helper() }\n\nfunc helper() int { return 1 }\n"), 0o644)
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "i"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}
	}
	c := &Client{Binary: bin, CacheDir: filepath.Join(base, "cache")}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	res, err := c.Index(ctx, repo, "t.repo", "fast")
	if err != nil || res.Nodes == 0 {
		t.Fatalf("index: %+v %v", res, err)
	}
	cli, err := c.Trace(ctx, "t.repo", "Charge", "outbound", 1)
	if err != nil || !strings.Contains(cli, "helper") {
		t.Fatalf("cli trace: %v\n%s", err, cli)
	}
	if err := c.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	t0 := time.Now()
	ses, err := c.Trace(ctx, "t.repo", "Charge", "outbound", 1)
	if err != nil || !strings.Contains(ses, "helper") {
		t.Fatalf("session trace: %v\n%s", err, ses)
	}
	t.Logf("session trace latency %s", time.Since(t0))
	if _, err := os.Stat(filepath.Join(base, "cache", ".boundedcode-configured")); err != nil {
		t.Fatal("private settings not applied")
	}
}
