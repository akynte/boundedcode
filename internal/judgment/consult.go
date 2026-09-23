package judgment

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// Source says why a value is the value. It is for the journal, the gate text
// and telemetry, and it is never read to decide anything: a call site that
// branched on Source would have made the judgment load-bearing by the back
// door.
type Source string

const (
	// SourceLive: a judgment arrived and was used.
	SourceLive Source = "live"
	// SourceCache: a judgment from a previous identical request was used.
	SourceCache Source = "cache"
	// SourceDisabled: no judge is configured. The ordinary case.
	SourceDisabled Source = "disabled"
	// SourceUnavailable: a judge is configured but did not answer — offline,
	// unreachable, timed out, or malformed.
	SourceUnavailable Source = "unavailable"
	// SourceLowConfidence: a judgment arrived and was not confident enough, or
	// the call site's own interpretation declined it.
	SourceLowConfidence Source = "low_confidence"
	// SourceError: the request could not be built. A refused state field lands
	// here, which is the point: a refusal is visible rather than silent.
	SourceError Source = "error"
	// SourceRefused: this system declined to ask — a malformed question, an
	// ineligible candidate. Distinct from SourceError because nothing went
	// wrong: a rule was applied.
	SourceRefused Source = "refused"
	// SourceSkipped: the caller decided the judgment could not be used even
	// if it arrived, so it was not requested. Distinct again, because no
	// tokens were spent and nothing was declined.
	SourceSkipped Source = "skipped"
)

// Sources lists every value, for CLI validation and for a trace's filter list.
func Sources() []Source {
	return []Source{SourceLive, SourceCache, SourceDisabled, SourceUnavailable,
		SourceLowConfidence, SourceError, SourceRefused, SourceSkipped}
}

// refuse records a locally declined judgment and returns the fallback shape.
// The record is best-effort: a judge with no recorder simply has none.
func refuse(ctx context.Context, j Judge, qs map[string]Question, source Source, detail string) (map[string]Answer, Note) {
	if r, ok := j.(interface {
		recordRefusal(context.Context, Source, string, []string)
	}); ok {
		ids := make([]string, 0, len(qs))
		for id := range qs {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		r.recordRefusal(ctx, source, detail, ids)
	}
	return unanswered(qs), Note{Source: source, Detail: detail}
}

// Note records how a value was chosen.
type Note struct {
	Source     Source
	Confidence float64
	// Detail is one line for a log or a gate. It never contains state.
	Detail string
	Usage  Usage
	// Class carries the typed failure through to a mandatory caller.
	//
	// Source is deliberately coarse: an advisory site only needs to know
	// that it did not get an answer. A required site needs to know *why*,
	// because a rejected credential, a rate limit and a 502 call for three
	// different responses — and flattening them here would make Require's
	// classification a guess. Empty for a successful call.
	Class FailureClass
}

// endpointOf reports the service a judge talks to, when it will say. It is an
// optional interface for the same reason LastUsage is: a fake judge in a test
// has no endpoint and must not be made to invent one.
func endpointOf(j Judge) string {
	if e, ok := j.(interface{ Endpoint() string }); ok {
		return e.Endpoint()
	}
	return ""
}

// observe reports one AskAll to the site observation in ctx, if a site began
// one. requested says whether a request actually left.
func observe(ctx context.Context, j Judge, note Note, questions int, requested bool) {
	o := observationFrom(ctx)
	if o == nil {
		return
	}
	name := ""
	if j != nil {
		name = j.Name()
	}
	o.asked(requested, note, questions, name, endpointOf(j))
}

// Used reports that a judgment actually changed the answer. Reporting is the
// only thing this is for.
func (n Note) Used() bool { return n.Source == SourceLive || n.Source == SourceCache }

// Query is one judgment and the deterministic answer it may improve on.
type Query[T any] struct {
	Question Question
	State    *State
	// Fallback is what the deterministic code already computed. It is not
	// optional and it is not a zero value to fill in later: it is the answer,
	// and the judgment is an opportunity to do better than it.
	Fallback T
	// Interpret turns a confident answer into T. Returning false keeps
	// Fallback, which is how a call site expresses a threshold that this
	// package cannot know — the probability at which a noul is worth acting
	// on depends on what acting on it costs.
	Interpret func(Answer) (T, bool)
	// MinConfidence gates KindChoice and KindScore before Interpret sees the
	// answer. Zero means the configured threshold, never "accept anything".
	// It does not apply to KindNoul, which reports no confidence: for a noul
	// the probability is the whole answer and Interpret is where the bar goes.
	MinConfidence float64
}

// Consult returns the judged value when there is a confident one, and Fallback
// otherwise.
//
// It does not return an error, and that is the load-bearing decision in this
// package. A signature with an error would be met at every call site with
// `if err != nil { return err }`, and at that moment an advisory judgment
// becomes something a task can fail on — a remote service outage would stop
// work that has no need of it. Everything that can go wrong is a Note instead.
func Consult[T any](ctx context.Context, j Judge, q Query[T]) (T, Note) {
	const id = "q"
	answers, note := AskAll(ctx, j, q.State, map[string]Question{id: q.Question})
	a := answers[id]
	if !a.Answered {
		return q.Fallback, note
	}
	if q.Interpret == nil {
		note.Source = SourceError
		note.Detail = "query has no Interpret"
		return q.Fallback, note
	}
	if a.Kind != KindNoul {
		min := q.MinConfidence
		if min <= 0 {
			min = confidenceFloor(j)
		}
		if a.Confidence < min {
			note.Source = SourceLowConfidence
			note.Detail = fmt.Sprintf("confidence %.2f below %.2f", a.Confidence, min)
			return q.Fallback, note
		}
	}
	value, ok := q.Interpret(a)
	if !ok {
		note.Source = SourceLowConfidence
		note.Detail = "the answer did not meet the call site's bar"
		return q.Fallback, note
	}
	note.Confidence = a.Confidence
	return value, note
}

// AskAll evaluates a set of questions over one state and cannot fail.
//
// Every failure — no judge, an unusable state, a transport error, a response
// missing an answer — comes back as unanswered questions and a Note saying
// why. Callers iterate their own question set and treat Answered false as
// "keep what I had", which is the same composition Consult does for one
// question. Batching is the point: questions in one request are evaluated in
// parallel, so forty of them cost about what one does.
func AskAll(ctx context.Context, j Judge, st *State, qs map[string]Question) (map[string]Answer, Note) {
	if len(qs) == 0 {
		note := Note{Source: SourceDisabled, Detail: "no questions"}
		observe(ctx, j, note, 0, false)
		return map[string]Answer{}, note
	}
	if j == nil {
		j = Off()
	}
	if !j.Available() {
		note := Note{Source: SourceDisabled, Detail: j.Name() + " is not available"}
		observe(ctx, j, note, len(qs), false)
		return unanswered(qs), note
	}
	if st.Empty() {
		a, note := refuse(ctx, j, qs, SourceError, "state is empty")
		observe(ctx, j, note, len(qs), false)
		return a, note
	}
	for id, q := range qs {
		if err := q.Validate(); err != nil {
			a, note := refuse(ctx, j, qs, SourceRefused,
				fmt.Sprintf("question %q: %v", id, err))
			observe(ctx, j, note, len(qs), false)
			return a, note
		}
	}

	answers, err := j.Ask(ctx, st, qs)
	if err != nil {
		note := Note{Source: SourceUnavailable, Detail: err.Error()}
		// A typed API failure keeps its class. Without this every failure
		// reaches a required decision as "unavailable", and the retry
		// semantics of a 401 and a 502 become the same.
		//
		// Source is deliberately left alone. It is the advisory view, and an
		// advisory site's behaviour must not change because a mandatory one
		// now wants more detail: every transport failure stays "unavailable"
		// there, and the finer class travels in its own field.
		var ae *APIError
		if errors.As(err, &ae) {
			note.Class = ae.Class
		}
		// requested: the call was made and failed, which is not a skip.
		observe(ctx, j, note, len(qs), true)
		return unanswered(qs), note
	}

	// Normalise: a caller must be able to range over its own question ids and
	// find every one present, so that "the service omitted this" and "the
	// service said no" are never the same read.
	out := make(map[string]Answer, len(qs))
	answered := 0
	for id, q := range qs {
		a, ok := answers[id]
		if !ok {
			out[id] = Answer{Kind: q.kind}
			continue
		}
		a.Kind = q.kind
		out[id] = a
		if a.Answered {
			answered++
		}
	}
	note := Note{Source: SourceLive,
		Detail: fmt.Sprintf("%d of %d question(s) answered by %s", answered, len(qs), j.Name())}
	if answered == 0 {
		note.Source = SourceUnavailable
	}
	if u, ok := j.(interface{ LastUsage() Usage }); ok {
		note.Usage = u.LastUsage()
	}
	if c, ok := j.(interface{ LastFromCache() bool }); ok && c.LastFromCache() && answered > 0 {
		note.Source = SourceCache
	}
	observe(ctx, j, note, len(qs), true)
	return out, note
}

// confidenceFloor asks the judge for its configured threshold, so that a call
// site which sets no bar gets the operator's rather than none.
func confidenceFloor(j Judge) float64 {
	if f, ok := j.(interface{ MinConfidence() float64 }); ok {
		if v := f.MinConfidence(); v > 0 {
			return v
		}
	}
	return DefaultMinConfidence
}

// taskIDKey identifies the task a judgment belongs to.
//
// It travels in the context rather than in a Request field because a judge is
// built once per supervisor and shared across phases, and because the callers
// that ask questions — a retriever, a localization pass — have no reason to
// know about journalling. The recorder reads it; nothing else does.
type taskIDKey struct{}

// WithTaskID marks a context as belonging to a task, so judgments made under
// it are journalled against that task.
func WithTaskID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, taskIDKey{}, id)
}

// TaskIDFrom reports the task a context belongs to, or "" outside one. A
// judgment with no task is not journalled: recording it under an empty task id
// would put it in every task's trace.
func TaskIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(taskIDKey{}).(string)
	return id
}

// phaseKey labels judgments with the pipeline phase that asked for them.
//
// In the context rather than on the judge, because one judge is shared across
// every phase of a task and a field on it would be a race waiting to be found.
type phaseKey struct{}

// WithPhase marks a context as belonging to a pipeline phase, so judgments
// made under it are journalled against it.
func WithPhase(ctx context.Context, phase string) context.Context {
	return context.WithValue(ctx, phaseKey{}, phase)
}

// PhaseFrom reports the phase a context belongs to, or "" outside one.
func PhaseFrom(ctx context.Context) string {
	phase, _ := ctx.Value(phaseKey{}).(string)
	return phase
}
