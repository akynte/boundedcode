package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/repointel"
	"github.com/akynte/boundedcode/internal/task"
)

type indexRecorder struct {
	repointel.Intelligence
	indexed []string
}

func (f *indexRecorder) Index(_ context.Context, path, name, _ string) (repointel.IndexResult, error) {
	f.indexed = append(f.indexed, path)
	return repointel.IndexResult{Project: name}, nil
}

// TestPackWorktreesUsesGitChanges: after an interrupted attempt the ledger
// lists no changed files yet, but the worktree has changes; the pack must
// describe the worktree (its own index project), not the primary checkout.
func TestPackWorktreesUsesGitChanges(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "b"}} {
		if _, err := gitops.Run(ctx, repo, a...); err != nil {
			t.Fatal(err)
		}
	}
	base, _ := gitops.Run(ctx, repo, "rev-parse", "HEAD")
	intel := &indexRecorder{}
	r := &Runner{Intel: intel, Paths: config.Paths{Data: t.TempDir()}}
	wts := []task.Worktree{{RepoName: "a", Path: repo, BaseCommit: base, IndexProject: "primary"}}
	tk := &task.Task{ID: "t1"} // ledger: no changed files recorded

	if out := r.packWorktrees(ctx, tk, wts); out[0].IndexProject != "primary" || len(intel.indexed) != 0 {
		t.Fatalf("unchanged worktree re-indexed: %+v", out)
	}
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n\nfunc F() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := r.packWorktrees(ctx, tk, wts)
	if out[0].IndexProject != WorktreeProject("t1", "a") || len(intel.indexed) != 1 {
		t.Fatalf("changed worktree not indexed as its own project: %+v", out)
	}
}
