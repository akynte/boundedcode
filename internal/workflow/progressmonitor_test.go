package workflow_test

import (
	"context"
	"testing"

	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/workflow"
)

func TestCheckProgressSkipsUnderStrictRedaction(t *testing.T) {
	j := &judgment.Fake{RedactMode: judgment.RedactStrict}
	reads := []workflow.ReadEvidence{{Tool: "read_file", Path: "a.go", Seq: 1, Excerpt: "func A() {}"}}
	res, err := workflow.CheckProgress(context.Background(), j, []string{"a.go"}, nil, reads, workflow.ProgressTuning{}, nil)
	if res.Attempted || res.SkipReason != "redact_strict" {
		t.Fatalf("result = %+v", res)
	}
	// There was a window to judge and this site may not send it. That is a
	// refusal to report, not a silent skip.
	if class, ok := judgment.FailureOf(err); !ok || class != judgment.FailureRefused {
		t.Fatalf("class = %q (ok %v), want %q", class, ok, judgment.FailureRefused)
	}
}

func TestCheckProgressSkipsWithNoEvidence(t *testing.T) {
	j := &judgment.Fake{RedactMode: judgment.RedactRepoText}
	res, err := workflow.CheckProgress(context.Background(), j, nil, nil, nil, workflow.ProgressTuning{}, nil)
	if err != nil {
		t.Fatalf("an empty window has nothing to decide: %v", err)
	}
	if res.Attempted || res.SkipReason != "no_evidence" {
		t.Fatalf("result = %+v", res)
	}
}

func TestCheckProgressClassifiesCircling(t *testing.T) {
	j := &judgment.Fake{
		RedactMode: judgment.RedactRepoText,
		Answers:    map[string]judgment.Answer{"category": {Choice: "circling", Confidence: 0.9}},
	}
	reads := []workflow.ReadEvidence{
		{Tool: "read_file", Path: "order.go", Detail: "1-80", Excerpt: "func Checkout", Seq: 1},
		{Tool: "read_file", Path: "order.go", Detail: "1-80", Excerpt: "func Checkout", Seq: 2},
	}
	res, err := workflow.CheckProgress(context.Background(), j, []string{"order.go"}, []string{"Checkout"},
		reads, workflow.ProgressTuning{}, nil)
	if err != nil {
		t.Fatalf("CheckProgress: %v", err)
	}

	if !res.Attempted || !res.Applied {
		t.Fatalf("result = %+v", res)
	}
	if res.Category != workflow.ProgressCircling || res.Confidence != 0.9 {
		t.Fatalf("result = %+v", res)
	}
	if res.WindowSize != 2 {
		t.Fatalf("WindowSize = %d, want 2", res.WindowSize)
	}
}

func TestCheckProgressCapsWindowSize(t *testing.T) {
	var reads []workflow.ReadEvidence
	for i := 0; i < 30; i++ {
		reads = append(reads, workflow.ReadEvidence{Tool: "read_file", Path: "a.go", Seq: i})
	}
	j := &judgment.Fake{RedactMode: judgment.RedactRepoText, Answers: map[string]judgment.Answer{"category": {Choice: "advancing", Confidence: 0.6}}}
	res, _ := workflow.CheckProgress(context.Background(), j, nil, nil, reads,
		workflow.ProgressTuning{WindowSize: 5}, nil)
	if res.WindowSize != 5 {
		t.Fatalf("WindowSize = %d, want 5 (capped)", res.WindowSize)
	}
}

func TestCheckProgressDefaultTierIsLogged(t *testing.T) {
	j := &judgment.Fake{RedactMode: judgment.RedactRepoText, Answers: map[string]judgment.Answer{"category": {Choice: "advancing", Confidence: 0.6}}}
	reads := []workflow.ReadEvidence{{Tool: "read_file", Path: "a.go", Seq: 1}}
	res, _ := workflow.CheckProgress(context.Background(), j, nil, nil, reads, workflow.ProgressTuning{}, nil)
	if res.Tier != judgment.TierLogged {
		t.Fatalf("Tier = %q, want logged", res.Tier)
	}
}

// The operator's confidence floor governs a batch site too. Without this the
// floor would apply to advisory single-question sites and not to the ones that
// can end an attempt, which is the wrong way round.
func TestCheckProgressRefusesAnAnswerBelowTheConfidenceFloor(t *testing.T) {
	j := &judgment.Fake{
		RedactMode: judgment.RedactRepoText,
		// Fake.MinConfidence is 0.5.
		Answers: map[string]judgment.Answer{"category": {Choice: "circling", Confidence: 0.3}},
	}
	reads := []workflow.ReadEvidence{{Tool: "read_file", Path: "a.go", Seq: 1, Excerpt: "func A() {}"}}
	res, err := workflow.CheckProgress(context.Background(), j, nil, nil, reads,
		workflow.ProgressTuning{}, nil)

	if res.Applied {
		t.Errorf("an answer below the floor must not be applied: %+v", res)
	}
	if res.Category != "" {
		t.Errorf("Category = %q, want empty", res.Category)
	}
	if class, ok := judgment.FailureOf(err); !ok || class != judgment.FailureLowConfidence {
		t.Fatalf("class = %q (ok %v), want %q", class, ok, judgment.FailureLowConfidence)
	}
}

// A healthy transport that answers nothing is not a decision.
func TestCheckProgressFailsWhenNoCategoryCameBack(t *testing.T) {
	j := &judgment.Fake{RedactMode: judgment.RedactRepoText, Answers: map[string]judgment.Answer{}}
	reads := []workflow.ReadEvidence{{Tool: "read_file", Path: "a.go", Seq: 1, Excerpt: "func A() {}"}}
	res, err := workflow.CheckProgress(context.Background(), j, nil, nil, reads,
		workflow.ProgressTuning{}, nil)
	if res.Applied {
		t.Errorf("result = %+v", res)
	}
	if _, ok := judgment.FailureOf(err); !ok {
		t.Fatalf("want a requirement failure, got %v", err)
	}
}
