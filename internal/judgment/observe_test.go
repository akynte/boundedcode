package judgment

// The property these cover is the one the table exists for: a consultation
// that finds nothing must still leave an attributable record, so that a
// site-level rate has a denominator. A test that only proved findings are
// recorded would pass against the instrumentation this replaced.

import (
	"context"
	"errors"
	"testing"
)

type capture struct{ got []Consultation }

func (c *capture) Consultation(_ context.Context, rec Consultation) { c.got = append(c.got, rec) }

// answering is a judge that answers every question with a noul.
type answering struct {
	name      string
	available bool
	err       error
	p         float64
}

func (a *answering) Name() string    { return a.name }
func (a *answering) Available() bool { return a.available }
func (a *answering) Ask(_ context.Context, _ *State, qs map[string]Question) (map[string]Answer, error) {
	if a.err != nil {
		return nil, a.err
	}
	out := map[string]Answer{}
	for id := range qs {
		out[id] = Answer{Answered: true, Kind: KindNoul, Noul: a.p}
	}
	return out, nil
}

func (a *answering) Endpoint() string { return "https://example.invalid/v1" }

func stateWith(t *testing.T) *State {
	t.Helper()
	st := NewState(RedactStrict)
	if err := st.Objective("fix the thing"); err != nil {
		t.Fatalf("objective: %v", err)
	}
	return st
}

func noul(t *testing.T) Question {
	t.Helper()
	return Noul("is this so")
}

// A consultation that is answered and yields no finding is the case the whole
// table exists for: before it, this left no row anywhere.
func TestCleanConsultationIsRecordedWithItsSite(t *testing.T) {
	cap := &capture{}
	ctx := WithObserver(context.Background(), cap)
	ctx = WithTaskID(ctx, "t-1")
	ctx = WithPhase(ctx, "VERIFY")

	jctx, obs := Begin(ctx, "verification_integrity")
	j := &answering{name: "typesafe:jev-test", available: true, p: 0.02}
	if _, note := AskAll(jctx, j, stateWith(t), map[string]Question{"a": noul(t)}); note.Source != SourceLive {
		t.Fatalf("source = %q, want live", note.Source)
	}
	obs.Finish(context.Background(), 0) // nothing found: the point of the case

	if len(cap.got) != 1 {
		t.Fatalf("recorded %d consultations, want exactly 1", len(cap.got))
	}
	c := cap.got[0]
	if c.Site != "verification_integrity" {
		t.Errorf("site = %q, want verification_integrity", c.Site)
	}
	if c.TaskID != "t-1" || c.Phase != "VERIFY" {
		t.Errorf("task/phase = %q/%q, want t-1/VERIFY", c.TaskID, c.Phase)
	}
	if !c.Reached || !c.Requested {
		t.Errorf("reached=%v requested=%v, want both true", c.Reached, c.Requested)
	}
	if c.Findings != 0 {
		t.Errorf("findings = %d, want 0", c.Findings)
	}
	if c.Questions != 1 {
		t.Errorf("questions = %d, want 1", c.Questions)
	}
	if c.Status != SourceLive {
		t.Errorf("status = %q, want live", c.Status)
	}
	if c.Model == "" || c.Endpoint == "" {
		t.Errorf("model/endpoint = %q/%q, want both set", c.Model, c.Endpoint)
	}
	if c.ID == "" {
		t.Error("no id, so no prediction could link to it")
	}
}

// A positive finding is recorded with the same attribution, so the two are
// comparable as a numerator over a denominator.
func TestFindingIsRecordedWithTheSameAttribution(t *testing.T) {
	cap := &capture{}
	ctx := WithObserver(context.Background(), cap)
	jctx, obs := Begin(ctx, "diff_conformance")
	j := &answering{name: "typesafe:jev-test", available: true, p: 0.91}
	_, _ = AskAll(jctx, j, stateWith(t), map[string]Question{"a": noul(t), "b": noul(t)})
	obs.Finish(context.Background(), 2)

	if len(cap.got) != 1 {
		t.Fatalf("recorded %d, want 1", len(cap.got))
	}
	if c := cap.got[0]; c.Findings != 2 || c.Site != "diff_conformance" || c.Questions != 2 {
		t.Errorf("got findings=%d site=%q questions=%d", c.Findings, c.Site, c.Questions)
	}
}

// A site that declines before asking must be distinguishable from one that was
// never reached. Never reached is the absence of a row; this is a row.
func TestPreRequestSkipIsRecordedAndDistinctFromNeverReached(t *testing.T) {
	cap := &capture{}
	ctx := WithObserver(context.Background(), cap)
	_, obs := Begin(ctx, "failure_triage")
	obs.Skip("too_many_candidates")
	obs.Finish(context.Background(), 0)

	if len(cap.got) != 1 {
		t.Fatalf("recorded %d, want 1", len(cap.got))
	}
	c := cap.got[0]
	if !c.Reached {
		t.Error("reached = false; a site that ran and skipped was reached")
	}
	if c.Requested {
		t.Error("requested = true; no request was made")
	}
	if c.SkipReason != "too_many_candidates" {
		t.Errorf("skip reason = %q, want too_many_candidates", c.SkipReason)
	}
	if c.Status != SourceSkipped {
		t.Errorf("status = %q, want skipped", c.Status)
	}
}

// Every way of not answering still produces one attributable row.
func TestUnavailableRefusedAndDisabledAreEachRecorded(t *testing.T) {
	for _, tc := range []struct {
		name   string
		judge  Judge
		state  func(*testing.T) *State
		want   Source
		wantRq bool
	}{
		{"transport error", &answering{name: "j", available: true, err: errors.New("dial failed")},
			stateWith, SourceUnavailable, true},
		{"judge off", &answering{name: "j", available: false}, stateWith, SourceDisabled, false},
		{"empty state", &answering{name: "j", available: true},
			func(*testing.T) *State { return NewState(RedactStrict) }, SourceError, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cap := &capture{}
			ctx := WithObserver(context.Background(), cap)
			jctx, obs := Begin(ctx, "progress_monitor")
			_, _ = AskAll(jctx, tc.judge, tc.state(t), map[string]Question{"a": noul(t)})
			obs.Finish(context.Background(), 0)

			if len(cap.got) != 1 {
				t.Fatalf("recorded %d, want 1", len(cap.got))
			}
			c := cap.got[0]
			if c.Status != tc.want {
				t.Errorf("status = %q, want %q", c.Status, tc.want)
			}
			if c.Requested != tc.wantRq {
				t.Errorf("requested = %v, want %v", c.Requested, tc.wantRq)
			}
			if c.Site != "progress_monitor" {
				t.Errorf("site = %q", c.Site)
			}
		})
	}
}

// Finish is idempotent, so a site may both defer it and call it explicitly
// without inflating its own denominator.
func TestFinishEmitsExactlyOnce(t *testing.T) {
	cap := &capture{}
	ctx := WithObserver(context.Background(), cap)
	_, obs := Begin(ctx, "note_relevance")
	obs.Finish(context.Background(), 0)
	obs.Finish(context.Background(), 3)
	if len(cap.got) != 1 {
		t.Fatalf("recorded %d, want 1", len(cap.got))
	}
	if cap.got[0].Findings != 0 {
		t.Errorf("findings = %d, want the first call's 0", cap.got[0].Findings)
	}
}

// Observation must never be load-bearing: with no observer wired, everything
// still runs and answers exactly as before.
func TestWithoutAnObserverNothingChanges(t *testing.T) {
	ctx := context.Background() // deliberately no observer
	jctx, obs := Begin(ctx, "review_rubric")
	j := &answering{name: "j", available: true, p: 0.5}
	answers, note := AskAll(jctx, j, stateWith(t), map[string]Question{"a": noul(t)})
	obs.Finish(context.Background(), 1)
	if note.Source != SourceLive {
		t.Errorf("source = %q, want live", note.Source)
	}
	if !answers["a"].Answered {
		t.Error("the answer changed when no observer was wired")
	}
}

// A nil Observation is safe, so a call site needs no guard.
func TestNilObservationIsSafe(t *testing.T) {
	var o *Observation
	o.Skip("x")
	o.Subjects(1)
	o.Link("y")
	o.Finish(context.Background(), 2)
	if o.ID() != "" {
		t.Error("nil observation invented an id")
	}
}

// The record is durable and lands in bug reports, so it must carry no text
// that a model or a repository could have supplied. Everything stored is a
// count, an identifier, or a value from a fixed set.
func TestNoFreeTextIsPersisted(t *testing.T) {
	cap := &capture{}
	ctx := WithObserver(context.Background(), cap)
	jctx, obs := Begin(ctx, "context_injection")

	// A question whose validation fails produces a formatted Detail; it must
	// not reach the record.
	bad := Choice("pick one", map[string]string{"only": "one option is invalid"})
	_, note := AskAll(jctx, &answering{name: "j", available: true}, stateWith(t),
		map[string]Question{"q": bad})
	obs.Finish(context.Background(), 0)

	if note.Detail == "" {
		t.Fatal("expected a formatted Detail on the note, so the case is real")
	}
	c := cap.got[0]
	if c.SkipReason == note.Detail {
		t.Errorf("the note's Detail was persisted verbatim: %q", c.SkipReason)
	}
	for _, ok := range Sources() {
		if c.SkipReason == string(ok) {
			return // a value from the fixed set
		}
	}
	t.Errorf("skip reason %q is not one of the fixed Source values", c.SkipReason)
}

// A site that asked, got an answer, and then reports its (empty) SkipReason
// must stay recorded as live. Labelling it a skip would understate how often
// the judge was actually used, which is the number the denominator exists for.
func TestAnEmptySkipReasonDoesNotRelabelALiveConsultation(t *testing.T) {
	cap := &capture{}
	ctx := WithObserver(context.Background(), cap)
	jctx, obs := Begin(ctx, "intake_profile")
	_, _ = AskAll(jctx, &answering{name: "j", available: true, p: 0.3}, stateWith(t),
		map[string]Question{"a": noul(t)})
	obs.Skip("") // what a call site passes when its result did not skip
	obs.Finish(context.Background(), 1)

	c := cap.got[0]
	if c.Status != SourceLive {
		t.Errorf("status = %q, want live", c.Status)
	}
	if !c.Requested {
		t.Error("requested = false after a real request")
	}
	if c.SkipReason != "" {
		t.Errorf("skip reason = %q, want empty", c.SkipReason)
	}
}
