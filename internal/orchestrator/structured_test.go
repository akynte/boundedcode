package orchestrator

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/task"
)

// The request-reading calls carry their reply's JSON schema as structured
// output when it is on, in the order the prompt lists the fields (the
// order llama.cpp's grammar makes the model write them).
func TestReaderBodySchema(t *testing.T) {
	call := ContractDerivation("Fix the crash in parse().")
	off := ReaderBody(call, 1500, 0.2, false)
	if _, has := off["response_format"]; has {
		t.Fatalf("schema sent with structured output off: %v", off)
	}
	on := ReaderBody(call, 1500, 0.2, true)
	b, err := json.Marshal(on)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"response_format":{"json_schema":{"name":"contract"`) {
		t.Fatalf("no response_format: %s", s)
	}
	if strings.Index(s, `"required":{"type":"array"`) > strings.Index(s, `"acceptable_alternatives":{`) {
		t.Fatalf("schema properties lost their order: %s", s)
	}
	for _, sc := range []json.RawMessage{task.ContractSchema, task.GroundingSchema, counterexampleSchema} {
		var v map[string]any
		if err := json.Unmarshal(sc, &v); err != nil {
			t.Fatalf("schema does not parse: %v", err)
		}
		strictObjects(t, v)
	}
}

// strictObjects checks what OpenAI's strict mode and Anthropic require of
// every object in a schema: no additional properties, every property
// required.
func strictObjects(t *testing.T, v map[string]any) {
	t.Helper()
	if v["type"] == "object" {
		if v["additionalProperties"] != false {
			t.Errorf("object allows additional properties: %v", v)
		}
		props, _ := v["properties"].(map[string]any)
		req, _ := v["required"].([]any)
		if len(req) != len(props) {
			t.Errorf("not every property is required: %v", v)
		}
		for _, p := range props {
			strictObjects(t, p.(map[string]any))
		}
	}
	if items, ok := v["items"].(map[string]any); ok {
		strictObjects(t, items)
	}
}

func TestParseCounterexamples(t *testing.T) {
	files, ok := ParseCounterexamples("Here:\n```json\n{\"tests\":[{\"path\":\"a_test.go\",\"content\":\"package a\"},{\"path\":\"\",\"content\":\"x\"},{\"path\":\"c_test.go\",\"content\":\"package c\"}]}\n```")
	if !ok || len(files) != 1 || files["a_test.go"] != "package a" {
		t.Fatalf("%v %v", files, ok)
	}
	for _, bad := range []string{"no json", `{"tests":[]}`, `{"tests":[{"path":"x","content":"unterminated}]}`} {
		if _, ok := ParseCounterexamples(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}
