package judgment

// The mandatory decision plane.
//
// Every case here is a way the decision can fail to be made, because that is
// the half of the contract that is new: Consult could not fail, and Require
// must. The cases that matter most are the ones asserting what does *not*
// happen — no silent default, no fallback value handed back beside an error,
// and no path by which a judgment turns a deterministic failure into a pass.
//
// Nothing here touches the live service. A unit test that depends on
// api.typesafe.ai reports the network's mood, not this code's.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// jevServer is a deterministic stand-in: it answers, stalls, or fails with a
// status, and records what it was sent.
type jevServer struct {
	status  int
	body    string
	stall   time.Duration
	gotBody []byte
	gotAuth string
}

func (s *jevServer) start(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.gotAuth = r.Header.Get("Authorization")
		buf := make([]byte, 1<<20)
		n, _ := r.Body.Read(buf)
		s.gotBody = buf[:n]
		if s.stall > 0 {
			time.Sleep(s.stall)
		}
		if s.status != 0 && s.status != http.StatusOK {
			w.WriteHeader(s.status)
			_, _ = w.Write([]byte(`{"error":"deliberate"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		body := s.body
		if body == "" {
			body = `{"answers":{"q":{"noul":0.91}}}`
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	t.Setenv("JEV_TEST_KEY", "test-credential")
	c, err := New(Config{
		Enabled: true, Endpoint: srv.URL, Model: "jev-test",
		APIKeyEnv: "JEV_TEST_KEY", Redact: RedactStrict,
		MinConfidence: 0.75, TimeoutSeconds: 5,
	}, Deps{})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	client, ok := c.(*Client)
	if !ok {
		t.Fatalf("New returned %T, want *Client", c)
	}
	return client
}

func requireState(t *testing.T) *State {
	t.Helper()
	st := NewState(RedactStrict)
	if err := st.Objective("fix the failing parser test"); err != nil {
		t.Fatal(err)
	}
	return st
}

func noulQuery(fallback bool) Query[bool] {
	return Query[bool]{
		Question:  Noul("is this so"),
		State:     nil, // set per call
		Fallback:  fallback,
		Interpret: func(a Answer) (bool, bool) { return a.Noul >= 0.5, true },
	}
}

func init() {
	// internal/judgment does not import the packages whose init() registers
	// the real sites, so the derivation is tested against sites registered
	// here. The rule under test is "MaxEffect == routing", not the contents
	// of the global registry.
	RegisterSite(SiteInfo{
		Name: "require_test_routing", Description: "routing test site",
		Mechanism: "TEST", Version: "1", MaxEffect: TierRouting, Redaction: RedactStrict,
	})
	RegisterSite(SiteInfo{
		Name: "require_test_ordering", Description: "ordering test site",
		Mechanism: "TEST", Version: "1", MaxEffect: TierOrdering, Redaction: RedactStrict,
	})
}

// The split is derived from the registration, not from a hand-kept list.
func TestMandatoryIsDerivedFromMaxEffect(t *testing.T) {
	if !Mandatory("require_test_routing") {
		t.Error("a routing site is not mandatory")
	}
	if Mandatory("require_test_ordering") {
		t.Error("an ordering site was reported mandatory; it degrades gracefully")
	}
	for _, info := range KnownSites() {
		want := info.MaxEffect == TierRouting
		if got := Mandatory(info.Name); got != want {
			t.Errorf("Mandatory(%q) = %v, want %v (MaxEffect %s)",
				info.Name, got, want, info.MaxEffect)
		}
	}
	if len(MandatorySites()) == 0 {
		t.Fatal("no site is mandatory; the decision plane would be optional")
	}
	// An unregistered site is not silently mandatory.
	if Mandatory("not_a_site") {
		t.Error("an unknown site was reported mandatory")
	}
}

func TestARequiredDecisionSucceedsAndReturnsTheJudgedValue(t *testing.T) {
	s := &jevServer{}
	c := s.start(t)
	q := noulQuery(false)
	q.State = requireState(t)

	got, note, err := Require(context.Background(), c, "diff_conformance", q)
	if err != nil {
		t.Fatalf("Require: %v", err)
	}
	if !got {
		t.Error("value = false; the server answered 0.91")
	}
	if note.Source != SourceLive {
		t.Errorf("source = %q, want live", note.Source)
	}
}

// The whole point: no default is substituted, and the fallback is not handed
// back beside the error as though it were an answer.
func TestAFailedRequiredDecisionReturnsNoValueAndDoesNotFallBack(t *testing.T) {
	s := &jevServer{status: http.StatusInternalServerError}
	c := s.start(t)
	q := noulQuery(true) // a fallback that would be wrong to use
	q.State = requireState(t)

	got, _, err := Require(context.Background(), c, "failure_triage", q)
	if err == nil {
		t.Fatal("a 500 produced no error; the decision was defaulted")
	}
	if got {
		t.Error("the fallback value was returned beside an error")
	}
	class, ok := FailureOf(err)
	if !ok {
		t.Fatalf("err %v is not a classified failure", err)
	}
	if class != FailureUnavailable {
		t.Errorf("class = %s, want %s", class, FailureUnavailable)
	}
	if !class.Retryable() {
		t.Error("a 5xx should be retryable")
	}
}

func TestFailureClassesAreDistinguished(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		want      FailureClass
		retryable bool
	}{
		{"unauthorized", http.StatusUnauthorized, FailureAuth, false},
		{"forbidden", http.StatusForbidden, FailureAuth, false},
		{"rate limited", http.StatusTooManyRequests, FailureRateLimited, true},
		{"server error", http.StatusBadGateway, FailureUnavailable, true},
		{"bad request", http.StatusBadRequest, FailureInvalid, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &jevServer{status: tc.status}
			c := s.start(t)
			q := noulQuery(false)
			q.State = requireState(t)

			_, _, err := Require(context.Background(), c, "progress_monitor", q)
			if err == nil {
				t.Fatal("no error")
			}
			class, _ := FailureOf(err)
			if class != tc.want {
				t.Errorf("class = %s, want %s", class, tc.want)
			}
			if class.Retryable() != tc.retryable {
				t.Errorf("Retryable() = %v, want %v", class.Retryable(), tc.retryable)
			}
		})
	}
}

func TestAMalformedResponseIsInvalidAndNotRetryable(t *testing.T) {
	s := &jevServer{body: `{"answers": "not an object"}`}
	c := s.start(t)
	q := noulQuery(false)
	q.State = requireState(t)

	_, _, err := Require(context.Background(), c, "verification_integrity", q)
	if err == nil {
		t.Fatal("a malformed body produced no error")
	}
	class, _ := FailureOf(err)
	if class != FailureInvalid {
		t.Errorf("class = %s, want %s", class, FailureInvalid)
	}
	if class.Retryable() {
		t.Error("the same request would produce the same shape; not retryable")
	}
}

func TestATimeoutIsItsOwnClass(t *testing.T) {
	s := &jevServer{stall: 2 * time.Second}
	c := s.start(t)
	q := noulQuery(false)
	q.State = requireState(t)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _, err := Require(ctx, c, "obligation_reason", q)
	if err == nil {
		t.Fatal("the stalled request produced no error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("took %s; the deadline did not bound the request", elapsed)
	}
	class, _ := FailureOf(err)
	if class != FailureTimeout && class != FailureUnavailable {
		t.Errorf("class = %s, want a timeout or unavailable", class)
	}
}

func TestCancellationPropagatesIntoARequiredDecision(t *testing.T) {
	s := &jevServer{stall: 2 * time.Second}
	c := s.start(t)
	q := noulQuery(false)
	q.State = requireState(t)

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()

	done := make(chan error, 1)
	go func() {
		_, _, err := Require(ctx, c, "intake_profile", q)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a cancelled request produced no error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not reach the request; it hung")
	}
}

// A confident answer that does not clear the bar is not a service failure, and
// must not be confused with one.
func TestLowConfidenceIsDistinctFromUnavailable(t *testing.T) {
	s := &jevServer{body: `{"answers":{"q":{"choice":"a","confidence":0.10}}}`}
	c := s.start(t)
	q := Query[string]{
		Question:      Choice("pick", map[string]string{"a": "first", "b": "second"}),
		State:         requireState(t),
		Fallback:      "fallback",
		MinConfidence: 0.75,
		Interpret:     func(a Answer) (string, bool) { return a.Choice, true },
	}
	got, _, err := Require(context.Background(), c, "diff_conformance", q)
	if err == nil {
		t.Fatal("a low-confidence answer was accepted for a required decision")
	}
	if got == "fallback" {
		t.Error("the fallback was returned beside the error")
	}
	class, _ := FailureOf(err)
	if class != FailureLowConfidence {
		t.Errorf("class = %s, want %s", class, FailureLowConfidence)
	}
	if class.Retryable() {
		t.Error("retrying an identical question is not a fix for low confidence")
	}
}

// A required decision with no configured judge fails as a configuration
// problem, which is what preflight exists to catch earlier.
func TestAnUnconfiguredJudgeFailsAsNotConfigured(t *testing.T) {
	q := noulQuery(false)
	q.State = requireState(t)
	_, _, err := Require(context.Background(), Off(), "failure_triage", q)
	if err == nil {
		t.Fatal("no judge produced no error")
	}
	class, _ := FailureOf(err)
	if class != FailureNotConfigured {
		t.Errorf("class = %s, want %s", class, FailureNotConfigured)
	}
}

// The credential must reach the service and must not reach the request body.
func TestTheCredentialIsSentAsAHeaderAndNeverInTheState(t *testing.T) {
	s := &jevServer{}
	c := s.start(t)
	q := noulQuery(false)
	q.State = requireState(t)

	if _, _, err := Require(context.Background(), c, "progress_monitor", q); err != nil {
		t.Fatalf("Require: %v", err)
	}
	if s.gotAuth != "Bearer test-credential" {
		t.Errorf("auth header = %q", s.gotAuth)
	}
	if strings.Contains(string(s.gotBody), "test-credential") {
		t.Error("the credential appears in the request body")
	}
	// The body is JSON and carries the objective, not the environment.
	var body map[string]any
	if err := json.Unmarshal(s.gotBody, &body); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
}

// An error from a required decision must never read as a success, and must
// name the site so evidence can say which decision stopped the task.
func TestARequirementErrorNamesItsSite(t *testing.T) {
	var re *RequirementError
	s := &jevServer{status: http.StatusServiceUnavailable}
	c := s.start(t)
	q := noulQuery(false)
	q.State = requireState(t)

	_, _, err := Require(context.Background(), c, "verification_integrity", q)
	if !errors.As(err, &re) {
		t.Fatalf("err %T is not a *RequirementError", err)
	}
	if re.Site != "verification_integrity" {
		t.Errorf("site = %q", re.Site)
	}
	if !strings.Contains(err.Error(), "verification_integrity") {
		t.Errorf("message does not name the site: %s", err)
	}
}

func TestARequiredDecisionNeedsASiteName(t *testing.T) {
	q := noulQuery(false)
	q.State = requireState(t)
	_, _, err := Require(context.Background(), Off(), "", q)
	if err == nil {
		t.Fatal("an unnamed required decision was allowed")
	}
	if class, _ := FailureOf(err); class != FailureRefused {
		t.Errorf("class = %s, want %s", class, FailureRefused)
	}
}

// --- RequireAll: the batch form the six mandatory sites use ----------------

func batchState(t *testing.T) *State {
	t.Helper()
	st := NewState(RedactStrict)
	if err := st.Objective("fix the failing parser test"); err != nil {
		t.Fatal(err)
	}
	return st
}

func twoNouls() map[string]Question {
	return map[string]Question{
		"a": Noul("the first proposition holds"),
		"b": Noul("the second proposition holds"),
	}
}

func TestRequireAllSucceedsWhenTheBatchIsAnswered(t *testing.T) {
	srv := &jevServer{body: `{"answers":{"a":{"noul":0.9},"b":{"noul":0.2}}}`}
	j := srv.start(t)

	answers, note, err := RequireAll(context.Background(), j, "require_test_routing",
		batchState(t), twoNouls())
	if err != nil {
		t.Fatalf("RequireAll: %v", err)
	}
	if !note.Used() {
		t.Errorf("note.Used() = false, want true (source %q)", note.Source)
	}
	if !answers["a"].Answered || !answers["b"].Answered {
		t.Errorf("both questions should be answered, got %+v", answers)
	}
}

// The distinguishing rule: the questions are independent, so seven of eight is
// seven decisions, not a failure. What must not happen is the missing one
// reading as settled.
func TestRequireAllAcceptsAPartiallyAnsweredBatch(t *testing.T) {
	srv := &jevServer{body: `{"answers":{"a":{"noul":0.9}}}`}
	j := srv.start(t)

	answers, _, err := RequireAll(context.Background(), j, "require_test_routing",
		batchState(t), twoNouls())
	if err != nil {
		t.Fatalf("a partially answered batch must not be a requirement failure: %v", err)
	}
	if !answers["a"].Answered {
		t.Error(`"a" should be answered`)
	}
	if answers["b"].Answered {
		t.Error(`"b" was omitted by the service and must not read as answered`)
	}
}

// An answered-nothing response is not a decision, however healthy the
// transport was.
func TestRequireAllFailsWhenNothingWasAnswered(t *testing.T) {
	srv := &jevServer{body: `{"answers":{}}`}
	j := srv.start(t)

	_, _, err := RequireAll(context.Background(), j, "require_test_routing",
		batchState(t), twoNouls())
	if err == nil {
		t.Fatal("a batch with no answers must be a requirement failure")
	}
	if class, ok := FailureOf(err); !ok || class != FailureUnavailable {
		t.Errorf("class = %q (ok %v), want %q", class, ok, FailureUnavailable)
	}
}

func TestRequireAllCarriesTheTransportClass(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		want   FailureClass
	}{
		{"credential rejected", http.StatusUnauthorized, FailureAuth},
		{"asked for less traffic", http.StatusTooManyRequests, FailureRateLimited},
		{"service faulted", http.StatusBadGateway, FailureUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := &jevServer{status: tc.status}
			j := srv.start(t)

			_, _, err := RequireAll(context.Background(), j, "require_test_routing",
				batchState(t), twoNouls())
			if err == nil {
				t.Fatal("want a requirement failure")
			}
			class, ok := FailureOf(err)
			if !ok || class != tc.want {
				t.Errorf("class = %q (ok %v), want %q", class, ok, tc.want)
			}
			var re *RequirementError
			if !errors.As(err, &re) || re.Site != "require_test_routing" {
				t.Errorf("the failure must name its site, got %v", err)
			}
		})
	}
}

func TestRequireAllFailsWithoutAJudge(t *testing.T) {
	_, _, err := RequireAll(context.Background(), nil, "require_test_routing",
		batchState(t), twoNouls())
	if err == nil {
		t.Fatal("no judge must be a requirement failure for a mandatory site")
	}
	if class, _ := FailureOf(err); class != FailureNotConfigured {
		t.Errorf("class = %q, want %q", class, FailureNotConfigured)
	}
}

func TestRequireAllNeedsASiteName(t *testing.T) {
	_, _, err := RequireAll(context.Background(), nil, "", batchState(t), twoNouls())
	if err == nil {
		t.Fatal("an unnamed required decision must be refused")
	}
	if class, _ := FailureOf(err); class != FailureRefused {
		t.Errorf("class = %q, want %q", class, FailureRefused)
	}
}

// Unmet is how a site reports a refusal it made itself; it must be the same
// type a service failure arrives as, so one caller check covers both.
func TestUnmetIsTheSameTypeAsAServiceFailure(t *testing.T) {
	err := Unmet("require_test_routing", FailureRefused, "redaction mode forbids diff hunks")
	class, ok := FailureOf(err)
	if !ok || class != FailureRefused {
		t.Fatalf("class = %q (ok %v), want %q", class, ok, FailureRefused)
	}
	var re *RequirementError
	if !errors.As(err, &re) {
		t.Fatalf("Unmet must produce a *RequirementError, got %T", err)
	}
	if re.Site != "require_test_routing" {
		t.Errorf("site = %q", re.Site)
	}
	if re.Retryable() {
		t.Error("a local refusal is not retryable")
	}
}

// --- Required and FailClosed: the tier makes the difference ----------------

// A routing-capable site left at logged is being observed, not obeyed. A
// transient failure there must not stop a task, or promotion would be the only
// way to get a reliable system and observation would cost availability.
func TestARoutingCapableSiteAtLoggedDoesNotRequireADecision(t *testing.T) {
	if Required("require_test_routing", TierLogged) {
		t.Error("a site at logged has no consumer for its answer; it cannot be required")
	}
	if Required("require_test_routing", TierOrdering) {
		t.Error("ordering does not permit routing, so the routing effect is still inert")
	}
	if !Required("require_test_routing", TierRouting) {
		t.Error("at routing the answer is acted on, so it must be required")
	}
}

// A site that can never change control flow is never required, at any tier a
// valid configuration could give it.
func TestAnOrderingSiteIsNeverRequired(t *testing.T) {
	for _, tier := range []Tier{TierLogged, TierOrdering, TierRouting} {
		if Required("require_test_ordering", tier) {
			t.Errorf("ordering-only site required at %v", tier)
		}
	}
}

func TestAnUnknownSiteIsNeverRequired(t *testing.T) {
	if Required("no_such_site", TierRouting) {
		t.Error("an unregistered site must not be able to stop a task")
	}
}

func TestFailClosedStopsOnlyWhereTheAnswerWouldBeUsed(t *testing.T) {
	err := Unmet("require_test_routing", FailureUnavailable, "service unreachable")

	if _, stop := FailClosed("require_test_routing", TierLogged, err); stop {
		t.Error("must not stop at logged")
	}
	class, stop := FailClosed("require_test_routing", TierRouting, err)
	if !stop {
		t.Fatal("must stop at routing")
	}
	if class != FailureUnavailable {
		t.Errorf("class = %q, want %q", class, FailureUnavailable)
	}
}

func TestFailClosedIgnoresWhatIsNotARequirementFailure(t *testing.T) {
	if _, stop := FailClosed("require_test_routing", TierRouting, nil); stop {
		t.Error("nil error is not a stop")
	}
	other := errors.New("the disk is full")
	if _, stop := FailClosed("require_test_routing", TierRouting, other); stop {
		t.Error("an unrelated error must not be reported as a judgment stop")
	}
}
