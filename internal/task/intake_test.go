package task

import (
	"context"
	"testing"

	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/recipe"
)

func TestCheckIntakeProfileWithNoJudgeChangesNothing(t *testing.T) {
	res, err := CheckIntakeProfile(context.Background(), judgment.Off(), "add a retry", nil, IntakeTuning{}, nil)
	if res.Attempted || res.SkipReason != "no_judge" {
		t.Fatalf("result = %+v", res)
	}
	// The failure is reported, but at the default logged tier it is not a
	// stop: FailClosed is what turns one into the other.
	if class, _ := judgment.FailureOf(err); class != judgment.FailureNotConfigured {
		t.Fatalf("class = %q, want %q", class, judgment.FailureNotConfigured)
	}
	if _, mustStop := judgment.FailClosed(IntakeSite, res.Tier, err); mustStop {
		t.Fatal("a site at the default logged tier must not stop a task")
	}
}

func TestCheckIntakeProfileReadsNoulsAboveTheFloor(t *testing.T) {
	j := &judgment.Fake{Answers: map[string]judgment.Answer{
		"needs_dependency": {Noul: 0.9},
		"needs_migration":  {Noul: 0.1},
		"ambiguous":        {Noul: 0.55}, // below the default floor: not a finding
		"blast_radius":     {Score: 2.0},
	}}
	presets := []recipe.Preset{{Name: "test", Kind: recipe.KindTest}}
	res, err := CheckIntakeProfile(context.Background(), j, "add rate limiting using a new library",
		presets, IntakeTuning{}, nil)
	if err != nil {
		t.Fatalf("CheckIntakeProfile: %v", err)
	}

	if !res.Attempted || !res.Applied {
		t.Fatalf("result = %+v", res)
	}
	if !res.NeedsDependency || res.NeedsDependencyP != 0.9 {
		t.Fatalf("NeedsDependency = %v/%v, want true/0.9", res.NeedsDependency, res.NeedsDependencyP)
	}
	if res.NeedsMigration {
		t.Fatalf("NeedsMigration must be false: %+v", res)
	}
	if res.Ambiguous {
		t.Fatalf("a Noul at 0.55 is below the floor and must not count as ambiguous: %+v", res)
	}
	if !res.BlastRadiusAnswered || res.BlastRadius != BlastSeveral {
		t.Fatalf("BlastRadius = %q (answered=%v), want several_packages/true",
			res.BlastRadius, res.BlastRadiusAnswered)
	}
}

func TestCheckIntakeProfileDefaultTierIsLogged(t *testing.T) {
	j := &judgment.Fake{Answers: map[string]judgment.Answer{"blast_radius": {Score: 0}}}
	res, _ := CheckIntakeProfile(context.Background(), j, "obj", nil, IntakeTuning{}, nil)
	if res.Tier != judgment.TierLogged {
		t.Fatalf("Tier = %q, want logged", res.Tier)
	}
}

// An empty objective is nothing to decide, not a decision that failed. The
// distinction is what keeps a mandatory site from blocking tasks it has no
// question about.
func TestAnEmptyObjectiveIsNotARequirementFailure(t *testing.T) {
	j := &judgment.Fake{Answers: map[string]judgment.Answer{}}
	res, err := CheckIntakeProfile(context.Background(), j, "   ", nil, IntakeTuning{}, nil)
	if err != nil {
		t.Fatalf("no objective is not a failed decision: %v", err)
	}
	if res.SkipReason != "no_objective" {
		t.Fatalf("SkipReason = %q", res.SkipReason)
	}
}
