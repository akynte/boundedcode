package task

import (
	"encoding/json"
	"errors"
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

func TestImplementationChoicesAreNotMaterial(t *testing.T) {
	a := Ambiguity{Question: "map or slice?", Interpretations: []string{"map", "slice"}, Affects: "implementation", Material: true}
	if a.IsMaterial() {
		t.Fatal("an implementation choice was material")
	}
	a.Affects = "implementation and observable behaviour"
	if !a.IsMaterial() {
		t.Fatal("a behaviour change was not material")
	}
}

func TestParseContractNothingRequired(t *testing.T) {
	if _, err := ParseContract(`{"required":[],"acceptance_evidence":["x"]}`); !errors.Is(err, ErrNothingRequired) {
		t.Fatalf("err = %v", err)
	}
	// The derivation cannot pre-empt the grounding check.
	c, err := ParseContract(`{"required":["x"],"unknown_or_ambiguous":[{"question":"q","interpretations":["a","b"],"affects":"behaviour","material":true,"grounding":{"demoted":"model said so"}}]}`)
	if err != nil || c.Ambiguities[0].Grounding != nil || len(c.Material()) != 1 {
		t.Fatalf("contract = %+v err=%v", c, err)
	}
}

func TestContractWithoutGroundingUnmarshals(t *testing.T) {
	var c Contract
	old := `{"required":["x"],"unknown_or_ambiguous":[{"question":"q","interpretations":["a","b"],"affects":"behaviour","material":true}]}`
	if err := json.Unmarshal([]byte(old), &c); err != nil || c.Ambiguities[0].Grounding != nil || len(c.Material()) != 1 {
		t.Fatalf("contract = %+v err=%v", c, err)
	}
}

func TestGround(t *testing.T) {
	const req = "When the body exceeds maxContentLength,\n  the request should be REJECTED with an error.\nExpected: both return 'object object'."
	amb := Ambiguity{Question: "q", Interpretations: []string{"one", "two"}, Affects: "behaviour", Material: true}
	cases := []struct {
		name    string
		ans     GroundingAnswer
		demoted string // a substring of the reason; "" stays material
	}{
		{"settled, quote found despite case, whitespace and quote marks", GroundingAnswer{Settled: true, SettledBy: "  “the request should be rejected with an error”"}, "settled by the request"},
		{"settled, quote not in the request", GroundingAnswer{Settled: true, SettledBy: "the request returns HTTP 400",
			Readings: []string{"exceeds maxContentLength", "should be REJECTED with an error"}}, ""},
		{"settled, quote too short", GroundingAnswer{Settled: true, SettledBy: "'object'",
			Readings: []string{"exceeds maxContentLength", "both return 'object object'"}}, ""},
		{"both readings grounded", GroundingAnswer{Readings: []string{"exceeds maxContentLength", "both return 'object object'"}}, ""},
		{"one reading invented", GroundingAnswer{Readings: []string{"exceeds maxContentLength", "return HTTP 400"}}, "1 of 2"},
		{"one reading missing", GroundingAnswer{Readings: []string{"exceeds maxContentLength"}}, "1 of 2"},
		{"both readings too short", GroundingAnswer{Readings: []string{"error", "object"}}, "0 of 2"},
		{"settling quote is a reading's own support", GroundingAnswer{Settled: true, SettledBy: "the request should be REJECTED",
			Readings: []string{"exceeds maxContentLength,\n the request should be REJECTED with an error", "both return 'object object'"}}, ""},
	}
	for _, tc := range cases {
		g := Ground(req, amb, tc.ans)
		a := amb
		a.Grounding = &g
		switch {
		case tc.demoted == "" && (g.Demoted != "" || !a.IsMaterial()):
			t.Errorf("%s: demoted %q", tc.name, g.Demoted)
		case tc.demoted != "" && (!strings.Contains(g.Demoted, tc.demoted) || a.IsMaterial()):
			t.Errorf("%s: demoted %q, want %q", tc.name, g.Demoted, tc.demoted)
		}
	}
}

func TestParseGrounding(t *testing.T) {
	p, err := ParseGrounding("```json\n" + `{"points":[{"id":1,"settled":true,"settled_by":"x","readings":["a",""]}]}` + "\n```")
	if err != nil || len(p) != 1 || !p[0].Settled || len(p[0].Readings) != 2 {
		t.Fatalf("points = %+v err=%v", p, err)
	}
	for _, bad := range []string{"no json", `{"points":[]}`, `{"points":[{"id":"x"}]}`} {
		if _, err := ParseGrounding(bad); err == nil {
			t.Errorf("%q: expected an error", bad)
		}
	}
}

func TestStripHTMLComments(t *testing.T) {
	in := "<!--\n Thanks for reporting! Please fill in:\n-->\n\nAdd a --json flag.\n\n<!-- version -->\n\n\nDetails <!-- inline --> here."
	if got := StripHTMLComments(in); got != "Add a --json flag.\n\nDetails  here." {
		t.Fatalf("got %q", got)
	}
	if got := StripHTMLComments("plain\n\n\ntext"); got != "plain\n\n\ntext" {
		t.Fatalf("text without comments changed: %q", got)
	}
}

// TestGroundLongQuote: a verbatim quote longer than what is stored still
// matches (it is truncated for storage only).
func TestGroundLongQuote(t *testing.T) {
	long := strings.Repeat("the importer must keep every original column order intact ", 8)
	req := "Request: " + long + "\nAlso: the exporter may sort the columns alphabetically instead."
	amb := Ambiguity{Question: "q", Interpretations: []string{"keep", "sort"}, Affects: "output", Material: true}
	g := Ground(req, amb, GroundingAnswer{Readings: []string{long, "the exporter may sort the columns alphabetically"}})
	if g.Grounded != 2 || g.Demoted != "" {
		t.Fatalf("long verbatim quote not matched: %+v", g)
	}
	if len(g.Readings[0]) > 300+len("…") {
		t.Fatalf("stored quote not bounded: %d bytes", len(g.Readings[0]))
	}
}

func TestGroundingAnswerComplete(t *testing.T) {
	if (GroundingAnswer{Readings: []string{"a"}}).Complete(2) || !(GroundingAnswer{Readings: []string{"", ""}}).Complete(2) {
		t.Fatal("Complete must require one entry per interpretation")
	}
}
