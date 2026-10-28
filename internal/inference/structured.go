package inference

import (
	"encoding/json"
	"strings"
)

// Structured output: a caller that needs JSON sends the OpenAI shape,
//
//	"response_format": {"type": "json_schema", "json_schema": {"name": ..., "schema": ..., "strict": true}}
//
// and each upstream turns it into what its API takes: llama.cpp and
// OpenAI-compatible servers take it as is (llama.cpp compiles the schema
// to a grammar), Anthropic as output_config.format, Gemini as
// generationConfig.responseJsonSchema. A provider that rejects it is asked
// again without it, and the caller's prompt, which still describes the
// JSON, is all that shapes the answer, as before.

// JSONSchemaFormat returns the response_format for schema. The schema
// should be strict-compatible: every property required and
// "additionalProperties": false on every object, as OpenAI's strict mode
// and Anthropic require. Pass it as json.RawMessage to keep its property
// order: llama.cpp's grammar writes properties in the order the schema
// lists them, which is the order the model then generates them in.
func JSONSchemaFormat(name string, schema any) map[string]any {
	return map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": name, "schema": schema, "strict": true}}
}

// oaResponseFormat is the response_format of a request.
type oaResponseFormat struct {
	Type       string `json:"type"`
	JSONSchema *struct {
		Name   string         `json:"name"`
		Schema map[string]any `json:"schema"`
	} `json:"json_schema"`
}

// schema returns the requested JSON schema, or nil.
func (r oaRequest) schema() map[string]any {
	if r.ResponseFormat == nil || r.ResponseFormat.Type != "json_schema" || r.ResponseFormat.JSONSchema == nil {
		return nil
	}
	return r.ResponseFormat.JSONSchema.Schema
}

// formatRejected reports a 400 that blames the requested output format,
// so the request is sent again without it.
func formatRejected(resp []byte) bool {
	m := strings.ToLower(errMessage(resp))
	for _, k := range []string{"output_config", "output_format", "response_format", "responsejsonschema", "response_json_schema", "responseschema", "json schema", "json_schema", "schema"} {
		if strings.Contains(m, k) {
			return true
		}
	}
	return false
}

// withoutFormat removes the structured-output fields a translator added.
func withoutFormat(payload map[string]any) {
	if oc, ok := payload["output_config"].(map[string]any); ok {
		delete(oc, "format")
		if len(oc) == 0 {
			delete(payload, "output_config")
		}
	}
	if gen, ok := payload["generationConfig"].(map[string]any); ok {
		delete(gen, "responseJsonSchema")
		delete(gen, "responseMimeType")
	}
}

// cloneJSON deep-copies a JSON value (schemas are shared by callers).
func cloneJSON(v map[string]any) map[string]any {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out map[string]any
	if json.Unmarshal(b, &out) != nil {
		return v
	}
	return out
}
