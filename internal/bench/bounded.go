package bench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/supervisor"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/workspace"
	"github.com/akynte/boundedcode/internal/worktree"
)

// ProductionFactory creates the production task runner and its fresh store
// for one benchmark execution. A factory is required rather than a singleton:
// sharing a store would share the ledger, task memory, OpenCode state, and
// model cache across repetitions.
type ProductionFactory func(context.Context, WorkerRequest) (*store.Store, *task.Runner, error)

// ProductionBounded enters the existing BoundedCode task lifecycle. It creates
// a normal task.Task, records the initial candidate and session binding, calls
// the production task.Runner, and returns the resulting candidate. It does not
// implement a benchmark-only supervisor or completion decision.
type ProductionBounded struct {
	Store  *store.Store
	Runner *task.Runner
	// RepoRoot is the production policy/index root. The candidate passed in
	// the request remains the immutable source from which the task worktree is
	// created.
	RepoRoot string
	// Verification overrides the task declaration when non-empty.
	Verification recipe.Level
	// SessionID is optional; a stable benchmark session is generated otherwise.
	SessionID string
	// CloseRoot is set only by a factory that owns a private data root. Normal
	// production callers leave it nil because their root is shared by the
	// process and is closed by the CLI.
	CloseRoot func() error
	// GenerationBudget is the provider-side admission counter installed by the
	// benchmark production factory. It is nil for programmatic adapters that
	// deliberately supply their own task runner.
	GenerationBudget *GenerationBudget
	// RestoreActive returns the shared production root to the workspace that
	// was active before this physical execution. It is supplied by the CLI
	// factory; programmatic factories may leave it nil.
	RestoreActive func() error

	closeOnce sync.Once
	closeErr  error
}

var _ BoundedExecutor = (*ProductionBounded)(nil)

// Close releases the per-run store and engine. The candidate branch/workspace
// is durable independently; closing the ledger cannot erase the result.
func (p *ProductionBounded) Close() error {
	if p == nil {
		return nil
	}
	p.closeOnce.Do(func() {
		var errs []error
		if p.Runner != nil && p.Runner.Engine != nil {
			errs = append(errs, p.Runner.Engine.Close())
		}
		if p.Runner != nil {
			if p.Runner.WorkflowModel != nil {
				errs = append(errs, p.Runner.WorkflowModel.Close())
			}
			if p.Runner.ReviewModel != nil {
				errs = append(errs, p.Runner.ReviewModel.Close())
			}
			if p.Runner.Critic != nil && p.Runner.Critic.Provider != nil {
				errs = append(errs, p.Runner.Critic.Provider.Close())
			}
		}
		if p.Store != nil {
			errs = append(errs, p.Store.Close())
		}
		if p.RestoreActive != nil {
			errs = append(errs, p.RestoreActive())
		}
		if p.CloseRoot != nil {
			errs = append(errs, p.CloseRoot())
		}
		p.closeErr = errors.Join(errs...)
	})
	return p.closeErr
}

// NewProductionAdapter constructs the real BOUNDED arm around a fresh factory.
func NewProductionAdapter(factory ProductionFactory) BoundedAdapter {
	return BoundedAdapter{Factory: func(ctx context.Context, req WorkerRequest) (BoundedExecutor, error) {
		if factory == nil {
			return nil, errors.New("bench: production factory is nil")
		}
		st, runner, err := factory(ctx, req)
		if err != nil {
			return nil, err
		}
		if st == nil || runner == nil {
			return nil, errors.New("bench: production factory returned a nil store or task runner")
		}
		return &ProductionBounded{Store: st, Runner: runner, RepoRoot: req.Workspace, Verification: recipe.Level(req.Task.Verification)}, nil
	}}
}

// ProductionFactoryWithRestore is the CLI form of ProductionFactory. The
// callback is invoked after the per-run store closes and restores the root's
// active workspace, so a shared production data root cannot retain a closed
// benchmark store as its global active state.
type ProductionFactoryWithRestore func(context.Context, WorkerRequest) (*store.Store, *task.Runner, func() error, error)

// NewProductionAdapterWithRestore wires the production path while returning
// the root's prior active workspace after every physical execution.
func NewProductionAdapterWithRestore(factory ProductionFactoryWithRestore) BoundedAdapter {
	return BoundedAdapter{Factory: func(ctx context.Context, req WorkerRequest) (BoundedExecutor, error) {
		if factory == nil {
			return nil, errors.New("bench: production factory is nil")
		}
		st, runner, restore, err := factory(ctx, req)
		if err != nil {
			return nil, err
		}
		if st == nil || runner == nil {
			return nil, errors.New("bench: production factory returned a nil store or task runner")
		}
		return &ProductionBounded{Store: st, Runner: runner, RepoRoot: req.Workspace,
			Verification: recipe.Level(req.Task.Verification), RestoreActive: restore}, nil
	}}
}

func (p *ProductionBounded) Execute(ctx context.Context, req WorkerRequest) (WorkerResult, error) {
	if err := req.ValidateRequest(); err != nil {
		return WorkerResult{}, err
	}
	if p == nil || p.Store == nil || p.Runner == nil {
		return WorkerResult{}, errors.New("bench: production bounded adapter is not wired")
	}
	verification := p.Verification
	if verification == "" {
		verification = recipe.Standard
	}
	if _, ok := recipe.ParseLevel(string(verification)); !ok {
		return WorkerResult{}, fmt.Errorf("bench: task %s has unsupported verification level %q", req.Task.ID, verification)
	}
	if !req.Smoke && len(req.Task.MutableScope) == 0 {
		return WorkerResult{}, fmt.Errorf("bench: production task %q has no mutable_scope; native edits would be denied", req.Task.ID)
	}
	ApplyFrozenModelConfig(p.Runner, p.Runner.Engine, req.Model)
	if p.GenerationBudget == nil {
		if _, alreadyCounted := p.Runner.Engine.(interface{ BenchmarkGenerationRequests() int }); !alreadyCounted {
			p.GenerationBudget = ApplyGenerationBudget(p.Runner, p.Runner.Engine, req.Limits.MaxGenerationRequests)
		}
	}
	if err := ValidateProductionModelRoles(p.Runner, req.Model, req.Smoke); err != nil {
		return WorkerResult{}, err
	}
	if !req.Smoke && req.Model.Model != "" {
		configured, ok := configuredModel(p.Runner.Engine)
		if !ok {
			return WorkerResult{}, fmt.Errorf("bench: cannot inspect the configured model route for %s", req.Task.ID)
		}
		if req.Model.Provider != "" && configured.Provider != req.Model.Provider {
			return WorkerResult{}, fmt.Errorf("bench: configured provider %q does not match frozen provider %q", configured.Provider, req.Model.Provider)
		}
		if configured.Model != req.Model.Model {
			return WorkerResult{}, fmt.Errorf("bench: configured model %q does not match frozen model %q", configured.Model, req.Model.Model)
		}
		if req.Model.ContextTokens > 0 && configured.ContextTokens > 0 && configured.ContextTokens != req.Model.ContextTokens {
			return WorkerResult{}, fmt.Errorf("bench: configured context window %d does not match frozen context window %d", configured.ContextTokens, req.Model.ContextTokens)
		}
	}
	objective := req.Task.Title
	if req.Task.Description != "" && req.Task.Description != objective {
		objective += "\n\n" + req.Task.Description
	}
	if len(req.Task.VisibleValidation) > 0 {
		objective += "\n\nVisible validation commands:"
		for _, command := range req.Task.VisibleValidation {
			objective += "\n- " + strings.Join(command.Argv, " ")
		}
	}
	taskID := "bench-" + shortHash(req.RunID)
	// A fresh run id must not collide with a task left by a crashed process.
	if existing, err := task.NewStore(p.Store).Get(ctx, taskID); err == nil && existing.ID != "" {
		taskID = task.NewID("bench")
	}
	initial, err := ContentManifest(req.Workspace)
	if err != nil {
		return WorkerResult{}, err
	}
	budget := task.Budget{
		MaxAttempts:           req.Limits.MaxVerificationAttempts,
		MaxGenerationRequests: req.Limits.MaxGenerationRequests,
		MaxWallTime:           req.Limits.WallClock(),
		MaxTokens:             req.Limits.MaxTokens,
		Scope:                 append([]string(nil), req.Task.MutableScope...),
	}
	if budget.MaxAttempts <= 0 {
		budget.MaxAttempts = 1
	}
	t := task.Task{ID: taskID, Title: objective, Kind: "supervised",
		Verification: verification, Budget: budget}
	if err := task.NewStore(p.Store).Create(ctx, t); err != nil {
		return WorkerResult{}, err
	}
	sessionID := p.SessionID
	if sessionID == "" {
		sessionID = "ses_bench_" + shortHash(req.RunID+req.Task.ID+req.ExecutionID)
	}
	if !strings.HasPrefix(sessionID, "ses_") {
		sessionID = "ses_bench_" + shortHash(sessionID)
	}
	if err := supervisor.RecordEventCandidate(ctx, p.Store, taskID, ledger.KindSessionStart, map[string]any{
		"executor": "bounded-benchmark", "phase": "EDITOR", "session_id": sessionID,
		"benchmark_run_id": req.RunID, "pair_id": req.PairID,
	}, initial); err != nil {
		return WorkerResult{}, err
	}
	out, runErr := p.Runner.Run(ctx, taskID, req.Workspace)
	if out == nil {
		return WorkerResult{WorkerStarted: true, TaskID: taskID, ReportedStatus: "error", FailureReason: fmt.Sprint(runErr)}, runErr
	}
	collectBase := context.WithoutCancel(ctx)
	if ctx.Err() != nil {
		collectBase = ctx
	}
	collectCtx, collectCancel := context.WithTimeout(collectBase, 2*time.Minute)
	candidate, candidateErr := p.collectCandidate(collectCtx, req, out)
	collectCancel()
	if candidateErr != nil && runErr == nil {
		runErr = candidateErr
	}
	model, modelVerified := observedModel(p.Runner.Engine)
	if !req.Smoke && req.Model.Model != "" {
		if !modelVerified {
			runErr = errors.Join(runErr, fmt.Errorf("bench: the provider did not return a served model identity for %s", req.Task.ID))
		} else {
			if req.Model.Provider != "" && model.Provider != req.Model.Provider {
				runErr = errors.Join(runErr, fmt.Errorf("bench: served provider %q does not match frozen provider %q", model.Provider, req.Model.Provider))
			}
			if model.Model != req.Model.Model {
				runErr = errors.Join(runErr, fmt.Errorf("bench: served model %q does not match frozen model %q", model.Model, req.Model.Model))
			}
			if req.Model.ContextTokens > 0 && model.ContextTokens != req.Model.ContextTokens {
				runErr = errors.Join(runErr, fmt.Errorf("bench: served context window %d does not match frozen context window %d", model.ContextTokens, req.Model.ContextTokens))
			}
			if req.Model.Runtime != "" && model.Runtime != req.Model.Runtime {
				runErr = errors.Join(runErr, fmt.Errorf("bench: served runtime %q does not match frozen runtime %q", model.Runtime, req.Model.Runtime))
			}
			if req.Model.ModelFileSHA256 != "" && model.ModelFileSHA256 != req.Model.ModelFileSHA256 {
				runErr = errors.Join(runErr, fmt.Errorf("bench: served model-file hash %q does not match frozen hash %q", model.ModelFileSHA256, req.Model.ModelFileSHA256))
			}
			if req.Model.Quantization != "" && model.Quantization != req.Model.Quantization {
				runErr = errors.Join(runErr, fmt.Errorf("bench: served quantization %q does not match frozen quantization %q", model.Quantization, req.Model.Quantization))
			}
			if req.Model.Temperature != nil && (model.Temperature == nil || *model.Temperature != *req.Model.Temperature) {
				runErr = errors.Join(runErr, fmt.Errorf("bench: served temperature does not match the frozen sampling configuration"))
			}
			if req.Model.TopP != nil && (model.TopP == nil || *model.TopP != *req.Model.TopP) {
				runErr = errors.Join(runErr, fmt.Errorf("bench: served top_p does not match the frozen sampling configuration"))
			}
			if req.Model.TopK != nil && (model.TopK == nil || *model.TopK != *req.Model.TopK) {
				runErr = errors.Join(runErr, fmt.Errorf("bench: served top_k does not match the frozen sampling configuration"))
			}
			if req.Model.Seed != nil && (model.Seed == nil || *model.Seed != *req.Model.Seed) {
				runErr = errors.Join(runErr, fmt.Errorf("bench: served seed does not match the frozen sampling configuration"))
			}
		}
	}
	metrics := metricsFromOutcome(out)
	if p.GenerationBudget != nil {
		metrics.GenerationRequests = measuredInt(p.GenerationBudget.Used())
	} else if counted, ok := p.Runner.Engine.(interface{ BenchmarkGenerationRequests() int }); ok {
		metrics.GenerationRequests = measuredInt(counted.BenchmarkGenerationRequests())
	}
	res := WorkerResult{
		Model:           model,
		ModelVerified:   modelVerified,
		WorkerStarted:   true,
		ReportedSuccess: out.Accepted,
		ReportedStatus:  string(out.Task.State),
		FailureReason:   strings.Join(out.Reasons, "; "),
		TaskID:          taskID, CandidateDir: candidate,
		Events: []Event{{Kind: "bounded_task_terminal", At: time.Now().UTC(), Detail: string(out.Task.State),
			Fields: map[string]any{"run_id": req.RunID, "execution_id": req.ExecutionID, "pair_id": req.PairID}}},
		Metrics: metrics,
	}
	if !out.Accepted && res.ReportedStatus == "" {
		res.ReportedStatus = "reported_failure"
	}
	return res, runErr
}

func observedModel(eng interface{}) (ModelConfig, bool) {
	identified, ok := eng.(interface {
		BenchmarkModelIdentity() (provider, model, runtime string, contextTokens int)
	})
	if !ok {
		return ModelConfig{}, false
	}
	provider, model, runtime, contextTokens := identified.BenchmarkModelIdentity()
	if strings.TrimSpace(model) == "" || contextTokens <= 0 {
		return ModelConfig{}, false
	}
	return ModelConfig{Provider: provider, Model: model, Runtime: runtime, ContextTokens: contextTokens}, true
}

func configuredModel(eng interface{}) (ModelConfig, bool) {
	configured, ok := eng.(interface {
		BenchmarkModelRoute() (provider, model string, contextTokens int)
	})
	if !ok {
		return ModelConfig{}, false
	}
	provider, model, contextTokens := configured.BenchmarkModelRoute()
	if strings.TrimSpace(model) == "" {
		return ModelConfig{}, false
	}
	return ModelConfig{Provider: provider, Model: model, ContextTokens: contextTokens}, true
}

func metricsFromOutcome(out *task.Outcome) Metrics {
	m := Metrics{}
	if out == nil {
		return m
	}
	m.AssistantTurns = measuredInt(out.Attempts)
	// A production task attempt contains the model step and its verification
	// pass. VerificationAttempts is the externally budgeted attempt count, not
	// the number of individual recipe checks in the final result set.
	m.VerificationAttempts = measuredInt(out.Attempts)
	m.Retries = measuredInt(maxInt(0, out.Attempts-1))
	if out.TokensUsed > 0 {
		v := int64(out.TokensUsed)
		m.TotalTokens = &v
	}
	return m
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func shortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:16]
}

func (p *ProductionBounded) collectCandidate(ctx context.Context, req WorkerRequest, out *task.Outcome) (string, error) {
	if out.Worktree != "" {
		if samePath(out.Worktree, req.Workspace) {
			return req.Workspace, nil
		}
		if _, err := os.Stat(out.Worktree); err == nil {
			// The production worktree can contain uncommitted edits. Use the
			// production Snapshot primitive so the evaluator receives the
			// complete candidate, not merely the last committed branch head.
			if p.Runner.Worktrees != nil {
				base, baseErr := gitOutput(ctx, 2*time.Minute, req.Workspace, "rev-parse", "HEAD")
				if baseErr != nil {
					return "", baseErr
				}
				snapshot, snapshotErr := p.Runner.Worktrees.Snapshot(ctx, &worktree.Worktree{
					ID: "bench-" + shortHash(req.RunID+req.ExecutionID), Path: out.Worktree,
					Base: base, Repo: req.Workspace,
				})
				if snapshotErr != nil {
					return "", snapshotErr
				}
				return snapshot.Path, nil
			}
			// A programmatic task runner may not own a worktree manager. Clone
			// its committed branch as a conservative fallback; official adapters
			// use the production manager above.
			return p.cloneCandidate(ctx, out.Worktree, filepath.Dir(req.Workspace), "bounded-worktree-", "")
		}
	}
	if out.Branch == "" {
		return req.Workspace, nil
	}
	// task.Runner preserves a changed task branch even after it removes the
	// checkout. Clone the disposable source repository and materialize the
	// branch for the independent evaluator; the source repository is never
	// modified by this operation.
	return p.cloneCandidate(ctx, req.Workspace, filepath.Dir(req.Workspace), "bounded-candidate-", out.Branch)
}

func (p *ProductionBounded) cloneCandidate(ctx context.Context, source, parent, prefix, ref string) (string, error) {
	dst, err := os.MkdirTemp(parent, prefix)
	if err != nil {
		return "", err
	}
	if err := os.Remove(dst); err != nil {
		return "", err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(dst)
		}
	}()
	if err := runGit(ctx, 2*time.Minute, parent, "clone", "--no-hardlinks", "--quiet", source, dst); err != nil {
		return "", err
	}
	if ref != "" {
		if err := runGit(ctx, 2*time.Minute, dst, "checkout", "--detach", "--quiet", "origin/"+ref); err != nil {
			return "", err
		}
	}
	keep = true
	return dst, nil
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return comparablePath(a) == comparablePath(b)
}

// StoreFactory is a small helper for callers that already have a data root.
// It derives a distinct workspace id for every run, opens its ledger, and
// returns a store the production factory can wire into supervisor.Runner.
func StoreFactory(root *store.Root, runID string) (*store.Store, error) {
	return StoreFactoryContext(context.Background(), root, runID)
}

// StoreFactoryForRequest uses the physical execution identity when present.
// This is the form benchmark adapters should use; the legacy run-id-only
// helper remains deterministic for callers that deliberately manage their own
// store lifetime.
func StoreFactoryForRequest(ctx context.Context, root *store.Root, req WorkerRequest) (*store.Store, error) {
	key := req.ExecutionID
	if key == "" {
		key = req.RunID
	}
	return StoreFactoryContext(ctx, root, key)
}

// StoreFactoryContext is the bounded form used by a running benchmark. The
// context prevents a cancelled/expired arm from opening a fresh control-plane
// store after its external deadline has already fired.
func StoreFactoryContext(ctx context.Context, root *store.Root, runID string) (*store.Store, error) {
	if root == nil {
		return nil, errors.New("bench: nil store root")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id := workspaceIDForRun(runID)
	st, err := root.OpenWorkspace(ctx, id)
	if err != nil {
		return nil, err
	}
	if _, err := root.SwitchTo(ctx, st.ID()); err != nil {
		_ = st.Close() //nolint:contextcheck // store cleanup is deliberately context-free
		return nil, err
	}
	return st, nil
}

// StoreFactoryWithRestore is the lease form used by the CLI's production
// factory. The returned callback is idempotent and only changes the root when
// this execution still owns the active slot, so a later owner is never
// accidentally overwritten during cleanup.
func StoreFactoryWithRestore(ctx context.Context, root *store.Root, runID string) (*store.Store, func() error, error) {
	if root == nil {
		return nil, nil, errors.New("bench: nil store root")
	}
	previous := root.Active()
	dataRelease, err := acquireOutputLock(ctx, filepath.Join(root.Layout().Root(), ".benchmark-control"))
	if err != nil {
		return nil, nil, err
	}
	st, err := StoreFactoryContext(ctx, root, runID)
	if err != nil {
		_ = dataRelease()
		return nil, nil, err
	}
	var once sync.Once
	var restoreErr error
	restore := func() error {
		once.Do(func() {
			if root.Active() == st.ID() {
				_, restoreErr = root.SwitchTo(context.Background(), previous)
			}
			restoreErr = errors.Join(restoreErr, dataRelease())
		})
		return restoreErr
	}
	return st, restore, nil
}

func workspaceIDForRun(runID string) workspace.ID {
	// DeriveID applies the repository's shape/charset rules; a hand-built
	// "bench-" prefix would look clear but be rejected by store.OpenWorkspace.
	return workspace.DeriveID("/boundedcode-benchmark/"+runID, "", "benchmark")
}

// ProductionRunnerBuilder is the callback used by cmd/bcode to assemble the
// same runnerFor/engineFor path used by ordinary bcode task execution.
type ProductionRunnerBuilder func(context.Context, WorkerRequest, *store.Store) (*task.Runner, error)
