package direct

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Hook events.
const (
	HookPreTool    = "pre_tool"    // before a tool runs; exit 2 or {"decision":"deny"} blocks it
	HookPostTool   = "post_tool"   // after a tool ran; stdout is shown to the model
	HookUserPrompt = "user_prompt" // before a message is sent; exit 2 blocks it, stdout is appended
	HookStop       = "stop"        // when the model stops; exit 2 makes it continue with stderr as the reason
)

// Hook is one configured hook: a shell command run for an event, with the
// event's data as JSON on stdin. Matcher is a regular expression on the
// tool name (pre_tool, post_tool); empty matches every tool.
type Hook struct {
	Event   string
	Matcher string
	Command string
	Timeout time.Duration
}

// HookInput is what a hook reads on stdin.
type HookInput struct {
	Event   string         `json:"event"`
	Session string         `json:"session,omitempty"`
	Cwd     string         `json:"cwd"`
	Tool    string         `json:"tool,omitempty"`
	Args    map[string]any `json:"args,omitempty"`
	Result  string         `json:"result,omitempty"`
	Error   string         `json:"error,omitempty"`
	Prompt  string         `json:"prompt,omitempty"`
}

// HookResult is the combined outcome of the hooks of one event.
type HookResult struct {
	Block  bool
	Reason string
	Extra  string // stdout to show the model or append to the prompt
}

func (r *Runner) runHooks(ctx context.Context, event string, in HookInput) HookResult {
	var res HookResult
	for _, h := range r.Opts.Hooks {
		if h.Event != event || strings.TrimSpace(h.Command) == "" {
			continue
		}
		if h.Matcher != "" && in.Tool != "" {
			if ok, _ := regexp.MatchString(h.Matcher, in.Tool); !ok {
				continue
			}
		}
		in.Event, in.Cwd = event, r.cwd()
		if r.Session != nil {
			in.Session = r.Session.ID
		}
		out, errOut, code := r.execHook(ctx, h, in)
		var decision struct {
			Decision, Reason, Message string
		}
		_ = json.Unmarshal([]byte(out), &decision)
		switch {
		case code == 2 || strings.EqualFold(decision.Decision, "deny") || strings.EqualFold(decision.Decision, "block"):
			reason := firstNonEmpty(decision.Reason, decision.Message, strings.TrimSpace(errOut), strings.TrimSpace(out), "hook "+h.Command)
			r.emit(Event{Kind: EventNote, Text: "hook (" + event + ") blocked: " + reason})
			return HookResult{Block: true, Reason: reason}
		case code != 0:
			r.emit(Event{Kind: EventNote, Text: "hook (" + event + ") failed: " + firstNonEmpty(strings.TrimSpace(errOut), "exit status")})
		default:
			if t := strings.TrimSpace(out); t != "" && decision.Decision == "" {
				if res.Extra != "" {
					res.Extra += "\n"
				}
				res.Extra += t
			}
		}
	}
	return res
}

func (r *Runner) execHook(ctx context.Context, h Hook, in HookInput) (stdout, stderr string, code int) {
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	argv := shellArgv(h.Command)
	c := exec.CommandContext(cctx, argv[0], argv[1:]...)
	c.Dir = r.cwd()
	c.Env = append(os.Environ(), "BOUNDEDCODE_HOOK_EVENT="+in.Event, "BOUNDEDCODE_PROJECT_DIR="+r.Opts.Root)
	b, _ := json.Marshal(in)
	c.Stdin = bytes.NewReader(b)
	var o, e bytes.Buffer
	c.Stdout, c.Stderr = &o, &e
	setProcessGroup(c)
	err := c.Run()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return o.String(), e.String(), ee.ExitCode()
		}
		return o.String(), err.Error(), 1
	}
	return o.String(), e.String(), 0
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
