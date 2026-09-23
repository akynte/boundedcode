package task_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/oracle"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/task"
)

// The canary is a string only the hidden check contains. If it appears
// anywhere a model could read — a reason, a result headline, a finding, an
// error — the hidden check has leaked.
const canary = "CANARY-hidden-7f3a9c"

const brokenAdd = "package a\n\nfunc Add(x, y int) int { return x - y }\n"
const fixedAdd = "package a\n\nfunc Add(x, y int) int { return x + y }\n"
const wrongAdd = "package a\n\nfunc Add(x, y int) int { return x * y }\n"

// hiddenSuite writes an oracle outside the repository: a hidden Go test for
// Add, and a visible test file it protects.
func hiddenSuite(t *testing.T, extra string) *oracle.Suite {
	t.Helper()
	dir := t.TempDir()
	body := `checks:
  - id: add-sums
    must_not_change: ["a_test.go"]
    files:
      hidden_add_test.go: |
        package a

        import "testing"

        func TestHiddenAdd(t *testing.T) {
        	if Add(2, 3) != 5 {
        		t.Fatalf("` + canary + `: Add(2, 3) = %d", Add(2, 3))
        	}
        }
    argv: ["go", "test", "-run", "TestHiddenAdd", "."]
    timeout_seconds: 120
` + extra
	if err := os.WriteFile(filepath.Join(dir, "checks.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	suite, err := oracle.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return suite
}

func hiddenRepo(t *testing.T) string {
	return gitRepo(t, map[string]string{
		"go.mod": goodModule,
		"a.go":   brokenAdd,
		// A visible test that does not exercise Add, so only the hidden check
		// can tell a right fix from a wrong one.
		"a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestNothing(t *testing.T) {}\n",
	})
}

func runHiddenTask(t *testing.T, repo string, suite *oracle.Suite, path, body string) (*task.Outcome, *task.Runner) {
	t.Helper()
	r, st := newRunner(t, &writingEngine{path: path, body: body})
	r.Oracle = suite
	id := task.NewID("t")
	if err := task.NewStore(st).Create(context.Background(), task.Task{
		ID: id, Title: "make Add add", Verification: recipe.Standard,
		Budget: task.Budget{MaxAttempts: 1, MaxWallTime: 5 * time.Minute},
	}); err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(context.Background(), id, repo)
	if err != nil {
		t.Fatal(err)
	}
	return out, r
}

func hiddenResults(out *task.Outcome) []recipe.Result {
	var hidden []recipe.Result
	for _, res := range out.Results {
		if res.Kind == recipe.KindHidden {
			hidden = append(hidden, res)
		}
	}
	return hidden
}

func assertNoCanary(t *testing.T, out *task.Outcome) {
	t.Helper()
	var visible []string
	visible = append(visible, out.Reasons...)
	for _, res := range out.Results {
		visible = append(visible, res.Err, res.Summary.Headline)
		for _, f := range res.Summary.Findings {
			visible = append(visible, f.File, f.Message, f.Test)
		}
		for name := range res.Summary.Tests {
			visible = append(visible, name)
		}
	}
	for _, s := range visible {
		if strings.Contains(s, canary) || strings.Contains(s, "TestHiddenAdd") {
			t.Errorf("hidden check content reached model-visible output: %q", s)
		}
	}
}

func TestHiddenCheckAcceptsACorrectFix(t *testing.T) {
	requireGo(t)
	out, _ := runHiddenTask(t, hiddenRepo(t), hiddenSuite(t, ""), "a.go", fixedAdd)
	if !out.Accepted {
		t.Fatalf("a fix that passes the hidden check must be accepted: %v", out.Reasons)
	}
	hidden := hiddenResults(out)
	if len(hidden) != 1 || hidden[0].Status != recipe.Pass {
		t.Fatalf("expected one passing hidden result, got %+v", hidden)
	}
}

// Every visible check passes on a wrong fix. Only the hidden check knows, and
// what it knows must not reach the model.
func TestHiddenCheckRejectsAWrongFixWithoutLeaking(t *testing.T) {
	requireGo(t)
	repo := hiddenRepo(t)
	out, r := runHiddenTask(t, repo, hiddenSuite(t, ""), "a.go", wrongAdd)
	if out.Accepted {
		t.Fatal("a fix that fails the hidden check must not be accepted")
	}
	if !containsSubstr(out.Reasons, "hidden acceptance check add-sums did not pass") {
		t.Errorf("the reason must name the check: %v", out.Reasons)
	}
	assertNoCanary(t, out)

	// A person can still read what failed.
	hidden := hiddenResults(out)
	if len(hidden) != 1 || hidden[0].ArtifactHash == "" {
		t.Fatalf("the hidden result must keep its artifact: %+v", hidden)
	}
	raw, err := r.Artifacts.Get(hidden[0].ArtifactHash)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), canary) {
		t.Error("the artifact must hold the full output for a person to read")
	}

	// The hidden test was written into the snapshot, not the task's checkout.
	if out.Worktree == "" {
		t.Fatal("a rejected task keeps its checkout")
	}
	if _, err := os.Stat(filepath.Join(out.Worktree, "hidden_add_test.go")); !os.IsNotExist(err) {
		t.Errorf("the hidden test file reached the task's worktree (stat err: %v)", err)
	}
	if out.Branch != "" {
		if err := exec.Command("git", "-C", repo, "cat-file", "-e", out.Branch+":hidden_add_test.go").Run(); err == nil {
			t.Error("the hidden test file was committed to the task branch")
		}
	}
}

// A candidate that edits the file a check protects has not passed it, even if
// the check's command would succeed.
func TestHiddenCheckRejectsTamperingWithAProtectedPath(t *testing.T) {
	requireGo(t)
	out, _ := runHiddenTask(t, hiddenRepo(t), hiddenSuite(t, ""),
		"a_test.go", "package a\n\nimport \"testing\"\n\nfunc TestNothing(t *testing.T) { t.Log(\"edited\") }\n")
	if out.Accepted {
		t.Fatal("a change to a protected path must not be accepted")
	}
	if !containsSubstr(out.Reasons, "modifies a path it protects") {
		t.Errorf("the reason must say why: %v", out.Reasons)
	}
}

// A check scoped to paths the change does not touch is not required of it.
func TestHiddenCheckAppliesOnlyToItsPaths(t *testing.T) {
	requireGo(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "scoped.yaml"), []byte(`checks:
  - id: elsewhere
    applies_to: ["services/billing/**"]
    argv: ["false"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	suite, err := oracle.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := runHiddenTask(t, hiddenRepo(t), suite, "a.go", fixedAdd)
	if !out.Accepted {
		t.Fatalf("a check for other paths must not block this change: %v", out.Reasons)
	}
	if len(hiddenResults(out)) != 0 {
		t.Error("an inapplicable check must not run")
	}
}

// A hidden check that cannot run is an acceptance criterion nobody has shown
// is met. Unlike a visible check's Error, it blocks.
func TestHiddenCheckThatCannotRunBlocks(t *testing.T) {
	requireGo(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.yaml"), []byte(`checks:
  - id: missing-tool
    argv: ["definitely-not-a-real-binary-for-bcode"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	suite, err := oracle.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := runHiddenTask(t, hiddenRepo(t), suite, "a.go", fixedAdd)
	if out.Accepted {
		t.Fatal("a hidden check that could not run must block acceptance")
	}
	if !containsSubstr(out.Reasons, "hidden acceptance check missing-tool could not complete") {
		t.Errorf("reasons: %v", out.Reasons)
	}
}
