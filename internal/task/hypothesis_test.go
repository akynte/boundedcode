package task

import (
	"context"
	"testing"

	"github.com/akynte/boundedcode/internal/contextpack"
	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/workflow"
)

// hypothesisTestState is a minimal workflow.State with just enough evidence
// (an open failure) for CheckHypothesis to be attempted from taskCard: a
// RootCause to become card.Hypothesis, and a non-empty Failures map to
// become card.OpenFailures.
func hypothesisTestState() *workflow.State {
	return &workflow.State{
		Plan:     workflow.Plan{RootCause: "the nil check is missing"},
		Failures: map[string]int{"fp1": 1},
	}
}

func TestCheckHypothesisSkipsWithNoEvidence(t *testing.T) {
	j := &judgment.Fake{Answers: map[string]judgment.Answer{"verdict": {Choice: "supported", Confidence: 0.9}}}
	res := CheckHypothesis(context.Background(), j, "the nil check is missing", nil, nil, nil)
	if res.Attempted || res.SkipReason != "no_evidence" {
		t.Fatalf("result = %+v", res)
	}
	if len(j.Calls()) != 0 {
		t.Fatalf("no evidence must place no call")
	}
}

func TestCheckHypothesisContradicted(t *testing.T) {
	j := &judgment.Fake{Answers: map[string]judgment.Answer{"verdict": {Choice: "contradicted", Confidence: 0.88}}}
	facts := []contextpack.Fact{{Fact: "Validate already checks for nil", Evidence: "graph"}}
	res := CheckHypothesis(context.Background(), j, "the nil check is missing", facts, nil, nil)

	if !res.Attempted || !res.Applied || !res.Answered {
		t.Fatalf("result = %+v", res)
	}
	if res.Verdict != HypothesisVerdictContradicted || res.Confidence != 0.88 {
		t.Fatalf("result = %+v", res)
	}
	// Strict tier: no repository source should have gone out.
	if got := j.RepoTextSent(); got != 0 {
		t.Fatalf("repo_text fields sent = %d, want 0", got)
	}
}

func TestTaskCardDropsAContradictedHypothesisAtOrderingTier(t *testing.T) {
	j := &judgment.Fake{
		Tiers:   map[string]judgment.Tier{HypothesisSite: judgment.TierOrdering},
		Answers: map[string]judgment.Answer{"verdict": {Choice: "contradicted", Confidence: 0.9}},
	}
	r := &Runner{Judge: j}
	s := hypothesisTestState()
	card := r.taskCard(context.Background(), &Task{Title: "fix checkout"}, s)
	if card.Hypothesis != "" {
		t.Fatalf("a contradicted hypothesis (ordering tier) must be dropped, got %q", card.Hypothesis)
	}
	if card.HypothesisStatus != contextpack.HypothesisContradicted {
		t.Fatalf("HypothesisStatus = %q, want contradicted", card.HypothesisStatus)
	}
	found := false
	for _, d := range card.Decisions {
		if d != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("dropping a hypothesis must leave a record in Decisions: %+v", card.Decisions)
	}
}

func TestTaskCardKeepsHypothesisAtDefaultLoggedTier(t *testing.T) {
	j := &judgment.Fake{
		// No Tiers set: HypothesisSite defaults to TierLogged.
		Answers: map[string]judgment.Answer{"verdict": {Choice: "contradicted", Confidence: 0.9}},
	}
	r := &Runner{Judge: j}
	s := hypothesisTestState()
	card := r.taskCard(context.Background(), &Task{Title: "fix checkout"}, s)
	if card.Hypothesis == "" {
		t.Fatalf("at TierLogged the hypothesis must be left exactly as computed deterministically")
	}
	if card.HypothesisStatus != "" {
		t.Fatalf("at TierLogged HypothesisStatus must stay unset, got %q", card.HypothesisStatus)
	}
}
