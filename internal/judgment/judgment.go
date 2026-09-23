// Package judgment is the boundary between this system and an external model
// that answers typed questions rather than generating text.
//
// Everywhere else in this repository a model call is chat-shaped: a prompt goes
// out, a JSON document comes back, and internal/llm owns it. This package is
// for the other shape. A judgment is one narrow question — is this relevant,
// which of these four happened, how severe is this — answered with a calibrated
// probability that code composes. Nothing here generates text, so nothing here
// reintroduces the parse-the-model's-prose problem that internal/critic refuses
// to have.
//
// Three rules hold throughout, and the types are built so that breaking one
// requires editing this package rather than a call site:
//
//  1. A judgment is advisory. It may reorder, annotate, or widen a refusal. It
//     may never accept work, reject work, or decide that a task is done. §10.1
//     requires candidates be "evidence-judged, not model-voted", and a
//     probability from a remote service is not evidence. An import-direction
//     test enforces the boundary; see direction_test.go.
//
//  2. Every call site keeps the deterministic answer it already computed. See
//     Consult: there is no way to obtain a judged value without supplying the
//     value that stands when no judgment arrives. A build with this disabled,
//     unreachable, or compiled against Off() behaves exactly as the system did
//     before any of this existed.
//
//  3. Repository text leaves this machine only through State, which refuses
//     rather than redacts. The default configuration does not let it leave at
//     all.
package judgment

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrNoCredential is what a Jev-backed path refuses with when the configured
// key is not in the environment. It is deliberately distinct from a
// and from a parse error: the configuration is fine and the operator is not
// offline, the one missing thing is the credential.
var ErrNoCredential = errors.New("judgment: no credential")

// Kind names the three question types. They are distinguished by what the
// answer means, not by how it is encoded.
type Kind string

const (
	// KindNoul asks whether something holds and answers with the probability
	// of yes. There is no separate confidence: 0.5 means the model finds yes
	// and no about equally likely, which is itself the answer.
	KindNoul Kind = "noul"
	// KindChoice picks one option from a named set and reports the
	// distribution over all of them.
	KindChoice Kind = "choice"
	// KindScore places something on an ordered list of described levels. The
	// result may fall between two levels.
	KindScore Kind = "score"
)

// Question is one judgment to make.
//
// The fields are unexported and there are no literals: a Question exists only
// because Noul, NoulTrueFalse, Choice or Score built it, so the kind and the
// criteria shape can never disagree. The earlier version exposed
// `Criteria any`, which made `Question{Kind: KindNoul, Criteria: []string{…}}`
// a thing a caller could write and a validator had to catch at runtime.
//
// Instructions carry the whole meaning, including *which* thing is being
// judged. The map key a Question is filed under is for this code's benefit and
// is never sent, so a question that only makes sense next to its key is a
// question the model cannot answer. When many questions share one state, name
// the subject inside the instructions — "Considering candidate `c17` in
// `candidates` …" — rather than relying on the key or on criteria.
type Question struct {
	kind         Kind
	instructions string
	criteria     any
}

// Noul asks whether a proposition holds and answers with the probability of
// yes. It carries no criteria: the proposition is the instructions.
func Noul(instructions string) Question {
	return Question{kind: KindNoul, instructions: instructions}
}

// NoulTrueFalse is Noul with explicit descriptions of what yes and no mean.
//
// Both descriptions are required. TypeSafe's contract for noul criteria is a
// true/false pair, and a partial one is a shape the service was not promised.
// Use it only where saying what the two answers mean materially sharpens the
// question; for a relevance judgment it usually does not.
func NoulTrueFalse(instructions, whenTrue, whenFalse string) Question {
	return Question{kind: KindNoul, instructions: instructions,
		criteria: map[string]string{"true": whenTrue, "false": whenFalse}}
}

// Choice picks one option from a named set and reports the distribution.
func Choice(instructions string, options map[string]string) Question {
	copied := make(map[string]string, len(options))
	for k, v := range options {
		copied[k] = v
	}
	return Question{kind: KindChoice, instructions: instructions, criteria: copied}
}

// Score places something on an ordered list of described levels, lowest first.
func Score(instructions string, levels []string) Question {
	return Question{kind: KindScore, instructions: instructions,
		criteria: append([]string(nil), levels...)}
}

// Kind reports the question type.
func (q Question) Kind() Kind { return q.kind }

// Instructions reports the proposition, for tests and for the journal.
func (q Question) Instructions() string { return q.instructions }

// Validate reports a question this package would rather refuse than send.
//
// With constructors in place most invalid states are unrepresentable, so what
// is left is the content: an empty proposition, a choice with one option, a
// noul whose criteria are not exactly a true/false pair. A malformed question
// does not fail a call site — it becomes a fallback — but it never reaches the
// HTTP client.
func (q Question) Validate() error {
	if strings.TrimSpace(q.instructions) == "" {
		return errors.New("question has no instructions")
	}
	switch q.kind {
	case KindNoul:
		if q.criteria == nil {
			return nil
		}
		c, ok := q.criteria.(map[string]string)
		if !ok {
			return fmt.Errorf("noul criteria must be a true/false pair, got %T", q.criteria)
		}
		if len(c) != 2 || c["true"] == "" || c["false"] == "" {
			return fmt.Errorf("noul criteria must be exactly {\"true\": …, \"false\": …}; " +
				"the candidate-specific proposition belongs in the instructions")
		}
	case KindChoice:
		c, ok := q.criteria.(map[string]string)
		if !ok {
			return fmt.Errorf("choice criteria must be map[string]string, got %T", q.criteria)
		}
		if len(c) < 2 {
			return errors.New("choice needs at least two options")
		}
		for option, desc := range c {
			if strings.TrimSpace(option) == "" || strings.TrimSpace(desc) == "" {
				return errors.New("every choice option needs a name and a description")
			}
		}
	case KindScore:
		c, ok := q.criteria.([]string)
		if !ok {
			return fmt.Errorf("score criteria must be []string, got %T", q.criteria)
		}
		if len(c) < 2 {
			return errors.New("score needs at least two levels")
		}
		for _, level := range c {
			if strings.TrimSpace(level) == "" {
				return errors.New("every score level needs a description")
			}
		}
	default:
		return fmt.Errorf("unknown question kind %q; build questions with Noul, Choice or Score", q.kind)
	}
	return nil
}

// Answer is one judgment.
//
// Answered is the field to read first. Every way a judgment can fail to arrive
// — disabled, offline, unreachable, timed out, malformed, or simply omitted
// from the response — produces the zero Answer, and a zero Answer must never be
// mistaken for a confident no. Noul is 0 in both cases.
type Answer struct {
	Kind Kind
	// Noul is P(yes), for KindNoul.
	Noul float64
	// Choice is the selected option, for KindChoice.
	Choice string
	// Score is the position on the level scale, for KindScore. It may fall
	// between two levels.
	Score float64
	// Probabilities is the distribution the answer came from.
	Probabilities map[string]float64
	// Confidence summarises how concentrated that distribution is, for
	// KindChoice and KindScore. It says nothing about whether the workflow is
	// right, only about how peaked this one distribution was.
	Confidence float64
	// Answered says a judgment actually arrived.
	Answered bool
}

// Usage is what one request cost.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	Requests     int `json:"requests"`
}

// Judge answers questions. It is not a model and has deliberately no chat
// surface: there is nothing here to send a prompt to.
//
// Ask returns one Answer per question key. A key whose answer did not arrive is
// present with Answered false rather than absent, so a caller iterating its own
// questions never has to distinguish "missing" from "unanswered".
type Judge interface {
	// Name identifies the backend for logs and `bcode doctor`.
	Name() string
	// Available reports whether a request would be attempted at all.
	Available() bool
	// Ask evaluates every question against one shared state.
	Ask(ctx context.Context, st *State, qs map[string]Question) (map[string]Answer, error)
}

// unanswered builds the response shape for every way of not answering.
func unanswered(qs map[string]Question) map[string]Answer {
	out := make(map[string]Answer, len(qs))
	for id, q := range qs {
		out[id] = Answer{Kind: q.kind}
	}
	return out
}

// off is the judge that is not configured.
type off struct{}

// Off returns a Judge that answers nothing.
//
// It is the default everywhere, and it is a value rather than a nil so that the
// unconfigured path is a path that runs rather than one that is skipped: the
// composition in every call site is exercised by every test, whether or not a
// judgment is configured.
func Off() Judge { return off{} }

func (off) Name() string    { return "off" }
func (off) Available() bool { return false }
func (off) Ask(_ context.Context, _ *State, qs map[string]Question) (map[string]Answer, error) {
	return unanswered(qs), nil
}
