package task

// Structured output that did not finish must not become task state.
//
// Django's judged arm died at LOCALIZE with "structured output was
// truncated" — a terminal failure for an answer the model could have given
// again, shorter. The recovery is a targeted retry under a tighter bound,
// never a continuation of malformed JSON.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/llm"
)

type probeOut struct {
	Files   []string `json:"files"`
	Symbols []string `json:"symbols"`
}

func TestCompleteStructuredOutputIsAccepted(t *testing.T) {
	resp := &llm.ChatResponse{
		Content:      `{"files":["a.py"],"symbols":["f"]}`,
		FinishReason: "stop",
	}
	if bad, why := structuredIncomplete(resp, &probeOut{}); bad {
		t.Errorf("a complete object was refused: %s", why)
	}
}

func TestTruncatedOutputIsRefused(t *testing.T) {
	// Both halves of the failure: the provider says it ran out, and the
	// bytes are half an object.
	resp := &llm.ChatResponse{
		Content:      `{"files":["a.py","b.py`,
		FinishReason: "length",
	}
	bad, why := structuredIncomplete(resp, &probeOut{})
	if !bad {
		t.Fatal("truncated output was accepted")
	}
	if !strings.Contains(why, "truncated") {
		t.Errorf("the reason does not say it was truncated: %s", why)
	}
}

// A provider that reports a clean stop but emits invalid JSON must be caught
// too, or partial state enters the task on the provider's word.
func TestSchemaInvalidOutputIsRefused(t *testing.T) {
	resp := &llm.ChatResponse{Content: `{"files": "not-an-array"}`, FinishReason: "stop"}
	bad, why := structuredIncomplete(resp, &probeOut{})
	if !bad {
		t.Fatal("output that does not fit the target was accepted")
	}
	if !strings.Contains(why, "did not parse") {
		t.Errorf("the reason should say it did not parse: %s", why)
	}
}

// Nothing is written into the caller's object by the check itself: a refused
// answer must leave the previous state exactly as it was.
func TestRefusedOutputDoesNotMutateState(t *testing.T) {
	out := &probeOut{Files: []string{"kept.py"}}
	resp := &llm.ChatResponse{Content: `{"files":["new.py"`, FinishReason: "length"}
	if bad, _ := structuredIncomplete(resp, out); !bad {
		t.Fatal("truncated output was accepted")
	}
	if len(out.Files) != 1 || out.Files[0] != "kept.py" {
		t.Errorf("the refused answer changed task state: %+v", out)
	}
}

// The retry reduces output pressure without relaxing the contract.
func TestTightenSchemaBoundsListsButKeepsTheContract(t *testing.T) {
	base := json.RawMessage(`{"type":"object","properties":{` +
		`"files":{"type":"array","items":{"type":"string"}},` +
		`"hypothesis":{"type":"string"}},` +
		`"required":["files","hypothesis"],"additionalProperties":false}`)
	var got map[string]any
	if err := json.Unmarshal(tightenSchema(base), &got); err != nil {
		t.Fatal(err)
	}
	props := got["properties"].(map[string]any)
	files := props["files"].(map[string]any)
	if _, ok := files["maxItems"]; !ok {
		t.Error("the array was not bounded, so the retry has the same output pressure")
	}
	if props["hypothesis"].(map[string]any)["type"] != "string" {
		t.Error("a property changed type")
	}
	req, _ := json.Marshal(got["required"])
	if string(req) != `["files","hypothesis"]` {
		t.Errorf("the required set changed: %s", req)
	}
	if got["additionalProperties"] != false {
		t.Error("the retry relaxed additionalProperties")
	}
}

func TestStructuredRetriesAreBounded(t *testing.T) {
	if maxStructuredRetries < 1 || maxStructuredRetries > 3 {
		t.Errorf("the structured retry budget is %d; it must be small and non-zero",
			maxStructuredRetries)
	}
}

// The retry instruction must ask for a whole object, never a continuation.
func TestRetryInstructionForbidsContinuation(t *testing.T) {
	msg := structuredRetryInstruction("was truncated at the output limit")
	for _, want := range []string{"complete", "Do not continue"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the instruction is missing %q: %s", want, msg)
		}
	}
}
