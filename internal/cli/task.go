package cli

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/akynte/boundedcode/internal/agent"
	"github.com/akynte/boundedcode/internal/agent/openhands"
	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/frontier"
	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/model"
	"github.com/akynte/boundedcode/internal/orchestrator"
	"github.com/akynte/boundedcode/internal/repointel/cbm"
	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/telemetry"
	"github.com/akynte/boundedcode/internal/verify"
	"github.com/akynte/boundedcode/internal/workspace"
)

type runFlags struct {
	unsafeNoSandbox bool
	approveFrontier bool
	condenseRetry   bool
	model           string
	serena          string // "", "on", "off": override repointel.serena.enabled
}

func (a *App) sandbox(allowNone bool) (sandbox.Sandbox, error) {
	c := a.Config.Sandbox
	if c.Kind == "none" {
		if !allowNone {
			return nil, errors.New("sandbox.kind is none: refusing to run an autonomous agent unsandboxed (pass --unsafe-no-sandbox to override)")
		}
		return sandbox.None{}, nil
	}
	return &sandbox.Container{Engine: c.Engine, Image: a.Config.Agent.Image, Network: c.Network, Memory: c.Memory, CPUs: c.CPUs,
		PIDs: 4096, UID: sandboxUID(), GID: sandboxGID()}, nil
}

func (a *App) adapterArgv(sb sandbox.Sandbox) ([]string, error) {
	if sb.Isolated() {
		return []string{"bc-openhands-adapter"}, nil
	}
	dir := a.Config.Agent.AdapterDir
	if dir == "" {
		return nil, errors.New("agent.adapter_dir is not configured (needed without a container sandbox)")
	}
	return []string{"uv", "run", "--frozen", "--project", dir, "bc-openhands-adapter"}, nil
}

// buildRunner wires a task runner from configuration. The returned cleanup
// closes long-lived helpers (repository-intelligence session).
func (a *App) buildRunner(ctx context.Context, f runFlags) (*orchestrator.Runner, func(), error) {
	s, err := a.Store(ctx)
	if err != nil {
		return nil, nil, err
	}
	return a.buildRunnerWith(ctx, f, s.DB, a.Paths)
}

// buildRunnerWith wires a runner against an explicit database and paths
// (benchmarks use an isolated state per task).
func (a *App) buildRunnerWith(ctx context.Context, f runFlags, db *sql.DB, paths config.Paths) (*orchestrator.Runner, func(), error) {
	var err error
	rec := telemetry.New(db, a.Log)
	cloudMode := a.Config.Inference.IsCloud()
	var p model.Profile
	if !cloudMode {
		if p, err = a.profile(f.model); err != nil {
			return nil, nil, err
		}
	}
	// Cheap dependency checks come before loading the model, so a missing
	// engine or image is reported at once and with its fix.
	sb, err := a.sandbox(f.unsafeNoSandbox)
	if err != nil {
		return nil, nil, err
	}
	if c, ok := sb.(sandbox.Checker); ok {
		if err := c.Check(ctx); err != nil {
			return nil, nil, err
		}
	}
	argv, err := a.adapterArgv(sb)
	if err != nil {
		return nil, nil, err
	}
	timeout := a.Config.Inference.RequestTimeout.D()
	// Inference endpoint.
	var ep inference.Endpoint
	var cloud *cloudEndpoint
	modelName, ctxSize := p.Name, p.Server.CtxSize
	if cloudMode {
		if cloud, err = a.prepareCloud(ctx, f.model); err != nil {
			return nil, nil, err
		}
		modelName, ctxSize = cloud.model, cloud.ctxSize
		a.printf("using %s model %s (working context %d tokens)\n", cloud.provider, cloud.model, cloud.ctxSize)
	} else if rt := a.inferenceRuntime(); rt != nil {
		a.printf("ensuring %s is serving %s…\n", rt.Name(), p.Name)
		if ep, err = rt.Ensure(ctx, p); err != nil {
			return nil, nil, err
		}
	} else {
		ep = inference.Endpoint{BaseURL: a.Config.Inference.ExternalURL, Model: p.Name}
		if err := a.checkExternalInference(ctx); err != nil {
			return nil, nil, err
		}
	}
	newGW := func(taskID string, maxTokens int) *inference.Gateway {
		gw := &inference.Gateway{Model: ep.Model, DB: db, TaskID: taskID, Source: "agent", MaxTokens: maxTokens}
		if cloud != nil {
			// The key stays here, in the host-side gateway; the agent's
			// sandbox only sees the stdio tunnel (ADR-0010).
			gw.Model, gw.Upstream = cloud.model, cloud.upstream(paths.TaskDir(taskID))
		} else {
			gw.Client = inference.NewClient(ep.BaseURL, timeout)
		}
		return gw
	}
	var rt agent.Runtime
	switch a.Config.Agent.Runtime {
	case "openhands":
		rt = &openhands.Runtime{Sandbox: sb, Argv: argv, Log: a.Log,
			Gateway: func(taskID string) *inference.Gateway { return newGW(taskID, 0) }}
	default:
		return nil, nil, fmt.Errorf("agent runtime %q cannot run tasks from the CLI", a.Config.Agent.Runtime)
	}
	// Repository intelligence is optional for a run: one warning names the
	// problem and its fix, and the run continues without graph context.
	var intel *cbm.Client
	cleanup := func() {}
	if err := cbm.Preflight(ctx, a.Config.RepoIntel.Binary); err != nil {
		fmt.Fprintf(a.Err, "warning: %v (continuing without graph context)\n", err)
	} else {
		intel = &cbm.Client{Binary: a.Config.RepoIntel.Binary, CacheDir: filepath.Join(paths.Cache, "codebase-memory")}
		cleanup = func() { _ = intel.Close() }
		if err := intel.Open(ctx); err != nil {
			a.Log.Warn("persistent repository-intelligence session unavailable; using the one-shot CLI", "err", err)
			cleanup = func() {}
		}
	}
	switch f.serena {
	case "on":
		a.Config.RepoIntel.Serena.Enabled = true
	case "off":
		a.Config.RepoIntel.Serena.Enabled = false
	case "":
	default:
		return nil, nil, fmt.Errorf("--serena: %q is not on|off", f.serena)
	}
	nav, closeNav := a.serenaNavigator(ctx, paths)
	if nav != nil {
		closeIntel := cleanup
		cleanup = func() { closeNav(); closeIntel() }
	}
	gomodcache, _ := exec.CommandContext(ctx, "go", "env", "GOMODCACHE").Output()
	r := &orchestrator.Runner{
		DB: db, Ledger: task.Ledger{DB: db}, Rec: rec, Agent: rt,
		Verify: &verify.Engine{Sandbox: sb, CacheDir: filepath.Join(paths.Cache, "build"), GoModCache: strings.TrimSpace(string(gomodcache)), Packages: sandbox.HostPackageCaches(), DB: db, Rec: rec},
		Cfg:    a.Config, Paths: paths, Model: modelName, CtxSize: ctxSize, Log: a.Log, Out: a.Err,
		CondenseEachRetry: f.condenseRetry,
		CrossService:      a.Config.RepoIntel.CrossService,
	}
	if nav != nil {
		r.Nav = nav // assigned only when usable: a nil *Navigator is a non-nil interface
	}
	if intel != nil {
		r.Intel = intel // likewise
	}
	r.WS = workspace.Store{DB: db}
	if rt := a.inferenceRuntime(); rt != nil && !cloudMode {
		r.EnsureModel = func(ctx context.Context) error { _, err := rt.Ensure(ctx, p); return err }
	}
	// Gateway budget is per task; the runner passes the remaining allowance.
	r.NewGateway = newGW
	if a.Config.Frontier.Enabled {
		switch a.Config.Frontier.Provider {
		case "codex":
			cx := &frontier.Codex{Binary: a.Config.Frontier.Binary, Model: a.Config.Frontier.Model, Timeout: a.Config.Frontier.Timeout.D()}
			if a.Config.Frontier.Contain {
				if a.Config.Sandbox.Kind != "docker" {
					return nil, nil, errors.New("frontier.contain requires a container engine (sandbox.kind: docker); set frontier.contain: false to run codex unconfined")
				}
				cx.Container = &frontier.CodexContainer{Engine: a.Config.Sandbox.Engine, Image: a.Config.Agent.Image, UID: sandboxUID(), GID: sandboxGID(),
					LinuxBinary: a.containerCodex()}
			}
			r.Frontier = cx
		case "manual":
			r.Frontier = frontier.Manual{}
		}
	}
	r.Approve = func(ctx context.Context, tr frontier.Trigger, packetPath string, tokens int) bool {
		if f.approveFrontier {
			return true
		}
		if a.approve != nil {
			return a.approve(ctx, tr, packetPath, tokens)
		}
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Fprintf(a.Err, "frontier escalation %s needs approval (non-interactive: declined; use --approve-frontier)\n", tr.Code)
			return false
		}
		fmt.Fprintf(a.Err, "\nFrontier escalation %s: %s\nPacket (%d tokens): %s\nSend to %s? [y/N] ", tr.Code, tr.Reason, tokens, packetPath, a.Config.Frontier.Provider)
		answer := make(chan string, 1)
		go func() {
			line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			answer <- line
		}()
		select {
		case line := <-answer:
			return strings.EqualFold(strings.TrimSpace(line), "y")
		case <-ctx.Done():
			return false
		}
	}
	return r, cleanup, nil
}

func addRunFlags(cmd *cobra.Command, f *runFlags) {
	cmd.Flags().BoolVar(&f.unsafeNoSandbox, "unsafe-no-sandbox", false, "allow running the agent without a container sandbox (development only)")
	cmd.Flags().BoolVar(&f.approveFrontier, "approve-frontier", false, "pre-approve frontier escalations (otherwise asked interactively)")
	cmd.Flags().BoolVar(&f.condenseRetry, "condense-each-retry", false, "force context condensation before every retry (continuity testing)")
	cmd.Flags().StringVarP(&f.model, "model", "m", "", "model profile")
	cmd.Flags().StringVar(&f.serena, "serena", "", "override repointel.serena.enabled for this run: on|off")
}

func newTaskCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "task", Short: "Create, run, resume and inspect tasks"}
	var (
		wsFlag   string
		repos    []string
		criteria []string
		run      bool
		fromTask string
		rf       runFlags
	)
	create := &cobra.Command{
		Use:   "create REQUEST",
		Short: "Create a task (isolated worktrees on agent/<task-id>); --run starts it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			t, r, cleanup, err := app.createTask(ctx, wsFlag, args[0], repos, criteria, fromTask, rf, run)
			if err != nil {
				return err
			}
			defer cleanup()
			app.printf("created task %s (branch %s)\n", t.ID, gitops.TaskBranch(t.ID))
			if !run {
				return nil
			}
			return runAndReport(ctx, app, r, t.ID, orchestrator.RunOptions{})
		},
	}
	create.Flags().StringVarP(&wsFlag, "workspace", "w", "", "workspace")
	create.Flags().StringSliceVarP(&repos, "repo", "r", nil, "repositories to include (default: all)")
	create.Flags().StringArrayVarP(&criteria, "criteria", "c", nil, "acceptance criterion (repeatable)")
	create.Flags().BoolVar(&run, "run", false, "run the task immediately")
	create.Flags().StringVar(&fromTask, "from", "", "follow-up: start from this task's branch instead of HEAD")
	addRunFlags(create, &rf)

	var rf2 runFlags
	var clarify string
	runCmd := &cobra.Command{
		Use:     "run TASK",
		Aliases: []string{"resume"},
		Short:   "Run or resume a task until it completes, blocks or is interrupted",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.applyTaskWorkspace(cmd.Context(), args[0]); err != nil {
				return err
			}
			r, cleanup, err := app.taskRunner(cmd.Context(), rf2, true)
			if err != nil {
				return err
			}
			defer cleanup()
			return runAndReport(cmd.Context(), app, r, args[0], orchestrator.RunOptions{Clarification: clarify})
		},
	}
	addRunFlags(runCmd, &rf2)
	runCmd.Flags().StringVar(&clarify, "clarify", "", "answer for a task blocked as materially ambiguous (SPEC_AMBIGUOUS)")

	status := &cobra.Command{
		Use: "status [TASK]", Short: "Show a task (or recent tasks)", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s, err := app.Store(ctx)
			if err != nil {
				return err
			}
			l := task.Ledger{DB: s.DB}
			if len(args) == 0 {
				ts, err := l.List(ctx, "", 20)
				if err != nil {
					return err
				}
				if app.jsonOut {
					return app.printJSON(ts)
				}
				for _, t := range ts {
					app.printf("%-20s %-10s %-13s attempts=%d verify=%-13s %s\n", t.ID, t.Status, t.Phase, t.AttemptCount, t.VerificationState, oneLine(t.OriginalRequest, 70))
				}
				return nil
			}
			t, err := l.Get(ctx, args[0])
			if err != nil {
				return err
			}
			strats, _ := l.Strategies(ctx, t.ID)
			wts, _ := l.Worktrees(ctx, t.ID)
			if app.jsonOut {
				return app.printJSON(map[string]any{"task": t, "strategies": strats, "worktrees": wts})
			}
			app.printf("task %s  status=%s phase=%s verification=%s\n", t.ID, t.Status, t.Phase, t.VerificationState)
			app.printf("request: %s\n", t.OriginalRequest)
			for _, c := range t.AcceptanceCriteria {
				app.printf("  criterion: %s\n", c)
			}
			b := t.Budget
			app.printf("attempts %d/%d  local tokens %d/%d  escalations %d/%d  wall %.0fs  condensations %d  resumes %d  resets %d\n",
				t.AttemptCount, b.MaxAttempts, b.UsedLocalTokens, b.MaxLocalTokens, b.UsedEscalations, b.MaxEscalations, b.UsedWallClockS,
				b.Condensations, b.SessionsResumed, b.ContextResets)
			app.printf("agent: %s session %s (model %s)\n", t.AgentRuntime, t.AgentSessionID, t.ModelProfile)
			for _, w := range wts {
				app.printf("worktree %s: %s (%s from %s)\n", w.RepoName, w.Path, w.Branch, w.BaseCommit[:min(12, len(w.BaseCommit))])
			}
			for _, f := range t.ChangedFiles {
				app.printf("  changed: %s\n", f)
			}
			for _, s := range strats {
				app.printf("  attempt %d [%s] %s — %s\n", s.Attempt, s.Outcome, oneLine(s.Summary, 80), oneLine(s.Reason, 100))
			}
			for _, d := range t.Decisions {
				app.printf("  decision [%s] %s\n", d.Source, oneLine(d.Text, 120))
			}
			return nil
		},
	}
	events := &cobra.Command{
		Use: "events TASK", Short: "Print the task's audit log", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s, err := app.Store(ctx)
			if err != nil {
				return err
			}
			t, err := task.Ledger{DB: s.DB}.Get(ctx, args[0])
			if err != nil {
				return err
			}
			evs, err := telemetry.Events(ctx, s.DB, t.ID, 0, 100000)
			if err != nil {
				return err
			}
			if app.jsonOut {
				return app.printJSON(evs)
			}
			for _, e := range evs {
				app.printf("%s %-20s %s\n", e.TS[11:19], e.Kind, oneLine(string(e.Data), 160))
			}
			return nil
		},
	}
	diff := &cobra.Command{
		Use: "diff TASK", Short: "Show the task's changes against its base commits", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			diffs, err := app.taskDiffs(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			for _, d := range diffs {
				app.printf("### %s\n%s\n", d.Repo, d.Diff)
			}
			return nil
		},
	}
	cancel := &cobra.Command{
		Use: "cancel TASK", Short: "Cancel a task (worktrees and branch are kept)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return app.cancelTask(cmd.Context(), args[0]) },
	}
	var deleteBranch bool
	cleanupCmd := &cobra.Command{
		Use:   "cleanup TASK",
		Short: "Remove a finished task's worktrees and caches (branch kept unless --delete-branch)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.cleanupTask(cmd.Context(), args[0], deleteBranch)
		},
	}
	cleanupCmd.Flags().BoolVar(&deleteBranch, "delete-branch", false, "also delete the agent/<task> branch (discards its commits)")
	var applyCommit, applyForce bool
	apply := &cobra.Command{
		Use:   "apply TASK",
		Short: "Bring the task's changes into your checkouts (staged for you to commit; --commit commits them)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.applyTask(cmd.Context(), args[0], applyCommit, applyForce)
		},
	}
	apply.Flags().BoolVar(&applyCommit, "commit", false, "commit the changes instead of leaving them staged")
	apply.Flags().BoolVar(&applyForce, "force", false, "apply a task that is not completed (unverified changes)")
	cmd.AddCommand(create, runCmd, status, events, diff, cancel, cleanupCmd, apply)
	return cmd
}

// createTask creates a task in the workspace (--workspace, else the current
// one). The returned runner can run it when willRun is set.
func (a *App) createTask(ctx context.Context, wsFlag, request string, repos, criteria []string, fromTask string, rf runFlags, willRun bool) (*task.Task, *orchestrator.Runner, func(), error) {
	w, err := a.resolveWorkspace(ctx, wsFlag)
	if err != nil {
		return nil, nil, nil, err
	}
	if a.Config, err = a.Config.WithWorkspace(a.Paths.Config, w.Name); err != nil {
		return nil, nil, nil, err
	}
	r, cleanup, err := a.taskRunner(ctx, rf, willRun)
	if err != nil {
		return nil, nil, nil, err
	}
	if fromTask != "" {
		prev, err := r.Ledger.Get(ctx, fromTask)
		if err != nil {
			cleanup()
			return nil, nil, nil, err
		}
		fromTask = prev.ID
	}
	t, err := r.CreateFrom(ctx, w, request, repos, criteria, fromTask)
	if err != nil {
		cleanup()
		return nil, nil, nil, err
	}
	return t, r, cleanup, nil
}

// applyTaskWorkspace applies the per-workspace config override of the
// task's workspace (config.WithWorkspace).
func (a *App) applyTaskWorkspace(ctx context.Context, id string) error {
	s, err := a.Store(ctx)
	if err != nil {
		return err
	}
	t, err := task.Ledger{DB: s.DB}.Get(ctx, id)
	if err != nil {
		return err
	}
	w, err := workspace.Store{DB: s.DB}.Get(ctx, t.WorkspaceID)
	if err != nil {
		return err
	}
	a.Config, err = a.Config.WithWorkspace(a.Paths.Config, w.Name)
	return err
}

// checkWorktrees verifies task worktrees before host git runs in them.
func checkWorktrees(ctx context.Context, wts []task.Worktree) error {
	for _, w := range wts {
		common, err := gitops.CommonDir(ctx, w.RepoPath)
		if err != nil {
			return err
		}
		if err := gitops.CheckTaskWorktree(w.Path, common, w.Branch); err != nil {
			return err
		}
	}
	return nil
}

// taskRunner builds a runner; when it will not run the agent (create without
// --run) it avoids starting the model server.
func (a *App) taskRunner(ctx context.Context, f runFlags, willRun bool) (*orchestrator.Runner, func(), error) {
	if willRun {
		return a.buildRunner(ctx, f)
	}
	s, err := a.Store(ctx)
	if err != nil {
		return nil, nil, err
	}
	ws, _ := a.workspaces(ctx)
	modelName := f.model
	if a.Config.Inference.IsCloud() {
		if modelName == "" {
			modelName = a.Config.Inference.Cloud().Model
		}
	} else {
		p, err := a.profile(f.model)
		if err != nil {
			return nil, nil, err
		}
		modelName = p.Name
	}
	return &orchestrator.Runner{DB: s.DB, Ledger: task.Ledger{DB: s.DB}, WS: ws, Rec: telemetry.New(s.DB, a.Log),
		Agent: &openhands.Runtime{}, Cfg: a.Config, Paths: a.Paths, Model: modelName, Log: a.Log, Out: a.Err}, func() {}, nil
}

func runAndReport(ctx context.Context, app *App, r *orchestrator.Runner, id string, opt orchestrator.RunOptions) error {
	t0 := time.Now()
	t, err := r.Run(ctx, id, opt)
	if t != nil {
		app.printf("task %s: status=%s phase=%s verification=%s attempts=%d tokens=%d escalations=%d (%s)\n",
			t.ID, t.Status, t.Phase, t.VerificationState, t.AttemptCount, t.Budget.UsedLocalTokens, t.Budget.UsedEscalations,
			time.Since(t0).Round(time.Second))
	}
	if errors.Is(err, context.Canceled) {
		app.printf("interrupted; resume with `task resume %s`\n", id)
		return nil
	}
	return err
}

func newVerifyCmd(app *App) *cobra.Command {
	var full, unsafe bool
	cmd := &cobra.Command{
		Use: "verify TASK", Short: "Run deterministic verification on a task's worktrees", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := app.verifyTask(cmd.Context(), args[0], full, unsafe)
			if err != nil {
				return err
			}
			ok := true
			for _, r := range res {
				if app.jsonOut {
					_ = app.printJSON(r.Result)
					continue
				}
				app.printf("## %s (%s): passed=%v\n", r.Repo, r.Scope, r.Result.Passed)
				for _, st := range r.Result.Stages {
					app.printf("  %-14s %-7s %6dms %s\n", st.Name, st.Status, st.DurationMS, st.Command)
					if st.Status == "fail" || st.Status == "error" {
						app.printf("%s\n", indent(st.Output, "      "))
					}
				}
				ok = ok && r.Result.Passed
			}
			if !ok {
				return errors.New("verification failed")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&full, "full", false, "run the full merge-candidate gate")
	cmd.Flags().BoolVar(&unsafe, "unsafe-no-sandbox", false, "run verification on the host")
	return cmd
}

func newFrontierCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "frontier", Short: "Frontier escalation: review, answer, status"}
	var rf runFlags
	review := &cobra.Command{
		Use: "review TASK", Short: "Request a frontier review (Z4) and continue the task locally", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !app.Config.Frontier.Enabled {
				return errors.New("frontier is disabled (set frontier.enabled: true in config.yaml)")
			}
			if err := app.applyTaskWorkspace(cmd.Context(), args[0]); err != nil {
				return err
			}
			r, cleanup, err := app.buildRunner(cmd.Context(), rf)
			if err != nil {
				return err
			}
			defer cleanup()
			return runAndReport(cmd.Context(), app, r, args[0], orchestrator.RunOptions{UserRequestedFrontier: true})
		},
	}
	addRunFlags(review, &rf)
	answer := &cobra.Command{
		Use: "answer TASK FILE", Short: "Store a manually obtained frontier answer for the task's pending escalation", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := app.answerEscalation(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			app.printf("stored answer for escalation %d; run `task resume %s`\n", id, args[0])
			return nil
		},
	}
	status := &cobra.Command{
		Use: "status", Short: "Frontier provider status and escalation history",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			fc := app.Config.Frontier
			app.printf("enabled=%v provider=%s approval=%v max_packet_tokens=%d\n", fc.Enabled, fc.Provider, fc.RequireApproval, fc.MaxPacketTokens)
			if fc.Provider == "codex" {
				st, err := (&frontier.Codex{Binary: fc.Binary}).LoginStatus(ctx)
				app.printf("codex: %s %v\n", st, errOrEmpty(err))
			}
			sum, err := app.escalationSummary(ctx)
			if err != nil {
				return err
			}
			for _, r := range sum {
				app.printf("  %s %-16s outcome=%-9s count=%d packet_tokens=%d\n", r.Trigger, r.Status, r.Outcome, r.Count, r.PacketTokens)
			}
			return nil
		},
	}
	list := &cobra.Command{
		Use: "list [TASK]", Short: "List escalations with trigger, model, outcome and whether the advice changed the code", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := ""
			if len(args) == 1 {
				taskID = args[0]
			}
			out, err := app.escalations(cmd.Context(), taskID)
			if err != nil {
				return err
			}
			if app.jsonOut {
				return app.printJSON(out)
			}
			for _, e := range out {
				changed := "-"
				if e.AdviceChangedCode != nil {
					changed = fmt.Sprint(*e.AdviceChangedCode)
				}
				app.printf("%3d %s %s %-9s %-18s model=%s tokens=%d outcome=%s task=%s changed_code=%s  %s\n", e.ID, e.Created[:min(19, len(e.Created))],
					e.Trigger, e.Status, e.Task, orDash(e.Model), e.PacketTokens, orDash(e.Outcome), orDash(e.TaskOutcome), changed, oneLine(e.Reason, 60))
			}
			return nil
		},
	}
	cmd.AddCommand(review, answer, status, list)
	return cmd
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func newSandboxCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "sandbox", Short: "Manage the agent sandbox image"}
	var dir string
	build := &cobra.Command{
		Use: "build", Short: "Build the sandbox image locally (never pushed)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dir == "" && app.Config.Agent.AdapterDir != "" {
				dir = filepath.Dir(app.Config.Agent.AdapterDir)
			}
			// Without a checkout the build context embedded in the binary is used.
			return app.buildSandboxImage(cmd.Context(), dir, app.Err)
		},
	}
	build.Flags().StringVar(&dir, "dir", "", "directory containing the sandbox Dockerfile (default: agent.adapter_dir's parent, else the copy built into this binary)")
	cmd.AddCommand(build)
	return cmd
}

func errOrEmpty(err error) string {
	if err == nil {
		return ""
	}
	return "(" + err.Error() + ")"
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func indent(s, pfx string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > 40 {
		lines = append([]string{"…"}, lines[len(lines)-40:]...)
	}
	return pfx + strings.Join(lines, "\n"+pfx)
}

// sandboxUID and sandboxGID are the user the sandbox runs as: the invoking
// user, so files written to the worktree keep their owner. Windows has no
// uid (os.Getuid returns -1); Docker Desktop maps file ownership itself, so
// a fixed unprivileged user is used there.
func sandboxUID() int {
	if u := os.Getuid(); u >= 0 {
		return u
	}
	return 1000
}

func sandboxGID() int {
	if g := os.Getgid(); g >= 0 {
		return g
	}
	return 1000
}
