package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	// The judgment site registry is populated by the init() of each package
	// that defines a site, the way sql drivers register themselves. This
	// package reads that registry but does not otherwise import them, so a
	// test binary has to link them explicitly or every site reads as unknown
	// and the checks below would pass for the wrong reason. cmd/bcode links all
	// three already.
	"github.com/akynte/boundedcode/internal/judgment"

	_ "github.com/akynte/boundedcode/internal/retrieval"
	_ "github.com/akynte/boundedcode/internal/task"
	_ "github.com/akynte/boundedcode/internal/workflow"
)

// A malformed judgment.yaml sites tier must be reported by `bcode doctor` even
// when enabled is false. Before this, checkJudgment returned OK the moment
// it saw !jcfg.Enabled, without ever calling Validate — so a typo staged
// ahead of turning the integration on was invisible until it was too late
// to catch cheaply.
func TestJudgmentDoctorCheckCatchesAMalformedTierWhileDisabled(t *testing.T) {
	dir := t.TempDir()
	body := "enabled: false\nsites:\n  review_rubric: routng\n"
	if err := os.WriteFile(filepath.Join(dir, "judgment.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	c := checkJudgment(context.Background(), nil, dir)

	if c.Level != Fail {
		t.Fatalf("a malformed tier must fail the check even while disabled, got %v: %s", c.Level, c.Detail)
	}
	if !strings.Contains(c.Detail, "routng") {
		t.Errorf("the detail must name the offending value: %q", c.Detail)
	}
}

// The decision plane is required, so a disabled one is a fault however
// well-formed the rest of the file is: no task can run.
func TestJudgmentDoctorCheckFailsWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	body := "enabled: false\nsites:\n  review_rubric: ordering\n"
	if err := os.WriteFile(filepath.Join(dir, "judgment.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	c := checkJudgment(context.Background(), nil, dir)

	if c.Level != Fail {
		t.Fatalf("a disabled decision plane must fail, got %v: %s", c.Level, c.Detail)
	}
	if !strings.Contains(c.Detail, "refuse to start") {
		t.Errorf("the detail must say what stops working: %q", c.Detail)
	}
}

// An absent judgment.yaml is the same state as a disabled one.
func TestJudgmentDoctorCheckFailsWithNoFileAtAll(t *testing.T) {
	dir := t.TempDir()

	c := checkJudgment(context.Background(), nil, dir)

	if c.Level != Fail {
		t.Fatalf("an absent judgment.yaml leaves no decision plane, got %v: %s", c.Level, c.Detail)
	}
}

// --- what the mandatory sites make of the same configurations --------------

func writeJudgmentConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "judgment.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Which sites can stop a task is derived from `redact`, not written down, so
// the report has to compute it the same way the runtime does.
func TestRoutingSitesFollowTheRedactionMode(t *testing.T) {
	for _, tc := range []struct {
		mode        judgment.RedactMode
		wantRouting []string
		wantAbsent  []string
	}{
		{judgment.RedactStrict,
			[]string{"intake_profile", "obligation_reason"},
			[]string{"verification_integrity", "diff_conformance", "progress_monitor", "failure_triage"}},
		{judgment.RedactRepoText,
			[]string{"intake_profile", "obligation_reason", "verification_integrity",
				"diff_conformance", "progress_monitor"},
			[]string{"failure_triage"}},
		{judgment.RedactOutput,
			[]string{"intake_profile", "obligation_reason", "verification_integrity",
				"diff_conformance", "progress_monitor", "failure_triage"},
			nil},
	} {
		t.Run(string(tc.mode), func(t *testing.T) {
			got := requiredRoutingSites(judgment.Config{Redact: tc.mode})
			has := func(s string) bool {
				for _, g := range got {
					if g == s {
						return true
					}
				}
				return false
			}
			for _, want := range tc.wantRouting {
				if !has(want) {
					t.Errorf("%s should route under %s; got %v", want, tc.mode, got)
				}
			}
			for _, absent := range tc.wantAbsent {
				if has(absent) {
					t.Errorf("%s cannot run under %s and must not route; got %v", absent, tc.mode, got)
				}
			}
		})
	}
}

// A default must never produce a configuration that contradicts itself: a site
// promoted by default to a tier its redaction mode forbids would refuse every
// task that reached it.
func TestTheShippedDefaultsAreNotSelfContradictory(t *testing.T) {
	for _, mode := range []judgment.RedactMode{
		judgment.RedactStrict, judgment.RedactRepoText, judgment.RedactOutput,
	} {
		if c := redactContradictions(judgment.Config{Redact: mode}); len(c) != 0 {
			t.Errorf("redact %s: defaults contradict themselves: %v", mode, c)
		}
	}
}

// A site promised authority over a decision the redact mode forbids it from
// ever making. Nothing at runtime can resolve this, so the report has to.
func TestJudgmentDoctorCheckFailsOnARedactContradiction(t *testing.T) {
	t.Setenv("BC_DOCTOR_TEST_KEY", "a-credential")
	dir := writeJudgmentConfig(t, "enabled: true\n"+
		"endpoint: https://api.example.test/v1/systemone\n"+
		"model: jev-1.13.0\n"+
		"redact: strict\n"+
		"api_key_env: BC_DOCTOR_TEST_KEY\n"+
		"sites:\n  verification_integrity: routing\n")

	c := checkJudgment(context.Background(), nil, dir)

	if c.Level != Fail {
		t.Fatalf("level = %v, want Fail: %s", c.Level, c.Detail)
	}
	if !strings.Contains(c.Detail, "verification_integrity") {
		t.Errorf("the detail must name the site: %q", c.Detail)
	}
	if !strings.Contains(c.Detail, "repo_text") {
		t.Errorf("the detail must name the mode the site needs: %q", c.Detail)
	}
}

// The same site at the default tier under the same strict mode is fine: it
// skips, and skipping is what a logged site is for.
func TestJudgmentDoctorCheckAllowsAStrictModeWhenNothingRoutes(t *testing.T) {
	t.Setenv("BC_DOCTOR_TEST_KEY", "a-credential")
	dir := writeJudgmentConfig(t, "enabled: true\n"+
		"endpoint: https://api.example.test/v1/systemone\n"+
		"model: jev-1.13.0\n"+
		"redact: strict\n"+
		"api_key_env: BC_DOCTOR_TEST_KEY\n")

	c := checkJudgment(context.Background(), nil, dir)

	if c.Level == Fail && strings.Contains(c.Detail, "routing") {
		t.Fatalf("nothing is at routing; this must not be a contradiction: %s", c.Detail)
	}
}

// A missing credential is a fault, not a warning: the plane is required, so a
// judge that cannot authenticate is a system that cannot run a task.
func TestJudgmentDoctorCheckFailsOnAMissingCredential(t *testing.T) {
	dir := writeJudgmentConfig(t, "enabled: true\n"+
		"endpoint: https://api.example.test/v1/systemone\n"+
		"model: jev-1.13.0\n"+
		"redact: repo_text\n"+
		"api_key_env: BC_DOCTOR_ABSENT_KEY\n")

	c := checkJudgment(context.Background(), nil, dir)

	if c.Level != Fail {
		t.Fatalf("level = %v, want Fail: %s", c.Level, c.Detail)
	}
	if !strings.Contains(c.Detail, "BC_DOCTOR_ABSENT_KEY") {
		t.Errorf("the detail must name the variable to export: %q", c.Detail)
	}
}
