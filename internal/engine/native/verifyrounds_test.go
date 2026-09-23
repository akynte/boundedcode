package native_test

import (
	"context"
	"testing"

	"github.com/akynte/boundedcode/internal/engine"
	"github.com/akynte/boundedcode/internal/firewall"
	"github.com/akynte/boundedcode/internal/llm"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/workflow"
)

func verify(id, kind string) *llm.ChatResponse {
	return call(id, "run_verification", map[string]any{"kind": kind})
}

func edit(id, old, new string) *llm.ChatResponse {
	return call(id, "edit_file", map[string]any{"path": "calc.go", "old": old, "new": new})
}

// The recorded run: one edit, then build, vet, test and format, one check per
// call as the tool asks. The fourth was refused as "EDIT phase budget
// exhausted" and the phase threw the passing transcript away. Checks of one
// state of the worktree are one round.
func TestChecksOfOneEditAreOneVerificationRound(t *testing.T) {
	wt := worktree(t, map[string]string{"calc.go": "package calc\n\nfunc Add(x, y int) int { return x - y }\n"})
	p := newScripted(
		edit("1", "return x - y", "return x + y"),
		verify("2", "build"), verify("3", "vet"), verify("4", "test"), verify("5", "format"),
		call("6", "done", map[string]any{"summary": "fixed"}),
	)
	e := newEngine(t, p)
	e.SetRecipeRunner(&recipe.Runner{})
	tr := &workflow.Transcript{}
	resp, err := e.Step(context.Background(), engine.Request{Access: firewall.Access{WriteScope: []string{"."}},
		TaskID: "t1", Objective: "fix Add", Worktree: wt, Attempt: 1, Transcript: tr})
	if err != nil {
		t.Fatal(err)
	}
	if resp.BudgetExhausted || !resp.ClaimsDone {
		t.Fatalf("four checks of one edit ended the attempt: %+v", resp)
	}
	if tr.VerifyRuns != 1 {
		t.Errorf("verification rounds = %d, want 1", tr.VerifyRuns)
	}
}

// The bound still holds where it was meant to: a fourth edit-and-verify
// round is refused.
func TestAFourthVerificationRoundIsStillRefused(t *testing.T) {
	wt := worktree(t, map[string]string{"calc.go": "package calc\n\nfunc Add(x, y int) int { return 0 }\n"})
	p := newScripted(
		edit("1", "return 0", "return 1"), verify("2", "test"),
		edit("3", "return 1", "return 2"), verify("4", "test"),
		edit("5", "return 2", "return 3"), verify("6", "test"),
		edit("7", "return 3", "return x + y"), verify("8", "test"),
		call("9", "done", map[string]any{"summary": "fixed"}),
	)
	e := newEngine(t, p)
	e.SetRecipeRunner(&recipe.Runner{})
	tr := &workflow.Transcript{}
	resp, err := e.Step(context.Background(), engine.Request{Access: firewall.Access{WriteScope: []string{"."}},
		TaskID: "t1", Objective: "fix Add", Worktree: wt, Attempt: 1, Transcript: tr})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.BudgetExhausted || resp.ClaimsDone {
		t.Fatalf("a fourth verification round was allowed: %+v, rounds %d", resp, tr.VerifyRuns)
	}
}
