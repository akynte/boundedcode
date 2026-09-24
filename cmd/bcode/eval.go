package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/eval"
	"github.com/akynte/boundedcode/internal/index"
	"github.com/akynte/boundedcode/internal/llm"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/sandbox"
)

func newEvalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Measure the system against a task set",
		Long: "eval runs a task set through one or more configurations and reports how often\n" +
			"each solved the task.\n\n" +
			"Two properties make the numbers mean something. A task's acceptance tests are\n" +
			"never in the worktree while the task runs, so a model cannot satisfy a test it\n" +
			"can read. And the system's own verdict is recorded separately from the ground\n" +
			"truth, so \"claimed success and was wrong\" is its own number rather than\n" +
			"something averaged away.",
	}
	cmd.AddCommand(newEvalRunCmd(), newEvalTasksCmd(), newEvalArmsCmd(), newEvalReportCmd(),
		newEvalQualifyCmd(), newEvalResultsCmd(), newEvalAnnotateCmd(), newEvalAnnotationsCmd(),
		newEvalRubricCmd(), newEvalAdjudicateCmd(), newEvalPreflightCmd(), newEvalAdmitCmd(), newEvalIntegrityCmd(), newEvalRuntimeCmd(),
		newEvalAuditCmd(), newEvalReliabilityCmd(), newEvalParityCmd(), newEvalReadinessCmd(), newEvalStabilityCmd(), newEvalCalibrateCmd(),
		newEvalJudgesCmd())
	return cmd
}

func newEvalTasksCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "tasks",
		Short: "List and validate the task set",
		RunE: func(cmd *cobra.Command, _ []string) error {
			tasks, err := eval.LoadSet(dir)
			if err != nil {
				return err
			}
			tasks, warnings, err := eval.ApplyAnnotations(dir, tasks)
			if err != nil {
				return err
			}
			for _, warning := range warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s\n", warning)
			}
			w := cmd.OutOrStdout()
			leak := map[eval.LeakRisk]int{}
			sets := map[eval.Set]int{}
			var unscorable []string
			for _, t := range tasks {
				truth := "—"
				if len(t.Expected.Files) > 0 {
					truth = fmt.Sprintf("%d req", len(t.Expected.Files))
				} else {
					unscorable = append(unscorable, t.ID)
				}
				fmt.Fprintf(w, "%-24s %-9s %-13s %-9s %-8s %s\n", t.ID, t.Membership(),
					t.Category, t.LeakRisk, truth, truncateHead(t.Objective, 40))
				leak[t.LeakRisk]++
				sets[t.Membership()]++
			}
			fmt.Fprintf(w, "\nsets: dev=%d heldout=%d\n", sets[eval.SetDev], sets[eval.SetHeldout])
			if sets[eval.SetHeldout] == 0 {
				fmt.Fprintln(w,
					"No task is in the held-out set. Every task defaults to dev, so a\n"+
						"parameter fitted on this set is no longer measured by any task in it:\n"+
						"there is nothing to promote a tuned configuration against.")
			}
			if len(unscorable) > 0 {
				fmt.Fprintf(w, "\n%d task(s) carry no localization ground truth and are\n"+
					"unscorable_for_localization: %s.\nThey contribute to the solve rate and to "+
					"nothing else. `bcode eval rubric` says\nwhat a label means; `bcode eval annotate "+
					"<task>` records one. Nothing infers them.\n",
					len(unscorable), strings.Join(unscorable, ", "))
			}
			fmt.Fprintf(w, "\n%d task(s). Provenance:", len(tasks))
			risks := make([]string, 0, len(leak))
			for r, n := range leak {
				risks = append(risks, fmt.Sprintf(" %s=%d", r, n))
			}
			sort.Strings(risks)
			fmt.Fprintf(w, "%s\n", strings.Join(risks, ""))
			if leak[eval.LeakPublic] > 0 || leak[eval.LeakUnknown] > 0 {
				fmt.Fprintln(w,
					"\nSome tasks may have been seen in training. Results over those are an\n"+
						"upper bound, and the report says so.")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "tasks", "evals/tasks", "the task set directory")
	return cmd
}

func newEvalArmsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "arms",
		Short: "Describe the configurations being compared and what each isolates",
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			for _, a := range eval.Arms() {
				fmt.Fprintf(w, "%s\n", a.Name)
				for _, line := range wrapText(a.Description, 72) {
					fmt.Fprintf(w, "  %s\n", line)
				}
				fmt.Fprintln(w)
			}
			fmt.Fprintln(w, "Comparisons these are designed to answer:")
			for _, c := range eval.Comparisons() {
				fmt.Fprintf(w, "\n  %s\n    %s vs %s\n", c.Question, c.Baseline, c.Variant)
				for _, line := range wrapText("isolates: "+c.WhatItIsolates, 70) {
					fmt.Fprintf(w, "    %s\n", line)
				}
			}
			return nil
		},
	}
}

func newEvalRunCmd() *cobra.Command {
	var (
		dir             string
		armList         []string
		only            []string
		out             string
		rawDir          string
		asJSON          bool
		repeat          int
		setName         string
		benchmark       bool
		requireClean    bool
		cacheMode       string
		orderSeed       int64
		allowUnverified bool
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the task set and report the results",
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if setName != "" && !slices.Contains(eval.Sets(), eval.Set(setName)) {
				return fmt.Errorf("unknown set %q; one of dev, heldout", setName)
			}
			if cacheMode != "" && !eval.CacheMode(cacheMode).Valid() {
				return fmt.Errorf("unknown cache mode %q; one of cold, warm, disabled", cacheMode)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			_, root, st, err := openWorkspace(ctx)
			if err != nil {
				return err
			}
			defer closeRoot(cmd, root)

			cfg, err := loadConfig(root)
			if err != nil {
				return err
			}
			providers, err := llm.LoadProvidersFile(root.Layout().ConfigDir())
			if err != nil {
				return fmt.Errorf("evaluation needs a model: run `bcode setup` and complete the guided configuration (%w)", err)
			}
			router, err := llm.NewRouter(providers)
			if err != nil {
				return err
			}
			defer router.Close()

			sb, report := selectSandbox(ctx, cfg)
			if sb == nil {
				return fmt.Errorf("no sandbox runner is available; evaluation must not run unconfined")
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "sandbox: %s (%v)\n", report.Runner, report.Active)

			dirs, err := st.TaskDirs()
			if err != nil {
				return err
			}
			profile := loadProfile(root, cfg)

			// One preparation path for the preflight and the run, so the two
			// cannot disagree about what is configured. It applies the
			// annotations, captures provenance, and makes the authenticated
			// smoke call when an arm needs the judge.
			bc, err := prepareBenchmark(ctx, cmd, root, st, cfg, router, profile,
				benchmarkOptions{
					TaskDir: dir, Set: setName, ArmNames: armList, Repeat: repeat,
					Benchmark: benchmark, RequireClean: requireClean,
					Cache: cachePolicyFor(cacheMode), OrderSeed: orderSeed,
					AllowUnverifiedModel: allowUnverified,
				})
			if err != nil {
				return err
			}
			for _, warning := range bc.Warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s\n", warning)
			}
			tasks, arms := bc.Tasks, bc.Arms
			if len(only) > 0 {
				tasks = filterTasks(tasks, only)
				if len(tasks) == 0 {
					return fmt.Errorf("no task matched %v", only)
				}
			}
			if setName != "" && len(tasks) == 0 {
				return fmt.Errorf("no task is in the %s set. Tasks declare `set:` in their "+
					"definition and default to dev; `bcode eval tasks` lists them", setName)
			}
			fmt.Fprint(cmd.ErrOrStderr(), bc.Preflight.Format())
			if bc.Preflight.Blocked() {
				return fmt.Errorf("preflight failed:\n  - %s",
					strings.Join(bc.Preflight.Blockers(), "\n  - "))
			}

			solver := &eval.SystemSolver{
				Root: root, Store: st, Router: router, Sandbox: sb,
				// The same analyzers and index settings `bcode index` uses, so a
				// task copy is indexed the way a real repository would be.
				Analyzers: analyzers(cmd),
				Semantic:  semanticIndexer(cmd),
				IndexOptions: index.Options{
					MaxFileBytes: cfg.Index.MaxFileBytes,
					Excludes:     cfg.Index.Excludes,
					ChunkLines:   cfg.Index.ChunkLines,
				},
				SandboxSpec: sandbox.Spec{
					ReadOnly: cfg.Sandbox.ReadOnlyPaths,
					TmpDir:   dirs.Tmp,
					Env:      recipe.GoEnv(dirs.GoBuildCache, dirs.GoModCache, dirs.Tmp),
				},
				Logf: func(f string, a ...any) { fmt.Fprintf(cmd.ErrOrStderr(), f+"\n", a...) },
			}
			solver.Judge = bc.Judge
			solver.LocalReranker = bc.Embedder
			if profile != nil {
				solver.MaxTools = profile.ToolSurfaceMax
				solver.MaxTokens = profile.ReservedOutput
				solver.Temperature = profile.Sampling.Temperature
				solver.Thinking = profile.Thinking
				solver.ContextTokens = profile.ContextTokens
				solver.MaxSteps = profile.MaxSteps
			}

			runner := &eval.Runner{
				RawDir:  rawDir,
				WorkDir: filepath.Join(dirs.Tmp, "eval"),
				Logf:    func(f string, a ...any) { fmt.Fprintf(cmd.ErrOrStderr(), f+"\n", a...) },
			}

			if repeat < 1 {
				repeat = 1
			}
			total := len(tasks) * len(arms) * repeat
			fmt.Fprintf(cmd.ErrOrStderr(), "running %d task(s) across %d arm(s)", len(tasks), len(arms))
			if repeat > 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), ", %d times each", repeat)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), " = %d runs\n\n", total)

			var outcomes []eval.Outcome
			done := 0
			start := time.Now()
			// Repetitions are the outer loop so that an interrupted run still
			// covers every cell the same number of times, rather than leaving
			// the last arm with fewer samples than the first.
			// Task outermost, arms innermost and rotated. Running a whole
			// arm before the next gives the later one warmer caches, a
			// warmer model server and whatever thermal state the first pass
			// left; none of that is the treatment and all of it lands on one
			// side. Rotating per cell means no arm is systematically first,
			// and the order used is recorded so a reader can check.
			var armOrders []eval.ArmOrder
			for rep := 1; rep <= repeat; rep++ {
				for _, task := range tasks {
					ordered := eval.OrderArms(orderSeed, task.ID, rep, arms)
					names := make([]string, 0, len(ordered))
					for _, a := range ordered {
						names = append(names, a.Name)
					}
					armOrders = append(armOrders, eval.ArmOrder{
						Task: task.ID, Repetition: rep, Order: names,
					})
					for position, arm := range ordered {
						done++
						fmt.Fprintf(cmd.ErrOrStderr(), "[%d/%d] %s / %s", done, total, arm.Name, task.ID)
						if repeat > 1 {
							fmt.Fprintf(cmd.ErrOrStderr(), " (pass %d/%d)", rep, repeat)
						}
						fmt.Fprint(cmd.ErrOrStderr(), "… ")
						o := runner.Run(ctx, task, arm, solver)
						o.Repetition = rep
						o.ArmPosition = position + 1
						outcomes = append(outcomes, o)
						fmt.Fprintf(cmd.ErrOrStderr(), "%s (%s)\n", verdict(o), o.Duration.Round(time.Second))
						if ctx.Err() != nil {
							fmt.Fprintf(cmd.ErrOrStderr(), "\ninterrupted after %d run(s)\n", done)
							break
						}
					}
				}
			}

			rep := eval.Aggregate(outcomes, tasks)
			// The exact configuration these numbers were produced under, and
			// the identity derived from it. A benchmark that cannot state its
			// parameters cannot be reproduced, and a tuned parameter absent
			// from the artifact is one nobody can check was frozen.
			bc.Provenance.ArmOrders = armOrders
			bc.Provenance.StartedAt = start
			bc.Provenance = bc.Provenance.Finalise()
			bc.Provenance.RunID = bc.Provenance.RunFingerprint(start)

			// The served-model check again, after the run rather than before
			// it. Preflight asks one smoke call what the service would do;
			// this asks what it did. In benchmark mode a mismatch is not an
			// observation to record — the artifact would name a model that
			// did not produce these numbers.
			if benchmark {
				if ok, why := bc.Provenance.ValidForBenchmark(); !ok {
					return fmt.Errorf("benchmark run invalid: %s", why)
				}
			}
			rep.Provenance = bc.Provenance
			rep.Frozen = eval.FrozenConfig{
				Set:            setName,
				Repeat:         repeat,
				JudgmentModel:  bc.Provenance.JudgmentModelRequested,
				Tuning:         solver.Tuning,
				LocalizeTuning: solver.LocalizeTuning,
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "\ncompleted in %s\n\n", time.Since(start).Round(time.Second))

			if out != "" {
				if err := rep.Save(out); err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s\n", out)
			}
			if asJSON {
				return emitJSON(rep)
			}
			fmt.Fprint(cmd.OutOrStdout(), rep.Format())
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "tasks", "evals/tasks", "the task set directory")
	cmd.Flags().StringSliceVar(&armList, "arms", []string{"unsupervised", "supervised"},
		"configurations to compare; `bcode eval arms` describes them")
	cmd.Flags().StringSliceVar(&only, "task", nil, "run only these task ids")
	// Three, not one. The first real run of this harness changed verdict on 4
	// of 12 task/arm cells between passes, so a single pass cannot tell a
	// result from noise — and a default of 1 meant the honest thing was the
	// thing you had to remember to ask for. Three is the smallest number that
	// can show a cell disagreeing with itself; --repeat 1 is still there for
	// a quick check that the harness runs at all.
	cmd.Flags().IntVar(&repeat, "repeat", 3,
		"run the whole set this many times; one run of a cell is a sample, not a measurement")
	cmd.Flags().StringVar(&rawDir, "raw", "",
		"write every run, including failures, as its own file in this directory")
	cmd.Flags().StringVar(&out, "out", "", "write the report as JSON to this path")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON to stdout")
	cmd.Flags().StringVar(&setName, "set", "",
		"run only the `dev` or `heldout` tasks; empty runs every task in the directory")
	cmd.Flags().BoolVar(&benchmark, "benchmark", false,
		"apply the benchmark rule: a missing treatment stops the run instead of falling back")
	cmd.Flags().BoolVar(&requireClean, "require-clean", false,
		"refuse a dirty working tree, for a run whose numbers will be published")
	cmd.Flags().StringVar(&cacheMode, "cache", "",
		"`cold`, `warm` or `disabled` for every cache; cold is the controlled condition "+
			"for cost and latency, warm is what production does")
	cmd.Flags().Int64Var(&orderSeed, "order-seed", 1,
		"seeds the per-cell arm rotation, so no arm is systematically first")
	cmd.Flags().BoolVar(&allowUnverified, "allow-unverified-model", false,
		"proceed when the judgment service names no model in its response; the run is "+
			"then a development diagnostic and is marked non-publishable")
	return cmd
}

// cachePolicyFor turns the flag into a policy. Empty means the ordinary warm
// state: an unqualified run is not a benchmark, and pretending otherwise by
// defaulting to cold would make every casual run slow for no reason.
func cachePolicyFor(mode string) eval.CachePolicy {
	switch eval.CacheMode(mode) {
	case eval.CacheCold:
		return eval.ColdCachePolicy()
	case eval.CacheDisabled:
		return eval.CachePolicy{
			Judgment: eval.CacheDisabled, Embedding: eval.CacheDisabled,
			Retrieval: eval.CacheDisabled,
		}
	default:
		return eval.DefaultCachePolicy()
	}
}

// filterSet keeps the tasks belonging to one evaluation set.
func filterSet(tasks []eval.Task, set eval.Set) []eval.Task {
	out := make([]eval.Task, 0, len(tasks))
	for _, t := range tasks {
		if t.Membership() == set {
			out = append(out, t)
		}
	}
	return out
}

func newEvalReportCmd() *cobra.Command {
	var tasks string
	cmd := &cobra.Command{
		Use:   "report <results.json>",
		Short: "Render a saved result file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rep, err := eval.LoadReport(args[0])
			if err != nil {
				return err
			}
			// A result file written before grading existed holds the
			// acceptance output but no per-test counts. Given the set it was
			// run against, those can be recovered without re-running it.
			if tasks != "" {
				set, err := eval.LoadSet(tasks)
				if err != nil {
					return err
				}
				if n := rep.GradeWith(set); n > 0 {
					fmt.Fprintf(cmd.ErrOrStderr(),
						"graded %d run(s) from their saved acceptance output\n\n", n)
				}
			}
			fmt.Fprint(cmd.OutOrStdout(), rep.Format())
			return nil
		},
	}
	cmd.Flags().StringVar(&tasks, "tasks", "",
		"the task set the results were produced from, to grade runs recorded before grading existed")
	return cmd
}

func verdict(o eval.Outcome) string {
	switch {
	case o.Errored():
		return "ERROR"
	case o.Tampered:
		return "tampered"
	case o.FalseAccept:
		return "FALSE ACCEPT"
	case o.Solved:
		return "solved"
	case o.MissedSuccess:
		return "solved (unclaimed)"
	default:
		return "not solved"
	}
}

func filterTasks(tasks []eval.Task, ids []string) []eval.Task {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []eval.Task
	for _, t := range tasks {
		if want[t.ID] {
			out = append(out, t)
		}
	}
	return out
}

func wrapText(s string, width int) []string {
	var lines []string
	var cur string
	for _, word := range strings.Fields(s) {
		switch {
		case cur == "":
			cur = word
		case len(cur)+1+len(word) > width:
			lines = append(lines, cur)
			cur = word
		default:
			cur += " " + word
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}
