package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/orchestrator"
	"github.com/akynte/boundedcode/internal/repointel/cbm"
	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/telemetry"
	"github.com/akynte/boundedcode/internal/verify"
)

// Task operations shared by the CLI commands and the terminal UI.

// taskDetail is a task with its strategies, worktrees and run lease.
type taskDetail struct {
	Task       *task.Task      `json:"task"`
	Strategies []task.Strategy `json:"strategies"`
	Worktrees  []task.Worktree `json:"worktrees"`
	Lease      task.Lease      `json:"lease"`
}

func (a *App) taskDetail(ctx context.Context, id string) (taskDetail, error) {
	s, err := a.Store(ctx)
	if err != nil {
		return taskDetail{}, err
	}
	l := task.Ledger{DB: s.DB}
	t, err := l.Get(ctx, id)
	if err != nil {
		return taskDetail{}, err
	}
	d := taskDetail{Task: t}
	d.Strategies, _ = l.Strategies(ctx, t.ID)
	d.Worktrees, _ = l.Worktrees(ctx, t.ID)
	d.Lease, _ = l.LeaseOf(ctx, t.ID)
	return d, nil
}

// cancelTask marks a task cancelled; a run in progress notices within a
// lease heartbeat.
func (a *App) cancelTask(ctx context.Context, id string) error {
	s, err := a.Store(ctx)
	if err != nil {
		return err
	}
	l := task.Ledger{DB: s.DB}
	t, err := l.Get(ctx, id)
	if err != nil {
		return err
	}
	t.Status, t.FinishedAt = task.StatusCancelled, store.Now()
	t.Decide("user", "cancelled")
	if err := l.Save(ctx, t); err != nil {
		return err
	}
	orchestrator.FinishEscalations(ctx, s.DB, t.ID, "no_effect")
	telemetry.New(s.DB, a.Log).Emit(ctx, t.ID, "task.cancelled", map[string]any{"phase": t.Phase, "attempts": t.AttemptCount})
	if ls, _ := l.LeaseOf(ctx, t.ID); ls.Owner != "" {
		a.printf("a run of this task is in progress (%s); it stops within ~15s\n", ls.Owner)
	}
	a.printf("cancelled %s; branch %s kept (`task cleanup %s` removes the worktrees)\n", t.ID, gitops.TaskBranch(t.ID), t.ID)
	return nil
}

// cleanupTask removes a finished task's worktrees, code-graph projects and
// caches. The ledger, audit log and frontier packets are kept.
func (a *App) cleanupTask(ctx context.Context, id string, deleteBranch bool) error {
	s, err := a.Store(ctx)
	if err != nil {
		return err
	}
	l := task.Ledger{DB: s.DB}
	t, err := l.Get(ctx, id)
	if err != nil {
		return err
	}
	switch t.Status {
	case task.StatusCompleted, task.StatusCancelled, task.StatusFailed:
	default:
		return fmt.Errorf("task %s is %s; cancel it first (cleanup only removes finished tasks)", t.ID, t.Status)
	}
	if ls, _ := l.LeaseOf(ctx, t.ID); ls.Owner != "" {
		return fmt.Errorf("task %s is still being run by %s", t.ID, ls.Owner)
	}
	wts, err := l.Worktrees(ctx, t.ID)
	if err != nil {
		return err
	}
	intel := &cbm.Client{Binary: a.Config.RepoIntel.Binary, CacheDir: filepath.Join(a.Paths.Cache, "codebase-memory")}
	for _, w := range wts {
		if _, err := os.Stat(w.Path); err == nil {
			if err := gitops.RemoveWorktree(ctx, w.RepoPath, w.Path); err != nil {
				return fmt.Errorf("%s: %w", w.RepoName, err)
			}
		}
		_ = gitops.PruneWorktrees(ctx, w.RepoPath)
		if deleteBranch {
			if err := gitops.DeleteTaskBranch(ctx, w.RepoPath, w.Branch); err != nil {
				return fmt.Errorf("%s: %w", w.RepoName, err)
			}
		}
		// The worktree's code-graph project, if one was built.
		_, _ = intel.Call(ctx, "delete_project", map[string]any{"project": orchestrator.WorktreeProject(t.ID, w.RepoName)})
		a.printf("%s: removed worktree %s%s\n", w.RepoName, w.Path, map[bool]string{true: " and branch " + w.Branch}[deleteBranch])
	}
	_ = intel.Close()
	_ = os.RemoveAll(filepath.Join(a.Paths.Cache, "build", "gocache", t.ID))
	_ = os.RemoveAll(filepath.Join(a.Paths.TaskDir(t.ID), "work"))
	telemetry.New(s.DB, a.Log).Emit(ctx, t.ID, "task.cleaned", map[string]any{"delete_branch": deleteBranch, "worktrees": len(wts)})
	a.printf("ledger, audit log and frontier packets of %s are kept\n", t.ID)
	return nil
}

// repoDiff is one repository's changes against the task's base commit.
type repoDiff struct {
	Repo string `json:"repo"`
	Diff string `json:"diff"`
}

// taskDiffs returns the non-empty diffs of the task's worktrees.
func (a *App) taskDiffs(ctx context.Context, id string) ([]repoDiff, error) {
	s, err := a.Store(ctx)
	if err != nil {
		return nil, err
	}
	l := task.Ledger{DB: s.DB}
	t, err := l.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	wts, _ := l.Worktrees(ctx, t.ID)
	if err := checkWorktrees(ctx, wts); err != nil {
		return nil, err
	}
	var out []repoDiff
	for _, w := range wts {
		d, err := gitops.Diff(ctx, w.Path, w.BaseCommit, false)
		if err != nil {
			return nil, err
		}
		if d != "" {
			out = append(out, repoDiff{w.RepoName, d})
		}
	}
	return out, nil
}

// repoVerification is one repository's verification result.
type repoVerification struct {
	Repo   string        `json:"repo"`
	Scope  verify.Scope  `json:"scope"`
	Result verify.Result `json:"result"`
}

// verifyTask runs deterministic verification on every task worktree.
func (a *App) verifyTask(ctx context.Context, id string, full, unsafe bool) ([]repoVerification, error) {
	s, err := a.Store(ctx)
	if err != nil {
		return nil, err
	}
	l := task.Ledger{DB: s.DB}
	t, err := l.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	sb, err := a.sandbox(unsafe)
	if err != nil {
		return nil, err
	}
	gomodcache, _ := exec.CommandContext(ctx, "go", "env", "GOMODCACHE").Output()
	e := &verify.Engine{Sandbox: sb, CacheDir: filepath.Join(a.Paths.Cache, "build"), GoModCache: strings.TrimSpace(string(gomodcache)), Packages: sandbox.HostPackageCaches(),
		DB: s.DB, Rec: telemetry.New(s.DB, a.Log)}
	scope := verify.Targeted
	if full {
		scope = verify.Full
	}
	wts, _ := l.Worktrees(ctx, t.ID)
	if err := checkWorktrees(ctx, wts); err != nil {
		return nil, err
	}
	var out []repoVerification
	for _, w := range wts {
		res, err := e.Run(ctx, verify.RepoTarget{Name: w.RepoName, Worktree: w.Path, Base: w.BaseCommit, TaskID: t.ID, Source: w.RepoPath}, scope)
		if err != nil {
			return out, err
		}
		out = append(out, repoVerification{w.RepoName, scope, res})
	}
	return out, nil
}

// escalation is one frontier escalation record.
type escalation struct {
	ID                                              int64
	Task, Trigger, Provider, Model, Status, Outcome string
	TaskOutcome                                     string
	PacketTokens                                    int
	DiffBefore, DiffAfter, Created, Reason          string
	AdviceChangedCode                               *bool
}

// escalations lists escalations, of one task when taskID is set.
func (a *App) escalations(ctx context.Context, taskID string) ([]escalation, error) {
	s, err := a.Store(ctx)
	if err != nil {
		return nil, err
	}
	q := `SELECT id, task_id, trigger, provider, model, status, outcome, task_outcome, packet_tokens, diff_before, diff_after, created_at, reason
		FROM escalations`
	var qargs []any
	if taskID != "" {
		t, err := task.Ledger{DB: s.DB}.Get(ctx, taskID)
		if err != nil {
			return nil, err
		}
		q += ` WHERE task_id = ?`
		qargs = append(qargs, t.ID)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY id`, qargs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []escalation
	for rows.Next() {
		var e escalation
		if err := rows.Scan(&e.ID, &e.Task, &e.Trigger, &e.Provider, &e.Model, &e.Status, &e.Outcome, &e.TaskOutcome,
			&e.PacketTokens, &e.DiffBefore, &e.DiffAfter, &e.Created, &e.Reason); err != nil {
			return nil, err
		}
		if e.DiffBefore != "" && e.DiffAfter != "" {
			changed := e.DiffBefore != e.DiffAfter
			e.AdviceChangedCode = &changed
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// escalationGroup counts escalations by trigger, status and outcome.
type escalationGroup struct {
	Trigger, Status, Outcome string
	Count, PacketTokens      int
}

func (a *App) escalationSummary(ctx context.Context) ([]escalationGroup, error) {
	s, err := a.Store(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT trigger, status, outcome, COUNT(*), COALESCE(SUM(packet_tokens),0) FROM escalations GROUP BY 1,2,3 ORDER BY 1,2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []escalationGroup
	for rows.Next() {
		var g escalationGroup
		if err := rows.Scan(&g.Trigger, &g.Status, &g.Outcome, &g.Count, &g.PacketTokens); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// answerEscalation stores a manually obtained frontier answer read from file.
func (a *App) answerEscalation(ctx context.Context, taskID, file string) (int64, error) {
	s, err := a.Store(ctx)
	if err != nil {
		return 0, err
	}
	t, err := task.Ledger{DB: s.DB}.Get(ctx, taskID)
	if err != nil {
		return 0, err
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return 0, err
	}
	return orchestrator.AnswerEscalation(ctx, s.DB, a.Paths.TaskDir(t.ID), t.ID, b)
}

// applyTask brings a task's changes into the user's checkouts as a squash
// merge: the changes are staged for the user to review and commit, or
// committed when commit is set. Checkouts with uncommitted changes to
// tracked files are refused, so nothing of the user's is overwritten.
func (a *App) applyTask(ctx context.Context, id string, commit, force bool) error {
	s, err := a.Store(ctx)
	if err != nil {
		return err
	}
	l := task.Ledger{DB: s.DB}
	t, err := l.Get(ctx, id)
	if err != nil {
		return err
	}
	if ls, _ := l.LeaseOf(ctx, t.ID); ls.Owner != "" {
		return fmt.Errorf("task %s is still being run by %s", t.ID, ls.Owner)
	}
	if t.Status != task.StatusCompleted && !force {
		return fmt.Errorf("task %s is %s, not a verified merge candidate (pass --force to apply it anyway)", t.ID, t.Status)
	}
	wts, err := l.Worktrees(ctx, t.ID)
	if err != nil {
		return err
	}
	type target struct {
		w     task.Worktree
		files []string
	}
	var targets []target
	for _, w := range wts {
		files, err := gitops.Run(ctx, w.RepoPath, "diff", "--name-only", "--no-ext-diff", w.BaseCommit, "refs/heads/"+w.Branch)
		if err != nil {
			return fmt.Errorf("%s: %w", w.RepoName, err)
		}
		if strings.TrimSpace(files) == "" {
			continue
		}
		dirty, err := gitops.Run(ctx, w.RepoPath, "status", "--porcelain", "--untracked-files=no")
		if err != nil {
			return fmt.Errorf("%s: %w", w.RepoName, err)
		}
		if dirty != "" {
			return fmt.Errorf("%s (%s) has uncommitted changes; commit or stash them first", w.RepoName, w.RepoPath)
		}
		targets = append(targets, target{w, strings.Split(files, "\n")})
	}
	if len(targets) == 0 {
		a.printf("task %s has no changes to apply\n", t.ID)
		return nil
	}
	for _, tg := range targets {
		w := tg.w
		if _, err := gitops.Run(ctx, w.RepoPath, "merge", "--squash", "--no-edit", "refs/heads/"+w.Branch); err != nil {
			_, _ = gitops.Run(ctx, w.RepoPath, "reset", "--merge")
			return fmt.Errorf("%s: the changes do not apply cleanly to your current branch (your checkout is unchanged): %w", w.RepoName, err)
		}
		msg := strings.SplitN(strings.TrimSpace(t.OriginalRequest), "\n", 2)[0]
		if len(msg) > 72 {
			msg = msg[:72]
		}
		if commit {
			if _, err := gitops.Run(ctx, w.RepoPath, "commit", "--no-verify", "-q", "-m", msg, "-m", "Applied from BoundedCode task "+t.ID+" (branch "+w.Branch+")."); err != nil {
				if strings.Contains(err.Error(), "tell me who you are") {
					return fmt.Errorf("%s: the changes are staged but git has no identity to commit them; set user.name and user.email (git config --global ...) and commit", w.RepoName)
				}
				return fmt.Errorf("%s: commit: %w", w.RepoName, err)
			}
			a.printf("%s: committed %d file(s) from %s\n", w.RepoName, len(tg.files), w.Branch)
		} else {
			a.printf("%s: %d file(s) from %s staged in %s; review with `git diff --cached`, then commit\n", w.RepoName, len(tg.files), w.Branch, w.RepoPath)
		}
	}
	telemetry.New(s.DB, a.Log).Emit(ctx, t.ID, "task.applied", map[string]any{"repos": len(targets), "commit": commit, "status": t.Status})
	return nil
}
