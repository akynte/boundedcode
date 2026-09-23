package retrieval

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/akynte/boundedcode/internal/judgment"
)

// Judged reranking of retrieval candidates (§8.2).
//
// The problem this addresses is stated by the code it changes. A lexical
// anchor carries a bm25 score; an expanded node carries 0.5/depth; an explicit
// or mandatory slice carries 1.0. Those are three different quantities, so
// sortByScore has only ever sorted inside an origin, and GraphBudgetFraction
// exists to fence expansion off rather than to rank it — a units workaround,
// not a relevance decision.
//
// Asking the same probabilistic proposition of every candidate produces one
// number per candidate drawn from one question. That is a *common semantic
// relevance signal*. Whether it is useful and sufficiently calibrated to order
// candidates across origins in this repository domain is what the pilot
// measures; it is not established here, and no code in this file should be
// read as assuming it.
//
// Everything here is advisory. Rerank can reorder candidates and shift how the
// budget is filled. It cannot introduce a slice retrieval did not find, remove
// one, or change any slice's provenance — Score is left exactly as it was, as
// the deterministic record of why a slice is here. A judge that is off leaves
// every field untouched.
//
// It runs *after* Guard and after the deterministic egress-eligibility filter,
// and that order is load-bearing rather than incidental: a candidate that
// belongs to another workspace, or whose path is egress-sensitive, must not
// contribute so much as its name to a request.

// Defaults for the judged rerank. Production uses these; the eval harness
// overrides them per arm through RerankTuning, so a threshold can be fitted on
// the dev set without recompiling.
const (
	// DefaultRerankBatch is how many candidates go in one request. Questions
	// in a single request are evaluated together, so the saving is in packing
	// them rather than in issuing more requests.
	DefaultRerankBatch = 40
	// DefaultRerankMaxCandidates is the largest set this will judge at all.
	//
	// Beyond it the rerank is skipped entirely and no request is made. The
	// earlier version judged the first 80 and left the tail unjudged, which
	// made allJudged false, discarded every answer, and billed for them —
	// an experiment whose treatment could not affect the result, run at full
	// price. Skipping and saying so is the honest behaviour.
	DefaultRerankMaxCandidates = 80
	// DefaultRerankConcurrency bounds requests in flight.
	DefaultRerankConcurrency = 2
	// DefaultRerankBudget bounds the whole rerank, as GrepBudget bounds
	// lexical search. Retrieval is on the hot path of every step and the
	// deterministic ordering is good enough to proceed on.
	DefaultRerankBudget = 1500 * time.Millisecond
	// DefaultRerankExcerptBytes caps the snippet sent per candidate, in
	// repo_text mode only.
	DefaultRerankExcerptBytes = 300
)

// RerankTuning is the knob set for judged reranking and the judged budget
// fill.
//
// It exists so the eval harness can vary these on the dev set while production
// keeps one set of defaults. Zero values mean "use the default", so a caller
// that sets one field gets the shipped behaviour for the rest.
type RerankTuning struct {
	Batch          int
	MaxCandidates  int
	Concurrency    int
	Budget         time.Duration
	ExcerptBytes   int
	RelevanceFloor float64
	GraphFloor     float64
	// FailureQuery, when set, adds a second relevance proposition per candidate —
	// design.md mechanism M9 — asking whether the candidate would help
	// resolve this specific open failure, rather than only the objective. A
	// candidate's Relevance becomes the greater of the two answers: either
	// reason is sufficient for an engineer to need to read it. The zero value
	// (no headline) keeps today's single-proposition behaviour exactly,
	// including for the benchmarked arm, which never sets this — see
	// design.md's own note that "the objective proposition alone remains the
	// benchmarked arm".
	FailureQuery FailureQuery
}

// FailureQuery names an open verification failure to rank relevance against,
// alongside the objective. Both fields empty means "no failure to key on".
//
// Symbols is a single pre-joined string rather than a slice so RerankTuning
// — which embeds a FailureQuery — stays comparable with ==, which
// internal/eval's preflight report relies on to tell an overridden tuning
// from the shipped defaults without reflection.
type FailureQuery struct {
	// Headline is the failure's normalized summary line.
	Headline string
	// Symbols are workflow.FailureRecord's primary symbols, space-joined,
	// when resolved.
	Symbols string
}

// empty reports a FailureQuery with nothing useful to ask about.
func (f FailureQuery) empty() bool {
	return strings.TrimSpace(f.Headline) == "" && strings.TrimSpace(f.Symbols) == ""
}

// withDefaults fills the zero fields.
func (t RerankTuning) withDefaults() RerankTuning {
	if t.Batch <= 0 {
		t.Batch = DefaultRerankBatch
	}
	if t.MaxCandidates <= 0 {
		t.MaxCandidates = DefaultRerankMaxCandidates
	}
	if t.Concurrency <= 0 {
		t.Concurrency = DefaultRerankConcurrency
	}
	if t.Budget <= 0 {
		t.Budget = DefaultRerankBudget
	}
	if t.ExcerptBytes <= 0 {
		t.ExcerptBytes = DefaultRerankExcerptBytes
	}
	if t.RelevanceFloor <= 0 {
		t.RelevanceFloor = DefaultRelevanceFloor
	}
	if t.GraphFloor <= 0 {
		t.GraphFloor = DefaultGraphFloorFraction
	}
	return t
}

// Skip reasons. They are recorded on the packet and carried into the
// evaluation, because "the treatment did not run" and "the treatment ran and
// did nothing" are different results and a benchmark that cannot separate them
// is not measuring anything.
const (
	SkipNoJudge        = "no_judge"
	SkipNoObjective    = "no_objective"
	SkipNoCandidates   = "no_candidates"
	SkipTooMany        = "too_many_candidates"
	SkipIneligible     = "ineligible_candidate"
	SkipPartialAnswer  = "partial_answer"
	SkipUnavailable    = "judge_unavailable"
	SkipStateRefused   = "state_refused"
	SkipBelowBatchSize = "no_questions_built"
)

// RerankResult is what a rerank did, in enough detail to interpret a
// benchmark row.
type RerankResult struct {
	// Attempted says a judge was configured and a rerank was considered.
	Attempted bool
	// Applied says judgment scores actually determined the ordering and the
	// budget fill. It is false whenever the packet fell back to the
	// deterministic path, including when some candidates were answered.
	Applied bool
	// Total is how many candidates were in scope; Judged is how many carried
	// an answer. Judged > 0 with Applied false is the partial-answer case.
	Total, Judged int
	// Source is where the answers came from: live, cache, disabled, …
	Source string
	// SkipReason is set whenever Applied is false.
	SkipReason string
	// Requests and Usage are what the rerank cost.
	Requests     int
	InputTokens  int
	OutputTokens int
	// Latency is wall time for the whole rerank.
	Latency time.Duration
	// Protected counts candidates held out of the request because their
	// paths may not be disclosed. They keep their deterministic order and
	// stay in the packet; what they never do is leave the machine.
	Protected int
}

// relevanceRubric is the definition of relevance every judged question
// carries.
//
// The earlier wording ended "…and for tests, fixtures or generated files
// unless the objective is about them", which is wrong in this system
// specifically. A test that pins the required behaviour is often the only
// place the requirement is written down — this project's own nil-deref
// evaluation task turns on exactly that — and an objective phrased as "make it
// return an error instead of panicking" is not "about tests" by any reading.
// Down-ranking the artefact that defines correctness because the task text did
// not mention it is the opposite of what a reranker is for.
//
// So the rubric is engineering necessity, for implementing *or verifying*, and
// the exclusions are about irrelevance rather than about file category.
const relevanceRubric = "A candidate is relevant if reading it would materially help an " +
	"engineer implement or verify the objective correctly. That includes code that must " +
	"change, contracts and interfaces that constrain the change, consumers and callers " +
	"whose expectations the change must not break, tests that define the required " +
	"behaviour, and configuration or migrations the change depends on. " +
	"It excludes code that merely shares vocabulary with the objective, code that " +
	"mentions the same names only in passing, and fixtures, generated code or vendored " +
	"dependencies with no bearing on this task. Judge by whether reading it changes what " +
	"a careful engineer would do, not by what kind of file it is."

// RelevanceRubric is the shared definition of relevance, exported so the
// localization pass asks the same question the retriever does. Two rubrics
// that drifted apart would make the two stages disagree about what they are
// selecting for, which is worse than either being wrong on its own.
func RelevanceRubric() string { return relevanceRubric }

// relevanceProposition builds the complete question for one candidate.
//
// The candidate is named in the instructions, not in the criteria and not by
// the question's map key. TypeSafe documents noul criteria as an optional
// true/false pair and the key is never sent, so a per-candidate binding
// anywhere else is a proposition the service was never given.
func relevanceProposition(collection, id string) string {
	return "Considering the candidate with id `" + id + "` in `" + collection + "`: " +
		"would an engineer need to read that candidate to carry out `objective` correctly? " +
		relevanceRubric
}

// failureProposition is relevanceProposition's counterpart for mechanism M9:
// the same rubric, aimed at resolving a specific open failure instead of the
// objective as a whole. `failure` is named separately from `objective` in the
// state (see rerankBatch) so the two propositions ask genuinely different
// questions rather than one repeating the other under a new label.
func failureProposition(collection, id string) string {
	return "Considering the candidate with id `" + id + "` in `" + collection + "`: " +
		"would an engineer need to read that candidate to resolve `failure` — the " +
		"verification failure blocking `objective` — rather than to carry out the " +
		"objective from scratch? " + relevanceRubric
}

// Rerank puts one comparable relevance on every candidate, or on none.
//
// It returns no error on purpose. A rerank that failed is a rerank that did
// not happen, and Build must not fail because an advisory call did — a call
// site that could receive an error here would turn a remote service's outage
// into a stopped task. What went wrong comes back in RerankResult and in logf.
//
// Its contract with the caller is all-or-nothing. Either every candidate
// carries an answer and Applied is true, or no candidate is treated as judged
// and the deterministic ordering stands untouched. There is no partial mode,
// because mixing a probability against a raw bm25 value is precisely the units
// error the deterministic path exists to avoid.
func Rerank(ctx context.Context, j judgment.Judge, objective string, slices []Slice,
	tuning RerankTuning, logf func(string, ...any)) RerankResult {

	if logf == nil {
		logf = func(string, ...any) {}
	}
	t := tuning.withDefaults()
	res := RerankResult{Total: len(slices), Source: string(judgment.SourceDisabled)}

	switch {
	case j == nil || !j.Available():
		res.SkipReason = SkipNoJudge
		return res
	case objective == "":
		res.SkipReason = SkipNoObjective
		return res
	case len(slices) == 0:
		res.SkipReason = SkipNoCandidates
		return res
	}
	res.Attempted = true

	// Every candidate must be disclosable, or none is asked about. A partial
	// question set cannot produce a complete ordering, and a complete
	// ordering is the only kind this uses.
	for _, s := range slices {
		if !externalizable(s) {
			res.SkipReason = SkipIneligible
			res.Source = string(judgment.SourceRefused)
			logf("rerank: skipped; a candidate is not eligible for egress and no candidate " +
				"set may be sent without it")
			return res
		}
	}

	if len(slices) > t.MaxCandidates {
		res.SkipReason = SkipTooMany
		res.Source = string(judgment.SourceSkipped)
		logf("rerank: skipped; %d candidates exceeds the %d this can judge completely, "+
			"and a partial judgment would be paid for and discarded", len(slices), t.MaxCandidates)
		return res
	}

	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, t.Budget)
	defer cancel()

	mode := redactMode(j)
	order := judgeOrder(slices)

	var (
		mu     sync.Mutex
		sem    = make(chan struct{}, t.Concurrency)
		wg     sync.WaitGroup
		scored = map[int]scoredCandidate{}
		notes  []judgment.Note
	)
	for start := 0; start < len(order); start += t.Batch {
		end := min(start+t.Batch, len(order))
		batch := order[start:end]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			got, note := rerankBatch(ctx, j, mode, objective, t.FailureQuery, slices, batch, t, logf)
			mu.Lock()
			defer mu.Unlock()
			notes = append(notes, note)
			for idx, p := range got {
				scored[idx] = p
			}
		}()
	}
	wg.Wait()

	res.Latency = time.Since(started)
	res.Judged = len(scored)
	for _, n := range notes {
		res.Requests += n.Usage.Requests
		res.InputTokens += n.Usage.InputTokens
		res.OutputTokens += n.Usage.OutputTokens
		if n.Used() {
			res.Source = string(n.Source)
		} else if res.Source == string(judgment.SourceDisabled) {
			res.Source = string(n.Source)
		}
	}

	if len(scored) != len(slices) {
		// Nothing is written to the slices. A partial answer leaves the
		// candidate set byte-identical to what the deterministic path
		// produced, so the fallback is not a reconstruction of the old order
		// — it is the old order, never having been touched.
		res.SkipReason = SkipPartialAnswer
		if res.Judged == 0 {
			res.SkipReason = SkipUnavailable
		}
		logf("rerank: %d of %d candidates answered; the packet keeps its deterministic order",
			res.Judged, len(slices))
		return res
	}

	for idx, c := range scored {
		slices[idx].Relevance = c.relevance
		slices[idx].Judged = true
		slices[idx].RelevanceSource = c.source
	}
	res.Applied = true
	return res
}

// scoredCandidate is one candidate's judged relevance, plus which
// proposition it came from when more than one was asked (mechanism M9).
type scoredCandidate struct {
	relevance float64
	// source is "objective", "failure", or "" when only one proposition was
	// ever in play (no FailureQuery was supplied) — Slice.RelevanceSource's
	// doc comment explains why "" is left unset in that ordinary case.
	source string
}

// rerankBatch asks about one batch and returns relevance by slice index.
//
// When failure is non-empty it asks a second proposition per candidate
// (failureProposition) alongside the objective's, and keeps the greater of
// the two: either reason is sufficient for an engineer to need to read the
// candidate, so this is a maximum rather than an average — design.md
// mechanism M9 is explicit that averaging would blend two different
// questions into a number that answers neither.
func rerankBatch(ctx context.Context, j judgment.Judge, mode judgment.RedactMode,
	objective string, failure FailureQuery, slices []Slice, batch []int, t RerankTuning,
	logf func(string, ...any)) (map[int]scoredCandidate, judgment.Note) {

	const collection = "candidates"
	st := judgment.NewState(mode)
	if err := st.Objective(objective); err != nil {
		logf("rerank: %v", err)
		return nil, judgment.Note{Source: judgment.SourceError, Detail: err.Error()}
	}
	// The failure fact is supervisor-computed, normalized text — the same
	// kind of content Objective already carries, not model output — so it
	// goes through TrustedFact rather than ClaimText. A refusal here (over
	// the scalar cap, or a stray credential in an environmental error) drops
	// the failure question rather than the batch: the objective proposition
	// alone is always available, per R2.
	failureOK := false
	if !failure.empty() {
		fact := strings.TrimSpace(failure.Headline + " " + failure.Symbols)
		if err := st.TrustedFact("failure", fact); err != nil {
			logf("rerank: failure-keyed proposition dropped: %v", err)
		} else {
			failureOK = true
		}
	}

	items := make([]*judgment.RepoItem, 0, len(batch))
	ids := make([]string, 0, len(batch))
	qs := make(map[string]judgment.Question, len(batch))
	for n, idx := range batch {
		s := slices[idx]
		id := "c" + strconv.Itoa(n)

		// NewRepoItem is where the path is checked. Rerank has already
		// refused the whole set if any candidate was ineligible, so a
		// refusal here is a disagreement between the two checks and is worth
		// failing the batch over rather than papering past.
		item, err := st.NewRepoItem(id, s.Path)
		if err != nil {
			logf("rerank: %v", err)
			return nil, judgment.Note{Source: judgment.SourceRefused, Detail: err.Error()}
		}
		item.Meta("symbol", s.Symbol).Meta("kind", string(s.Kind)).Lines(s.StartLine, s.EndLine)

		// Signatures and excerpts are repository source. Under the default
		// redaction mode they do not go, and the judgment runs on paths and
		// symbol names — a weaker question, asked rather than skipped.
		if mode.AtLeast(judgment.RedactRepoText) {
			if err := item.Text("signature", s.Signature, st); err != nil {
				logf("rerank: %v", err)
				return nil, judgment.Note{Source: judgment.SourceRefused, Detail: err.Error()}
			}
			if err := item.Text("excerpt", truncate(s.Body, t.ExcerptBytes), st); err != nil {
				logf("rerank: %v", err)
				return nil, judgment.Note{Source: judgment.SourceRefused, Detail: err.Error()}
			}
		}

		items = append(items, item)
		ids = append(ids, id)
		qs[id] = judgment.Noul(relevanceProposition(collection, id))
		if failureOK {
			qs["f"+id] = judgment.Noul(failureProposition(collection, id))
		}
	}
	if err := st.AddItems(collection, items); err != nil {
		logf("rerank: %v", err)
		return nil, judgment.Note{Source: judgment.SourceError, Detail: err.Error()}
	}

	answers, note := judgment.AskAll(ctx, j, st, qs)
	if !note.Used() {
		logf("rerank: %s", note.Detail)
		return nil, note
	}
	out := make(map[int]scoredCandidate, len(batch))
	for n, idx := range batch {
		a := answers[ids[n]]
		if !a.Answered {
			continue
		}
		c := scoredCandidate{relevance: a.Noul}
		if failureOK {
			c.source = "objective"
			if fa := answers["f"+ids[n]]; fa.Answered && fa.Noul > c.relevance {
				c.relevance = fa.Noul
				c.source = "failure"
			}
		}
		out[idx] = c
	}
	return out, note
}

// externalizable reports whether a slice may be described to a service
// outside this machine.
//
// Two conditions, and both are deterministic. The path must be a real
// repository path — a fallback FQN cannot be checked as one — and it must not
// be egress-sensitive. Neither is ever decided by a model: a judgment about
// whether something is safe to send has to be sent somewhere first.
func externalizable(s Slice) bool {
	return s.PathKnown && judgment.Eligible(s.Path)
}

// judgeOrder lists candidate indexes best-first by the deterministic score,
// inside each origin. The set is judged completely or not at all, so this
// decides only the order questions are packed into batches — which matters
// only if a batch fails.
func judgeOrder(slices []Slice) []int {
	byOrigin := map[Origin][]int{}
	var origins []Origin
	for i, s := range slices {
		if _, seen := byOrigin[s.Origin]; !seen {
			origins = append(origins, s.Origin)
		}
		byOrigin[s.Origin] = append(byOrigin[s.Origin], i)
	}
	var out []int
	for _, o := range origins {
		idx := byOrigin[o]
		sort.SliceStable(idx, func(a, b int) bool {
			return slices[idx[a]].Score > slices[idx[b]].Score
		})
		out = append(out, idx...)
	}
	return out
}

// allJudged reports that every slice carries a relevance. Rerank maintains
// this as an invariant — it writes all of them or none — and this is the
// independent check the sort and the fill consult, so a future partial writer
// cannot quietly create a mixed-scale ordering.
// judgedPrefix reports a candidate set that may be ordered by relevance.
//
// The set is orderable when its judged candidates form a prefix: every
// candidate before the first unjudged one carries a probability, and
// everything after it is the protected lane, which has none and never will.
// Sorting the prefix by relevance is then comparing like with like, and the
// lane keeps the deterministic order it arrived in.
//
// The stricter "every candidate is judged" it replaces was the right rule
// when a protected candidate refused the whole rerank, because then the only
// two states were all or nothing. With a partition there is a third, and
// treating it as nothing threw away a completed rerank.
func judgedPrefix(slices []Slice) bool {
	judged := 0
	for _, s := range slices {
		if !s.Judged {
			break
		}
		judged++
	}
	if judged == 0 {
		return false
	}
	// Nothing judged may follow an unjudged candidate, or the two lanes are
	// interleaved and sorting the whole set would compare a probability with
	// a bm25 value.
	for _, s := range slices[judged:] {
		if s.Judged {
			return false
		}
	}
	return true
}

// redactMode asks the judge what it is permitted to see, so a call site builds
// the richest state it may rather than the richest it can imagine.
func redactMode(j judgment.Judge) judgment.RedactMode {
	if m, ok := j.(interface{ Redact() judgment.RedactMode }); ok {
		return m.Redact()
	}
	return judgment.RedactStrict
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
