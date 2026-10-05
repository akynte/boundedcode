package verify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/sandbox"
)

// TestVerificationUndoesItsSideEffects is the regression test for the
// 2026-10-04 axios rerun: the full-scope `npm run build` stage rewrote 12
// tracked dist/ files in the task worktree, so the candidate no longer
// matched its commit and a retry would have committed the residue.
func TestVerificationUndoesItsSideEffects(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	writeFiles(t, repo, map[string]string{
		ConfigPath: `version: 1
stages:
  - name: build
    run: ["sh", "-c", "echo rebuilt > dist/bundle.js && echo new > out.tmp && mkdir -p build && echo x > build/o && echo verifier > notes.txt"]
`,
		"dist/bundle.js": "original\n",
		"notes.txt":      "committed\n",
		".gitignore":     "build/\n",
		"main.txt":       "code\n",
	})
	for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "base"}} {
		if _, err := gitops.Run(ctx, repo, a...); err != nil {
			t.Fatal(err)
		}
	}
	base, _ := gitops.Run(ctx, repo, "rev-parse", "HEAD")
	// Uncommitted work present before verification must survive as it was.
	writeFiles(t, repo, map[string]string{"notes.txt": "agent edit\n", "agent.tmp": "agent\n"})

	e := &Engine{Sandbox: sandbox.None{}, Gitleaks: fakeGitleaks(t)}
	res, err := e.Run(ctx, RepoTarget{Name: "r", Worktree: repo, Base: base, TaskID: "t"}, Targeted)
	if err != nil {
		t.Fatal(err)
	}
	read := func(p string) string { b, _ := os.ReadFile(filepath.Join(repo, p)); return string(b) }
	if got := read("dist/bundle.js"); got != "original\n" {
		t.Errorf("tracked file not restored: %q", got)
	}
	if got := read("notes.txt"); got != "agent edit\n" {
		t.Errorf("pre-verification edit not restored: %q", got)
	}
	if read("agent.tmp") != "agent\n" {
		t.Error("an untracked file that existed before verification was removed")
	}
	if _, err := os.Stat(filepath.Join(repo, "out.tmp")); !os.IsNotExist(err) {
		t.Error("untracked file created by verification was kept")
	}
	if read("build/o") != "x\n" {
		t.Error("ignored output should be left alone")
	}
	var se *StageResult
	for i := range res.Stages {
		if res.Stages[i].Name == "side-effects" {
			se = &res.Stages[i]
		}
	}
	if se == nil || se.Status != "pass" || !strings.Contains(se.Output, "dist/bundle.js") || !strings.Contains(se.Output, "out.tmp") {
		t.Fatalf("side effects not reported: %+v", res.Stages)
	}
	if !res.Passed {
		t.Fatalf("verification should pass: %+v", res.Failures())
	}
}
