package direct

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const maxInstructionBytes = 24 << 10

// systemPrompt builds the system message: identity, how to work, the
// skills and the project's own instructions. It is identical from turn to
// turn on purpose: an OpenAI-compatible server reuses the key-value cache
// only for an unchanged prefix, so anything that varies per turn (the git
// status, the date, the mode) travels with the user message instead
// (turnContext).
func (r *Runner) systemPrompt() string {
	var b strings.Builder
	b.WriteString(`You are BoundedCode, a coding agent working directly in the user's project checkout. You help with everyday software development: reading and explaining code, fixing bugs, adding features, writing tests, refactoring, and running builds and tests.

How to work:
- Be concise. Answer directly; no preamble, no restating the request.
- Look before you change: read the relevant code, find callers with grep, and follow the project's existing conventions.
- Make the smallest correct change. Do not refactor or reformat beyond the request.
- Use edit for changes to existing files (copy old_string exactly from a read), write only for new files.
- After changing code, run the relevant build or tests with bash when the project has them, and report the result honestly. Do not claim a test passed without running it.
- Never commit, push, publish or deploy unless the user asks explicitly. Pushes and infrastructure mutations are refused by policy anyway.
- If an action is refused or not approved, do not retry it; explain and offer an alternative.
- When a request is genuinely ambiguous in a way that changes the work, ask one precise question instead of guessing.
- When a change is risky (authentication, payments, migrations, shared contracts, large multi-file changes) suggest that the user run it in bounded mode (the /task command), where it runs in an isolated worktree and sandbox and must be backed by a test that fails without the change.
`)
	if sk := r.skillsPrompt(); sk != "" {
		b.WriteString("\n" + sk + "\n")
	}
	if ins := r.projectInstructions(); ins != "" {
		b.WriteString("\nProject instructions (from the repository; follow them):\n\n")
		b.WriteString(ins)
		b.WriteString("\n")
	}
	if r.ExtraSystem != "" {
		b.WriteString("\n" + r.ExtraSystem + "\n")
	}
	return b.String()
}

// turnContext is what changes from turn to turn: the environment (working
// directory, git branch and status, date) and how this message was
// classified. It is stored with the user message (Message.Context) and
// sent after its text, so the system prompt and the earlier history stay
// a stable prefix for the server's cache.
func (r *Runner) turnContext() string {
	var b strings.Builder
	env := r.environment()
	b.WriteString("Environment:\n")
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "- %s: %s\n", k, env[k])
	}
	if f := modeFraming(r.Opts.Mode); f != "" {
		b.WriteString("\n" + f + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// modeFraming tells the model how the message was classified.
func modeFraming(mode string) string {
	switch mode {
	case ModeChat:
		return "This message was classified as a question or discussion: answer it from the code. Do not change files unless the user clearly asked for a change; if they did, go ahead (the mode switches automatically)."
	case ModeCode:
		return "This message was classified as a request for a direct change in the checkout. Changes to authentication, payments, migrations, shared contracts or infrastructure are handed off automatically to a verified task; do not work around that."
	}
	return ""
}

// reframeTurn updates the current user message's framing after a mode
// switch during the turn, so the model is told what mode it is in now.
// Only the in-memory message changes: the framing is per turn, and the
// history before this message stays a stable prefix.
func (r *Runner) reframeTurn() {
	if r.Session == nil {
		return
	}
	for i := len(r.Session.Messages) - 1; i >= 0; i-- {
		m := &r.Session.Messages[i]
		if m.Role != "user" || m.Context == "" {
			continue
		}
		// The context is: environment, framing, then the places found for
		// the request; only the framing changes.
		env, rest, _ := strings.Cut(m.Context, "\n\nThis message was classified")
		places := ""
		if i := strings.Index(rest, "\n\n"); rest != "" && i >= 0 {
			places = rest[i:]
		} else if rest == "" && strings.Contains(env, "\n\nLikely relevant places") {
			i := strings.Index(env, "\n\nLikely relevant places")
			env, places = env[:i], env[i:]
		}
		m.Context = env
		if f := modeFraming(r.Opts.Mode); f != "" {
			m.Context += "\n\n" + f
		}
		m.Context += places
		return
	}
}

func (r *Runner) environment() map[string]string {
	if r.Opts.Env != nil {
		return r.Opts.Env
	}
	now := time.Now
	if r.Opts.Now != nil {
		now = r.Opts.Now
	}
	env := map[string]string{
		"working directory": r.cwd(),
		"project root":      r.Opts.Root + " (tools are confined to it; relative paths are from the working directory)",
		"platform":          runtime.GOOS + "/" + runtime.GOARCH,
		"date":              now().Format("2006-01-02"),
		"shell":             "bash (sh -c semantics; no interactive prompts)",
	}
	if runtime.GOOS == "windows" {
		env["shell"] = "PowerShell"
	}
	if branch := gitOutput(r.Opts.Root, "rev-parse", "--abbrev-ref", "HEAD"); branch != "" {
		env["git branch"] = branch
		if st := gitOutput(r.Opts.Root, "status", "--porcelain"); st != "" {
			n := len(strings.Split(strings.TrimRight(st, "\n"), "\n"))
			env["git status"] = fmt.Sprintf("%d changed path(s)", n)
		} else {
			env["git status"] = "clean"
		}
	} else {
		env["git"] = "not a git repository"
	}
	r.Opts.Env = env
	r.envAuto = true
	return env
}

func gitOutput(dir string, args ...string) string {
	c := exec.CommandContext(context.Background(), "git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := c.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// projectInstructions returns the first instruction file found at the
// root (direct.instructions), followed by the user's own file, if any.
func (r *Runner) projectInstructions() string {
	var parts []string
	names := r.Opts.Instructions
	if len(names) == 0 {
		names = []string{"AGENTS.md", "BOUNDEDCODE.md", "CLAUDE.md", ".boundedcode/instructions.md"}
	}
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(r.Opts.Root, filepath.FromSlash(n)))
		if err != nil {
			continue
		}
		parts = append(parts, "<!-- "+n+" -->\n"+capBytes(string(b), maxInstructionBytes))
		break
	}
	if r.Opts.UserInstructions != "" {
		if b, err := os.ReadFile(r.Opts.UserInstructions); err == nil && strings.TrimSpace(string(b)) != "" {
			parts = append(parts, "<!-- user instructions -->\n"+capBytes(string(b), maxInstructionBytes/2))
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func capBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n… (truncated)"
}
