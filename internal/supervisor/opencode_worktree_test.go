package supervisor_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/supervisor"
)

func TestOpenCodeTasksUseIndependentAuthoritativeWorktrees(t *testing.T) {
	ctx := context.Background()
	s := bound(t)
	if output, err := exec.Command("git", "-C", s.Workspace.Root, "add", ".").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
	if output, err := exec.Command("git", "-C", s.Workspace.Root, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "-q", "-m", "base").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}

	first, err := supervisor.StartTask(ctx, s.Store, "first task", recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	second, err := supervisor.StartTask(ctx, s.Store, "second task", recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	firstWT, releaseFirst, err := supervisor.EnsureTaskWorktreeForSession(ctx, s.Store, s.Workspace.Root, first.ID, "ses_first")
	if err != nil {
		t.Fatal(err)
	}
	secondWT, releaseSecond, err := supervisor.EnsureTaskWorktreeForSession(ctx, s.Store, s.Workspace.Root, second.ID, "ses_second")
	if err != nil {
		releaseFirst()
		t.Fatal(err)
	}
	defer releaseFirst()
	defer releaseSecond()
	if firstWT.Path == secondWT.Path || firstWT.Path == s.Workspace.Root || secondWT.Path == s.Workspace.Root {
		t.Fatalf("task worktrees are not independent: first=%s second=%s root=%s", firstWT.Path, secondWT.Path, s.Workspace.Root)
	}
	firstFile := filepath.Join(firstWT.Path, "only-first.txt")
	secondFile := filepath.Join(secondWT.Path, "only-second.txt")
	if err := os.WriteFile(firstFile, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondFile, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(secondWT.Path, "only-first.txt")); !os.IsNotExist(err) {
		t.Fatalf("first task edit leaked into second worktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Workspace.Root, "only-first.txt")); !os.IsNotExist(err) {
		t.Fatalf("task edit leaked into operator checkout: %v", err)
	}
}

func TestOpenCodeTaskLeaseRejectsAConcurrentSession(t *testing.T) {
	ctx := context.Background()
	s := bound(t)
	if output, err := exec.Command("git", "-C", s.Workspace.Root, "add", ".").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
	if output, err := exec.Command("git", "-C", s.Workspace.Root, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "-q", "-m", "base").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}
	taskInfo, err := supervisor.StartTask(ctx, s.Store, "leased task", recipe.Standard)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := supervisor.EnsureTaskWorktreeForSession(ctx, s.Store, s.Workspace.Root, taskInfo.ID, "ses_owner"); err != nil {
		t.Fatal(err)
	}
	release, err := supervisor.AcquireTaskLease(ctx, s.Store, taskInfo.ID, "ses_owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.AcquireTaskLease(ctx, s.Store, taskInfo.ID, "ses_other"); !errors.Is(err, ledger.ErrLeaseHeld) {
		release()
		t.Fatalf("concurrent session lease error = %v, want ErrLeaseHeld", err)
	}
	release()
	if _, err := supervisor.AcquireTaskLease(ctx, s.Store, taskInfo.ID, "ses_other"); err != nil {
		t.Fatalf("expired/released lease could not be acquired: %v", err)
	}
}
