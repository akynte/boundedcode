package judgment

// Mandatory decisions.
//
// Consult is advisory by construction: it cannot fail, and a site that gets no
// answer keeps the deterministic value it already had. That is the right
// contract for a judgment that reorders candidates, because the ordering the
// code computed on its own is a real answer and a worse one is not a wrong
// one.
//
// It is the wrong contract for a decision that changes control flow. When a
// site decides *which way the task goes* — whether an attempt is making
// progress, which failure this is, whether a waiver holds — falling back to a
// default is not "keeping the deterministic answer", because there is no
// deterministic answer to keep. It is picking one and not saying so.
//
// Require is the failing form for those. The split is not a list somebody
// maintains: a site is mandatory exactly when its registered MaxEffect is
// TierRouting, because that is the field that already declares whether the
// site can change control flow.
//
// What Require does not do is override evidence. A mandatory decision can stop
// a task, route it, or refuse to conclude; it cannot turn a failing test into
// a passing one. internal/policy, internal/firewall, internal/broker and
// internal/recipe still cannot reach this package at all, and
// TestDecisionPackagesCannotReachJudgment still enforces that.

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// FailureClass names why a mandatory decision could not be made.
//
// The classes are distinguishable because they call for different responses: a
// timeout is worth retrying, a bad credential is not, and a confident answer
// below the site's bar is not a failure of the service at all.
type FailureClass string

const (
	// FailureNotConfigured: no usable Jev configuration. A permanent error
	// that preflight is meant to catch before any work is done.
	FailureNotConfigured FailureClass = "JEV_NOT_CONFIGURED"
	// FailureAuth: the credential was rejected. Permanent until changed.
	FailureAuth FailureClass = "JEV_AUTH_FAILED"
	// FailureUnavailable: the service could not be reached, or answered with
	// a server error. Retryable.
	FailureUnavailable FailureClass = "JEV_UNAVAILABLE"
	// FailureTimeout: the request outlived its deadline. Retryable.
	FailureTimeout FailureClass = "JEV_TIMEOUT"
	// FailureRateLimited: the service asked for less traffic. Retryable.
	FailureRateLimited FailureClass = "JEV_RATE_LIMITED"
	// FailureInvalid: a response this build cannot read. Not retryable: the
	// same request will produce the same shape.
	FailureInvalid FailureClass = "JEV_INVALID_RESPONSE"
	// FailureLowConfidence: an answer arrived and did not clear the site's
	// bar. The service is healthy; the question was not settled.
	FailureLowConfidence FailureClass = "JEV_LOW_CONFIDENCE"
	// FailureRefused: this system declined to ask — a malformed question, or
	// state a redaction mode forbids. A local fault, never the service's.
	FailureRefused FailureClass = "JEV_REFUSED"
)

// Retryable reports whether trying the same request again could succeed.
func (c FailureClass) Retryable() bool {
	switch c {
	case FailureUnavailable, FailureTimeout, FailureRateLimited:
		return true
	default:
		return false
	}
}

// APIError is a typed failure from the service, so a caller can tell a
// rejected credential from an outage without reading an error string.
//
// It deliberately carries no response body: a service may echo the request,
// and the request may carry repository text.
type APIError struct {
	Class    FailureClass
	Endpoint string
	Status   int
	Detail   string
}

func (e *APIError) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("judgment: %s: %s (%s)", e.Endpoint, e.Detail, e.Class)
	}
	return fmt.Sprintf("judgment: %s: %s (%s)", e.Endpoint, e.Detail, e.Class)
}

// classifyStatus maps an HTTP status onto a failure class.
func classifyStatus(status int) FailureClass {
	switch {
	case status == 401 || status == 403:
		return FailureAuth
	case status == 429:
		return FailureRateLimited
	case status >= 500:
		return FailureUnavailable
	default:
		return FailureInvalid
	}
}

// RequirementError is what a mandatory decision returns when it cannot be
// made. It names the site, so evidence and telemetry can say which decision
// the task stopped on.
type RequirementError struct {
	Site   string
	Class  FailureClass
	Detail string
}

func (e *RequirementError) Error() string {
	return fmt.Sprintf("judgment: required decision %q could not be made: %s (%s)",
		e.Site, e.Detail, e.Class)
}

// Retryable reports whether the same decision could succeed on a later
// attempt.
func (e *RequirementError) Retryable() bool { return e.Class.Retryable() }

// FailureOf returns the class of a requirement failure, and whether err was
// one. It is the way a caller inspects a failure without type-switching.
func FailureOf(err error) (FailureClass, bool) {
	var re *RequirementError
	if errors.As(err, &re) {
		return re.Class, true
	}
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Class, true
	}
	return "", false
}

// Mandatory reports whether a site's decision must be made by Jev rather than
// defaulted.
//
// A site is mandatory exactly when it can change control flow. That is what
// MaxEffect already records, so this reads the registration rather than
// keeping a second list that could disagree with it.
func Mandatory(site string) bool {
	info, ok := Site(site)
	return ok && info.MaxEffect == TierRouting
}

// MandatorySites lists them, sorted, for preflight and for `bcode doctor`.
func MandatorySites() []string {
	var out []string
	for _, info := range KnownSites() {
		if info.MaxEffect == TierRouting {
			out = append(out, info.Name)
		}
	}
	sort.Strings(out)
	return out
}

// Require is Consult for a decision that may not be defaulted.
//
// It returns the judged value, the note describing how the answer was
// obtained, and an error when no answer good enough to act on arrived. The
// caller must not substitute a value of its own for that error: the point of
// the failing form is that a site which changes control flow says so when it
// cannot decide, instead of choosing silently.
//
// Fallback on the query is still used, but only as the value returned
// alongside a nil error when the judgment agrees with it. On error the zero
// value is returned with the error, so a caller that ignores the error gets
// something obviously wrong rather than something plausibly wrong.
func Require[T any](ctx context.Context, j Judge, site string, q Query[T]) (T, Note, error) {
	var zero T
	if site == "" {
		return zero, Note{Source: SourceError}, &RequirementError{
			Site: "(unnamed)", Class: FailureRefused, Detail: "a required decision needs a site name"}
	}
	value, note := Consult(ctx, j, q)
	if note.Used() {
		return value, note, nil
	}
	return zero, note, &RequirementError{
		Site: site, Class: classifyNote(note), Detail: noteDetail(note)}
}

// classifyNote maps a Note onto a failure class, preferring the typed class
// the transport supplied over the coarse Source it was flattened into.
func classifyNote(n Note) FailureClass {
	if n.Class != "" {
		return n.Class
	}
	switch n.Source {
	case SourceDisabled:
		return FailureNotConfigured
	case SourceLowConfidence:
		return FailureLowConfidence
	case SourceRefused:
		return FailureRefused
	case SourceUnavailable:
		return FailureUnavailable
	case SourceError:
		return FailureInvalid
	default:
		return FailureUnavailable
	}
}

func noteDetail(n Note) string {
	if n.Detail != "" {
		return n.Detail
	}
	return string(n.Source)
}

// RequireAll is AskAll for a site whose decision may not be defaulted.
//
// The six mandatory sites ask a batch of independent questions rather than
// one, so this, not Require, is the form they use. The contract is the same:
// when no usable answer set arrived, the caller gets an error naming the site
// and the class, and must not invent an answer to carry on with.
//
// The failure test is note.Used(), which is false for every way the batch
// produced nothing to read — no judge, an unusable state, a refused question,
// a transport error, and the case AskAll folds in for us where the request
// succeeded and every answer was omitted. A batch that was answered in part
// is *not* a failure here: the questions are independent by construction, and
// a site that got seven of eight answers has seven decisions it can make. What
// it may not do is treat the eighth as settled, which is why the answers map
// still reports Answered per question.
func RequireAll(ctx context.Context, j Judge, site string, st *State,
	qs map[string]Question) (map[string]Answer, Note, error) {

	if site == "" {
		return unanswered(qs), Note{Source: SourceError}, &RequirementError{
			Site: "(unnamed)", Class: FailureRefused, Detail: "a required decision needs a site name"}
	}
	answers, note := AskAll(ctx, j, st, qs)
	if note.Used() {
		return answers, note, nil
	}
	return answers, note, &RequirementError{
		Site: site, Class: classifyNote(note), Detail: noteDetail(note)}
}

// Unmet is the failure a mandatory site returns when it refuses locally,
// before any request is made — an unusable state, or a redaction mode that
// forbids the very thing this site reads.
//
// It exists so that a local refusal and a service failure reach the caller as
// the same type. The caller's question is "was this decision made?", and the
// answer is no in both cases; which of them it was is the class.
func Unmet(site string, class FailureClass, detail string) error {
	return &RequirementError{Site: site, Class: class, Detail: detail}
}

// Required reports whether a decision at this site, configured at this tier,
// must be made rather than defaulted.
//
// Mandatory alone is not the test, and the difference matters. Mandatory asks
// what a site's code is *capable* of — can it change control flow at all —
// and that is a property of the site, fixed at registration. Required asks
// whether, as this system is configured right now, the answer would actually
// be consumed.
//
// The two come apart at every site left at TierLogged, which is all of them by
// default. There, the site is asked, the answer is journalled, and the run
// behaves exactly as it did before the site existed. If a timeout at such a
// site stopped the task, an observational site would have become a hard
// runtime dependency, and the system would be less reliable for no safety
// gained — nothing was going to read the answer.
//
// Promoted to TierRouting the same failure is the opposite case. The site
// would have blocked, rerouted, or ended an attempt on what it found; not
// knowing what it found and carrying on is not "keeping the deterministic
// answer", because there is no deterministic answer here. It is assuming the
// benign one silently, which is the thing the failing form exists to prevent.
//
// This is not a way for a missing Jev to go unnoticed. Jev is mandatory as a
// component: Preflight refuses to start a run without a working one, whatever
// the tiers say. Required governs only what a *transient* failure does to a
// task already under way.
func Required(site string, tier Tier) bool {
	return Mandatory(site) && tier.Permits(TierRouting)
}

// FailClosed reports the class a caller should stop on, and whether it must.
//
// It is the one line a mandatory call site needs: it folds together "was this
// a requirement failure" and "does this site's configured tier mean the
// failure matters", so a caller cannot accidentally fail a task over a site
// that was only being observed, or silently continue past one that was not.
//
// A nil error is never a stop. An error that is not a requirement failure is
// not one either — it belongs to whatever else the caller was doing.
func FailClosed(site string, tier Tier, err error) (FailureClass, bool) {
	if err == nil {
		return "", false
	}
	class, ok := FailureOf(err)
	if !ok {
		return "", false
	}
	return class, Required(site, tier)
}

// Unready explains in one line why a judge cannot answer, for the detail of a
// requirement failure and for `bcode doctor`.
//
// Client.Unready gives the operator-actionable version — which setting or
// credential is missing — and this is how a call site holding only a Judge
// reaches it. A judge that is available has nothing to explain and returns "".
func Unready(j Judge) string {
	if j == nil {
		return "no judge is configured"
	}
	if u, ok := j.(interface{ Unready() string }); ok {
		if reason := u.Unready(); reason != "" {
			return reason
		}
	}
	if j.Available() {
		return ""
	}
	return j.Name() + " is not available"
}

// MinConfidenceOf is the operator's confidence floor for this judge.
//
// Consult applies it for single questions; AskAll cannot, because a batch has
// no single answer to gate. The six mandatory sites ask in batches, so each
// applies this itself to the Choice and Score answers it reads — otherwise the
// floor an operator configured would govern advisory sites and not the ones
// that change control flow, which is backwards.
//
// It does not apply to a Noul, which reports no confidence: there the
// probability is the whole answer and the call site's own bar is the gate.
func MinConfidenceOf(j Judge) float64 { return confidenceFloor(j) }

// EffectThresholdOf is the probability at or above which a site's caller acts
// on a finding, as the site registered it.
//
// Call sites read this rather than repeating the number. SiteInfo.EffectThreshold
// is what `bcode judgment replay` uses to say what a recorded prediction would
// have done, and a literal at the call site would let the report and the
// behaviour it reports on drift apart silently.
func EffectThresholdOf(site string) float64 {
	info, ok := Site(site)
	if !ok {
		return 0
	}
	return info.EffectThreshold
}

// RequiredSites names the sites this configuration actually routes on.
//
// MandatorySites is the fixed list of sites that *can* change control flow;
// this is the subset an operator has promoted to a tier where they do. The
// difference is what separates "Jev would be nice" from "this run cannot
// proceed without Jev", and every caller that fails closed tests this one.
func RequiredSites(j Judge) []string {
	var out []string
	for _, name := range MandatorySites() {
		if Required(name, SiteTier(j, name)) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
