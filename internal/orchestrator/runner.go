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
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/akynte/boundedcode/internal/agent"
	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/contextplan"
	"github.com/akynte/boundedcode/internal/frontier"
	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/hw"
	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/policy"
	"github.com/akynte/boundedcode/internal/repointel"
	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/telemetry"
	"github.com/akynte/boundedcode/internal/verify"
	"github.com/akynte/boundedcode/internal/workspace"
	"github.com/akynte/boundedcode/internal/xservice"
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
	Nav      repointel.Navigator    // optional: Serena symbol navigation per task worktree (ADR-0008)
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
	// ContractModel, when set, replaces the local-model call that derives
	// the task contract (tests).
	ContractModel func(ctx context.Context, request string) (string, error)
	// CondenseEachRetry forces a context condensation before every retry
	// (used by continuity tests and benchmarks).
	CondenseEachRetry bool
	// CrossService enables cross-service contract analysis (internal/xservice).
	CrossService bool
	// EnsureModel (re)starts the local model server after an infrastructure
	// failure; nil means the runner cannot repair it and blocks instead.
	EnsureModel func(ctx context.Context) error
	// LeaseOwner identifies this process in task leases (default host:pid).
	LeaseOwner string
}

// Lease timing: a runner renews its lease every leaseHeartbeat; a lease not
// renewed for leaseStale belongs to a dead process.
var (
	leaseHeartbeat = 15 * time.Second
	leaseStale     = 2 * time.Minute
)

// maxInfraRetries bounds model-server repairs per run.
const maxInfraRetries = 3

// RunOptions modify one run.
type RunOptions struct {
	UserRequestedFrontier bool
	// Clarification answers a task blocked as materially ambiguous.
	Clarification string
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
	for _, n := range repos {
		if !slices.ContainsFunc(all, func(r workspace.Repository) bool { return r.Name == n }) {
			return nil, fmt.Errorf("repository %q is not an enabled repository of workspace %s", n, w.Name)
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
	}
	// One live runner per task. A stale lease means the previous runner died
	// (SIGKILL, crash, reboot): reconcile what it left behind.
	owner := r.LeaseOwner
	if owner == "" {
		host, _ := os.Hostname()
		owner = fmt.Sprintf("%s:%d", host, os.Getpid())
	}
	prev, err := r.Ledger.AcquireLease(ctx, t.ID, owner, leaseStale)
	if err != nil {
		return t, err
	}
	defer func() { _ = r.Ledger.ReleaseLease(context.WithoutCancel(ctx), t.ID, owner) }()
	ctx, stopRun := context.WithCancelCause(ctx)
	defer stopRun(nil)
	go r.keepLease(ctx, t.ID, owner, stopRun)
	if n, _ := r.Ledger.RejectActiveStrategies(ctx, t.ID, "interrupted: the previous run stopped without finishing this attempt"); n > 0 || prev.Owner != "" && prev.Owner != owner {
		r.Rec.Emit(ctx, t.ID, "task.recovered", map[string]any{"previous_owner": prev.Owner, "last_heartbeat": prev.Heartbeat, "orphaned_attempts": n, "phase": t.Phase})
		t.Decide("policy", fmt.Sprintf("recovered after an unclean stop (phase %s, %d unfinished attempt(s))", t.Phase, n))
		r.say("recovering task %s after an unclean stop of the previous run", t.ID)
	}
	if t.Status == task.StatusBlocked {
		t.Status = task.StatusActive // explicit resume re-arms a blocked task
		t.Decide("user", "resumed after block")
	}
	wts, err := r.Ledger.Worktrees(ctx, t.ID)
	if err != nil {
		return t, err
	}
	var gitDirs, adminDirs []string
	for _, w := range wts {
		// Recreate missing worktrees from their branch: git is the persistence
		// layer. An existing worktree is integrity-checked before host git
		// runs in it.
		if err := gitops.EnsureWorktree(ctx, w.RepoPath, w.Path, w.Branch, w.BaseCommit); err != nil {
			return t, err
		}
		common, err := gitops.CommonDir(ctx, w.RepoPath)
		if err != nil {
			return t, err
		}
		if err := gitops.CheckTaskWorktree(w.Path, common, w.Branch); err != nil {
			return t, err
		}
		admin, err := gitops.AdminDir(ctx, w.Path)
		if err != nil {
			return t, err
		}
		gitDirs, adminDirs = append(gitDirs, common), append(adminDirs, admin)
	}
	runStart := time.Now()
	priorTokens, priorGen, priorCached := t.Budget.UsedLocalTokens, t.Budget.GeneratedTokens, t.Budget.CachedTokens
	gw := r.NewGateway(t.ID, max(t.Budget.MaxLocalTokens-priorTokens, 1))

	// Events from the runtime are audited; condensations are counted; the
	// current attempt's governor sees them (see governor.go).
	var evMu sync.Mutex
	condensations := 0
	var gov atomic.Pointer[governor]
	generated := func() int { _, g, _ := gw.Stats(); return g }
	onEvent := func(e agent.Event) {
		if g := gov.Load(); g != nil {
			g.observe(e, generated())
		}
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
	masks, err := secretMasks(r.WorkDir(t.ID), wts)
	if err != nil {
		return t, fmt.Errorf("refusing to start the agent: %w", err)
	}
	deps, depScratch, err := dependencyMounts(wts)
	if err != nil {
		return t, fmt.Errorf("refusing to start the agent: %w", err)
	}
	if r.Nav != nil {
		r.prepareNav(wts)
		defer r.releaseNav(wts)
	}
	openReq := agent.OpenRequest{TaskID: t.ID, SessionID: t.AgentSessionID, Workspace: r.WorkDir(t.ID), GitCommonDirs: gitDirs, GitAdminDirs: adminDirs,
		PersistenceDir: filepath.Join(r.Paths.TaskDir(t.ID), "runtime"), MaxIterations: r.Cfg.Agent.MaxIterations,
		MaxInputTokens: r.CtxSize, MaxOutputTokens: r.Cfg.Agent.MaxOutputTokens, CondenserMaxEvents: r.Cfg.Agent.CondenserMaxEvents,
		CondenserMaxTokens: r.CtxSize * 7 / 10, Masks: masks, DependencyMounts: deps, DependencyScratch: depScratch, Toolchain: r.agentToolchain(t.ID), OnEvent: onEvent, Gateway: gw,
		LLMTimeout: r.Cfg.Inference.RequestTimeout.D()}
	sess, mode, err := r.openSession(ctx, t, openReq)
	if err != nil {
		return t, err
	}
	defer sess.Close()

	consecutive := r.recentFailures(ctx, t.ID)
	lastSig := ""
	advice := r.pendingAdvice(ctx, t.ID)
	strategyStopped := "" // set when the governor stops an attempt; consumed by the next pack
	userFrontier := opt.UserRequestedFrontier
	contractChecked := false
	reviewedZ1 := r.hasEscalation(ctx, t.ID, frontier.Z1)
	reviewedZ3 := r.hasEscalation(ctx, t.ID, frontier.Z3)
	infraRetries := 0

	save := func() error {
		used, gen, cached := gw.Stats()
		t.Budget.UsedLocalTokens = priorTokens + used
		t.Budget.GeneratedTokens, t.Budget.CachedTokens = priorGen+gen, priorCached+cached
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

	interrupted := func(reason string) (*task.Task, error) {
		err := context.Cause(ctx)
		serr := save()
		r.Rec.Emit(context.WithoutCancel(ctx), t.ID, "task.interrupted", map[string]any{"reason": reason, "cause": fmt.Sprint(err),
			"attempt": t.AttemptCount, "phase": t.Phase, "wall_s": round1(t.Budget.UsedWallClockS)})
		if errors.Is(serr, task.ErrCancelled) {
			return t, serr
		}
		return t, err
	}
	// Read the request before implementing it (see contract.go): a
	// clarification from the user is recorded first, then the contract is
	// derived (once) and a material ambiguity is raised per policy.
	clarified := strings.TrimSpace(opt.Clarification) != ""
	if clarified {
		c := strings.TrimSpace(opt.Clarification)
		t.Goal = strings.TrimSpace(contractRequest(t) + "\n\nClarification from the user: " + c)
		t.Decide("user", "clarification: "+oneLineStr(c, 300))
		_ = os.Remove(r.contractPath(t.ID)) // re-derive with the answer
		r.Rec.Emit(ctx, t.ID, "task.clarified", map[string]any{"chars": len(c)})
	}
	contract := r.ensureContract(ctx, t)
	if stop, questions := r.ambiguityGate(ctx, t, contract, clarified); stop {
		r.say("task %s needs clarification before implementation:\n- %s\nanswer with: task run %s --clarify \"...\"", t.ID, questions, t.ID)
		return t, r.block(t, save, "SPEC_AMBIGUOUS: the request is materially ambiguous; clarify with `task run "+t.ID+" --clarify \"...\"`:\n- "+questions)
	}
	contractText := contractPackText(contract, len(contract.MaterialOrNone()) > 0)
	for {
		if ctx.Err() != nil {
			return interrupted("before attempt")
		}
		if reason := r.budgetExceeded(t, gw); reason != "" {
			return t, r.block(t, save, reason)
		}
		t.AttemptCount++
		t.Phase = task.PhaseImplementing
		if err := save(); err != nil {
			return t, err
		}
		attemptStart := time.Now()
		stratID, _ := r.Ledger.AddStrategy(ctx, t.ID, t.AttemptCount, "(in progress)")
		r.Rec.Emit(ctx, t.ID, "attempt.started", map[string]any{"attempt": t.AttemptCount, "mode": mode, "resources": r.resources(ctx)})
		results := r.latestVerification(ctx, t.ID, wts)
		strategies, _ := r.Ledger.Strategies(ctx, t.ID)
		contracts := r.contracts(ctx, t, wts)
		if strategyStopped != "" {
			advice = strings.TrimSpace(strategyStopped + "\n\n" + advice)
			strategyStopped = ""
		}
		pack, err := contextplan.Build(ctx, contextplan.Inputs{Task: t, Worktrees: r.packWorktrees(ctx, t, wts), WorkDir: r.WorkDir(t.ID),
			Strategies: strategies, Verification: results, Intel: r.Intel, Nav: r.Nav, Mode: mode,
			BudgetTokens: r.Cfg.Budgets.ContextPackTokens, MaxAttempts: t.Budget.MaxAttempts, Advice: advice,
			Contracts: contracts, ChangedFiles: t.ChangedFiles, Heads: heads(ctx, wts), TaskContract: contractText})
		if err != nil {
			return t, err
		}
		r.Rec.Emit(ctx, t.ID, "context.pack", map[string]any{"attempt": t.AttemptCount, "mode": pack.Mode, "tokens": pack.Tokens, "sections": pack.Sections, "dropped": pack.Dropped, "intel": pack.Intel})
		r.say("attempt %d/%d: sending %s context pack (%d tokens)", t.AttemptCount, t.Budget.MaxAttempts, mode, pack.Tokens)
		// The attempt is bounded by the remaining wall-clock budget, not only
		// checked between attempts.
		sendCtx, cancelSend := ctx, context.CancelFunc(func() {})
		if b := t.Budget; b.MaxWallClockS > 0 {
			left := time.Duration((b.MaxWallClockS-b.UsedWallClockS)*float64(time.Second)) - time.Since(runStart)
			sendCtx, cancelSend = context.WithTimeout(ctx, max(left, time.Second))
		}
		// The governor bounds this attempt's strategy by its progress.
		g := newGovernor(r.Cfg.Agent.Strategy, generated(), time.Now())
		gov.Store(g)
		turnCtx, stopTurn := context.WithCancelCause(sendCtx)
		watchDone := make(chan struct{})
		go func() {
			tick := time.NewTicker(governorTick)
			defer tick.Stop()
			for {
				select {
				case <-watchDone:
					return
				case now := <-tick.C:
					if stop, why := g.check(generated(), now); stop {
						stopTurn(errNoProgress{why})
						return
					}
				}
			}
		}()
		res, err := sess.Send(turnCtx, pack.Render())
		close(watchDone)
		var np errNoProgress
		noProgress := errors.As(context.Cause(turnCtx), &np) && ctx.Err() == nil
		stopTurn(nil)
		gov.Store(nil)
		budgetHit := sendCtx.Err() != nil && ctx.Err() == nil
		cancelSend()
		if noProgress {
			// Not a failure of the runtime: the strategy is over. Its work is
			// checkpointed and verified as usual; the next attempt is told
			// what this one did and to change approach. Long output alone
			// does not escalate (Z2 still applies to verification failures).
			summary := g.summary()
			r.Rec.Emit(ctx, t.ID, "strategy.stopped", map[string]any{"attempt": t.AttemptCount, "reason": np.reason, "summary": summary})
			r.say("strategy stopped: %s (%s)", np.reason, summary)
			strategyStopped = "The previous attempt was stopped for lack of progress: " + np.reason + ". It did: " + summary + ".\n" +
				"Do not repeat that approach. Re-read the task and the verification results, form a different hypothesis about the cause, " +
				"write a small test that reproduces the problem first, and make the smallest change that makes it pass."
			sess.Close()
			if sess, mode, err = r.openSession(ctx, t, openReq); err != nil {
				return t, err
			}
			// Compact the stopped strategy's history before the next attempt.
			if err := sess.Condense(ctx); err != nil {
				r.Log.Warn("condense after stopped strategy", "err", err)
			}
			err = nil
			res = agent.Result{Status: "stopped", FinalMessage: "strategy stopped: " + np.reason}
		}
		if err != nil {
			if ctx.Err() != nil {
				_ = r.Ledger.ResolveStrategy(context.WithoutCancel(ctx), stratID, "rejected", "interrupted before completion")
				return interrupted("during agent turn")
			}
			if budgetHit {
				_ = r.Ledger.ResolveStrategy(ctx, stratID, "rejected", "wall-clock budget exhausted during the attempt")
				return t, r.block(t, save, "wall-clock budget exhausted")
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
		// A model server failure is infrastructure, not a failed strategy: it
		// must not burn attempts or count toward Z2. Repair and retry.
		if n := r.transportErrors(ctx, t.ID, attemptStart); n > 0 && res.Error != "" {
			infraRetries++
			_ = r.Ledger.ResolveStrategy(ctx, stratID, "rejected", "infrastructure: model server unavailable ("+trunc(res.Error, 160)+")")
			t.AttemptCount-- // not a real attempt
			r.Rec.Emit(ctx, t.ID, "infra.failure", map[string]any{"transport_errors": n, "retry": infraRetries, "error": trunc(res.Error, 300)})
			r.say("model server failure (%d transport errors); repairing (%d/%d)", n, infraRetries, maxInfraRetries)
			if infraRetries > maxInfraRetries || r.EnsureModel == nil {
				return t, r.block(t, save, "local model server unavailable")
			}
			if err := r.EnsureModel(ctx); err != nil {
				r.Rec.Emit(ctx, t.ID, "infra.repair_failed", map[string]any{"error": trunc(err.Error(), 300)})
				return t, r.block(t, save, "local model server could not be restarted: "+trunc(err.Error(), 200))
			}
			if err := save(); err != nil {
				return t, err
			}
			continue
		}

		// Git checkpoint: persist the attempt's work on the task branch. The
		// worktree pointer is re-checked first: the agent could have tampered with it.
		changedAny := false
		var changedFiles, changedRepos []string
		var changedSyms []string
		for i, w := range wts {
			if err := gitops.CheckTaskWorktree(w.Path, gitDirs[i], w.Branch); err != nil {
				r.Rec.Emit(ctx, t.ID, "policy.violation", map[string]any{"repo": w.RepoName, "error": err.Error()})
				_ = r.Ledger.ResolveStrategy(ctx, stratID, "rejected", "worktree integrity check failed")
				return t, r.block(t, save, "worktree integrity check failed: "+err.Error())
			}
			// A checkpoint that cannot be committed would let verification
			// pass on work the task branch does not contain.
			if _, err := gitops.CommitAll(ctx, w.Path, fmt.Sprintf("boundedcode %s: attempt %d", t.ID, t.AttemptCount)); err != nil {
				r.Rec.Emit(ctx, t.ID, "checkpoint.failed", map[string]any{"repo": w.RepoName, "error": trunc(err.Error(), 400)})
				_ = r.Ledger.ResolveStrategy(ctx, stratID, "rejected", "checkpoint commit failed")
				return t, r.block(t, save, fmt.Sprintf("checkpoint commit failed in %s: %s", w.RepoName, trunc(err.Error(), 200)))
			}
			files, _ := gitops.ChangedFiles(ctx, w.Path, w.BaseCommit)
			if len(files) > 0 {
				changedAny = true
				changedRepos = append(changedRepos, w.RepoName)
				for _, f := range files {
					changedFiles = append(changedFiles, w.RepoName+"/"+f)
				}
				syms, _ := gitops.ChangedSymbols(ctx, w.Path, w.BaseCommit)
				for _, sym := range syms {
					changedSyms = append(changedSyms, w.RepoName+"/"+sym)
				}
			}
		}
		t.ChangedFiles, t.ChangedRepositories, t.ChangedSymbols = changedFiles, changedRepos, changedSyms
		if changedAny {
			t.MarkStep("implement")
		}
		r.recordDiffAfter(ctx, t.ID, wts)
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
		if ctx.Err() != nil {
			_ = r.Ledger.ResolveStrategy(context.WithoutCancel(ctx), stratID, "rejected", "interrupted during verification")
			return interrupted("during verification")
		}
		t.Budget.VerificationRuns++
		summary := trunc(res.FinalMessage, 500)
		if summary == "" {
			summary = fmt.Sprintf("attempt %d (status %s, %d events)", t.AttemptCount, res.Status, res.EventsNew)
		}
		if passed {
			inTask, outside := r.unupdatedCounterparts(ctx, t, wts, changedFiles, changedRepos)
			// Local contract check first: a counterpart the agent can edit gets
			// one targeted round before any frontier use.
			roundsLeft := t.Budget.MaxAttempts == 0 || t.AttemptCount < t.Budget.MaxAttempts
			if len(inTask) > 0 && !contractChecked && roundsLeft {
				contractChecked = true
				r.Rec.Emit(ctx, t.ID, "contract.check", map[string]any{"counterparts": inTask})
				r.say("contract check: %d cross-service counterpart(s) not updated; asking the agent to verify", len(inTask))
				_ = r.Ledger.ResolveStrategy(ctx, stratID, "succeeded", "verification passed; contract check requested")
				_ = r.updateStrategySummary(ctx, stratID, summary)
				advice = "Contract check (deterministic analysis, not a test failure): your change touches cross-service contracts whose other side you did not modify:\n- " +
					strings.Join(inTask, "\n- ") + "\nInspect each counterpart. If it must change for the system to keep working end to end (field names, payload shape, paths, topics, env names), update it and its tests. If it is unaffected, finish without changes."
				mode = contextplan.ModeRetry
				continue
			}
			// Z3: high-risk changes get one frontier review before merge. The
			// review happens even on the last attempt; advice that cannot be
			// applied then blocks the task instead of being dropped.
			if !reviewedZ3 {
				trs := frontier.Evaluate(r.Cfg.Escalation, frontier.Signals{Text: t.OriginalRequest, ChangedFiles: changedFiles,
					ChangedRepos: len(changedRepos), PreMerge: true, EscalationsUsed: t.Budget.UsedEscalations,
					MaxEscalations: t.Budget.MaxEscalations, AlreadyReviewedZ1: reviewedZ1, UserRequested: userFrontier,
					UnupdatedCounterparts: outside})
				if tr, ok := find(trs, frontier.Z3, frontier.Z4); ok {
					reviewedZ3, userFrontier = true, false
					if a := r.escalate(ctx, t, wts, tr, contextplan.ModeRetry); a != "" && !roundsLeft {
						// The answer stays pending (no attempt applied it yet), so
						// the next run picks it up (pendingAdvice).
						_ = r.Ledger.ResolveStrategy(ctx, stratID, "succeeded", "verification passed; frontier review pending")
						return t, r.block(t, save, "pre-merge review returned advice but no attempts are left; `task resume` applies it with a larger budget")
					} else if a != "" {
						advice = "Pre-merge review from the frontier model. Apply findings that are real defects; ignore ones that are not. If nothing needs fixing, finish without changes.\n\n" + a
						_ = r.Ledger.ResolveStrategy(ctx, stratID, "succeeded", "verification passed; frontier review requested")
						mode = contextplan.ModeRetry
						continue
					}
				}
			}
			// Green checks are not proof that the requested behaviour exists:
			// ask once for a test that demonstrates it (see verify/delta.go).
			verified, why := r.behaviourEvidence(ctx, t, wts, changedRepos)
			if !verified && roundsLeft && !r.hasEvent(ctx, t.ID, "verify.evidence_requested") {
				r.Rec.Emit(ctx, t.ID, "verify.evidence_requested", map[string]any{"reason": why})
				r.say("verification passed but %s; asking the agent for a test that demonstrates the change", why)
				_ = r.Ledger.ResolveStrategy(ctx, stratID, "succeeded", "verification passed; behavioural evidence requested")
				_ = r.updateStrategySummary(ctx, stratID, summary)
				advice = evidenceRequest(why)
				mode = contextplan.ModeRetry
				continue
			}
			_ = r.Ledger.ResolveStrategy(ctx, stratID, "succeeded", "verification passed (targeted + full)")
			_ = r.updateStrategySummary(ctx, stratID, summary)
			t.MarkStep("implement")
			t.MarkStep("verify (full gate)")
			t.Status, t.Phase, t.FinishedAt = task.StatusCompleted, task.PhaseReview, store.Now()
			if verified {
				t.VerificationState = task.VerificationTaskVerified
				t.Decide("policy", "merge candidate: all verification passed and a test demonstrates the change, on branch "+gitops.TaskBranch(t.ID))
			} else {
				t.VerificationState = task.VerificationTestsGreen
				t.Decide("policy", "UNVERIFIED candidate: checks are green but "+why+"; review before merging branch "+gitops.TaskBranch(t.ID))
			}
			if err := save(); err != nil {
				return t, err
			}
			r.finishEscalations(ctx, t.ID, "helped")
			SetEscalationTaskOutcome(ctx, r.DB, t.ID, string(task.StatusCompleted))
			r.Rec.Emit(ctx, t.ID, "task.completed", map[string]any{"attempts": t.AttemptCount, "tokens": t.Budget.UsedLocalTokens,
				"escalations": t.Budget.UsedEscalations, "condensations": t.Budget.Condensations, "wall_s": round1(t.Budget.UsedWallClockS)})
			if verified {
				r.say("task %s is a verified merge candidate on %s", t.ID, gitops.TaskBranch(t.ID))
			} else {
				r.say("task %s: checks green but UNVERIFIED (%s); candidate on %s needs review", t.ID, why, gitops.TaskBranch(t.ID))
			}
			return t, nil
		}

		// Failure path.
		t.ReopenStep("verify (targeted)")
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
		if tr, ok := find(trs, frontier.Z2, frontier.Z1, frontier.Z4); ok {
			userFrontier = false
			if tr.Code == frontier.Z1 {
				reviewedZ1 = true
			}
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
	SetEscalationTaskOutcome(context.Background(), r.DB, t.ID, string(task.StatusBlocked))
	r.Rec.Emit(context.Background(), t.ID, "task.blocked", map[string]any{"reason": reason, "attempts": t.AttemptCount, "wall_s": round1(t.Budget.UsedWallClockS)})
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
		res, err := r.Verify.Run(ctx, verify.RepoTarget{Name: w.RepoName, Worktree: w.Path, Base: w.BaseCommit, TaskID: t.ID, Source: w.RepoPath}, scope)
		if err != nil {
			return false, "verify-error", err.Error()
		}
		r.say("verify %s %s: passed=%v (%s)", scope, w.RepoName, res.Passed, res.Duration.Round(time.Millisecond))
		if !res.Passed {
			passed = false
			sigs = append(sigs, w.RepoName+":"+res.Signature())
			for _, f := range res.Failures() {
				summary = append(summary, fmt.Sprintf("%s/%s: %s", w.RepoName, f.Name, f.Headline()))
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

// prepareNav hides each worktree's secret paths from the navigator, the same
// paths the sandbox masks for the agent.
func (r *Runner) prepareNav(wts []task.Worktree) {
	ig, ok := r.Nav.(interface {
		Ignore(root string, paths []string)
	})
	if !ok {
		return
	}
	for _, w := range wts {
		found, _ := policy.FindSecretPaths(w.Path, policy.MaxSecretMasks)
		ig.Ignore(w.Path, found)
	}
}

// releaseNav stops the navigator's processes for this task's worktrees.
func (r *Runner) releaseNav(wts []task.Worktree) {
	rel, ok := r.Nav.(interface{ Release(roots ...string) })
	if !ok {
		return
	}
	roots := make([]string, 0, len(wts))
	for _, w := range wts {
		roots = append(roots, w.Path)
	}
	rel.Release(roots...)
}

func secretMasks(workDir string, wts []task.Worktree) ([]string, error) {
	var out []string
	for _, w := range wts {
		found, err := policy.FindSecretPaths(w.Path, policy.MaxSecretMasks)
		if err != nil {
			return nil, err
		}
		rel, _ := filepath.Rel(workDir, w.Path)
		for _, f := range found {
			out = append(out, filepath.Join(rel, f))
		}
	}
	return out, nil
}

// behaviourEvidence reports whether a test the change added or modified
// fails on the base and passes on the candidate, in any changed repository.
func (r *Runner) behaviourEvidence(ctx context.Context, t *task.Task, wts []task.Worktree, changedRepos []string) (bool, string) {
	why := "no test demonstrates the change"
	for _, w := range wts {
		if !slices.Contains(changedRepos, w.RepoName) {
			continue
		}
		ev, err := r.Verify.BehaviourEvidence(ctx, verify.RepoTarget{Name: w.RepoName, Worktree: w.Path, Base: w.BaseCommit, TaskID: t.ID, Source: w.RepoPath})
		if err != nil {
			r.Log.Warn("behavioural evidence", "repo", w.RepoName, "err", err)
			why = "the behavioural check failed to run: " + trunc(err.Error(), 200)
			continue
		}
		r.Rec.Emit(ctx, t.ID, "verify.evidence", map[string]any{"repo": w.RepoName, "verified": ev.Verified, "tests": ev.Tests,
			"test_files": len(ev.TestFiles), "reason": ev.Reason})
		if ev.Verified {
			return true, ev.Reason
		}
		why = ev.Reason
	}
	return false, why
}

// evidenceRequest is the retry instruction when checks pass without
// behavioural evidence.
func evidenceRequest(why string) string {
	return "Verification passed, but nothing yet shows that the requested behaviour works: " + why + ".\n" +
		"Add a test that reproduces the problem or requirement described in the task, next to the existing tests of the code you changed. " +
		"It must fail on the original code and pass with your change. Use the concrete inputs and expected results from the task where it gives them. " +
		"Run it yourself, fix the code if it fails, and do not change expected values just to match the current output."
}

// hasEvent reports whether the task's audit log has an event of this kind.
func (r *Runner) hasEvent(ctx context.Context, taskID, kind string) bool {
	var n int
	_ = r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE task_id = ? AND kind = ?`, taskID, kind).Scan(&n)
	return n > 0
}

// agentToolchain is the agent's build environment: the module cache
// verification uses, and a build cache of the agent's own in the task dir.
func (r *Runner) agentToolchain(taskID string) agent.Toolchain {
	tc := agent.Toolchain{GoCache: filepath.Join(r.Paths.TaskDir(taskID), "agent-gocache")}
	if r.Verify != nil {
		tc.GoModCache = r.Verify.GoModCache
	}
	return tc
}

// dependencyMounts returns the installed dependencies of each worktree's
// repository checkout (see sandbox.DependencyMounts). The source path comes
// from the ledger, not from the agent-writable worktree.
func dependencyMounts(wts []task.Worktree) ([]agent.DependencyMount, []string, error) {
	var out []agent.DependencyMount
	var scratch []string
	for _, w := range wts {
		deps, err := sandbox.DependencyMounts(w.RepoPath, w.Path)
		if err != nil {
			return nil, nil, err
		}
		for _, m := range deps.Mounts {
			out = append(out, agent.DependencyMount{Host: m.Host, Target: m.Target})
		}
		scratch = append(scratch, deps.Scratch...)
	}
	return out, scratch, nil
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

// contracts returns cross-service links involving the task's repositories.
// Task repositories are rescanned from their worktrees (so the agent's edits
// are reflected); other workspace repositories come from the index.
func (r *Runner) contracts(ctx context.Context, t *task.Task, wts []task.Worktree) []xservice.Link {
	if !r.CrossService {
		return nil
	}
	inTask := map[string]bool{}
	var eps []xservice.Endpoint
	for _, w := range wts {
		inTask[w.RepoName] = true
		e, _, err := xservice.Scan(w.RepoName, w.Path, xservice.ScanOptions{})
		if err != nil {
			r.Log.Warn("cross-service scan failed", "repo", w.RepoName, "err", err)
			continue
		}
		eps = append(eps, e...)
	}
	stored, err := xservice.LoadWorkspace(ctx, r.DB, t.WorkspaceID)
	if err != nil {
		r.Log.Warn("load cross-service index", "err", err)
	}
	for _, e := range stored {
		if !inTask[e.Repo] {
			eps = append(eps, e)
		}
	}
	var out []xservice.Link
	for _, l := range xservice.LinkAll(eps, xservice.LinkOptions{}) {
		if inTask[l.From.Repo] || inTask[l.To.Repo] {
			out = append(out, l)
		}
	}
	return out
}

// unupdatedCounterparts lists HTTP/topic contracts touched by the change
// whose other side is in a repository the task did not change, split into
// counterparts the agent can edit (task repos) and ones it cannot.
func (r *Runner) unupdatedCounterparts(ctx context.Context, t *task.Task, wts []task.Worktree, changedFiles, changedRepos []string) (inTask, outside []string) {
	links := xservice.Touching(r.contracts(ctx, t, wts), changedFiles)
	changed := map[string]bool{}
	for _, c := range changedRepos {
		changed[c] = true
	}
	taskRepo := map[string]bool{}
	for _, w := range wts {
		taskRepo[w.RepoName] = true
	}
	seen := map[string]bool{}
	for _, l := range links {
		if l.Kind != "http" && l.Kind != "topic" {
			continue
		}
		for _, pair := range [][2]xservice.Endpoint{{l.From, l.To}, {l.To, l.From}} {
			mine, other := pair[0], pair[1]
			if !changed[mine.Repo] || changed[other.Repo] || mine.Repo == other.Repo {
				continue
			}
			d := fmt.Sprintf("%s: %s side in ./%s/%s:%d", l.Contract, other.Kind, other.Repo, other.File, other.Line)
			if seen[d] {
				continue
			}
			seen[d] = true
			if taskRepo[other.Repo] {
				inTask = append(inTask, d)
			} else {
				outside = append(outside, d)
			}
		}
	}
	return inTask, outside
}

// keepLease renews the run lease and stops the run when the lease is lost or
// the task is cancelled from another process (`task cancel`).
func (r *Runner) keepLease(ctx context.Context, taskID, owner string, stop context.CancelCauseFunc) {
	tick := time.NewTicker(leaseHeartbeat)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		if err := r.Ledger.Heartbeat(ctx, taskID, owner); err != nil && ctx.Err() == nil {
			stop(fmt.Errorf("lease lost: %w", err))
			return
		}
		if st, err := r.Ledger.Status(ctx, taskID); err == nil && st == task.StatusCancelled {
			stop(task.ErrCancelled)
			return
		}
	}
}

// transportErrors counts model calls of the task that failed to reach the
// model server since t.
func (r *Runner) transportErrors(ctx context.Context, taskID string, since time.Time) int {
	var n int
	_ = r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM model_calls WHERE task_id = ? AND status = 'transport_error' AND created_at >= ?`,
		taskID, since.UTC().Format(time.RFC3339Nano)).Scan(&n)
	return n
}

// packWorktrees returns the worktrees as the context planner should see
// them. codebase-memory-mcp's change impact only diffs the checkout a project
// was indexed from, so once a task has changes each worktree is indexed as
// its own project (fast mode, ~5 s on the fixtures) and the pack's impact,
// graph snippets and callers describe the task's code, not the primary
// checkout. Index failures fall back to the primary project.
//
// Whether a worktree has changes is read from git, not from the ledger's
// ChangedFiles: those are recorded when an attempt finishes, so after an
// interrupted attempt the worktree has changes the ledger does not list yet,
// and the pack described the unchanged primary checkout ("changed_total: 0"
// next to a real diff, found on a resumed Prometheus task).
func (r *Runner) packWorktrees(ctx context.Context, t *task.Task, wts []task.Worktree) []task.Worktree {
	out := append([]task.Worktree(nil), wts...)
	if r.Intel == nil {
		return out
	}
	for i, w := range out {
		if w.IndexProject == "" {
			continue
		}
		if files, err := gitops.ChangedFiles(ctx, w.Path, w.BaseCommit); err != nil || len(files) == 0 {
			continue
		}
		start := time.Now()
		res, err := r.Intel.Index(ctx, w.Path, WorktreeProject(t.ID, w.RepoName), "fast")
		if err != nil || res.Project == "" {
			r.Log.Warn("worktree index failed; using the primary checkout's graph", "repo", w.RepoName, "err", err)
			continue
		}
		out[i].IndexProject = res.Project
		r.Rec.Emit(ctx, t.ID, "intel.worktree_indexed", map[string]any{"repo": w.RepoName, "project": res.Project,
			"nodes": res.Nodes, "ms": time.Since(start).Milliseconds()})
	}
	return out
}

// WorktreeProject names the code-graph project of a task worktree.
func WorktreeProject(taskID, repo string) string { return "bc-task-" + taskID + "-" + repo }

// heads returns each worktree's current commit.
func heads(ctx context.Context, wts []task.Worktree) map[string]string {
	out := map[string]string{}
	for _, w := range wts {
		if h, err := gitops.Run(ctx, w.Path, "rev-parse", "HEAD"); err == nil && h != w.BaseCommit {
			out[w.RepoName] = h
		}
	}
	return out
}

// resources is a compact resource sample for attempt events.
func (r *Runner) resources(ctx context.Context) map[string]any {
	snap := hw.Probe(ctx)
	out := map[string]any{"mem_available_mib": snap.MemAvailMiB, "swap_used_mib": snap.SwapUsedMiB}
	for _, g := range snap.GPUs {
		out[fmt.Sprintf("gpu%d_mem_used_mib", g.Index)] = g.MemUsedMiB
		out[fmt.Sprintf("gpu%d_temp_c", g.Index)] = g.TempC
	}
	return out
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }
