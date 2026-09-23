package task_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/engine"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/task"
)

// Verification runs apart from the task's checkout. A check that writes into
// its working directory — here a test that drops a file, then fails so the
// checkout is kept — must leave the task's worktree and its branch untouched.
// Run in the worktree, the file would appear there and be committed to the
// task branch as if the task had written it.
func TestVerificationCannotWriteTheTaskWorktree(t *testing.T) {
	requireGo(t)
	repo := gitRepo(t, map[string]string{
		"go.mod": goodModule,
		"a.go":   "package a\n\nfunc Add(x, y int) int { return x + y }\n",
		"a_test.go": "package a\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\n" +
			"func TestLeak(t *testing.T) {\n" +
			"\t_ = os.WriteFile(\"leak.txt\", []byte(\"written by a check\"), 0o644)\n" +
			"\tt.Fatal(\"fail so the checkout is kept\")\n}\n",
	})

	r, st := newRunner(t, engine.Verify{})
	ctx := context.Background()

	id := task.NewID("t")
	if err := task.NewStore(st).Create(ctx, task.Task{
		ID: id, Title: "verify", Verification: recipe.Standard,
		Budget: task.Budget{MaxAttempts: 1, MaxWallTime: 5 * time.Minute},
	}); err != nil {
		t.Fatal(err)
	}

	out, err := r.Run(ctx, id, repo)
	if err != nil {
		t.Fatal(err)
	}
	if out.Accepted {
		t.Fatal("the failing test must block acceptance")
	}
	if out.Worktree == "" {
		t.Fatal("a failed task keeps its checkout")
	}
	if _, err := os.Stat(filepath.Join(out.Worktree, "leak.txt")); !os.IsNotExist(err) {
		t.Errorf("a file written by a check reached the task's worktree (stat err: %v)", err)
	}
	if out.Branch != "" {
		if err := exec.Command("git", "-C", repo, "cat-file", "-e", out.Branch+":leak.txt").Run(); err == nil {
			t.Errorf("a file written by a check was committed to %s", out.Branch)
		}
	}
}
