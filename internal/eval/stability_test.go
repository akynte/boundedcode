package eval

import (
	"context"
	"errors"
	"testing"

	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/retrieval"
	"github.com/akynte/boundedcode/internal/workspace"
)

// A stability diagnostic against a stub would report perfect stability,
// which is both true and worthless. It must refuse.
func TestStabilityRefusesWithoutALiveJudge(t *testing.T) {
	for name, judge := range map[string]judgment.Judge{
		"no judge":            nil,
		"a judge that is off": judgment.Off(),
		"an unavailable fake": &judgment.Fake{Unavailable: true},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := RunStability(context.Background(), StabilityInput{
				Judge:      judge,
				Objective:  "x",
				Candidates: []retrieval.Slice{{Path: "a.go"}, {Path: "b.go"}},
			})
			if !errors.Is(err, ErrNoLiveJudge) {
				t.Fatalf("err = %v, want ErrNoLiveJudge", err)
			}
		})
	}
}

func stabilitySlice(path string) retrieval.Slice {
	return retrieval.Slice{
		WorkspaceID: workspace.ID("ws"), RepositoryID: "r", WorktreeID: "wt",
		Path: path, PathKnown: true, Symbol: "S", ContentHash: "h", IndexVersion: 1,
		Origin: retrieval.OriginAnchor, Score: 1, StartLine: 1, EndLine: 5,
	}
}

// A judge that answers identically whatever the request looks like must
// produce a perfectly stable report — the arithmetic has to be right before
// a real distribution means anything.
func TestStabilityOnAConstantJudgeIsPerfect(t *testing.T) {
	answers := map[string]judgment.Answer{}
	for i := range 40 {
		answers["c"+itoa(i)] = judgment.Answer{Noul: 0.5}
	}
	rep, err := RunStability(context.Background(), StabilityInput{
		Judge:     &judgment.Fake{Answers: answers},
		Objective: "make the limit configurable",
		Candidates: []retrieval.Slice{
			stabilitySlice("a.go"), stabilitySlice("b.go"),
			stabilitySlice("c.go"), stabilitySlice("d.go"),
		},
		Distractors: []retrieval.Slice{stabilitySlice("LICENSE.go")},
		Repeats:     2, Seed: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[StabilityKind]bool{}
	for _, trial := range rep.Trials {
		if trial.Note != "" {
			t.Fatalf("%s: %s", trial.Kind, trial.Note)
		}
		kinds[trial.Kind] = true
		if trial.MaxAbsDelta != 0 {
			t.Errorf("%s: a constant judge moved a score by %v", trial.Kind, trial.MaxAbsDelta)
		}
	}
	for _, want := range []StabilityKind{
		StabilityPermutation, StabilityBatch, StabilityDistractor,
	} {
		if !kinds[want] {
			t.Errorf("%s was never exercised", want)
		}
	}
	if rep.Model == "" {
		t.Error("the model was not recorded; a stability figure without one is not reusable")
	}
}

// The distractor trial must compare only the original candidates: the
// distractors have no reference score and including them would measure
// nothing.
func TestDistractorTrialComparesOnlyTheOriginals(t *testing.T) {
	answers := map[string]judgment.Answer{}
	for i := range 40 {
		answers["c"+itoa(i)] = judgment.Answer{Noul: 0.5}
	}
	rep, err := RunStability(context.Background(), StabilityInput{
		Judge:       &judgment.Fake{Answers: answers},
		Objective:   "o",
		Candidates:  []retrieval.Slice{stabilitySlice("a.go"), stabilitySlice("b.go")},
		Distractors: []retrieval.Slice{stabilitySlice("x.go"), stabilitySlice("y.go")},
		Repeats:     1, Seed: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, trial := range rep.Trials {
		if trial.Kind != StabilityDistractor {
			continue
		}
		if trial.Candidates != 2 {
			t.Fatalf("the distractor trial compared %d candidates, want the 2 originals",
				trial.Candidates)
		}
	}
}

// The summary is what a distribution is read from, so its worst-case figures
// must be worst-case rather than averages.
func TestStabilitySummaryKeepsTheWorstCase(t *testing.T) {
	rep := StabilityReport{Trials: []StabilityTrial{
		{Kind: StabilityPermutation, MeanAbsDelta: 0.01, MaxAbsDelta: 0.02, SpearmanRho: 1.0, TopKOverlap: 1.0},
		{Kind: StabilityPermutation, MeanAbsDelta: 0.30, MaxAbsDelta: 0.60, SpearmanRho: 0.1, TopKOverlap: 0.5},
	}}
	s := rep.Summarise()
	if len(s) != 1 {
		t.Fatalf("got %d summaries", len(s))
	}
	if s[0].WorstAbsDelta != 0.60 {
		t.Errorf("worst |Δ| = %v, want 0.60", s[0].WorstAbsDelta)
	}
	if s[0].WorstRho != 0.1 {
		t.Errorf("worst ρ = %v, want 0.1", s[0].WorstRho)
	}
	if s[0].WorstTopK != 0.5 {
		t.Errorf("worst top-K = %v, want 0.5", s[0].WorstTopK)
	}
}
