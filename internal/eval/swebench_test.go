package eval_test

// What SWE-bench brings that nothing else in this set does.
//
// Every other task here was built from a fixture somebody in this project
// wrote or extracted, and the invariants that protect them — the hidden
// acceptance, the leak scan, the base digest — are checked by the harness
// itself. An imported SWE-bench instance adds three routes those checks do
// not cover, because they are properties of the *import* rather than of the
// task format:
//
//  1. The fixture is an upstream checkout, and nothing in the task file can
//     show that it is the pre-fix tree rather than a later one. The official
//     gold patch is the only witness: it applies to the pre-fix tree and does
//     not apply to a tree that already has the fix.
//  2. The hidden test comes from the instance's test_patch rather than from a
//     file somebody wrote, so "the acceptance file is not in the fixture" is
//     not the same statement as "the test is not in the fixture" — the patch
//     edits a file the fixture already contains.
//  3. The objective is the instance's problem_statement, and the whole value
//     of importing a public instance is that the objective was not rewritten.
//     A paraphrase would make the score incomparable to a published one while
//     still looking fine.
//
// Everything else about these tasks is checked by the shared task tests.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/eval"
)

const swebenchSpec = ".bc-swebench/instance.json"

// swebenchInstance is the official row the acceptance key carries.
type swebenchInstance struct {
	InstanceID       string   `json:"instance_id"`
	Repo             string   `json:"repo"`
	BaseCommit       string   `json:"base_commit"`
	Image            string   `json:"image"`
	Patch            string   `json:"patch"`
	TestPatch        string   `json:"test_patch"`
	ProblemStatement string   `json:"problem_statement"`
	FailToPass       []string `json:"FAIL_TO_PASS"`
	PassToPass       []string `json:"PASS_TO_PASS"`
}

// swebenchTasks returns the imported tasks together with their official rows.
func swebenchTasks(t *testing.T) map[string]struct {
	Task eval.Task
	Spec swebenchInstance
} {
	t.Helper()
	out := map[string]struct {
		Task eval.Task
		Spec swebenchInstance
	}{}
	for _, task := range loadSet(t) {
		body, ok := task.Acceptance.Files[swebenchSpec]
		if !ok {
			continue
		}
		var spec swebenchInstance
		if err := json.Unmarshal([]byte(body), &spec); err != nil {
			t.Fatalf("%s: %s does not parse: %v", task.ID, swebenchSpec, err)
		}
		out[task.ID] = struct {
			Task eval.Task
			Spec swebenchInstance
		}{task, spec}
	}
	if len(out) == 0 {
		t.Skip("no SWE-bench tasks in the set")
	}
	return out
}

// fixtureOrSkip resolves a fixture that materialize.sh may not have produced
// on this machine. The fixtures are 110 MB of upstream checkouts and are not
// committed, so their absence is an ordinary state and not a failure — but a
// present-and-wrong fixture must still fail loudly, which is why the skip is
// on existence alone.
func fixtureOrSkip(t *testing.T, task eval.Task) string {
	t.Helper()
	dir := task.FixturePath()
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Skipf("%s: fixture not materialized; run evals/tasks/swebench/materialize.sh", task.ID)
	}
	return dir
}

// The fixture must be the pre-fix tree, and the gold patch is the witness.
//
// Forward-applying proves the fixture is the state the patch was written
// against — a later tree would reject the context. Reverse-applying proves
// the fix is not already there: on a fixed tree the reverse would succeed,
// and a task whose starting state already contains its own answer measures
// nothing at all.
//
// The patch is applied in a temporary tree holding only the files it touches,
// not in the fixture. The fixture is gitignored — it is 110 MB this project
// does not own — and `git apply` silently *skips* a patch whose target is
// ignored, reporting success. Both directions then pass and the test asserts
// nothing, which is how it behaved when it was first written.
func TestSWEBenchFixtureIsThePreFixTree(t *testing.T) {
	for id, entry := range swebenchTasks(t) {
		t.Run(id, func(t *testing.T) {
			dir := fixtureOrSkip(t, entry.Task)
			targets := patchTargets(entry.Spec.Patch)
			if len(targets) == 0 {
				t.Fatal("the gold patch names no files, so it cannot witness anything")
			}
			tree := copyOut(t, dir, targets)
			patch := filepath.Join(t.TempDir(), "gold.patch")
			if err := os.WriteFile(patch, []byte(entry.Spec.Patch), 0o600); err != nil {
				t.Fatal(err)
			}
			fwd, fwdOut := gitApplyCheck(t, tree, patch)
			if !fwd {
				t.Errorf("the official gold patch does not apply to the fixture, so the "+
					"fixture is not the tree the instance was taken from:\n%s", fwdOut)
			}
			rev, revOut := gitApplyCheck(t, tree, patch, "-R")
			if rev {
				t.Errorf("the official gold patch reverse-applies to the fixture, which "+
					"means the fix is already in the starting state:\n%s", revOut)
			}
		})
	}
}

// patchTargets lists the repository-relative files a unified diff touches.
func patchTargets(patch string) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(patch, "\n") {
		if !strings.HasPrefix(line, "--- a/") {
			continue
		}
		p := strings.TrimSpace(strings.TrimPrefix(line, "--- a/"))
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// copyOut reproduces the named files in a temporary tree outside any
// repository, so git applies to them rather than ignoring them.
func copyOut(t *testing.T, fixture string, paths []string) string {
	t.Helper()
	tree := t.TempDir()
	for _, rel := range paths {
		body, err := os.ReadFile(filepath.Join(fixture, filepath.FromSlash(rel))) //nolint:gosec // a path from the task's own acceptance key
		if err != nil {
			t.Fatalf("the gold patch names %s, which the fixture does not have: %v", rel, err)
		}
		dst := filepath.Join(tree, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return tree
}

// gitApplyCheck reports whether the patch applies, and refuses to call a skip
// a success.
func gitApplyCheck(t *testing.T, dir, patch string, extra ...string) (bool, string) {
	t.Helper()
	args := append([]string{"apply", "--check", "-v"}, extra...)
	cmd := exec.Command("git", append(args, patch)...) //nolint:gosec // fixed arguments and a path this test wrote
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if strings.Contains(string(out), "Skipped patch") {
		t.Fatalf("git skipped the patch instead of checking it, so this proves nothing:\n%s", out)
	}
	return err == nil, strings.TrimSpace(string(out))
}

// The hidden test must not be in the fixture.
//
// The acceptance key is a patch against a file the fixture already contains,
// so the harness's "acceptance files are not in the fixture" check says
// nothing here: the file is there, and what must be absent is the test the
// patch adds. Checked block by block rather than line by line — a single
// added line can be `    return None`, which occurs everywhere and would make
// this assert nothing.
func TestSWEBenchHiddenTestIsAbsentFromTheFixture(t *testing.T) {
	for id, entry := range swebenchTasks(t) {
		t.Run(id, func(t *testing.T) {
			dir := fixtureOrSkip(t, entry.Task)
			blocks := addedBlocks(entry.Spec.TestPatch)
			if len(blocks) == 0 {
				t.Fatal("the test patch adds no multi-line block, so this test cannot show " +
					"the hidden test is absent")
			}
			for _, target := range patchTargets(entry.Spec.TestPatch) {
				body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(target))) //nolint:gosec // a path from the task's own acceptance key
				if err != nil {
					// The test patch may create a file that does not exist
					// yet, which is the strongest form of absent.
					continue
				}
				for _, block := range blocks {
					if strings.Contains(string(body), block) {
						t.Errorf("%s already contains a block the hidden test adds, so the "+
							"model can read the test it will be graded on:\n%s", target, block)
					}
				}
			}
		})
	}
}

// addedBlocks returns the runs of consecutive added lines in a unified diff,
// each as the text it would insert.
func addedBlocks(patch string) []string {
	var blocks []string
	var run []string
	flush := func() {
		if len(run) >= 2 {
			block := strings.Join(run, "\n")
			if len(strings.TrimSpace(block)) >= 20 {
				blocks = append(blocks, block)
			}
		}
		run = nil
	}
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"):
			flush()
		case strings.HasPrefix(line, "+"):
			run = append(run, line[1:])
		default:
			flush()
		}
	}
	flush()
	return blocks
}

// The objective must be the official problem statement, byte for byte.
//
// This is the whole reason to import a public instance rather than write a
// task about the same bug: a score is comparable to a published one only if
// the system was asked the published question. A rewrite that clarified the
// objective would raise the score and quietly stop measuring SWE-bench.
func TestSWEBenchObjectiveIsTheOfficialProblemStatement(t *testing.T) {
	for id, entry := range swebenchTasks(t) {
		t.Run(id, func(t *testing.T) {
			if entry.Task.Objective != entry.Spec.ProblemStatement {
				t.Errorf("the objective is not the instance's problem_statement verbatim; "+
					"objective is %d bytes, problem_statement is %d",
					len(entry.Task.Objective), len(entry.Spec.ProblemStatement))
			}
		})
	}
}

// An imported instance carries no localization ground truth.
//
// The gold patch is in the acceptance key, and it would be one line of code
// to turn its file list into `expected.files`. That would not be ground
// truth: it is where one engineer made the change, not the set of files an
// engineer must read, and scoring retrieval against it would reward a
// retriever for guessing the diff. Labels come from the annotation workflow
// or they do not exist. The same argument bars `scope`, which is derived from
// the same list and is visible to the solver.
func TestSWEBenchTasksCarryNoLocalizationLabels(t *testing.T) {
	for id, entry := range swebenchTasks(t) {
		t.Run(id, func(t *testing.T) {
			if !entry.Task.Expected.Empty() {
				t.Error("the task declares expected localization labels; an imported instance " +
					"has none until an annotator produces them")
			}
			if len(entry.Task.Scope) > 0 {
				t.Errorf("the task declares scope %v, which is the gold patch's file list "+
					"handed to the solver", entry.Task.Scope)
			}
		})
	}
}

// Provenance is disclosed, not assumed.
func TestSWEBenchTasksDiscloseTheirLeakRisk(t *testing.T) {
	for id, entry := range swebenchTasks(t) {
		t.Run(id, func(t *testing.T) {
			if entry.Task.LeakRisk != eval.LeakPublic {
				t.Errorf("leak_risk is %q; an instance from a public dataset over a public "+
					"repository is %q, and a score over it is an upper bound",
					entry.Task.LeakRisk, eval.LeakPublic)
			}
			if entry.Task.Membership() != eval.SetDev {
				t.Errorf("set is %q; a task this likely to be in training data does not belong "+
					"in a held-out headline number", entry.Task.Membership())
			}
		})
	}
}

// The reference solution for an imported instance is its own gold patch.
//
// TestKnownGoodSolutionsPass demands one per task, and for these three it is
// not something to write: the official patch is what really resolved the
// issue upstream, and running it through the whole path — worktree, patch
// extraction in the official image, the official grader — is the only way to
// show that this import decides the instance the way SWE-bench decides it.
//
// It costs a container per task, so the precondition below skips rather than
// fails where docker or the image is missing. An absent runtime is not
// evidence that the task is broken; a present runtime that says "unresolved"
// is, and that still fails.
func swebenchGoldSolver(id string) eval.Solver {
	return solverFunc(func(_ context.Context, req eval.SolveRequest) (eval.SolveResult, error) {
		body, ok := req.Task.Acceptance.Files[swebenchSpec]
		if !ok {
			return eval.SolveResult{}, fmt.Errorf("%s: no %s in the acceptance key", id, swebenchSpec)
		}
		var spec swebenchInstance
		if err := json.Unmarshal([]byte(body), &spec); err != nil {
			return eval.SolveResult{}, err
		}
		patch := filepath.Join(req.Worktree, ".bc-gold.patch")
		if err := os.WriteFile(patch, []byte(spec.Patch), 0o600); err != nil {
			return eval.SolveResult{}, err
		}
		cmd := exec.Command("git", "apply", "-p1", patch) //nolint:gosec // a path this test wrote
		cmd.Dir = req.Worktree
		if out, err := cmd.CombinedOutput(); err != nil {
			return eval.SolveResult{}, fmt.Errorf("applying the gold patch: %s", out)
		}
		if err := os.Remove(patch); err != nil {
			return eval.SolveResult{}, err
		}
		return eval.SolveResult{Claimed: true, Attempts: 1}, nil
	})
}

// swebenchRuntime skips when the official evaluation runtime is not here.
func swebenchRuntime(t *testing.T, task eval.Task) {
	t.Helper()
	if _, err := os.Stat(task.FixturePath()); err != nil {
		t.Skipf("fixture not materialized; run evals/tasks/swebench/materialize.sh")
	}
	if exec.Command("docker", "info").Run() != nil {
		t.Skip("no docker daemon; the official SWE-bench image cannot be run")
	}
	var spec swebenchInstance
	if err := json.Unmarshal([]byte(task.Acceptance.Files[swebenchSpec]), &spec); err != nil {
		t.Fatal(err)
	}
	if exec.Command("docker", "image", "inspect", spec.Image).Run() != nil { //nolint:gosec // a name from the task's own acceptance key
		t.Skipf("the official image %s is not present; `docker pull %s`", spec.Image, spec.Image)
	}
	if err := exec.Command(swebenchPython(), "-c", "import swebench").Run(); err != nil { //nolint:gosec // an operator-supplied interpreter path
		t.Skipf("%s cannot import the official swebench harness", swebenchPython())
	}
}

func swebenchPython() string {
	if p := os.Getenv("BC_SWEBENCH_PYTHON"); p != "" {
		return p
	}
	return "python3"
}
