package judgment

// Preflight is where "Jev is mandatory" is actually enforced: a run that needs
// a decision plane establishes it exists before spending minutes of local
// inference, rather than discovering it at VERIFY.

import (
	"context"
	"strings"
	"testing"
)

func init() {
	// A routing-capable site whose subject is repository source, so the
	// redaction contradiction has something to fire on. The real sites are
	// registered by packages this one cannot import.
	RegisterSite(SiteInfo{
		Name: "preflight_test_repo_text", Description: "routing site needing repo text",
		Mechanism: "TEST", Version: "1", MaxEffect: TierRouting, Redaction: RedactRepoText,
	})
}

func TestPreflightWithNoJudgeIsNotConfigured(t *testing.T) {
	r := Preflight(context.Background(), nil, false)
	if r.OK() {
		t.Fatal("no judge must not be OK")
	}
	if r.Class != FailureNotConfigured {
		t.Errorf("Class = %q, want %q", r.Class, FailureNotConfigured)
	}
	if r.Err() == nil {
		t.Error("Err() must be non-nil when not OK")
	}
}

// The error names what is unavailable, so an operator does not have to guess
// which parts of the system just stopped working.
func TestPreflightErrorNamesTheMandatorySites(t *testing.T) {
	r := Preflight(context.Background(), nil, false)
	err := r.Err()
	if err == nil {
		t.Fatal("want an error")
	}
	if len(r.MandatorySites) == 0 {
		t.Fatal("MandatorySites must not be empty")
	}
	for _, site := range r.MandatorySites {
		if !strings.Contains(err.Error(), site) {
			t.Errorf("error does not name %q: %v", site, err)
		}
	}
}

func TestPreflightWithAnUnavailableJudgeReportsWhy(t *testing.T) {
	r := Preflight(context.Background(), &Fake{Unavailable: true}, false)
	if r.OK() || r.Configured {
		t.Fatalf("readiness = %+v", r)
	}
	if r.Class != FailureNotConfigured {
		t.Errorf("Class = %q", r.Class)
	}
}

func TestPreflightPassesWithoutProbingWhenConfigured(t *testing.T) {
	r := Preflight(context.Background(), &Fake{}, false)
	if !r.OK() {
		t.Fatalf("readiness = %+v", r)
	}
	if r.Probed {
		t.Error("probe was false; no live call should have been made")
	}
	if r.Reachable {
		t.Error("Reachable must stay false when nothing was probed")
	}
}

func TestPreflightProbeMakesOneCallCarryingNoRepositoryContent(t *testing.T) {
	j := &Fake{Answers: map[string]Answer{"ready": {Noul: 0.5}}}
	r := Preflight(context.Background(), j, true)
	if !r.OK() {
		t.Fatalf("readiness = %+v", r)
	}
	if !r.Probed || !r.Reachable {
		t.Fatalf("readiness = %+v", r)
	}
	calls := j.Calls()
	if len(calls) != 1 {
		t.Fatalf("made %d calls, want exactly 1", len(calls))
	}
	// The probe must not be a way for repository text to leave under a mode
	// that would otherwise forbid it.
	if got := calls[0].State.Mode(); got != RedactStrict {
		t.Errorf("probe state mode = %q, want %q", got, RedactStrict)
	}
}

func TestPreflightReportsAnUnreachableService(t *testing.T) {
	j := &Fake{Err: &APIError{Class: FailureUnavailable, Detail: "connection refused"}}
	r := Preflight(context.Background(), j, true)
	if r.OK() {
		t.Fatal("an unreachable service must not be OK")
	}
	if !r.Configured {
		t.Error("the configuration was fine; only reachability failed")
	}
	if r.Class != FailureUnavailable {
		t.Errorf("Class = %q, want %q", r.Class, FailureUnavailable)
	}
}

// The contradiction: a site promoted to routing whose subject the configured
// redact mode forbids. Every task would reach it, be refused, and stop — so
// this is reported once, here, rather than once per task.
func TestPreflightCatchesARoutingSiteTheRedactModeForbids(t *testing.T) {
	j := &Fake{
		RedactMode: RedactStrict,
		Tiers:      map[string]Tier{"preflight_test_repo_text": TierRouting},
	}
	r := Preflight(context.Background(), j, false)
	if r.OK() {
		t.Fatal("a site that can never answer must fail preflight")
	}
	if len(r.Contradictions) != 1 {
		t.Fatalf("Contradictions = %v, want exactly one", r.Contradictions)
	}
	if !strings.Contains(r.Contradictions[0], "preflight_test_repo_text") {
		t.Errorf("the contradiction must name the site: %q", r.Contradictions[0])
	}
	if r.Class != FailureRefused {
		t.Errorf("Class = %q, want %q", r.Class, FailureRefused)
	}
}

// The same site left at the default tier is being observed, not obeyed: it
// skips under strict mode and nothing is contradicted.
func TestPreflightDoesNotComplainAboutALoggedSiteUnderStrictMode(t *testing.T) {
	j := &Fake{RedactMode: RedactStrict}
	r := Preflight(context.Background(), j, false)
	if !r.OK() {
		t.Fatalf("readiness = %+v", r)
	}
	if len(r.Contradictions) != 0 {
		t.Errorf("Contradictions = %v, want none", r.Contradictions)
	}
}

// A contradiction is a local configuration fault: it must be found without
// making a request.
func TestPreflightFindsAContradictionWithoutCallingTheService(t *testing.T) {
	j := &Fake{
		RedactMode: RedactStrict,
		Tiers:      map[string]Tier{"preflight_test_repo_text": TierRouting},
		Answers:    map[string]Answer{"ready": {Noul: 0.5}},
	}
	r := Preflight(context.Background(), j, true)
	if r.OK() {
		t.Fatal("want a failure")
	}
	if len(j.Calls()) != 0 {
		t.Errorf("made %d calls; a local configuration fault needs none", len(j.Calls()))
	}
}

// --- Blocking: what actually refuses to start a run ------------------------

// The decision plane is required to exist even when every site is at the
// default logged tier and a judgment would therefore change nothing. A
// component the product runs fine without is not a mandatory component.
func TestBlockingRefusesAnUnusablePlaneEvenWhenNothingRoutes(t *testing.T) {
	r := Preflight(context.Background(), &Fake{Unavailable: true}, false)
	if r.OK() {
		t.Fatal("the plane is genuinely unusable")
	}
	if len(r.Required) != 0 {
		t.Fatalf("no site should be at routing here, got %v", r.Required)
	}
	if err := r.Blocking(); err == nil {
		t.Fatal("an unusable decision plane must refuse to start a run")
	}
}

// Promote one site and the same unusable plane becomes a refusal to start.
func TestBlockingRefusesWhenASiteRoutes(t *testing.T) {
	j := &Fake{
		Unavailable: true,
		Tiers:       map[string]Tier{"require_test_routing": TierRouting},
	}
	r := Preflight(context.Background(), j, false)
	err := r.Blocking()
	if err == nil {
		t.Fatal("a site configured at routing must refuse to start without a decision plane")
	}
	if class, ok := FailureOf(err); !ok || class != FailureNotConfigured {
		t.Errorf("class = %q (ok %v), want %q", class, ok, FailureNotConfigured)
	}
}

// A healthy plane never blocks, whatever is promoted.
func TestBlockingIsNilWhenThePlaneIsUsable(t *testing.T) {
	j := &Fake{Tiers: map[string]Tier{"require_test_routing": TierRouting}}
	r := Preflight(context.Background(), j, false)
	if err := r.Blocking(); err != nil {
		t.Fatalf("a usable plane must not block: %v", err)
	}
	if len(r.Required) != 1 || r.Required[0] != "require_test_routing" {
		t.Errorf("Required = %v", r.Required)
	}
}

// An ordering promotion cannot change control flow, so it does not make the
// site *required* — but the plane is required regardless, so the run is still
// refused. The two are separate questions and this pins both.
func TestAnOrderingPromotionDoesNotMakeASiteRequired(t *testing.T) {
	j := &Fake{
		Unavailable: true,
		Tiers:       map[string]Tier{"require_test_ordering": TierOrdering},
	}
	r := Preflight(context.Background(), j, false)
	for _, name := range r.Required {
		if name == "require_test_ordering" {
			t.Error("an ordering site must never be reported as required")
		}
	}
	if err := r.Blocking(); err == nil {
		t.Error("the plane itself is still required")
	}
}
