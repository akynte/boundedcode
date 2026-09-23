package task_test

// The decision plane is a required runtime component. These pin the product
// behaviour that follows from that, because it is the kind of thing a later
// convenience ("just skip it when there's no judge") would quietly undo.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/engine"
	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/task"
)

// No judge at all: the run is refused before any expensive work, and the error
// says what is missing and which decisions depend on it.
func TestATaskWillNotRunWithoutADecisionPlane(t *testing.T) {
	requireGo(t)
	repo := gitRepo(t, map[string]string{"go.mod": goodModule, "a.go": "package a\n"})

	r, st := newRunner(t, &silentEditor{body: "package a\n"})
	r.WorkflowModel = &phaseModel{accept: true}
	r.Judge = nil // as an installation with no judgment.yaml is assembled

	ctx := context.Background()
	id := task.NewID("t")
	if err := task.NewStore(st).Create(ctx, task.Task{
		ID: id, Title: "verify", Verification: recipe.Low,
		Budget: task.Budget{MaxAttempts: 1, MaxWallTime: time.Minute},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := r.Run(ctx, id, repo)
	if err == nil {
		t.Fatal("a task must not run without a decision plane")
	}
	class, ok := judgment.FailureOf(err)
	if !ok || class != judgment.FailureNotConfigured {
		t.Fatalf("class = %q (ok %v), want %q", class, ok, judgment.FailureNotConfigured)
	}
	// An operator needs to know which decisions just became unavailable.
	for _, site := range judgment.MandatorySites() {
		if !strings.Contains(err.Error(), site) {
			t.Errorf("the refusal does not name %q: %v", site, err)
		}
	}
}

// A judge that exists but is unusable — enabled with no credential — is the
// same refusal, not a quiet downgrade.
func TestATaskWillNotRunWithAnUnusableDecisionPlane(t *testing.T) {
	requireGo(t)
	repo := gitRepo(t, map[string]string{"go.mod": goodModule, "a.go": "package a\n"})

	r, st := newRunner(t, &silentEditor{body: "package a\n"})
	r.WorkflowModel = &phaseModel{accept: true}
	r.Judge = &judgment.Fake{Unavailable: true}

	ctx := context.Background()
	id := task.NewID("t")
	if err := task.NewStore(st).Create(ctx, task.Task{
		ID: id, Title: "verify", Verification: recipe.Low,
		Budget: task.Budget{MaxAttempts: 1, MaxWallTime: time.Minute},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := r.Run(ctx, id, repo); err == nil {
		t.Fatal("an unusable decision plane must refuse the run")
	}
}

// The refusal happens before the task is touched: it is a precondition, not a
// failure of the work, so the task stays runnable once the operator fixes the
// configuration.
func TestARefusedRunLeavesTheTaskRunnable(t *testing.T) {
	requireGo(t)
	repo := gitRepo(t, map[string]string{"go.mod": goodModule, "a.go": "package a\n"})

	r, st := newRunner(t, &silentEditor{body: "package a\n"})
	r.WorkflowModel = &phaseModel{accept: true}
	r.Judge = nil

	ctx := context.Background()
	store := task.NewStore(st)
	id := task.NewID("t")
	if err := store.Create(ctx, task.Task{
		ID: id, Title: "verify", Verification: recipe.Low,
		Budget: task.Budget{MaxAttempts: 1, MaxWallTime: time.Minute},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := r.Run(ctx, id, repo); err == nil {
		t.Fatal("want a refusal")
	}
	after, err := store.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.State.Terminal() {
		t.Fatalf("state = %q; a missing decision plane is the operator's to fix, "+
			"not a terminal outcome for the work", after.State)
	}

	// And with the plane restored, the same task runs.
	r.Judge = &judgment.Fake{}
	if _, err := r.Run(ctx, id, repo); err != nil {
		var re *judgment.RequirementError
		if errors.As(err, &re) {
			t.Fatalf("still refused after the plane was restored: %v", err)
		}
	}
}

// The promotion this system ships with: a routing-capable site whose decision
// cannot be made stops the task rather than assuming the benign answer. This
// is the end-to-end form of judgment.FailClosed, driven through the real phase
// machine with a judge that is present and reachable but answers nothing.
func runWithIntakeTier(t *testing.T, tier judgment.Tier) (*task.Outcome, task.State) {
	t.Helper()
	requireGo(t)
	repo := gitRepo(t, map[string]string{
		"go.mod": goodModule,
		"a.go":   "package a\n\nfunc Add(x, y int) int { return x - y }\n",
	})

	r, st := newRunner(t, &silentEditor{body: "package a\n\nfunc Add(x, y int) int { return x + y }\n"})
	r.WorkflowModel = &phaseModel{accept: true}
	// Config promotes intake_profile to routing by default under strict; the
	// fake is opt-in, so the tier is stated here to match what ships.
	r.Judge = &judgment.Fake{
		Tiers:   map[string]judgment.Tier{task.IntakeSite: tier},
		Answers: map[string]judgment.Answer{}, // reachable, decides nothing
	}

	ctx := context.Background()
	id := task.NewID("t")
	if err := task.NewStore(st).Create(ctx, task.Task{
		ID: id, Title: "correct addition", Verification: recipe.Standard,
		Budget: task.Budget{MaxAttempts: 1},
	}); err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(ctx, id, repo)
	if err != nil {
		t.Fatalf("the task should stop cleanly, not error out: %v", err)
	}
	after, err := task.NewStore(st).Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return out, after.State
}

func TestARoutingSiteThatCannotDecideBlocksTheTask(t *testing.T) {
	out, state := runWithIntakeTier(t, judgment.TierRouting)

	if out.Accepted {
		t.Fatal("a task whose required decision was never made must not be accepted")
	}
	if state != task.StateBlocked {
		t.Fatalf("state = %q, want %q: an undecided routing site is the operator's to "+
			"resolve, and the task must stay resumable", state, task.StateBlocked)
	}
	joined := strings.Join(out.Reasons, " | ")
	if !strings.Contains(joined, task.IntakeSite) {
		t.Errorf("the reason must name the site that could not decide: %q", joined)
	}
	if !strings.Contains(joined, "JEV_") {
		t.Errorf("the reason must name the failure class: %q", joined)
	}
}

// The same site at logged is being observed: the identical failure changes
// nothing, which is what keeps observation from costing availability.
func TestTheSameFailureAtLoggedDoesNotBlock(t *testing.T) {
	out, state := runWithIntakeTier(t, judgment.TierLogged)

	if state == task.StateBlocked {
		t.Fatalf("a logged site must not block the task: %v", out.Reasons)
	}
	for _, reason := range out.Reasons {
		if strings.Contains(reason, task.IntakeSite) && strings.Contains(reason, "JEV_") {
			t.Errorf("a logged site must not stop the task: %q", reason)
		}
	}
}

// The regression this pins: `bcode task verify` performs no model inference and
// consults no judgment site, because a verification-only run never reaches the
// phase machine. Requiring a decision plane for it gated work that cannot use
// one — it broke the container smoke test in CI, and would have forced anyone
// wanting to run a plain verification to obtain a TypeSafe credential first.
func TestAVerificationOnlyRunNeedsNoDecisionPlane(t *testing.T) {
	requireGo(t)
	repo := gitRepo(t, map[string]string{"go.mod": goodModule, "a.go": "package a\n"})

	r, st := newRunner(t, engine.Verify{})
	// No WorkflowModel, so this never enters runPhases — and no judge at all,
	// as an installation with no judgment.yaml is assembled.
	r.Judge = nil

	ctx := context.Background()
	id := task.NewID("t")
	if err := task.NewStore(st).Create(ctx, task.Task{
		ID: id, Title: "verify", Verification: recipe.Low,
		Budget: task.Budget{MaxAttempts: 1, MaxWallTime: time.Minute},
	}); err != nil {
		t.Fatal(err)
	}

	out, err := r.Run(ctx, id, repo)
	if err != nil {
		t.Fatalf("a verification-only run must not require a decision plane: %v", err)
	}
	if !out.Accepted {
		t.Fatalf("the fixture is clean and should verify: %v", out.Reasons)
	}
	for _, reason := range out.Reasons {
		if strings.Contains(reason, "JEV_") || strings.Contains(reason, "judgment:") {
			t.Errorf("no judgment should be involved in a verification-only run: %q", reason)
		}
	}
}
