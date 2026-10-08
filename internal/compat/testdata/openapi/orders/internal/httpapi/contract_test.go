package httpapi

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestRoutesAreInTheSpec checks every served route against the published
// specification in the api repository (a sibling checkout).
func TestRoutesAreInTheSpec(t *testing.T) {
	b, err := os.ReadFile("../../../api/openapi.json")
	if err != nil {
		t.Skip("api checkout not found:", err)
	}
	var spec struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatal(err)
	}
	for _, r := range Routes() {
		if _, ok := spec.Paths[r.Path][strings.ToLower(r.Method)]; !ok {
			t.Errorf("%s %s is not in the specification", r.Method, r.Path)
		}
	}
}
