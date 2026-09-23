package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/eval"
	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/retrieval"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/supervisor"
)

// Diagnostics for whether a Jev score means what production treats it as
// meaning. They need a live service and say so rather than degrading: a
// stability measurement against a stub reports perfect stability, which is
// both true and worthless.

func newEvalStabilityCmd() *cobra.Command {
	var (
		query    string
		repeats  int
		seed     int64
		asJSON   bool
		maxCands int
	)
	cmd := &cobra.Command{
		Use:   "stability <objective>",
		Args:  cobra.ExactArgs(1),
		Short: "Measure whether a candidate's Jev score is stable under irrelevant request structure",
		Long: "Production compares a candidate's probability against a floor and against\n" +
			"other candidates'. Both assume the number is a property of the candidate.\n" +
			"It might instead be partly a property of the request — where the candidate\n" +
			"sat in the list, what else was in the batch, how much noise surrounded it.\n\n" +
			"This perturbs those three things and reports the distribution. No pass/fail\n" +
			"bar is imposed: the point of a first measurement is to find out what the\n" +
			"distribution looks like, and a limit invented before seeing it would be a\n" +
			"number pretending to be a finding.\n\n" +
			"Diagnostic only. Nothing here is consulted by a task.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			_, root, st, err := openWorkspace(ctx)
			if err != nil {
				return err
			}
			defer closeRoot(cmd, root)

			judge, err := supervisor.Judge(root, st, func(f string, a ...any) {
				fmt.Fprintf(cmd.ErrOrStderr(), f+"\n", a...)
			})
			if err != nil {
				return err
			}
			if !judge.Available() {
				return eval.ErrNoLiveJudge
			}

			// Real candidates from the real index: a synthesised list would
			// measure how the service treats made-up paths.
			retriever := retrieval.New(st)
			pkt, err := retriever.Build(ctx, retrieval.Request{
				Query: query, Objective: args[0], TokenBudget: 20000,
			})
			if err != nil {
				return err
			}
			candidates := pkt.Slices
			if maxCands > 0 && len(candidates) > maxCands {
				candidates = candidates[:maxCands]
			}
			if len(candidates) < 2 {
				return fmt.Errorf("retrieval produced %d candidate(s) for %q; "+
					"stability needs at least two. Index a repository first, or pass "+
					"a --query that matches something", len(candidates), query)
			}

			// Distractors come from the same index, drawn from a deliberately
			// unrelated query, so they are real files with no bearing on the
			// objective rather than invented noise.
			var distractors []retrieval.Slice
			if noise, err := retriever.Build(ctx, retrieval.Request{
				Query: "license copyright readme changelog", TokenBudget: 4000,
			}); err == nil {
				for _, s := range noise.Slices {
					if len(distractors) >= 5 {
						break
					}
					distractors = append(distractors, s)
				}
			}

			rep, err := eval.RunStability(ctx, eval.StabilityInput{
				Judge: judge, Objective: args[0], Candidates: candidates,
				Distractors: distractors, Repeats: repeats, Seed: seed,
			})
			if err != nil {
				return err
			}
			// The exact model and configuration these numbers describe.
			commit, dirty := eval.CaptureRevision(ctx, ".")
			jcfg, _ := judgment.Load(root.Layout().ConfigDir())
			prov := eval.Provenance{
				Commit: commit, Dirty: dirty,
				JudgmentModelRequested: jcfg.Model, Redact: string(jcfg.Redact),
				Seed: seed,
			}.Finalise()
			rep.ExperimentID = prov.ExperimentID

			w := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(rep)
			}
			fmt.Fprintf(w, "model %s  experiment %s  seed %d\n%d candidate(s), "+
				"%d request(s), %d input token(s)\n\n",
				rep.Model, rep.ExperimentID, rep.Seed, len(candidates),
				rep.Requests, rep.InputTokens)
			fmt.Fprintf(w, "%-18s %6s %10s %10s %10s %10s\n",
				"PERTURBATION", "N", "MEAN|Δ|", "P95|Δ|", "MEAN ρ", "TOP-K")
			for _, s := range rep.Summarise() {
				fmt.Fprintf(w, "%-18s %6d %10.4f %10.4f %10.3f %10.2f\n",
					s.Kind, s.Trials, s.MeanAbsDelta, s.P95AbsDelta, s.MeanRho, s.MeanTopK)
			}
			fmt.Fprintln(w, "\n|Δ| is the per-candidate probability change, which is what a\n"+
				"relevance floor compares against. ρ is rank correlation and TOP-K the\n"+
				"overlap of the leading candidates, which is what the packet fill\n"+
				"consumes: a score that moves while preserving order is a smaller\n"+
				"problem than the reverse.\n\n"+
				"No threshold is applied. Collect this on dev across several objectives\n"+
				"and decide a limit from the distribution.")
			return nil
		},
	}
	cmd.Flags().StringVar(&query, "query", "", "lexical search text; defaults to the objective")
	cmd.Flags().IntVar(&repeats, "repeats", 3, "perturbations of each kind")
	cmd.Flags().Int64Var(&seed, "seed", 1, "seeds the permutations, for reproducibility")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the full report as JSON")
	cmd.Flags().IntVar(&maxCands, "max-candidates", 20, "cap the candidate set")
	return cmd
}

func newEvalCalibrateCmd() *cobra.Command {
	var (
		dir    string
		bins   int
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "calibrate",
		Short: "Measure whether Jev relevance scores have useful probability semantics here",
		Long: "Production compares scores against an absolute floor. That only means\n" +
			"something if 0.35 means something — if candidates scoring 0.35 are relevant\n" +
			"about a third of the time. A score that ranks well and is badly calibrated\n" +
			"would make the ranking useful and every threshold arbitrary.\n\n" +
			"REQUIRED against NOT_REQUIRED is the calibration subset: the two categories\n" +
			"mean exactly what the question asks. USEFUL is reported separately and\n" +
			"never folded into either — coercing it would make the figure an artefact of\n" +
			"the coercion.\n\n" +
			"Scores are stratified by retrieval origin, which is the assumption the\n" +
			"whole cross-origin ordering rests on and has never been checked.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			_, root, st, err := openWorkspace(ctx)
			if err != nil {
				return err
			}
			defer closeRoot(cmd, root)

			judge, err := supervisor.Judge(root, st, func(f string, a ...any) {
				fmt.Fprintf(cmd.ErrOrStderr(), f+"\n", a...)
			})
			if err != nil {
				return err
			}
			if !judge.Available() {
				return eval.ErrNoLiveJudge
			}
			tasks, err := eval.LoadSet(dir)
			if err != nil {
				return err
			}
			points, err := collectCalibrationPoints(ctx, cmd, root, dir, tasks, judge)
			if err != nil {
				return err
			}
			if len(points) == 0 {
				return errors.New("no candidate carries both a Jev score and a human label; " +
					"annotate some dev tasks first (`bcode eval annotate`)")
			}
			results := eval.CalibrationByOrigin(points, bins)
			w := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(results)
			}
			for _, c := range results {
				fmt.Fprintln(w, c.Format())
			}
			fmt.Fprintln(w, "Calibration is diagnostic. A well-calibrated reranker that does "+
				"not improve\ntask success is still not worth running.")
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "tasks", "evals/tasks", "the task set directory")
	cmd.Flags().IntVar(&bins, "bins", eval.DefaultCalibrationBins, "reliability bands")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the full report as JSON")
	return cmd
}

// collectCalibrationPoints scores each annotated task's real candidates and
// pairs them with their labels.
func collectCalibrationPoints(ctx context.Context, cmd *cobra.Command, root *store.Root,
	dir string, tasks []eval.Task, judge judgment.Judge) ([]eval.CalibrationPoint, error) {

	var points []eval.CalibrationPoint
	for _, t := range tasks {
		a, err := eval.LoadAnnotation(dir, t.ID)
		if err != nil {
			return nil, err
		}
		if a == nil {
			continue
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "scoring %s… ", t.ID)

		// The candidates come from the task's own fixture, indexed the way a
		// run would index it, so the scores are the ones production would
		// have produced.
		slices, release, err := eval.IndexFixture(ctx, root, t, analyzers(cmd))
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "skipped: %v\n", err)
			continue
		}
		res := retrieval.Rerank(ctx, judge, t.Objective, slices, retrieval.RerankTuning{}, nil)
		if !res.Applied {
			fmt.Fprintf(cmd.ErrOrStderr(), "skipped: %s\n", res.SkipReason)
			release()
			continue
		}
		var scored int
		for _, s := range slices {
			label, ok := a.Files[s.Path]
			if !ok {
				continue
			}
			points = append(points, eval.CalibrationPoint{
				TaskID: t.ID, Path: s.Path, Score: s.Relevance,
				Label: label, Origin: string(s.Origin),
			})
			scored++
		}
		release()
		fmt.Fprintf(cmd.ErrOrStderr(), "%d labelled candidate(s)\n", scored)
	}
	return points, nil
}
