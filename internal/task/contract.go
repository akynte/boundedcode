package task

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Contract is a compact reading of what a request asks for, derived before
// implementation. It separates what is required from what the request
// leaves open, so an implementation of one explicitly allowed behaviour is
// not mistaken for a wrong one, and a genuine ambiguity is raised before the
// work rather than discovered in review. (In the 2026-10 runs a request that
// allowed two behaviours was implemented one way in some runs and the other
// way in others.)
type Contract struct {
	Required     []string      `json:"required"`
	Alternatives []Alternative `json:"acceptable_alternatives"`
	Constraints  []string      `json:"constraints"`
	NotRequired  []string      `json:"explicitly_not_required"`
	Ambiguities  []Ambiguity   `json:"unknown_or_ambiguous"`
	Evidence     []string      `json:"acceptance_evidence"`
}

// Alternative is a point where the request explicitly accepts more than one
// outcome. Any listed option satisfies it.
type Alternative struct {
	Point   string   `json:"point"`
	Options []string `json:"options"`
}

// Ambiguity is something the request does not settle.
type Ambiguity struct {
	Question        string   `json:"question"`
	Interpretations []string `json:"interpretations"`
	// Affects is what would differ between interpretations.
	Affects  string `json:"affects"`
	Material bool   `json:"material"`
}

// materialAreas are the consequences that make an ambiguity worth raising.
var materialAreas = []string{"implementation", "api", "behavio", "data", "schema", "security", "compatib", "test", "output", "error"}

// IsMaterial reports whether an ambiguity would change the work: it must be
// declared material, offer at least two interpretations, and affect
// behaviour, an API, data, security, compatibility, tests or output.
// Wording-level vagueness does not count.
func (a Ambiguity) IsMaterial() bool {
	if !a.Material || len(a.Interpretations) < 2 || strings.TrimSpace(a.Question) == "" {
		return false
	}
	af := strings.ToLower(a.Affects)
	for _, m := range materialAreas {
		if strings.Contains(af, m) {
			return true
		}
	}
	return false
}

// MaterialOrNone is Material on a possibly nil contract.
func (c *Contract) MaterialOrNone() []Ambiguity {
	if c == nil {
		return nil
	}
	return c.Material()
}

// Material returns the material ambiguities.
func (c Contract) Material() []Ambiguity {
	var out []Ambiguity
	for _, a := range c.Ambiguities {
		if a.IsMaterial() {
			out = append(out, a)
		}
	}
	return out
}

var jsonObjectRE = regexp.MustCompile(`(?s)\{.*\}`)

// ParseContract reads a contract from a model reply (the first JSON object
// in it, fenced or not) and bounds its size.
func ParseContract(reply string) (Contract, error) {
	var c Contract
	m := jsonObjectRE.FindString(reply)
	if m == "" {
		return c, errors.New("contract: no JSON object in the reply")
	}
	if err := json.Unmarshal([]byte(m), &c); err != nil {
		return c, fmt.Errorf("contract: %w", err)
	}
	clip := func(s []string) []string {
		var out []string
		for _, x := range s {
			if x = strings.TrimSpace(x); x != "" && len(out) < 8 {
				out = append(out, oneLineN(x, 300))
			}
		}
		return out
	}
	c.Required, c.Constraints, c.NotRequired, c.Evidence = clip(c.Required), clip(c.Constraints), clip(c.NotRequired), clip(c.Evidence)
	var alts []Alternative
	for _, a := range c.Alternatives {
		if o := clip(a.Options); len(o) >= 2 && len(alts) < 5 {
			alts = append(alts, Alternative{Point: oneLineN(a.Point, 200), Options: o})
		}
	}
	c.Alternatives = alts
	var amb []Ambiguity
	for _, a := range c.Ambiguities {
		if len(amb) < 5 {
			a.Question, a.Affects, a.Interpretations = oneLineN(a.Question, 300), oneLineN(a.Affects, 120), clip(a.Interpretations)
			amb = append(amb, a)
		}
	}
	c.Ambiguities = amb
	if len(c.Required) == 0 && len(c.Alternatives) == 0 {
		return c, errors.New("contract: nothing required")
	}
	return c, nil
}

// Render formats the contract for a context pack (compact, YAML-like).
func (c Contract) Render() string {
	var b strings.Builder
	list := func(title string, xs []string) {
		if len(xs) == 0 {
			return
		}
		b.WriteString(title + ":\n")
		for _, x := range xs {
			b.WriteString("  - " + x + "\n")
		}
	}
	list("required", c.Required)
	if len(c.Alternatives) > 0 {
		b.WriteString("acceptable_alternatives (any one option satisfies the point; say which you chose):\n")
		for _, a := range c.Alternatives {
			fmt.Fprintf(&b, "  - %s: %s\n", a.Point, strings.Join(a.Options, " | "))
		}
	}
	list("constraints", c.Constraints)
	list("explicitly_not_required", c.NotRequired)
	if m := c.Material(); len(m) > 0 {
		b.WriteString("unknown_or_ambiguous (material):\n")
		for _, a := range m {
			fmt.Fprintf(&b, "  - %s (%s): %s\n", a.Question, a.Affects, strings.Join(a.Interpretations, " | "))
		}
	}
	list("acceptance_evidence", c.Evidence)
	return strings.TrimRight(b.String(), "\n")
}

func oneLineN(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
