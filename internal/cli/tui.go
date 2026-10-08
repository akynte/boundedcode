package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/akynte/boundedcode/internal/buildinfo"
	"github.com/akynte/boundedcode/internal/compat"
	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/frontier"
	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/model"
	"github.com/akynte/boundedcode/internal/stats"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/telemetry"
	"github.com/akynte/boundedcode/internal/tui"
	"github.com/akynte/boundedcode/internal/verify"
	"github.com/akynte/boundedcode/internal/workspace"
)

func newTUICmd(app *App) *cobra.Command {
	var taskID, dir string
	cmd := &cobra.Command{
		Use:     "tui",
		Aliases: []string{"ui"},
		Short:   "Open the interactive terminal interface (the default when run without arguments)",
		Long: `Opens a full-screen terminal interface. It starts in a chat for the git
repository in the current directory (registered and indexed on first use):
each message becomes a task, or steers the current one. It also covers
everything else the CLI does: tasks
with live progress, diffs and verification, workspaces and indexing,
repository intelligence, the inference runtime, frontier escalations,
statistics, environment checks, and a console for any other command.

Actions run the same commands as the CLI, in-process; frontier approvals and
other confirmations appear as dialogs. Logs go to <state dir>/tui.log.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
				return errors.New("the terminal interface needs an interactive terminal (use the CLI commands in scripts)")
			}
			be, closeBE, err := newTUIBackend(cmd.Context(), app)
			if err != nil {
				return err
			}
			defer closeBE()
			if dir == "" {
				if dir, err = os.Getwd(); err != nil {
					return err
				}
			}
			if dir, err = filepath.Abs(dir); err != nil {
				return err
			}
			return tui.Run(cmd.Context(), be, tui.Options{Task: taskID, Dir: dir})
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "open this task on start")
	cmd.Flags().StringVarP(&dir, "dir", "C", "", "work in this directory (default: the current directory)")
	return cmd
}

// tuiBackend implements tui.Backend on top of App. Every call that depends
// on configuration loads it afresh, so changes made by actions (init,
// workspace use) or by hand are picked up without restarting.
type tuiBackend struct {
	base    *App
	log     *slog.Logger
	logPath string

	mu       sync.Mutex
	prompter tui.Prompter
}

func newTUIBackend(ctx context.Context, app *App) (*tuiBackend, func(), error) {
	if err := app.Paths.Ensure(); err != nil {
		return nil, nil, err
	}
	logPath := filepath.Join(app.Paths.State, "tui.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, err
	}
	// Nothing may write to the terminal while the interface owns it.
	log := slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo}))
	app.Log, app.Out, app.Err = log, io.Discard, io.Discard
	if _, err := app.Store(ctx); err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return &tuiBackend{base: app, log: log, logPath: logPath}, func() { _ = f.Close() }, nil
}

// fork returns an App with freshly loaded configuration that shares the
// state database and writes to out.
func (b *tuiBackend) fork(out io.Writer) (*App, error) {
	a := &App{Out: out, Err: out, Log: b.log, st: b.base.st}
	b.mu.Lock()
	p := b.prompter
	b.mu.Unlock()
	if p != nil {
		a.approve = func(ctx context.Context, tr frontier.Trigger, packetPath string, tokens int) bool {
			return p(ctx, "Frontier escalation "+string(tr.Code),
				fmt.Sprintf("%s\n\nThe packet (%d tokens) leaves this machine and is sent to %s:\n%s\n\nSend it?", tr.Reason, tokens, a.Config.Frontier.Provider, packetPath))
		}
		a.confirmFn = func(prompt string) bool {
			body := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(prompt), "[y/N]"))
			return p(context.Background(), "Confirm", body)
		}
	}
	return a, a.load()
}

func (b *tuiBackend) SetPrompter(p tui.Prompter) {
	b.mu.Lock()
	b.prompter = p
	b.mu.Unlock()
}

func (b *tuiBackend) Exec(ctx context.Context, args []string, out io.Writer) error {
	a := &App{Out: out, Err: out, st: b.base.st}
	if f, err := b.fork(out); err == nil {
		a.approve, a.confirmFn = f.approve, f.confirmFn
	}
	root := newRoot(a)
	root.SetArgs(args)
	root.SetOut(out)
	root.SetErr(out)
	root.SetIn(strings.NewReader(""))
	b.log.Info("tui exec", "args", strings.Join(args, " "))
	err := root.ExecuteContext(ctx)
	if err != nil {
		b.log.Info("tui exec failed", "args", strings.Join(args, " "), "err", err)
	}
	return err
}

func (b *tuiBackend) Commands() []tui.CommandInfo {
	var out []tui.CommandInfo
	var walk func(c *cobra.Command, prefix string)
	walk = func(c *cobra.Command, prefix string) {
		for _, sub := range c.Commands() {
			name := sub.Name()
			if sub.Hidden || name == "help" || name == "completion" || name == "tui" {
				continue
			}
			path := strings.TrimSpace(prefix + " " + name)
			if sub.Runnable() {
				out = append(out, tui.CommandInfo{Path: path, Use: strings.TrimSpace(prefix + " " + sub.Use), Short: sub.Short})
			}
			walk(sub, path)
		}
	}
	walk(newRoot(&App{}), "")
	return out
}

func (b *tuiBackend) Info(ctx context.Context) tui.Info {
	in := tui.Info{Name: buildinfo.Name, Version: buildinfo.Version, Commit: buildinfo.Commit(), LogFile: b.logPath}
	a, err := b.fork(io.Discard)
	if err != nil {
		in.Err = err.Error()
		a = b.base
	}
	c := a.Config
	in.ConfigFile = a.configFile()
	_, statErr := os.Stat(in.ConfigFile)
	in.ConfigExists = statErr == nil
	in.StateDB = a.Paths.StateDB()
	in.DefaultModel = c.DefaultModel
	in.Provider = c.Inference.Provider
	if c.Inference.IsCloud() {
		in.ProviderModel = c.Inference.Cloud().Model
	}
	in.InferenceMode, in.ExternalURL = c.Inference.Mode, c.Inference.ExternalURL
	in.SandboxKind, in.AgentImage, in.AdapterDir = c.Sandbox.Kind, c.Agent.Image, c.Agent.AdapterDir
	in.FrontierEnabled, in.FrontierProvider = c.Frontier.Enabled, c.Frontier.Provider
	in.FrontierApproval, in.FrontierMaxPacket = c.Frontier.RequireApproval, c.Frontier.MaxPacketTokens
	in.SerenaEnabled, in.CrossService = c.RepoIntel.Serena.Enabled, c.RepoIntel.CrossService
	if cur, err := os.ReadFile(a.currentWorkspaceFile()); err == nil {
		in.CurrentWorkspace = strings.TrimSpace(string(cur))
	} else if all, err := (workspace.Store{DB: b.base.st.DB}).List(ctx); err == nil && len(all) == 1 {
		in.CurrentWorkspace = all[0].Name
	}
	return in
}

func (b *tuiBackend) Workspaces(ctx context.Context) ([]tui.Workspace, error) {
	ws := workspace.Store{DB: b.base.st.DB}
	all, err := ws.List(ctx)
	if err != nil {
		return nil, err
	}
	cur := ""
	if c, err := os.ReadFile(b.base.currentWorkspaceFile()); err == nil {
		cur = strings.TrimSpace(string(c))
	} else if len(all) == 1 {
		cur = all[0].Name
	}
	out := make([]tui.Workspace, 0, len(all))
	for _, w := range all {
		repos, err := ws.AllRepos(ctx, w.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, tui.Workspace{Workspace: w, Current: w.Name == cur, Repos: repos})
	}
	return out, nil
}

func (b *tuiBackend) Tasks(ctx context.Context, limit int) ([]*task.Task, error) {
	return task.Ledger{DB: b.base.st.DB}.List(ctx, "", limit)
}

func (b *tuiBackend) Task(ctx context.Context, id string) (tui.TaskDetail, error) {
	d, err := b.base.taskDetail(ctx, id)
	if err != nil {
		return tui.TaskDetail{}, err
	}
	return tui.TaskDetail{Task: d.Task, Strategies: d.Strategies, Worktrees: d.Worktrees, LeaseOwner: d.Lease.Owner}, nil
}

func (b *tuiBackend) Events(ctx context.Context, taskID string, after int64, limit int) ([]telemetry.Event, error) {
	return telemetry.Events(ctx, b.base.st.DB, taskID, after, limit)
}

func (b *tuiBackend) Compat(ctx context.Context, taskID string) (compat.Report, bool, error) {
	wts, err := task.Ledger{DB: b.base.st.DB}.Worktrees(ctx, taskID)
	if err != nil {
		return compat.Report{}, false, err
	}
	return taskCompat(ctx, b.base.st.DB, taskID, wts)
}

func (b *tuiBackend) Verifications(ctx context.Context, taskID string, limit int) ([]verify.Result, error) {
	return verify.LoadRuns(ctx, b.base.st.DB, taskID, limit)
}

func (b *tuiBackend) Diffs(ctx context.Context, taskID string) ([]tui.RepoDiff, error) {
	ds, err := b.base.taskDiffs(ctx, taskID)
	out := make([]tui.RepoDiff, 0, len(ds))
	for _, d := range ds {
		out = append(out, tui.RepoDiff{Repo: d.Repo, Diff: d.Diff})
	}
	return out, err
}

func (b *tuiBackend) Escalations(ctx context.Context, taskID string) ([]tui.Escalation, error) {
	es, err := b.base.escalations(ctx, taskID)
	out := make([]tui.Escalation, 0, len(es))
	for _, e := range es {
		out = append(out, tui.Escalation{ID: e.ID, Task: e.Task, Trigger: e.Trigger, Provider: e.Provider, Model: e.Model, Status: e.Status,
			Outcome: e.Outcome, TaskOutcome: e.TaskOutcome, Created: e.Created, Reason: e.Reason, PacketTokens: e.PacketTokens,
			AdviceChangedCode: e.AdviceChangedCode})
	}
	return out, err
}

func (b *tuiBackend) EscalationSummary(ctx context.Context) ([]tui.EscalationGroup, error) {
	gs, err := b.base.escalationSummary(ctx)
	out := make([]tui.EscalationGroup, 0, len(gs))
	for _, g := range gs {
		out = append(out, tui.EscalationGroup(g))
	}
	return out, err
}

func (b *tuiBackend) FrontierLogin(ctx context.Context) (string, error) {
	a, err := b.fork(io.Discard)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return (&frontier.Codex{Binary: a.Config.Frontier.Binary}).LoginStatus(ctx)
}

func (b *tuiBackend) Stats(ctx context.Context, since time.Duration) (stats.Summary, error) {
	from := ""
	if since > 0 {
		from = time.Now().UTC().Add(-since).Format(time.RFC3339Nano)
	}
	return b.base.computeStats(ctx, b.base.st.DB, from)
}

func convertChecks(cs []check) []tui.Check {
	out := make([]tui.Check, 0, len(cs))
	for _, c := range cs {
		out = append(out, tui.Check{Name: c.Name, Status: string(c.Status), Detail: c.Detail, Hint: c.Hint})
	}
	return out
}

func (b *tuiBackend) Doctor(ctx context.Context) []tui.Check {
	a, err := b.fork(io.Discard)
	if err != nil {
		return []tui.Check{{Name: "config", Status: string(statusFail), Detail: err.Error()}}
	}
	return convertChecks(runDoctor(ctx, a))
}

func (b *tuiBackend) SerenaChecks(ctx context.Context) []tui.Check {
	a, err := b.fork(io.Discard)
	if err != nil {
		return []tui.Check{{Name: "config", Status: string(statusFail), Detail: err.Error()}}
	}
	return convertChecks(serenaChecks(ctx, a, true))
}

func (b *tuiBackend) Runtime(ctx context.Context) (inference.Status, error) {
	a, err := b.fork(io.Discard)
	if err != nil {
		return inference.Status{}, err
	}
	return a.runtimeStatus(ctx)
}

func (b *tuiBackend) RuntimeLog(ctx context.Context, lines int) (string, []string, error) {
	a, err := b.fork(io.Discard)
	if err != nil {
		return "", nil, err
	}
	return a.runtimeLog(lines)
}

func (b *tuiBackend) Models(ctx context.Context) ([]tui.ModelRow, error) {
	a, err := b.fork(io.Discard)
	if err != nil {
		return nil, err
	}
	var out []tui.ModelRow
	for _, r := range a.modelRows() {
		out = append(out, tui.ModelRow{Name: r.Name, Display: r.Display, File: r.File, License: r.License, Present: r.Present, Default: r.Default,
			Description: r.Description, Status: r.Status, SizeBytes: r.SizeBytes, Fit: r.Fit, FitDetail: r.FitDetail, Recommended: r.Recommended})
	}
	return out, nil
}

func (b *tuiBackend) CreateTask(ctx context.Context, req tui.CreateTaskRequest) (string, error) {
	a, err := b.fork(io.Discard)
	if err != nil {
		return "", err
	}
	t, _, cleanup, err := a.createTask(ctx, req.Workspace, req.Request, req.Repos, req.Criteria, req.FromTask, runFlags{model: req.Model}, false)
	if err != nil {
		return "", err
	}
	cleanup()
	return t.ID, nil
}

var nonName = regexp.MustCompile(`[^a-z0-9._-]+`)

func (b *tuiBackend) Project(ctx context.Context, dir string) (tui.Project, error) {
	p := tui.Project{Dir: dir}
	if _, err := gitops.Run(ctx, dir, "rev-parse", "--show-toplevel"); err != nil {
		return p, nil //nolint:nilerr // not inside a git work tree: the chat offers git init
	}
	info, err := gitops.Inspect(ctx, dir)
	if err != nil {
		return p, err
	}
	p.Root, p.Branch, p.Dirty = info.Root, info.Branch, info.Dirty
	ws := workspace.Store{DB: b.base.st.DB}
	all, err := ws.List(ctx)
	if err != nil {
		return p, err
	}
	for _, w := range all {
		repos, err := ws.AllRepos(ctx, w.ID)
		if err != nil {
			return p, err
		}
		for _, r := range repos {
			if r.Path == info.Root {
				p.Workspace, p.Repo, p.Indexed = w.Name, r.Name, r.IndexedAt != ""
				return p, nil
			}
		}
	}
	// First use of this repository: a workspace named after it.
	base := strings.Trim(nonName.ReplaceAllString(strings.ToLower(filepath.Base(info.Root)), "-"), "-._")
	if base == "" {
		base = "project"
	}
	if len(base) > 50 {
		base = base[:50]
	}
	name := base
	taken := map[string]bool{}
	for _, w := range all {
		taken[w.Name] = true
	}
	for i := 2; taken[name]; i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	w, err := ws.Create(ctx, name)
	if err != nil {
		return p, err
	}
	r, err := ws.AddRepo(ctx, w, info.Root, "")
	if err != nil {
		return p, err
	}
	rec := telemetry.New(b.base.st.DB, b.log)
	rec.Emit(ctx, "", "workspace.created", w)
	rec.Emit(ctx, "", "workspace.repo_added", map[string]any{"workspace": w.Name, "repo": r.Name, "path": r.Path})
	p.Workspace, p.Repo, p.Created = w.Name, r.Name, true
	return p, nil
}

func (b *tuiBackend) GitInit(ctx context.Context, dir string) error {
	// Check the identity first: without it the initial commit fails and
	// would leave a repository without commits behind.
	if _, err := gitops.Run(ctx, dir, "var", "GIT_COMMITTER_IDENT"); err != nil {
		return errors.New("git needs your name and email first: git config --global user.name \"Your Name\" && git config --global user.email you@example.com")
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"commit", "-q", "--allow-empty", "-m", "Initial commit"}} {
		if _, err := gitops.Run(ctx, dir, args...); err != nil {
			if strings.Contains(err.Error(), "tell me who you are") || strings.Contains(err.Error(), "identity") {
				return errors.New("git needs your name and email first: git config --global user.name \"Your Name\" && git config --global user.email you@example.com")
			}
			return err
		}
	}
	return nil
}

func (b *tuiBackend) Setup(ctx context.Context) []tui.SetupStep {
	a, err := b.fork(io.Discard)
	if err != nil {
		return []tui.SetupStep{{Name: "config", Title: "Configuration", Detail: err.Error()}}
	}
	var out []tui.SetupStep
	for _, s := range a.checkSetup(ctx) {
		out = append(out, tui.SetupStep{Name: s.Name, Title: s.Title, Detail: s.Detail, OK: s.OK})
	}
	return out
}

func (b *tuiBackend) Hardware(ctx context.Context) tui.HardwareInfo {
	a, err := b.fork(io.Discard)
	if err != nil {
		return tui.HardwareInfo{Summary: err.Error()}
	}
	snap := a.hardware(ctx)
	rec := model.Recommend(a.Models, a.Config.DefaultModel, snap)
	return tui.HardwareInfo{Summary: describeHardware(snap), Recommended: rec.Best.Profile, Reason: rec.Reason}
}

func (b *tuiBackend) Providers(context.Context) []tui.ProviderRow {
	a, err := b.fork(io.Discard)
	if err != nil {
		return nil
	}
	var out []tui.ProviderRow
	for _, r := range a.providerRows() {
		out = append(out, tui.ProviderRow{Name: r.Name, Model: r.Model, BaseURL: r.BaseURL, Selected: r.Selected, KeySource: r.KeySource})
	}
	return out
}

func (b *tuiBackend) ProviderModels(ctx context.Context, provider, baseURL string) ([]tui.ProviderModel, error) {
	a, err := b.fork(io.Discard)
	if err != nil {
		return nil, err
	}
	if baseURL != "" {
		if a.Config.Inference.Providers == nil {
			a.Config.Inference.Providers = map[string]config.ProviderConfig{}
		}
		p := a.Config.Inference.Providers[provider]
		p.BaseURL = strings.TrimRight(baseURL, "/")
		a.Config.Inference.Providers[provider] = p
	}
	list, err := a.providerModels(ctx, provider)
	if err != nil {
		return nil, err
	}
	out := make([]tui.ProviderModel, 0, len(list))
	for _, m := range list {
		out = append(out, tui.ProviderModel{ID: m.ID, Display: m.DisplayName, ContextWindow: m.ContextWindow})
	}
	return out, nil
}

func (b *tuiBackend) SetProviderKey(_ context.Context, provider, key string) (string, error) {
	a, err := b.fork(io.Discard)
	if err != nil {
		return "", err
	}
	name, err := normalizeProvider(provider)
	if err != nil {
		return "", err
	}
	src, err := a.secretStore().Set(name, key)
	if err != nil {
		return "", err
	}
	return describeSource(src, a), nil
}

func (b *tuiBackend) DeleteProviderKey(_ context.Context, provider string) error {
	a, err := b.fork(io.Discard)
	if err != nil {
		return err
	}
	return a.secretStore().Delete(provider)
}

func (b *tuiBackend) TestProvider(ctx context.Context) (string, error) {
	a, err := b.fork(io.Discard)
	if err != nil {
		return "", err
	}
	res, err := a.testProvider(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s %s answered in %s (%d prompt + %d output tokens)", res.Provider, res.Model, res.Latency, res.Prompt, res.Output), nil
}
