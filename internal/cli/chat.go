package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	r := &direct.Runner{
		Gateway: ep.newGateway(sess.ID),
		Session: sess,
		Opts: direct.Options{
			Root: root, Dir: cwd, Approval: approval, MaxTurns: a.Config.Direct.MaxTurns,
			CommandTimeout: a.Config.Direct.CommandTimeout.D(), ContextTokens: ep.ctxSize,
			MaxOutputTokens: a.Config.Agent.MaxOutputTokens, Instructions: a.Config.Direct.Instructions,
			UserInstructions: a.userInstructionsFile(),
		},
	}
	// The audit log records what ran, never file contents or outputs.
	r.Sink = func(e direct.Event) {
		switch e.Kind {
		case direct.EventToolStart:
			rec.Emit(ctx, sess.ID, "direct.tool", map[string]any{"tool": e.Tool, "summary": telemetry.Redact(e.Summary)})
		case direct.EventDenied:
			rec.Emit(ctx, sess.ID, "direct.denied", map[string]any{"tool": e.Tool, "summary": telemetry.Redact(e.Summary), "reason": e.Err})
		case direct.EventCompact:
			rec.Emit(ctx, sess.ID, "direct.compact", nil)
		}
	}
	return r, nil
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
	r.Opts.Mode, r.Opts.AllowRisky = d.Mode, !caps.BoundedAvailable
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
	audit := r.Sink
	enc := json.NewEncoder(a.Out)
	r.Sink = func(e direct.Event) {
		audit(e)
		if o.jsonEvents {
			_ = enc.Encode(e)
			return
		}
		switch e.Kind {
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
		case direct.EventMode:
			fmt.Fprintf(a.Err, "· mode: %s (%s)\n", e.Text, e.Summary)
		case direct.EventText:
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

func humanInt(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}
