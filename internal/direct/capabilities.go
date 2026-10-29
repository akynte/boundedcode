package direct

import (
	"strings"

	"github.com/akynte/boundedcode/internal/capability"
)

// builtinCapabilities describes the built-in tools in the vocabulary both
// modes share (internal/capability): what a call does beyond its reply,
// whether it may run alongside the other concurrent calls of a reply, and
// what permission rules match. TestEveryToolIsDescribed keeps it complete.
var builtinCapabilities = map[string]capability.Descriptor{
	"read":                 {Effects: capability.Read, Concurrent: true, Subject: capability.SubjectPath},
	"read_many":            {Effects: capability.Read, Concurrent: true, Subject: capability.SubjectPath},
	"grep":                 {Effects: capability.Read, Concurrent: true, Subject: capability.SubjectPath},
	"glob":                 {Effects: capability.Read, Concurrent: true, Subject: capability.SubjectPath},
	"ls":                   {Effects: capability.Read, Concurrent: true, Subject: capability.SubjectPath},
	"notebook_read":        {Effects: capability.Read, Concurrent: true, Subject: capability.SubjectPath},
	"find_symbol":          {Effects: capability.Read, Concurrent: true},
	"find_references":      {Effects: capability.Read, Concurrent: true},
	"find_implementations": {Effects: capability.Read, Concurrent: true},
	"recall":               {Effects: capability.Read, Concurrent: true},
	"web_fetch":            {Effects: capability.Network, Concurrent: true, Subject: capability.SubjectHost},
	"web_search":           {Effects: capability.Network, Concurrent: true, Subject: capability.SubjectQuery},
	"write":                {Effects: capability.Write, Subject: capability.SubjectPath},
	"edit":                 {Effects: capability.Read | capability.Write, Subject: capability.SubjectPath},
	"multi_edit":           {Effects: capability.Read | capability.Write, Subject: capability.SubjectPath},
	"replace_symbol":       {Effects: capability.Read | capability.Write, Subject: capability.SubjectPath},
	"notebook_edit":        {Effects: capability.Read | capability.Write, Subject: capability.SubjectPath},
	// A command can do anything the sandbox allows; ordered.
	"bash":            {Effects: capability.Exec | capability.Read | capability.Write | capability.Network, Subject: capability.SubjectCommand},
	"bash_output":     {Effects: capability.Exec}, // consumes the shell's new output
	"kill_shell":      {Effects: capability.Exec},
	"todo_write":      {Effects: capability.State},
	"enter_plan_mode": {Effects: capability.State},
	"exit_plan_mode":  {Effects: capability.State | capability.Interact},
	"ask_user":        {Effects: capability.Interact},
	"save_memory":     {Effects: capability.State | capability.Write},
	"skill":           {Effects: capability.State | capability.Exec}, // a command template can run commands
	"task":            {Effects: capability.Delegate},
	// Searches the deferred definitions held in memory.
	"mcp_search": {Concurrent: true},
	// Runs a deferred MCP tool, which can do anything: ordered.
	"mcp_call": {Effects: capability.Read | capability.Write | capability.Exec | capability.Network},
}

// enforcedDirect is where direct mode checks a call, in order (runTool).
const enforcedDirect = "precheck, deny rules, plan mode, risky-path hand-off, pre_tool hooks, the guard (sensitive categories), approval mode; commands in the sandbox (execbox)"

// describe is a tool's capability descriptor. MCP tools are external: their
// effects are not known, so they are treated as able to do anything and
// run in order.
func describe(t *Tool) capability.Descriptor {
	d, ok := builtinCapabilities[t.Name]
	switch {
	case ok:
		d.Source = "builtin"
	case strings.HasPrefix(t.Name, "mcp__"):
		server, _, _ := strings.Cut(strings.TrimPrefix(t.Name, "mcp__"), "__")
		d = capability.Descriptor{Effects: capability.Read | capability.Write | capability.Exec | capability.Network, Source: "mcp:" + server}
	default:
		d = capability.Descriptor{Effects: capability.Read | capability.Write | capability.Exec | capability.Network, Source: "unknown"}
	}
	d.Name, d.Mode, d.Enforced = t.Name, capability.Direct, enforcedDirect
	return d
}

// Capabilities describes the tools this runner offers.
func (r *Runner) Capabilities() []capability.Descriptor {
	ts := r.toolList()
	out := make([]capability.Descriptor, 0, len(ts))
	for i := range ts {
		out = append(out, describe(&ts[i]))
	}
	return out
}
