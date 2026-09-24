package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/eval"
	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/llm"
	"github.com/akynte/boundedcode/internal/retrieval"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/supervisor"
	"github.com/akynte/boundedcode/internal/task"
)

// benchmarkContext is everything the preflight and the run both need, built
// once so the two cannot disagree about what is configured.
type benchmarkContext struct {
	Tasks      []Task
	Arms       []eval.Arm
	Provenance eval.Provenance
	Preflight  eval.Preflight
	Judge      judgment.Judge
	Embedder   *retrieval.LocalReranker
	Warnings   []string
}

// Task is an alias so this file reads without importing the name twice.
type Task = eval.Task

type benchmarkOptions struct {
	TaskDir              string
	Set                  string
	ArmNames             []string
	Repeat               int
	Benchmark            bool
	RequireClean         bool
	Tuning               retrieval.RerankTuning
	Localize             task.LocalizeTuning
	Cache                eval.CachePolicy
	OrderSeed            int64
	AllowUnverifiedModel bool
}

// prepareBenchmark loads the set, applies annotations, captures provenance and
// runs the preflight. It performs the authenticated smoke call when an arm
// needs the external judge, because a benchmark must not discover at minute
// forty that its treatment was never available.
func prepareBenchmark(ctx context.Context, cmd *cobra.Command, root *store.Root, st *store.Store,
	cfg config.Config, router *llm.Router, profile *config.Profile,
	opts benchmarkOptions) (*benchmarkContext, error) {

	errf := func(f string, a ...any) { fmt.Fprintf(cmd.ErrOrStderr(), f+"\n", a...) }

	tasks, err := eval.LoadSet(opts.TaskDir)
	if err != nil {
		return nil, err
	}
	// Ground truth is folded in here, on the evaluator side, after loading and
	// before anything runs. It never travels with the task into a solver.
	tasks, warnings, err := eval.ApplyAnnotations(opts.TaskDir, tasks)
	if err != nil {
		return nil, err
	}
	if opts.Set != "" {
		tasks = filterSet(tasks, eval.Set(opts.Set))
	}

	arms := make([]eval.Arm, 0, len(opts.ArmNames))
	for _, name := range opts.ArmNames {
		a, err := eval.ArmByName(name)
		if err != nil {
			return nil, err
		}
		arms = append(arms, a)
	}

	commit, dirty := eval.CaptureRevision(ctx, ".")
	dataset, membership, annotation, scorable := eval.DatasetDigests(tasks)
	coverage, err := eval.AnnotationCoverage(opts.TaskDir, tasks)
	if err != nil {
		return nil, err
	}
	reliability, err := eval.Reliability(opts.TaskDir, tasks)
	if err != nil {
		return nil, err
	}

	prov := eval.Provenance{
		Commit: commit, Dirty: dirty,
		Set: opts.Set, Repeat: opts.Repeat, Arms: opts.ArmNames,
		Tuning: opts.Tuning, LocalizeTuning: opts.Localize,
		Cache:                opts.Cache,
		AllowUnverifiedModel: opts.AllowUnverifiedModel,
		DatasetDigest:        dataset, MembershipDigest: membership,
		AnnotationDigest: annotation, TaskCount: len(tasks), ScorableCount: scorable,
		Runtime: eval.CaptureRuntime(0, 0),
	}
	if coverage.Annotated > 0 {
		prov.AnnotationRubricVersion = eval.AnnotationRubricVersion
	}
	if profile != nil {
		prov.Inference = eval.InferenceParams{
			Temperature: profile.Sampling.Temperature, MaxTokens: profile.ReservedOutput,
			ContextTokens: profile.ContextTokens, MaxSteps: profile.MaxSteps,
			MaxTools: profile.ToolSurfaceMax, Thinking: profile.Thinking,
			Profile: profile.Name,
		}
	}
	if p, err := router.For(llm.RoleCoding); err == nil && p != nil {
		prov.ReasoningModel = p.Name()
	}

	bc := &benchmarkContext{Tasks: tasks, Arms: arms, Warnings: warnings}

	// The external judge, and the smoke call, only when an arm needs them.
	var wantsJudge, wantsLocal bool
	for _, a := range arms {
		switch a.Rerank {
		case eval.RerankJudged:
			wantsJudge = true
		case eval.RerankLocal:
			wantsLocal = true
		}
	}

	judge, err := supervisor.Judge(root, st, errf)
	if err != nil {
		return nil, err
	}
	bc.Judge = judge
	jcfg, _ := judgment.Load(root.Layout().ConfigDir())
	if jcfg.Enabled {
		prov.JudgmentModelRequested = jcfg.Model
		prov.JudgmentModelPinned = jcfg.PinnedModel()
		prov.Redact = string(jcfg.Redact)
	}

	var smokeErr error
	var served string
	if wantsJudge && judge.Available() {
		probe, cancel := context.WithTimeout(ctx, 30*time.Second)
		res, err := judgment.Smoke(probe, judge)
		cancel()
		smokeErr = err
		served = res.Served
		prov.JudgmentModelServed = served
	}

	if wantsLocal {
		if p, err := router.For(llm.RoleEmbedding); err == nil && p != nil &&
			p.Capabilities().Embeddings {
			bc.Embedder = &retrieval.LocalReranker{Provider: p}
			prov.EmbeddingModel = p.Name()
		}
	}

	bc.Provenance = prov.Finalise()
	bc.Preflight = eval.RunPreflight(ctx, eval.PreflightInput{
		Tasks: tasks, Arms: arms, Set: opts.Set, TaskDir: opts.TaskDir,
		Benchmark: opts.Benchmark, RequireClean: opts.RequireClean,
		JudgeAvailable: judge.Available(), SmokeErr: smokeErr, SmokeServed: served,
		EmbeddingAvailable: bc.Embedder != nil,
		Annotated:          coverage,
		HeldoutRequired:    eval.DefaultThresholds().HeldoutRealTasks,
		Provenance:         bc.Provenance,
		Admissions:         eval.AdmitAll(opts.TaskDir, tasks),
		Reliability:        reliability,
	})
	return bc, nil
}

func newEvalPreflightCmd() *cobra.Command {
	var (
		dir             string
		setName         string
		armList         []string
		repeat          int
		benchmark       bool
		requireClean    bool
		cacheMode       string
		allowUnverified bool
	)
	cmd := &cobra.Command{
		Use:   "preflight",
		Short: "Check whether a benchmark configuration would produce interpretable numbers",
		Long: "preflight reports what a run would record and what would make it\n" +
			"uninterpretable, before any of it is spent.\n\n" +
			"With --benchmark it applies the stricter rule: a treatment that is not\n" +
			"available stops the run rather than falling back. Falling back is correct\n" +
			"in a task and a silent lie in an experiment — an arm named after a treatment\n" +
			"it did not receive publishes the baseline's numbers under the treatment's\n" +
			"name, and nothing downstream can tell.",
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
				return fmt.Errorf("evaluation needs a model: run `bcode setup` (%w)", err)
			}
			router, err := llm.NewRouter(providers)
			if err != nil {
				return err
			}
			defer router.Close()

			bc, err := prepareBenchmark(ctx, cmd, root, st, cfg, router, loadProfile(root, cfg),
				benchmarkOptions{
					TaskDir: dir, Set: setName, ArmNames: armList, Repeat: repeat,
					Benchmark: benchmark, RequireClean: requireClean,
					Cache: cachePolicyFor(cacheMode), AllowUnverifiedModel: allowUnverified,
				})
			if err != nil {
				return err
			}
			for _, warning := range bc.Warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s\n", warning)
			}
			fmt.Fprint(cmd.OutOrStdout(), bc.Preflight.Format())
			if bc.Preflight.Blocked() {
				return fmt.Errorf("preflight failed")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "tasks", "evals/tasks", "the task set directory")
	cmd.Flags().StringVar(&setName, "set", "", "the `dev` or `heldout` set")
	cmd.Flags().StringSliceVar(&armList, "arms", []string{"supervised"}, "configurations to compare")
	cmd.Flags().IntVar(&repeat, "repeat", 3, "passes per cell")
	cmd.Flags().BoolVar(&benchmark, "benchmark", false,
		"apply the benchmark rule: a missing treatment stops the run instead of falling back")
	cmd.Flags().BoolVar(&requireClean, "require-clean", false,
		"refuse a dirty working tree, for a run whose numbers will be published")
	cmd.Flags().StringVar(&cacheMode, "cache", "", "`cold`, `warm` or `disabled`")
	cmd.Flags().BoolVar(&allowUnverified, "allow-unverified-model", false,
		"proceed when the judgment service names no model; non-publishable")
	return cmd
}
