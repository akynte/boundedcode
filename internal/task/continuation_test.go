package task

import (
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/llm"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/workflow"
)

func continuationState() *workflow.State {
	return &workflow.State{
		Plan: workflow.Plan{
			RootCause: "the filter writes a response and does not abort the chain",
			Files: workflow.Targets{
				{Path: "routergroup.go", Reason: "the static handler"},
				{Path: "routergroup_test.go", Reason: "a regression test"},
			},
			WriteAllowlist: []string{"routergroup.go", "routergroup_test.go"},
			Tests:          []string{"go test"},
			Obligations: []workflow.Obligation{
				{Symbol: "Static", Path: "routergroup.go", Reason: "calls the handler",
					Resolution: workflow.Resolution{Action: workflow.ActionEdit}},
				{Symbol: "Logger", Path: "logger.go", Reason: "reads the status",
					Resolution: workflow.Resolution{Action: workflow.ActionNoChange, Reason: "unchanged"}},
			},
		},
		Edit: workflow.Transcript{Messages: make([]llm.Message, 6)},
		Results: []recipe.Result{
			{Recipe: "go build", Status: recipe.Pass, Summary: recipe.Summary{Headline: "compiles"}},
			{Recipe: "go test", Status: recipe.Fail, Summary: recipe.Summary{Headline: "1 test failed"}},
		},
		Tried: []workflow.TriedCall{
			{Fingerprint: "search_code\x00{\"query\":\"func TestFilterEncoder\"}", Digest: "d1", Count: 4, Corrected: true},
			{Fingerprint: "read_file\x00{\"path\":\"a.go\"}", Digest: "d2", Count: 1},
		},
	}
}

// The summary a continuation receives must carry the work forward and must
// come from the record, not from the model.
func TestContinuationSummaryCarriesTheState(t *testing.T) {
	s := continuationState()
	task := &Task{ID: "T1", Title: "static files log two 404 lines",
		Budget: Budget{MaxTokens: 100000}}
	s.Tokens = 40000

	got := continuationSummary(task, s, []string{"routergroup.go"}, "context limit reached")

	for _, want := range []string{
		"static files log two 404 lines",             // objective
		"the filter writes a response",               // accepted plan
		"routergroup.go",                             // declared file and modified file
		"You may write only:",                        // write scope
		"go test",                                    // how it is shown to work
		"already modified in the worktree",           // worktree state
		"Static in routergroup.go",                   // unresolved obligation
		"go build: pass",                             // verified evidence
		"search_code",                                // loop guard memory
		"Do not repeat them",                         //
		"continuation 1 of 3",                        // remaining budget
		"Tokens remaining for the whole task: 60000", //
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the continuation summary omits %q\n---\n%s", want, got)
		}
	}
	// An obligation the plan closed is not outstanding work.
	if strings.Contains(got, "Logger in logger.go") {
		t.Error("a resolved obligation was listed as outstanding")
	}
}

// The boundary is bounded, and the last one hands back to the ordinary path.
func TestContinuationBudgetTerminatesCleanly(t *testing.T) {
	if maxEditContinuations <= 0 || maxEditContinuations > 5 {
		t.Fatalf("maxEditContinuations = %d, which is not a small fixed constant", maxEditContinuations)
	}
	s := continuationState()
	s.EditContinuations = maxEditContinuations
	if _, ok := (&Runner{}).continueEditAllowed(s); ok {
		t.Error("a continuation was granted after the budget was spent")
	}
	s.EditContinuations = maxEditContinuations - 1
	if _, ok := (&Runner{}).continueEditAllowed(s); !ok {
		t.Error("a continuation was refused while budget remained")
	}
	// And an EDIT that never said anything is not worth continuing.
	s.EditContinuations, s.Edit.Messages = 0, make([]llm.Message, 2)
	if _, ok := (&Runner{}).continueEditAllowed(s); ok {
		t.Error("an empty EDIT was continued")
	}
}

// The loop guard survives the boundary: a call already found unhelpful stays
// on the record, so the fresh budget cannot be spent repeating it.
func TestLoopGuardSurvivesTheBoundary(t *testing.T) {
	s := continuationState()
	got := continuationSummary(&Task{ID: "T1", Title: "x"}, s, nil, "")
	if !strings.Contains(got, "search_code") || !strings.Contains(got, "4 time(s)") {
		t.Errorf("the repeated call was not carried across the boundary:\n%s", got)
	}
	// A call seen once and never corrected is not worth naming.
	if strings.Contains(got, "read_file") {
		t.Error("a call that produced information was listed as unhelpful")
	}
}
