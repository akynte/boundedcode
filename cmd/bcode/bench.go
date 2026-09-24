package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/bench"
	"github.com/akynte/boundedcode/internal/engine"
	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/sandbox/bwrap"
	"github.com/akynte/boundedcode/internal/sandbox/container"
	"github.com/akynte/boundedcode/internal/sandbox/landlock"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/supervisor"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/workspace"
)

func newBenchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bench",
		Short: "Run paired RAW and BOUNDED coding benchmarks",
		Long: "Benchmark one immutable task through a raw OpenCode workflow and through " +
			"the real BoundedCode Supervisor path. The independent evaluator, not either " +
			"system's success claim, decides benchmark correctness.",
	}
	cmd.AddCommand(newBenchValidateCmd(), newBenchPlanCmd(), newBenchRunCmd(), newBenchReportCmd(), newBenchFreezeCmd())
	return cmd
}

func newBenchValidateCmd() *cobra.Command {
	var suitePath string
	cmd := &cobra.Command{Use: "validate", Short: "Validate a benchmark suite without running workers", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := bench.LoadSuite(suitePath)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "suite %s version %s hash %s: valid (%d task(s), smoke=%t, official=%t)\n", s.ID, s.Version, s.Hash(), len(s.Tasks), s.Smoke, s.Official)
		for _, t := range s.Tasks {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s: base=%s evaluator=%s\n", t.ID, t.BaseCommit, t.EvaluatorHashForDisplay())
		}
		return nil
	}}
	cmd.Flags().StringVar(&suitePath, "suite", "benchmarks/smoke", "benchmark suite directory or suite.yaml")
	return cmd
}

func newBenchPlanCmd() *cobra.Command {
	var suitePath, modeText, taskIDs string
	var runs int
	var seed int64
	var timeout time.Duration
	var asJSON bool
	cmd := &cobra.Command{Use: "plan", Short: "Show the deterministic execution schedule", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, modes, selected, err := loadBenchSuite(suitePath, modeText, taskIDs, timeout)
		if err != nil {
			return err
		}
		schedule, err := bench.BuildSchedule(s, modes, runs, seed, selected)
		if err != nil {
			return err
		}
		if asJSON {
			return emitJSON(schedule)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "suite=%s seed=%d runs=%d modes=%s entries=%d\n", s.ID, seed, runs, modeText, len(schedule.Entries))
		for _, e := range schedule.Entries {
			fmt.Fprintf(cmd.OutOrStdout(), "%04d %-7s %-24s pair=%s run=%s\n", e.RunIndex, e.Mode, e.TaskID, e.PairID, e.RunID)
		}
		for _, warning := range schedule.Warnings {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", warning)
		}
		return nil
	}}
	addPlanFlags(cmd, &suitePath, &modeText, &taskIDs, &runs, &seed, &timeout, &asJSON)
	return cmd
}

func newBenchRunCmd() *cobra.Command {
	var suitePath, modeText, taskIDs, output, rawCommand string
	var runs int
	var seed int64
	var timeout time.Duration
	var smoke, rawShell, rerun bool
	cmd := &cobra.Command{Use: "run", Short: "Execute a paired benchmark schedule", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, modes, selected, err := loadBenchSuite(suitePath, modeText, taskIDs, timeout)
		if err != nil {
			return err
		}
		if output == "" {
			output = "results/" + s.ID
		}
		schedule, err := bench.BuildSchedule(s, modes, runs, seed, selected)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(output, 0o750); err != nil {
			return err
		}
		schedulePath := output + "/schedule.json"
		if err := bench.WriteSchedule(schedulePath, schedule); err != nil {
			return err
		}
		runner, closeFn, err := configuredBenchRunner(cmd, s, smoke, rawCommand, rawShell)
		if err != nil {
			return err
		}
		defer closeFn()
		runner.Suite, runner.OutputRoot = s, output
		runner.Rerun = rerun
		rerunIDs := map[string]bool{}
		if rerun {
			for _, e := range schedule.Entries {
				rerunIDs[e.RunID] = true
			}
		}
		results, runErr := runner.RunSchedule(cmd.Context(), schedule, rerunIDs)
		if runErr != nil {
			return runErr
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "benchmark runs: %d (results: %s)\n", len(results), output)
		for _, r := range results {
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s: %s evaluator=%s\n", r.TaskID, r.Mode, r.Status, r.Evaluator.Status)
		}
		return nil
	}}
	addPlanFlags(cmd, &suitePath, &modeText, &taskIDs, &runs, &seed, &timeout, nil)
	cmd.Flags().StringVar(&output, "output", "", "durable result directory")
	cmd.Flags().BoolVar(&smoke, "smoke", false, "use the deterministic non-official smoke worker (required for benchmarks/smoke without a model)")
	cmd.Flags().StringVar(&rawCommand, "raw-command", "", "standalone OpenCode/raw worker command (argv fields; required for a real RAW arm)")
	cmd.Flags().BoolVar(&rawShell, "raw-shell", false, "execute --raw-command through /bin/sh")
	cmd.Flags().BoolVar(&rerun, "rerun", false, "rerun completed run ids")
	return cmd
}

func addPlanFlags(cmd *cobra.Command, suitePath, modeText, taskIDs *string, runs *int, seed *int64, timeout *time.Duration, asJSON *bool) {
	cmd.Flags().StringVar(suitePath, "suite", "benchmarks/smoke", "benchmark suite directory or suite.yaml")
	cmd.Flags().StringVar(modeText, "mode", "raw,bounded", "comma-separated modes")
	cmd.Flags().StringVar(taskIDs, "tasks", "", "comma-separated task ids (default: all)")
	cmd.Flags().IntVar(runs, "runs", 1, "repetitions per task")
	cmd.Flags().Int64Var(seed, "seed", 1, "schedule/bootstrap seed")
	cmd.Flags().DurationVar(timeout, "timeout", 0, "override every task's external wall-clock limit")
	if asJSON != nil {
		cmd.Flags().BoolVar(asJSON, "json", false, "emit machine-readable JSON")
	}
}

func loadBenchSuite(path, modeText, taskText string, timeout time.Duration) (bench.Suite, []bench.Mode, []string, error) {
	s, err := bench.LoadSuite(path)
	if err != nil {
		return bench.Suite{}, nil, nil, err
	}
	if timeout > 0 {
		s = s.WithWallClock(timeout)
	}
	modes := make([]bench.Mode, 0)
	for _, raw := range strings.Split(modeText, ",") {
		raw = strings.TrimSpace(strings.ToLower(raw))
		if raw == "" {
			continue
		}
		modes = append(modes, bench.Mode(raw))
	}
	var selected []string
	for _, id := range strings.Split(taskText, ",") {
		if id = strings.TrimSpace(id); id != "" {
			selected = append(selected, id)
		}
	}
	return s, modes, selected, nil
}

func configuredBenchRunner(cmd *cobra.Command, suite bench.Suite, forceSmoke bool, rawCommand string, rawShell bool) (*bench.Runner, func(), error) {
	noop := func() {}
	if forceSmoke || suite.Smoke {
		return &bench.Runner{
			Raw:         bench.RawAdapter{Worker: bench.NewSmokeRawWorker()},
			Bounded:     bench.BoundedAdapter{Factory: smokeBoundedFactory(cmd)},
			Environment: bench.CaptureEnvironment(cmd.Context(), mustCwd()),
		}, noop, nil
	}
	if strings.TrimSpace(rawCommand) == "" {
		return nil, noop, fmt.Errorf("bench: --raw-command is required for a real RAW run; use --smoke only for the non-official infrastructure fixture")
	}
	fields := strings.Fields(rawCommand)
	worker, err := bench.NewCommandWorker(fields, bench.WithCommandShell(rawShell), bench.WithCommandName("raw-opencode"))
	if err != nil {
		return nil, noop, err
	}
	ws, root, _, err := openWorkspace(cmd.Context())
	if err != nil {
		return nil, noop, err
	}
	return &bench.Runner{
		Raw:         bench.RawAdapter{Worker: worker},
		Bounded:     bench.BoundedAdapter{Factory: bench.NewProductionAdapter(configuredProductionFactory(cmd, ws, root)).Factory},
		Environment: bench.CaptureEnvironment(cmd.Context(), mustCwd()),
	}, func() { _ = root.CloseAll() }, nil
}

func mustCwd() string { dir, _ := os.Getwd(); return dir }

func smokeBoundedFactory(cmd *cobra.Command) bench.BoundedExecutorFactory {
	return func(ctx context.Context, req bench.WorkerRequest) (bench.BoundedExecutor, error) {
		ws, root, _, err := openWorkspace(ctx)
		if err != nil {
			return nil, err
		}
		st, err := bench.StoreFactory(root, req.RunID)
		if err != nil {
			_ = root.CloseAll()
			return nil, err
		}
		eng := bench.NewSmokeEngine()
		runner, err := supervisor.Runner(ctx, root, st, eng, supervisor.Options{RepoRoot: ws.Root,
			Logf: func(f string, a ...any) { fmt.Fprintf(cmd.ErrOrStderr(), "bounded: "+f+"\n", a...) }})
		if err != nil {
			// The fallback still uses production task.Runner, worktrees,
			// ledger and recipes; it only skips optional graph/judge assembly
			// when a minimal host cannot construct the full Supervisor.
			ll, _ := landlock.New()
			candidates := []sandbox.Runner{&container.Runner{}}
			if ll != nil {
				candidates = append([]sandbox.Runner{bwrap.New(ll), ll}, candidates...)
			}
			sb, _ := sandbox.Select(ctx, candidates)
			if sb == nil {
				_ = st.Close()
				_ = root.CloseAll()
				return nil, err
			}
			runner, err = task.NewRunner(st, eng, sb, "benchmark-smoke")
		}
		if err != nil {
			_ = st.Close()
			_ = root.CloseAll()
			return nil, err
		}
		// A deterministic smoke engine has no model-backed planning role; keep
		// the simple production task loop while retaining its real verifier.
		runner.WorkflowModel = nil
		runner.Critic = nil
		return &bench.ProductionBounded{Store: st, Runner: runner, RepoRoot: ws.Root}, nil
	}
}

// configuredProductionFactory is used by non-smoke runs once a real bounded
// worker is selected in a future CLI wiring change. It is kept as a named
// constructor so integrations do not accidentally call a benchmark-only task
// runner.
func configuredProductionFactory(cmd *cobra.Command, ws *workspace.Workspace, root *store.Root) bench.ProductionFactory {
	return func(ctx context.Context, req bench.WorkerRequest) (*store.Store, *task.Runner, error) {
		st, err := bench.StoreFactory(root, req.RunID)
		if err != nil {
			return nil, nil, err
		}
		eng, err := engineFor(cmd, root, st, ws)
		if err != nil {
			_ = st.Close()
			return nil, nil, err
		}
		r, err := supervisor.Runner(ctx, root, st, eng, supervisor.Options{RepoRoot: ws.Root,
			Logf: func(f string, a ...any) { fmt.Fprintf(cmd.ErrOrStderr(), "bounded: "+f+"\n", a...) }})
		if err != nil {
			_ = eng.Close()
			_ = st.Close()
			return nil, nil, err
		}
		return st, r, nil
	}
}

func newBenchReportCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "report <results-path>", Short: "Report paired benchmark results", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		results, err := bench.LoadResults(args[0])
		if err != nil {
			return err
		}
		report := bench.BuildReport(results)
		if asJSON {
			return emitJSON(report)
		}
		_, _, err = bench.WriteReport(args[0], report)
		return err
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the machine-readable report")
	return cmd
}

func newBenchFreezeCmd() *cobra.Command {
	var suitePath, output string
	var seed int64
	var official bool
	cmd := &cobra.Command{Use: "freeze", Short: "Write a frozen benchmark manifest", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := bench.LoadSuite(suitePath)
		if err != nil {
			return err
		}
		m, err := bench.Freeze(cmd.Context(), s, seed, official)
		if err != nil {
			return err
		}
		if output == "" {
			output = strings.TrimSuffix(s.SuitePath(), ".yaml") + ".manifest.json"
		}
		if err := bench.WriteManifest(output, m); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), output)
		return nil
	}}
	cmd.Flags().StringVar(&suitePath, "suite", "benchmarks/smoke", "benchmark suite directory or suite.yaml")
	cmd.Flags().StringVar(&output, "output", "", "manifest output path")
	cmd.Flags().Int64Var(&seed, "seed", 1, "execution seed")
	cmd.Flags().BoolVar(&official, "official", false, "mark the manifest as an official run")
	return cmd
}

// Keep the production factory visibly linked to the command package's normal
// engine/supervisor assembly. A build that does not use it still retains the
// constructor for API users and the linker does not discard the source path.
var _ = configuredProductionFactory
var _ = json.Valid
var _ = engine.Engine(nil)
