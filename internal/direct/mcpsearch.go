package direct

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/contextplan"
)

// Many MCP servers can bring hundreds of tool definitions, all of them in
// every request. Past a share of the working set they are deferred: the
// model gets mcp_search, which returns matching tools' definitions as an
// ordinary result, and mcp_call, which runs one of them. The tools a
// request carries stay the same for the whole session, so the prompt
// cache keeps its prefix (adding definitions as they were found would
// change the system prompt the server renders them into). A deferred tool
// runs exactly as a listed one: the same trust pins, rules, approval,
// provenance and data header, under its own name.

// MCP tool search settings (direct.mcp_tool_search).
const (
	MCPSearchAuto = "auto" // defer when the definitions exceed a tenth of the working set
	MCPSearchOn   = "on"
	MCPSearchOff  = "off"
)

// mcpSearchShare is the share of the working set MCP definitions may take
// before they are deferred (auto).
const mcpSearchShare = 10

// deferMCP decides whether the MCP tools are deferred.
func (r *Runner) deferMCP(ts []Tool) bool {
	switch r.Opts.MCPToolSearch {
	case MCPSearchOff:
		return false
	case MCPSearchOn:
		return len(ts) > 0
	}
	if len(ts) == 0 {
		return false
	}
	return definitionTokens(ts) > r.workingSetTokens()/mcpSearchShare
}

// definitionTokens estimates what tool definitions cost in a request.
func definitionTokens(ts []Tool) int {
	b, _ := json.Marshal(toolSchemas(ts))
	return contextplan.EstimateTokens(string(b))
}

// ToolDefinitionTokens estimates what the tool definitions this runner
// sends cost in each request.
func (r *Runner) ToolDefinitionTokens() int { return definitionTokens(r.toolList()) }

// DeferredTools lists the MCP tools reached through mcp_search.
func (r *Runner) DeferredTools() []string {
	r.toolList()
	out := make([]string, 0, len(r.deferred))
	for _, t := range r.deferred {
		out = append(out, t.Name)
	}
	return out
}

// deferredTool finds a deferred MCP tool by name.
func (r *Runner) deferredTool(name string) *Tool {
	for i := range r.deferred {
		if r.deferred[i].Name == name {
			return &r.deferred[i]
		}
	}
	return nil
}

// mcpSearchNote tells the model, in the system prompt, which tools are
// deferred: each server's tool names, which cost a few tokens each where a
// definition costs a hundred, so the model knows what is there to search
// for (a count alone was not enough in bench mcpsearch).
func (r *Runner) mcpSearchNote() string {
	if len(r.Opts.MCP) == 0 {
		return ""
	}
	r.toolList() // decides what is deferred
	if len(r.deferred) == 0 {
		return ""
	}
	var servers []string
	names := map[string][]string{}
	for _, t := range r.deferred {
		s, tool, _ := strings.Cut(strings.TrimPrefix(t.Name, "mcp__"), "__")
		if _, ok := names[s]; !ok {
			servers = append(servers, s)
		}
		names[s] = append(names[s], tool)
	}
	sort.Strings(servers)
	var b strings.Builder
	b.WriteString("MCP servers connected to this session provide more tools, not listed with the others to save space. When a request concerns what one of them serves, use it rather than searching the project: find the tool with mcp_search (a few words, or select:NAME) to get its arguments, then run it with mcp_call.\n")
	const maxNames = 150
	shown := 0
	for _, s := range servers {
		ns := names[s]
		if shown+len(ns) > maxNames {
			fmt.Fprintf(&b, "- %s: %d tools\n", s, len(ns))
			continue
		}
		shown += len(ns)
		fmt.Fprintf(&b, "- %s: %s\n", s, strings.Join(ns, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
}

func mcpSearchTool() Tool {
	return Tool{
		Name:        "mcp_search",
		Description: "Find tools of the connected MCP servers that are not listed: give a few words of what you need (or select:NAME,NAME for exact names). Returns each match's name, description and arguments; run one with mcp_call.",
		Schema: schema(map[string]any{
			"query": prop("string", "What the tool should do, or select:NAME,NAME"),
			"limit": prop("integer", "At most this many tools (default 5)"),
		}, "query"),
		Summary: func(a map[string]any) string { return "MCP tools: " + str(a, "query") },
		Run: func(_ context.Context, r *Runner, a map[string]any) (string, error) {
			limit := num(a, "limit")
			if limit <= 0 {
				limit = 5
			}
			found := searchTools(r.deferred, str(a, "query"), min(limit, 20))
			if len(found) == 0 {
				return fmt.Sprintf("No MCP tool matches %q. %d are available; try other words.", str(a, "query"), len(r.deferred)), nil
			}
			var b strings.Builder
			for _, t := range found {
				sch, _ := json.Marshal(t.Schema)
				fmt.Fprintf(&b, "%s: %s\narguments: %s\n\n", t.Name, t.Description, sch)
			}
			b.WriteString(`Run one with mcp_call {"tool": NAME, "arguments": {...}}.`)
			return b.String(), nil
		},
	}
}

func mcpCallTool() Tool {
	return Tool{
		Name:        "mcp_call",
		Description: "Run a tool of a connected MCP server found with mcp_search.",
		Schema: schema(map[string]any{
			"tool":      prop("string", "The tool's name, as mcp_search gives it"),
			"arguments": map[string]any{"type": "object", "description": "The tool's arguments"},
		}, "tool"),
		Summary: func(a map[string]any) string { return "MCP " + str(a, "tool") },
		// prepareTool runs the named tool in its place.
		Run: func(context.Context, *Runner, map[string]any) (string, error) {
			return "", fmt.Errorf("mcp_call runs the tool it names")
		},
	}
}

// unwrapMCPCall is the tool and arguments an mcp_call names.
func (r *Runner) unwrapMCPCall(call ToolCall) (name, args string, err error) {
	var a struct {
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(call.Function.Arguments), &a); err != nil {
		return "", "", fmt.Errorf("arguments are not a JSON object: %w", err)
	}
	if r.deferredTool(a.Tool) == nil {
		return "", "", fmt.Errorf("no MCP tool %q: find it with mcp_search first", a.Tool)
	}
	args = string(a.Arguments)
	if strings.TrimSpace(args) == "" || args == "null" {
		args = "{}"
	}
	return a.Tool, args, nil
}

// searchTools ranks tools by the words of query found in their names and
// descriptions; select:A,B picks by name.
func searchTools(ts []Tool, query string, limit int) []Tool {
	q := strings.ToLower(strings.TrimSpace(query))
	if names, ok := strings.CutPrefix(q, "select:"); ok {
		var out []Tool
		for _, n := range strings.Split(names, ",") {
			for _, t := range ts {
				if strings.EqualFold(t.Name, strings.TrimSpace(n)) || strings.HasSuffix(strings.ToLower(t.Name), "__"+strings.TrimSpace(n)) {
					out = append(out, t)
				}
			}
		}
		return out
	}
	words := strings.FieldsFunc(q, func(r rune) bool { return (r < 'a' || r > 'z') && (r < '0' || r > '9') })
	type scored struct {
		t     Tool
		score int
	}
	var hits []scored
	for _, t := range ts {
		name := strings.ToLower(t.Name)
		desc := strings.ToLower(t.Description)
		score := 0
		for _, w := range words {
			if len(w) < 2 {
				continue
			}
			if strings.Contains(name, w) {
				score += 3
			}
			if strings.Contains(desc, w) {
				score++
			}
		}
		if score > 0 {
			hits = append(hits, scored{t, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	var out []Tool
	for _, h := range hits {
		if len(out) == limit {
			break
		}
		out = append(out, h.t)
	}
	return out
}
