package judgment

// Preflight: establish that the mandatory decision plane exists before a task
// spends minutes of local inference discovering that it does not.
//
// Six of the eleven registered sites can change control flow, and those are
// required rather than advisory. A run that reaches VERIFY and only then finds
// that no credential was ever configured has wasted the expensive part of the
// work on a task it was always going to have to stop.
//
// The check is layered so the cheap parts run every time and the network call
// is opt-in. Configuration and credential faults are permanent and local, and
// catching those costs nothing; reachability costs a request, and a command
// that runs a hundred times should not make a hundred of them.

import (
	"context"
	"fmt"
	"strings"
)

// Readiness is what preflight found.
type Readiness struct {
	// Configured reports that a usable configuration exists: enabled, with an
	// endpoint, a model and a credential.
	Configured bool
	// Reachable reports that a live call was made and answered. False with
	// Probed false means the check was not attempted, not that it failed.
	Reachable bool
	Probed    bool
	// Class is the failure, when there is one.
	Class FailureClass
	// Detail is one line for an operator. It never contains the credential.
	Detail string
	// Model and Endpoint are echoed so a report can say what was checked.
	Model    string
	Endpoint string
	// MandatorySites is what would have been unavailable.
	MandatorySites []string
	// Required is the subset of those an operator has promoted to a tier
	// where their answer actually changes what a task does. Empty means the
	// decision plane is being observed rather than obeyed, and a run may
	// proceed without it.
	Required []string
	// Contradictions names sites an operator has configured to route on a
	// decision they can never make, because the site's subject is repository
	// source or verification output and the configured redact mode forbids
	// sending it.
	//
	// Nothing at runtime can resolve this: every task would reach the site,
	// be refused locally, and stop. It is a configuration fault, so it is
	// reported once here rather than discovered once per task.
	Contradictions []string
}

// OK reports whether the mandatory plane is usable as far as this check went.
func (r Readiness) OK() bool {
	return r.Configured && len(r.Contradictions) == 0 && (!r.Probed || r.Reachable)
}

// Err renders the readiness as an error, or nil when it is usable.
func (r Readiness) Err() error {
	if r.OK() {
		return nil
	}
	return &RequirementError{
		Site:  "preflight",
		Class: r.Class,
		Detail: fmt.Sprintf("%s; %d site(s) require it: %s", r.Detail,
			len(r.MandatorySites), strings.Join(r.MandatorySites, ", ")),
	}
}

// Blocking is the error a run must not start on, or nil.
//
// The decision plane is required to exist, and that is not conditional on any
// site's configured tier. It is tempting to make it conditional — with every
// site at the default logged tier a judgment changes nothing, so a run without
// one would produce the same result — but a component the product runs
// perfectly well without is not a mandatory component, and a system that
// quietly does the degraded thing is the silent fallback this design exists to
// remove. Running without a decision plane is a configuration this project
// does not support, so it is refused rather than half-supported.
//
// What stays conditional is a site's *authority*, which Required reports and
// FailClosed applies. Requiring the plane to exist and granting a site power
// over control flow are different questions, and only the second is earned
// from calibration evidence.
//
// This gate is on task execution, not on the CLI: `bcode doctor`, `bcode config
// show` and the rest still run and are how an operator diagnoses exactly this.
func (r Readiness) Blocking() error { return r.Err() }

// Preflight checks the configuration, and optionally the service.
//
// probe controls the live call. It is deliberately a parameter rather than a
// setting: `bcode doctor` wants the network check and a task run wants the cheap
// one, and that is a property of the caller rather than of the operator's
// configuration.
func Preflight(ctx context.Context, j Judge, probe bool) Readiness {
	r := Readiness{MandatorySites: MandatorySites()}

	if j == nil {
		r.Class, r.Detail = FailureNotConfigured, "no judge is configured"
		// A nil judge carries no site configuration, so nothing can be at
		// routing and nothing is blocked. Required stays empty.
		return r
	}
	r.Required = RequiredSites(j)
	if m, ok := j.(interface{ ModelID() string }); ok {
		r.Model = m.ModelID()
	}
	if e, ok := j.(interface{ Endpoint() string }); ok {
		r.Endpoint = e.Endpoint()
	}
	if !j.Available() {
		// Available() is false for a judge that is off, and for one that is
		// on with no credential. The distinction matters to an operator, and
		// the client reports it through Unready.
		r.Class = FailureNotConfigured
		r.Detail = "judgments are required but not usable: " + Unready(j)
		return r
	}
	r.Configured = true

	// A site promoted to routing whose subject the configured redact mode
	// forbids is a promise the configuration cannot keep.
	if mode := RedactModeOf(j); mode.Valid() {
		for _, name := range r.MandatorySites {
			info, known := Site(name)
			if !known || !Required(name, SiteTier(j, name)) {
				continue
			}
			if !mode.AtLeast(info.Redaction) {
				r.Contradictions = append(r.Contradictions, fmt.Sprintf(
					"%s is configured at routing but needs redact %s, and the configured mode is %s",
					name, info.Redaction, mode))
			}
		}
	}
	if len(r.Contradictions) > 0 {
		r.Class = FailureRefused
		r.Detail = "a site is configured to route on a decision it may never make: " +
			strings.Join(r.Contradictions, "; ")
		return r
	}

	if !probe {
		return r
	}
	r.Probed = true
	if err := Ping(ctx, j); err != nil {
		if class, ok := FailureOf(err); ok {
			r.Class = class
		} else {
			r.Class = FailureUnavailable
		}
		r.Detail = "the decision service did not answer: " + err.Error()
		return r
	}
	r.Reachable = true
	return r
}

// Ping makes the smallest possible live call: one noul carrying no repository
// content, so a reachability check never sends anything a redaction mode would
// have to consider.
func Ping(ctx context.Context, j Judge) error {
	st := NewState(RedactStrict)
	if err := st.Objective("preflight"); err != nil {
		return &APIError{Class: FailureRefused, Detail: "building preflight state"}
	}
	answers, note := AskAll(ctx, j, st, map[string]Question{
		"ready": Noul("This is a reachability check. Answer with any probability."),
	})
	if !note.Used() {
		return &APIError{
			Class: classifyNote(note), Endpoint: "", Detail: noteDetail(note)}
	}
	if !answers["ready"].Answered {
		return &APIError{Class: FailureInvalid, Detail: "the service answered no question"}
	}
	return nil
}
