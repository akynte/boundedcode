// Package capability describes what the agent's tools do, in both
// execution modes, in one vocabulary: the effects a call has outside the
// model's reply, whether it may run alongside other calls, what policy and
// permission rules match it on, and the codes a refused or failed call
// reports. The modes keep their own execution and isolation (direct mode
// runs tools in the user's checkout under the runner's policy; bounded
// mode's agent runs OpenHands' tools in a container); this package is the
// shared description, not a shared executor.
package capability

import (
	"sort"
	"strings"
)

// Effect is one kind of thing a call does beyond producing text.
type Effect uint16

const (
	// Read reads project files or repository state.
	Read Effect = 1 << iota
	// Write changes project files.
	Write
	// Exec runs a command.
	Exec
	// Network reaches a host other than this machine.
	Network
	// Interact asks the user.
	Interact
	// Delegate runs another agent.
	Delegate
	// State changes the session's own state (a task list, plan mode, a
	// memory file, the end of the turn).
	State
)

var effectNames = []struct {
	e    Effect
	name string
}{{Read, "read"}, {Write, "write"}, {Exec, "exec"}, {Network, "network"}, {Interact, "interact"}, {Delegate, "delegate"}, {State, "state"}}

// Has reports whether e includes every effect in x.
func (e Effect) Has(x Effect) bool { return x != 0 && e&x == x }

func (e Effect) String() string {
	var out []string
	for _, n := range effectNames {
		if e&n.e != 0 {
			out = append(out, n.name)
		}
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, ",")
}

// Subject is the part of a call that policy and permission rules match.
type Subject string

const (
	SubjectNone    Subject = ""
	SubjectPath    Subject = "path"    // a project path (or several)
	SubjectCommand Subject = "command" // a shell command
	SubjectHost    Subject = "host"    // a URL's host
	SubjectQuery   Subject = "query"   // a search query
)

// Mode is an execution mode.
type Mode string

const (
	Direct  Mode = "direct"
	Bounded Mode = "bounded"
)

// Descriptor describes one tool.
type Descriptor struct {
	Name    string
	Mode    Mode
	Effects Effect
	// Concurrent: a call may run at the same time as the other concurrent
	// calls of the same model reply. Only calls that change nothing (no
	// files, no session state, no user prompt) qualify.
	Concurrent bool
	Subject    Subject
	// Source is "builtin", "mcp:<server>", "skill", "agent" or, in bounded
	// mode, "openhands".
	Source string
	// Enforced names where each effect is checked, for the documentation
	// and `bcode capabilities tools`.
	Enforced string
}

// ReadOnly reports whether a call changes nothing outside the reply.
func (d Descriptor) ReadOnly() bool { return d.Effects&^(Read|Network) == 0 }

// Code classifies why a call did not produce an ordinary result.
type Code string

const (
	CodeOK          Code = ""
	CodeUnknownTool Code = "unknown_tool"
	CodeBadArgs     Code = "bad_arguments"
	CodePolicy      Code = "denied_by_policy" // a deterministic check before anyone is asked
	CodeRule        Code = "denied_by_rule"   // the user's deny rules
	CodePlanMode    Code = "plan_mode"
	CodeHandoff     Code = "handed_off" // the change goes to a verified task
	CodeHook        Code = "blocked_by_hook"
	CodeNotApproved Code = "not_approved"
	CodeConflict    Code = "conflict" // the file changed since it was last read
	CodeFailed      Code = "failed"   // the tool ran and reported an error
	CodeCanceled    Code = "canceled"
	CodeNotRun      Code = "not_run" // skipped: the turn ended before it
)

// Error is a structured tool error: what the user interface and traces
// see. The model sees its Message.
type Error struct {
	Code    Code
	Message string
}

func (e *Error) Error() string { return e.Message }

// Refused reports whether the call was stopped before it ran.
func (c Code) Refused() bool {
	switch c {
	case CodeUnknownTool, CodeBadArgs, CodePolicy, CodeRule, CodePlanMode, CodeHandoff, CodeHook, CodeNotApproved, CodeNotRun:
		return true
	}
	return false
}

// BoundedTools are the tools of bounded mode's agent: OpenHands SDK
// built-ins registered by adapters/openhands (TerminalTool, FileEditorTool,
// TaskTrackerTool) and the SDK's defaults (think, finish). BoundedCode adds
// none. They run inside the task's container, so their effects are bounded
// by it rather than checked per call.
var BoundedTools = []Descriptor{
	{Name: "terminal", Mode: Bounded, Effects: Exec | Read | Write, Subject: SubjectCommand, Source: "openhands",
		Enforced: "container (no network by default, capabilities dropped, secret paths masked); verification checks the diff afterwards"},
	{Name: "file_editor", Mode: Bounded, Effects: Read | Write, Subject: SubjectPath, Source: "openhands",
		Enforced: "container mounts (worktree only, secret paths masked); verification refuses protected and secret paths in the diff"},
	{Name: "task_tracker", Mode: Bounded, Effects: State, Source: "openhands", Enforced: "none needed"},
	{Name: "think", Mode: Bounded, Source: "openhands", Enforced: "none needed"},
	{Name: "finish", Mode: Bounded, Effects: State, Source: "openhands", Enforced: "verification runs after the agent finishes"},
}

// fileEditorReads are file_editor's commands that only look.
var fileEditorReads = map[string]bool{"view": true}

// BoundedAction is the effect of one bounded-mode action: the tool and,
// for file_editor, its command (view, create, str_replace, insert,
// undo_edit).
func BoundedAction(tool, command string) Effect {
	switch tool {
	case "file_editor":
		if fileEditorReads[command] {
			return Read
		}
		if command == "" {
			return 0
		}
		return Write
	case "terminal":
		return Exec
	}
	if d, ok := Lookup(BoundedTools, tool); ok {
		return d.Effects
	}
	return 0
}

// Lookup finds a tool by name.
func Lookup(ds []Descriptor, name string) (Descriptor, bool) {
	for _, d := range ds {
		if d.Name == name {
			return d, true
		}
	}
	return Descriptor{}, false
}

// Tally counts calls per tool and outcome, the same way in both modes.
type Tally struct {
	Calls    map[string]int          `json:"calls"`
	Outcomes map[string]map[Code]int `json:"outcomes,omitempty"`
}

// Add counts one call of tool with outcome code.
func (t *Tally) Add(tool string, code Code) {
	if t.Calls == nil {
		t.Calls = map[string]int{}
	}
	t.Calls[tool]++
	if code == CodeOK {
		return
	}
	if t.Outcomes == nil {
		t.Outcomes = map[string]map[Code]int{}
	}
	if t.Outcomes[tool] == nil {
		t.Outcomes[tool] = map[Code]int{}
	}
	t.Outcomes[tool][code]++
}

// Names lists the tools counted, sorted.
func (t *Tally) Names() []string {
	out := make([]string, 0, len(t.Calls))
	for n := range t.Calls {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
