package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/engine/native"
	"github.com/akynte/boundedcode/internal/evidence"
	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/llm"
)

// runRoot is the guarded scratch root every Evidence Suite v1 disposable
// artifact lives under — the same root
// evals/evidence-v1/tooling/safe_cleanup.py enforces from the Python side.
// Go code here never deletes anything outside it either.
//
// The directory was named .local-engineer-runs before the project was renamed,
// and a checkout that still has one keeps using it. Retained evidence runs are
// the accumulated output of hours of work; moving the root out from under them
// would leave the ledger pointing at workspaces nothing looks for any more.
// New checkouts get the new name, and nothing has to be migrated by hand.
func evidenceRunRoot(repoRoot string) string {
	// Keyed on the legacy *evidence* directory, not on whether the new parent
	// exists. An empty .boundedcode-runs/ appears the moment any test touches
	// the root, and testing existence of the parent would hand that empty
	// directory the win while the retained runs sat in the old one.
	legacy := filepath.Join(repoRoot, ".local-engineer-runs", "evidence-v1")
	if info, err := os.Stat(legacy); err == nil && info.IsDir() {
		return legacy
	}
	return filepath.Join(repoRoot, ".boundedcode-runs", "evidence-v1")
}

func evidenceLedgerPath(repoRoot string) string {
	return filepath.Join(evidenceRunRoot(repoRoot), "ledger", "v1.jsonl")
}

func newEvidenceStatusCmd() *cobra.Command {
	var suite string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show Evidence Suite v1's planned-run status (durable, resumable across restarts)",
		Long: "Reads the durable run ledger and reports, per status, how many of the 16\n" +
			"planned runs (8 frozen tasks x 2 arms) are PENDING, RUNNING, SOLVED, FAILED,\n" +
			"INFRA_ERROR, or INVALID. Useful for checking on a multi-hour live campaign\n" +
			"without interrupting it. Makes no network call, no model call, and never\n" +
			"writes to the ledger.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if suite != "v1" {
				return fmt.Errorf("unknown suite %q; only v1 is defined", suite)
			}
			root, err := findRepoRoot()
			if err != nil {
				return err
			}
			plans, err := evidence.BuildPlan(
				filepath.Join(root, evidenceManifestPath), filepath.Join(root, evidenceProfilePath))
			if err != nil {
				return err
			}
			led, err := evidence.OpenLedger(evidenceLedgerPath(root))
			if err != nil {
				return err
			}
			current, err := led.Current()
			if err != nil {
				return err
			}
			summary := evidence.Summarize(plans, current)

			if asJSON {
				type row struct {
					Key    string          `json:"key"`
					Status evidence.Status `json:"status"`
					RunID  string          `json:"run_id,omitempty"`
				}
				rows := make([]row, 0, len(plans))
				for _, k := range evidence.SortedKeys(plans) {
					st := evidence.StatusPending
					runID := ""
					if rec, ok := current[k]; ok {
						st = rec.Status
						runID = rec.RunID
					}
					rows = append(rows, row{Key: k, Status: st, RunID: runID})
				}
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(struct {
					Summary evidence.Summary `json:"summary"`
					Runs    []row            `json:"runs"`
				}{summary, rows})
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "planned runs: %d\n\n", summary.Planned)
			for _, s := range []evidence.Status{evidence.StatusPending, evidence.StatusRunning,
				evidence.StatusSolved, evidence.StatusFailed, evidence.StatusInfraErr, evidence.StatusInvalid} {
				fmt.Fprintf(w, "%-12s %d\n", s, summary.ByStatus[s])
			}
			fmt.Fprintln(w)
			for _, k := range evidence.SortedKeys(plans) {
				st := evidence.StatusPending
				if rec, ok := current[k]; ok {
					st = rec.Status
				}
				fmt.Fprintf(w, "  %-12s %s\n", st, k)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&suite, "suite", "v1", "evidence suite version")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print as JSON")
	return cmd
}

// evidenceRunOptions is shared between `run --dry-run` and `run --live`,
// since design instruction §23/§34 requires the dry-run to share
// implementation with the live path rather than a parallel one.
func evidenceBuildAndCheckPlan(root string) ([]evidence.RunPlan, []evidence.ArmDiff, error) {
	plans, err := evidence.BuildPlan(
		filepath.Join(root, evidenceManifestPath), filepath.Join(root, evidenceProfilePath))
	if err != nil {
		return nil, nil, err
	}
	diffs, err := evidence.CompareAllArms(plans)
	if err != nil {
		return nil, nil, err
	}
	for _, d := range diffs {
		if !d.OnlyJevDiffers {
			return plans, diffs, fmt.Errorf("evidence: task %s: arm configurations differ beyond "+
				"the Jev treatment (%v) — refusing to proceed", d.TaskID, d.Differences)
		}
	}
	return plans, diffs, nil
}

// evidenceDryRun constructs and validates every one of the 16 planned
// runs — real workspace preparation, real anti-leakage re-verification,
// real fingerprinting for every SWE-Bench Pro Verified plan — without any
// model or Jev call. It is the same construction path `run --live` uses
// before it ever reaches the credential boundary (design instruction §23:
// "control/treatment equality must be generated by the actual
// orchestrator... for all 8 task pairs in dry-run").
func evidenceDryRun(ctx context.Context, w interface{ Write([]byte) (int, error) }, root string) error {
	plans, diffs, err := evidenceBuildAndCheckPlan(root)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "constructed %d planned run(s) from the frozen suite\n", len(plans))

	byArm := evidence.CountByArmAndBenchmark(plans)
	for _, arm := range []evidence.Arm{evidence.ArmControl, evidence.ArmExperimentalJevFull} {
		total := 0
		for _, n := range byArm[arm] {
			total += n
		}
		fmt.Fprintf(w, "  %d %s run configs ready\n", total, arm)
	}

	runRoot := evidenceRunRoot(root)
	prepared, failed := 0, 0
	for i := range plans {
		p := plans[i]
		pp, err := evidence.Prepare(ctx, runRoot, p)
		if err != nil {
			fmt.Fprintf(w, "  NOT PREPARED  %-70s %v\n", p.Key(), err)
			failed++
			continue
		}
		plans[i] = pp
		fmt.Fprintf(w, "  prepared       %-70s fingerprint=%s...\n", p.Key(), pp.SourceFingerprint[:12])
		prepared++
	}

	fmt.Fprintf(w, "\narm equality (all 8 task pairs): ")
	allOK := true
	for _, d := range diffs {
		if !d.OnlyJevDiffers {
			allOK = false
		}
	}
	if allOK {
		fmt.Fprintln(w, "PASS — every pair differs only in the Jev treatment")
	} else {
		fmt.Fprintln(w, "FAIL")
	}

	fmt.Fprintf(w, "\nprepared: %d / 16, not prepared: %d / 16\n", prepared, failed)
	if !allOK {
		return fmt.Errorf("evidence: dry-run found arm configurations that differ beyond the " +
			"Jev treatment — see above")
	}
	if prepared < 16 {
		return fmt.Errorf("evidence: dry-run could only prepare %d of the 16 planned runs — "+
			"see the NOT PREPARED lines above", prepared)
	}
	return nil
}

// buildEvidenceRunnerFactory constructs the real, shared generator (and,
// for the treatment arm, the real Jev judge from the frozen
// jev-profile.yaml) exactly the way cmd/bcode/task.go's engineFor builds one
// for an ordinary task — providers.yaml -> llm.NewRouter ->
// Router.For(llm.RoleCoding) -> a 15s Health probe -> the active hardware
// profile's tuning knobs. This is the ONE place either is constructed;
// evidence.RunnerFactory reuses it across all 16 planned runs (design
// instruction §24). It is only ever reached once both required
// credentials are already confirmed present, and never called by any test
// in this repository — the Health probe below is a real network call, and
// this task is forbidden from making one.
func buildEvidenceRunnerFactory(cmd *cobra.Command, repoRoot string) (*evidence.RunnerFactory, error) {
	return buildEvidenceRunnerFactoryImpl(cmd, repoRoot, true)
}

// buildEvidenceRunnerFactoryImpl is the real construction path.
// probeHealth is true in every production call; a test passes false to
// exercise every construction step (runtime config, committed
// providers.yaml, router, role resolution, treatment judge, tuning
// options) up to — but never across — the one real network boundary,
// design instruction §37's "provider-disabled acceptance mode": stop
// before network, never accidentally contact a provider.
func buildEvidenceRunnerFactoryImpl(cmd *cobra.Command, repoRoot string, probeHealth bool) (*evidence.RunnerFactory, error) {
	rcfg, rcfgSHA, err := evidence.LoadRuntimeConfig(filepath.Join(repoRoot, evidenceRuntimeConfigPath))
	if err != nil {
		return nil, fmt.Errorf("evidence: loading the frozen generator runtime config: %w", err)
	}

	// Read from evals/evidence-v1/generator/providers.yaml — Evidence
	// Suite v1's own committed, secret-free provider file (GAP 1) — never
	// the operator's .bc/config/providers.yaml, which is untracked and
	// per-environment. Same loader (llm.LoadProvidersFile) every ordinary
	// `bcode task run` uses; LoadProvidersFile itself always appends the
	// fixed filename "providers.yaml" to the directory given.
	f, err := llm.LoadProvidersFile(filepath.Join(repoRoot, filepath.Dir(rcfg.Generator.ProvidersFile)))
	if err != nil {
		return nil, fmt.Errorf("evidence: no committed generator providers.yaml at %s: %w",
			rcfg.Generator.ProvidersFile, err)
	}
	router, err := llm.NewRouter(f)
	if err != nil {
		return nil, fmt.Errorf("evidence: %s is invalid: %w", rcfg.Generator.ProvidersFile, err)
	}
	provider, err := router.For(llm.Role(rcfg.Generator.Routing.Role))
	if err != nil {
		return nil, err
	}
	if provider.Name() != rcfg.Generator.Provider {
		return nil, fmt.Errorf("evidence: runtime-config.yaml names provider %q but the routed "+
			"provider is %q — no silent substitution", rcfg.Generator.Provider, provider.Name())
	}

	if probeHealth {
		probe, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
		defer cancel()
		if err := provider.Health(probe); err != nil {
			return nil, fmt.Errorf("evidence: the coding role is routed to %s, which is not "+
				"reachable: %w", provider.Name(), err)
		}
	}

	opts := native.Options{
		Logf:          func(f string, a ...any) { fmt.Fprintf(cmd.ErrOrStderr(), f+"\n", a...) },
		MaxTools:      rcfg.Generator.Context.MaxToolsExposed,
		MaxTokens:     rcfg.Generator.Context.ReservedOutputTokens,
		Temperature:   rcfg.Generator.Sampling.Temperature,
		Thinking:      rcfg.Generator.Reasoning.ThinkingPolicy,
		ContextTokens: rcfg.Generator.Context.ContextTokens,
		MaxSteps:      rcfg.Generator.Context.MaxSteps,
	}

	jevKeyEnv := "TYPESAFE_API_KEY"
	judge, err := evidence.BuildTreatmentJudge(filepath.Join(repoRoot, evidenceProfilePath), jevKeyEnv,
		judgment.Deps{Logf: func(f string, a ...any) { fmt.Fprintf(cmd.ErrOrStderr(), f+"\n", a...) }})
	if err != nil {
		return nil, fmt.Errorf("evidence: constructing the treatment Jev judge: %w", err)
	}

	commit := "unknown"
	if body, err := os.ReadFile(filepath.Join(repoRoot, ".git", "HEAD")); err == nil {
		commit = string(body)
	}

	return &evidence.RunnerFactory{
		Provider: provider, EngineOptions: opts, TreatmentJudge: judge, BoundedCodeCommit: commit,
		JevProfilePath: filepath.Join(repoRoot, evidenceProfilePath), JevAPIKeyEnv: jevKeyEnv,
		RuntimeConfigSHA256: rcfgSHA,
		AtlasEvalAPIKey:     os.Getenv(evidenceAtlasEvalCredentialEnv),
		RepoRoot:            repoRoot,
		PythonExe:           filepath.Join(evidenceRunRoot(repoRoot), "venv", "bin", "python3"),
		HarborExe:           filepath.Join(evidenceRunRoot(repoRoot), "venv", "bin", "harbor"),
	}, nil
}

// evidenceCredentialsPresent reports exactly the two credentials design
// instruction §10/§43 name, by presence only.
func evidenceCredentialsPresent(repoRoot string) (generator, typesafe, atlasEvaluator bool) {
	for _, c := range checkEvidenceCredentials(repoRoot) {
		switch c.Role {
		case "generator":
			generator = c.Present
		case "jev":
			typesafe = c.Present
		case "atlas_evaluator":
			atlasEvaluator = c.Present
		}
	}
	return generator, typesafe, atlasEvaluator
}
