package gitops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

func TestCheckWorktreeDetectsTampering(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	info, _ := Inspect(ctx, repo)
	wt := filepath.Join(t.TempDir(), "wt")
	if err := EnsureWorktree(ctx, repo, wt, TaskBranch("t2"), info.Head); err != nil {
		t.Fatal(err)
	}
	common, _ := CommonDir(ctx, repo)
	if err := CheckWorktree(wt, common); err != nil {
		t.Fatalf("fresh worktree rejected: %v", err)
	}
	// An agent redirects .git to a crafted gitdir inside the worktree.
	evil := filepath.Join(wt, ".evil")
	_ = os.MkdirAll(evil, 0o755)
	_ = os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+evil+"\n"), 0o644)
	if err := CheckWorktree(wt, common); err == nil {
		t.Fatal("tampered pointer accepted")
	}
	_ = os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+filepath.Join(common, "worktrees", "..", "..", "x")+"\n"), 0o644)
	if err := CheckWorktree(wt, common); err == nil {
		t.Fatal("path traversal accepted")
	}
}

func TestHostGitIgnoresHooks(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	canary := filepath.Join(t.TempDir(), "pwned")
	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	_ = os.WriteFile(hook, []byte("#!/bin/sh\ntouch "+canary+"\n"), 0o755)
	_ = os.WriteFile(filepath.Join(repo, "x.go"), []byte("package a\n"), 0o644)
	if _, err := Run(ctx, repo, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, repo, "commit", "-qm", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(canary); err == nil {
		t.Fatal("repository hook executed on the host")
	}
}

// TestAdminDirTamperingDetected plants, through the agent-writable admin dir,
// a crafted common dir whose config defines a clean filter. The attack is
// real (host `git add` runs the filter), and CheckWorktree must refuse it.
func TestAdminDirTamperingDetected(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	info, _ := Inspect(ctx, repo)
	wt := filepath.Join(t.TempDir(), "wt")
	if err := EnsureWorktree(ctx, repo, wt, TaskBranch("t3"), info.Head); err != nil {
		t.Fatal(err)
	}
	common, _ := CommonDir(ctx, repo)
	admin, _ := AdminDir(ctx, wt)

	// Build an evil common dir: a copy of the real one plus a filter driver.
	evil := filepath.Join(wt, ".cache", "evil")
	_ = os.MkdirAll(filepath.Dir(evil), 0o755)
	if out, err := exec.Command("cp", "-a", common, evil).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v %s", err, out)
	}
	canary := filepath.Join(t.TempDir(), "pwned")
	cfg, _ := os.ReadFile(filepath.Join(evil, "config"))
	cfg = append(cfg, []byte("[filter \"x\"]\n\tclean = touch "+canary+"; cat\n")...)
	_ = os.WriteFile(filepath.Join(evil, "config"), cfg, 0o644)
	_ = os.WriteFile(filepath.Join(wt, ".gitattributes"), []byte("*.go filter=x\n"), 0o644)
	_ = os.WriteFile(filepath.Join(wt, "z.go"), []byte("package a\n"), 0o644)
	orig, _ := os.ReadFile(filepath.Join(admin, "commondir"))
	_ = os.WriteFile(filepath.Join(admin, "commondir"), []byte(evil+"\n"), 0o644)

	if err := CheckWorktree(wt, common); err == nil {
		t.Fatal("redirected commondir accepted")
	}
	// Demonstrate the attack the check prevents.
	_, _ = Run(ctx, wt, "add", "-A")
	if _, err := os.Stat(canary); err != nil {
		t.Log("note: host git did not run the planted filter on this git version")
	}
	_ = os.WriteFile(filepath.Join(admin, "commondir"), orig, 0o644)
	if err := CheckTaskWorktree(wt, common, TaskBranch("t3")); err != nil {
		t.Fatalf("restored worktree rejected: %v", err)
	}

	// Per-worktree config is refused.
	_ = os.WriteFile(filepath.Join(admin, "config.worktree"), []byte("[core]\n\tfsmonitor = true\n"), 0o644)
	if err := CheckWorktree(wt, common); err == nil {
		t.Fatal("config.worktree accepted")
	}
	_ = os.Remove(filepath.Join(admin, "config.worktree"))

	// HEAD moved to another branch is refused.
	_ = os.WriteFile(filepath.Join(admin, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644)
	if err := CheckTaskWorktree(wt, common, TaskBranch("t3")); err == nil {
		t.Fatal("HEAD on another branch accepted")
	}
	if err := EnsureWorktree(ctx, repo, wt, TaskBranch("t3"), info.Head); err == nil {
		t.Fatal("EnsureWorktree reused a tampered worktree")
	}
}

func TestChangedSymbols(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	src := "package a\n\ntype Ledger struct{}\n\nfunc (l *Ledger) Post(x int) int {\n\ty := x\n\treturn y\n}\n\nfunc Keep() {}\n"
	_ = os.WriteFile(filepath.Join(repo, "l.go"), []byte(src), 0o644)
	_, _ = Run(ctx, repo, "add", "-A")
	_, _ = Run(ctx, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "l")
	base, _ := Run(ctx, repo, "rev-parse", "HEAD")
	_ = os.WriteFile(filepath.Join(repo, "l.go"), []byte(strings.Replace(src, "y := x", "y := x + 1", 1)+"\nfunc Added() {}\n"), 0o644)
	_ = os.WriteFile(filepath.Join(repo, "svc.ts"), []byte("export class PaymentService {\n  charge(id: string): void {\n  }\n}\n"), 0o644)
	got, err := ChangedSymbols(ctx, repo, base)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"l.go:Post", "l.go:Added", "svc.ts:PaymentService", "svc.ts:charge"}
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("missing %s in %v", w, got)
		}
	}
	if slices.Contains(got, "l.go:Keep") {
		t.Errorf("untouched symbol reported: %v", got)
	}
}
