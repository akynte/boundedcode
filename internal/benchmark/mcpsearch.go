package benchmark

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/direct"
)

// MCPSearchBench measures deferred MCP tool definitions (direct.
// mcp_tool_search) with the agent's model: requests that each need one of
// 60 tools of an MCP server, with every definition listed (off) or found
// through mcp_search and run with mcp_call (on). A case passes when the
// right tool ran with the right id and the answer reports what it
// returned.
type MCPSearchBench struct {
	// NewRunner builds a direct runner for root with the MCP servers and
	// the mcp_tool_search setting.
	NewRunner func(ctx context.Context, root string, servers []direct.MCPServer, search string) (*direct.Runner, func(), error)
	Arms      []string
	Runs      int
	Model     string
	Progress  func(string)
}

// MCPSearchCase is one request.
type MCPSearchCase struct {
	ID, Prompt string
	// Tool and ID are the call that answers it; Expect is in the answer.
	Tool, Arg, Expect string
}

// MCPSearchCases are the requests, each answered by one tool.
func MCPSearchCases() []MCPSearchCase {
	return []MCPSearchCase{
		{"refund-status", "What is the status of refund R7 in the operations system?", "get_refund", "R7", "approved"},
		{"shipment-location", "Where is shipment S-1009 now, according to the operations system?", "get_shipment", "S-1009", "Rotterdam"},
		{"delete-alert", "Delete alert A-77 in the operations system, then tell me it is done.", "delete_alert", "A-77", "deleted"},
		{"metric-value", "What is the current value of metric M-cpu in the operations system?", "get_metric", "M-cpu", "73"},
		{"user-email", "Look up user U-5 in the operations system: what is their email address?", "get_user", "U-5", "dana@example.com"},
		{"invoice-total", "What is the total of invoice INV-204 in the operations system?", "get_invoice", "INV-204", "1,240.00"},
	}
}

// opsServer is the MCP server: 60 tools (six verbs on ten record types),
// each answering the cases' calls with a fixed text.
const opsServer = `
import sys, json
topics = ["invoice", "customer", "shipment", "refund", "ticket", "deploy", "metric", "alert", "report", "user"]
verbs = ["create", "get", "list", "update", "delete", "search"]
tools = [{"name": v + "_" + t, "description": "%s %s records in the operations system." % (v.capitalize(), t),
          "inputSchema": {"type": "object", "properties": {"id": {"type": "string", "description": "The %s id" % t},
                          "fields": {"type": "object", "description": "Fields to set, or to filter on"},
                          "limit": {"type": "integer", "description": "At most this many records"}}}}
         for t in topics for v in verbs]
answers = {("get_refund", "R7"): "refund R7: status=approved, amount=12.50 EUR",
           ("get_shipment", "S-1009"): "shipment S-1009: location=Rotterdam, eta=2026-11-02",
           ("delete_alert", "A-77"): "alert A-77 deleted",
           ("get_metric", "M-cpu"): "metric M-cpu: value=73%, unit=percent",
           ("get_user", "U-5"): "user U-5: name=Dana, email=dana@example.com",
           ("get_invoice", "INV-204"): "invoice INV-204: total=1,240.00 EUR, status=paid"}
for line in sys.stdin:
    m = json.loads(line)
    if m.get("method") == "initialize":
        out = {"jsonrpc":"2.0","id":m["id"],"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"ops"}}}
    elif m.get("method") == "tools/list":
        out = {"jsonrpc":"2.0","id":m["id"],"result":{"tools":tools}}
    elif m.get("method") == "tools/call":
        p = m["params"]; a = p.get("arguments") or {}
        text = answers.get((p["name"], str(a.get("id", ""))), "%s: no record %s" % (p["name"], a.get("id", "")))
        out = {"jsonrpc":"2.0","id":m["id"],"result":{"content":[{"type":"text","text":text}]}}
    else:
        continue
    sys.stdout.write(json.dumps(out)+"\n"); sys.stdout.flush()
`

// MCPSearchOutcome is one case in one arm.
type MCPSearchOutcome struct {
	Case       string   `json:"case"`
	Arm        string   `json:"arm"`
	Run        int      `json:"run"`
	Success    bool     `json:"success"`
	RightTool  bool     `json:"right_tool"`
	ModelCalls int      `json:"model_calls"`
	ToolCalls  int      `json:"tool_calls"`
	Tools      []string `json:"tools"`
	// PromptTokens is the prompts' size; Evaluated what the server
	// processed of them (the rest came from its cache).
	PromptTokens int     `json:"prompt_tokens"`
	Evaluated    int     `json:"evaluated_tokens"`
	Definitions  int     `json:"definition_tokens"`
	Seconds      float64 `json:"seconds"`
	Answer       string  `json:"answer"`
	Error        string  `json:"error,omitempty"`
}

// MCPSearchReport is the bench's result.
type MCPSearchReport struct {
	Started  time.Time          `json:"started"`
	Model    string             `json:"model"`
	Outcomes []MCPSearchOutcome `json:"outcomes"`
}

// Run runs every case in every arm, Runs times, interleaved.
func (b *MCPSearchBench) Run(ctx context.Context) (*MCPSearchReport, error) {
	rep := &MCPSearchReport{Started: time.Now().UTC(), Model: b.Model}
	dir, err := os.MkdirTemp("", "bc-mcpsearch-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	script := filepath.Join(dir, "ops.py")
	if err := os.WriteFile(script, []byte(opsServer), 0o644); err != nil {
		return nil, err
	}
	servers := []direct.MCPServer{{Name: "ops", Command: "python3", Args: []string{"-I", script}}}
	for run := 1; run <= max(b.Runs, 1); run++ {
		for _, c := range MCPSearchCases() {
			for _, arm := range b.Arms {
				if err := ctx.Err(); err != nil {
					return rep, err
				}
				o := b.runCase(ctx, c, arm, run, dir, servers)
				rep.Outcomes = append(rep.Outcomes, o)
				if b.Progress != nil {
					b.Progress(fmt.Sprintf("%s %s r%d: success=%v right_tool=%v calls=%d tools=%v %.0fs %s", c.ID, arm, run, o.Success, o.RightTool, o.ModelCalls, o.Tools, o.Seconds, o.Error))
				}
			}
		}
	}
	return rep, nil
}

func (b *MCPSearchBench) runCase(ctx context.Context, c MCPSearchCase, arm string, run int, dir string, servers []direct.MCPServer) MCPSearchOutcome {
	o := MCPSearchOutcome{Case: c.ID, Arm: arm, Run: run}
	root := filepath.Join(dir, fmt.Sprintf("%s-%s-%d", c.ID, arm, run))
	if err := os.MkdirAll(root, 0o755); err != nil {
		o.Error = err.Error()
		return o
	}
	r, done, err := b.NewRunner(ctx, root, servers, arm)
	if err != nil {
		o.Error = err.Error()
		return o
	}
	defer done()
	r.Gateway.KeepLog = true
	want := "mcp__ops__" + c.Tool
	inner := r.Sink
	r.Sink = func(e direct.Event) {
		if inner != nil {
			inner(e)
		}
		if e.Kind == direct.EventToolStart {
			o.ToolCalls++
			o.Tools = append(o.Tools, e.Tool)
			if e.Tool == want && fmt.Sprint(e.Args["id"]) == c.Arg {
				o.RightTool = true
			}
		}
	}
	o.Definitions = r.ToolDefinitionTokens()
	start := time.Now()
	answer, err := r.Send(ctx, c.Prompt)
	o.Seconds = time.Since(start).Seconds()
	if err != nil {
		o.Error = err.Error()
	}
	o.Answer = answer
	for _, cs := range r.Gateway.CallLog() {
		o.ModelCalls++
		o.PromptTokens += cs.Prompt
		o.Evaluated += cs.Evaluated
	}
	o.Success = o.RightTool && strings.Contains(answer, c.Expect)
	return o
}

// WriteMCPSearchMarkdown writes the report's summary per arm and its
// outcomes.
func WriteMCPSearchMarkdown(w io.Writer, r *MCPSearchReport) error {
	fmt.Fprintf(w, "# MCP tool search\n\nModel `%s`, started %s. %d requests, each needing one of 60 tools of an MCP server.\n\n", r.Model, r.Started.Format(time.RFC3339), len(MCPSearchCases()))
	type agg struct {
		n, ok, right, calls, tools, prompt, eval, defs int
		secs                                           []float64
	}
	arms := map[string]*agg{}
	var order []string
	for _, o := range r.Outcomes {
		a := arms[o.Arm]
		if a == nil {
			a = &agg{}
			arms[o.Arm] = a
			order = append(order, o.Arm)
		}
		a.n++
		if o.Success {
			a.ok++
		}
		if o.RightTool {
			a.right++
		}
		a.calls += o.ModelCalls
		a.tools += o.ToolCalls
		a.prompt += o.PromptTokens
		a.eval += o.Evaluated
		a.defs = o.Definitions
		a.secs = append(a.secs, o.Seconds)
	}
	fmt.Fprintln(w, "| mcp_tool_search | Passed | Right tool | Model calls per request | Tool calls per request | Tool definitions (tokens) | Prompt tokens per request | Processed by the server per request | Median s |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|---|")
	for _, k := range order {
		a := arms[k]
		sort.Float64s(a.secs)
		fmt.Fprintf(w, "| %s | %d / %d | %d / %d | %.1f | %.1f | %d | %d | %d | %.1f |\n", k, a.ok, a.n, a.right, a.n,
			float64(a.calls)/float64(a.n), float64(a.tools)/float64(a.n), a.defs, a.prompt/a.n, a.eval/a.n, a.secs[len(a.secs)/2])
	}
	fmt.Fprintln(w, "\n| Case | Arm | Run | Passed | Tools | Answer |\n|---|---|---|---|---|---|")
	for _, o := range r.Outcomes {
		ans := strings.ReplaceAll(strings.TrimSpace(o.Answer), "\n", " ")
		if len(ans) > 120 {
			ans = ans[:120] + "…"
		}
		if o.Error != "" {
			ans = "error: " + o.Error
		}
		fmt.Fprintf(w, "| %s | %s | %d | %v | %s | %s |\n", o.Case, o.Arm, o.Run, o.Success, strings.Join(o.Tools, ", "), strings.ReplaceAll(ans, "|", "\\|"))
	}
	return nil
}
