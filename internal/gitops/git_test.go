package gitops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// InitRepo creates a repository with one commit (test helper).
func initRepo(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "t"}} {
		if _, err := Run(ctx, dir, args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, dir, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, dir, "commit", "-q", "-m", "init"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestWorktreeLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	info, err := Inspect(ctx, repo)
	if err != nil || info.Branch != "main" || info.Dirty {
		t.Fatalf("inspect: %+v %v", info, err)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	for range 2 { // idempotent
		if err := EnsureWorktree(ctx, repo, wt, TaskBranch("t1"), info.Head); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(wt, "a.go"), []byte("package a\n\nfunc F() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "b.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := ChangedFiles(ctx, wt, info.Head)
	if err != nil || strings.Join(files, ",") != "a.go,b.go" {
		t.Fatalf("changed = %v %v", files, err)
	}
	d, err := Diff(ctx, wt, info.Head, false)
	if err != nil || !strings.Contains(d, "+func F() {}") || !strings.Contains(d, "b.go") {
		t.Fatalf("diff: %v\n%s", err, d)
	}
	sha, err := CommitAll(ctx, wt, "agent work")
	if err != nil || sha == info.Head {
		t.Fatalf("commit: %s %v", sha, err)
	}
	if _, err := CommitAll(ctx, repo, "x"); err == nil {
		t.Fatal("commit on main must be refused")
	}
	cd, err := CommonDir(ctx, wt)
	if err != nil || cd != filepath.Join(repo, ".git") {
		t.Fatalf("common dir %s %v", cd, err)
	}
	// Removing the worktree keeps the branch.
	if err := RemoveWorktree(ctx, repo, wt); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, repo, "rev-parse", "--verify", "agent/t1"); err != nil {
		t.Fatal("branch lost after worktree removal")
	}
	// Re-creating from the existing branch resumes the work.
	if err := EnsureWorktree(ctx, repo, wt, TaskBranch("t1"), info.Head); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wt, "b.go")); err != nil {
		t.Fatal("committed work not restored")
	}
}
