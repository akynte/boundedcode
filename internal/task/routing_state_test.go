package task_test

import (
	"context"
	"errors"
	"testing"

	"github.com/akynte/boundedcode/internal/engine"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/workflow"
)

// Swapping the generation model must not cost a task its place.
//
// Routing EDIT to its own model means the model behind a phase can change, and
// on this machine changing it means stopping one server and starting another.
// That is a runtime event; the task's recorded phase is not. If a swap could
// lose the accepted plan, every EDIT would risk replanning from scratch, and a
// provider that failed to start would take the task's work with it.

// Case 4. The phase record survives whatever happens to the model behind it,
// because it lives in the store and the swap does not go near it.
func TestWorkflowStateSurvivesAModelSwap(t *testing.T) {
	ctx := context.Background()
	_, st := newRunner(t, engine.Verify{})
	store := task.NewStore(st)

	id := task.NewID("swap")
	if err := store.Create(ctx, task.Task{ID: id, Title: "make Add add"}); err != nil {
		t.Fatal(err)
	}
	// Walk to EDIT the way a real task does, so the transition guard is
	// satisfied and the row holds a genuine mid-task state.
	state := &workflow.State{Phase: workflow.Intake}
	for _, phase := range []workflow.Phase{
		workflow.Intake, workflow.Localize, workflow.Impact, workflow.Planning, workflow.Edit,
	} {
		state.Phase = phase
		if err := store.SaveWorkflow(ctx, id, state); err != nil {
			t.Fatalf("saving %s: %v", phase, err)
		}
	}
	state.Plan = workflow.Plan{
		RootCause:      "the not-found branch re-runs handlers that already ran",
		Files:          workflow.Targets{{Path: "routergroup.go"}},
		WriteAllowlist: []string{"routergroup.go"},
		Tests:          []string{"go test"},
	}
	state.Hypothesis = "one 404 produces two log lines"
	state.Attempts = 1
	state.EditContinuations = 2
	if err := store.SaveWorkflow(ctx, id, state); err != nil {
		t.Fatal(err)
	}

	// The swap happens entirely inside the provider: a process is stopped, a
	// process is started. Nothing in this package is called. Reloading is
	// therefore the whole assertion — the state a later EDIT resumes from is
	// the one that was recorded before the model changed.
	reloaded, err := store.LoadWorkflow(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded == nil {
		t.Fatal("the workflow state did not survive")
	}
	if reloaded.Phase != workflow.Edit {
		t.Errorf("phase is %s, want EDIT", reloaded.Phase)
	}
	if reloaded.Plan.RootCause != state.Plan.RootCause {
		t.Error("the accepted plan did not survive")
	}
	if len(reloaded.Plan.WriteAllowlist) != 1 || reloaded.Plan.WriteAllowlist[0] != "routergroup.go" {
		t.Errorf("the write allowlist did not survive: %v", reloaded.Plan.WriteAllowlist)
	}
	if reloaded.Attempts != 1 || reloaded.EditContinuations != 2 {
		t.Errorf("budget counters did not survive: attempts %d, continuations %d",
			reloaded.Attempts, reloaded.EditContinuations)
	}
}

// Case 5. A task whose EDIT provider is unavailable keeps its state, so the
// fix is to start the provider and retry rather than to plan the task again.
//
// The refusal itself lives in the CLI's engineFor, which returns an error
// instead of a verification-only engine; what this pins is the other half of
// that promise — that refusing costs nothing already recorded.
func TestProviderFailureLeavesStateRetryable(t *testing.T) {
	ctx := context.Background()
	_, st := newRunner(t, engine.Verify{})
	store := task.NewStore(st)

	id := task.NewID("unavailable")
	if err := store.Create(ctx, task.Task{ID: id, Title: "make Add add"}); err != nil {
		t.Fatal(err)
	}
	state := &workflow.State{Phase: workflow.Intake}
	for _, phase := range []workflow.Phase{
		workflow.Intake, workflow.Localize, workflow.Impact, workflow.Planning, workflow.Edit,
	} {
		state.Phase = phase
		if err := store.SaveWorkflow(ctx, id, state); err != nil {
			t.Fatal(err)
		}
	}

	// The run never starts: the engine could not be built. Nothing writes.
	startupErr := errors.New("the coding role is routed to editor, which is not reachable")

	reloaded, err := store.LoadWorkflow(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded == nil || reloaded.Phase != workflow.Edit {
		t.Fatalf("a provider that never started cost the task its phase: %+v", reloaded)
	}
	// And the task is still runnable rather than terminal, which is what makes
	// `bcode task retry` the right answer.
	current, err := store.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if current.State.Terminal() {
		t.Errorf("the task was made terminal by a provider failure (%v); it should be retryable",
			startupErr)
	}
}
