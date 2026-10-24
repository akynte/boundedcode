package direct

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/akynte/boundedcode/internal/policy"
)

func policyCheck(argv []string) error { return policy.CheckCommand(argv) }

// Built-in sub-agent kinds.
const (
	agentExplore = "explore" // read-only: find and report
	agentGeneral = "general" // the full tool set, no nested sub-agents
)

func taskTool() Tool {
	return Tool{
		Name:        "task",
		Description: "Delegate a self-contained piece of work to a sub-agent with its own context and return its report. subagent_type: explore (read-only search and analysis, cheap), general (can change files and run commands), or a custom agent by name.",
		Schema: schema(map[string]any{
			"description":   prop("string", "What the sub-agent is for, in a few words"),
			"prompt":        prop("string", "The complete task for the sub-agent (it does not see this conversation)"),
			"subagent_type": prop("string", "explore (default), general, or a custom agent name"),
		}, "prompt"),
		Summary: func(a map[string]any) string {
			kind := firstNonEmpty(str(a, "subagent_type"), agentExplore)
			return "sub-agent " + kind + ": " + oneLine(firstNonEmpty(str(a, "description"), str(a, "prompt")), 70)
		},
		Run: func(ctx context.Context, r *Runner, a map[string]any) (string, error) {
			prompt := strings.TrimSpace(str(a, "prompt"))
			if prompt == "" {
				return "", errors.New("prompt is empty")
			}
			kind := firstNonEmpty(strings.ToLower(strings.TrimSpace(str(a, "subagent_type"))), agentExplore)
			child, err := r.subagent(kind)
			if err != nil {
				return "", err
			}
			out, err := child.Send(ctx, prompt)
			child.Close()
			if err != nil && out == "" {
				return "", fmt.Errorf("sub-agent %s: %w", kind, err)
			}
			if out == "" {
				out = "(the sub-agent produced no report)"
			}
			return fmt.Sprintf("<subagent type=%q>\n%s\n</subagent>", kind, out), err
		},
	}
}

// subagent builds a child runner sharing the gateway and approvals, with
// its own in-memory session and tool set.
func (r *Runner) subagent(kind string) (*Runner, error) {
	opts := r.Opts
	opts.Mode = ModeCode
	opts.NoSubagents = true
	opts.CheckpointDir = r.Opts.CheckpointDir // edits by sub-agents are undoable too
	extra := ""
	switch kind {
	case agentExplore:
		opts.ReadOnly = true
		extra = "You are a read-only exploration sub-agent. Find what was asked and report precisely: paths with line numbers, the relevant code, and a short conclusion. Do not change files."
	case agentGeneral:
		opts.ReadOnly = false
		extra = "You are a sub-agent carrying out one delegated task. Do it completely, then report what you changed and what you verified."
	default:
		var def *AgentDef
		for i := range r.Agents() {
			if r.agents[i].Name == kind {
				def = &r.agents[i]
			}
		}
		if def == nil {
			return nil, fmt.Errorf("no sub-agent type %q (explore, general, or one of: %s)", kind, strings.Join(agentNames(r.Agents()), ", "))
		}
		opts.ReadOnly = def.ReadOnly
		extra = def.Prompt
	}
	child := &Runner{Gateway: r.Gateway, Opts: opts, Approve: r.Approve, Asker: r.Asker, Nav: r.Nav, Depth: r.Depth + 1,
		Session: &Session{ID: r.Session.ID + "/" + kind}, ExtraSystem: extra, skills: r.skills, agents: r.agents}
	child.cp = r.cp
	child.loadOnce.Do(func() {})
	// The parent's MCP servers are reused rather than started again.
	child.mcp, child.sharedMCP = r.mcp, true
	child.mcpOnce.Do(func() {})
	if kind != agentExplore && kind != agentGeneral {
		for i := range r.agents {
			if r.agents[i].Name == kind {
				child.only = r.agents[i].Tools
			}
		}
	}
	if r.Sink != nil {
		child.Sink = func(e Event) {
			if e.Kind == EventDone {
				return
			}
			r.Sink(Event{Kind: EventSubagent, Tool: kind, Text: string(e.Kind), Summary: firstNonEmpty(e.Summary, oneLine(e.Text, 120)), Err: e.Err})
		}
	}
	return child, nil
}

func agentNames(defs []AgentDef) []string {
	var out []string
	for _, d := range defs {
		out = append(out, d.Name)
	}
	return out
}

// Close releases what a runner holds: background shells and MCP servers.
func (r *Runner) Close() {
	r.bg.Close()
	if r.sharedMCP {
		return
	}
	for _, c := range r.mcp {
		c.close()
	}
}
