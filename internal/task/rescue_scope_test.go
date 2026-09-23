package task

// The recorded failure this covers.
//
// `conflict-status` was run four times against scope internal/api and failed
// in PLAN every time — twice on targets that did not exist, once on one that
// existed but was out of scope, once in LOCALIZE. It read as model variance.
// It was not. IMPACT found no actionable consumer, the bounded rescue ran
// lexical retrieval across the whole repository, and appended what it found
// straight into s.Files — which is the planner's "files" evidence and the
// list the planner is told to plan from. The planner named
// internal/inventory/inventory.go because the harness put it in front of it,
// and Plan.Validate then rejected the plan for exceeding operator scope after
// two correction rounds and a full phase budget.
//
// Two deterministic layers disagreed about what the scope meant, and the model
// was blamed for obeying the one that was wrong.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/retrieval"
	"github.com/akynte/boundedcode/internal/workflow"
)

func slice(path, symbol string) retrieval.Slice {
	return retrieval.Slice{Path: path, Symbol: symbol, PathKnown: true}
}

func TestRescueNeverAddsAFileTheOperatorScopeExcludes(t *testing.T) {
	slices := []retrieval.Slice{
		slice("internal/api/api.go", "Reserve"),
		slice("internal/inventory/inventory.go", "Reserve"), // the real rejection
		slice("internal/orders/orders.go", "Place"),
	}
	files, _, outOfScope, _ := rescueAdditions(slices, map[string]bool{},
		[]string{"internal/api"}, 8)

	for _, f := range files {
		if f != "internal/api/api.go" {
			t.Errorf("rescue added %q, which the scope internal/api does not cover; "+
				"the planner will name it and PLAN will reject the plan", f)
		}
	}
	if len(files) != 1 {
		t.Errorf("added %v, want only the in-scope file", files)
	}
	if outOfScope != 2 {
		t.Errorf("outOfScope = %d, want 2; the count is what tells an operator "+
			"their scope is wrong rather than their retrieval empty", outOfScope)
	}
}

// An empty scope means unrestricted here, as it does everywhere else.
func TestAnEmptyScopeAddsEverythingRetrievalFound(t *testing.T) {
	slices := []retrieval.Slice{
		slice("internal/api/api.go", "A"),
		slice("internal/inventory/inventory.go", "B"),
	}
	files, _, outOfScope, _ := rescueAdditions(slices, map[string]bool{}, nil, 8)
	if len(files) != 2 {
		t.Errorf("added %v, want both under an empty scope", files)
	}
	if outOfScope != 0 {
		t.Errorf("outOfScope = %d, want 0 under an empty scope", outOfScope)
	}
}

// A scope that excludes everything retrieval found must add nothing, so the
// caller reports it rather than handing the planner an impossible list.
func TestAScopeThatExcludesEverythingAddsNothing(t *testing.T) {
	slices := []retrieval.Slice{
		slice("internal/inventory/inventory.go", "B"),
		slice("internal/orders/orders.go", "C"),
	}
	files, symbols, outOfScope, _ := rescueAdditions(slices, map[string]bool{},
		[]string{"internal/api"}, 8)
	if len(files) != 0 || len(symbols) != 0 {
		t.Errorf("added %v/%v, want nothing", files, symbols)
	}
	if outOfScope != 2 {
		t.Errorf("outOfScope = %d, want 2", outOfScope)
	}
}

// The pre-existing filters still hold: already-known paths, unknown paths and
// the cap are unchanged by the scope rule.
func TestRescueKeepsItsOtherFilters(t *testing.T) {
	known := map[string]bool{"internal/api/known.go": true}
	slices := []retrieval.Slice{
		slice("internal/api/known.go", "already"),
		{Path: "internal/api/unknown.go", PathKnown: false},
		{Path: "", PathKnown: true},
		slice("internal/api/a.go", "A"),
		slice("internal/api/b.go", "B"),
		slice("internal/api/c.go", "C"),
	}
	files, _, outOfScope, _ := rescueAdditions(slices, known, []string{"internal/api"}, 2)
	if len(files) != 2 {
		t.Errorf("added %v, want the cap of 2 honoured", files)
	}
	for _, f := range files {
		if f == "internal/api/known.go" || f == "internal/api/unknown.go" || f == "" {
			t.Errorf("added %q, which an existing filter should have excluded", f)
		}
	}
	if outOfScope != 0 {
		t.Errorf("outOfScope = %d, want 0; nothing here is out of scope", outOfScope)
	}
}

// A symbol equal to its path is not a symbol, and that rule is unchanged.
func TestRescueDoesNotTreatAPathAsASymbol(t *testing.T) {
	slices := []retrieval.Slice{slice("internal/api/a.go", "internal/api/a.go")}
	files, symbols, _, _ := rescueAdditions(slices, map[string]bool{}, []string{"internal/api"}, 8)
	if len(files) != 1 {
		t.Fatalf("files = %v, want the one file", files)
	}
	if len(symbols) != 0 {
		t.Errorf("symbols = %v, want none when the symbol repeats the path", symbols)
	}
}

// Retrieval agreeing with what LOCALIZE already chose is confirmation, not an
// empty result — and the difference decides whether a task is blocked.
//
// The recorded failure: perishable-zone had six in-scope localized files and
// a change with no downstream consumers. The rescue added nothing, because
// everything relevant was already localized, and the IMPACT guard read
// "added nothing" as "no context" and blocked a perfectly plannable task with
// "insufficient context". A local change legitimately has no consumers.
func TestRescueCountsWhatWasAlreadyLocalizedSeparately(t *testing.T) {
	known := map[string]bool{
		"internal/shipping/shipping.go": true,
		"internal/shipping/zones.go":    true,
	}
	slices := []retrieval.Slice{
		slice("internal/shipping/shipping.go", "Quote"), // already localized
		slice("internal/shipping/zones.go", "Zone"),     // already localized
		slice("internal/orders/orders.go", "Place"),     // out of scope
	}
	files, _, outOfScope, alreadyKnown := rescueAdditions(slices, known,
		[]string{"internal/shipping"}, 8)

	if len(files) != 0 {
		t.Errorf("added %v, want nothing new", files)
	}
	if alreadyKnown != 2 {
		t.Errorf("alreadyKnown = %d, want 2; without this the caller cannot tell "+
			"'the surface was already found' from 'nothing was found'", alreadyKnown)
	}
	if outOfScope != 1 {
		t.Errorf("outOfScope = %d, want 1", outOfScope)
	}
}

// Nothing at all is the one case that is genuinely insufficient context.
func TestRescueReportsNothingFoundDistinctly(t *testing.T) {
	files, _, outOfScope, alreadyKnown := rescueAdditions(nil, map[string]bool{},
		[]string{"internal/shipping"}, 8)
	if len(files) != 0 || outOfScope != 0 || alreadyKnown != 0 {
		t.Errorf("got files=%v outOfScope=%d alreadyKnown=%d, want all empty",
			files, outOfScope, alreadyKnown)
	}
}

// The wall-clock budget bounds ctx, so a task that runs out of time fails
// inside whatever call was in flight and reports that call's error.
//
// The recorded failure: conflict-status ran 1803s against an 1800s budget and
// reported "native: step 3: llm: local /v1/chat/completions: Post ...:
// context deadline exceeded". An operator reading that goes and debugs an
// inference server that was working correctly.
func TestBudgetExhaustionIsNamedRatherThanItsSymptom(t *testing.T) {
	started := time.Now().Add(-31 * time.Minute)
	task := &Task{Budget: Budget{MaxWallTime: 30 * time.Minute}}
	st := &workflow.State{Phase: workflow.Edit, StartedAt: started.UnixMilli()}

	wrapped := fmt.Errorf("native: step 3: llm: local /v1/chat/completions: %w",
		context.DeadlineExceeded)
	got := budgetAwareReason(task, st, wrapped)

	if !strings.Contains(got, "wall-clock budget exhausted") {
		t.Errorf("reason = %q, want the budget named as the cause", got)
	}
	if !strings.Contains(got, "EDIT") {
		t.Errorf("reason = %q, want the phase it ran out in", got)
	}
	if !strings.Contains(got, "llm: local") {
		t.Errorf("reason = %q, want the interrupted call kept as evidence", got)
	}
}

// An ordinary failure inside the budget keeps its own message.
func TestAnErrorInsideTheBudgetIsReportedAsItself(t *testing.T) {
	task := &Task{Budget: Budget{MaxWallTime: 30 * time.Minute}}
	st := &workflow.State{Phase: workflow.Edit, StartedAt: time.Now().UnixMilli()}
	got := budgetAwareReason(task, st, errors.New("connection refused"))
	if got != "connection refused" {
		t.Errorf("reason = %q, want the original error", got)
	}
}

// A deadline error with no budget configured is not a budget exhaustion.
func TestADeadlineWithNoBudgetIsNotRelabelled(t *testing.T) {
	task := &Task{}
	st := &workflow.State{Phase: workflow.Edit, StartedAt: time.Now().Add(-time.Hour).UnixMilli()}
	got := budgetAwareReason(task, st, context.DeadlineExceeded)
	if strings.Contains(got, "budget exhausted") {
		t.Errorf("reason = %q, want no budget claim when none is configured", got)
	}
}
