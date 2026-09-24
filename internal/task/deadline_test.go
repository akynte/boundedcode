package task

import (
	"errors"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/llm"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/workflow"
)

func budgeted(total time.Duration) (*Task, *workflow.State) {
	start := time.Unix(1_800_000_000, 0)
	return &Task{Budget: Budget{MaxWallTime: total}}, &workflow.State{StartedAt: start.UnixMilli()}
}

// Every later phase keeps its reserve: planning ends first, then EDIT, then
// VERIFY, and REVIEW has until the task's own deadline.
func TestPhaseDeadlinesHoldBackForLaterPhases(t *testing.T) {
	for _, total := range []time.Duration{600 * time.Second, 2400 * time.Second, 20 * time.Second} {
		tk, s := budgeted(total)
		plan := phaseDeadline(tk, s, workflow.Planning)
		edit := phaseDeadline(tk, s, workflow.Edit)
		verify := phaseDeadline(tk, s, workflow.Verify)
		review := phaseDeadline(tk, s, workflow.Review)
		if !plan.Before(edit) || !edit.Before(verify) || !verify.Before(review) {
			t.Errorf("%s: deadlines out of order: plan %v edit %v verify %v review %v", total, plan, edit, verify, review)
		}
		if !review.Equal(taskDeadline(tk, s)) {
			t.Errorf("%s: REVIEW does not have until the task deadline", total)
		}
		// However small the budget, planning keeps at least 40% of it.
		if planning := plan.Sub(time.UnixMilli(s.StartedAt)); planning < total*40/100-time.Millisecond {
			t.Errorf("%s: planning keeps only %s", total, planning)
		}
	}
	tk, s := &Task{}, &workflow.State{}
	if !phaseDeadline(tk, s, workflow.Planning).IsZero() {
		t.Error("a task with no wall-clock budget got a phase deadline")
	}
}

// Plenty of time leaves the configured limits alone; little time shrinks the
// reasoning first; too little to answer refuses the call before it starts.
func TestACallIsSizedToTheTimeItsPhaseHasLeft(t *testing.T) {
	tk, s := budgeted(2400 * time.Second)
	s.DecodeTPS, s.PrefillTPS = 28, 260
	start := time.UnixMilli(s.StartedAt)

	limit, reasoning, err := sizeCall(tk, s, workflow.Planning, 4000, 8192, 3072, start)
	if err != nil || limit != 8192 || reasoning != 3072 {
		t.Errorf("with time to spare: limit %d reasoning %d err %v, want the configured 8192/3072", limit, reasoning, err)
	}

	pd := phaseDeadline(tk, s, workflow.Planning)
	limit, reasoning, err = sizeCall(tk, s, workflow.Planning, 4000, 8192, 3072, pd.Add(-4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if reasoning >= 3072 || limit > 8192 || reasoning < 1 {
		t.Errorf("with four minutes left: limit %d reasoning %d, want reasoning cut", limit, reasoning)
	}
	if limit-reasoning < answerTokens(workflow.Planning) {
		t.Errorf("the answer lost its room: limit %d reasoning %d", limit, reasoning)
	}

	// The recorded refusal: two minutes left, a call that needs about one and
	// a quarter. The share is too small; the window is not. The call goes
	// ahead with the least reasoning.
	limit, reasoning, err = sizeCall(tk, s, workflow.Planning, 4000, 8192, 3072, pd.Add(-121*time.Second))
	if err != nil {
		t.Fatalf("refused with the window able to hold the call: %v", err)
	}
	if reasoning >= 3072 || limit-reasoning < answerTokens(workflow.Planning) {
		t.Errorf("with two minutes left: limit %d reasoning %d, want the answer kept and reasoning cut", limit, reasoning)
	}

	// The recorded LOCALIZE starvation: a 15K-token prompt with four and a
	// half minutes left was sized to no reasoning at all, and answered with
	// an empty selection. Reading the prompt is paid once, not shared.
	ld := phaseDeadline(tk, s, workflow.Localize)
	_, reasoning, err = sizeCall(tk, s, workflow.Localize, 15489, 4096, 1024, ld.Add(-275*time.Second))
	if err != nil || reasoning < 512 {
		t.Errorf("LOCALIZE with 275s left and a 15K prompt: reasoning %d err %v, want real reasoning", reasoning, err)
	}

	_, _, err = sizeCall(tk, s, workflow.Planning, 4000, 8192, 3072, pd.Add(-20*time.Second))
	var oot *OutOfTimeError
	if !errors.As(err, &oot) || oot.Phase != workflow.Planning {
		t.Errorf("with twenty seconds left: err %v, want an OutOfTimeError for PLAN", err)
	}
}

// The measured rate replaces the default, and verification's own duration
// sizes its reserve.
func TestThroughputAndVerificationAreMeasured(t *testing.T) {
	s := &workflow.State{}
	observeThroughput(s, &llm.ChatResponse{OutputTokens: 280, DecodeMS: 10_000, PromptTokens: 2600, PrefillMS: 10_000})
	if s.DecodeTPS != 28 || s.PrefillTPS != 260 {
		t.Errorf("decode %v prefill %v, want 28 and 260", s.DecodeTPS, s.PrefillTPS)
	}
	if verifyReserve(s) != defaultVerifyReserve {
		t.Error("an unverified task did not get the default verification reserve")
	}
	s.Results = []recipe.Result{{Duration: 40 * time.Second}, {Duration: 20 * time.Second}}
	if got := verifyReserve(s); got != 90*time.Second {
		t.Errorf("verification reserve %s, want 1.5x the measured 60s", got)
	}
}
