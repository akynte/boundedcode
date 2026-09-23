package eval

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Ground truth is evaluator-only.
//
// If a label reaches retrieval, the judge, the local reranker, the context
// packer, the reasoning model or the repair loop, the benchmark stops
// measuring retrieval and starts measuring how well the harness cheats. The
// failure would not announce itself: recall would rise, every test would
// still pass, and the number would be worthless.
//
// So the property is asserted four ways — by package boundary, by what the
// solver is handed, by what a request carries, and by what is on disk in the
// fixture — because each catches a different way of getting it wrong.

// The packages that must never be able to see an expected label. Ground truth
// lives in eval.Task.Expected and in the annotation files; none of these may
// reach the package that defines them.
var groundTruthFree = []string{
	"github.com/akynte/boundedcode/internal/retrieval",
	"github.com/akynte/boundedcode/internal/judgment",
	"github.com/akynte/boundedcode/internal/contextpack",
	"github.com/akynte/boundedcode/internal/task",
	"github.com/akynte/boundedcode/internal/engine",
	"github.com/akynte/boundedcode/internal/engine/native",
	"github.com/akynte/boundedcode/internal/critic",
	"github.com/akynte/boundedcode/internal/llm",
}

// 1. By package boundary. internal/eval holds the labels, so a pipeline
// package that could import it could read them.
func TestPipelinePackagesCannotReachGroundTruth(t *testing.T) {
	const evalPkg = "github.com/akynte/boundedcode/internal/eval"
	for _, pkg := range groundTruthFree {
		t.Run(pkg, func(t *testing.T) {
			out, err := exec.Command("go", "list", "-deps", pkg).Output()
			if err != nil {
				t.Fatalf("go list -deps %s: %v", pkg, err)
			}
			for _, line := range strings.Split(string(out), "\n") {
				if strings.TrimSpace(line) == evalPkg {
					t.Fatalf("%s reaches %s, which defines Task.Expected and loads the "+
						"annotation files. A pipeline package that can read the labels can "+
						"be made to use them, and recall would then measure the harness "+
						"cheating rather than retrieval working.", pkg, evalPkg)
				}
			}
		})
	}
}

// 2. By what the solver is handed. SolveRequest is the whole surface between
// the harness and the system under test, so a label can only cross there.
func TestSolveRequestCarriesNoGroundTruth(t *testing.T) {
	// Task travels into the solver, and Task.Expected is where labels live.
	// It must be empty by the time it gets there: ApplyAnnotations fills it
	// on the evaluator side, and Runner.Run is responsible for stripping it.
	full := Task{
		ID:        "t1",
		Objective: "make the limit configurable",
		Expected: Expected{
			Files: []string{"internal/billing/limit.go"}, Useful: []string{"internal/billing/doc.go"},
			Symbols: []string{"DailyLimit"}, Tests: []string{"TestDailyLimit"},
			RubricVersion: AnnotationRubricVersion, Annotator: "someone",
		},
	}
	stripped := full.WithoutGroundTruth()

	if !stripped.Expected.Empty() {
		t.Fatalf("WithoutGroundTruth left labels behind: %+v", stripped.Expected)
	}
	if stripped.ID != full.ID || stripped.Objective != full.Objective {
		t.Fatal("stripping the labels altered the problem statement")
	}
	// And the original is untouched, because the evaluator still needs it.
	if len(full.Expected.Files) != 1 {
		t.Fatal("stripping mutated the caller's task; the score would have nothing to " +
			"compare against")
	}

	// Serialised, which is how it would actually leak: a struct that marshals
	// its labels into a prompt or an artifact handed to the model.
	body, err := json.Marshal(struct {
		Task Task `json:"task"`
	}{stripped})
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"limit.go", "DailyLimit", "TestDailyLimit", "someone",
		"expected", "useful", "annotator"} {
		if strings.Contains(strings.ToLower(string(body)), strings.ToLower(leak)) {
			t.Fatalf("a stripped task still serialises %q:\n%s", leak, body)
		}
	}
}

// 3. By what is on disk where the model can look. The annotation files live
// beside the task definitions, and a task's fixture is the directory copied
// into the worktree the model edits. The two must not overlap.
func TestAnnotationsLiveOutsideEveryFixture(t *testing.T) {
	root := moduleRootForTest(t)
	taskDir := filepath.Join(root, "evals", "tasks")
	tasks, err := LoadSet(taskDir)
	if err != nil {
		t.Skipf("no task set: %v", err)
	}
	annotations, err := filepath.Abs(filepath.Join(taskDir, AnnotationDir))
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		fixture, err := filepath.Abs(task.FixturePath())
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(annotations, fixture+string(filepath.Separator)) {
			t.Fatalf("%s: the annotation directory is inside the fixture at %s, so the "+
				"labels would be copied into the worktree the model edits", task.ID, fixture)
		}
		// And nothing that looks like an annotation is already in there.
		err = filepath.WalkDir(fixture, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil || d.IsDir() {
				return nil //nolint:nilerr // an unreadable entry is not a finding
			}
			if strings.Contains(d.Name(), ".annotation.") {
				t.Errorf("%s: %s is inside the fixture", task.ID, path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// 4. By what a judgment request can carry. The state a question is asked
// about is built from retrieval candidates; no field on it is fed from the
// evaluator, and this asserts the type has no door that could be.
func TestJudgmentStateHasNoGroundTruthDoor(t *testing.T) {
	root := moduleRootForTest(t)
	path := filepath.Join(root, "internal", "judgment", "state.go")
	body, err := os.ReadFile(path) //nolint:gosec // a path this test constructed
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, body, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || !ast.IsExported(fn.Name.Name) {
			return true
		}
		for _, banned := range []string{"Expected", "GroundTruth", "Label", "Annotation"} {
			if strings.Contains(fn.Name.Name, banned) {
				t.Errorf("judgment.State has an exported %s method; the state a question "+
					"is asked about must be built from candidates, never from labels",
					fn.Name.Name)
			}
		}
		return true
	})
}

// The evaluator does still need the labels, so a test that passed by deleting
// them would be worse than none. This pins the other direction.
func TestEvaluatorStillSeesGroundTruth(t *testing.T) {
	expected := Expected{Files: []string{"a.go"}}
	score := ScoreLocalization(expected, []string{"a.go"}, []string{"a.go"})
	if !score.Scored() || score.Recall != 1 {
		t.Fatalf("the evaluator cannot score with labels present: %+v", score)
	}
}

func moduleRootForTest(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for range 6 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Skip("module root not found")
	return ""
}

// The stripping must happen at the boundary, not merely be available there.
// A recording solver captures exactly what the system under test was given.
func TestRunnerStripsGroundTruthBeforeSolving(t *testing.T) {
	dir := t.TempDir()
	fixture := filepath.Join(dir, "fixture")
	if err := os.MkdirAll(fixture, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "main.go"),
		[]byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	task := Task{
		ID: "leak-1", Objective: "do the thing", Fixture: fixture,
		Category: CategoryBugFix, LeakRisk: LeakNone,
		Acceptance: Acceptance{Argv: []string{"true"}, TimeoutSeconds: 10},
		Budget:     Budget{MaxAttempts: 1, MaxWallSeconds: 30},
		Expected: Expected{
			Files: []string{"internal/secret-answer.go"}, Symbols: []string{"TheAnswer"},
			RubricVersion: AnnotationRubricVersion, Annotator: "annotator@example.test",
		},
	}
	spy := &recordingSolver{}
	runner := &Runner{WorkDir: filepath.Join(dir, "work")}
	_ = runner.Run(context.Background(), task, Arm{Name: "unsupervised"}, spy)

	if spy.seen == nil {
		t.Fatal("the solver was never called; this test proves nothing")
	}
	if !spy.seen.Expected.Empty() {
		t.Fatalf("the solver was handed ground truth: %+v", spy.seen.Expected)
	}
	if spy.seen.Objective != task.Objective {
		t.Fatal("stripping altered the problem statement")
	}
	if len(task.Expected.Files) != 1 {
		t.Fatal("the evaluator's own copy was mutated; nothing could be scored afterwards")
	}
}

type recordingSolver struct{ seen *Task }

func (r *recordingSolver) Solve(_ context.Context, req SolveRequest) (SolveResult, error) {
	t := req.Task
	r.seen = &t
	return SolveResult{}, nil
}
