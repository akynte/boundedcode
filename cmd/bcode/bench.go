package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/bench"
	"github.com/akynte/boundedcode/internal/broker"
	"github.com/akynte/boundedcode/internal/engine"
	"github.com/akynte/boundedcode/internal/engine/native"
	"github.com/akynte/boundedcode/internal/index"
	"github.com/akynte/boundedcode/internal/sandbox"
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
			return emitJSONTo(cmd.OutOrStdout(), schedule)
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
	var suitePath, modeText, taskIDs, output, rawCommand, manifestPath string
	var runs int
	var seed int64
	var timeout time.Duration
	var smoke, rawShell, rerun bool
	var rerunIDs []string
	cmd := &cobra.Command{Use: "run", Short: "Execute a paired benchmark schedule", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, modes, selected, err := loadBenchSuite(suitePath, modeText, taskIDs, timeout)
		if err != nil {
			return err
		}
		if output == "" {
			output = defaultBenchOutput(s.ID)
		}
		if err := bench.ValidateOutputLocation(output, s); err != nil {
			return err
		}
		schedule, err := bench.BuildSchedule(s, modes, runs, seed, selected)
		if err != nil {
			return err
		}
		var manifest *bench.FrozenManifest
		if manifestPath != "" {
			m, err := bench.LoadManifest(manifestPath)
			if err != nil {
				return err
			}
			if err := bench.VerifyFrozenAt(cmd.Context(), s, m); err != nil {
				return err
			}
			manifest = &m
		} else {
			autoPath := filepath.Join(output, "manifest.json")
			if _, statErr := os.Stat(autoPath); statErr == nil {
				auto, err := bench.LoadManifest(autoPath)
				if err != nil {
					return err
				}
				if err := bench.VerifyFrozenAt(cmd.Context(), s, auto); err != nil {
					return err
				}
				manifest = &auto
			} else if !os.IsNotExist(statErr) {
				return statErr
			}
		}
		if s.Official && manifest == nil {
			return fmt.Errorf("bench: official suite runs require --manifest or an existing verified manifest")
		}
		runner, closeFn, err := configuredBenchRunnerForModes(cmd, s, modes, smoke, rawCommand, rawShell)
		if err != nil {
			return err
		}
		defer closeFn()
		runner.Suite, runner.OutputRoot = s, output
		runner.Manifest = manifest
		runner.Rerun = rerun
		rerunSet := map[string]bool{}
		if rerun {
			for _, e := range schedule.Entries {
				rerunSet[e.RunID] = true
			}
		}
		knownRunIDs := make(map[string]bool, len(schedule.Entries))
		for _, entry := range schedule.Entries {
			knownRunIDs[entry.RunID] = true
		}
		for _, id := range rerunIDs {
			id = strings.TrimSpace(id)
			if !knownRunIDs[id] {
				return fmt.Errorf("bench: --rerun-id %q is not in this schedule", id)
			}
			rerunSet[id] = true
		}
		results, runErr := runner.RunSchedule(cmd.Context(), schedule, rerunSet)
		fmt.Fprintf(cmd.ErrOrStderr(), "benchmark runs: %d (results: %s)\n", len(results), output)
		for _, r := range results {
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s: %s evaluator=%s\n", r.TaskID, r.Mode, r.Status, r.Evaluator.Status)
		}
		return runErr
	}}
	addPlanFlags(cmd, &suitePath, &modeText, &taskIDs, &runs, &seed, &timeout, nil)
	cmd.Flags().StringVar(&output, "output", "", "durable result directory")
	cmd.Flags().StringVar(&manifestPath, "manifest", "", "frozen manifest to verify and retain")
	cmd.Flags().BoolVar(&smoke, "smoke", false, "use the deterministic non-official smoke worker (required for benchmarks/smoke without a model)")
	cmd.Flags().StringVar(&rawCommand, "raw-command", "", "standalone OpenCode/raw worker command (argv fields; required for a real RAW arm)")
	cmd.Flags().BoolVar(&rawShell, "raw-shell", false, "execute --raw-command through /bin/sh")
	cmd.Flags().BoolVar(&rerun, "rerun", false, "rerun all completed run ids")
	cmd.Flags().StringSliceVar(&rerunIDs, "rerun-id", nil, "explicit completed run id(s) to rerun")
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
	if timeout < 0 {
		return bench.Suite{}, nil, nil, fmt.Errorf("bench: timeout cannot be negative")
	}
	if timeout > 0 {
		if timeout%time.Second != 0 {
			return bench.Suite{}, nil, nil, fmt.Errorf("bench: timeout must be a whole number of seconds")
		}
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

// configuredBenchRunner is retained as the two-mode constructor used by
// integrations that do not need to select a subset of arms.
//
//nolint:unused
func configuredBenchRunner(cmd *cobra.Command, suite bench.Suite, forceSmoke bool, rawCommand string, rawShell bool) (*bench.Runner, func(), error) {
	return configuredBenchRunnerForModes(cmd, suite, []bench.Mode{bench.Raw, bench.Bounded}, forceSmoke, rawCommand, rawShell)
}

func configuredBenchRunnerForModes(cmd *cobra.Command, suite bench.Suite, modes []bench.Mode, forceSmoke bool, rawCommand string, rawShell bool) (*bench.Runner, func(), error) {
	noop := func() {}
	hasRaw, hasBounded := false, false
	for _, mode := range modes {
		hasRaw = hasRaw || mode == bench.Raw
		hasBounded = hasBounded || mode == bench.Bounded
	}
	if !hasRaw && !hasBounded {
		return nil, noop, fmt.Errorf("bench: no execution mode selected")
	}
	if forceSmoke && suite.Official {
		return nil, noop, fmt.Errorf("bench: the deterministic smoke worker is forbidden for an official suite")
	}
	if forceSmoke || suite.Smoke {
		root, cleanup, err := bench.OpenPrivateRoot()
		if err != nil {
			return nil, noop, err
		}
		evalSandbox := bench.SelectCommandSandbox(cmd.Context())
		if err := bench.RequireConfinedRunner(evalSandbox, sandbox.NetworkNone); err != nil {
			_ = cleanup()
			return nil, noop, fmt.Errorf("bench: independent evaluator sandbox: %w", err)
		}
		return &bench.Runner{
			ForceSmoke:   true,
			Raw:          bench.RawAdapter{Worker: bench.NewSmokeRawWorker()},
			Bounded:      bench.BoundedAdapter{Factory: smokeBoundedFactoryWithRoot(cmd, root)},
			Materializer: bench.Materializer{Sandbox: evalSandbox},
			Evaluator:    bench.IndependentEvaluator{Runner: evalSandbox},
			Environment:  bench.CaptureEnvironment(cmd.Context(), mustCwd()),
		}, func() { _ = cleanup() }, nil
	}
	evalSandbox := bench.SelectCommandSandbox(cmd.Context())
	if err := bench.RequireConfinedRunner(evalSandbox, sandbox.NetworkNone); err != nil {
		return nil, noop, fmt.Errorf("bench: independent evaluator sandbox: %w", err)
	}
	runner := &bench.Runner{
		Materializer: bench.Materializer{Sandbox: evalSandbox},
		Evaluator:    bench.IndependentEvaluator{Runner: evalSandbox},
		Environment:  bench.CaptureEnvironment(cmd.Context(), mustCwd()),
	}
	if hasRaw {
		if suite.Official {
			return nil, noop, fmt.Errorf("bench: official RAW runs require an attested worker adapter; --raw-command cannot provide served-model identity")
		}
		if strings.TrimSpace(rawCommand) == "" {
			return nil, noop, fmt.Errorf("bench: --raw-command is required for a real RAW run; use --smoke only for the non-official infrastructure fixture")
		}
		rawFields := strings.Fields(rawCommand)
		if rawShell {
			rawFields = []string{rawCommand}
		}
		worker, err := bench.NewCommandWorker(rawFields, bench.WithCommandSandbox(evalSandbox), bench.WithCommandShell(rawShell), bench.WithCommandName("raw-opencode"))
		if err != nil {
			return nil, noop, err
		}
		runner.Raw = bench.RawAdapter{Worker: worker}
	}
	if hasBounded {
		ws, root, _, err := openWorkspace(cmd.Context())
		if err != nil {
			return nil, noop, err
		}
		runner.Bounded = bench.NewProductionAdapterWithRestore(configuredProductionFactoryWithRestore(cmd, ws, root))
		runner.Bounded.RequireProduction = suite.Official
		return runner, func() { _ = root.CloseAll() }, nil
	}
	return runner, noop, nil
}

func mustCwd() string { dir, _ := os.Getwd(); return dir }

func defaultBenchOutput(suiteID string) string {
	base := g.dataDir
	if base == "" {
		base = os.Getenv("BC_DATA")
	}
	if base == "" {
		base = store.DefaultDataDirPath()
	}
	return filepath.Join(base, "benchmarks", "results", suiteID)
}

// smokeBoundedFactory is the workspace-opening form used by embedders that
// construct a benchmark factory outside the CLI lifecycle.
//
//nolint:unused
func smokeBoundedFactory(cmd *cobra.Command) bench.BoundedExecutorFactory {
	return func(ctx context.Context, req bench.WorkerRequest) (bench.BoundedExecutor, error) {
		_, root, _, err := openWorkspace(ctx)
		if err != nil {
			return nil, err
		}
		return smokeBoundedFactoryWithRoot(cmd, root)(ctx, req)
	}
}

func smokeBoundedFactoryWithRoot(cmd *cobra.Command, root *store.Root) bench.BoundedExecutorFactory {
	return func(ctx context.Context, req bench.WorkerRequest) (bench.BoundedExecutor, error) {
		key := req.ExecutionID
		if key == "" {
			key = req.RunID
		}
		st, restore, err := bench.StoreFactoryWithRestore(ctx, root, key)
		if err != nil {
			return nil, err
		}
		keepStore := false
		defer func() {
			if !keepStore {
				_ = st.Close() //nolint:contextcheck // store cleanup is deliberately context-free
				_ = restore()
			}
		}()
		if err := prepareBenchmarkIndex(ctx, st, root, req.Workspace, func(f string, a ...any) {
			fmt.Fprintf(cmd.ErrOrStderr(), "bounded-index: "+f+"\n", a...)
		}); err != nil {
			_ = st.Close() //nolint:contextcheck // store cleanup is deliberately context-free
			return nil, err
		}
		eng := bench.NewSmokeEngine()
		runner, err := supervisor.Runner(ctx, root, st, eng, supervisor.Options{RepoRoot: req.Workspace, DisableOracle: true, DisableJudgment: true,
			Logf: func(f string, a ...any) { fmt.Fprintf(cmd.ErrOrStderr(), "bounded: "+f+"\n", a...) }})
		if err != nil {
			// The fallback still uses production task.Runner, worktrees,
			// ledger and recipes; it only skips optional graph/judge assembly
			// when a minimal host cannot construct the full Supervisor.
			sb := bench.SelectCommandSandbox(ctx)
			if sandboxErr := bench.RequireConfinedRunner(sb, sandbox.NetworkNone); sandboxErr != nil {
				_ = st.Close() //nolint:contextcheck // store cleanup is deliberately context-free
				return nil, sandboxErr
			}
			runner, err = task.NewRunner(st, eng, sb, "benchmark-smoke")
		}
		if err != nil {
			_ = st.Close() //nolint:contextcheck // store cleanup is deliberately context-free
			return nil, err
		}
		if sandboxErr := bench.RequireConfinedRunner(runner.Sandbox, sandbox.NetworkNone); sandboxErr != nil {
			_ = st.Close() //nolint:contextcheck // store cleanup is deliberately context-free
			return nil, sandboxErr
		}
		// A deterministic smoke engine has no model-backed planning role; keep
		// the simple production task loop while retaining its real verifier.
		runner.WorkflowModel = nil
		runner.Critic = nil
		// Benchmarks are unattended. Keep the production gate machinery in the
		// path, but use its explicit permissive policy so a task cannot stop
		// at a human decision that no benchmark operator will make.
		runner.Broker = broker.New(st, broker.Permissive())
		// The CLI owns the private root for the whole schedule. Closing it from
		// every per-run executor would close unrelated fresh stores and turn a
		// sequential resume into shared-lifecycle coupling; only restore the
		// active-workspace pointer here.
		keepStore = true
		return &bench.ProductionBounded{Store: st, Runner: runner, RepoRoot: req.Workspace, RestoreActive: restore}, nil
	}
}

func prepareBenchmarkIndex(ctx context.Context, st *store.Store, root *store.Root, repoPath string, warnf func(string, ...any)) error {
	opts := index.DefaultOptions()
	opts.Analyzers = supervisor.Analyzers(warnf)
	opts.Semantic = supervisor.Semantic(warnf)
	ix := index.New(st, opts)
	repoID := workspace.DeriveRepositoryID(st.ID(), ".", "")
	if err := ix.RegisterRepository(ctx, workspace.Repository{ID: repoID, Name: filepath.Base(repoPath), Path: ".", DefaultBranch: "main"}); err != nil {
		return err
	}
	stats, err := ix.Repository(ctx, repoID, repoPath)
	if err != nil {
		return err
	}
	if stats.Files == 0 {
		return fmt.Errorf("benchmark candidate %s indexed to no files", repoPath)
	}
	_ = root
	return nil
}

// configuredProductionFactory assembles the non-smoke BOUNDED arm through the
// same engine and Supervisor path as ordinary production task execution. It is
// kept as a named constructor so integrations do not accidentally call a
// benchmark-only task runner.
func configuredProductionFactoryWithRestore(cmd *cobra.Command, _ *workspace.Workspace, root *store.Root) bench.ProductionFactoryWithRestore {
	return func(ctx context.Context, req bench.WorkerRequest) (*store.Store, *task.Runner, func() error, error) {
		key := req.ExecutionID
		if key == "" {
			key = req.RunID
		}
		st, restore, err := bench.StoreFactoryWithRestore(ctx, root, key)
		if err != nil {
			return nil, nil, nil, err
		}
		keepStore := false
		defer func() {
			if !keepStore {
				_ = st.Close() //nolint:contextcheck // store cleanup is deliberately context-free
				_ = restore()
			}
		}()
		if err := prepareBenchmarkIndex(ctx, st, root, req.Workspace, func(f string, a ...any) {
			fmt.Fprintf(cmd.ErrOrStderr(), "bounded-index: "+f+"\n", a...)
		}); err != nil {
			_ = st.Close() //nolint:contextcheck // store cleanup is deliberately context-free
			return nil, nil, nil, err
		}
		candidateWorkspace := &workspace.Workspace{Root: req.Workspace}
		eng, err := engineForContext(ctx, cmd, root, st, candidateWorkspace)
		if err != nil {
			_ = st.Close() //nolint:contextcheck // store cleanup is deliberately context-free
			return nil, nil, nil, err
		}
		if nativeEngine, ok := eng.(*native.Engine); ok {
			if req.Model.Temperature != nil {
				nativeEngine.Temperature = *req.Model.Temperature
			}
			nativeEngine.TopP = req.Model.TopP
			nativeEngine.TopK = req.Model.TopK
			nativeEngine.Seed = req.Model.Seed
			if req.Model.ContextTokens > 0 {
				nativeEngine.ContextTokens = req.Model.ContextTokens
			}
			if req.Limits.MaxTokens > 0 {
				nativeEngine.MaxTokens = req.Limits.MaxTokens
			}
			if req.Limits.MaxGenerationRequests > 0 && (nativeEngine.MaxSteps <= 0 || nativeEngine.MaxSteps > req.Limits.MaxGenerationRequests) {
				nativeEngine.MaxSteps = req.Limits.MaxGenerationRequests
			}
		}
		r, err := supervisor.Runner(ctx, root, st, eng, supervisor.Options{RepoRoot: req.Workspace, DisableOracle: true, DisableJudgment: true,
			Logf: func(f string, a ...any) { fmt.Fprintf(cmd.ErrOrStderr(), "bounded: "+f+"\n", a...) }})
		if err != nil {
			_ = eng.Close()
			_ = st.Close() //nolint:contextcheck // store cleanup is deliberately context-free
			return nil, nil, nil, err
		}
		requiredNetwork := sandbox.NetworkNone
		if req.Network == "host" {
			requiredNetwork = sandbox.NetworkHost
		}
		if err := bench.RequireConfinedRunner(r.Sandbox, requiredNetwork); err != nil {
			_ = eng.Close()
			_ = st.Close() //nolint:contextcheck // store cleanup is deliberately context-free
			return nil, nil, nil, err
		}
		if err := bench.ValidateProductionModelRoles(r, req.Model, req.Smoke); err != nil {
			_ = eng.Close()
			_ = st.Close() //nolint:contextcheck // store cleanup is deliberately context-free
			return nil, nil, nil, err
		}
		r.SandboxSpec.Env = bench.AppendSafeEnvironment(r.SandboxSpec.Env, req.Environment)
		switch req.Network {
		case "":
			r.SandboxSpec.Network = sandbox.NetworkNone
			r.SandboxSpec.TCPConnect = nil
			r.SandboxSpec.AllowEphemeralTCP = false
		case "host":
			r.SandboxSpec.Network = sandbox.NetworkHost
		case "none":
			r.SandboxSpec.Network = sandbox.NetworkNone
			r.SandboxSpec.TCPConnect = nil
			r.SandboxSpec.AllowEphemeralTCP = false
		case "allowlist":
			_ = eng.Close()
			_ = st.Close() //nolint:contextcheck // store cleanup is deliberately context-free
			return nil, nil, nil, fmt.Errorf("bench: network_policy=allowlist is not implemented for the production benchmark path")
		default:
			_ = eng.Close()
			_ = st.Close() //nolint:contextcheck // store cleanup is deliberately context-free
			return nil, nil, nil, fmt.Errorf("bench: unsupported network policy %q", req.Network)
		}
		// Count and bound every provider generation in the production task
		// lifecycle, including structured planning/review calls that do not pass
		// through native.Engine.MaxSteps. The counter is local to this physical
		// execution and is reported in WorkerResult.Metrics.
		bench.ApplyFrozenModelConfig(r, eng, req.Model)
		bench.ApplyGenerationBudget(r, eng, req.Limits.MaxGenerationRequests)
		if r.Critic != nil && req.Limits.MaxTokens > 0 && (r.Critic.MaxTokens <= 0 || r.Critic.MaxTokens > req.Limits.MaxTokens) {
			r.Critic.MaxTokens = req.Limits.MaxTokens
		}
		// The benchmark has no human gate operator. This is an explicit,
		// recorded unattended policy, not a silently removed production
		// subsystem; the normal CLI keeps its configured gate policy.
		r.Broker = broker.New(st, broker.Permissive())
		keepStore = true
		return st, r, restore, nil
	}
}

func newBenchReportCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "report <results-path>", Short: "Report paired benchmark results", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		report, err := bench.ReportDirectory(args[0], !asJSON)
		if err != nil {
			return err
		}
		if asJSON {
			return emitJSONTo(cmd.OutOrStdout(), report)
		}
		return nil
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
			output = filepath.Join(defaultBenchOutput(s.ID), "manifest.json")
		}
		if err := bench.ValidateOutputLocation(filepath.Dir(output), s); err != nil {
			return err
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

// configuredProductionFactory preserves the original programmatic factory
// shape for embedders that do not need active-root restoration. The CLI uses
// configuredProductionFactoryWithRestore below.
func configuredProductionFactory(cmd *cobra.Command, ws *workspace.Workspace, root *store.Root) bench.ProductionFactory {
	factory := configuredProductionFactoryWithRestore(cmd, ws, root)
	return func(ctx context.Context, req bench.WorkerRequest) (*store.Store, *task.Runner, error) {
		st, runner, _, err := factory(ctx, req)
		return st, runner, err
	}
}

// Keep the production factory visibly linked to the command package's normal
// engine/supervisor assembly. A build that does not use it still retains the
// constructor for API users and the linker does not discard the source path.
var _ = configuredProductionFactory
var _ = json.Valid
var _ = engine.Engine(nil)
