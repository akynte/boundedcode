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
	"time"

	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/supervisor"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/workspace"
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
}

var _ BoundedExecutor = (*ProductionBounded)(nil)

// Close releases the per-run store and engine. The candidate branch/workspace
// is durable independently; closing the ledger cannot erase the result.
func (p *ProductionBounded) Close() error {
	if p == nil {
		return nil
	}
	var errs []error
	if p.Runner != nil && p.Runner.Engine != nil {
		errs = append(errs, p.Runner.Engine.Close())
	}
	if p.Store != nil {
		errs = append(errs, p.Store.Close())
	}
	return errors.Join(errs...)
}

// NewProductionAdapter constructs the real BOUNDED arm around a fresh factory.
func NewProductionAdapter(factory ProductionFactory) BoundedAdapter {
	return BoundedAdapter{Factory: func(ctx context.Context, req WorkerRequest) (BoundedExecutor, error) {
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
	t := task.Task{ID: taskID, Title: req.Task.Description, Kind: "supervised",
		Verification: verification, Budget: budget}
	if err := task.NewStore(p.Store).Create(ctx, t); err != nil {
		return WorkerResult{}, err
	}
	sessionID := p.SessionID
	if sessionID == "" {
		sessionID = "ses_bench_" + shortHash(req.RunID+req.Task.ID)
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
		return WorkerResult{TaskID: taskID, ReportedStatus: "error", FailureReason: fmt.Sprint(runErr)}, runErr
	}
	candidate, candidateErr := p.collectCandidate(req, out)
	if candidateErr != nil && runErr == nil {
		runErr = candidateErr
	}
	res := WorkerResult{
		ReportedSuccess: out.Accepted,
		ReportedStatus:  string(out.Task.State),
		FailureReason:   strings.Join(out.Reasons, "; "),
		TaskID:          taskID, CandidateDir: candidate,
		Events:  []Event{{Kind: "bounded_task_terminal", At: time.Now().UTC(), Detail: string(out.Task.State)}},
		Metrics: metricsFromOutcome(out),
	}
	if !out.Accepted && res.ReportedStatus == "" {
		res.ReportedStatus = "reported_failure"
	}
	return res, runErr
}

func metricsFromOutcome(out *task.Outcome) Metrics {
	m := Metrics{}
	if out == nil {
		return m
	}
	m.AssistantTurns = intPtr(out.Attempts)
	m.VerificationAttempts = intPtr(len(out.Results))
	m.Retries = intPtr(maxInt(0, out.Attempts-1))
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

func (p *ProductionBounded) collectCandidate(req WorkerRequest, out *task.Outcome) (string, error) {
	if out.Worktree != "" {
		if _, err := os.Stat(out.Worktree); err == nil {
			return out.Worktree, nil
		}
	}
	if out.Branch == "" {
		return req.Workspace, nil
	}
	// task.Runner preserves a changed task branch even after it removes the
	// checkout. Clone the disposable source repository and materialize the
	// branch for the independent evaluator; the source repository is never
	// modified by this operation.
	dst, err := os.MkdirTemp(filepath.Dir(req.Workspace), "bounded-candidate-")
	if err != nil {
		return "", err
	}
	if err := os.Remove(dst); err != nil {
		return "", err
	}
	if err := runGit(context.Background(), 2*time.Minute, filepath.Dir(req.Workspace), "clone", "--no-hardlinks", "--quiet", req.Workspace, dst); err != nil {
		return "", err
	}
	if err := runGit(context.Background(), 2*time.Minute, dst, "checkout", "--detach", "--quiet", out.Branch); err != nil {
		_ = os.RemoveAll(dst)
		return "", err
	}
	return dst, nil
}

// StoreFactory is a small helper for callers that already have a data root.
// It derives a distinct workspace id for every run, opens its ledger, and
// returns a store the production factory can wire into supervisor.Runner.
func StoreFactory(root *store.Root, runID string) (*store.Store, error) {
	if root == nil {
		return nil, errors.New("bench: nil store root")
	}
	id := workspaceIDForRun(runID)
	return root.OpenWorkspace(context.Background(), id)
}

func workspaceIDForRun(runID string) workspace.ID {
	// DeriveID applies the repository's shape/charset rules; a hand-built
	// "bench-" prefix would look clear but be rejected by store.OpenWorkspace.
	return workspace.DeriveID("/boundedcode-benchmark/"+runID, "", "benchmark")
}

// ProductionRunnerBuilder is the callback used by cmd/bcode to assemble the
// same runnerFor/engineFor path used by ordinary bcode task execution.
type ProductionRunnerBuilder func(context.Context, WorkerRequest, *store.Store) (*task.Runner, error)
