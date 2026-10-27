package direct

import (
	"context"
	"os"
	"strings"
	"sync"

	"github.com/akynte/boundedcode/internal/execbox"
	"github.com/akynte/boundedcode/internal/policy"
)

// Commands the agent runs (bash, background shells, template commands,
// post-edit checks) run in the session's sandbox (internal/execbox) when
// one is configured. When the containment it gives is weaker than strong,
// running anything beyond read-only file utilities needs the user's
// authorization, once per session: direct.sandbox.fallback ask (default;
// refused when no one can be asked), refuse, or allow (audited). Choosing
// a weaker setting in the configuration (sandbox.mode off, network host, a
// weaker backend) is itself that authorization.

// EventSandbox reports the sandbox: its containment once per session
// (Text), and each network decision of its proxy (Args host, port,
// allowed, reason).
const EventSandbox EventKind = "sandbox"

// sandboxState is shared by a runner and its sub-agents.
type sandboxState struct {
	mu       sync.Mutex
	reported bool
	approved bool
}

func (r *Runner) sbxState() *sandboxState {
	if r.sbx == nil {
		r.sbx = &sandboxState{}
	}
	return r.sbx
}

// noteContainment reports the sandbox once per session.
func (r *Runner) noteContainment() execbox.Report {
	rep := r.Opts.Sandbox.Report()
	st := r.sbxState()
	st.mu.Lock()
	first := !st.reported
	st.reported = true
	st.mu.Unlock()
	if first {
		r.emit(Event{Kind: EventSandbox, Text: rep.String(), Summary: "commands run with " + rep.Level + " containment (" + rep.Backend + ")",
			Args: map[string]any{"backend": rep.Backend, "level": rep.Level}})
	}
	return rep
}

// containment reports whether a command may run under the sandbox there
// is, asking the user when it is weaker than strong.
func (r *Runner) containment(ctx context.Context, cmd string) (bool, string) {
	if r.Opts.Sandbox == nil {
		return true, ""
	}
	rep := r.noteContainment()
	if rep.Level == execbox.LevelStrong || rep.Level == execbox.LevelOff || rep.ByConfig || policy.Inert(cmd) {
		return true, ""
	}
	st := r.sbxState()
	st.mu.Lock()
	ok := st.approved
	st.mu.Unlock()
	if ok {
		return true, ""
	}
	f := policy.Finding{Category: "containment", Subject: rep.Backend, Reason: "commands would run with " + rep.Level + " containment"}
	why := "Commands cannot be fully contained here (" + rep.Level + ", " + rep.Backend + ")."
	switch r.Opts.SandboxFallback {
	case "allow":
		r.guardEvent("bash", f, "authorized", "sandbox fallback allow")
	case "refuse":
		r.guardEvent("bash", f, "refused", "sandbox fallback refuse")
		return false, "refused: " + why + " The configuration (direct.sandbox.fallback: refuse) does not allow running commands without full containment. Explain this to the user."
	default:
		if r.Approve == nil {
			r.guardEvent("bash", f, "refused", "no approver")
			return false, "refused: " + why + " No one can be asked to allow running commands with weaker containment. The user can allow it with direct.sandbox.fallback: allow, or run interactively."
		}
		if !r.Approve(ctx, ApprovalRequest{Tool: "sandbox", Summary: "Run commands with " + rep.Level + " containment for this session?",
			Detail: why + "\n\n" + rep.String() + "\n\nCommands from now on in this session run like this. Read-only file utilities (ls, cat, grep, …) are not affected.\n\n$ " + cmd}) {
			r.guardEvent("bash", f, "refused", "not approved")
			return false, "refused: " + why + " The user did not allow it. Explain this; do not try to run the command another way."
		}
		r.guardEvent("bash", f, "approved", "approval")
	}
	st.mu.Lock()
	st.approved = true
	st.mu.Unlock()
	return true, ""
}

// command prepares a subprocess: in the sandbox when there is one, and in
// every case ending its whole process tree on cancel. line is the shell
// command it runs ("" for a check), whose authorized hosts and package
// managers decide what the proxy admits.
func (r *Runner) command(ctx context.Context, argv []string, dir string, env []string, line string, refused ...*netRefusals) (*execbox.Cmd, error) {
	if r.Opts.Sandbox == nil {
		return execbox.Plain(ctx, argv, dir, append(os.Environ(), env...)), nil
	}
	onNet := r.netEvent
	if len(refused) > 0 && refused[0] != nil {
		nr := refused[0]
		onNet = func(e execbox.NetEvent) {
			r.netEvent(e)
			if !e.Allowed {
				nr.add(e.Host)
			}
		}
	}
	spec := execbox.Spec{Argv: argv, Dir: dir, Env: env, OnNet: onNet}
	if line != "" {
		spec.Tools = policy.CommandTools(line)
		// Every finding of the command was authorized before it got here
		// (guard.go): the hosts it names may be reached.
		for _, f := range policy.ClassifyCommand(line, r.commandContext()) {
			if f.Category == policy.CatNetwork && f.Subject != "" {
				spec.Hosts = append(spec.Hosts, f.Subject)
			}
			if f.Category == policy.CatInstall {
				for _, t := range spec.Tools {
					spec.Hosts = append(spec.Hosts, execbox.Registries(t)...)
				}
			}
		}
	} else {
		spec.Tools = []string{argv[0]}
	}
	return r.Opts.Sandbox.Command(ctx, spec)
}

func (r *Runner) netEvent(e execbox.NetEvent) {
	decision := "refused"
	if e.Allowed {
		decision = "allowed"
	}
	r.emit(Event{Kind: EventSandbox, Summary: "network " + decision + ": " + e.Host + ":" + e.Port,
		Args: map[string]any{"host": e.Host, "port": e.Port, "allowed": e.Allowed, "reason": e.Reason}})
}

// netRefusals collects the hosts the proxy refused during one command.
type netRefusals struct {
	mu    sync.Mutex
	hosts []string
}

func (n *netRefusals) add(h string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, x := range n.hosts {
		if x == h {
			return
		}
	}
	n.hosts = append(n.hosts, h)
}

// note explains refusals the command's output may not show (curl -s).
func (n *netRefusals) note() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.hosts) == 0 {
		return ""
	}
	return "\n\n[sandbox: network access to " + strings.Join(n.hosts, ", ") + " was refused: commands run without network, except " +
		"hosts the user allowed. If the task needs it, ask the user, naming the host.]"
}
