package task_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/analyzers/golang"
	"github.com/akynte/boundedcode/internal/engine"
	"github.com/akynte/boundedcode/internal/index"
	"github.com/akynte/boundedcode/internal/policy"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/workspace"
)

// filesEngine writes several files in one step, standing in for an edit that
// touches code and tests together.
type filesEngine struct{ files map[string]string }

func (e *filesEngine) Name() string                 { return "files-test-engine" }
func (e *filesEngine) Health(context.Context) error { return nil }
func (e *filesEngine) Close() error                 { return nil }
func (e *filesEngine) Step(_ context.Context, req engine.Request) (*engine.Response, error) {
	for rel, body := range e.files {
		p := filepath.Join(req.Worktree, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			return nil, err
		}
	}
	return &engine.Response{Summary: "edited", ClaimsDone: true}, nil
}

func runFilesTask(t *testing.T, repo string, files map[string]string, setup func(*task.Runner, *store.Store)) *task.Outcome {
	t.Helper()
	r, st := newRunner(t, &filesEngine{files: files})
	if setup != nil {
		setup(r, st)
	}
	id := task.NewID("t")
	if err := task.NewStore(st).Create(context.Background(), task.Task{
		ID: id, Title: "change Add", Verification: recipe.Standard,
		Budget: task.Budget{MaxAttempts: 1, MaxWallTime: 5 * time.Minute},
	}); err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(context.Background(), id, repo)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func impactResult(out *task.Outcome) (recipe.Result, bool) {
	for _, res := range out.Results {
		if res.Recipe == task.ImpactRecipe {
			return res, true
		}
	}
	return recipe.Result{}, false
}

const addTest = "package a\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(2, 3) != 5 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n"

// The attack the preset cannot see: break the function, and teach the test
// that would catch it to skip. `go test ./...` passes. The impact check runs
// the tests that reach Add by name and sees the skip in a file the task
// changed.
func TestImpactCatchesATestTaughtToSkip(t *testing.T) {
	requireGo(t)
	repo := gitRepo(t, map[string]string{"go.mod": goodModule, "a.go": fixedAdd, "a_test.go": addTest})
	out := runFilesTask(t, repo, map[string]string{
		"a.go": wrongAdd,
		"a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tt.Skip(\"flaky\")\n" +
			"\tif Add(2, 3) != 5 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n",
	}, nil)
	if out.Accepted {
		t.Fatal("a change that silences the test reaching it must not be accepted")
	}
	if !containsSubstr(out.Reasons, "TestAdd reaches a.go Add and now skips") {
		t.Errorf("reasons: %v", out.Reasons)
	}
}

func requirePolicy(t *testing.T, repo, pattern string) func(*task.Runner, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	body := "name: tested\nrequire_tests:\n  - path: " + pattern + "\n    reason: arithmetic is load-bearing\n"
	if err := os.WriteFile(filepath.Join(dir, "p.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	set, err := policy.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return func(r *task.Runner, _ *store.Store) { r.Policies = set }
}

// A policy demanding test evidence for a path is not met by a suite that
// passes without touching the change.
func TestImpactEnforcesRequireTests(t *testing.T) {
	requireGo(t)
	repo := hiddenRepo(t) // Add is broken; the only test exercises nothing
	out := runFilesTask(t, repo, map[string]string{"a.go": fixedAdd}, requirePolicy(t, repo, "a.go"))
	if out.Accepted {
		t.Fatal("a change the policy requires a test for, with no test reaching it, must not be accepted")
	}
	if !containsSubstr(out.Reasons, "policy tested requires a passing test that reaches it") {
		t.Errorf("reasons: %v", out.Reasons)
	}
}

// The graph describes the base commit and cannot see a test the task adds.
// A new test in the same package that calls the changed function counts.
func TestImpactCountsATestTheTaskAdds(t *testing.T) {
	requireGo(t)
	repo := hiddenRepo(t)
	out := runFilesTask(t, repo, map[string]string{"a.go": fixedAdd, "add_test.go": addTest},
		requirePolicy(t, repo, "a.go"))
	if !out.Accepted {
		t.Fatalf("a change with a new passing test reaching it must be accepted: %v", out.Reasons)
	}
	res, ok := impactResult(out)
	if !ok || res.Status != recipe.Pass {
		t.Fatalf("impact result = %+v (present=%v)", res, ok)
	}
}

// Across packages only the graph knows: Total in another package calls Add,
// and TestTotal tests Total. With the repository indexed, TestTotal is found,
// run and credited to the change in calc.
func TestImpactFollowsTheGraphAcrossPackages(t *testing.T) {
	requireGo(t)
	files := map[string]string{
		"go.mod":      goodModule,
		"calc/add.go": "package calc\n\n// Add returns the sum.\nfunc Add(x, y int) int { return x - y }\n",
		"report/report.go": "package report\n\nimport \"example.test/task/calc\"\n\n" +
			"// Total sums the values.\nfunc Total(v []int) int {\n\tt := 0\n\tfor _, x := range v {\n\t\tt = calc.Add(t, x)\n\t}\n\treturn t\n}\n",
		"report/report_test.go": "package report\n\nimport \"testing\"\n\n" +
			"func TestTotal(t *testing.T) {\n\tif Total([]int{1, 2}) != 3 {\n\t\tt.Skip(\"the arithmetic is broken at base\")\n\t}\n}\n",
	}
	repo := gitRepo(t, files)
	fixed := "package calc\n\n// Add returns the sum.\nfunc Add(x, y int) int { return x + y }\n"

	indexed := func(r *task.Runner, st *store.Store) {
		ix := index.New(st, index.Options{Analyzers: []index.Analyzer{golang.New()}})
		ctx := context.Background()
		if err := ix.RegisterRepository(ctx, workspace.Repository{ID: "r", Name: "r", Path: repo, DefaultBranch: "main"}); err != nil {
			t.Fatal(err)
		}
		if _, err := ix.Repository(ctx, "r", repo); err != nil {
			t.Fatal(err)
		}
		requirePolicy(t, repo, "calc/**")(r, st)
	}
	out := runFilesTask(t, repo, map[string]string{"calc/add.go": fixed}, indexed)
	if !out.Accepted {
		t.Fatalf("TestTotal reaches Add through the graph and passes; reasons: %v", out.Reasons)
	}
	res, ok := impactResult(out)
	if !ok {
		t.Fatal("no impact result")
	}
	if !strings.Contains(res.Summary.Headline, "1 reached by a passing test") {
		t.Errorf("headline = %q", res.Summary.Headline)
	}

	// Without the graph the same change has no test the scan can see, and
	// the policy is not met: the graph is what found the evidence.
	out = runFilesTask(t, gitRepo(t, files), map[string]string{"calc/add.go": fixed}, requirePolicy(t, repo, "calc/**"))
	if out.Accepted {
		t.Fatal("without the graph no test reaches Add, so the policy must not be met")
	}
}

// The report a person reads names each declaration, the tests that reach it
// and how each was found.
func TestImpactReportIsStored(t *testing.T) {
	requireGo(t)
	repo := gitRepo(t, map[string]string{"go.mod": goodModule, "a.go": fixedAdd, "a_test.go": addTest})
	var runner *task.Runner
	out := runFilesTask(t, repo, map[string]string{"a.go": "package a\n\nfunc Add(x, y int) int {\n\treturn y + x\n}\n"},
		func(r *task.Runner, _ *store.Store) { runner = r })
	res, ok := impactResult(out)
	if !ok || res.ArtifactHash == "" {
		t.Fatalf("impact result = %+v", res)
	}
	body, err := runner.Artifacts.Get(res.ArtifactHash)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Declarations []struct {
			Declaration string
			Tests       []struct{ Name, Via string }
		}
		Statuses map[string]string
	}
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Declarations) != 1 || report.Declarations[0].Declaration != "a.go Add" ||
		len(report.Declarations[0].Tests) != 1 || report.Declarations[0].Tests[0].Via != "same package" {
		t.Errorf("report = %+v", report)
	}
	// A root-level file's directory is ".".
	if report.Statuses["./TestAdd"] != "pass" {
		t.Errorf("statuses = %v", report.Statuses)
	}
}
