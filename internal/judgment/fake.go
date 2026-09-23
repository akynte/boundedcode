package judgment

import (
	"context"
	"sync"
)

// Fake is a judge for tests, and it lives in the ordinary package rather than
// in a _test file because internal/retrieval, internal/task and
// internal/supervisor all need one.
//
// Calls is the field that matters. The load-bearing tests here are not "did
// the ranking change when the judge said so" but "did a sensitive path or a
// credential ever reach the state", and that question is answered by
// inspecting what was asked, not what came back.
type Fake struct {
	// Answers is keyed by question id. A question with no entry comes back
	// unanswered, which is how a test exercises a partial response.
	Answers map[string]Answer
	// Err makes every request fail.
	Err error
	// Unavailable makes the judge report itself off, as an unconfigured one
	// does.
	Unavailable bool
	// Confidence fills in Answer.Confidence where an entry leaves it zero, so
	// a test that cares about ranking does not have to restate the threshold.
	Confidence float64
	// Tiers lets a test set a site's authority tier, the same way judgment.yaml
	// would. A site absent from the map reads as TierLogged, matching Config.
	Tiers map[string]Tier
	// RedactMode overrides what RedactModeOf reports for this fake. The zero
	// value is RedactStrict, matching Config's default.
	RedactMode RedactMode

	mu    sync.Mutex
	calls []FakeCall
}

// FakeCall is one recorded request.
type FakeCall struct {
	State     *State
	Questions map[string]Question
}

// Name identifies the fake.
func (f *Fake) Name() string { return "fake" }

// Available reports whether a request would be attempted.
func (f *Fake) Available() bool { return !f.Unavailable }

// MinConfidence keeps Consult from reaching for the package default in tests
// that set none.
func (f *Fake) MinConfidence() float64 { return 0.5 }

// Tier reports the tier a test configured for site, or TierLogged.
//
// Deliberately *not* Config's default, which promotes routing-capable sites.
// A test double is opt-in: a test that does not mention tiers is not asking
// for a site's effects, and inheriting the production default would make every
// unrelated task test depend on this fake answering plausibly. Tests that mean
// to exercise a promoted site set Tiers explicitly — see
// internal/task/decisionplane_test.go.
func (f *Fake) Tier(site string) Tier {
	if f.Tiers == nil {
		return TierLogged
	}
	if t, ok := f.Tiers[site]; ok && t.Valid() {
		return t
	}
	return TierLogged
}

// Redact reports the redaction mode a test configured, or RedactStrict.
func (f *Fake) Redact() RedactMode {
	if f.RedactMode == "" {
		return RedactStrict
	}
	return f.RedactMode
}

// Ask records the request and replays Answers.
func (f *Fake) Ask(_ context.Context, st *State, qs map[string]Question) (map[string]Answer, error) {
	f.mu.Lock()
	f.calls = append(f.calls, FakeCall{State: st, Questions: qs})
	f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	out := make(map[string]Answer, len(qs))
	for id, q := range qs {
		a, ok := f.Answers[id]
		if !ok {
			out[id] = Answer{Kind: q.kind}
			continue
		}
		a.Kind = q.kind
		a.Answered = true
		if a.Confidence == 0 {
			a.Confidence = f.Confidence
		}
		out[id] = a
	}
	return out, nil
}

// Calls returns the requests made so far.
func (f *Fake) Calls() []FakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]FakeCall(nil), f.calls...)
}

// Sent reports every state field the fake was handed, as it would have gone
// out. It is how a test asserts that nothing reached the wire that should not
// have.
func (f *Fake) Sent() []string {
	var out []string
	for _, c := range f.Calls() {
		if c.State == nil {
			continue
		}
		body, err := c.State.canonical()
		if err != nil {
			continue
		}
		out = append(out, body)
	}
	return out
}

// RepoTextSent totals the repository *source* fields across every request.
// Under redact: strict this must be zero, and a test says so.
func (f *Fake) RepoTextSent() int {
	total := 0
	for _, c := range f.Calls() {
		total += c.State.RepoTextFields()
	}
	return total
}

// RepoMetadataSent totals the repository *records* — paths, symbols, line
// ranges — across every request. Unlike source, this is non-zero in strict
// mode by design, and a test asserts what it contains rather than that it is
// empty.
func (f *Fake) RepoMetadataSent() int {
	total := 0
	for _, c := range f.Calls() {
		total += c.State.RepoMetadataFields()
	}
	return total
}

// OutputSent totals verification-output fields across every request. Under
// anything below redact: output this must be zero, and a test says so.
func (f *Fake) OutputSent() int {
	total := 0
	for _, c := range f.Calls() {
		total += c.State.OutputFields()
	}
	return total
}
