package task

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/graph"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/workflow"
)

// PLAN must fit its evidence to the phase budget rather than refuse it.
//
// The recorded failure these cover is GIN-1805: IMPACT succeeded, returned a
// large consumer set, and PLAN died twice — once on the byte ceiling and once
// on the append-only log — having made zero attempts. The run that reached
// EDIT was the one where IMPACT found nothing.

// bigImpact builds an impact report of the size that broke PLAN: many
// consumers, spread over files, with a mix of verdicts.
func bigImpact(files, perFile int) *graph.Impact {
	im := &graph.Impact{
		Kind:   graph.ChangeSignature,
		Caveat: graph.ImpactCaveat,
		Counts: map[graph.Verdict]int{},
	}
	for f := range files {
		path := fmt.Sprintf("internal/pkg%02d/service.go", f)
		for i := range perFile {
			v := graph.Compatible
			switch i % 4 {
			case 0:
				v = graph.Breaking
			case 1:
				v = graph.Undetermined
			case 2:
				v = graph.Compiles
			}
			im.Consumers = append(im.Consumers, graph.Consumer{
				Node: graph.Node{Path: path, Name: fmt.Sprintf("Consumer%03d", i),
					Kind: graph.KindFunction},
				Depth: 1 + i%3, Via: graph.EdgeCalls, Evidence: graph.Resolved,
				Verdict: v,
				Migration: "update the call site to the new signature; " +
					strings.Repeat("detail ", 20),
			})
			im.Counts[v]++
		}
	}
	return im
}

func planState(files, perFile int) *workflow.State {
	s := &workflow.State{
		Hypothesis: "the static handler installs every no-route handler instead of the bare one",
		Files:      []string{"routergroup.go", "gin.go"},
		Symbols:    []string{"createStaticHandler", "allNoRoute"},
		Impact:     bigImpact(files, perFile),
		Bodies:     map[string]string{},
	}
	for i := range 40 {
		s.Bodies[fmt.Sprintf("internal/pkg%02d/service.go:Consumer%03d", i, i)] =
			strings.Repeat("func Consumer() { doSomethingFairlyLong() }\n", 120)
	}
	s.Bodies["routergroup.go:createStaticHandler"] = strings.Repeat("// the real one\n", 100)
	return s
}

// Case 1. A large impact report still produces evidence the phase admits.
func TestLargeImpactStillFitsThePlanBudget(t *testing.T) {
	s := planState(24, 30) // 720 consumers
	evidence, sum := fitPlanEvidence(s, map[string]any{"operator_scope": []string{"."}}, nil)

	body, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > phaseEvidenceLimit {
		t.Fatalf("PLAN evidence is %d bytes, over the %d budget: the defect is not fixed",
			len(body), phaseEvidenceLimit)
	}
	if sum.Bytes != len(body) {
		t.Errorf("the recorded size %d does not match the real %d", sum.Bytes, len(body))
	}
	if sum.Available <= sum.Retained {
		t.Errorf("nothing was dropped from %d available items", sum.Available)
	}
	if !sum.Truncated {
		t.Error("the evidence was abbreviated without saying so")
	}
}

// The admission test is honoured, not just the byte ceiling. This is the
// bound the append-only log actually imposes and the one PLAN hit second.
func TestPlanEvidenceHonoursTheAdmissionTest(t *testing.T) {
	s := planState(24, 30)
	// A deliberately tight admission test, standing in for a nearly full log.
	const tight = 12000
	evidence, sum := fitPlanEvidence(s, nil, func(b []byte) bool { return len(b) <= tight })

	body, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > tight {
		t.Fatalf("the fitted evidence is %d bytes against an admission limit of %d",
			len(body), tight)
	}
	if sum.ActionableRetained == 0 {
		t.Error("every actionable obligation was dropped to fit")
	}
}

// Case 2. The objective and the accepted targets are never what gets dropped.
func TestPlanKeepsObjectiveAndTargetsUnderPressure(t *testing.T) {
	s := planState(40, 40) // 1600 consumers: far past any budget
	evidence, _ := fitPlanEvidence(s, nil, func(b []byte) bool { return len(b) <= 4000 })

	if got, _ := evidence["hypothesis"].(string); got != s.Hypothesis {
		t.Errorf("the hypothesis was dropped or altered: %q", got)
	}
	files, _ := evidence["files"].([]string)
	if len(files) != len(s.Files) {
		t.Errorf("accepted files were dropped: %v", files)
	}
	symbols, _ := evidence["symbols"].([]string)
	if len(symbols) != len(s.Symbols) {
		t.Errorf("symbols were dropped: %v", symbols)
	}
}

// Case 3 and 4 together. Actionable obligations outlive peripheral graph
// results: under pressure the breaking consumers are what remains.
func TestActionableObligationsOutlivePeripheralEvidence(t *testing.T) {
	s := planState(24, 30)
	_, sum := fitPlanEvidence(s, nil, func(b []byte) bool { return len(b) <= 20000 })

	if sum.ConsumersRetained == 0 {
		t.Fatal("every consumer was dropped")
	}
	peripheralRetained := sum.ConsumersRetained - sum.ActionableRetained
	if peripheralRetained > sum.ActionableRetained {
		t.Errorf("peripheral evidence (%d) outlived actionable evidence (%d)",
			peripheralRetained, sum.ActionableRetained)
	}
	// And what survived really is the actionable kind.
	evidence, _ := fitPlanEvidence(s, nil, func(b []byte) bool { return len(b) <= 20000 })
	im, ok := evidence["impact"].(*graph.Impact)
	if !ok {
		t.Fatal("the impact report is missing from the evidence")
	}
	var breaking int
	for _, c := range im.Consumers {
		if c.Verdict == graph.Breaking || c.Verdict == graph.Undetermined {
			breaking++
		}
	}
	if breaking == 0 {
		t.Error("no actionable consumer survived")
	}
	if !im.Truncated {
		t.Error("a truncated consumer list was not marked truncated")
	}
}

// A bounded consumer list is spread across files rather than taken from the
// front, so the planner is not handed one crowded file and nothing else.
func TestConsumerSelectionIsDiverseAcrossFiles(t *testing.T) {
	s := planState(12, 60)
	evidence, _ := fitPlanEvidence(s, nil, func(b []byte) bool { return len(b) <= 20000 })
	im, ok := evidence["impact"].(*graph.Impact)
	if !ok {
		t.Fatal("the impact report is missing")
	}
	files := map[string]bool{}
	for _, c := range im.Consumers {
		files[c.Node.Path] = true
	}
	if len(files) < 4 {
		t.Errorf("the retained consumers cover only %d file(s): %d kept from too few places",
			len(files), len(im.Consumers))
	}
}

// Case 5 and 6. A correction can always be added, and repeated corrections do
// not grow the evidence without bound.
func TestCorrectionsFitAndStayBounded(t *testing.T) {
	s := planState(24, 30)
	var sizes []int
	for round := range 8 {
		s.Feedback = append(s.Feedback,
			phaseFinding(fmt.Sprintf("correction %d: the plan named a file outside the allowlist; "+
				"%s", round, strings.Repeat("detail ", 40))))
		evidence, sum := fitPlanEvidence(s, nil, nil)
		body, err := json.Marshal(evidence)
		if err != nil {
			t.Fatal(err)
		}
		if len(body) > phaseEvidenceLimit {
			t.Fatalf("round %d: evidence is %d bytes, over budget", round, len(body))
		}
		if sum.FailuresRetained == 0 {
			t.Fatalf("round %d: the contradiction the model must resolve was dropped", round)
		}
		sizes = append(sizes, len(body))
	}
	// Bounded: the last round is not dramatically larger than the first.
	if sizes[len(sizes)-1] > sizes[0]*2 {
		t.Errorf("repeated corrections grew the evidence from %d to %d bytes",
			sizes[0], sizes[len(sizes)-1])
	}
}

// The contradiction kept is the most recent one: correcting a fault already
// corrected is how a plan loop stops converging.
func TestTheNewestCorrectionIsTheOneKept(t *testing.T) {
	s := planState(40, 40)
	for i := range 20 {
		s.Feedback = append(s.Feedback, phaseFinding(fmt.Sprintf("correction-%02d", i)))
	}
	// Squeeze hard enough that only a little survives.
	evidence, _ := fitPlanEvidence(s, nil, func(b []byte) bool { return len(b) <= 3000 })
	if planFailureCount(evidence) == 0 {
		t.Fatal("no correction survived")
	}
	body, err := json.Marshal(evidence["failures"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "correction-19") {
		t.Errorf("the newest correction was not the one kept: %s", body)
	}
}

// Case 7. Evidence that already fits is passed through unreduced.
func TestSmallPlanEvidenceIsUnchanged(t *testing.T) {
	s := &workflow.State{
		Hypothesis: "a small problem",
		Files:      []string{"a.go"},
		Symbols:    []string{"Add"},
		Bodies:     map[string]string{"a.go:Add": "func Add() {}"},
		Impact: &graph.Impact{
			Kind: graph.ChangeSignature, Caveat: graph.ImpactCaveat,
			Consumers: []graph.Consumer{{
				Node:  graph.Node{Path: "b.go", Name: "Caller", Kind: graph.KindFunction},
				Depth: 1, Via: graph.EdgeCalls, Evidence: graph.Resolved, Verdict: graph.Breaking,
			}},
		},
		Feedback: []recipe.Result{phaseFinding("one correction")},
	}
	evidence, sum := fitPlanEvidence(s, map[string]any{"operator_scope": []string{"."}}, nil)

	if sum.Dropped != 0 {
		t.Errorf("small evidence was reduced: %d dropped", sum.Dropped)
	}
	if sum.Truncated {
		t.Error("small evidence was marked truncated")
	}
	if _, present := evidence["evidence_truncated"]; present {
		t.Error("an abbreviation note was added to evidence that was not abbreviated")
	}
	// Everything is still there, unmodified.
	bodies, _ := evidence["bodies"].(map[string]string)
	if bodies["a.go:Add"] != "func Add() {}" {
		t.Errorf("a body that fitted was altered: %q", bodies["a.go:Add"])
	}
	im, _ := evidence["impact"].(*graph.Impact)
	if im == nil || len(im.Consumers) != 1 || im.Truncated {
		t.Errorf("an impact report that fitted was altered: %+v", im)
	}
	if planFailureCount(evidence) != 1 {
		t.Error("the correction was dropped from evidence that fitted")
	}
}

// The same state must produce the same evidence twice: a planner whose input
// varies run to run cannot be debugged.
func TestPlanEvidenceIsDeterministic(t *testing.T) {
	for range 5 {
		a, _ := fitPlanEvidence(planState(20, 25), nil, func(b []byte) bool { return len(b) <= 18000 })
		b, _ := fitPlanEvidence(planState(20, 25), nil, func(b []byte) bool { return len(b) <= 18000 })
		ja, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		jb, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		if string(ja) != string(jb) {
			t.Fatal("two fits of the same state produced different evidence")
		}
	}
}

// The replay proof: the evidence shape that killed GIN-1805 in PLAN, built
// the way the old code built it, and the same state through the new path.
//
// The failed runs' worktrees are gone and the eval store is ephemeral, so
// this reconstructs the condition rather than replaying the exact bytes: a
// gin-sized repository, a signature change, and an IMPACT that succeeded with
// a large consumer set — which is precisely the case that failed, and the
// case the run that reached EDIT did not hit because its IMPACT found
// nothing.
func TestPreviouslyOversizedPlanEvidenceNowFits(t *testing.T) {
	s := planState(24, 30)
	s.Feedback = []recipe.Result{
		phaseFinding("the plan named a file outside the write allowlist"),
		phaseFinding("tests must name at least one entry"),
	}

	// 1. The old construction: every field handed over as it stood. This is
	//    the literal map the Planning case used to build.
	old := map[string]any{
		"hypothesis": s.Hypothesis, "files": s.Files,
		"symbols": s.Symbols, "bodies": s.Bodies, "impact": s.Impact,
		"failures": s.Feedback, "operator_scope": []string{"."},
		"available_generators": []string{},
	}
	oldBody, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if len(oldBody) <= phaseEvidenceLimit {
		t.Fatalf("the fixture does not reproduce the failure: %d bytes is within the %d budget",
			len(oldBody), phaseEvidenceLimit)
	}
	t.Logf("old construction: %d bytes, over the %d budget by %d",
		len(oldBody), phaseEvidenceLimit, len(oldBody)-phaseEvidenceLimit)

	// 2. The new construction, against the byte ceiling.
	fitted, sum := fitPlanEvidence(s, map[string]any{
		"operator_scope": []string{"."}, "available_generators": []string{},
	}, nil)
	newBody, err := json.Marshal(fitted)
	if err != nil {
		t.Fatal(err)
	}
	if len(newBody) > phaseEvidenceLimit {
		t.Fatalf("PLAN_EVIDENCE_BUDGET_STILL_BROKEN: %d bytes, still over the %d budget",
			len(newBody), phaseEvidenceLimit)
	}
	t.Logf("new construction: %d bytes, %d of %d items retained (%d actionable of %d)",
		len(newBody), sum.Retained, sum.Available, sum.ActionableRetained, sum.ActionableAvailable)

	// 3. And against the tighter append-only log bound, which is the other
	//    error the failed runs produced.
	fittedTight, tightSum := fitPlanEvidence(s, map[string]any{
		"operator_scope": []string{"."}, "available_generators": []string{},
	}, func(b []byte) bool { return len(b) <= 24000 })
	tightBody, err := json.Marshal(fittedTight)
	if err != nil {
		t.Fatal(err)
	}
	if len(tightBody) > 24000 {
		t.Fatalf("PLAN_EVIDENCE_BUDGET_STILL_BROKEN under the log bound: %d bytes", len(tightBody))
	}
	t.Logf("under a 24000-byte log bound: %d bytes, %d actionable consumer(s) retained",
		len(tightBody), tightSum.ActionableRetained)

	// The planner is still given a plannable problem in both cases.
	for name, ev := range map[string]map[string]any{"byte ceiling": fitted, "log bound": fittedTight} {
		if got, _ := ev["hypothesis"].(string); got != s.Hypothesis {
			t.Errorf("%s: the hypothesis was lost", name)
		}
		if planFailureCount(ev) == 0 {
			t.Errorf("%s: the contradiction to resolve was lost", name)
		}
		im, _ := ev["impact"].(*graph.Impact)
		if im == nil || len(im.Consumers) == 0 {
			t.Errorf("%s: every consumer was dropped", name)
		}
	}
}
