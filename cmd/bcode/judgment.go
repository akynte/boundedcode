package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/supervisor"
)

func newJudgmentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "judgment",
		Short: "Inspect the external judgment service",
		Long: "Judgments are typed, calibrated answers from a service outside this machine,\n" +
			"used where deterministic code has to make a semantic call. They are off by\n" +
			"default, advisory everywhere, and can never decide whether work is accepted.",
	}
	cmd.AddCommand(newJudgmentSmokeCmd(), newJudgmentSitesCmd(), newJudgmentCalibrateCmd(),
		newJudgmentReplayCmd(), newJudgmentEvalCmd(), newJudgmentEligibilityCmd(), newJudgmentPolicyCmd(),
		newJudgmentConsultationsCmd())
	return cmd
}

func newJudgmentReplayCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "replay <task>",
		Short: "Replay a task's recorded judgments against the configuration in force now",
		Args:  cobra.ExactArgs(1),
		Long: "Every judgment a task made was recorded: which site asked, about what, what\n" +
			"probability it composed, which model answered, which question version it was,\n" +
			"the authority in force, and whether an effect was actually applied. replay\n" +
			"reads those records back and reports what the *current* judgment.yaml would\n" +
			"do with the same predictions — which is the question a promotion decision\n" +
			"asks: on this task, under this tier, what would this site have done?\n\n" +
			"It is read-only and offline. It makes no model call, runs no tool, writes no\n" +
			"repository file, and does not touch the task it reads.\n\n" +
			"What it does not replay: the model's answers. The journal records that a\n" +
			"request happened, against which model, with a digest of the state — not the\n" +
			"answers themselves. So this replays the composed predictions, which is the\n" +
			"layer control flow reads, and says so rather than pretending to more.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			taskID := args[0]

			_, root, st, err := openWorkspace(ctx)
			if err != nil {
				return err
			}
			defer closeRoot(cmd, root)

			cfg, err := judgment.Load(root.Layout().ConfigDir())
			if err != nil {
				return err
			}
			rep, err := ledger.NewCalibrationStore(st).ReplayTask(ctx, ledger.New(st), taskID,
				supervisor.SiteAuthority(cfg))
			if err != nil {
				return err
			}

			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(rep)
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "task:                %s\n", rep.TaskID)
			fmt.Fprintf(w, "judgment requests:   %d (from the journal)\n", rep.JudgmentOps)
			fmt.Fprintf(w, "recorded predictions: %d\n", len(rep.Records))
			if !rep.Replayable() {
				if rep.JudgmentOps == 0 {
					fmt.Fprintf(w, "\nNothing to replay: this task made no judgment requests. It may "+
						"have run with\njudgments disabled, which is the shipped default.\n")
				} else {
					fmt.Fprintf(w, "\nNothing to replay: this task made %d judgment request(s) but no site "+
						"composed a\nprediction from them — every site declined, found nothing, or ran "+
						"below the\nredaction tier it needs.\n", rep.JudgmentOps)
				}
				return nil
			}
			if len(rep.ModelsSeen) > 0 {
				fmt.Fprintf(w, "models:              %s\n", strings.Join(rep.ModelsSeen, ", "))
			}
			for _, s := range rep.UnknownSites {
				fmt.Fprintf(w, "\nnot replayed: %s is not a site this build registers; the record was "+
					"written by\na different build and is reported rather than replayed against a guess.\n", s)
			}
			for _, s := range rep.VersionMismatch {
				fmt.Fprintf(w, "\nnot replayed: %s — the recorded probability answers a question this "+
					"build no\nlonger asks, so replaying it would compare two different measurements.\n", s)
			}

			tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "\nSITE\tSUBJECT\tPREDICTED\tAPPLIED THEN\tOUTCOME")
			for _, rec := range rep.Records {
				outcome := "unresolved"
				if rec.Outcome != nil {
					outcome = "false"
					if *rec.Outcome {
						outcome = "true"
					}
				}
				fmt.Fprintf(tw, "%s\t%s\t%.2f\t%v\t%s\n",
					rec.Site, rec.Subject, rec.Predicted, rec.Intervened, outcome)
			}
			if err := tw.Flush(); err != nil {
				return err
			}

			if len(rep.Divergences) == 0 {
				fmt.Fprintf(w, "\nNo divergence: the configuration in force now would apply exactly "+
					"the effects\nthis task recorded.\n")
				return nil
			}
			fmt.Fprintf(w, "\n%d divergence(s) — the configuration in force now would act "+
				"differently:\n", len(rep.Divergences))
			dw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
			fmt.Fprintln(dw, "SITE\tSUBJECT\tTHEN\tNOW\tWHY")
			for _, d := range rep.Divergences {
				fmt.Fprintf(dw, "%s\t%s\t%s\t%s\t%s\n", d.Site, d.Subject, d.Recorded, d.Replayed, d.Reason)
			}
			return dw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print as JSON")
	return cmd
}

func newJudgmentSitesCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "sites",
		Short: "List judgment sites this build defines, and each one's configured tier",
		Long: "A judgment site is one named question a part of the harness asks a judge —\n" +
			"see docs/explanation/judgments.md. Every site starts at the logged tier: it\n" +
			"runs and journals, and nothing consumes the answer. A site's tier is promoted\n" +
			"by adding a line under `sites:` in judgment.yaml, never by this program.\n\n" +
			"This lists what the binary you are running defines, not what has ever been\n" +
			"asked: a site with no judge configured still appears here, at logged.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			_, root, _, err := openWorkspace(ctx)
			if err != nil {
				return err
			}
			defer closeRoot(cmd, root)

			cfg, err := judgment.Load(root.Layout().ConfigDir())
			if err != nil {
				return err
			}

			sites := judgment.KnownSites()
			if asJSON {
				type row struct {
					Name        string              `json:"name"`
					Mechanism   string              `json:"mechanism"`
					Version     string              `json:"version"`
					Tier        judgment.Tier       `json:"tier"`
					MaxEffect   judgment.Tier       `json:"max_effect"`
					Redaction   judgment.RedactMode `json:"redaction"`
					Paired      bool                `json:"paired"`
					Outcome     string              `json:"outcome,omitempty"`
					Description string              `json:"description"`
				}
				out := make([]row, 0, len(sites))
				for _, s := range sites {
					out = append(out, row{
						Name: s.Name, Mechanism: s.Mechanism, Version: s.Version,
						Tier: cfg.Tier(s.Name), MaxEffect: s.MaxEffect, Redaction: s.Redaction,
						Paired: s.Paired(), Outcome: s.Outcome, Description: s.Description,
					})
				}
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "SITE\tMECH\tVER\tTIER\tMAX\tREDACT\tPAIRED\tOUTCOME")
			paired := 0
			for _, s := range sites {
				mark, outcome := "no", "no outcome available yet"
				if s.Paired() {
					mark, outcome = "yes", s.Outcome
					paired++
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					s.Name, s.Mechanism, s.Version, cfg.Tier(s.Name), s.MaxEffect,
					s.Redaction, mark, outcome)
			}
			if err := w.Flush(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"\n%d site(s); %d record predictions paired with a later outcome, %d do not.\n"+
					"TIER is what judgment.yaml grants; MAX is the highest this site's own code\n"+
					"implements, and a configuration above it is refused. REDACT is the least\n"+
					"permissive redact mode under which the site runs at all.\n",
				len(sites), paired, len(sites)-paired)
			for _, s := range sites {
				fmt.Fprintf(cmd.OutOrStdout(), "\n%s (%s)\n  %s\n", s.Name, s.Mechanism, s.Description)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print as JSON")
	return cmd
}

func newJudgmentCalibrateCmd() *cobra.Command {
	var asJSON, byVersion bool
	cmd := &cobra.Command{
		Use:   "calibrate <site>",
		Short: "Report a judgment site's calibration from paired predictions and outcomes",
		Args:  cobra.ExactArgs(1),
		Long: "Every prediction a judgment site makes is recorded; at the points this\n" +
			"program knows an outcome — a gate decision, a task's terminal state, a\n" +
			"rerun's result — it is paired with what was recorded. This reads those\n" +
			"pairs back and reports the site's Brier skill (positive beats guessing the\n" +
			"site's own base rate, negative does worse) and a reliability table.\n\n" +
			"This is a diagnostic over ordinary use, not a benchmark: nothing here spends\n" +
			"a judgment or runs a task. Below a sample floor it reports the count and the\n" +
			"base rate honestly and declines to report a skill figure computed from too\n" +
			"few rows to mean anything.\n\n" +
			"See `bcode judgment sites` for the list of sites this build defines.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			site := args[0]

			known := false
			for _, s := range judgment.KnownSites() {
				if s.Name == site {
					known = true
					break
				}
			}
			if !known {
				return fmt.Errorf("%q is not a judgment site this build defines; "+
					"run `bcode judgment sites` for the list", site)
			}

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
			pending, err := calib.PendingCount(ctx, site)
			if err != nil {
				return err
			}
			if byVersion {
				reports := ledger.CalibrateGrouped(site, pairs, pending, 0)
				if asJSON {
					enc := json.NewEncoder(cmd.OutOrStdout())
					enc.SetIndent("", "  ")
					return enc.Encode(reports)
				}
				if len(reports) == 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "site:     %s\npending:  %d\n\n"+
						"No resolved predictions yet.\n", site, pending)
					return nil
				}
				for _, rep := range reports {
					v := rep.Versions[0]
					fmt.Fprintf(cmd.OutOrStdout(), "\n=== model %s, question version %s ===\n",
						orUnknown(v.Model), orUnknown(v.SiteVersion))
					writeCalibration(cmd.OutOrStdout(), rep)
				}
				return nil
			}

			report := ledger.Calibrate(site, pairs, pending, 0)
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(report)
			}
			writeCalibration(cmd.OutOrStdout(), report)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print as JSON")
	cmd.Flags().BoolVar(&byVersion, "by-version", false,
		"report each (model, question version) separately instead of pooling them")
	return cmd
}

func orUnknown(s string) string {
	if s == "" {
		return "(unrecorded)"
	}
	return s
}

// writeCalibration prints one report, shadow population first.
//
// Shadow before operational, and labelled, because they answer different
// questions: a shadow row is an observation of a world the prediction did
// not touch, and an operational row is one where the site's own effect could
// have moved the outcome it is now scored against. A promotion decision
// reads the shadow figure; the operational figure is reported beside it so a
// reader can see how far the two have diverged, never so the larger sample
// can stand in for the honest one.
func writeCalibration(w io.Writer, rep ledger.Report) {
	fmt.Fprintf(w, "site:     %s\n", rep.Site)
	fmt.Fprintf(w, "pending:  %d (predicted, outcome not yet known)\n", rep.Pending)
	if len(rep.Versions) > 1 {
		fmt.Fprintf(w, "\nThis report pools %d (model, question version) combinations. "+
			"Re-run with\n--by-version before reading a skill figure from it: a model change "+
			"or a reworded\nquestion is a different measurement.\n", len(rep.Versions))
	}
	if rep.Shadow.N == 0 && rep.Operational.N == 0 {
		fmt.Fprintf(w, "\nNo resolved predictions yet. This site may be at the logged tier "+
			"(which still\nrecords predictions) with nothing yet in a position to resolve "+
			"them, or it may\nnever have been asked.\n")
		return
	}
	writePopulation(w, rep.Shadow,
		"shadow — the prediction changed nothing, so the outcome is an observation")
	writePopulation(w, rep.Operational,
		"operational — an effect from this site was applied, so it may have moved this outcome")
	if rep.Operational.N > 0 && rep.Shadow.N < ledger.MinCalibrationSample {
		fmt.Fprintf(w, "\nThe shadow population is below the %d-row floor. Promotion should "+
			"rest on shadow\nevidence: scoring a site on outcomes its own effects helped "+
			"produce is self-confirming.\n", ledger.MinCalibrationSample)
	}
}

func writePopulation(w io.Writer, p ledger.Population, caption string) {
	fmt.Fprintf(w, "\n%s\n  %s\n", p.Name, caption)
	if p.N == 0 {
		fmt.Fprintf(w, "  no rows\n")
		return
	}
	fmt.Fprintf(w, "  resolved:   %d\n", p.N)
	fmt.Fprintf(w, "  positives:  %d\n", p.Positives)
	fmt.Fprintf(w, "  base rate:  %.3f\n", p.BaseRate)
	if !p.Interpretable {
		fmt.Fprintf(w, "  %d rows is below the %d this reports a skill figure on as meaningful;\n"+
			"  the count and base rate above are facts, a Brier score from this few is not.\n",
			p.N, ledger.MinCalibrationSample)
		return
	}
	fmt.Fprintf(w, "  brier:      %.4f (baseline %.4f, always guessing the base rate)\n",
		p.Brier, p.BrierBaseline)
	fmt.Fprintf(w, "  skill:      %+.3f (positive beats the base rate, negative does worse)\n", p.Skill)
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "  BAND\tCOUNT\tMEAN PREDICTED\tOBSERVED")
	for _, b := range p.Bins {
		if b.Count == 0 {
			continue
		}
		fmt.Fprintf(tw, "  %.1f-%.1f\t%d\t%.3f\t%.3f\n", b.Low, b.High, b.Count, b.MeanPredicted, b.Observed)
	}
	_ = tw.Flush()
}

func newJudgmentSmokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "smoke",
		Short: "Make one live call to confirm the configured model is accepted",
		Long: "smoke sends one trivial question carrying no repository content at all, and\n" +
			"reports the model the service says it ran.\n\n" +
			"Run it before a benchmark depends on a configuration. A model identifier that\n" +
			"has never been sent anywhere is an identifier nobody has checked: the unit\n" +
			"tests answer from a local stub and cannot tell a real snapshot from an\n" +
			"invented one.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			_, root, st, err := openWorkspace(ctx)
			if err != nil {
				return err
			}
			defer closeRoot(cmd, root)

			j, err := supervisor.Judge(root, st, func(f string, a ...any) {
				fmt.Fprintf(cmd.ErrOrStderr(), f+"\n", a...)
			})
			if err != nil {
				return err
			}
			// Not available has two causes now that a missing key no longer
			// stops construction, and they need different fixes: the
			// integration is off, or it is on and the credential is not
			// exported. Smoke distinguishes them, so let it answer rather
			// than reporting "not enabled" for a file that says enabled.
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			res, err := judgment.Smoke(ctx, j)
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "requested model: %s\n", res.Requested)
			if res.Served == "" {
				fmt.Fprintf(w, "served model:    (the service did not name one)\n")
			} else {
				fmt.Fprintf(w, "served model:    %s\n", res.Served)
			}
			if res.Served != "" && res.Served != res.Requested {
				fmt.Fprintf(w, "\nThe service ran a different model from the one requested. A\n"+
					"benchmark recorded against %q would be describing %q.\n",
					res.Requested, res.Served)
			}
			fmt.Fprintf(w, "answer:          %.2f\n", res.Noul)
			fmt.Fprintf(w, "tokens:          %d in, %d out\n",
				res.Usage.InputTokens, res.Usage.OutputTokens)
			return nil
		},
	}
}
