package task_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/engine"
	"github.com/akynte/boundedcode/internal/llm"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/workflow"
)

// continuingEngine fills its context once, then finishes.
//
// It writes to the worktree before the boundary, so the test can tell whether
// the supervisor kept the work or threw it away with the conversation.
type continuingEngine struct {
	engine.Verify
	steps        int
	sawContinued string
	sawTried     []workflow.TriedCall
	sawEdit      string
	worktree     string
}

func (*continuingEngine) Edits() bool { return true }

func (e *continuingEngine) Step(ctx context.Context, req engine.Request) (*engine.Response, error) {
	e.steps++
	e.worktree = req.Worktree
	if req.Transcript != nil {
		// A conversation long enough to be worth continuing.
		req.Transcript.Messages = []llm.Message{
			{Role: "system", Content: "s"}, {Role: "user", Content: "u"},
			{Role: "assistant", Content: "a"}, {Role: "user", Content: "u2"},
		}
		if req.SaveTranscript != nil {
			if err := req.SaveTranscript(ctx, req.Transcript); err != nil {
				return nil, err
			}
		}
	}
	if e.steps == 1 {
		// Real work, then the context runs out.
		if err := os.WriteFile(filepath.Join(req.Worktree, "a.go"),
			[]byte("package a\n\n// edited before the boundary\nfunc Add(x, y int) int { return x + y }\n"), 0o600); err != nil {
			return nil, err
		}
		return &engine.Response{
			Edited: true, BudgetExhausted: true, TokensUsed: 10,
			Summary: "EDIT context budget exhausted; a supervisor phase boundary is required",
			Tried: []workflow.TriedCall{
				{Fingerprint: "search_code\x00{\"query\":\"nothing\"}", Digest: "d", Count: 4, Corrected: true},
			},
		}, nil
	}
	e.sawContinued, e.sawTried = req.Continuation, req.Tried
	// What the continuation can actually see in the worktree is the test
	// that matters: the conversation was discarded, the work must not be.
	if body, err := os.ReadFile(filepath.Join(req.Worktree, "a.go")); err == nil {
		e.sawEdit = string(body)
	}
	return &engine.Response{Edited: true, ClaimsDone: true, TokensUsed: 10}, nil
}

// Context exhaustion is a boundary, not a death. The work, the plan and the
// loop guard's memory all cross it.
func TestEditContextExhaustionContinuesBoundedly(t *testing.T) {
	ctx := context.Background()
	eng := &continuingEngine{}
	requireGo(t)
	r, st := newRunner(t, eng)
	r.WorkflowModel = &phaseModel{accept: true}
	repo := gitRepo(t, map[string]string{"go.mod": goodModule,
		"a.go": "package a\n\nfunc Add(x, y int) int { return x - y }\n"})
	id := task.NewID("continue")
	if err := task.NewStore(st).Create(ctx, task.Task{
		ID: id, Title: "update README", Verification: recipe.Standard,
		Budget: task.Budget{MaxAttempts: 2},
	}); err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(ctx, id, repo)
	if err != nil {
		t.Fatal(err)
	}

	if eng.steps < 2 {
		t.Fatalf("the supervisor did not continue past the boundary: %d step(s), task ended %s, reasons %v",
			eng.steps, out.Task.State, out.Reasons)
	}
	if out.Task.State == task.StateBlocked {
		t.Errorf("context exhaustion killed the task instead of continuing it")
	}

	// The edit made before the boundary is still there when the
	// continuation looks.
	if !strings.Contains(eng.sawEdit, "edited before the boundary") {
		t.Errorf("the edit made before the boundary was lost; the continuation saw %q", eng.sawEdit)
	}

	// The continuation was told what it needs, by the supervisor.
	if eng.sawContinued == "" {
		t.Fatal("the continuation received no state summary")
	}
	for _, want := range []string{"update README", "continuing it, not starting over", "continuation 1 of"} {
		if !strings.Contains(eng.sawContinued, want) {
			t.Errorf("the state summary omits %q:\n%s", want, eng.sawContinued)
		}
	}

	// And the loop guard's memory crossed too.
	if len(eng.sawTried) == 0 {
		t.Error("repeated-call protection did not survive the boundary")
	}
	if !strings.Contains(eng.sawContinued, "Do not repeat them") {
		t.Errorf("the summary did not carry the calls already found unhelpful:\n%s", eng.sawContinued)
	}
}

// exhaustingEngine never finishes: every step reports the context full.
type exhaustingEngine struct {
	engine.Verify
	steps int
}

func (*exhaustingEngine) Edits() bool { return true }

func (e *exhaustingEngine) Step(ctx context.Context, req engine.Request) (*engine.Response, error) {
	e.steps++
	if req.Transcript != nil {
		req.Transcript.Messages = []llm.Message{
			{Role: "system", Content: "s"}, {Role: "user", Content: "u"},
			{Role: "assistant", Content: "a"}, {Role: "user", Content: "u2"},
		}
		if req.SaveTranscript != nil {
			if err := req.SaveTranscript(ctx, req.Transcript); err != nil {
				return nil, err
			}
		}
	}
	return &engine.Response{BudgetExhausted: true, TokensUsed: 5,
		Summary: "EDIT context budget exhausted; a supervisor phase boundary is required"}, nil
}

// The boundary is bounded. A model that fills the context every time must
// stop, not reset forever.
func TestContinuationBudgetEndsTheTask(t *testing.T) {
	ctx := context.Background()
	eng := &exhaustingEngine{}
	requireGo(t)
	r, st := newRunner(t, eng)
	r.WorkflowModel = &phaseModel{accept: true}
	repo := gitRepo(t, map[string]string{"go.mod": goodModule,
		"a.go": "package a\n\nfunc Add(x, y int) int { return x - y }\n"})
	id := task.NewID("exhaust")
	if err := task.NewStore(st).Create(ctx, task.Task{
		ID: id, Title: "update README", Verification: recipe.Standard,
		Budget: task.Budget{MaxAttempts: 2},
	}); err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(ctx, id, repo)
	if err != nil {
		t.Fatal(err)
	}
	if out.Accepted {
		t.Error("a task that never finished was accepted")
	}
	if eng.steps > 12 {
		t.Errorf("the boundary became an unbounded reset loop: %d steps", eng.steps)
	}
	if out.Task.State != task.StateBlocked && out.Task.State != task.StateFailed {
		t.Errorf("the task ended %s, want a clean stop", out.Task.State)
	}
}
