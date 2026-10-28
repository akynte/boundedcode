package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/akynte/boundedcode/internal/execbox"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/akynte/boundedcode/internal/buildinfo"
	"github.com/akynte/boundedcode/internal/direct"
	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/orchestrator"
	"github.com/akynte/boundedcode/internal/telemetry"
)

// sessionsDir holds direct-mode session files.
func (a *App) sessionsDir() string { return filepath.Join(a.Paths.Data, "sessions") }

// userInstructionsFile is the user's own instruction file for direct mode.
func (a *App) userInstructionsFile() string { return filepath.Join(a.Paths.Config, "AGENTS.md") }

// directEndpoint resolves the model for direct mode: the selected cloud
// provider, the managed llama.cpp server (started when needed) or the
// external server.
type directEndpoint struct {
	provider, model string
	ctxSize         int
	newGateway      func(sessionID string) *inference.Gateway
	// pin identifies the model for benchmark reports; tokenize counts
	// tokens with the local server's tokenizer (nil for a cloud provider).
	pin      map[string]any
	tokenize func(context.Context, string) (int, error)
}

func (a *App) directEndpoint(ctx context.Context, modelOverride string) (*directEndpoint, error) {
	s, err := a.Store(ctx)
	if err != nil {
		return nil, err
	}
	db := s.DB
	timeout := a.Config.Inference.RequestTimeout.D()
	maxTokens := a.Config.Direct.MaxTokens
	if a.Config.Inference.IsCloud() {
		cloud, err := a.prepareCloud(ctx, modelOverride)
		if err != nil {
			return nil, err
		}
		return &directEndpoint{provider: cloud.provider, model: cloud.model, ctxSize: cloud.ctxSize,
			pin: map[string]any{"provider": cloud.provider, "model": cloud.model, "context_tokens": cloud.ctxSize},
			newGateway: func(id string) *inference.Gateway {
				return &inference.Gateway{Model: cloud.model, Upstream: cloud.upstream(filepath.Join(a.sessionsDir(), ".replay", id)), DB: db, TaskID: id, Source: "direct", MaxTokens: maxTokens}
			}}, nil
	}
	p, err := a.profile(modelOverride)
	if err != nil {
		return nil, err
	}
	var ep inference.Endpoint
	if rt := a.inferenceRuntime(); rt != nil {
		if ep, err = rt.Ensure(ctx, p); err != nil {
			return nil, err
		}
	} else {
		ep = inference.Endpoint{BaseURL: a.Config.Inference.ExternalURL, Model: p.Name}
		if err := a.checkExternalInference(ctx); err != nil {
			return nil, err
		}
	}
	return &directEndpoint{provider: inference.ProviderLocal, model: ep.Model, ctxSize: p.Server.CtxSize,
		pin: modelPin(p), tokenize: inference.NewClient(ep.BaseURL, timeout).Tokenize,
		newGateway: func(id string) *inference.Gateway {
			return &inference.Gateway{Model: ep.Model, Client: inference.NewClient(ep.BaseURL, timeout), DB: db, TaskID: id, Source: "direct", MaxTokens: maxTokens}
		}}, nil
}

// directRunner wires a direct-mode runner for dir, on a new session or a
// stored one (sessionID "" = new; "last" = the most recent).
func (a *App) directRunner(ctx context.Context, dir, sessionID, modelOverride, approval string) (*direct.Runner, error) {
	root, err := projectRoot(dir)
	if err != nil {
		return nil, err
	}
	if dir == "" {
		dir = "."
	}
	cwd, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if approval == "" {
		approval = a.Config.Direct.Approval
	}
	switch approval {
	case direct.ApprovalAsk, direct.ApprovalEdits, direct.ApprovalAuto:
	default:
		return nil, fmt.Errorf("approval %q is not ask|edits|auto", approval)
	}
	ep, err := a.directEndpoint(ctx, modelOverride)
	if err != nil {
		return nil, err
	}
	var sess *direct.Session
	if sessionID == "" {
		if sess, err = direct.NewSession(a.sessionsDir(), root, ep.provider, ep.model); err != nil {
			return nil, err
		}
	} else {
		path, err := direct.FindSession(a.sessionsDir(), root, sessionID)
		if err != nil {
			return nil, err
		}
		if sess, err = direct.LoadSession(path); err != nil {
			return nil, err
		}
	}
	rec := telemetry.New(a.st.DB, a.Log)
	r := &direct.Runner{Gateway: ep.newGateway(sess.ID), Session: sess, Opts: a.directOptions(root, cwd, approval, ep.ctxSize)}
	if nav, _ := a.serenaNavigator(ctx, a.Paths); nav != nil {
		r.Nav = nav
	}
	// The audit log records what ran, never file contents or outputs.
	r.Sink = func(e direct.Event) {
		switch e.Kind {
		case direct.EventToolStart:
			rec.Emit(ctx, sess.ID, "direct.tool", map[string]any{"tool": e.Tool, "summary": telemetry.Redact(e.Summary)})
		case direct.EventDenied:
			rec.Emit(ctx, sess.ID, "direct.denied", map[string]any{"tool": e.Tool, "summary": telemetry.Redact(e.Summary), "reason": e.Err})
		case direct.EventGuard:
			// Subjects are hosts, packages, paths and git keys: redacted
			// like summaries, never contents.
			rec.Emit(ctx, sess.ID, "direct.guard", map[string]any{"tool": e.Tool, "category": e.Args["category"],
				"subject": telemetry.Redact(fmt.Sprint(e.Args["subject"])), "decision": e.Args["decision"], "by": e.Args["by"]})
		case direct.EventSandbox:
			rec.Emit(ctx, sess.ID, "direct.sandbox", e.Args)
		case direct.EventCompact:
			rec.Emit(ctx, sess.ID, "direct.compact", nil)
		}
	}
	return r, nil
}

// directOptions are the runner options for a checkout at root with working
// directory dir, from the configuration: the project's and the user's
// instruction files, skills, agents, commands, hooks, MCP servers and
// checkpoints.
func (a *App) directOptions(root, dir, approval string, ctxSize int) direct.Options {
	dc := a.Config.Direct
	opts := direct.Options{
		Root: root, Dir: dir, Approval: approval, MaxTurns: dc.MaxTurns,
		CommandTimeout: dc.CommandTimeout.D(), ContextTokens: ctxSize,
		WorkingSetTokens: dc.WorkingSetTokens, MaxToolOutput: dc.MaxToolOutput, NoChecks: !dc.ChecksOn(), NoSeedPack: !dc.SeedPackOn(), Retrieval: dc.Retrieval, NoHistoryNotes: !dc.HistoryNotesOn(), NoStream: !dc.StreamOn(),
		MaxOutputTokens: a.Config.Agent.MaxOutputTokens, Instructions: dc.Instructions,
		UserInstructions: a.userInstructionsFile(),
		Web:              direct.WebOptions{AllowHosts: dc.Web.AllowHosts, SearchURL: dc.Web.SearchURL},
		Permissions:      direct.Rules{Allow: dc.Permissions.Allow, Deny: dc.Permissions.Deny},
		VerifyFallback:   dc.VerifyFallback,
		MemoryFile:       a.userInstructionsFile(),
		SkillDirs: []string{filepath.Join(root, ".boundedcode", "skills"), filepath.Join(root, ".agents", "skills"), filepath.Join(root, ".claude", "skills"),
			filepath.Join(a.Paths.Config, "skills")},
		AgentDirs: []string{filepath.Join(root, ".boundedcode", "agents"), filepath.Join(root, ".claude", "agents"), filepath.Join(a.Paths.Config, "agents")},
		UserDir:   a.Paths.Config,
	}
	for _, h := range dc.Hooks {
		opts.Hooks = append(opts.Hooks, direct.Hook{Event: h.Event, Matcher: h.Matcher, Command: h.Command, Timeout: h.Timeout.D()})
	}
	opts.MCP = a.mcpServers(root)
	opts.Sandbox, opts.SandboxFallback = a.sandboxBox(root), dc.Sandbox.Fallback
	opts.MCPTrust = &direct.MCPTrust{Path: filepath.Join(a.Paths.Data, "mcp-trust.json")}
	if dc.CheckpointsOn() {
		opts.CheckpointDir = filepath.Join(a.sessionsDir(), ".checkpoints", direct.ProjectKey(root))
	}
	return opts
}

// sandboxBox is the containment for one session's commands, with its
// /tmp and proxy sockets in a fresh directory under the data directory.
func (a *App) sandboxBox(root string) *execbox.Box {
	sc := a.Config.Direct.Sandbox
	base := filepath.Join(a.Paths.Data, "sandbox")
	state := ""
	if err := os.MkdirAll(base, 0o700); err == nil {
		state, _ = os.MkdirTemp(base, "s-")
	}
	return execbox.New(root, execbox.Config{Mode: sc.Mode, Backend: sc.Backend, Network: sc.Network, AllowHosts: sc.AllowHosts,
		Registries: sc.RegistriesOn(), ReadPaths: sc.ReadPaths, WritePaths: sc.WritePaths, EnvAllow: sc.Env, StateDir: state})
}

// applyVerifyFallback tells a runner what verification it could not have:
// a change that deserved it, or a risky path, is then decided by
// direct.verify_fallback instead of being made directly (fail closed).
func applyVerifyFallback(o *direct.Options, d direct.Decision, caps direct.Capabilities) {
	if !caps.BoundedAvailable {
		o.VerifyUnavailable = caps.BoundedReason
		if o.VerifyUnavailable == "" {
			o.VerifyUnavailable = "bounded mode is not available here"
		}
	}
	if d.Fallback == direct.ModeVerify {
		o.VerifyDeserved = d.Reason
	}
}

// riskRules builds the router's risk rules from the escalation keyword
// lists, so direct mode and frontier escalation agree on what is risky.
func (a *App) riskRules() direct.RiskRules {
	var kw []string
	kw = append(kw, a.Config.Escalation.ArchitecturalRisk...)
	kw = append(kw, a.Config.Escalation.HighRiskReview...)
	return direct.RiskRules{Keywords: kw, MaxDirectFiles: a.Config.Direct.MaxFiles}
}

// capabilities reports whether a bounded task can run for dir: a git
// repository with a committed HEAD, and every set-up step done (sandbox
// image included).
func (a *App) capabilities(ctx context.Context, dir string) direct.Capabilities {
	if capabilitiesFn != nil {
		return capabilitiesFn(ctx, a, dir)
	}
	if dir == "" {
		dir = "."
	}
	if _, err := gitops.Run(ctx, dir, "rev-parse", "--verify", "HEAD"); err != nil {
		return direct.Capabilities{BoundedReason: "not a git repository with a commit"}
	}
	for _, st := range setupSteps() {
		if ok, detail := st.check(ctx, a); !ok {
			return direct.Capabilities{BoundedReason: "set-up incomplete (" + st.Title + ": " + detail + ")"}
		}
	}
	return direct.Capabilities{BoundedAvailable: true}
}

// capabilitiesFn replaces capabilities in tests.
var capabilitiesFn func(ctx context.Context, a *App, dir string) direct.Capabilities

// forcedMode applies direct.mode when it is not auto.
func (a *App) forcedMode(d direct.Decision) direct.Decision {
	if m := a.Config.Direct.Mode; m != "" && m != "auto" {
		return direct.Decision{Mode: m, Reason: "direct.mode is " + m, Confidence: "high"}
	}
	return d
}

// mcpServers merges the configured MCP servers with the project's
// .boundedcode/mcp.json (or .mcp.json), in the common "mcpServers" form.
func (a *App) mcpServers(root string) []direct.MCPServer {
	var out []direct.MCPServer
	names := map[string]bool{}
	for name, m := range a.Config.Direct.MCP {
		names[name] = true
		out = append(out, direct.MCPServer{Name: name, Command: m.Command, Args: m.Args, Env: m.Env, Cwd: m.Cwd, Timeout: m.Timeout.D()})
	}
	for _, f := range []string{filepath.Join(root, ".boundedcode", "mcp.json"), filepath.Join(root, ".mcp.json")} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var doc struct {
			Servers map[string]struct {
				Command string            `json:"command"`
				Args    []string          `json:"args"`
				Env     map[string]string `json:"env"`
				Cwd     string            `json:"cwd"`
			} `json:"mcpServers"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			a.Log.Warn("ignoring MCP configuration", "file", f, "err", err)
			continue
		}
		for name, m := range doc.Servers {
			if names[name] || m.Command == "" {
				continue
			}
			names[name] = true
			cwd := m.Cwd
			if cwd == "" {
				cwd = root
			}
			out = append(out, direct.MCPServer{Name: name, Command: m.Command, Args: m.Args, Env: m.Env, Cwd: cwd, Project: root})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// projectRoot is the git top level containing dir, or dir itself.
func projectRoot(dir string) (string, error) {
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return "", fmt.Errorf("%s is not a directory", abs)
	}
	if root, err := gitops.Run(context.Background(), abs, "rev-parse", "--show-toplevel"); err == nil && strings.TrimSpace(root) != "" {
		return filepath.Clean(strings.TrimSpace(root)), nil
	}
	return abs, nil
}

func newChatCmd(app *App) *cobra.Command {
	var f struct {
		prompt, dir, resume, model, approval, mode string
		cont, jsonEvents                           bool
	}
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Direct mode from a script: send one message to the agent and print its answer (the interactive chat is `" + buildinfo.Command() + "` with no arguments)",
		Long: `Direct mode works in your checkout with file, search and shell tools, like
other coding agents, through the configured model. It is for everyday
development; bounded mode ("task create") is for changes that need isolation
and test-backed evidence.

The message comes from --prompt or from standard input. File edits inside
the project are applied without asking in the default approval mode; shell
commands other than known build, test and read-only commands need approval,
which is refused when there is no terminal (use --approval auto, or ask).

Examples:
  ` + buildinfo.Command() + ` chat -p "explain how retries are configured"
  ` + buildinfo.Command() + ` chat --continue -p "now add a test for it"
  git diff | ` + buildinfo.Command() + ` chat -p "review this diff"`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return app.runChat(cmd.Context(), chatOptions{prompt: f.prompt, dir: f.dir, resume: f.resume, cont: f.cont, model: f.model, approval: f.approval, mode: f.mode, jsonEvents: f.jsonEvents, stdin: cmd.InOrStdin()})
		},
	}
	cmd.Flags().StringVarP(&f.prompt, "prompt", "p", "", "the message (default: standard input)")
	cmd.PersistentFlags().StringVarP(&f.dir, "dir", "C", "", "project directory (default: the current directory)")
	cmd.Flags().BoolVar(&f.cont, "continue", false, "continue the most recent session of this project")
	cmd.Flags().StringVar(&f.resume, "resume", "", "continue the session with this id (or unique prefix)")
	cmd.Flags().StringVar(&f.model, "model", "", "model profile (local) or model name (cloud) for this session")
	cmd.Flags().StringVar(&f.approval, "approval", "", "ask|edits|auto (default: direct.approval in the configuration)")
	cmd.Flags().BoolVar(&f.jsonEvents, "events", false, "print every event as a JSON line instead of text")
	cmd.Flags().StringVar(&f.mode, "mode", "", "auto|chat|code|verify (default: direct.mode; auto routes from the message's intent and risk)")
	cmd.AddCommand(&cobra.Command{
		Use: "undo [SESSION]", Short: "Restore the files changed by the last turn of a session (the most recent session by default); repeat to go further back", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := "last"
			if len(args) == 1 {
				id = args[0]
			}
			root, err := projectRoot(f.dir)
			if err != nil {
				return err
			}
			path, err := direct.FindSession(app.sessionsDir(), root, id)
			if err != nil {
				return err
			}
			sess, err := direct.LoadSession(path)
			if err != nil {
				return err
			}
			r := &direct.Runner{Session: sess, Opts: direct.Options{Root: root, CheckpointDir: filepath.Join(app.sessionsDir(), ".checkpoints", direct.ProjectKey(root))}}
			restored, err := r.Undo()
			if err != nil {
				return err
			}
			app.printf("restored %s\n", strings.Join(restored, ", "))
			return nil
		},
	}, &cobra.Command{
		Use: "tools", Short: "List the tools direct mode offers here (built-in, skills, agents, MCP)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := projectRoot(f.dir)
			if err != nil {
				return err
			}
			r := &direct.Runner{Session: &direct.Session{}, Opts: direct.Options{Root: root, MemoryFile: app.userInstructionsFile(), MCP: app.mcpServers(root)}}
			r.Asker = func(context.Context, string, []string) (string, error) { return "", nil }
			for _, t := range r.Tools() {
				app.printf("%-24s %s\n", t.Name, oneLineText(t.Description, 100))
			}
			if sk := r.Skills(); len(sk) > 0 {
				app.printf("\nskills:\n")
				for _, s := range sk {
					app.printf("  /%-22s %s\n", s.Name, oneLineText(s.Description, 100))
				}
			}
			if ag := r.Agents(); len(ag) > 0 {
				app.printf("\nagents:\n")
				for _, s := range ag {
					app.printf("  %-23s %s\n", s.Name, oneLineText(s.Description, 100))
				}
			}
			if cs := r.Commands(app.Paths.Config); len(cs) > 0 {
				app.printf("\ncommands (/name args):\n")
				for _, c := range cs {
					app.printf("  /%-22s %s\n", c.Name, oneLineText(c.Description, 100))
				}
			}
			for _, l := range r.MCPStatus() {
				app.printf("mcp %s\n", l)
			}
			r.Close()
			return nil
		},
	})
	var exportOut string
	exportCmd := &cobra.Command{
		Use: "export [SESSION]", Short: "Write a session's transcript as Markdown (the most recent session by default; standard output unless --out)", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := "last"
			if len(args) == 1 {
				id = args[0]
			}
			root, err := projectRoot(f.dir)
			if err != nil {
				return err
			}
			path, err := direct.FindSession(app.sessionsDir(), root, id)
			if err != nil {
				return err
			}
			sess, err := direct.LoadSession(path)
			if err != nil {
				return err
			}
			if exportOut == "" {
				app.printf("%s", sess.Markdown())
				return nil
			}
			if err := os.WriteFile(exportOut, []byte(sess.Markdown()), 0o644); err != nil {
				return err
			}
			app.printf("wrote %s\n", exportOut)
			return nil
		},
	}
	exportCmd.Flags().StringVarP(&exportOut, "out", "o", "", "write to this file instead of standard output")
	cmd.AddCommand(exportCmd)
	cmd.AddCommand(&cobra.Command{
		Use: "sessions", Short: "List stored direct-mode sessions of this project", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := projectRoot(f.dir)
			if err != nil {
				return err
			}
			list, err := direct.ListSessions(app.sessionsDir(), root)
			if err != nil {
				return err
			}
			if app.jsonOut {
				return app.printJSON(list)
			}
			if len(list) == 0 {
				app.printf("no sessions for %s\n", root)
				return nil
			}
			for _, s := range list {
				app.printf("%-22s %-19s %3d msg  %s/%s  %s\n", s.ID, s.Updated.Local().Format("2006-01-02 15:04:05"), s.Messages, s.Provider, s.Model, s.Title)
			}
			return nil
		},
	})
	return cmd
}

type chatOptions struct {
	prompt, dir, resume, model, approval, mode string
	cont, jsonEvents                           bool
	stdin                                      io.Reader
}

// runBounded runs a request as a bounded task for dir's repository and
// reports like `task create --run`. Tests replace it.
var runBounded = func(ctx context.Context, a *App, dir, request, model string) error {
	p, err := a.project(ctx, dir)
	if err != nil {
		return err
	}
	if p.Workspace == "" {
		return errors.New("not a git repository")
	}
	t, r, cleanup, err := a.createTask(ctx, p.Workspace, request, nil, nil, "", runFlags{model: model}, true)
	if err != nil {
		return err
	}
	defer cleanup()
	fmt.Fprintf(a.Err, "· verification: task %s on branch %s\n", t.ID, gitops.TaskBranch(t.ID))
	return runAndReport(ctx, a, r, t.ID, orchestrator.RunOptions{})
}

// runChat is the headless (print) form of direct mode.
func (a *App) runChat(ctx context.Context, o chatOptions) error {
	prompt := o.prompt
	if prompt == "" {
		if f, ok := o.stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
			return fmt.Errorf("no message: pass -p \"...\" or pipe one in (run `%s` with no arguments for the interactive chat)", buildinfo.Command())
		}
		b, err := io.ReadAll(o.stdin)
		if err != nil {
			return err
		}
		prompt = strings.TrimSpace(string(b))
		if prompt == "" {
			return errors.New("no message on standard input")
		}
	} else if f, ok := o.stdin.(*os.File); ok && !term.IsTerminal(int(f.Fd())) {
		// A prompt and piped input: the input is context for the prompt.
		if b, _ := io.ReadAll(io.LimitReader(o.stdin, 4<<20)); len(strings.TrimSpace(string(b))) > 0 {
			prompt += "\n\n<stdin>\n" + string(b) + "\n</stdin>"
		}
	}
	session := ""
	switch {
	case o.resume != "":
		session = o.resume
	case o.cont:
		session = "last"
	}
	// Route the message: chat, code or verify.
	mode := o.mode
	if mode == "" {
		mode = a.Config.Direct.Mode
	}
	caps := a.capabilities(ctx, o.dir)
	var d direct.Decision
	switch mode {
	case "", "auto":
		d = direct.Route(prompt, a.riskRules(), caps)
	case direct.ModeChat, direct.ModeCode, direct.ModeVerify:
		d = direct.Decision{Mode: mode, Reason: "requested", Confidence: "high"}
		if mode == direct.ModeVerify && !caps.BoundedAvailable {
			return errors.New("verification is not available here: " + caps.BoundedReason)
		}
	default:
		return fmt.Errorf("mode %q is not auto|chat|code|verify", mode)
	}
	if d.Mode == direct.ModeVerify {
		if !o.jsonEvents {
			fmt.Fprintf(a.Err, "· mode: verify (%s)\n", d.Reason)
		}
		return runBounded(ctx, a, o.dir, prompt, o.model)
	}
	r, err := a.directRunner(ctx, o.dir, session, o.model, o.approval)
	if err != nil {
		return err
	}
	if d.Confidence == "low" && (mode == "" || mode == "auto") {
		d = direct.RouteWithModel(ctx, r.Gateway, prompt, a.riskRules(), caps)
		if d.Mode == direct.ModeVerify {
			if !o.jsonEvents {
				fmt.Fprintf(a.Err, "· mode: verify (%s)\n", d.Reason)
			}
			return runBounded(ctx, a, o.dir, prompt, o.model)
		}
	}
	r.Opts.Mode = d.Mode
	applyVerifyFallback(&r.Opts, d, caps)
	if !o.jsonEvents {
		fmt.Fprintf(a.Err, "· mode: %s (%s)\n", d.Mode, d.Reason)
	}
	interactive := false
	if f, ok := o.stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) && !o.jsonEvents {
		interactive = true
	}
	r.Approve = func(_ context.Context, req direct.ApprovalRequest) bool {
		if a.confirmFn != nil {
			return a.confirmFn(req.Summary + "\n\n" + req.Detail + "\n\nAllow? [y/N] ")
		}
		if !interactive {
			fmt.Fprintf(a.Err, "needs approval (no terminal; use --approval auto to allow): %s\n", req.Summary)
			return false
		}
		fmt.Fprintf(a.Err, "\n%s\n\n%s\n\nAllow? [y/N] ", req.Summary, req.Detail)
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		return strings.EqualFold(strings.TrimSpace(line), "y")
	}
	if interactive {
		r.Asker = func(_ context.Context, q string, opts []string) (string, error) {
			fmt.Fprintf(a.Err, "\n? %s\n", q)
			for i, o := range opts {
				fmt.Fprintf(a.Err, "  %d. %s\n", i+1, o)
			}
			fmt.Fprint(a.Err, "> ")
			line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			line = strings.TrimSpace(line)
			var n int
			if _, err := fmt.Sscanf(line, "%d", &n); err == nil && n >= 1 && n <= len(opts) {
				return opts[n-1], nil
			}
			return line, nil
		}
	}
	// /name args runs a project or user command template.
	if strings.HasPrefix(prompt, "/") {
		name, args, _ := strings.Cut(strings.TrimPrefix(prompt, "/"), " ")
		if expanded, ok, err := r.ExpandCommand(ctx, a.Paths.Config, strings.TrimSpace(name), strings.TrimSpace(args)); err != nil {
			return err
		} else if ok {
			prompt = expanded
		}
	}
	defer r.Close()
	audit := r.Sink
	enc := json.NewEncoder(a.Out)
	streamed := false
	r.Sink = func(e direct.Event) {
		audit(e)
		if o.jsonEvents {
			_ = enc.Encode(e)
			return
		}
		switch e.Kind {
		case direct.EventTextDelta:
			streamed = true
			fmt.Fprint(a.Out, e.Text)
		case direct.EventToolStart:
			fmt.Fprintf(a.Err, "⚙ %s\n", e.Summary)
		case direct.EventToolEnd:
			if e.Err != "" {
				fmt.Fprintf(a.Err, "  ✘ %s (%s)\n", e.Err, e.Elapsed.Round(time.Millisecond))
			}
		case direct.EventDenied:
			fmt.Fprintf(a.Err, "✘ %s: %s\n", e.Summary, e.Err)
		case direct.EventCompact, direct.EventNote:
			fmt.Fprintf(a.Err, "· %s\n", e.Text)
		case direct.EventSandbox:
			if e.Args["level"] != nil || e.Args["allowed"] == false {
				fmt.Fprintf(a.Err, "· %s\n", e.Summary)
			}
		case direct.EventMode:
			fmt.Fprintf(a.Err, "· mode: %s (%s)\n", e.Text, e.Summary)
		case direct.EventTodos:
			fmt.Fprintf(a.Err, "· tasks:\n%s\n", indentLines(e.Text, "    "))
		case direct.EventQuestion:
			fmt.Fprintf(a.Err, "? %s\n  → %s\n", e.Text, e.Output)
		case direct.EventSubagent:
			if e.Text == string(direct.EventToolStart) {
				fmt.Fprintf(a.Err, "  ↳ %s: %s\n", e.Tool, e.Summary)
			}
		case direct.EventText:
			if streamed {
				// Already printed as it arrived.
				streamed = false
				fmt.Fprintln(a.Out)
				break
			}
			fmt.Fprintln(a.Out, e.Text)
		case direct.EventDone:
			switch {
			case len(e.Changed) > 0:
				fmt.Fprintf(a.Err, "· changed: %s\n", strings.Join(e.Changed, ", "))
			case e.Changed == nil:
			default:
				fmt.Fprintln(a.Err, "· no files changed")
			}
			if e.Usage != nil {
				fmt.Fprintf(a.Err, "· session %s · %s tokens processed, %s generated\n", r.Session.ID, humanInt(e.Usage.Processed), humanInt(e.Usage.Generated))
			}
		}
	}
	_, err = r.Send(ctx, prompt)
	var h *direct.Handoff
	if errors.As(err, &h) {
		if len(h.Changed) > 0 {
			fmt.Fprintf(a.Err, "· earlier changes of this turn are kept in your checkout: %s\n", strings.Join(h.Changed, ", "))
		}
		return runBounded(ctx, a, o.dir, prompt, o.model)
	}
	return err
}

func oneLineText(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func indentLines(s, pre string) string {
	return pre + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n"+pre)
}

func humanInt(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}
