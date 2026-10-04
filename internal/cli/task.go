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
	"github.com/akynte/boundedcode/internal/orchestrator"
	"github.com/akynte/boundedcode/internal/repointel/cbm"
	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/store"
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
		PIDs: 4096, UID: os.Getuid(), GID: os.Getgid()}, nil
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
	p, err := a.profile(f.model)
	if err != nil {
		return nil, nil, err
	}
	// Inference endpoint.
	var ep inference.Endpoint
	if rt := a.inferenceRuntime(); rt != nil {
		a.printf("ensuring %s is serving %s…\n", rt.Name(), p.Name)
		if ep, err = rt.Ensure(ctx, p); err != nil {
			return nil, nil, err
		}
	} else {
		ep = inference.Endpoint{BaseURL: a.Config.Inference.ExternalURL, Model: p.Name}
	}
	sb, err := a.sandbox(f.unsafeNoSandbox)
	if err != nil {
		return nil, nil, err
	}
	argv, err := a.adapterArgv(sb)
	if err != nil {
		return nil, nil, err
	}
	timeout := a.Config.Inference.RequestTimeout.D()
	newGW := func(taskID string, maxTokens int) *inference.Gateway {
		return &inference.Gateway{Client: inference.NewClient(ep.BaseURL, timeout), Model: ep.Model, DB: db,
			TaskID: taskID, Source: "agent", MaxTokens: maxTokens}
	}
	var rt agent.Runtime
	switch a.Config.Agent.Runtime {
	case "openhands":
		rt = &openhands.Runtime{Sandbox: sb, Argv: argv, Log: a.Log, LogDir: filepath.Join(paths.State, "adapter"),
			Gateway: func(taskID string) *inference.Gateway { return newGW(taskID, 0) }}
	default:
		return nil, nil, fmt.Errorf("agent runtime %q cannot run tasks from the CLI", a.Config.Agent.Runtime)
	}
	intel := &cbm.Client{Binary: a.Config.RepoIntel.Binary, CacheDir: filepath.Join(paths.Cache, "codebase-memory")}
	cleanup := func() { _ = intel.Close() }
	if err := intel.Open(ctx); err != nil {
		a.Log.Warn("repository intelligence unavailable; continuing without graph context", "err", err)
		cleanup = func() {}
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
		DB: db, Ledger: task.Ledger{DB: db}, Rec: rec, Agent: rt, Intel: intel,
		Verify: &verify.Engine{Sandbox: sb, CacheDir: filepath.Join(paths.Cache, "build"), GoModCache: strings.TrimSpace(string(gomodcache)), DB: db, Rec: rec},
		Cfg:    a.Config, Paths: paths, Model: p.Name, CtxSize: p.Server.CtxSize, Log: a.Log, Out: a.Err,
		CondenseEachRetry: f.condenseRetry,
		CrossService:      a.Config.RepoIntel.CrossService,
	}
	if nav != nil {
		r.Nav = nav // assigned only when usable: a nil *Navigator is a non-nil interface
	}
	r.WS = workspace.Store{DB: db}
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
				cx.Container = &frontier.CodexContainer{Engine: a.Config.Sandbox.Engine, Image: a.Config.Agent.Image, UID: os.Getuid(), GID: os.Getgid()}
			}
			r.Frontier = cx
		case "manual":
			r.Frontier = frontier.Manual{}
		}
	}
	r.Approve = func(_ context.Context, tr frontier.Trigger, packetPath string, tokens int) bool {
		if f.approveFrontier {
			return true
		}
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Fprintf(a.Err, "frontier escalation %s needs approval (non-interactive: declined; use --approve-frontier)\n", tr.Code)
			return false
		}
		fmt.Fprintf(a.Err, "\nFrontier escalation %s: %s\nPacket (%d tokens): %s\nSend to %s? [y/N] ", tr.Code, tr.Reason, tokens, packetPath, a.Config.Frontier.Provider)
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		return strings.EqualFold(strings.TrimSpace(line), "y")
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
		rf       runFlags
	)
	create := &cobra.Command{
		Use:   "create REQUEST",
		Short: "Create a task (isolated worktrees on agent/<task-id>); --run starts it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w, err := app.resolveWorkspace(ctx, wsFlag)
			if err != nil {
				return err
			}
			r, cleanup, err := app.taskRunner(ctx, rf, run)
			if err != nil {
				return err
			}
			defer cleanup()
			t, err := r.Create(ctx, w, args[0], repos, criteria)
			if err != nil {
				return err
			}
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
	addRunFlags(create, &rf)

	var rf2 runFlags
	runCmd := &cobra.Command{
		Use:     "run TASK",
		Aliases: []string{"resume"},
		Short:   "Run or resume a task until it completes, blocks or is interrupted",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, cleanup, err := app.taskRunner(cmd.Context(), rf2, true)
			if err != nil {
				return err
			}
			defer cleanup()
			return runAndReport(cmd.Context(), app, r, args[0], orchestrator.RunOptions{})
		},
	}
	addRunFlags(runCmd, &rf2)

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
			ctx := cmd.Context()
			s, err := app.Store(ctx)
			if err != nil {
				return err
			}
			l := task.Ledger{DB: s.DB}
			t, err := l.Get(ctx, args[0])
			if err != nil {
				return err
			}
			wts, _ := l.Worktrees(ctx, t.ID)
			for _, w := range wts {
				d, err := gitops.Diff(ctx, w.Path, w.BaseCommit, false)
				if err != nil {
					return err
				}
				if d != "" {
					app.printf("### %s\n%s\n", w.RepoName, d)
				}
			}
			return nil
		},
	}
	cancel := &cobra.Command{
		Use: "cancel TASK", Short: "Cancel a task (worktrees and branch are kept)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s, err := app.Store(ctx)
			if err != nil {
				return err
			}
			l := task.Ledger{DB: s.DB}
			t, err := l.Get(ctx, args[0])
			if err != nil {
				return err
			}
			t.Status, t.FinishedAt = task.StatusCancelled, store.Now()
			t.Decide("user", "cancelled")
			if err := l.Save(ctx, t); err != nil {
				return err
			}
			orchestrator.FinishEscalations(ctx, s.DB, t.ID, "no_effect")
			app.printf("cancelled %s; branch %s kept\n", t.ID, gitops.TaskBranch(t.ID))
			return nil
		},
	}
	cmd.AddCommand(create, runCmd, status, events, diff, cancel)
	return cmd
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
	p, err := a.profile(f.model)
	if err != nil {
		return nil, nil, err
	}
	return &orchestrator.Runner{DB: s.DB, Ledger: task.Ledger{DB: s.DB}, WS: ws, Rec: telemetry.New(s.DB, a.Log),
		Agent: &openhands.Runtime{}, Cfg: a.Config, Paths: a.Paths, Model: p.Name, Log: a.Log, Out: a.Err}, func() {}, nil
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
			ctx := cmd.Context()
			s, err := app.Store(ctx)
			if err != nil {
				return err
			}
			l := task.Ledger{DB: s.DB}
			t, err := l.Get(ctx, args[0])
			if err != nil {
				return err
			}
			sb, err := app.sandbox(unsafe)
			if err != nil {
				return err
			}
			gomodcache, _ := exec.CommandContext(ctx, "go", "env", "GOMODCACHE").Output()
			e := &verify.Engine{Sandbox: sb, CacheDir: filepath.Join(app.Paths.Cache, "build"), GoModCache: strings.TrimSpace(string(gomodcache)),
				DB: s.DB, Rec: telemetry.New(s.DB, app.Log)}
			scope := verify.Targeted
			if full {
				scope = verify.Full
			}
			wts, _ := l.Worktrees(ctx, t.ID)
			ok := true
			for _, w := range wts {
				res, err := e.Run(ctx, verify.RepoTarget{Name: w.RepoName, Worktree: w.Path, Base: w.BaseCommit, TaskID: t.ID}, scope)
				if err != nil {
					return err
				}
				if app.jsonOut {
					_ = app.printJSON(res)
					continue
				}
				app.printf("## %s (%s): passed=%v\n", w.RepoName, scope, res.Passed)
				for _, st := range res.Stages {
					app.printf("  %-14s %-7s %6dms %s\n", st.Name, st.Status, st.DurationMS, st.Command)
					if st.Status == "fail" || st.Status == "error" {
						app.printf("%s\n", indent(st.Output, "      "))
					}
				}
				ok = ok && res.Passed
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
			ctx := cmd.Context()
			s, err := app.Store(ctx)
			if err != nil {
				return err
			}
			t, err := task.Ledger{DB: s.DB}.Get(ctx, args[0])
			if err != nil {
				return err
			}
			b, err := os.ReadFile(args[1])
			if err != nil {
				return err
			}
			id, err := orchestrator.AnswerEscalation(ctx, s.DB, app.Paths.TaskDir(t.ID), t.ID, b)
			if err != nil {
				return err
			}
			app.printf("stored answer for escalation %d; run `task resume %s`\n", id, t.ID)
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
			s, err := app.Store(ctx)
			if err != nil {
				return err
			}
			rows, err := s.DB.QueryContext(ctx, `SELECT trigger, status, outcome, COUNT(*), COALESCE(SUM(packet_tokens),0) FROM escalations GROUP BY 1,2,3 ORDER BY 1,2`)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var trig, st, out string
				var n, tok int
				if err := rows.Scan(&trig, &st, &out, &n, &tok); err != nil {
					return err
				}
				app.printf("  %s %-16s outcome=%-9s count=%d packet_tokens=%d\n", trig, st, out, n, tok)
			}
			return rows.Err()
		},
	}
	cmd.AddCommand(review, answer, status)
	return cmd
}

func newSandboxCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "sandbox", Short: "Manage the agent sandbox image"}
	var dir string
	build := &cobra.Command{
		Use: "build", Short: "Build the sandbox image locally (never pushed)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dir == "" {
				if app.Config.Agent.AdapterDir == "" {
					return errors.New("pass --dir adapters/openhands (or set agent.adapter_dir)")
				}
				dir = filepath.Dir(app.Config.Agent.AdapterDir)
			}
			c := exec.CommandContext(cmd.Context(), app.Config.Sandbox.Engine, "build", "-t", app.Config.Agent.Image, dir)
			c.Stdout, c.Stderr = app.Err, app.Err
			return c.Run()
		},
	}
	build.Flags().StringVar(&dir, "dir", "", "directory containing the sandbox Dockerfile (adapters/openhands)")
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
