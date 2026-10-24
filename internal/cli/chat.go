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
		prompt, dir, resume, model, approval string
		cont, jsonEvents                     bool
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
			return app.runChat(cmd.Context(), chatOptions{prompt: f.prompt, dir: f.dir, resume: f.resume, cont: f.cont, model: f.model, approval: f.approval, jsonEvents: f.jsonEvents, stdin: cmd.InOrStdin()})
		},
	}
	cmd.Flags().StringVarP(&f.prompt, "prompt", "p", "", "the message (default: standard input)")
	cmd.PersistentFlags().StringVarP(&f.dir, "dir", "C", "", "project directory (default: the current directory)")
	cmd.Flags().BoolVar(&f.cont, "continue", false, "continue the most recent session of this project")
	cmd.Flags().StringVar(&f.resume, "resume", "", "continue the session with this id (or unique prefix)")
	cmd.Flags().StringVar(&f.model, "model", "", "model profile (local) or model name (cloud) for this session")
	cmd.Flags().StringVar(&f.approval, "approval", "", "ask|edits|auto (default: direct.approval in the configuration)")
	cmd.Flags().BoolVar(&f.jsonEvents, "events", false, "print every event as a JSON line instead of text")
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
	prompt, dir, resume, model, approval string
	cont, jsonEvents                     bool
	stdin                                io.Reader
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
	r, err := a.directRunner(ctx, o.dir, session, o.model, o.approval)
	if err != nil {
		return err
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
