package evidence

import (
	"os"
	"testing"

	"github.com/akynte/boundedcode/internal/judgment"
)

// TestBuildTreatmentJudgeFromTheRealFrozenProfile proves the frozen
// evals/evidence-v1/jev-profile.yaml (never a production judgment.yaml)
// parses into a valid judgment.Config and constructs a real judgment.Judge
// — without ever making a network call, matching judgment.New's own
// construct-without-credential contract. This is GAP 3's treatment-arm
// wiring, proven structurally; the network round trip itself needs a real
// credential, which this task is forbidden from using.
func TestBuildTreatmentJudgeFromTheRealFrozenProfile(t *testing.T) {
	judge, err := BuildTreatmentJudge("../../evals/evidence-v1/jev-profile.yaml", "TYPESAFE_API_KEY", judgment.Deps{})
	if err != nil {
		t.Fatalf("BuildTreatmentJudge: %v", err)
	}
	if judge == nil {
		t.Fatal("BuildTreatmentJudge returned a nil Judge with no error")
	}
	if judge.Name() == "" {
		t.Fatal("constructed judge has no Name()")
	}
}

// TestBuildTreatmentJudgeRefusesAnOverGrantSitesCannotHave proves the same
// structural refusal judgment.Config.Validate already enforces for
// production judgment.yaml also applies to the frozen Evidence Suite
// profile: this package does not, and cannot, invent authority a site's own
// code does not implement.
func TestBuildTreatmentJudgeRefusesAnOverGrantSitesCannotHave(t *testing.T) {
	tmp := t.TempDir() + "/bad-profile.yaml"
	body := "schema_version: 1\nsuite: evidence-v1\njev_model: jev-1.13.0\nredact: repo_text\n" +
		"sites:\n  - site: intake_profile\n    site_version: \"1\"\n    authority: not-a-real-tier\n"
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildTreatmentJudge(tmp, "TYPESAFE_API_KEY", judgment.Deps{}); err == nil {
		t.Fatal("expected an invalid authority tier to be refused, got nil error")
	}
}

// TestGradingRegistryCoversEverySWEBenchProVerifiedTask proves GAP 3's
// official grading path has a real test command for all four frozen
// SWE-Bench Pro Verified tasks — a missing entry here would silently fall
// back to INFRA_ERROR mid-campaign rather than failing preflight.
func TestGradingRegistryCoversEverySWEBenchProVerifiedTask(t *testing.T) {
	entries, err := LoadRegistry("../../evals/evidence-v1/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Benchmark != BenchmarkSWEBenchProVerified {
			continue
		}
		if _, ok := GradingRegistry[e.TaskID]; !ok {
			t.Errorf("no GradingRegistry entry for SWE-Bench Pro Verified task %s", e.TaskID)
		}
	}
}
