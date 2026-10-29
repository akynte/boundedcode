package direct

import (
	"context"
	"testing"

	"github.com/akynte/boundedcode/internal/capability"
)

// Every tool a runner can offer is described, and the descriptions agree
// with how the runner treats the tool: a concurrent tool changes nothing,
// and a tool that is approved as a change says it writes or executes.
func TestEveryToolIsDescribed(t *testing.T) {
	r, _, _ := newRunner(t, &scripted{}, ApprovalAuto)
	r.Nav = fakeNav{}
	r.Asker = func(context.Context, string, []string) (string, error) { return "", nil }
	r.Opts.MemoryFile = t.TempDir() + "/AGENTS.md"
	ts := append(r.toolList(), skillTool(), mcpSearchTool(), mcpCallTool())
	for i := range ts {
		tl := &ts[i]
		d := describe(tl)
		if d.Source == "unknown" {
			t.Errorf("%s is not described (builtinCapabilities)", tl.Name)
			continue
		}
		if d.Concurrent && (!d.ReadOnly() || tl.Mutates || tl.Consent != nil && d.Effects.Has(capability.Write)) {
			t.Errorf("%s is concurrent but changes something (%s)", tl.Name, d.Effects)
		}
		if tl.Mutates && d.Effects&(capability.Write|capability.Exec) == 0 {
			t.Errorf("%s mutates but its effects are %s", tl.Name, d.Effects)
		}
	}
	if d := describe(&Tool{Name: "mcp__srv__x"}); d.Concurrent || d.Source != "mcp:srv" {
		t.Errorf("MCP tool %+v", d)
	}
}

func TestBoundedActions(t *testing.T) {
	for _, c := range []struct {
		tool, cmd string
		want      capability.Effect
	}{
		{"file_editor", "view", capability.Read},
		{"file_editor", "str_replace", capability.Write},
		{"file_editor", "create", capability.Write},
		{"file_editor", "", 0},
		{"terminal", "go test ./...", capability.Exec},
		{"task_tracker", "", capability.State},
		{"think", "", 0},
		{"unknown_tool", "", 0},
	} {
		if got := capability.BoundedAction(c.tool, c.cmd); got != c.want {
			t.Errorf("%s %q: %s, want %s", c.tool, c.cmd, got, c.want)
		}
	}
}
