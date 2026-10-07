package orchestrator

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/agent"
)

func TestAgentEventDataSummarizesActions(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"kind": "ActionEvent", "tool": "terminal",
		"thought": "Run the tests", "action": `{"command":"go test ./...","kind":"TerminalAction"}`})
	d := agentEventData(agent.Event{Kind: "ActionEvent", Tool: "terminal", Raw: raw})
	if d["tool"] != "terminal" || d["thought"] != "Run the tests" || !strings.Contains(d["action"].(string), "go test ./...") {
		t.Fatalf("data = %v", d)
	}

	long := strings.Repeat("x", 1000)
	raw, _ = json.Marshal(map[string]any{"action": long})
	if a := agentEventData(agent.Event{Kind: "ActionEvent", Raw: raw})["action"].(string); len(a) > 310 {
		t.Fatalf("action not truncated: %d bytes", len(a))
	}

	d = agentEventData(agent.Event{Kind: "ObservationEvent", Tool: "terminal", Text: "secret output", IsError: true})
	if d["is_error"] != true || d["text"] != nil {
		t.Fatalf("observation data = %v (output must not be recorded)", d)
	}
	if d := agentEventData(agent.Event{Kind: "ActionEvent", Raw: json.RawMessage(`not json`)}); len(d) != 1 {
		t.Fatalf("malformed raw event = %v", d)
	}
}
