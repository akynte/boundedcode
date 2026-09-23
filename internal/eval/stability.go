package eval

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sort"

	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/retrieval"
)

// Diagnostics for whether a Jev relevance score means what production treats
// it as meaning.
//
// Production compares one candidate's probability against a floor and against
// other candidates' probabilities. Both uses assume the number is a property
// of the candidate. It might instead be partly a property of the request —
// where the candidate sat in the list, what else was in the batch, how much
// noise surrounded it. Nothing in the integration can tell the difference,
// and a reranker built on a score that moves with request structure would
// produce orderings that look principled and are not.
//
// These are diagnostics, not production logic. Nothing here is consulted by a
// task, no threshold is enforced, and no pass/fail bar is imposed: the point
// of a first measurement is to find out what the distribution looks like, and
// a limit invented before seeing it would be a number pretending to be a
// finding.

// StabilityKind names one perturbation.
type StabilityKind string

const (
	// StabilityPermutation: the same candidates in a different order.
	StabilityPermutation StabilityKind = "permutation"
	// StabilityBatch: the same candidates split across different batches.
	StabilityBatch StabilityKind = "batch_composition"
	// StabilityDistractor: the same candidates with irrelevant ones added.
	StabilityDistractor StabilityKind = "distractor"
)

// StabilityTrial is one perturbation's effect.
type StabilityTrial struct {
	Kind StabilityKind `json:"kind"`
	// Candidates is how many were judged in both the reference and the
	// perturbed request.
	Candidates int `json:"candidates"`
	// MeanAbsDelta and MaxAbsDelta are the per-candidate probability change.
	// A relevance floor is a comparison against an absolute number, so an
	// absolute movement is what decides whether the floor means anything.
	MeanAbsDelta float64 `json:"mean_abs_delta"`
	MaxAbsDelta  float64 `json:"max_abs_delta"`
	// SpearmanRho is the rank correlation between the two orderings. Ranking
	// is what the packet fill actually consumes, so a score that moves a lot
	// while preserving order is a smaller problem than the reverse.
	SpearmanRho float64 `json:"spearman_rho"`
	// TopKOverlap is the share of the reference top-K still in the perturbed
	// top-K, at the K a packet realistically carries.
	TopKOverlap float64 `json:"top_k_overlap"`
	K           int     `json:"k"`
	// Note records a trial that could not be completed.
	Note string `json:"note,omitempty"`
}

// StabilityReport is a run of the diagnostics.
type StabilityReport struct {
	// Model and ExperimentID tie the distribution to what produced it. A
	// stability figure without a model version is not reusable.
	Model        string           `json:"model"`
	ModelServed  string           `json:"model_served,omitempty"`
	ExperimentID string           `json:"experiment_id,omitempty"`
	Seed         int64            `json:"seed"`
	Trials       []StabilityTrial `json:"trials"`
	// Requests and InputTokens are what the diagnostic cost.
	Requests    int `json:"requests"`
	InputTokens int `json:"input_tokens"`
}

// Summary aggregates trials of one kind, which is the shape a distribution
// is read in. Per-trial figures are kept too: an outlier is the interesting
// case and a mean would hide it.
type StabilitySummary struct {
	Kind          StabilityKind `json:"kind"`
	Trials        int           `json:"trials"`
	MeanAbsDelta  float64       `json:"mean_abs_delta"`
	P95AbsDelta   float64       `json:"p95_abs_delta"`
	WorstAbsDelta float64       `json:"worst_abs_delta"`
	MeanRho       float64       `json:"mean_spearman_rho"`
	WorstRho      float64       `json:"worst_spearman_rho"`
	MeanTopK      float64       `json:"mean_top_k_overlap"`
	WorstTopK     float64       `json:"worst_top_k_overlap"`
}

// Summarise groups the trials by kind.
func (r StabilityReport) Summarise() []StabilitySummary {
	byKind := map[StabilityKind][]StabilityTrial{}
	var order []StabilityKind
	for _, t := range r.Trials {
		if t.Note != "" {
			continue
		}
		if _, seen := byKind[t.Kind]; !seen {
			order = append(order, t.Kind)
		}
		byKind[t.Kind] = append(byKind[t.Kind], t)
	}
	var out []StabilitySummary
	for _, kind := range order {
		trials := byKind[kind]
		s := StabilitySummary{Kind: kind, Trials: len(trials), WorstRho: 1, WorstTopK: 1}
		var deltas []float64
		for _, t := range trials {
			deltas = append(deltas, t.MaxAbsDelta)
			s.MeanAbsDelta += t.MeanAbsDelta
			s.MeanRho += t.SpearmanRho
			s.MeanTopK += t.TopKOverlap
			s.WorstAbsDelta = math.Max(s.WorstAbsDelta, t.MaxAbsDelta)
			s.WorstRho = math.Min(s.WorstRho, t.SpearmanRho)
			s.WorstTopK = math.Min(s.WorstTopK, t.TopKOverlap)
		}
		n := float64(len(trials))
		s.MeanAbsDelta /= n
		s.MeanRho /= n
		s.MeanTopK /= n
		sort.Float64s(deltas)
		s.P95AbsDelta = percentile(deltas, 0.95)
		out = append(out, s)
	}
	return out
}

// StabilityInput is what a diagnostic run needs.
type StabilityInput struct {
	Judge judgment.Judge
	// Objective and Candidates are one realistic retrieval request.
	Objective  string
	Candidates []retrieval.Slice
	// Distractors are clearly irrelevant candidates, supplied by the caller
	// rather than generated here: what counts as irrelevant is a fact about
	// the repository, and a synthesised path would test whether Jev notices
	// a made-up file rather than whether noise moves a real ranking.
	Distractors []retrieval.Slice
	// Repeats is how many perturbations of each kind to run.
	Repeats int
	Seed    int64
	// K for the top-K overlap. Zero uses a default close to what a packet
	// carries.
	K int
}

// DefaultStabilityK is roughly what a packet holds after the budget fill.
const DefaultStabilityK = 8

// ErrNoLiveJudge is returned when the diagnostics are asked for without one.
var ErrNoLiveJudge = fmt.Errorf("eval: these diagnostics need a live judgment service; " +
	"they measure how the service behaves and cannot be faked")

// RunStability measures how a candidate's score moves with request structure.
//
// It refuses rather than degrades when no live judge is configured. A
// stability diagnostic against a stub would report perfect stability, which
// is both true and worthless.
func RunStability(ctx context.Context, in StabilityInput) (StabilityReport, error) {
	if in.Judge == nil || !in.Judge.Available() {
		return StabilityReport{}, ErrNoLiveJudge
	}
	if len(in.Candidates) < 2 {
		return StabilityReport{}, fmt.Errorf("eval: stability needs at least two candidates")
	}
	k := in.K
	if k <= 0 {
		k = DefaultStabilityK
	}
	repeats := in.Repeats
	if repeats <= 0 {
		repeats = 3
	}
	rep := StabilityReport{Model: in.Judge.Name(), Seed: in.Seed}
	rng := rand.New(rand.NewSource(in.Seed)) //nolint:gosec // reproducibility, not secrecy

	reference, refRes := scoreCandidates(ctx, in.Judge, in.Objective, in.Candidates)
	rep.Requests += refRes.Requests
	rep.InputTokens += refRes.InputTokens
	if len(reference) == 0 {
		return rep, fmt.Errorf("eval: the reference request produced no scores: %s",
			refRes.SkipReason)
	}

	add := func(kind StabilityKind, scores map[string]float64, res retrieval.RerankResult) {
		rep.Requests += res.Requests
		rep.InputTokens += res.InputTokens
		if len(scores) == 0 {
			rep.Trials = append(rep.Trials, StabilityTrial{
				Kind: kind, Note: "the perturbed request produced no scores: " + res.SkipReason,
			})
			return
		}
		rep.Trials = append(rep.Trials, compareScores(kind, reference, scores, k))
	}

	for i := 0; i < repeats; i++ {
		// Permutation: the same candidates, shuffled.
		shuffled := append([]retrieval.Slice(nil), in.Candidates...)
		rng.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		scores, res := scoreCandidates(ctx, in.Judge, in.Objective, shuffled)
		add(StabilityPermutation, scores, res)

		// Batch composition: the same candidates split in two, so each is
		// judged alongside a different subset of the others.
		half := len(in.Candidates) / 2
		if half >= 1 {
			left, leftRes := scoreCandidates(ctx, in.Judge, in.Objective, shuffled[:half])
			right, rightRes := scoreCandidates(ctx, in.Judge, in.Objective, shuffled[half:])
			merged := map[string]float64{}
			for p, v := range left {
				merged[p] = v
			}
			for p, v := range right {
				merged[p] = v
			}
			combined := retrieval.RerankResult{
				Requests:    leftRes.Requests + rightRes.Requests,
				InputTokens: leftRes.InputTokens + rightRes.InputTokens,
				SkipReason:  firstNonEmpty(leftRes.SkipReason, rightRes.SkipReason),
			}
			add(StabilityBatch, merged, combined)
		}

		// Distractors: the same candidates plus irrelevant ones.
		if len(in.Distractors) > 0 {
			noisy := append(append([]retrieval.Slice(nil), in.Candidates...), in.Distractors...)
			rng.Shuffle(len(noisy), func(a, b int) { noisy[a], noisy[b] = noisy[b], noisy[a] })
			scores, res := scoreCandidates(ctx, in.Judge, in.Objective, noisy)
			// Only the original candidates are compared: the distractors have
			// no reference score and including them would measure nothing.
			trimmed := map[string]float64{}
			for _, c := range in.Candidates {
				if v, ok := scores[c.Path+"#"+c.Symbol]; ok {
					trimmed[c.Path+"#"+c.Symbol] = v
				}
			}
			add(StabilityDistractor, trimmed, res)
		}
	}
	return rep, nil
}

// scoreCandidates runs one real rerank and returns the scores by candidate
// identity. It uses the production path deliberately: a diagnostic that built
// its own request would measure a request nobody makes.
func scoreCandidates(ctx context.Context, j judgment.Judge, objective string,
	candidates []retrieval.Slice) (map[string]float64, retrieval.RerankResult) {

	work := append([]retrieval.Slice(nil), candidates...)
	res := retrieval.Rerank(ctx, j, objective, work, retrieval.RerankTuning{}, nil)
	if !res.Applied {
		return nil, res
	}
	out := make(map[string]float64, len(work))
	for _, s := range work {
		out[s.Path+"#"+s.Symbol] = s.Relevance
	}
	return out, res
}

func compareScores(kind StabilityKind, reference, perturbed map[string]float64, k int) StabilityTrial {
	t := StabilityTrial{Kind: kind, K: k}
	var keys []string
	for key := range reference {
		if _, ok := perturbed[key]; ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	t.Candidates = len(keys)
	if len(keys) == 0 {
		t.Note = "no candidate appeared in both requests"
		return t
	}
	for _, key := range keys {
		d := math.Abs(perturbed[key] - reference[key])
		t.MeanAbsDelta += d
		t.MaxAbsDelta = math.Max(t.MaxAbsDelta, d)
	}
	t.MeanAbsDelta /= float64(len(keys))
	t.SpearmanRho = spearman(keys, reference, perturbed)
	t.TopKOverlap = topKOverlap(keys, reference, perturbed, k)
	return t
}

// spearman is the rank correlation between two scorings of the same keys.
func spearman(keys []string, a, b map[string]float64) float64 {
	n := len(keys)
	if n < 2 {
		return 1
	}
	ra, rb := ranksOf(keys, a), ranksOf(keys, b)
	var sum float64
	for _, key := range keys {
		d := ra[key] - rb[key]
		sum += d * d
	}
	// The tie-free form. Ties are possible here — two candidates can score
	// identically — and with them this is an approximation, which for a
	// diagnostic read as a distribution is the right trade against carrying
	// a full Pearson-on-ranks implementation.
	return 1 - (6*sum)/(float64(n)*(float64(n)*float64(n)-1))
}

func ranksOf(keys []string, scores map[string]float64) map[string]float64 {
	sorted := append([]string(nil), keys...)
	sort.Slice(sorted, func(i, j int) bool { return scores[sorted[i]] > scores[sorted[j]] })
	out := make(map[string]float64, len(sorted))
	for i, key := range sorted {
		out[key] = float64(i + 1)
	}
	return out
}

func topKOverlap(keys []string, a, b map[string]float64, k int) float64 {
	if k > len(keys) {
		k = len(keys)
	}
	if k == 0 {
		return 1
	}
	top := func(scores map[string]float64) map[string]bool {
		sorted := append([]string(nil), keys...)
		sort.Slice(sorted, func(i, j int) bool {
			if scores[sorted[i]] != scores[sorted[j]] {
				return scores[sorted[i]] > scores[sorted[j]]
			}
			return sorted[i] < sorted[j]
		})
		out := map[string]bool{}
		for _, key := range sorted[:k] {
			out[key] = true
		}
		return out
	}
	ta, tb := top(a), top(b)
	var shared int
	for key := range ta {
		if tb[key] {
			shared++
		}
	}
	return float64(shared) / float64(k)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
