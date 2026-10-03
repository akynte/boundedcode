// Package orchestrator runs tasks: it owns the control loop that connects the
// task ledger, git worktrees, context planning, the agent runtime,
// verification and frontier escalation. Every transition is persisted before
// it is acted on, so a run can be killed at any point and resumed.
package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/akynte/boundedcode/internal/agent"
	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/contextplan"
	"github.com/akynte/boundedcode/internal/frontier"
	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/policy"
	"github.com/akynte/boundedcode/internal/repointel"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/telemetry"
	"github.com/akynte/boundedcode/internal/verify"
	"github.com/akynte/boundedcode/internal/workspace"
)

// Runner executes tasks.
type Runner struct {
	DB       *sql.DB
	Ledger   task.Ledger
	WS       workspace.Store
	Rec      *telemetry.Recorder
	Agent    agent.Runtime
	Verify   *verify.Engine
	Intel    repointel.Intelligence // optional
	Frontier frontier.Provider      // nil: frontier disabled
	// Approve is asked before any packet leaves the machine. nil denies.
	Approve func(ctx context.Context, tr frontier.Trigger, packetPath string, tokens int) bool
	// NewGateway returns the metering LLM gateway for a task.
	NewGateway func(taskID string, maxTokens int) *inference.Gateway
	Cfg        config.Config
	Paths      config.Paths
	Model      string
	CtxSize    int
	Log        *slog.Logger
	Out        io.Writer
	// CondenseEachRetry forces a context condensation before every retry
	// (used by continuity tests and benchmarks).
	CondenseEachRetry bool
}

// RunOptions modify one run.
type RunOptions struct {
	UserRequestedFrontier bool
}

func (r *Runner) say(format string, args ...any) {
	if r.Out != nil {
		fmt.Fprintf(r.Out, "%s "+format+"\n", append([]any{time.Now().Format("15:04:05")}, args...)...)
	}
}

// WorkDir is the agent-visible root for a task.
func (r *Runner) WorkDir(taskID string) string { return filepath.Join(r.Paths.TaskDir(taskID), "work") }

// Create registers a task and its worktrees. repos selects repository names
// (empty = every repository in the workspace).
func (r *Runner) Create(ctx context.Context, w workspace.Workspace, request string, repos, criteria []string) (*task.Task, error) {
	all, err := r.WS.Repos(ctx, w.ID)
	if err != nil {
		return nil, err
	}
	var chosen []workspace.Repository
	for _, repo := range all {
		if len(repos) == 0 || contains(repos, repo.Name) {
			chosen = append(chosen, repo)
		}
	}
	if len(chosen) == 0 {
		return nil, fmt.Errorf("no repositories selected in workspace %s", w.Name)
	}
	b := r.Cfg.Budgets
	t := &task.Task{WorkspaceID: w.ID, OriginalRequest: request, AcceptanceCriteria: criteria,
		RemainingSteps: []string{"implement", "verify (targeted)", "verify (full gate)"},
		AgentRuntime:   r.Agent.Name(), ModelProfile: r.Model,
		Budget: task.Budget{MaxAttempts: b.MaxAttempts, MaxWallClockS: b.MaxWallClock.D().Seconds(),
			MaxLocalTokens: b.MaxLocalTokens, MaxEscalations: b.MaxEscalations}}
	if err := r.Ledger.Create(ctx, t); err != nil {
		return nil, err
	}
	for _, repo := range chosen {
		info, err := gitops.Inspect(ctx, repo.Path)
		if err != nil {
			return nil, err
		}
		wt := task.Worktree{TaskID: t.ID, RepositoryID: repo.ID, Path: filepath.Join(r.WorkDir(t.ID), repo.Name),
			Branch: gitops.TaskBranch(t.ID), BaseCommit: info.Head}
		if err := r.Ledger.AddWorktree(ctx, wt); err != nil { // persist intent first
			return nil, err
		}
		if err := gitops.EnsureWorktree(ctx, repo.Path, wt.Path, wt.Branch, wt.BaseCommit); err != nil {
			return nil, err
		}
	}
	r.Rec.Emit(ctx, t.ID, "task.created", map[string]any{"workspace": w.Name, "repos": len(chosen), "request_chars": len(request)})
	return t, nil
}

// Run drives a task until it completes, blocks, or ctx is cancelled.
func (r *Runner) Run(ctx context.Context, taskID string, opt RunOptions) (*task.Task, error) {
	t, err := r.Ledger.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	switch t.Status {
	case task.StatusCompleted, task.StatusCancelled, task.StatusFailed:
		return t, fmt.Errorf("task %s is %s", t.ID, t.Status)
	case task.StatusBlocked:
		t.Status = task.StatusActive // explicit resume re-arms a blocked task
		t.Decide("user", "resumed after block")
	}
	wts, err := r.Ledger.Worktrees(ctx, t.ID)
	if err != nil {
		return t, err
	}
	var gitDirs []string
	for _, w := range wts {
		// Recreate missing worktrees from their branch: git is the persistence layer.
		if err := gitops.EnsureWorktree(ctx, w.RepoPath, w.Path, w.Branch, w.BaseCommit); err != nil {
			return t, err
		}
		if d, err := gitops.CommonDir(ctx, w.Path); err == nil {
			gitDirs = append(gitDirs, d)
		}
	}
	runStart := time.Now()
	priorTokens := t.Budget.UsedLocalTokens
	gw := r.NewGateway(t.ID, max(t.Budget.MaxLocalTokens-priorTokens, 1))

	// Events from the runtime are audited; condensations are counted.
	var evMu sync.Mutex
	condensations := 0
	onEvent := func(e agent.Event) {
		if e.Kind == "Condensation" {
			evMu.Lock()
			condensations++
			evMu.Unlock()
		}
		data := map[string]any{"kind": e.Kind}
		if e.Tool != "" {
			data["tool"] = e.Tool
		}
		if e.Error != "" {
			data["error"] = trunc(e.Error, 400)
		}
		if e.Kind == "MessageEvent" || e.Kind == "Condensation" {
			data["text"] = trunc(e.Text, 400)
		}
		r.Rec.Emit(context.WithoutCancel(ctx), t.ID, "agent.event", data)
	}
	masks := secretMasks(r.WorkDir(t.ID), wts)
	openReq := agent.OpenRequest{TaskID: t.ID, SessionID: t.AgentSessionID, Workspace: r.WorkDir(t.ID), GitCommonDirs: gitDirs,
		PersistenceDir: filepath.Join(r.Paths.TaskDir(t.ID), "runtime"), MaxIterations: r.Cfg.Agent.MaxIterations,
		MaxInputTokens: r.CtxSize, MaxOutputTokens: 8192, CondenserMaxEvents: r.Cfg.Agent.CondenserMaxEvents,
		CondenserMaxTokens: r.CtxSize * 7 / 10, Masks: masks, OnEvent: onEvent}
	sess, mode, err := r.openSession(ctx, t, openReq)
	if err != nil {
		return t, err
	}
	defer sess.Close()

	consecutive := r.recentFailures(ctx, t.ID)
	lastSig := ""
	advice := r.pendingAdvice(ctx, t.ID)
	userFrontier := opt.UserRequestedFrontier
	reviewedZ1 := r.hasEscalation(ctx, t.ID, frontier.Z1)
	reviewedZ3 := r.hasEscalation(ctx, t.ID, frontier.Z3)

	save := func() error {
		t.Budget.UsedLocalTokens = priorTokens + gw.Used()
		t.Budget.UsedWallClockS += time.Since(runStart).Seconds()
		runStart = time.Now()
		evMu.Lock()
		t.Budget.Condensations += condensations
		condensations = 0
		evMu.Unlock()
		return r.Ledger.Save(context.WithoutCancel(ctx), t)
	}

	// Z1 (and Z4) are evaluated once before implementation starts.
	if t.AttemptCount == 0 {
		trs := frontier.Evaluate(r.Cfg.Escalation, frontier.Signals{Text: t.OriginalRequest + "\n" + t.Goal, ChangedRepos: len(wts),
			UserRequested: userFrontier, EscalationsUsed: t.Budget.UsedEscalations, MaxEscalations: t.Budget.MaxEscalations})
		for _, tr := range trs {
			if tr.Code == frontier.Z1 || tr.Code == frontier.Z4 {
				if a := r.escalate(ctx, t, wts, tr, mode); a != "" {
					advice = a
				}
				userFrontier = false
				reviewedZ1 = true
				break
			}
		}
	}

	for {
		if err := ctx.Err(); err != nil {
			_ = save()
			return t, err
		}
		if reason := r.budgetExceeded(t, gw); reason != "" {
			return t, r.block(t, save, reason)
		}
		t.AttemptCount++
		t.Phase = task.PhaseImplementing
		if err := save(); err != nil {
			return t, err
		}
		stratID, _ := r.Ledger.AddStrategy(ctx, t.ID, t.AttemptCount, "(in progress)")
		results := r.latestVerification(ctx, t.ID, wts)
		strategies, _ := r.Ledger.Strategies(ctx, t.ID)
		pack, err := contextplan.Build(ctx, contextplan.Inputs{Task: t, Worktrees: wts, WorkDir: r.WorkDir(t.ID),
			Strategies: strategies, Verification: results, Intel: r.Intel, Mode: mode,
			BudgetTokens: r.Cfg.Budgets.ContextPackTokens, MaxAttempts: t.Budget.MaxAttempts, Advice: advice})
		if err != nil {
			return t, err
		}
		r.Rec.Emit(ctx, t.ID, "context.pack", map[string]any{"mode": pack.Mode, "tokens": pack.Tokens, "sections": pack.Sections, "dropped": pack.Dropped})
		r.say("attempt %d/%d: sending %s context pack (%d tokens)", t.AttemptCount, t.Budget.MaxAttempts, mode, pack.Tokens)
		res, err := sess.Send(ctx, pack.Render())
		if err != nil {
			if ctx.Err() != nil {
				_ = save()
				return t, ctx.Err()
			}
			// Runtime failure (adapter crash, model server down): record, reopen
			// the session from persistence and continue with a resume pack.
			r.Rec.Emit(ctx, t.ID, "agent.error", map[string]any{"error": trunc(err.Error(), 600)})
			r.say("agent runtime error: %v; reopening session", trunc(err.Error(), 200))
			_ = r.Ledger.ResolveStrategy(ctx, stratID, "rejected", "runtime error: "+trunc(err.Error(), 200))
			sess.Close()
			if sess, mode, err = r.openSession(ctx, t, openReq); err != nil {
				return t, err
			}
			if err := save(); err != nil {
				return t, err
			}
			continue
		}
		r.Rec.Emit(ctx, t.ID, "agent.turn", map[string]any{"status": res.Status, "events_new": res.EventsNew, "stuck": res.Stuck,
			"final": trunc(res.FinalMessage, 600), "error": res.Error})
		r.say("agent stopped: status=%s events=%d stuck=%v", res.Status, res.EventsNew, res.Stuck)
		if errors.Is(gwErr(gw, t), inference.ErrBudgetExhausted) {
			_ = r.Ledger.ResolveStrategy(ctx, stratID, "rejected", "token budget exhausted")
			return t, r.block(t, save, "local token budget exhausted")
		}

		// Git checkpoint: persist the attempt's work on the task branch.
		changedAny := false
		var changedFiles, changedRepos []string
		for _, w := range wts {
			if _, err := gitops.CommitAll(ctx, w.Path, fmt.Sprintf("boundedcode %s: attempt %d", t.ID, t.AttemptCount)); err != nil {
				r.Log.Warn("checkpoint commit failed", "repo", w.RepoName, "err", err)
			}
			files, _ := gitops.ChangedFiles(ctx, w.Path, w.BaseCommit)
			if len(files) > 0 {
				changedAny = true
				changedRepos = append(changedRepos, w.RepoName)
				for _, f := range files {
					changedFiles = append(changedFiles, w.RepoName+"/"+f)
				}
			}
		}
		t.ChangedFiles, t.ChangedRepositories = changedFiles, changedRepos
		t.Phase = task.PhaseVerifying
		if err := save(); err != nil {
			return t, err
		}

		passed, sig, failSummary := false, "", ""
		if !changedAny {
			sig, failSummary = "no-changes", "the agent made no changes"
		} else {
			passed, sig, failSummary = r.verifyAll(ctx, t, wts, verify.Targeted)
			if passed {
				t.VerificationState = "targeted_pass"
				t.MarkStep("verify (targeted)")
				passed, sig, failSummary = r.verifyAll(ctx, t, wts, verify.Full)
			}
		}
		t.Budget.VerificationRuns++
		summary := trunc(res.FinalMessage, 500)
		if summary == "" {
			summary = fmt.Sprintf("attempt %d (status %s, %d events)", t.AttemptCount, res.Status, res.EventsNew)
		}
		if passed {
			// Z3: high-risk changes get one frontier review before merge.
			if !reviewedZ3 {
				trs := frontier.Evaluate(r.Cfg.Escalation, frontier.Signals{Text: t.OriginalRequest, ChangedFiles: changedFiles,
					ChangedRepos: len(changedRepos), PreMerge: true, EscalationsUsed: t.Budget.UsedEscalations,
					MaxEscalations: t.Budget.MaxEscalations, AlreadyReviewedZ1: true, UserRequested: userFrontier})
				if tr, ok := find(trs, frontier.Z3, frontier.Z4); ok {
					reviewedZ3, userFrontier = true, false
					if a := r.escalate(ctx, t, wts, tr, contextplan.ModeRetry); a != "" {
						advice = "Pre-merge review from the frontier model. Apply findings that are real defects; ignore ones that are not. If nothing needs fixing, finish without changes.\n\n" + a
						_ = r.Ledger.ResolveStrategy(ctx, stratID, "succeeded", "verification passed; frontier review requested")
						mode = contextplan.ModeRetry
						continue
					}
				}
			}
			_ = r.Ledger.ResolveStrategy(ctx, stratID, "succeeded", "verification passed (targeted + full)")
			t.VerificationState = "full_pass"
			t.MarkStep("implement")
			t.MarkStep("verify (full gate)")
			t.Status, t.Phase, t.FinishedAt = task.StatusCompleted, task.PhaseReview, store.Now()
			t.Decide("policy", "merge candidate: all verification passed on branch "+gitops.TaskBranch(t.ID))
			if err := save(); err != nil {
				return t, err
			}
			r.finishEscalations(ctx, t.ID, "helped")
			r.Rec.Emit(ctx, t.ID, "task.completed", map[string]any{"attempts": t.AttemptCount, "tokens": t.Budget.UsedLocalTokens,
				"escalations": t.Budget.UsedEscalations, "condensations": t.Budget.Condensations})
			r.say("task %s is a verified merge candidate on %s", t.ID, gitops.TaskBranch(t.ID))
			return t, nil
		}

		// Failure path.
		consecutive++
		t.Budget.FailedVerifyRuns++
		t.VerificationState = "failing"
		_ = r.Ledger.ResolveStrategy(ctx, stratID, "rejected", failSummary)
		_ = r.updateStrategySummary(ctx, stratID, summary)
		repeated := sig != "" && sig == lastSig
		lastSig = sig
		r.say("verification failed: %s", trunc(failSummary, 200))
		if err := save(); err != nil {
			return t, err
		}
		strategies, _ = r.Ledger.Strategies(ctx, t.ID)
		trs := frontier.Evaluate(r.Cfg.Escalation, frontier.Signals{Text: t.OriginalRequest, ChangedFiles: changedFiles,
			ChangedRepos: len(changedRepos), ConsecutiveFailures: consecutive, RejectedStrategies: countRejected(strategies),
			RepeatedFailure: repeated, Stuck: res.Stuck || res.Status == "stuck", EscalationsUsed: t.Budget.UsedEscalations,
			MaxEscalations: t.Budget.MaxEscalations, AlreadyReviewedZ1: reviewedZ1, UserRequested: userFrontier})
		advice = ""
		if tr, ok := find(trs, frontier.Z2, frontier.Z4); ok {
			userFrontier = false
			advice = r.escalate(ctx, t, wts, tr, contextplan.ModeRetry)
		}
		mode = contextplan.ModeRetry
		if r.CondenseEachRetry {
			if err := sess.Condense(ctx); err != nil {
				r.Log.Warn("forced condensation failed", "err", err)
			} else {
				r.Rec.Emit(ctx, t.ID, "context.condensed", map[string]any{"forced": true})
			}
		}
	}
}

func gwErr(gw *inference.Gateway, t *task.Task) error {
	if t.Budget.MaxLocalTokens > 0 && gw.MaxTokens > 0 && gw.Used() >= gw.MaxTokens {
		return inference.ErrBudgetExhausted
	}
	return nil
}

func (r *Runner) openSession(ctx context.Context, t *task.Task, req agent.OpenRequest) (agent.Session, contextplan.Mode, error) {
	mode := contextplan.ModeInitial
	if t.AttemptCount > 0 {
		mode = contextplan.ModeResume
	}
	sess, err := r.Agent.Open(ctx, req)
	if err != nil && req.SessionID != "" {
		// The runtime could not restore the conversation: start a fresh one.
		// The task is the source of truth, so a resume pack rebuilds context.
		r.Rec.Emit(ctx, t.ID, "agent.resume_failed", map[string]any{"session": req.SessionID, "error": trunc(err.Error(), 400)})
		req.SessionID = ""
		t.Budget.ContextResets++
		sess, err = r.Agent.Open(ctx, req)
		mode = contextplan.ModeResume
	}
	if err != nil {
		return nil, mode, fmt.Errorf("open agent session: %w", err)
	}
	if sess.Resumed() {
		t.Budget.SessionsResumed++
		mode = contextplan.ModeResume
		r.say("resumed agent session %s", sess.ID())
	}
	t.AgentSessionID = sess.ID()
	r.Rec.Emit(ctx, t.ID, "agent.session", map[string]any{"session": sess.ID(), "resumed": sess.Resumed(), "mode": mode})
	return sess, mode, r.Ledger.Save(ctx, t)
}

func (r *Runner) budgetExceeded(t *task.Task, gw *inference.Gateway) string {
	b := t.Budget
	switch {
	case b.MaxAttempts > 0 && t.AttemptCount >= b.MaxAttempts:
		return fmt.Sprintf("attempt budget exhausted (%d)", b.MaxAttempts)
	case b.MaxWallClockS > 0 && b.UsedWallClockS >= b.MaxWallClockS:
		return "wall-clock budget exhausted"
	case b.MaxLocalTokens > 0 && b.UsedLocalTokens+gw.Used() >= b.MaxLocalTokens:
		return "local token budget exhausted"
	}
	return ""
}

func (r *Runner) block(t *task.Task, save func() error, reason string) error {
	t.Status, t.Phase = task.StatusBlocked, task.PhaseImplementing
	t.Decide("policy", "blocked: "+reason)
	if err := save(); err != nil {
		return err
	}
	// Blocked is not terminal: escalation outcomes are decided when the task
	// completes ("helped") or is abandoned ("no_effect", see task cancel).
	r.Rec.Emit(context.Background(), t.ID, "task.blocked", map[string]any{"reason": reason})
	r.say("task %s blocked: %s (work preserved on %s; `task resume` to continue)", t.ID, reason, gitops.TaskBranch(t.ID))
	return nil
}

// verifyAll verifies every repository with changes (all repos in full scope).
func (r *Runner) verifyAll(ctx context.Context, t *task.Task, wts []task.Worktree, scope verify.Scope) (bool, string, string) {
	passed := true
	var sigs, summary []string
	for _, w := range wts {
		files, _ := gitops.ChangedFiles(ctx, w.Path, w.BaseCommit)
		if len(files) == 0 && scope == verify.Targeted {
			continue
		}
		res, err := r.Verify.Run(ctx, verify.RepoTarget{Name: w.RepoName, Worktree: w.Path, Base: w.BaseCommit, TaskID: t.ID}, scope)
		if err != nil {
			return false, "verify-error", err.Error()
		}
		r.say("verify %s %s: passed=%v (%s)", scope, w.RepoName, res.Passed, res.Duration.Round(time.Millisecond))
		if !res.Passed {
			passed = false
			sigs = append(sigs, w.RepoName+":"+res.Signature())
			for _, f := range res.Failures() {
				summary = append(summary, fmt.Sprintf("%s/%s: %s", w.RepoName, f.Name, firstLine(f.Output)))
			}
		}
	}
	return passed, strings.Join(sigs, ";"), strings.Join(summary, "; ")
}

// latestVerification returns the most recent result per repository.
func (r *Runner) latestVerification(ctx context.Context, taskID string, wts []task.Worktree) []verify.Result {
	runs, err := verify.LoadRuns(ctx, r.DB, taskID, 4*len(wts)+4)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []verify.Result
	for _, run := range runs {
		if seen[run.Repository] {
			continue
		}
		seen[run.Repository] = true
		out = append(out, run)
	}
	return out
}

// recentFailures counts trailing failed attempts (survives restarts).
func (r *Runner) recentFailures(ctx context.Context, taskID string) int {
	st, err := r.Ledger.Strategies(ctx, taskID)
	if err != nil {
		return 0
	}
	n := 0
	for i := len(st) - 1; i >= 0 && st[i].Outcome == "rejected"; i-- {
		n++
	}
	return n
}

func (r *Runner) updateStrategySummary(ctx context.Context, id int64, summary string) error {
	_, err := r.DB.ExecContext(ctx, `UPDATE strategies SET summary = ? WHERE id = ?`, summary, id)
	return err
}

func secretMasks(workDir string, wts []task.Worktree) []string {
	var out []string
	for _, w := range wts {
		found, _ := policy.FindSecretPaths(w.Path, 200)
		rel, _ := filepath.Rel(workDir, w.Path)
		for _, f := range found {
			out = append(out, filepath.Join(rel, f))
		}
	}
	return out
}

func countRejected(st []task.Strategy) int {
	n := 0
	for _, s := range st {
		if s.Outcome == "rejected" {
			n++
		}
	}
	return n
}

func find(trs []frontier.Trigger, codes ...frontier.Code) (frontier.Trigger, bool) {
	for _, c := range codes {
		for _, t := range trs {
			if t.Code == c {
				return t, true
			}
		}
	}
	return frontier.Trigger{}, false
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func firstLine(s string) string {
	for l := range strings.SplitSeq(s, "\n") {
		l = strings.TrimSpace(strings.TrimPrefix(l, "…"))
		if l != "" {
			return trunc(l, 160)
		}
	}
	return ""
}
