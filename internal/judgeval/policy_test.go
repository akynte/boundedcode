package judgeval

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/workflow"
)

func TestDefaultPolicyMarksEveryThresholdUnconfigured(t *testing.T) {
	info, ok := judgment.Site(workflow.IntegritySite)
	if !ok {
		t.Fatal("verification_integrity is not registered; init() did not run")
	}
	p := DefaultPolicy(info)
	for name, th := range map[string]Threshold{
		"min_promotion_samples": p.MinPromotionSamples,
		"min_skill_lower_bound": p.MinSkillLowerBound,
		"max_flip_rate":         p.MaxFlipRate,
		"max_distractor_delta":  p.MaxDistractorDelta,
		"max_latency_p95":       p.MaxLatencyP95Seconds,
	} {
		if th.Configured() {
			t.Errorf("%s must default to unconfigured (RequiresSelection=true)", name)
		}
		if th.Note == "" {
			t.Errorf("%s must explain what it needs, even while unconfigured", name)
		}
	}
	if p.EffectClass != EffectRouting {
		t.Errorf("EffectClass = %q, want routing (verification_integrity's MaxEffect is routing)", p.EffectClass)
	}
	if !p.RequireBaselineBeaten {
		t.Error("a routing-class site should default to requiring the baseline be beaten")
	}
}

func TestLoadPolicyOnAMissingFileReturnsAllUnconfigured(t *testing.T) {
	dir := t.TempDir()
	pf, err := LoadPolicy(filepath.Join(dir, "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("a missing policy file must not be an error: %v", err)
	}
	info, _ := judgment.Site(workflow.IntegritySite)
	p := pf.For(info)
	if p.MinPromotionSamples.Configured() {
		t.Fatal("a missing file must yield an unconfigured threshold, not a guessed default")
	}
}

func TestWriteDefaultPolicyThenLoadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	if err := WriteDefaultPolicy(path); err != nil {
		t.Fatal(err)
	}
	pf, err := LoadPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(pf.Sites) != len(judgment.KnownSites()) {
		t.Fatalf("wrote %d sites, want %d (one per registered site)", len(pf.Sites), len(judgment.KnownSites()))
	}
}

func TestLoadPolicyRespectsAnOperatorConfiguredThreshold(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	body := "schema_version: 1\n" +
		"sites:\n" +
		"  verification_integrity:\n" +
		"    site: verification_integrity\n" +
		"    effect_class: routing\n" +
		"    min_reportable_samples: 20\n" +
		"    min_promotion_samples:\n" +
		"      value: 200\n" +
		"      requires_selection: false\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	pf, err := LoadPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := judgment.Site(workflow.IntegritySite)
	p := pf.For(info)
	if !p.MinPromotionSamples.Configured() || p.MinPromotionSamples.Value != 200 {
		t.Fatalf("MinPromotionSamples = %+v, want {200, configured}", p.MinPromotionSamples)
	}
}

func TestLoadPolicyRejectsWrongSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 99\nsites: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPolicy(path); err == nil {
		t.Fatal("expected an error for a mismatched schema_version")
	}
}
