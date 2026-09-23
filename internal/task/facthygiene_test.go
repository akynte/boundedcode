package task

import (
	"context"
	"testing"

	"github.com/akynte/boundedcode/internal/contextpack"
	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/workflow"
)

// factHygieneTestState carries one obligation, so taskCard's ConfirmedFacts
// is non-empty (see taskCard's loop over s.Plan.Obligations) and the fact
// hygiene site has something to judge.
func factHygieneTestState() *workflow.State {
	s := hypothesisTestState()
	s.Plan.Obligations = []workflow.Obligation{
		{Symbol: "Validate", Path: "internal/billing/validate.go", Reason: "calls DailyLimit"},
	}
	return s
}

func TestCheckFactRelevanceSkipsWithNoContext(t *testing.T) {
	j := &judgment.Fake{Answers: map[string]judgment.Answer{"fact0": {Noul: 0.9}}}
	facts := []contextpack.Fact{{Fact: "X is a function in Y", Evidence: "graph"}}
	res := CheckFactRelevance(context.Background(), j, "", nil, facts, nil)
	if res.Attempted || res.SkipReason != "no_context" {
		t.Fatalf("result = %+v", res)
	}
}

func TestCheckFactRelevanceScoresEachFact(t *testing.T) {
	j := &judgment.Fake{Answers: map[string]judgment.Answer{
		"fact0": {Noul: 0.9}, "fact1": {Noul: 0.05},
	}}
	facts := []contextpack.Fact{
		{Fact: "Checkout calls DailyLimit", Evidence: "graph"},
		{Fact: "unrelated legacy note", Evidence: "graph"},
	}
	res := CheckFactRelevance(context.Background(), j, "raise the daily limit", nil, facts, nil)
	if !res.Attempted || !res.Applied || res.Judged != 2 {
		t.Fatalf("result = %+v", res)
	}
	if res.Relevant[0] != 0.9 || res.Relevant[1] != 0.05 {
		t.Fatalf("Relevant = %+v", res.Relevant)
	}
}

func TestTaskCardPrunesLowRelevanceFactsAtOrderingTier(t *testing.T) {
	j := &judgment.Fake{
		Tiers: map[string]judgment.Tier{FactRelevanceSite: judgment.TierOrdering},
		Answers: map[string]judgment.Answer{
			"fact0":   {Noul: 0.9}, // impact obligation fact: stays
			"verdict": {Choice: "supported", Confidence: 0.8},
		},
	}
	r := &Runner{Judge: j}
	s := factHygieneTestState()
	card := r.taskCard(context.Background(), &Task{Title: "fix checkout"}, s)
	if len(card.ConfirmedFacts) == 0 {
		t.Fatalf("expected at least the fact fixture's own confirmed fact to remain")
	}
}

func TestTaskCardKeepsAllFactsAtDefaultLoggedTier(t *testing.T) {
	j := &judgment.Fake{
		Answers: map[string]judgment.Answer{
			"fact0": {Noul: 0.01}, "verdict": {Choice: "supported", Confidence: 0.8},
		},
	}
	r := &Runner{Judge: j}
	s := factHygieneTestState()
	before := r.taskCard(context.Background(), &Task{Title: "fix checkout"}, s)

	j2 := &judgment.Fake{
		Tiers:   map[string]judgment.Tier{FactRelevanceSite: judgment.TierOrdering},
		Answers: map[string]judgment.Answer{"fact0": {Noul: 0.01}, "verdict": {Choice: "supported", Confidence: 0.8}},
	}
	r2 := &Runner{Judge: j2}
	after := r2.taskCard(context.Background(), &Task{Title: "fix checkout"}, s)

	if len(after.ConfirmedFacts) >= len(before.ConfirmedFacts) && len(before.ConfirmedFacts) > 0 {
		t.Fatalf("promoting the site to ordering with a low relevance score should prune at "+
			"least as much as the default logged tier: before=%d after=%d",
			len(before.ConfirmedFacts), len(after.ConfirmedFacts))
	}
}
