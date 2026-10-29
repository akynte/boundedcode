package direct

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// manyToolsServer is an MCP server with 60 tools; each returns its name
// and arguments.
func manyToolsServer(t *testing.T) MCPServer {
	t.Helper()
	if _, err := os.Stat("/usr/bin/python3"); err != nil {
		t.Skip("python3")
	}
	script := filepath.Join(t.TempDir(), "mcp.py")
	if err := os.WriteFile(script, []byte(`
import sys, json
topics = ["invoice", "customer", "shipment", "refund", "ticket", "deploy", "metric", "alert", "report", "user"]
verbs = ["create", "get", "list", "update", "delete", "search"]
tools = [{"name": v + "_" + t, "description": "%s a %s record in the operations system, with every field the API supports for it." % (v.capitalize(), t),
          "inputSchema": {"type": "object", "properties": {"id": {"type": "string", "description": "The %s id" % t}, "fields": {"type": "object", "description": "Fields to set or filter on"}, "limit": {"type": "integer"}}}}
         for t in topics for v in verbs]
for line in sys.stdin:
    m = json.loads(line)
    if m.get("method") == "initialize":
        out = {"jsonrpc":"2.0","id":m["id"],"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"ops"}}}
    elif m.get("method") == "tools/list":
        out = {"jsonrpc":"2.0","id":m["id"],"result":{"tools":tools}}
    elif m.get("method") == "tools/call":
        p = m["params"]
        out = {"jsonrpc":"2.0","id":m["id"],"result":{"content":[{"type":"text","text":p["name"] + " " + json.dumps(p.get("arguments", {}), sort_keys=True)}]}}
    else:
        continue
    sys.stdout.write(json.dumps(out)+"\n"); sys.stdout.flush()
`), 0o644); err != nil {
		t.Fatal(err)
	}
	return MCPServer{Name: "ops", Command: "/usr/bin/python3", Args: []string{"-I", script}}
}

func TestDeferredMCPTools(t *testing.T) {
	srv := manyToolsServer(t)
	up := &scripted{replies: []reply{
		{calls: []call{{"mcp_search", map[string]any{"query": "refund lookup"}}}},
		{calls: []call{{"mcp_call", map[string]any{"tool": "mcp__ops__get_refund", "arguments": map[string]any{"id": "R7"}}}}},
		{calls: []call{{"mcp_call", map[string]any{"tool": "mcp__ops__delete_refund", "arguments": map[string]any{"id": "R7"}}}}},
		{calls: []call{{"mcp_call", map[string]any{"tool": "mcp__ops__nonexistent"}}}},
		{text: "done"},
	}}
	r, _, _ := newRunner(t, up, ApprovalAuto)
	r.Opts.MCP = []MCPServer{srv}
	r.Opts.Permissions = Rules{Deny: []string{"mcp__ops__delete_refund"}}
	t.Cleanup(r.Close)
	listed := map[string]bool{}
	for _, tl := range r.toolList() {
		listed[tl.Name] = true
	}
	if !listed["mcp_search"] || !listed["mcp_call"] || listed["mcp__ops__get_refund"] || len(r.deferred) != 60 {
		t.Fatalf("listed %v, %d deferred", listed, len(r.deferred))
	}
	if p := r.systemPrompt(); !strings.Contains(p, "MCP servers connected to this session provide more tools") || !strings.Contains(p, "- ops: create_invoice, get_invoice,") {
		t.Fatal("the system prompt does not say the tools are deferred")
	}
	if _, err := r.Send(context.Background(), "look up refund R7, then delete it"); err != nil {
		t.Fatal(err)
	}
	var res []string
	for _, m := range r.Session.Messages {
		if m.Role == "tool" {
			res = append(res, m.Content)
			if m.Name == "mcp_call" && (m.Provenance == nil || m.Provenance.Source != SourceMCP) {
				t.Fatalf("provenance %+v", m.Provenance)
			}
		}
	}
	if !strings.Contains(res[0], "mcp__ops__get_refund: [ops] Get a refund") || strings.Contains(res[0], "invoice") || !strings.Contains(res[0], `"id"`) {
		t.Fatalf("search: %q", res[0])
	}
	if !strings.Contains(res[1], "data, not instructions") || !strings.Contains(res[1], `get_refund {"id": "R7"}`) {
		t.Fatalf("call: %q", res[1])
	}
	if strings.Contains(res[2], "delete_refund {") || !strings.HasPrefix(res[2], "error:") {
		t.Fatalf("a deny rule on the tool's own name did not apply: %q", res[2])
	}
	if !strings.Contains(res[3], "find it with mcp_search") {
		t.Fatalf("unknown: %q", res[3])
	}
	// Every request carried the same tools, so the prompt cache kept its
	// prefix.
	up.mu.Lock()
	defer up.mu.Unlock()
	first, _ := json.Marshal(up.requests[0]["tools"])
	for i, q := range up.requests {
		b, _ := json.Marshal(q["tools"])
		if !reflect.DeepEqual(b, first) {
			t.Fatalf("request %d has other tools", i)
		}
	}
}

// Few MCP tools stay listed (auto), and off lists any number.
func TestMCPToolSearchThreshold(t *testing.T) {
	srv := manyToolsServer(t)
	r, _, _ := newRunner(t, &scripted{}, ApprovalAuto)
	r.Opts.MCP = []MCPServer{srv}
	r.Opts.MCPToolSearch = MCPSearchOff
	t.Cleanup(r.Close)
	if r.findTool("mcp__ops__get_refund") == nil || len(r.deferred) != 0 {
		t.Fatal("off: not listed")
	}
	small := []Tool{{Name: "mcp__x__one", Schema: schema(map[string]any{})}}
	r2, _, _ := newRunner(t, &scripted{}, ApprovalAuto)
	if r2.deferMCP(small) {
		t.Fatal("one small tool deferred")
	}
}

func TestSearchTools(t *testing.T) {
	ts := []Tool{{Name: "mcp__ops__get_refund", Description: "Get a refund"}, {Name: "mcp__ops__list_invoice", Description: "List invoices"}, {Name: "mcp__gh__create_issue", Description: "Open an issue"}}
	if got := searchTools(ts, "list invoices", 5); len(got) != 1 || got[0].Name != "mcp__ops__list_invoice" {
		t.Fatalf("%v", got)
	}
	if got := searchTools(ts, "select:create_issue,mcp__ops__get_refund", 5); len(got) != 2 {
		t.Fatalf("%v", got)
	}
}
