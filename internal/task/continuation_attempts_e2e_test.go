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

// A continuation is not an attempt.
//
// The recorded failure these cover: a context boundary emptied the EDIT
// transcript, the loop read an empty transcript as a new attempt, and three
// boundaries spent an attempt budget of three. GIN-1805 died having made no
// edit and having failed no attempt — the rescue mechanism consumed the
// budget it exists to protect, and the third continuation it had just built
// was discarded before the model saw it.

// attemptBoundaryEngine fills its context a fixed number of times, recording the
// attempt number the supervisor reported on every step.
type attemptBoundaryEngine struct {
	engine.Verify
	boundaries   int // how many times to report the context full
	steps        int
	attemptSeen  []int
	continuation []string
	sawReads     [][]workflow.ReadEvidence
	sawTried     [][]workflow.TriedCall
	editedAt     int
}

func (*attemptBoundaryEngine) Edits() bool { return true }

func (e *attemptBoundaryEngine) Step(ctx context.Context, req engine.Request) (*engine.Response, error) {
	e.steps++
	e.attemptSeen = append(e.attemptSeen, req.Attempt)
	e.continuation = append(e.continuation, req.Continuation)
	e.sawReads = append(e.sawReads, req.Reads)
	e.sawTried = append(e.sawTried, req.Tried)

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

	// Evidence accumulates the way the real engine's cache does.
	reads := append([]workflow.ReadEvidence(nil), req.Reads...)
	reads = append(reads, workflow.ReadEvidence{
		Key:  "read_file\x00{\"path\":\"step" + attemptItoa(e.steps) + ".go\"}",
		Tool: "read_file", Path: "step" + attemptItoa(e.steps) + ".go",
		Digest: "d" + attemptItoa(e.steps), Seq: e.steps,
		Decls: []string{"func Step" + attemptItoa(e.steps) + "()"},
	})
	tried := append([]workflow.TriedCall(nil), req.Tried...)
	tried = append(tried, workflow.TriedCall{
		Fingerprint: "read_file\x00{\"path\":\"step" + attemptItoa(e.steps) + ".go\"}",
		Digest:      "d" + attemptItoa(e.steps), Count: 4, Corrected: true,
	})

	if e.steps <= e.boundaries {
		return &engine.Response{
			BudgetExhausted: true, TokensUsed: 10, Reads: reads, Tried: tried,
			Summary: "EDIT context budget exhausted; a supervisor phase boundary is required",
		}, nil
	}
	// Past the boundaries the model finally does the work.
	e.editedAt = e.steps
	if err := os.WriteFile(filepath.Join(req.Worktree, "a.go"),
		[]byte("package a\n\nfunc Add(x, y int) int { return x + y }\n"), 0o600); err != nil {
		return nil, err
	}
	return &engine.Response{Edited: true, ClaimsDone: true, TokensUsed: 10,
		Reads: reads, Tried: tried}, nil
}

func attemptItoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func runBoundaryTask(t *testing.T, eng *attemptBoundaryEngine, maxAttempts int) task.Outcome {
	t.Helper()
	ctx := context.Background()
	requireGo(t)
	r, st := newRunner(t, eng)
	r.WorkflowModel = &phaseModel{accept: true}
	repo := gitRepo(t, map[string]string{"go.mod": goodModule,
		"a.go": "package a\n\nfunc Add(x, y int) int { return x - y }\n"})
	id := task.NewID("attempts")
	if err := task.NewStore(st).Create(ctx, task.Task{
		ID: id, Title: "make Add add", Verification: recipe.Standard,
		Budget: task.Budget{MaxAttempts: maxAttempts},
	}); err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(ctx, id, repo)
	if err != nil {
		t.Fatal(err)
	}
	return *out
}

// Cases 1, 2 and 3 together. Three boundaries inside one attempt, the third
// continuation actually runs, and the attempt counter never moves.
func TestContinuationsDoNotConsumeEditAttempts(t *testing.T) {
	// Three boundaries, then real work: exactly the shape that used to die.
	eng := &attemptBoundaryEngine{boundaries: 3}
	out := runBoundaryTask(t, eng, 3)

	// Case 3: the step after the third boundary happened at all.
	if eng.editedAt == 0 {
		t.Fatalf("the model never got a turn after the boundaries: %d step(s), state %s, reasons %v",
			eng.steps, out.Task.State, out.Reasons)
	}
	if eng.steps < 4 {
		t.Fatalf("expected four steps (three boundaries plus the work), got %d", eng.steps)
	}

	// Case 1 and 2: every step before the work reported the same attempt.
	first := eng.attemptSeen[0]
	for i, a := range eng.attemptSeen {
		if a != first {
			t.Errorf("step %d reported attempt %d, want %d: a continuation spent an attempt\n%v",
				i+1, a, first, eng.attemptSeen)
			break
		}
	}
	if out.Attempts != 1 {
		t.Errorf("the task recorded %d attempt(s) for one attempt with three continuations",
			out.Attempts)
	}

	// And the task was not failed by the attempt budget.
	for _, r := range out.Reasons {
		if strings.Contains(r, "attempt budget") {
			t.Errorf("the attempt budget ended a task that only ever continued: %v", out.Reasons)
		}
	}
}

// Case 4 and 8 at the supervisor boundary: what the previous conversation
// found is handed to the next one, and so is the whole repetition record.
func TestEvidenceAndRepetitionCrossEveryBoundary(t *testing.T) {
	eng := &attemptBoundaryEngine{boundaries: 3}
	runBoundaryTask(t, eng, 3)

	if len(eng.sawReads) < 4 {
		t.Fatalf("expected at least four steps, got %d", len(eng.sawReads))
	}
	// The first step starts empty; each later one receives everything found
	// so far, and the counts only grow.
	for i := 1; i < len(eng.sawReads); i++ {
		if len(eng.sawReads[i]) < i {
			t.Errorf("step %d received %d evidence record(s), want at least %d: "+
				"evidence was dropped at a boundary", i+1, len(eng.sawReads[i]), i)
		}
		if len(eng.sawTried[i]) < i {
			t.Errorf("step %d received %d tried call(s), want at least %d: "+
				"repetition memory was reset at a boundary", i+1, len(eng.sawTried[i]), i)
		}
	}

	// The summary names what was found, not merely that something was read.
	last := eng.continuation[len(eng.continuation)-1]
	if last == "" {
		t.Fatal("the final continuation carried no summary")
	}
	if !strings.Contains(last, "already inspected") {
		t.Errorf("the continuation carried no read evidence:\n%s", last)
	}
	if !strings.Contains(last, "func Step1()") {
		t.Errorf("the continuation lost what the first read found:\n%s", last)
	}
}

// Case 9. The worktree and the accepted plan cross the boundaries: an edit
// made before one is still on disk after it.
func TestWorktreeAndPlanSurviveEveryBoundary(t *testing.T) {
	eng := &attemptBoundaryEngine{boundaries: 2}
	out := runBoundaryTask(t, eng, 3)

	if eng.editedAt == 0 {
		t.Fatal("the model never reached the step that edits")
	}
	for i, c := range eng.continuation {
		if i == 0 {
			continue // the first step is not a continuation
		}
		if c == "" {
			t.Errorf("step %d resumed with no state summary", i+1)
			continue
		}
		if !strings.Contains(c, "make Add add") {
			t.Errorf("step %d lost the objective:\n%s", i+1, c)
		}
		if !strings.Contains(c, "continuing it, not starting over") {
			t.Errorf("step %d was not told it was continuing:\n%s", i+1, c)
		}
	}
	if out.Task.State == task.StateBlocked {
		t.Errorf("the task was blocked despite completing its work: %v", out.Reasons)
	}
}

// Case 10. The continuation budget still bounds the whole thing: a model that
// never stops filling the context is stopped, and not by running forever.
func TestContinuationBudgetStillBoundsTheTask(t *testing.T) {
	// Far more boundaries than the budget allows.
	eng := &attemptBoundaryEngine{boundaries: 50}
	out := runBoundaryTask(t, eng, 2)

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
