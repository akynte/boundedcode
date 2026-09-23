package retrieval_test

import (
	"context"
	"testing"

	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/retrieval"
)

// Mechanism M9: a second relevance proposition keyed on an open failure,
// combined with the objective's by taking the greater of the two.

func TestRerankWithoutFailureAsksOnlyOneProposition(t *testing.T) {
	in := []retrieval.Slice{slice("a.go", "A", retrieval.OriginAnchor, 3)}
	j := &judgment.Fake{Answers: map[string]judgment.Answer{"c0": {Noul: 0.4}}}
	res := retrieval.Rerank(context.Background(), j, "do the thing", in, retrieval.RerankTuning{}, nil)
	if !res.Applied || in[0].RelevanceSource != "" {
		t.Fatalf("no FailureQuery was set; RelevanceSource must stay empty: res=%+v slice=%+v", res, in[0])
	}
}

func TestRerankWithFailureTakesTheGreaterProbability(t *testing.T) {
	in := []retrieval.Slice{
		slice("a.go", "A", retrieval.OriginAnchor, 3),  // low on objective, high on failure
		slice("b.go", "B", retrieval.OriginGraph, 0.5), // high on objective, low on failure
	}
	j := &judgment.Fake{Answers: map[string]judgment.Answer{
		"c0": {Noul: 0.1}, "fc0": {Noul: 0.9},
		"c1": {Noul: 0.8}, "fc1": {Noul: 0.05},
	}}
	tn := retrieval.RerankTuning{FailureQuery: retrieval.FailureQuery{
		Headline: "nil pointer in Checkout", Symbols: "Checkout",
	}}
	res := retrieval.Rerank(context.Background(), j, "do the thing", in, tn, nil)
	if !res.Attempted || !res.Applied {
		t.Fatalf("result = %+v", res)
	}
	if in[0].Relevance != 0.9 || in[0].RelevanceSource != "failure" {
		t.Fatalf("candidate a.go: relevance=%v source=%q, want 0.9/failure", in[0].Relevance, in[0].RelevanceSource)
	}
	if in[1].Relevance != 0.8 || in[1].RelevanceSource != "objective" {
		t.Fatalf("candidate b.go: relevance=%v source=%q, want 0.8/objective", in[1].Relevance, in[1].RelevanceSource)
	}
}

func TestRerankFailureFactNeverLeavesUnderStrictWithoutObjectiveField(t *testing.T) {
	// The failure fact must be carried as a TrustedFact alongside the
	// objective, not as repository text; it must still work at redact:
	// strict, the default.
	in := []retrieval.Slice{slice("a.go", "A", retrieval.OriginAnchor, 3)}
	j := &judgment.Fake{Answers: map[string]judgment.Answer{"c0": {Noul: 0.5}, "fc0": {Noul: 0.5}}}
	tn := retrieval.RerankTuning{FailureQuery: retrieval.FailureQuery{Headline: "boom"}}
	res := retrieval.Rerank(context.Background(), j, "do the thing", in, tn, nil)
	if !res.Applied {
		t.Fatalf("strict mode must still allow a failure-keyed proposition (it is metadata-shaped text): %+v", res)
	}
	if got := j.RepoTextSent(); got != 0 {
		t.Fatalf("strict mode sent %d repo_text field(s), want 0", got)
	}
}
