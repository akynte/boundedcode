package orchestrator

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/task"
)

// TestAgentDependencyMounts: the agent gets the checkout's installed
// dependencies read-only, located through the ledger's repository path; a
// planted symlink at the mount point refuses the session.
func TestAgentDependencyMounts(t *testing.T) {
	src, wt := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "node_modules", "m"), 0o755); err != nil {
		t.Fatal(err)
	}
	ms, scratch, err := dependencyMounts([]task.Worktree{{RepoName: "web", RepoPath: src, Path: wt}})
	if err != nil {
		t.Fatal(err)
	}
	if len(scratch) != 3 || scratch[1] != filepath.Join(wt, "node_modules", ".vite") {
		t.Fatalf("scratch = %v", scratch)
	}
	if len(ms) != 1 || ms[0].Host != filepath.Join(src, "node_modules") || ms[0].Target != filepath.Join(wt, "node_modules") {
		t.Fatalf("mounts = %+v", ms)
	}
	if err := os.Symlink("/usr/bin", filepath.Join(wt, "node_modules")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := dependencyMounts([]task.Worktree{{RepoName: "web", RepoPath: src, Path: wt}}); !errors.Is(err, sandbox.ErrUnsafeDependencyTarget) {
		t.Fatalf("want ErrUnsafeDependencyTarget, got %v", err)
	}
}
