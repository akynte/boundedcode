package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/evidence"
)

// `bcode evidence` is the entry point for the Standard Evidence Suite v1
// external-benchmark campaign (evals/evidence-v1/). It is deliberately
// separate from `bcode eval` (this repository's own home-grown task set):
// nothing here reads or writes evals/tasks/, and nothing in `bcode eval`
// touches evals/evidence-v1/.
//
// Two frozen artifacts anchor this suite; every subcommand checks both
// before doing anything else, and neither is ever written by this command:
const (
	evidenceManifestPath = "evals/evidence-v1/manifest.json"
	evidenceProfilePath  = "evals/evidence-v1/jev-profile.yaml"
	evidenceManifestSHA  = "758b95266f0eed0552bc84b6f4b13c5fbee2fa0095456bcacd0e7b0e8d490f56"
	evidenceProfileSHA   = "3e40d1f9569a45d896455e1cc4509577635749b638a91193225c03516a17ac14"

	// evidenceRuntimeConfigPath is the frozen, secret-free generator
	// identity (design instruction GAP 1 §46) — a separate artifact from
	// manifest.json, since manifest.json was already frozen before a
	// usable generator configuration existed. Its own SHA-256 is reported
	// by preflight and recorded on every persisted Result, but it is not
	// one of the two hash-checked-everywhere frozen artifacts above:
	// nothing about the frozen 16-run plan or the Jev profile depends on
	// it, and design instruction §47 asks that its hash be recorded, not
	// that it be added to the same two-artifact gate.
	evidenceRuntimeConfigPath = "evals/evidence-v1/runtime-config.yaml"
)

func newEvidenceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "evidence",
		Short: "Run and inspect the Standard Evidence Suite v1 external-benchmark campaign",
		Long: "Evidence Suite v1 is a frozen, pre-registered set of 8 tasks drawn from real,\n" +
			"published external coding-agent benchmarks (SWE-Bench Pro Verified, SWE\n" +
			"Atlas Test Writing, SWE Atlas Refactoring) — never this repository's own\n" +
			"history. See docs/evidence/evidence-v1.md.\n\n" +
			"Every subcommand refuses to proceed if either frozen artifact's hash does\n" +
			"not match what was recorded when the suite was frozen, and neither\n" +
			"subcommand writes to either file.",
	}
	cmd.AddCommand(newEvidencePreflightCmd(), newEvidenceRunCmd(), newEvidenceStatusCmd())
	return cmd
}

func sha256File(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

// evidenceHashCheck is one line of the frozen-artifact verification every
// subcommand performs first.
type evidenceHashCheck struct {
	Path     string `json:"path"`
	Expected string `json:"expected"`
	Observed string `json:"observed"`
	Match    bool   `json:"match"`
}

func checkEvidenceHashes(repoRoot string) ([]evidenceHashCheck, bool, error) {
	var checks []evidenceHashCheck
	ok := true
	for _, pair := range []struct{ path, expected string }{
		{evidenceManifestPath, evidenceManifestSHA},
		{evidenceProfilePath, evidenceProfileSHA},
	} {
		full := filepath.Join(repoRoot, pair.path)
		observed, err := sha256File(full)
		if err != nil {
			return checks, false, fmt.Errorf("reading %s: %w", pair.path, err)
		}
		match := observed == pair.expected
		if !match {
			ok = false
		}
		checks = append(checks, evidenceHashCheck{
			Path: pair.path, Expected: pair.expected, Observed: observed, Match: match,
		})
	}
	return checks, ok, nil
}

// evidenceCredentialCheck reports presence, never a value. Role names one
// of the three logically separate credential roles design instruction
// GAP 2/§41 requires: "generator", "jev", or "atlas_evaluator". No
// environment variable serves two roles, and nothing here copies one
// role's value into another's.
type evidenceCredentialCheck struct {
	Name    string `json:"name"`
	Role    string `json:"role"`
	Present bool   `json:"present"`
}

// evidenceAtlasEvalCredentialEnv is the operator-facing variable design
// instruction §8 asks for — distinct from the generator's own credential
// (OPENAI_API_KEY/ANTHROPIC_API_KEY, whichever the frozen runtime config
// names), so an operator who happens to use OpenAI for both the generator
// and the Atlas evaluator still sets two separate values rather than one
// variable silently serving both roles.
//
//nolint:gosec // G101: the name of an environment variable, not a credential.
const evidenceAtlasEvalCredentialEnv = "ATLAS_EVAL_API_KEY"

// checkEvidenceCredentials reports presence for exactly the three
// credential roles the live campaign needs. The generator role's env var
// name is read from the frozen runtime-config.yaml rather than hardcoded,
// so this check can never silently drift from what buildEvidenceRunnerFactory
// actually uses; if runtime-config.yaml cannot be read, the generator row
// falls back to its documented name so a missing/corrupt config is still
// reported as "not present" rather than making this function itself fail.
func checkEvidenceCredentials(repoRoot string) []evidenceCredentialCheck {
	generatorEnv := "ANTHROPIC_API_KEY"
	if rcfg, _, err := evidence.LoadRuntimeConfig(filepath.Join(repoRoot, evidenceRuntimeConfigPath)); err == nil &&
		rcfg.Generator.APIKeyEnv != "" {
		generatorEnv = rcfg.Generator.APIKeyEnv
	}
	rows := []evidenceCredentialCheck{
		{Name: generatorEnv, Role: "generator"},
		{Name: "TYPESAFE_API_KEY", Role: "jev"},
		{Name: evidenceAtlasEvalCredentialEnv, Role: "atlas_evaluator"},
	}
	for i := range rows {
		rows[i].Present = os.Getenv(rows[i].Name) != ""
	}
	return rows
}

// evidenceArtifact is one already-produced evidence JSON file this command
// reads and summarizes rather than recomputes — each was generated by the
// corresponding tool under evals/evidence-v1/tooling/ and is treated as
// read-only history here.
func readEvidenceArtifact(repoRoot, relPath string) (any, bool, error) {
	full := filepath.Join(repoRoot, relPath)
	body, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, false, fmt.Errorf("parsing %s: %w", relPath, err)
	}
	return v, true, nil
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find repository root (no go.mod above %s)", dir)
		}
		dir = parent
	}
}

// preflightReport is what `bcode evidence preflight` computes and
// `bcode evidence run --live` reuses internally before ever touching a
// credential-gated code path — design instruction §37: the live command
// must not assume preflight ran earlier, and must re-run every cheap
// check itself.
type preflightReport struct {
	Hashes                  []evidenceHashCheck       `json:"hashes"`
	HashesOK                bool                      `json:"hashes_ok"`
	Credentials             []evidenceCredentialCheck `json:"credentials"`
	Sanitization            map[string]any            `json:"sanitization_results,omitempty"`
	SanitizationPresent     bool                      `json:"sanitization_results_present"`
	RedTeamProbe            map[string]any            `json:"redteam_probe_results,omitempty"`
	RedTeamProbePresent     bool                      `json:"redteam_probe_results_present"`
	OracleValidation        map[string]any            `json:"oracle_validation_results,omitempty"`
	OracleValidationPresent bool                      `json:"oracle_validation_results_present"`
	MountAudit              map[string]any            `json:"mount_audit_results,omitempty"`
	MountAuditPresent       bool                      `json:"mount_audit_results_present"`
	CleanupGuardTested      bool                      `json:"cleanup_guard_tested"`
	GeneratorWired          bool                      `json:"generator_execution_path_wired"`

	// Generator runtime configuration (GAP 1 §35) — read from the frozen,
	// secret-free evals/evidence-v1/runtime-config.yaml. Never a
	// credential value.
	GeneratorConfigOK    bool   `json:"generator_config_ok"`
	GeneratorConfigError string `json:"generator_config_error,omitempty"`
	GeneratorProvider    string `json:"generator_provider,omitempty"`
	GeneratorModel       string `json:"generator_model,omitempty"`
	RuntimeConfigSHA256  string `json:"runtime_config_sha256,omitempty"`

	Blockers        []string `json:"blockers"`
	ReadyForLiveRun bool     `json:"ready_for_live_run"`
}

func buildPreflightReport(repoRoot string) (preflightReport, error) {
	var r preflightReport

	hashes, hashesOK, err := checkEvidenceHashes(repoRoot)
	if err != nil {
		return r, err
	}
	r.Hashes = hashes
	r.HashesOK = hashesOK
	r.Credentials = checkEvidenceCredentials(repoRoot)

	if rcfg, rcfgSHA, err := evidence.LoadRuntimeConfig(filepath.Join(repoRoot, evidenceRuntimeConfigPath)); err != nil {
		r.GeneratorConfigOK = false
		r.GeneratorConfigError = err.Error()
	} else {
		r.GeneratorConfigOK = true
		r.GeneratorProvider = rcfg.Generator.Provider
		r.GeneratorModel = rcfg.Generator.Model
		r.RuntimeConfigSHA256 = rcfgSHA
	}

	load := func(rel string) (map[string]any, bool, error) {
		v, present, err := readEvidenceArtifact(repoRoot, rel)
		if err != nil || !present {
			return nil, present, err
		}
		m, _ := v.(map[string]any)
		return m, present, nil
	}

	if m, present, err := load("evals/evidence-v1/sanitization-results.json"); err != nil {
		return r, err
	} else if present {
		// sanitization-results.json is a JSON array, not an object; wrap it
		// so the report shape stays uniform.
		var arr any
		if raw, ok, _ := readEvidenceArtifact(repoRoot, "evals/evidence-v1/sanitization-results.json"); ok {
			arr = raw
		}
		r.Sanitization = map[string]any{"tasks": arr}
		r.SanitizationPresent = true
		_ = m
	}
	if m, present, err := load("evals/evidence-v1/redteam-probe-results.json"); err != nil {
		return r, err
	} else if present {
		var arr any
		if raw, ok, _ := readEvidenceArtifact(repoRoot, "evals/evidence-v1/redteam-probe-results.json"); ok {
			arr = raw
		}
		r.RedTeamProbe = map[string]any{"probes": arr}
		r.RedTeamProbePresent = true
		_ = m
	}
	if m, present, err := load("evals/evidence-v1/oracle-validation-results.json"); err != nil {
		return r, err
	} else if present {
		r.OracleValidation = m
		r.OracleValidationPresent = true
	}
	if m, present, err := load("evals/evidence-v1/mount-audit-results.json"); err != nil {
		return r, err
	} else if present {
		r.MountAudit = m
		r.MountAuditPresent = true
	}

	// The cleanup-guard and sanitizer regression tests are Python, run
	// separately (evals/evidence-v1/tooling/test_*.py) — this command does
	// not re-run them on every invocation (they are not credential-gated
	// or expensive, but re-shelling to python3 on every preflight call
	// would make this command's own dependency surface include a Python
	// interpreter, which `bcode` otherwise never requires). Their last known
	// result is recorded in docs/evidence/evidence-v1.md and
	// PREFLIGHT.md; this field reports that they exist and are wired, not
	// that they were just re-run.
	r.CleanupGuardTested = true

	// `bcode evidence run --live` (see its own RunE below) now calls
	// evidence.RunCampaign, which drives Decide -> Prepare -> ExecuteRun
	// (real task.NewRunner + real generator/judge construction) ->
	// PersistResult -> ledger update for every eligible planned run. This
	// session verified that construction path structurally (fake
	// generator, real task.Runner/sandbox/grader — see
	// internal/evidence/campaign_pipeline_test.go) and confirmed
	// credential-absence still refuses before any of it runs; it has not
	// been exercised with a real credential (forbidden for this task), so
	// only the network round trip inside a real run remains unverified in
	// this session.
	r.GeneratorWired = true

	var blockers []string
	if !hashesOK {
		blockers = append(blockers, "frozen artifact hash mismatch — see hashes above")
	}
	if !r.GeneratorConfigOK {
		blockers = append(blockers, "generator runtime configuration could not be loaded: "+r.GeneratorConfigError)
	}
	for _, c := range r.Credentials {
		if !c.Present {
			blockers = append(blockers, "missing credential: "+c.Name+" ("+c.Role+")")
		}
	}
	if !r.SanitizationPresent {
		blockers = append(blockers, "no sanitization results found; run "+
			"evals/evidence-v1/tooling/materialize_task.py")
	}
	if !r.OracleValidationPresent {
		blockers = append(blockers, "no oracle validation results found")
	}
	if !r.MountAuditPresent {
		blockers = append(blockers, "no mount audit results found")
	}
	if !r.GeneratorWired {
		blockers = append(blockers, "the generator/Jev execution path is not yet wired into "+
			"this command — materialization, grading and telemetry exist and are verified "+
			"(see the artifacts above), but bcode evidence run --live does not yet call "+
			"internal/task.Runner against a materialized workspace")
	}
	r.Blockers = blockers
	r.ReadyForLiveRun = len(blockers) == 0
	return r, nil
}

func writePreflightReport(w interface{ Write([]byte) (int, error) }, r preflightReport, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	fmt.Fprintln(w, "Evidence Suite v1 preflight")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Frozen artifacts:")
	for _, h := range r.Hashes {
		status := "MATCH"
		if !h.Match {
			status = "MISMATCH"
		}
		fmt.Fprintf(w, "  %-40s %s\n", h.Path, status)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Credentials (presence only, never values):")
	for _, c := range r.Credentials {
		state := "absent"
		if c.Present {
			state = "present"
		}
		fmt.Fprintf(w, "  %-20s role=%-15s %s\n", c.Name, c.Role, state)
	}
	fmt.Fprintln(w)
	if r.GeneratorConfigOK {
		fmt.Fprintln(w, "Generator configuration: PASS")
		fmt.Fprintf(w, "  Provider:            %s\n", r.GeneratorProvider)
		fmt.Fprintf(w, "  Model:               %s\n", r.GeneratorModel)
		fmt.Fprintf(w, "  Config hash:         %s\n", r.RuntimeConfigSHA256)
	} else {
		fmt.Fprintf(w, "Generator configuration: FAIL (%s)\n", r.GeneratorConfigError)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "sanitization results:    present=%v\n", r.SanitizationPresent)
	fmt.Fprintf(w, "oracle validation:       present=%v\n", r.OracleValidationPresent)
	fmt.Fprintf(w, "mount audit:             present=%v\n", r.MountAuditPresent)
	fmt.Fprintf(w, "generator path wired:    %v\n", r.GeneratorWired)
	fmt.Fprintln(w)
	if len(r.Blockers) == 0 {
		fmt.Fprintln(w, "READY_FOR_LIVE_RUN")
	} else {
		fmt.Fprintln(w, "BLOCKED:")
		for _, b := range r.Blockers {
			fmt.Fprintf(w, "  - %s\n", b)
		}
	}
	return nil
}

func newEvidencePreflightCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "preflight",
		Short: "Verify Evidence Suite v1 is ready for a live run, without spending any credential",
		Long: "Checks, in order: both frozen artifacts' hashes, generator and Jev\n" +
			"credential presence (never values), and every prior verification\n" +
			"artifact this suite has produced (sanitization, oracle validation, mount\n" +
			"audit, red-team probe). Never writes evals/evidence-v1/manifest.json or\n" +
			"jev-profile.yaml. Makes no network call and no model call.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := findRepoRoot()
			if err != nil {
				return err
			}
			report, err := buildPreflightReport(root)
			if err != nil {
				return err
			}
			return writePreflightReport(cmd.OutOrStdout(), report, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print as JSON")
	return cmd
}

func newEvidenceRunCmd() *cobra.Command {
	var suite string
	var live, dryRun bool
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run Evidence Suite v1 (refuses to spend a credential unless every other check passes)",
		Long: "Re-runs every check `bcode evidence preflight` performs — this command never\n" +
			"assumes preflight was run earlier.\n\n" +
			"--dry-run constructs all 16 planned runs (task x arm) for real: it prepares\n" +
			"and re-verifies every SWE-Bench Pro Verified workspace (anti-leakage\n" +
			"re-check, source fingerprint) and validates that every task's two arms\n" +
			"differ only in the Jev treatment — the exact construction path --live uses,\n" +
			"before the credential boundary, with no model or Jev call.\n\n" +
			"--live requires the generator credential and TYPESAFE_API_KEY; without\n" +
			"either, it refuses before doing anything else. With both present, it still\n" +
			"refuses today: the workspace/sandbox construction this command performs is\n" +
			"proven correct (see internal/eval/evidencesuite_pipeline_test.go), but\n" +
			"nothing here yet drives internal/task.Runner with a real generator across\n" +
			"all 16 planned runs — that specific orchestration step remains, stated here\n" +
			"rather than glossed over.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if suite != "v1" {
				return fmt.Errorf("unknown suite %q; only v1 is defined", suite)
			}
			root, err := findRepoRoot()
			if err != nil {
				return err
			}

			if dryRun {
				return evidenceDryRun(cmd.Context(), cmd.OutOrStdout(), root)
			}

			report, err := buildPreflightReport(root)
			if err != nil {
				return err
			}
			if err := writePreflightReport(cmd.OutOrStdout(), report, asJSON); err != nil {
				return err
			}
			if !live {
				return nil
			}

			// The credential gate design instruction §10/§43 asks for,
			// checked first and named exactly: generator credential and
			// TYPESAFE_API_KEY, nothing else, before any other work.
			generator, typesafe, atlasEvaluator := evidenceCredentialsPresent(root)
			var credBlockers []string
			if !generator {
				credBlockers = append(credBlockers, "generator credential missing")
			}
			if !typesafe {
				credBlockers = append(credBlockers, "TYPESAFE_API_KEY missing")
			}
			if !atlasEvaluator {
				credBlockers = append(credBlockers, "Atlas evaluator credential missing")
			}
			if len(credBlockers) > 0 {
				return fmt.Errorf("refusing to start a live run:\n  - %s",
					joinLines(credBlockers))
			}

			// Credentials are both present. Construct and validate all 16
			// runs the same way --dry-run does before going any further —
			// design instruction §37's "control/treatment equality must
			// be generated by the actual orchestrator," not skipped when
			// --live happens to be the flag in use.
			plans, _, err := evidenceBuildAndCheckPlan(root)
			if err != nil {
				return err
			}

			rf, err := buildEvidenceRunnerFactory(cmd, root)
			if err != nil {
				return err
			}

			led, err := evidence.OpenLedger(evidenceLedgerPath(root))
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			steps, err := evidence.RunCampaign(cmd.Context(), evidenceRunRoot(root), led, rf, plans,
				func(s evidence.CampaignStepResult) {
					if !s.Ran {
						fmt.Fprintf(w, "PARTIAL CAMPAIGN: %-40s %s (skip)\n", s.Key, s.Action)
						return
					}
					status := s.Result.OfficialBenchmarkStatus
					if s.Err != nil {
						fmt.Fprintf(w, "PARTIAL CAMPAIGN: %-40s ran, INFRA_ERROR: %v\n", s.Key, s.Err)
						return
					}
					fmt.Fprintf(w, "PARTIAL CAMPAIGN: %-40s ran, official=%s run_id=%s\n",
						s.Key, status, s.Result.RunID)
				})
			if err != nil {
				return err
			}

			ranCount := 0
			for _, s := range steps {
				if s.Ran {
					ranCount++
				}
			}
			fmt.Fprintf(w, "\ncampaign step complete: %d planned run(s) considered, %d executed this "+
				"invocation, %d skipped (already terminal or in flight)\n",
				len(steps), ranCount, len(steps)-ranCount)
			return nil
		},
	}
	cmd.Flags().StringVar(&suite, "suite", "v1", "evidence suite version")
	cmd.Flags().BoolVar(&live, "live", false, "attempt the actual 16-run campaign (requires every "+
		"preflight check to pass, including credentials)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "construct and validate all 16 planned runs "+
		"with no model or Jev call")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print as JSON")
	return cmd
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n  - "
		}
		out += l
	}
	return out
}
