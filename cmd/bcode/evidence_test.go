package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The frozen Evidence Suite v1 hashes, restated here so a change to either
// file is caught by this test suite too, independent of the command's own
// hardcoded constants matching by construction.
const (
	wantEvidenceManifestSHA = "758b95266f0eed0552bc84b6f4b13c5fbee2fa0095456bcacd0e7b0e8d490f56"
	wantEvidenceProfileSHA  = "3e40d1f9569a45d896455e1cc4509577635749b638a91193225c03516a17ac14"
)

func TestFrozenArtifactConstantsMatchTheRepositoryFiles(t *testing.T) {
	root, err := findRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	manifestSHA, err := sha256File(filepath.Join(root, evidenceManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	if manifestSHA != wantEvidenceManifestSHA {
		t.Fatalf("evals/evidence-v1/manifest.json hash changed: got %s, want %s (frozen — must "+
			"not be modified)", manifestSHA, wantEvidenceManifestSHA)
	}
	profileSHA, err := sha256File(filepath.Join(root, evidenceProfilePath))
	if err != nil {
		t.Fatal(err)
	}
	if profileSHA != wantEvidenceProfileSHA {
		t.Fatalf("evals/evidence-v1/jev-profile.yaml hash changed: got %s, want %s (frozen — "+
			"must not be modified)", profileSHA, wantEvidenceProfileSHA)
	}
}

func TestCheckEvidenceHashesDetectsATamperedFile(t *testing.T) {
	root, err := findRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, evidenceManifestPath)
	original, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.WriteFile(manifestPath, original, 0o644); err != nil {
			t.Fatalf("failed to restore %s after test: %v", manifestPath, err)
		}
	})

	tampered := append(append([]byte(nil), original...), '\n', '/', '/', 't', 'a', 'm', 'p', 'e', 'r')
	if err := os.WriteFile(manifestPath, tampered, 0o644); err != nil {
		t.Fatal(err)
	}

	checks, ok, err := checkEvidenceHashes(root)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected checkEvidenceHashes to report a mismatch for a tampered manifest")
	}
	if len(checks) == 0 || checks[0].Match {
		t.Fatalf("expected the manifest check to report Match=false: %+v", checks)
	}
}

func TestCheckEvidenceHashesPassesOnTheUntamperedFiles(t *testing.T) {
	root, err := findRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	checks, ok, err := checkEvidenceHashes(root)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("expected both frozen artifacts to match, got: %+v", checks)
	}
	if len(checks) != 2 {
		t.Fatalf("expected exactly 2 hash checks (manifest + profile), got %d", len(checks))
	}
}

func TestCheckEvidenceCredentialsNeverLeaksAValue(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "super-secret-value-must-not-appear")
	checks := checkEvidenceCredentials(mustFindRepoRootForTest(t))
	var found bool
	for _, c := range checks {
		if c.Name == "TYPESAFE_API_KEY" {
			found = true
			if !c.Present {
				t.Fatal("expected TYPESAFE_API_KEY to report present")
			}
		}
	}
	if !found {
		t.Fatal("expected a TYPESAFE_API_KEY entry")
	}
	body, err := json.Marshal(checks)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte("super-secret-value-must-not-appear")) {
		t.Fatal("credential value leaked into the serialized check result")
	}
}

func TestCheckEvidenceCredentialsReportsAbsentWhenUnset(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	os.Unsetenv("TYPESAFE_API_KEY")
	checks := checkEvidenceCredentials(mustFindRepoRootForTest(t))
	for _, c := range checks {
		if c.Name == "TYPESAFE_API_KEY" && c.Present {
			t.Fatal("expected TYPESAFE_API_KEY to report absent when unset")
		}
	}
}

func TestBuildPreflightReportRefusesWhenCredentialsMissing(t *testing.T) {
	for _, name := range []string{"TYPESAFE_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY"} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	root, err := findRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	report, err := buildPreflightReport(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.ReadyForLiveRun {
		t.Fatal("must not report ready with every credential absent")
	}
	if len(report.Blockers) == 0 {
		t.Fatal("expected at least one blocker")
	}
}

func TestBuildPreflightReportNowReportsGeneratorWired(t *testing.T) {
	// evidence.RunCampaign now drives ExecuteRun for real from `bcode evidence
	// run --live` (internal/evidence/campaign.go, wired in this session) —
	// GeneratorWired flips to true regardless of credential presence,
	// since it reports whether the code path exists, not whether it was
	// run with a real credential (forbidden for this task).
	root, err := findRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	report, err := buildPreflightReport(root)
	if err != nil {
		t.Fatal(err)
	}
	if !report.GeneratorWired {
		t.Fatal("GeneratorWired must be true now that bcode evidence run --live calls " +
			"evidence.RunCampaign/ExecuteRun")
	}
}

func TestEvidenceRunRefusesLiveWithoutEveryCheckPassing(t *testing.T) {
	for _, name := range []string{"TYPESAFE_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY"} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	cmd := newEvidenceRunCmd()
	cmd.SetArgs([]string{"--live"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected `bcode evidence run --live` to refuse without credentials")
	}
}

func TestEvidenceRunWithoutLiveNeverRequiresCredentials(t *testing.T) {
	for _, name := range []string{"TYPESAFE_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY"} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	cmd := newEvidenceRunCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("bcode evidence run (no --live) must succeed even with no credentials: %v", err)
	}
}

// TestEvidenceRunnerFactoryWithFakeCredentialsConstructsUpToTheNetworkBoundary
// exercises design instruction §37 (GAP 1/2's "provider-disabled
// acceptance mode"): with fake credential strings for all three roles,
// every construction step — runtime config load, the committed
// evals/evidence-v1/generator/providers.yaml, router construction, role
// resolution, the frozen-profile treatment Jev judge — must succeed for
// real, with probeHealth=false so this test makes no network call. This
// proves no hidden engineering blocker (no providers.yaml, ExecuteRun not
// wired, Atlas unsupported, etc.) remains behind the credential gate.
func TestEvidenceRunnerFactoryWithFakeCredentialsConstructsUpToTheNetworkBoundary(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "fake-generator-value-for-test")
	t.Setenv("TYPESAFE_API_KEY", "fake-jev-value-for-test")
	t.Setenv(evidenceAtlasEvalCredentialEnv, "fake-atlas-value-for-test")

	root := mustFindRepoRootForTest(t)
	cmd := newEvidenceRunCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	rf, err := buildEvidenceRunnerFactoryImpl(cmd, root, false)
	if err != nil {
		t.Fatalf("expected construction to succeed up to the network boundary with fake "+
			"credentials, got: %v", err)
	}
	if rf.Provider == nil {
		t.Fatal("expected a constructed Provider")
	}
	if rf.Provider.Name() != "anthropic" {
		t.Fatalf("expected the frozen runtime config's provider, got %q", rf.Provider.Name())
	}
	if rf.TreatmentJudge == nil {
		t.Fatal("expected a constructed treatment Jev judge")
	}
	if rf.RuntimeConfigSHA256 == "" {
		t.Fatal("expected a non-empty runtime config hash")
	}
}

// TestEvidenceRunLiveGateNamesAllThreeCredentialRoles proves §30/§31: with
// no credentials at all, `run --live` refuses citing exactly the three
// credential roles, never an engineering blocker.
func TestEvidenceRunLiveGateNamesAllThreeCredentialRoles(t *testing.T) {
	for _, name := range []string{"TYPESAFE_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY",
		evidenceAtlasEvalCredentialEnv} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	cmd := newEvidenceRunCmd()
	cmd.SetArgs([]string{"--live"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected `bcode evidence run --live` to refuse without credentials")
	}
	msg := err.Error()
	for _, want := range []string{"generator credential missing", "TYPESAFE_API_KEY missing",
		"Atlas evaluator credential missing"} {
		if !contains(msg, want) {
			t.Fatalf("expected the refusal to name %q, got: %s", want, msg)
		}
	}
	for _, forbidden := range []string{"Atlas unsupported", "Harbor failed", "ExecuteRun not wired",
		"result persistence missing", "telemetry not implemented", "grader missing",
		"providers.yaml"} {
		if contains(msg, forbidden) {
			t.Fatalf("refusal must never cite a hidden engineering blocker (%q), got: %s", forbidden, msg)
		}
	}
}

// TestGeneratorAndAtlasEvaluatorCredentialsAreNeverCoupled proves design
// instruction §9: each of the three credential roles resolves
// independently — one present while the others are absent is reported
// accurately, and no role's value is copied into another's.
func TestGeneratorAndAtlasEvaluatorCredentialsAreNeverCoupled(t *testing.T) {
	root := mustFindRepoRootForTest(t)
	clearAllEvidenceCredentialsForTest(t)

	t.Setenv("ANTHROPIC_API_KEY", "generator-only")
	generator, typesafe, atlas := evidenceCredentialsPresent(root)
	if !generator || typesafe || atlas {
		t.Fatalf("generator-only env must resolve to (true,false,false), got (%v,%v,%v)",
			generator, typesafe, atlas)
	}
	os.Unsetenv("ANTHROPIC_API_KEY")

	t.Setenv(evidenceAtlasEvalCredentialEnv, "atlas-only")
	generator, typesafe, atlas = evidenceCredentialsPresent(root)
	if generator || typesafe || !atlas {
		t.Fatalf("atlas-only env must resolve to (false,false,true), got (%v,%v,%v)",
			generator, typesafe, atlas)
	}
}

func clearAllEvidenceCredentialsForTest(t *testing.T) {
	t.Helper()
	for _, name := range []string{"TYPESAFE_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY",
		evidenceAtlasEvalCredentialEnv} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
}

func mustFindRepoRootForTest(t *testing.T) string {
	t.Helper()
	root, err := findRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOfSub(s, sub) >= 0)
}

func indexOfSub(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestEvidencePreflightJSONOutputParses(t *testing.T) {
	cmd := newEvidencePreflightCmd()
	cmd.SetArgs([]string{"--json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var report preflightReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("preflight --json did not produce parseable JSON: %v\n%s", err, out.String())
	}
	if !report.HashesOK {
		t.Fatalf("expected HashesOK=true against the unmodified repository: %+v", report.Hashes)
	}
}
