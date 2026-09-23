package task_test

// A budget that runs out is a reason to stop asking the model, not a reason
// to throw away what it already wrote.
//
// pytest-5631 produced the correct one-line fix and the official SWE-bench
// grader called it RESOLVED, while this system recorded a failure — because
// the model had not announced completion before its last attempt expired.
// The completion contract has never rested on the model saying it is done.
// It rests on the verifier, and an unverified worktree with real changes in
// it is a question nobody asked.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/engine"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/workflow"
)

// silentEditor writes what it is told to and never claims completion, which
// is exactly the shape of the run that was lost.
type silentEditor struct {
	engine.Verify
	body  string
	calls int
}

func (*silentEditor) Edits() bool { return true }
func (e *silentEditor) Step(_ context.Context, req engine.Request) (*engine.Response, error) {
	e.calls++
	if req.Phase != workflow.Edit || req.Plan == nil {
		panic("missing validated plan")
	}
	if e.body == "" {
		// Budget spent reading, nothing written.
		return &engine.Response{Summary: "out of steps"}, nil
	}
	if err := req.Access.Check(req.Worktree, "a.go", true); err != nil {
		return nil, err
	}
	err := os.WriteFile(filepath.Join(req.Worktree, "a.go"), []byte(e.body), 0o644)
	// Edited, but never ClaimsDone: the budget went before the announcement.
	return &engine.Response{Edited: true, Summary: "budget exhausted"}, err
}

func runBudgeted(t *testing.T, body string) (*task.Outcome, *workflow.State) {
	t.Helper()
	requireGo(t)
	repo := gitRepo(t, map[string]string{
		"go.mod": goodModule,
		"a.go":   "package a\n\nfunc Add(x, y int) int { return x - y }\n",
		"a_test.go": "package a\n\nimport \"testing\"\n\n" +
			"func TestAdd(t *testing.T) {\n\tif Add(2, 2) != 4 {\n\t\tt.Fatal(\"wrong\")\n\t}\n}\n",
	})
	editor := &silentEditor{body: body}
	r, st := newRunner(t, editor)
	r.WorkflowModel = &phaseModel{accept: true}

	ctx := context.Background()
	id := task.NewID("budget")
	if err := task.NewStore(st).Create(ctx, task.Task{
		ID: id, Title: "correct addition", Verification: recipe.Standard,
		Budget: task.Budget{MaxAttempts: 1},
	}); err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(ctx, id, repo)
	if err != nil {
		t.Fatal(err)
	}
	s, err := task.NewStore(st).LoadWorkflow(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return out, s
}

// Budget expires with nothing written: the old behaviour, unchanged.
func TestBudgetExhaustedWithNoPatchStillFails(t *testing.T) {
	out, s := runBudgeted(t, "")
	if out.Accepted {
		t.Fatal("a task that changed nothing was accepted")
	}
	if s.Budgeted {
		t.Error("an empty worktree was sent to verification")
	}
	// Either exhaustion path is fine; what matters is that an empty
	// worktree is still a failure and verification was never invited.
	joined := strings.Join(out.Reasons, "; ")
	if !strings.Contains(joined, "budget") && !strings.Contains(joined, "without declaring completion") {
		t.Errorf("the reason changed: %v", out.Reasons)
	}
}

// Budget expires with a wrong patch: verification runs and refuses it. The
// model never said it was done, and it is still not accepted.
func TestBudgetExhaustedWithBadPatchFailsVerification(t *testing.T) {
	out, s := runBudgeted(t, "package a\n\nfunc Add(x, y int) int { return x + y + 1 }\n")
	if out.Accepted {
		t.Fatal("a patch that fails the tests was accepted")
	}
	if !s.Budgeted {
		t.Error("the changed worktree was not verified before failing")
	}
	joined := strings.Join(out.Reasons, "; ")
	if !strings.Contains(joined, "does not verify") {
		t.Errorf("the failure does not say verification refused it: %v", out.Reasons)
	}
}

// Budget expires with a correct patch: verification passes and the task is
// accepted on deterministic evidence alone.
func TestBudgetExhaustedWithValidPatchIsAccepted(t *testing.T) {
	out, s := runBudgeted(t, "package a\n\nfunc Add(x, y int) int { return x + y }\n")
	if !s.Budgeted {
		t.Fatal("the changed worktree was not verified before failing")
	}
	if !out.Accepted {
		t.Fatalf("a correct, verified patch was discarded because the model never said "+
			"it had finished: %v", out.Reasons)
	}
	// Review still ran: an exhausted edit budget stops more editing, not the
	// independent check that stands between a verified change and the
	// branch. workflow.Transition refuses VERIFY -> FINALIZE for that reason.
	if s.Verdict == nil {
		t.Error("the change was finalized without an independent review")
	}
	if len(s.Results) == 0 {
		t.Error("the task was accepted without recorded verification evidence")
	}
}
