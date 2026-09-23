package task_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/engine"
	"github.com/akynte/boundedcode/internal/llm"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/workflow"
	"github.com/akynte/boundedcode/internal/worktree"
)

// correctedPlanner answers the first PLAN call with a plan that names a file
// in a directory the repository does not have, and every later one with a
// valid plan, so the task goes through exactly one plan correction.
type correctedPlanner struct {
	phaseModel
	plans int
}

func (p *correctedPlanner) ChatStructured(ctx context.Context, req llm.ChatRequest, schema json.RawMessage) (*llm.ChatResponse, error) {
	instruction := req.Messages[len(req.Messages)-1].Content
	if strings.Contains(instruction, "executable plan") {
		p.plans++
		if p.plans == 1 {
			return &llm.ChatResponse{Content: `{"root_cause":"addition needs correction","files":["a.go","invented/missing.go"],"symbols":["Add"],"tests":["go test ./..."],"contracts":[],"write_allowlist":["a.go"],"risks":[]}`,
				PromptTokens: 10, OutputTokens: 10}, nil
		}
	}
	return p.phaseModel.ChatStructured(ctx, req, schema)
}

// feedbackEditor records what EDIT was told to fix before doing the edit.
type feedbackEditor struct {
	phaseEditor
	feedback []recipe.Result
}

func (e *feedbackEditor) Step(ctx context.Context, req engine.Request) (*engine.Response, error) {
	e.feedback = append(e.feedback, req.Feedback...)
	return e.phaseEditor.Step(ctx, req)
}

// A correction is for the planner. The recorded failure: after one corrected
// plan was accepted, EDIT's first attempt opened with "Verification findings
// from the previous attempt — fix these: the previous plan was rejected …
// return the plan again", and the model spent its first steps on a plan it
// could not return.
func TestAPlanCorrectionDoesNotReachEdit(t *testing.T) {
	requireGo(t)
	repo := gitRepo(t, map[string]string{"go.mod": goodModule, "a.go": "package a\n\nfunc Add(x, y int) int { return x - y }\n"})
	editor := &feedbackEditor{}
	r, st := newRunner(t, editor)
	planner := &correctedPlanner{phaseModel: phaseModel{accept: true}}
	r.WorkflowModel = planner
	ctx := context.Background()
	id := task.NewID("plancorrection")
	if err := task.NewStore(st).Create(ctx, task.Task{ID: id, Title: "correct addition",
		Verification: recipe.Standard, Budget: task.Budget{MaxAttempts: 1}}); err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(ctx, id, repo)
	if err != nil {
		t.Fatal(err)
	}
	if planner.plans != 2 {
		t.Fatalf("expected one corrected plan (2 PLAN calls), got %d; outcome %+v", planner.plans, out)
	}
	if editor.calls == 0 {
		t.Fatalf("EDIT never ran; outcome %+v", out)
	}
	for _, f := range editor.feedback {
		if strings.Contains(f.Summary.Headline, "plan was rejected") {
			t.Fatalf("EDIT was handed the planner's correction as a finding to fix: %q", f.Summary.Headline)
		}
	}
}

// recordingPlanner is correctedPlanner that keeps the evidence each PLAN call
// was shown, so a test can read the correction.
type recordingPlanner struct {
	correctedPlanner
	seen []string
}

func (p *recordingPlanner) ChatStructured(ctx context.Context, req llm.ChatRequest, schema json.RawMessage) (*llm.ChatResponse, error) {
	if strings.Contains(req.Messages[len(req.Messages)-1].Content, "executable plan") {
		var all strings.Builder
		for _, m := range req.Messages {
			all.WriteString(m.Content)
		}
		p.seen = append(p.seen, all.String())
	}
	return p.correctedPlanner.ChatStructured(ctx, req, schema)
}

// Every problem goes back in one correction. The recorded task learned one
// per round — a symbol, then scope, then its obligations — and ran out of
// corrections before the third arrived.
func TestOneCorrectionCarriesEveryProblem(t *testing.T) {
	requireGo(t)
	repo := gitRepo(t, map[string]string{"go.mod": goodModule, "a.go": "package a\n\nfunc Add(x, y int) int { return x - y }\n"})
	editor := &feedbackEditor{}
	r, st := newRunner(t, editor)
	planner := &recordingPlanner{correctedPlanner: correctedPlanner{phaseModel: phaseModel{accept: true}}}
	r.WorkflowModel = planner
	ctx := context.Background()
	id := task.NewID("allproblems")
	// invented/missing.go is both a file in an invented directory and outside
	// the scope: two checks object to the first plan.
	if err := task.NewStore(st).Create(ctx, task.Task{ID: id, Title: "correct addition",
		Verification: recipe.Standard, Budget: task.Budget{MaxAttempts: 1, Scope: []string{"a.go"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, id, repo); err != nil {
		t.Fatal(err)
	}
	if len(planner.seen) < 2 {
		t.Fatalf("the first plan was not corrected (%d PLAN calls)", len(planner.seen))
	}
	second := planner.seen[1]
	for _, want := range []string{"invented/missing.go", "operator scope", "problems", "Whatever else changes"} {
		if !strings.Contains(second, want) {
			t.Errorf("the correction does not mention %q", want)
		}
	}
}

// stalledPlanner never answers PLAN: it waits until the task's context ends.
type stalledPlanner struct {
	phaseModel
	plans int
}

func (p *stalledPlanner) ChatStructured(ctx context.Context, req llm.ChatRequest, schema json.RawMessage) (*llm.ChatResponse, error) {
	if strings.Contains(req.Messages[len(req.Messages)-1].Content, "executable plan") {
		p.plans++
		<-ctx.Done()
		return nil, fmt.Errorf("llm: local /v1/chat/completions: %w", ctx.Err())
	}
	return p.phaseModel.ChatStructured(ctx, req, schema)
}

// The call the wall-clock budget interrupts is not a rejected plan. It was
// fed back as "the previous plan was rejected: … context deadline exceeded",
// spent the correction rounds, and the task read as a transport failure.
func TestADeadlineInPlanIsTheBudgetNotARejectedPlan(t *testing.T) {
	requireGo(t)
	repo := gitRepo(t, map[string]string{"go.mod": goodModule, "a.go": "package a\n\nfunc Add(x, y int) int { return x - y }\n"})
	r, st := newRunner(t, &feedbackEditor{})
	planner := &stalledPlanner{phaseModel: phaseModel{accept: true}}
	r.WorkflowModel = planner
	ctx := context.Background()
	id := task.NewID("plandeadline")
	if err := task.NewStore(st).Create(ctx, task.Task{ID: id, Title: "correct addition",
		Verification: recipe.Standard, Budget: task.Budget{MaxAttempts: 1, MaxWallTime: 2 * time.Second}}); err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(ctx, id, repo)
	if err != nil {
		t.Fatal(err)
	}
	// With deadline-aware phases the task may stop before PLAN is asked at
	// all: two seconds cannot fit a planning call, and the supervisor says so
	// rather than starting one. What must never happen is a retry.
	if planner.plans > 1 {
		t.Errorf("PLAN was asked %d times; an interrupted call must not be retried as a correction", planner.plans)
	}
	reasons := strings.Join(out.Reasons, " ")
	if !strings.Contains(reasons, "wall-clock budget exhausted") || strings.Contains(reasons, "corrections") {
		t.Errorf("reasons = %q, want the budget named and no correction rounds", reasons)
	}
}

// scriptedPlanner answers each PLAN call with the next scripted plan.
type scriptedPlanner struct {
	phaseModel
	plans []string
	calls int
}

func (p *scriptedPlanner) ChatStructured(ctx context.Context, req llm.ChatRequest, schema json.RawMessage) (*llm.ChatResponse, error) {
	if strings.Contains(req.Messages[len(req.Messages)-1].Content, "executable plan") {
		body := p.plans[min(p.calls, len(p.plans)-1)]
		p.calls++
		return &llm.ChatResponse{Content: body, PromptTokens: 10, OutputTokens: 10}, nil
	}
	return p.phaseModel.ChatStructured(ctx, req, schema)
}

// A round that only repeats a refusal is answered as one, and does not spend
// the budget — once. The recorded rewrite named an out-of-scope file the round
// before had removed, and cost a correction for it.
func TestARepeatedRefusalIsNamedAndCostsNoRound(t *testing.T) {
	requireGo(t)
	repo := gitRepo(t, map[string]string{"go.mod": goodModule, "a.go": "package a\n\nfunc Add(x, y int) int { return x - y }\n",
		"b.go": "package a\n"})
	outOfScope := `{"root_cause":"addition needs correction","files":["a.go","b.go"],"symbols":["Add"],"tests":["go test ./..."],"contracts":[],"write_allowlist":["a.go","b.go"],"risks":[]}`
	valid := `{"root_cause":"addition needs correction","files":["a.go"],"symbols":["Add"],"tests":["go test ./..."],"contracts":[],"write_allowlist":["a.go"],"risks":[]}`
	planner := &scriptedPlanner{phaseModel: phaseModel{accept: true}, plans: []string{outOfScope, outOfScope, valid}}
	r, st := newRunner(t, &feedbackEditor{})
	r.WorkflowModel = planner
	ctx := context.Background()
	id := task.NewID("repeat")
	if err := task.NewStore(st).Create(ctx, task.Task{ID: id, Title: "correct addition",
		Verification: recipe.Standard, Budget: task.Budget{MaxAttempts: 1, Scope: []string{"a.go"}}}); err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(ctx, id, repo)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Accepted || planner.calls != 3 {
		t.Fatalf("accepted=%v after %d PLAN calls; reasons %v", out.Accepted, planner.calls, out.Reasons)
	}
	s, err := task.NewStore(st).LoadWorkflow(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if s.Replans != 1 || !s.Settled.FreeRoundUsed {
		t.Errorf("replans = %d, free round used = %v; the repeat should have cost nothing", s.Replans, s.Settled.FreeRoundUsed)
	}
}

// stallingEditor makes its fix and then does not finish: it waits for its
// context to end, as a model mid-exploration does.
type stallingEditor struct{ phaseEditor }

func (e *stallingEditor) Step(ctx context.Context, req engine.Request) (*engine.Response, error) {
	if _, err := e.phaseEditor.Step(ctx, req); err != nil {
		return nil, err
	}
	<-ctx.Done()
	return nil, fmt.Errorf("native: step 9: llm: local /v1/chat/completions: %w", ctx.Err())
}

// EDIT that reaches its share of the budget stops and verifies what it has,
// with time held back for VERIFY and REVIEW. The recorded run passed
// verification and then ran out of time in REVIEW, with the fix in place and
// nothing left to accept it.
func TestEditAtItsDeadlineIsVerifiedAndReviewed(t *testing.T) {
	requireGo(t)
	repo := gitRepo(t, map[string]string{"go.mod": goodModule, "a.go": "package a\n\nfunc Add(x, y int) int { return x - y }\n"})
	r, st := newRunner(t, &stallingEditor{})
	r.WorkflowModel = &phaseModel{accept: true}
	// A fast model, so the structured phases fit a budget small enough for a
	// test; EDIT is what runs into its deadline.
	r.SeedState = func(_ context.Context, _ *task.Task, _ *worktree.Worktree, s *workflow.State) error {
		s.DecodeTPS, s.PrefillTPS = 1e6, 1e6
		return nil
	}
	ctx := context.Background()
	id := task.NewID("editdeadline")
	if err := task.NewStore(st).Create(ctx, task.Task{ID: id, Title: "correct addition",
		Verification: recipe.Standard, Budget: task.Budget{MaxAttempts: 1, MaxWallTime: 8 * time.Second}}); err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(ctx, id, repo)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Accepted {
		t.Fatalf("a verified fix was not accepted when EDIT reached its deadline: %v", out.Reasons)
	}
}
