package supervisor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/worktree"
)

// EnsureTaskWorktree is the shared boundary for editor and native tasks. An
// OpenCode task is not allowed to use the operator checkout as its candidate:
// the same manager and the same Git worktree implementation that native tasks
// use are therefore opened before an editor is allowed to mutate anything.
//
// The path is deterministic from the task id, so a fresh OpenCode session can
// recover the authoritative checkout without trusting its conversation.
func EnsureTaskWorktree(ctx context.Context, st *store.Store, repoRoot, taskID string) (*worktree.Worktree, error) {
	dirs, err := st.TaskDirs()
	if err != nil {
		return nil, err
	}
	manager, err := worktree.NewManager(dirs.Worktrees)
	if err != nil {
		return nil, err
	}
	if wt, openErr := manager.Open(ctx, repoRoot, taskID); openErr == nil {
		return wt, nil
	}
	return manager.Create(ctx, repoRoot, taskID)
}

// TaskWorktree opens an existing task worktree without creating one. It is used
// by context reconstruction, where creating state as a side effect of rendering
// a prompt would itself violate the supervisor boundary.
func TaskWorktree(ctx context.Context, st *store.Store, taskID string) (*worktree.Worktree, error) {
	dirs, err := st.TaskDirs()
	if err != nil {
		return nil, err
	}
	manager, err := worktree.NewManager(dirs.Worktrees)
	if err != nil {
		return nil, err
	}
	return manager.Open(ctx, "", taskID)
}

// TaskWorktreePath returns the authoritative task checkout when one exists.
// Legacy tasks created before worktree supervision fall back to the supplied
// repository root only for read-only context reconstruction; mutating tools
// must use EnsureTaskWorktree and fail closed when creation is impossible.
func TaskWorktreePath(ctx context.Context, st *store.Store, taskID, fallback string) (string, error) {
	wt, err := TaskWorktree(ctx, st, taskID)
	if err == nil {
		return wt.Path, nil
	}
	if taskID == "" {
		return fallback, nil
	}
	return fallback, nil
}

// AcquireTaskLease claims a task worktree for one supervisor-authorized
// operation. A lease is deliberately scoped to an operation rather than to an
// OpenCode conversation: a model may think for minutes without holding a
// filesystem claim, while two sessions still cannot execute side effects in the
// same authoritative checkout concurrently.
func AcquireTaskLease(ctx context.Context, st *store.Store, taskID, sessionID string) (func(), error) {
	sessionID = strings.TrimSpace(sessionID)
	if !strings.HasPrefix(sessionID, "ses_") || len(sessionID) > 100 {
		return nil, fmt.Errorf("supervisor: a valid OpenCode session is required for a task lease")
	}
	wt, err := TaskWorktree(ctx, st, taskID)
	if err != nil {
		return nil, err
	}
	holder := "opencode/" + sessionID
	ledgerStore := ledger.New(st)
	if _, err := ledgerStore.AcquireLease(ctx, wt.ID, taskID, holder, 0); err != nil {
		return nil, err
	}
	return func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = ledgerStore.ReleaseLease(releaseCtx, wt.ID, holder)
	}, nil
}

// WorkerGenerationUsage returns the supervisor-owned generation budget for a
// task. It is used in durable context so a fresh session can see the same
// budget state rather than inferring progress from chat length.
func WorkerGenerationUsage(ctx context.Context, st *store.Store, taskID string) (used, limit int, err error) {
	t, err := task.NewStore(st).Get(ctx, taskID)
	if err != nil {
		return 0, 0, err
	}
	limit = t.Budget.MaxGenerationRequests
	if limit <= 0 {
		limit = 64
	}
	if err := st.Ledger().SQL().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM operations
		WHERE task_id=? AND kind='generation' AND outcome IS NOT NULL`, taskID).Scan(&used); err != nil {
		return 0, 0, err
	}
	return used, limit, nil
}

// AuthorizeModelRequest is called at the OpenCode provider boundary, before a
// worker can generate another turn. An unbound session may make the initial
// request needed to discover the repository; once it has opened a task, every
// subsequent request is charged to that task's supervisor-owned budget.
//
// The legacy-shaped wrapper is useful to callers that do not own a route. The
// broker uses AuthorizeModelRequestOnRoute so an OpenCode-selected provider or
// model cannot bypass the configured BoundedCode route.
func AuthorizeModelRequest(ctx context.Context, st *store.Store, sessionID, model string) error {
	return AuthorizeModelRequestOnRoute(ctx, st, sessionID, "", model, "", "")
}

// AuthorizeModelRequestOnRoute additionally enforces the Supervisor-selected
// provider/model pair. OpenCode may render that pair in its UI, but it cannot
// select a different one for a supervised task.
func AuthorizeModelRequestOnRoute(ctx context.Context, st *store.Store, sessionID, provider, model, allowedProvider, allowedModel string) error {
	sessionID = strings.TrimSpace(sessionID)
	if !strings.HasPrefix(sessionID, "ses_") || len(sessionID) > 100 {
		return fmt.Errorf("supervisor: invalid OpenCode session")
	}
	// OpenCode does not expose provider/model metadata on every provider hook,
	// especially on the first request of a fresh session. Missing metadata is
	// not a worker-selected alternate route; an explicit non-empty mismatch is.
	if allowedProvider != "" && provider != "" && provider != allowedProvider {
		return fmt.Errorf("supervisor: provider %q is not authorized; expected %q", provider, allowedProvider)
	}
	if allowedModel != "" && model != "" && model != allowedModel {
		return fmt.Errorf("supervisor: model %q is not authorized; expected %q", model, allowedModel)
	}
	var taskID string
	err := st.Ledger().SQL().QueryRowContext(ctx, `
		SELECT task_id FROM operations
		WHERE kind='session_start' AND json_valid(intent)
		  AND json_extract(intent, '$.session_id') = ?
		ORDER BY id DESC LIMIT 1`, sessionID).Scan(&taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	t, err := task.NewStore(st).Get(ctx, taskID)
	if err != nil {
		return err
	}
	if t.State.Terminal() {
		// OpenCode needs one provider turn to render the tool result after a
		// terminal finish. It is a read-only response boundary, not another
		// worker attempt; no task tool is authorized in this state. The first
		// such turn is recorded as the session end, and the next one is refused.
		var sessionStart int64
		if err := st.Ledger().SQL().QueryRowContext(ctx, `
			SELECT COALESCE(MAX(id), 0) FROM operations
			WHERE task_id=? AND kind='session_start'`, taskID).Scan(&sessionStart); err != nil {
			return err
		}
		var ended int
		if err := st.Ledger().SQL().QueryRowContext(ctx, `
			SELECT COUNT(*) FROM operations
			WHERE task_id=? AND kind='session_end' AND id>?`, taskID, sessionStart).Scan(&ended); err != nil {
			return err
		}
		if ended > 0 {
			return fmt.Errorf("supervisor: task %q is %s; the terminal response turn is already complete", taskID, t.State)
		}
		return RecordEvent(ctx, st, taskID, ledger.KindSessionEnd, map[string]any{
			"executor": "opencode", "session_id": sessionID, "reason": "terminal response",
		})
	}
	if t.State == task.StateBlocked || t.State == task.StatePaused {
		return fmt.Errorf("supervisor: task %q is %s; the supervisor is not accepting generation", taskID, t.State)
	}
	if t.Budget.MaxWallTime > 0 && time.Since(t.CreatedAt) > t.Budget.MaxWallTime {
		return fmt.Errorf("supervisor: task %q exceeded its wall-clock budget", taskID)
	}
	phase, err := CurrentPhase(ctx, st, taskID)
	if err != nil {
		return err
	}
	if phase != "EDITOR" {
		return fmt.Errorf("supervisor: task %q is in phase %q; generation is not authorized", taskID, phase)
	}
	maxRequests := t.Budget.MaxGenerationRequests
	if maxRequests <= 0 {
		maxRequests = 64
	}
	var requests int
	if err := st.Ledger().SQL().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM operations
		WHERE task_id=? AND kind='generation' AND outcome IS NOT NULL`, taskID).Scan(&requests); err != nil {
		return err
	}
	if requests >= maxRequests {
		return fmt.Errorf("supervisor: task %q exhausted its bounded worker-generation budget (%d/%d)", taskID, requests, maxRequests)
	}
	return RecordEvent(ctx, st, taskID, ledger.KindGeneration, map[string]any{
		"executor": "opencode", "session_id": sessionID, "model": model,
	})
}

// authorization: AuthorizeEditorOperation is the executable gate that turns a
// proposal into a task-scoped operation after checking state and ownership.
type EditorOperation string

const (
	EditorRead   EditorOperation = "read"
	EditorMemory EditorOperation = "memory"
	EditorAnswer EditorOperation = "answer"
	EditorFact   EditorOperation = "fact"
	EditorEdit   EditorOperation = "edit"
	EditorVerify EditorOperation = "verify"
	EditorFinish EditorOperation = "finish"
)

// AuthorizeEditorOperation is the shared MCP/native boundary for an editor
// action. OpenCode may propose an operation, but this function decides whether
// the current task, session, phase, state, and worktree permit it. Every caller
// that can observe or mutate a supervised task goes through here before doing
// work.
func AuthorizeEditorOperation(ctx context.Context, st *store.Store, repoRoot, taskID, sessionID string, op EditorOperation) (task.Task, *worktree.Worktree, func(), error) {
	bound, err := TaskBoundToSession(ctx, st, taskID, sessionID)
	if err != nil {
		return task.Task{}, nil, nil, fmt.Errorf("checking task/session binding: %w", err)
	}
	if !bound {
		return task.Task{}, nil, nil, fmt.Errorf("task %q is not bound to this OpenCode session; call bc_task_start or bc_task_resume first", taskID)
	}
	t, err := task.NewStore(st).Get(ctx, taskID)
	if err != nil {
		return task.Task{}, nil, nil, err
	}
	if t.Kind != "supervised" {
		return task.Task{}, nil, nil, fmt.Errorf("task %q is not a supervised task", taskID)
	}
	if t.State.Terminal() {
		return task.Task{}, nil, nil, fmt.Errorf("task %q is already %s", taskID, t.State)
	}
	if t.State == task.StateBlocked || t.State == task.StatePaused {
		return task.Task{}, nil, nil, fmt.Errorf("task %q is %s; the supervisor is not accepting worker operations", taskID, t.State)
	}
	phase, err := CurrentPhase(ctx, st, taskID)
	if err != nil {
		return task.Task{}, nil, nil, fmt.Errorf("reading task phase: %w", err)
	}
	if phase != "EDITOR" {
		return task.Task{}, nil, nil, fmt.Errorf("task %q is in phase %q; the editor worker is not authorized in this phase", taskID, phase)
	}
	if op == EditorEdit && t.State == task.StateReview {
		return task.Task{}, nil, nil, fmt.Errorf("task %q is in review; the supervisor must authorize a new attempt before editing", taskID)
	}
	if t.Budget.MaxWallTime > 0 && time.Since(t.CreatedAt) > t.Budget.MaxWallTime {
		return task.Task{}, nil, nil, fmt.Errorf("task %q exceeded its wall-clock budget", taskID)
	}
	wt, release, err := EnsureTaskWorktreeForSession(ctx, st, repoRoot, taskID, sessionID)
	if err != nil {
		return task.Task{}, nil, nil, err
	}
	return t, wt, release, nil
}

// EnsureTaskWorktreeForSession is the operation-level adapter used by MCP
// tools. It resolves the worktree, persists its identity on the task, and
// acquires the lease before the caller performs any task mutation.
func EnsureTaskWorktreeForSession(ctx context.Context, st *store.Store, repoRoot, taskID, sessionID string) (*worktree.Worktree, func(), error) {
	wt, err := EnsureTaskWorktree(ctx, st, repoRoot, taskID)
	if err != nil {
		return nil, nil, err
	}
	ts := task.NewStore(st)
	if err := ts.SetWorktree(ctx, taskID, wt.ID); err != nil {
		return nil, nil, err
	}
	release, err := AcquireTaskLease(ctx, st, taskID, sessionID)
	if err != nil {
		return nil, nil, err
	}
	return wt, release, nil
}
