package judgment

// Site-level observation: one attributable record per consultation, including
// the ones that find nothing.
//
// The journal in typesafe.go records that a *request* left the machine, and
// ledger.CalibrationStore records a *finding*. Neither answers "was this site
// consulted at all", because a site that is asked and reports nothing writes to
// neither. That makes the denominator unrecoverable: "never reached" and
// "reached, asked, clean" are the same absence of rows, and a site cannot be
// shown to be useless or useful without knowing how often it was consulted.
//
// So this records the consultation itself. It is observational in the strict
// sense — nothing here is read by any decision path, an Observer that fails is
// ignored, and an absent Observer changes nothing but the records. It carries
// counts, statuses and identifiers, never state, never question text and never
// repository content: the same rule the journal follows, for the same reason.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"
)

// newConsultationID is a random identifier. A failure to read randomness
// leaves it empty rather than failing a task: an unlinked record is a smaller
// loss than a task that stopped because observation could not name itself.
func newConsultationID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

// Consultation is one site's use of the judge, recorded whatever the outcome.
type Consultation struct {
	ID     string
	Site   string
	TaskID string
	Phase  string
	// Reached is always true for a record that exists: the site ran. It is
	// stored anyway so a query does not have to know that, and so "no row"
	// keeps its one meaning — the site was never reached.
	Reached bool
	// Requested reports whether a request actually left for the service.
	// False with Reached true is the deliberate pre-request skip.
	Requested bool
	// Status is the Source that describes the outcome. For a pre-request
	// skip it is SourceSkipped.
	Status Source
	// SkipReason is the site's own reason, verbatim from its result type
	// (for example "no_judge", "no_objective", "too_many_candidates").
	SkipReason string
	// Questions is how many were submitted; Subjects how many distinct
	// things they were about, when a site distinguishes them.
	Questions int
	Subjects  int
	// Findings is how many the site produced from the answers. Zero with
	// Status live is the clean consultation this type exists to record.
	Findings int
	Model    string
	Endpoint string
	Latency  time.Duration
	// PredictionIDs links to judgment_predictions rows this consultation
	// produced, when it produced any.
	PredictionIDs []string
	At            time.Time
}

// Observer persists consultations. It is an interface here for the same reason
// Recorder is: this package must not depend on the store.
type Observer interface {
	// Consultation records one. It returns no error: a failure to observe
	// must never change what a task does.
	Consultation(ctx context.Context, c Consultation)
}

type observerKey struct{}

// WithObserver attaches the observer every consultation is recorded through.
// Without one, observation is a no-op and nothing else changes.
func WithObserver(ctx context.Context, o Observer) context.Context {
	if o == nil {
		return ctx
	}
	return context.WithValue(ctx, observerKey{}, o)
}

func observerFrom(ctx context.Context) Observer {
	o, _ := ctx.Value(observerKey{}).(Observer)
	return o
}

type siteKey struct{}

// WithSite names the site a context's consultations belong to. Each site
// wrapper sets it once, so AskAll can attribute a request without every
// question carrying the name.
func WithSite(ctx context.Context, site string) context.Context {
	if site == "" {
		return ctx
	}
	return context.WithValue(ctx, siteKey{}, site)
}

// SiteFrom reports the site a context belongs to, or "" outside one.
func SiteFrom(ctx context.Context) string {
	s, _ := ctx.Value(siteKey{}).(string)
	return s
}

// observeTimeout bounds the detached write of a consultation record. It
// matches ledger.recordTimeout and is short for the same reason: the write is
// one small INSERT against a local file, and a diagnostic must never be able
// to hang the path it is observing.
const observeTimeout = 10 * time.Second

// Observation accumulates one consultation while a site runs.
//
// A site begins one, the AskAll it calls fills in what the request did, and
// the site finishes it with its own counts. Exactly one record is emitted per
// Begin, by Finish.
type Observation struct {
	id       string
	c        Consultation
	obs      Observer
	started  time.Time
	finished bool
}

// ID identifies this consultation, so a prediction it produced can name it.
// It is stable from Begin, before the record is written.
func (o *Observation) ID() string {
	if o == nil {
		return ""
	}
	return o.id
}

type observationKey struct{}

// Begin starts an observation for a site and returns the context its AskAll
// must be given. The returned Observation is finished exactly once, normally
// with defer, and finishing it is what writes the record.
//
// Begin never returns nil, so a call site needs no guard.
func Begin(ctx context.Context, site string) (context.Context, *Observation) {
	o := &Observation{
		id: newConsultationID(),
		c: Consultation{
			Site:    site,
			TaskID:  TaskIDFrom(ctx),
			Phase:   PhaseFrom(ctx),
			Reached: true,
			Status:  SourceSkipped,
		},
		obs:     observerFrom(ctx),
		started: time.Now(),
	}
	ctx = WithSite(ctx, site)
	ctx = context.WithValue(ctx, observationKey{}, o)
	return ctx, o
}

func observationFrom(ctx context.Context) *Observation {
	o, _ := ctx.Value(observationKey{}).(*Observation)
	return o
}

// asked is called by AskAll with what the request did. A site that calls
// AskAll more than once keeps the last request's status and sums the
// questions, which is what a "how many questions did this site submit" query
// needs.
func (o *Observation) asked(requested bool, note Note, questions int, model, endpoint string) {
	if o == nil {
		return
	}
	o.c.Requested = o.c.Requested || requested
	o.c.Status = note.Source
	o.c.Questions += questions
	if model != "" {
		o.c.Model = model
	}
	if endpoint != "" {
		o.c.Endpoint = endpoint
	}
	// Deliberately not note.Detail. Detail is a formatted human string that
	// today is structural but is free to change, and this row is durable and
	// ends up in bug reports. The status already says what happened; a reason
	// with more shape than that comes from the site's own constant, via Skip.
	if !requested && o.c.SkipReason == "" {
		o.c.SkipReason = string(note.Source)
	}
}

// Skip records that the site declined before any request, with its own reason.
// It does not emit: Finish still does that, so there is one record per Begin.
//
// An empty reason is a no-op. Call sites pass their result's SkipReason
// unconditionally, and that field is empty on the path where the site did ask
// — forcing the status there would label a live consultation a skip and make
// the skip rate wrong in the one direction that flatters the instrumentation.
// A request that was made also wins over a late Skip, for the same reason.
func (o *Observation) Skip(reason string) {
	if o == nil || reason == "" || o.c.Requested {
		return
	}
	o.c.Status = SourceSkipped
	o.c.SkipReason = reason
}

// Subjects records how many distinct things the site judged about.
func (o *Observation) Subjects(n int) {
	if o != nil {
		o.c.Subjects = n
	}
}

// Link records a prediction this consultation produced.
func (o *Observation) Link(id string) {
	if o != nil && id != "" {
		o.c.PredictionIDs = append(o.c.PredictionIDs, id)
	}
}

// Finish emits the record. Calling it twice emits once: a site with several
// return paths can defer it and also call it explicitly without double
// counting.
//
// It takes the caller's context and detaches it under a bound, the same way
// ledger.recording does for an interrupted journal write, and for the same
// two reasons.
//
// Cancellation is dropped because the task's context carries the wall-clock
// budget deadline, and the moment that deadline fires is exactly when the
// record explaining what happened matters most: writing through the cancelled
// context would lose it. Values are kept, so tracing still resolves.
//
// The bound replaces the inherited deadline rather than removing deadlines
// altogether. WithoutCancel alone would let a wedged database hang a task's
// exit on a diagnostic write, which is the opposite of what observation is
// for. One small INSERT against a local file does not need longer.
func (o *Observation) Finish(ctx context.Context, findings int) {
	if o == nil || o.finished {
		return
	}
	o.finished = true
	o.c.ID = o.id
	o.c.Findings = findings
	o.c.Latency = time.Since(o.started)
	o.c.At = o.started
	if o.c.SkipReason == "" && !o.c.Requested {
		o.c.SkipReason = "not_requested"
	}
	if o.obs == nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), observeTimeout)
	defer cancel()
	o.obs.Consultation(writeCtx, o.c)
}

// Record is what a site with no Observation of its own can still emit — used
// where a consultation is a single self-contained call.
func Record(ctx context.Context, c Consultation) {
	o := observerFrom(ctx)
	if o == nil {
		return
	}
	c.Reached = true
	if c.TaskID == "" {
		c.TaskID = TaskIDFrom(ctx)
	}
	if c.Phase == "" {
		c.Phase = PhaseFrom(ctx)
	}
	if c.At.IsZero() {
		c.At = time.Now()
	}
	o.Consultation(ctx, c)
}
