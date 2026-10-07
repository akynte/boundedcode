package task

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
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
	// Grounding is the check of a material ambiguity against the request
	// text (nil: not checked; contracts written before the check have none).
	Grounding *Grounding `json:"grounding,omitempty"`
}

// Grounding records whether the request itself settles an ambiguity or
// supports its readings. The model proposes quotes; whether a quote occurs
// in the request is decided here, not by the model.
type Grounding struct {
	// SettledBy is the request's own words that answer the question, when
	// the model said so and the words occur in the request.
	SettledBy string `json:"settled_by,omitempty"`
	// Readings are the model's supporting quotes, one per interpretation
	// ("" when it gave none).
	Readings []string `json:"reading_quotes,omitempty"`
	// Grounded counts the interpretations whose quote occurs in the request.
	Grounded int `json:"grounded_readings"`
	// Demoted says why the ambiguity is not material; empty if it stays so.
	Demoted string `json:"demoted,omitempty"`
}

// materialAreas are the consequences that make an ambiguity worth raising.
// Implementation choices are not among them: an engineer may make those
// freely (the contract prompt says so too).
var materialAreas = []string{"api", "behavio", "data", "schema", "security", "compatib", "test", "output", "error"}

// IsMaterial reports whether an ambiguity would change the work: it must be
// declared material, offer at least two interpretations, and affect
// behaviour, an API, data, security, compatibility, tests or output; and the
// grounding check, if it ran, must not have demoted it. Wording-level
// vagueness does not count.
func (a Ambiguity) IsMaterial() bool {
	if !a.Material || len(a.Interpretations) < 2 || strings.TrimSpace(a.Question) == "" {
		return false
	}
	if a.Grounding != nil && a.Grounding.Demoted != "" {
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

// ErrNothingRequired is a parseable contract that names nothing the request
// asks for.
var ErrNothingRequired = errors.New("contract: nothing required")

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
			a.Grounding = nil // set only by the grounding check, never by the derivation
			amb = append(amb, a)
		}
	}
	c.Ambiguities = amb
	if len(c.Required) == 0 && len(c.Alternatives) == 0 {
		return c, ErrNothingRequired
	}
	return c, nil
}

var (
	htmlCommentRE = regexp.MustCompile(`(?s)<!--.*?-->`)
	blankRunRE    = regexp.MustCompile(`\n[ \t]*(?:\n[ \t]*)+\n`)
)

// StripHTMLComments removes HTML comments from a request (an issue
// template's instructions, which are not part of what is asked) and the
// blank lines they leave.
func StripHTMLComments(s string) string {
	if !strings.Contains(s, "<!--") {
		return s
	}
	s = htmlCommentRE.ReplaceAllString(s, "")
	return strings.TrimSpace(blankRunRE.ReplaceAllString(s, "\n\n"))
}

// GroundingAnswer is the model's answer for one ambiguity in a grounding
// check (see Ground).
type GroundingAnswer struct {
	ID        int      `json:"id"`
	Settled   bool     `json:"settled"`
	SettledBy string   `json:"settled_by"`
	Readings  []string `json:"readings"`
}

// ParseGrounding reads the answers of a grounding check from a model reply
// (the first JSON object in it, fenced or not).
func ParseGrounding(reply string) ([]GroundingAnswer, error) {
	m := jsonObjectRE.FindString(reply)
	if m == "" {
		return nil, errors.New("grounding: no JSON object in the reply")
	}
	var g struct {
		Points []GroundingAnswer `json:"points"`
	}
	if err := json.Unmarshal([]byte(m), &g); err != nil {
		return nil, fmt.Errorf("grounding: %w", err)
	}
	if len(g.Points) == 0 {
		return nil, errors.New("grounding: no points in the reply")
	}
	return g.Points, nil
}

// minQuote is the shortest quote (in non-space characters) accepted as
// evidence; shorter ones occur in almost any request.
const minQuote = 12

// Ground decides from the model's answer whether an ambiguity stays
// material for this request. It is settled, and not material, when the
// model says the request answers it and the quoted answer occurs in the
// request. It is ungrounded, and not material, when fewer than two of its
// interpretations have a supporting quote that occurs in the request. A
// quote not found in the request counts as no quote.
func Ground(request string, a Ambiguity, ans GroundingAnswer) Grounding {
	var g Grounding
	var found []string // normalized reading quotes that occur in the request
	for i := range a.Interpretations {
		q := ""
		if i < len(ans.Readings) {
			q = ans.Readings[i]
		}
		g.Readings = append(g.Readings, oneLineN(q, 300))
		if QuoteIn(request, q) {
			g.Grounded++
			found = append(found, normQuote(q))
		}
	}
	// A "settling" quote that is (part of) a reading's own support is the
	// ambiguous passage itself, not an answer to it.
	settles := ans.Settled && QuoteIn(request, ans.SettledBy)
	for _, f := range found {
		if sq := normQuote(ans.SettledBy); strings.Contains(f, sq) || strings.Contains(sq, f) {
			settles = false
		}
	}
	switch {
	case settles:
		g.SettledBy = oneLineN(ans.SettledBy, 300)
		g.Demoted = fmt.Sprintf("settled by the request: %q", g.SettledBy)
	case g.Grounded < 2:
		g.Demoted = fmt.Sprintf("ungrounded readings: %d of %d interpretations are supported by a quote from the request", g.Grounded, len(a.Interpretations))
	}
	return g
}

// Complete reports whether an answer addresses every interpretation of an
// ambiguity with n interpretations. An incomplete answer decides nothing:
// the ambiguity stays material.
func (ans GroundingAnswer) Complete(n int) bool {
	return len(ans.Readings) == n
}

// quoteFold maps typographic quotes to plain ones.
var quoteFold = strings.NewReplacer("“", `"`, "”", `"`, "‘", "'", "’", "'")

// QuoteIn reports whether quote occurs verbatim in text, ignoring case,
// whitespace, typographic quote marks and quote marks around the whole
// quote, and is at least minQuote non-space characters long.
func QuoteIn(text, quote string) bool {
	q := normQuote(quote)
	if utf8.RuneCountInString(strings.ReplaceAll(q, " ", "")) < minQuote {
		return false
	}
	return strings.Contains(normText(text), q)
}

func normText(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(quoteFold.Replace(s)), " "))
}

// normQuote is a quote as QuoteIn compares it.
func normQuote(s string) string { return strings.TrimSpace(strings.Trim(normText(s), "\"'`")) }

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
