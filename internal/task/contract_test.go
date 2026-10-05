package task

import (
	"strings"
	"testing"
)

func TestParseContract(t *testing.T) {
	c, err := ParseContract("Here it is:\n```json\n" + `{"required":["reject negative amounts"],"acceptable_alternatives":[{"point":"invalid input","options":["return an error","clamp to zero"]}],
"unknown_or_ambiguous":[{"question":"q","interpretations":["a","b"],"affects":"observable output","material":true},{"question":"wording","interpretations":["x","y"],"affects":"phrasing","material":true}]}` + "\n```")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Required) != 1 || len(c.Alternatives) != 1 || len(c.Material()) != 1 {
		t.Fatalf("contract = %+v (material %d)", c, len(c.Material()))
	}
	r := c.Render()
	for _, want := range []string{"required:", "return an error | clamp to zero", "unknown_or_ambiguous (material)"} {
		if !strings.Contains(r, want) {
			t.Errorf("render lacks %q:\n%s", want, r)
		}
	}
	for _, bad := range []string{"no json here", `{"required": []}`, `{"required": [1,2]`} {
		if _, err := ParseContract(bad); err == nil {
			t.Errorf("%q: expected an error", bad)
		}
	}
	// An "alternative" with one option is not an alternative.
	c, _ = ParseContract(`{"required":["x"],"acceptable_alternatives":[{"point":"p","options":["only one"]}]}`)
	if len(c.Alternatives) != 0 {
		t.Fatalf("single-option alternative kept: %+v", c.Alternatives)
	}
}
