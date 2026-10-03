// Package task is the canonical task ledger. The task, not the model's
// conversation, is the source of truth: everything needed to rebuild working
// context after a crash, restart or condensation is recorded here, in Git,
// or referenced (runtime session ids), never duplicated.
package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/akynte/boundedcode/internal/ids"
	"github.com/akynte/boundedcode/internal/store"
)

// Status is the coarse lifecycle state.
type Status string

// Task statuses.
const (
	StatusActive    Status = "active"
	StatusBlocked   Status = "blocked"   // budget exhausted or needs a human; work preserved
	StatusCompleted Status = "completed" // verified merge candidate
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Phase is the step within the lifecycle.
type Phase string

// Task phases.
const (
	PhaseSetup        Phase = "setup"
	PhaseImplementing Phase = "implementing"
	PhaseVerifying    Phase = "verifying"
	PhaseEscalating   Phase = "escalating"
	PhaseReview       Phase = "review" // merge candidate awaiting a human
	PhaseDone         Phase = "done"
)

// Task is the canonical record.
type Task struct {
	ID                  string     `json:"id"`
	WorkspaceID         string     `json:"workspace_id"`
	OriginalRequest     string     `json:"original_request"`
	Goal                string     `json:"goal"`
	AcceptanceCriteria  []string   `json:"acceptance_criteria"`
	Status              Status     `json:"status"`
	Phase               Phase      `json:"phase"`
	CompletedSteps      []string   `json:"completed_steps"`
	RemainingSteps      []string   `json:"remaining_steps"`
	AttemptCount        int        `json:"attempt_count"`
	Decisions           []Decision `json:"decisions"`
	VerificationState   string     `json:"verification_state"` // none | failing | targeted_pass | full_pass
	ChangedRepositories []string   `json:"changed_repositories"`
	ChangedFiles        []string   `json:"changed_files"`
	ChangedSymbols      []string   `json:"changed_symbols"`
	AgentRuntime        string     `json:"agent_runtime"`
	AgentSessionID      string     `json:"agent_session_id"`
	ModelProfile        string     `json:"model_profile"`
	Budget              Budget     `json:"budget"`
	CreatedAt           string     `json:"created_at"`
	UpdatedAt           string     `json:"updated_at"`
	FinishedAt          string     `json:"finished_at"`
}

// Decision is an important choice made during the task.
type Decision struct {
	At     string `json:"at"`
	Source string `json:"source"` // agent | user | frontier | policy
	Text   string `json:"text"`
}

// Budget is the per-task resource allowance and consumption.
type Budget struct {
	MaxAttempts      int     `json:"max_attempts"`
	MaxWallClockS    float64 `json:"max_wall_clock_s"`
	MaxLocalTokens   int     `json:"max_local_tokens"`
	MaxEscalations   int     `json:"max_escalations"`
	UsedWallClockS   float64 `json:"used_wall_clock_s"`
	UsedLocalTokens  int     `json:"used_local_tokens"`
	UsedEscalations  int     `json:"used_escalations"`
	ContextResets    int     `json:"context_resets"`
	Condensations    int     `json:"condensations"`
	SessionsResumed  int     `json:"sessions_resumed"`
	VerificationRuns int     `json:"verification_runs"`
	FailedVerifyRuns int     `json:"failed_verification_runs"`
}

// Worktree links a task to one repository checkout.
type Worktree struct {
	TaskID       string `json:"task_id"`
	RepositoryID string `json:"repository_id"`
	RepoName     string `json:"repo_name"`
	RepoPath     string `json:"repo_path"`
	IndexProject string `json:"index_project"`
	Path         string `json:"path"`
	Branch       string `json:"branch"`
	BaseCommit   string `json:"base_commit"`
}

// Strategy is one approach tried in an attempt.
type Strategy struct {
	ID        int64  `json:"id"`
	Attempt   int    `json:"attempt"`
	Summary   string `json:"summary"`
	Outcome   string `json:"outcome"` // active | succeeded | rejected
	Reason    string `json:"reason"`
	CreatedAt string `json:"created_at"`
}

// Ledger persists tasks.
type Ledger struct{ DB *sql.DB }

// Create inserts a new task.
func (l Ledger) Create(ctx context.Context, t *Task) error {
	if t.ID == "" {
		t.ID = ids.New("t")
	}
	if t.Status == "" {
		t.Status = StatusActive
	}
	if t.Phase == "" {
		t.Phase = PhaseSetup
	}
	if t.VerificationState == "" {
		t.VerificationState = "none"
	}
	if t.Goal == "" {
		t.Goal = t.OriginalRequest
	}
	t.CreatedAt = store.Now()
	t.UpdatedAt = t.CreatedAt
	_, err := l.DB.ExecContext(ctx, `INSERT INTO tasks(id, workspace_id, original_request, status, phase, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?)`, t.ID, t.WorkspaceID, t.OriginalRequest, t.Status, t.Phase, t.CreatedAt, t.UpdatedAt)
	if err != nil {
		return err
	}
	return l.Save(ctx, t)
}

// Save persists every mutable field. Callers save before and after each
// transition (persist-then-act).
func (l Ledger) Save(ctx context.Context, t *Task) error {
	t.UpdatedAt = store.Now()
	j := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	res, err := l.DB.ExecContext(ctx, `UPDATE tasks SET goal=?, acceptance_criteria=?, status=?, phase=?, completed_steps=?,
		remaining_steps=?, attempt_count=?, decisions=?, verification_state=?, changed_repositories=?, changed_files=?,
		changed_symbols=?, agent_runtime=?, agent_session_id=?, model_profile=?, budget=?, updated_at=?, finished_at=?
		WHERE id=?`,
		t.Goal, j(nonNil(t.AcceptanceCriteria)), t.Status, t.Phase, j(nonNil(t.CompletedSteps)), j(nonNil(t.RemainingSteps)),
		t.AttemptCount, j(nonNilD(t.Decisions)), t.VerificationState, j(nonNil(t.ChangedRepositories)), j(nonNil(t.ChangedFiles)),
		j(nonNil(t.ChangedSymbols)), t.AgentRuntime, t.AgentSessionID, t.ModelProfile, j(t.Budget), t.UpdatedAt, t.FinishedAt, t.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("task %s: %w", t.ID, store.ErrNotFound)
	}
	return nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilD(s []Decision) []Decision {
	if s == nil {
		return []Decision{}
	}
	return s
}

const taskCols = `id, workspace_id, original_request, goal, acceptance_criteria, status, phase, completed_steps, remaining_steps,
	attempt_count, decisions, verification_state, changed_repositories, changed_files, changed_symbols, agent_runtime,
	agent_session_id, model_profile, budget, created_at, updated_at, finished_at`

func scanTask(sc interface{ Scan(...any) error }) (*Task, error) {
	var t Task
	var ac, cs, rs, dec, cr, cf, csym, bud string
	err := sc.Scan(&t.ID, &t.WorkspaceID, &t.OriginalRequest, &t.Goal, &ac, &t.Status, &t.Phase, &cs, &rs, &t.AttemptCount,
		&dec, &t.VerificationState, &cr, &cf, &csym, &t.AgentRuntime, &t.AgentSessionID, &t.ModelProfile, &bud,
		&t.CreatedAt, &t.UpdatedAt, &t.FinishedAt)
	if err != nil {
		return nil, err
	}
	for _, x := range []struct {
		s string
		v any
	}{{ac, &t.AcceptanceCriteria}, {cs, &t.CompletedSteps}, {rs, &t.RemainingSteps}, {dec, &t.Decisions},
		{cr, &t.ChangedRepositories}, {cf, &t.ChangedFiles}, {csym, &t.ChangedSymbols}, {bud, &t.Budget}} {
		if err := json.Unmarshal([]byte(x.s), x.v); err != nil {
			return nil, fmt.Errorf("task %s: corrupt column: %w", t.ID, err)
		}
	}
	return &t, nil
}

// Get loads a task by id or unique id prefix.
func (l Ledger) Get(ctx context.Context, id string) (*Task, error) {
	rows, err := l.DB.QueryContext(ctx, `SELECT `+taskCols+` FROM tasks WHERE id = ? OR id LIKE ? ORDER BY id LIMIT 2`, id, id+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	switch {
	case len(out) == 0:
		return nil, fmt.Errorf("task %q: %w", id, store.ErrNotFound)
	case len(out) > 1 && out[0].ID != id:
		return nil, fmt.Errorf("task id prefix %q is ambiguous", id)
	}
	return out[0], rows.Err()
}

// List returns recent tasks, newest first.
func (l Ledger) List(ctx context.Context, workspaceID string, limit int) ([]*Task, error) {
	q := `SELECT ` + taskCols + ` FROM tasks`
	var args []any
	if workspaceID != "" {
		q += ` WHERE workspace_id = ?`
		args = append(args, workspaceID)
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, max(limit, 1))
	rows, err := l.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// AddWorktree records a task worktree (idempotent).
func (l Ledger) AddWorktree(ctx context.Context, w Worktree) error {
	_, err := l.DB.ExecContext(ctx, `INSERT INTO task_worktrees(task_id, repository_id, path, branch, base_commit) VALUES(?,?,?,?,?)
		ON CONFLICT(task_id, repository_id) DO NOTHING`, w.TaskID, w.RepositoryID, w.Path, w.Branch, w.BaseCommit)
	return err
}

// Worktrees lists a task's worktrees joined with repository metadata.
func (l Ledger) Worktrees(ctx context.Context, taskID string) ([]Worktree, error) {
	rows, err := l.DB.QueryContext(ctx, `SELECT w.task_id, w.repository_id, r.name, r.path, r.index_project, w.path, w.branch, w.base_commit
		FROM task_worktrees w JOIN repositories r ON r.id = w.repository_id WHERE w.task_id = ? ORDER BY r.name`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Worktree
	for rows.Next() {
		var w Worktree
		if err := rows.Scan(&w.TaskID, &w.RepositoryID, &w.RepoName, &w.RepoPath, &w.IndexProject, &w.Path, &w.Branch, &w.BaseCommit); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// AddStrategy records a new active strategy for an attempt.
func (l Ledger) AddStrategy(ctx context.Context, taskID string, attempt int, summary string) (int64, error) {
	res, err := l.DB.ExecContext(ctx, `INSERT INTO strategies(task_id, attempt, summary, outcome, created_at) VALUES(?,?,?,?,?)`,
		taskID, attempt, summary, "active", store.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ResolveStrategy marks a strategy succeeded or rejected.
func (l Ledger) ResolveStrategy(ctx context.Context, id int64, outcome, reason string) error {
	if outcome != "succeeded" && outcome != "rejected" {
		return errors.New("outcome must be succeeded or rejected")
	}
	_, err := l.DB.ExecContext(ctx, `UPDATE strategies SET outcome = ?, reason = ? WHERE id = ?`, outcome, reason, id)
	return err
}

// Strategies lists a task's strategies in order.
func (l Ledger) Strategies(ctx context.Context, taskID string) ([]Strategy, error) {
	rows, err := l.DB.QueryContext(ctx, `SELECT id, attempt, summary, outcome, reason, created_at FROM strategies WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Strategy
	for rows.Next() {
		var s Strategy
		if err := rows.Scan(&s.ID, &s.Attempt, &s.Summary, &s.Outcome, &s.Reason, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Decide appends a decision.
func (t *Task) Decide(source, text string) {
	t.Decisions = append(t.Decisions, Decision{At: time.Now().UTC().Format(time.RFC3339), Source: source, Text: text})
}

// MarkStep moves a step from remaining to completed (adds it if unknown).
func (t *Task) MarkStep(step string) {
	for i, s := range t.RemainingSteps {
		if s == step {
			t.RemainingSteps = append(t.RemainingSteps[:i], t.RemainingSteps[i+1:]...)
			break
		}
	}
	t.CompletedSteps = append(t.CompletedSteps, step)
}
