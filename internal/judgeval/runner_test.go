package judgeval

import (
	"context"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/workflow"
)

// A Fake that always answers "yes, this weakens the check" for the first
// hunk in a batch — every generated mutation case has exactly one hunk, so
// its question ids are always h0.
func alwaysWeakensJudge() *judgment.Fake {
	return &judgment.Fake{
		RedactMode: judgment.RedactRepoText,
		Answers: map[string]judgment.Answer{
			"weakens_h0":    {Kind: judgment.KindNoul, Noul: 0.95, Answered: true},
			"legitimate_h0": {Kind: judgment.KindNoul, Noul: 0.05, Answered: true},
		},
	}
}

func alwaysLegitimateJudge() *judgment.Fake {
	return &judgment.Fake{
		RedactMode: judgment.RedactRepoText,
		Answers: map[string]judgment.Answer{
			"weakens_h0":    {Kind: judgment.KindNoul, Noul: 0.1, Answered: true},
			"legitimate_h0": {Kind: judgment.KindNoul, Noul: 0.9, Answered: true},
		},
	}
}

func mutationCasesFor(t *testing.T) []Case {
	t.Helper()
	cases := GenerateMutationCases("example_test.go", sampleTestSource, "fix Add", "1", SplitDev)
	if len(cases) == 0 {
		t.Fatal("no mutation cases generated")
	}
	return cases
}

func TestRunM4CampaignBaselineAndLocalControlAreHonestlyUnavailable(t *testing.T) {
	arms, err := RunM4Campaign(context.Background(), alwaysWeakensJudge(), mutationCasesFor(t), workflow.IntegrityTuning{})
	if err != nil {
		t.Fatal(err)
	}
	if arms[ArmDeterministicBaseline].Available {
		t.Fatal("no deterministic baseline exists for M4; this must report unavailable")
	}
	if arms[ArmDeterministicBaseline].UnavailableReason == "" {
		t.Fatal("an unavailable arm must name the blocker")
	}
	if arms[ArmLocalControl].Available {
		t.Fatal("no local control is wired for M4; this must report unavailable")
	}
	if arms[ArmLocalControl].UnavailableReason == "" {
		t.Fatal("an unavailable arm must name the blocker")
	}
}

func TestRunM4CampaignJevArmClassifiesEveryCaseUsingTheProductionAdapter(t *testing.T) {
	cases := mutationCasesFor(t)
	arms, err := RunM4Campaign(context.Background(), alwaysWeakensJudge(), cases, workflow.IntegrityTuning{})
	if err != nil {
		t.Fatal(err)
	}
	jev := arms[ArmJev]
	if !jev.Available {
		t.Fatal("expected the Jev arm to be available with a configured Fake judge")
	}
	if jev.N != len(cases) {
		t.Fatalf("N = %d, want %d", jev.N, len(cases))
	}
	if jev.Classification == nil {
		t.Fatal("expected a classification report")
	}
	// Every case's ground truth is "weakening" and the fake judge always
	// answers "weakens", so the classifier should have perfect recall on
	// the weakening class.
	var found bool
	for _, c := range jev.Classification.Classes {
		if c.Label == "weakening" {
			found = true
			if c.Recall != 1 {
				t.Errorf("recall = %v, want 1 (the fake judge always answers weakens)", c.Recall)
			}
		}
	}
	if !found {
		t.Fatal("expected a weakening class in the classification report")
	}
}

func TestRunM4CampaignJevArmMissesWhenTheJudgeDisagrees(t *testing.T) {
	cases := mutationCasesFor(t)
	arms, err := RunM4Campaign(context.Background(), alwaysLegitimateJudge(), cases, workflow.IntegrityTuning{})
	if err != nil {
		t.Fatal(err)
	}
	jev := arms[ArmJev]
	for _, c := range jev.Classification.Classes {
		if c.Label == "weakening" && c.Recall != 0 {
			t.Errorf("recall = %v, want 0 (the fake judge never answers weakens)", c.Recall)
		}
	}
}

func TestRunM4CampaignWithNoJudgeReportsUnavailableNotAnError(t *testing.T) {
	arms, err := RunM4Campaign(context.Background(), judgment.Off(), mutationCasesFor(t), workflow.IntegrityTuning{})
	if err != nil {
		t.Fatal(err)
	}
	if arms[ArmJev].Available {
		t.Fatal("Off() must report the Jev arm as unavailable")
	}
}

func TestAdapterRegisteredOnlyCoversVerificationIntegrity(t *testing.T) {
	if !AdapterRegistered(workflow.IntegritySite) {
		t.Error("expected verification_integrity to have a registered adapter")
	}
	if AdapterRegistered("review_rubric") {
		t.Error("no adapter is wired for review_rubric; this audit must not claim one")
	}
}

// The R2 fallback guarantee under instrumentation: every failure mode of the
// judge must leave CheckTestIntegrity's ordinary skip behaviour intact.
func TestFallbackTrialsNeverAttemptOrFindWithoutAWorkingJudge(t *testing.T) {
	hunks := []workflow.Hunk{{Path: "x_test.go", IsTest: true, Body: "@@ -1,1 +1,1 @@\n-a\n+b\n"}}
	expiredCtx := func() context.Context {
		ctx, cancel := context.WithTimeout(context.Background(), 0)
		cancel()
		time.Sleep(time.Millisecond)
		return ctx
	}
	trials := []FallbackTrial{
		{Name: "disabled", Judge: judgment.Off()},
		{Name: "nil judge", Judge: nil},
		{Name: "unavailable fake", Judge: &judgment.Fake{Unavailable: true}},
		{Name: "malformed/unanswered fake", Judge: &judgment.Fake{RedactMode: judgment.RedactRepoText}},
		{Name: "expired context", Judge: alwaysWeakensJudge(), Ctx: expiredCtx},
	}
	results := RunFallbackTrials(trials, "fix bug", hunks, workflow.IntegrityTuning{})
	if len(results) != len(trials) {
		t.Fatalf("got %d results, want %d", len(results), len(trials))
	}
	for _, r := range results {
		if !r.Completed {
			t.Errorf("trial %s did not complete", r.Name)
		}
		if r.AttemptedWithoutJudge {
			t.Errorf("trial %s: Attempted was true with no working judge — R2 violation", r.Name)
		}
		if r.FindingsWithNoJudge {
			t.Errorf("trial %s: a finding was reported with no working judge — R2 violation", r.Name)
		}
	}
}
