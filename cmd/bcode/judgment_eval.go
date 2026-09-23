package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/judgeval"
	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/supervisor"
	"github.com/akynte/boundedcode/internal/workflow"
)

// The Judgment Validation Campaign's CLI surface: `bcode judgment eval` and
// `bcode judgment eligibility`. Neither promotes a site — see
// internal/judgeval/eligibility.go's package comment — and neither reaches
// the network unless --live is explicitly passed, matching the rest of this
// program's rule that an ordinary command never spends a judgment without
// being asked.
//
// This file, like docs/explanation/judgment-validation.md, is honest about
// scope: only verification_integrity (M4) has a wired campaign adapter
// today (judgeval.AdapterRegistered). Every other registered site can still
// have its dataset validated, split, and inspected through this surface;
// running the Jev arm against it needs an adapter this task did not have
// scope to build for all eleven sites — see judgment-validation.md.

const defaultDatasetDir = "evals/judgment/datasets"
const defaultResultsDir = "evals/judgment/results"
const defaultManifestDir = "evals/judgment/manifests"
const defaultPolicyPath = "evals/judgment/policy.yaml"

func newJudgmentEvalCmd() *cobra.Command {
	var datasetDir, split, resultsDir, manifestDir string
	var asJSON, live bool
	var seed int64

	cmd := &cobra.Command{
		Use:   "eval <site>",
		Short: "Run the offline validation campaign for one judgment site",
		Long: "Loads evals/judgment/datasets/<site>.jsonl, runs every configured arm\n" +
			"(deterministic baseline, local control, Jev) over the requested split, and\n" +
			"reports per-arm metrics plus a reproducibility manifest.\n\n" +
			"This never promotes a site: see `bcode judgment eligibility` for the advisory\n" +
			"report a promotion decision should read, and judgment.yaml for the only file\n" +
			"that actually grants authority.\n\n" +
			"Without --live this makes no network call: the Jev arm runs against\n" +
			"judgment.Off(), which reports itself unavailable rather than silently\n" +
			"answering nothing. Pass --live to use the configured judge — an explicit,\n" +
			"billed, opt-in operation, matching every other live call this program makes.\n\n" +
			"Only verification_integrity (M4) has a wired campaign adapter today. Every\n" +
			"other site's dataset can still be loaded and inspected; running the Jev arm\n" +
			"against it is not yet implemented — this command says so rather than\n" +
			"fabricating a result.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			site := args[0]
			info, ok := judgment.Site(site)
			if !ok {
				return fmt.Errorf("%q is not a judgment site this build defines; "+
					"run `bcode judgment sites` for the list", site)
			}
			if split != "dev" && split != "heldout" {
				return fmt.Errorf("--split must be dev or heldout, got %q", split)
			}

			path := filepath.Join(datasetDir, site+".jsonl")
			ds, err := judgeval.LoadDataset(path)
			if errors.Is(err, os.ErrNotExist) {
				fmt.Fprintf(cmd.OutOrStdout(),
					"no dataset file at %s; nothing to evaluate.\n"+
						"See docs/explanation/judgment-validation.md for the dataset format and\n"+
						"how to add cases.\n", path)
				return nil
			}
			if err != nil {
				return fmt.Errorf("loading dataset: %w", err)
			}

			var cases []judgeval.Case
			if split == "dev" {
				cases = ds.Dev()
			} else {
				cases = ds.Heldout()
			}
			fmt.Fprintf(cmd.OutOrStdout(), "site:    %s (%s)\ndataset: %s\nsplit:   %s (%d cases)\n\n",
				site, info.Mechanism, path, split, len(cases))

			if !judgeval.AdapterRegistered(site) {
				fmt.Fprintf(cmd.OutOrStdout(),
					"no campaign arm adapter is wired for %q yet. The dataset above loaded and\n"+
						"validated correctly, but this command cannot run an arm against it — see\n"+
						"judgeval.AdapterRegistered and judgment-validation.md's scope section.\n", site)
				return nil
			}
			if len(cases) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "no cases in this split; nothing to run.\n")
				return nil
			}

			j := judgment.Off()
			modelID := ""
			if live {
				_, root, st, err := openWorkspace(ctx)
				if err != nil {
					return err
				}
				defer closeRoot(cmd, root)
				lj, err := supervisor.Judge(root, st, func(f string, a ...any) {
					fmt.Fprintf(cmd.ErrOrStderr(), f+"\n", a...)
				})
				if err != nil {
					return err
				}
				j = lj
				if m, ok := lj.(interface{ ModelID() string }); ok {
					modelID = m.ModelID()
				}
			}

			started := time.Now()
			arms, err := judgeval.RunM4Campaign(ctx, j, cases, workflow.IntegrityTuning{})
			if err != nil {
				return err
			}
			elapsed := time.Since(started)

			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(arms)
			}
			writeArms(cmd.OutOrStdout(), arms)

			if resultsDir == "" || manifestDir == "" {
				return nil
			}
			hash, herr := judgeval.HashFile(path)
			if herr != nil {
				return herr
			}
			rev, dirty := judgeval.GitRevision(cmd.Context(), ".")
			m := judgeval.Manifest{
				SchemaVersion: 1, RepoRevision: rev, RepoDirty: dirty,
				Site: site, SiteVersion: info.Version, DatasetPath: path, DatasetHash: hash,
				Split: judgeval.Split(split), Model: modelID, PolicyVersion: judgeval.PolicySchemaVersion,
				Arm: string(judgeval.ArmJev), Seed: seed, Live: live,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			}
			manifestPath := filepath.Join(manifestDir, fmt.Sprintf("%s-%s-%d.json", site, split, time.Now().UnixNano()))
			if err := judgeval.SaveManifest(manifestPath, m); err != nil {
				return err
			}
			resultPath := filepath.Join(resultsDir, fmt.Sprintf("%s-%s-%d.json", site, split, time.Now().UnixNano()))
			if err := judgeval.SaveResult(resultPath, arms); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\nelapsed:  %s\nresult:   %s\nmanifest: %s\n",
				elapsed.Round(time.Millisecond), resultPath, manifestPath)
			return nil
		},
	}
	cmd.Flags().StringVar(&datasetDir, "dataset", defaultDatasetDir, "directory holding <site>.jsonl dataset files")
	cmd.Flags().StringVar(&split, "split", "dev", "dev or heldout")
	cmd.Flags().StringVar(&resultsDir, "results-dir", defaultResultsDir, "where to write the result artifact (empty to skip writing)")
	cmd.Flags().StringVar(&manifestDir, "manifests-dir", defaultManifestDir, "where to write the reproducibility manifest (empty to skip writing)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print as JSON instead of a table")
	cmd.Flags().BoolVar(&live, "live", false, "use the configured judge for the Jev arm (a real, billed network call); without this flag the arm runs against no judge and reports unavailable")
	cmd.Flags().Int64Var(&seed, "seed", 1, "seed recorded in the manifest for any randomized step (bootstrap CI, case sampling)")
	return cmd
}

func writeArms(w interface{ Write([]byte) (int, error) }, arms map[judgeval.Arm]judgeval.ArmResult) {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ARM\tAVAILABLE\tN\tREQUESTS\tNOTE")
	for _, arm := range []judgeval.Arm{judgeval.ArmDeterministicBaseline, judgeval.ArmLocalControl, judgeval.ArmJev} {
		r, ok := arms[arm]
		if !ok {
			continue
		}
		note := ""
		if !r.Available {
			note = r.UnavailableReason
		}
		fmt.Fprintf(tw, "%s\t%v\t%d\t%d\t%s\n", r.Arm, r.Available, r.N, r.Requests, note)
	}
	tw.Flush()
	for _, arm := range []judgeval.Arm{judgeval.ArmJev} {
		r, ok := arms[arm]
		if !ok || r.Classification == nil {
			continue
		}
		fmt.Fprintf(w, "\n%s classification (ambiguous excluded: %d of %d):\n",
			r.Arm, r.Classification.Ambiguous, r.Classification.Total)
		ctw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
		fmt.Fprintln(ctw, "LABEL\tTP\tFP\tFN\tPRECISION\tRECALL\tF1")
		for _, c := range r.Classification.Classes {
			fmt.Fprintf(ctw, "%s\t%d\t%d\t%d\t%.3f\t%.3f\t%.3f\n", c.Label, c.TP, c.FP, c.FN, c.Precision, c.Recall, c.F1)
		}
		ctw.Flush()
	}
}

func newJudgmentEligibilityCmd() *cobra.Command {
	var policyPath, datasetDir string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "eligibility <site>",
		Short: "Report whether a judgment site's evidence supports a promotion, without promoting it",
		Long: "Reads the site's live shadow calibration report (internal/ledger.Calibrate)\n" +
			"and evals/judgment/policy.yaml, and reports whether the evidence clears\n" +
			"every configured requirement for ordering and for routing.\n\n" +
			"This never edits judgment.yaml. A promotion is a line a person adds there\n" +
			"after reading this report, never something this command does.\n\n" +
			"A threshold policy.yaml has not set reads as an unconditional blocking\n" +
			"reason, not as a pass: no site may be eligible on a requirement no one has\n" +
			"chosen a real value for. Run `bcode judgment policy init` to write a starting\n" +
			"policy file with every threshold marked as requiring selection.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			site := args[0]
			info, ok := judgment.Site(site)
			if !ok {
				return fmt.Errorf("%q is not a judgment site this build defines; "+
					"run `bcode judgment sites` for the list", site)
			}

			pf, err := judgeval.LoadPolicy(policyPath)
			if err != nil {
				return err
			}
			policy := pf.For(info)

			_, root, st, err := openWorkspace(ctx)
			if err != nil {
				return err
			}
			defer closeRoot(cmd, root)

			calib := ledger.NewCalibrationStore(st)
			pairs, err := calib.Paired(ctx, site)
			if err != nil {
				return err
			}
			shadowPairs := make([]judgeval.PredictedOutcome, 0, len(pairs))
			for _, p := range pairs {
				if !p.Intervened {
					shadowPairs = append(shadowPairs, judgeval.PredictedOutcome{Predicted: p.Predicted, Outcome: p.Outcome})
				}
			}
			hasRun := len(pairs) > 0
			brier := judgeval.ScoreBrier(shadowPairs, ledger.MinCalibrationSample, 2000, 1)

			e := judgeval.Evaluate(judgeval.EligibilityInput{
				Site: info, Policy: policy,
				HeldoutShadow: brier, HasHeldoutRun: hasRun,
			})

			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(e)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "site:             %s\ncurrent tier:     %s\ndeclared ceiling: %s\n\n",
				e.Site, e.CurrentTier, e.DeclaredCeiling)
			if e.SafetyBlocked {
				fmt.Fprintf(w, "SAFETY BLOCKED — this overrides every other figure:\n")
				for _, r := range e.SafetyReasons {
					fmt.Fprintf(w, "  - %s\n", r)
				}
				return nil
			}
			writeEligibilitySection(w, "ordering", e.OrderingEligible, e.OrderingReasons)
			writeEligibilitySection(w, "routing", e.RoutingEligible, e.RoutingReasons)
			fmt.Fprintf(w, "\nThis report is advisory. Nothing here writes judgment.yaml; a promotion is\n"+
				"a person's decision after reading it, not this command's.\n"+
				"Evidence note: shadow (non-intervened) N=%d from ordinary task runs; no\n"+
				"held-out campaign evidence is included above unless a `bcode judgment eval\n"+
				"--split heldout` run's manifest was folded into this report by hand — this\n"+
				"tool currently reads live shadow calibration only.\n", brier.N)
			return nil
		},
	}
	cmd.Flags().StringVar(&policyPath, "policy", defaultPolicyPath, "promotion policy file")
	cmd.Flags().StringVar(&datasetDir, "dataset", defaultDatasetDir, "unused today; reserved for folding held-out campaign evidence in")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print as JSON")
	return cmd
}

func writeEligibilitySection(w interface{ Write([]byte) (int, error) }, name string, eligible bool, reasons []string) {
	status := "NO"
	if eligible {
		status = "YES"
	}
	fmt.Fprintf(w, "%s eligibility:\n  %s\n", name, status)
	if len(reasons) > 0 {
		fmt.Fprintf(w, "\nReasons:\n")
		for _, r := range reasons {
			fmt.Fprintf(w, "  - %s\n", r)
		}
	}
	fmt.Fprintln(w)
}

func newJudgmentPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Manage the judgment promotion-policy file",
	}
	cmd.AddCommand(newJudgmentPolicyInitCmd())
	return cmd
}

func newJudgmentPolicyInitCmd() *cobra.Command {
	var path string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a starting promotion-policy file with every threshold unconfigured",
		Long: "Writes one entry per registered judgment site, each with its correct effect\n" +
			"class and reporting floor, and every promotion threshold marked as requiring\n" +
			"a real, evidence-based value — never a guessed default. Edit the file before\n" +
			"`bcode judgment eligibility` can report anything as eligible.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("%s already exists; edit it directly, or remove it first "+
					"if you want a fresh unconfigured file", path)
			}
			if err := judgeval.WriteDefaultPolicy(path); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s (%d sites, every threshold unconfigured)\n",
				path, len(judgment.KnownSites()))
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "path", defaultPolicyPath, "where to write the policy file")
	return cmd
}
