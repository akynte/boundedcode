package judgeval

import (
	"math"
	"math/rand"
	"sort"
)

// PredictedOutcome is one (predicted probability, observed boolean) pair, the
// unit ledger.Pair also scores but constructed here from an offline dataset
// rather than from recorded production predictions. The two are kept as
// separate types on purpose: a ledger.Pair carries provenance (model,
// site version, intervened) this package's own manifest already records at
// the batch level, and importing internal/ledger from here — a package
// whose reports are meant to be inspectable by an external engineer with no
// live store open — would pull in the store driver for no benefit.
type PredictedOutcome struct {
	Predicted float64
	Outcome   bool
}

// BrierResult mirrors ledger.Population's probability-calibration fields,
// plus the confidence interval design instruction §9 requires and
// ledger.Population does not compute (that package scores live, paired
// production rows; the interval matters most exactly here, where a report
// may be read once and used to decide a promotion).
type BrierResult struct {
	N             int     `json:"n"`
	Positives     int     `json:"positives"`
	BaseRate      float64 `json:"base_rate"`
	Brier         float64 `json:"brier"`
	BrierBaseline float64 `json:"brier_baseline"`
	Skill         float64 `json:"skill"`
	// SkillCILow, SkillCIHigh are a bootstrap percentile confidence interval
	// on Skill (see BootstrapSkillCI). A promotion decision must read
	// SkillCILow, not Skill: design instruction §9's conservative criterion
	// is "lower confidence bound of skill > 0", never the point estimate.
	SkillCILow  float64 `json:"skill_ci_low,omitempty"`
	SkillCIHigh float64 `json:"skill_ci_high,omitempty"`
	// Interpretable mirrors ledger.Population.Interpretable: false below the
	// reporting floor or for a degenerate (single-outcome-value) population.
	Interpretable bool `json:"interpretable"`
	Degenerate    bool `json:"degenerate,omitempty"`
}

// ScoreBrier computes Brier score, skill against the base rate, and (when
// interpretable and iterations > 0) a bootstrap CI on skill. It intentionally
// duplicates the small amount of arithmetic internal/ledger's
// scorePopulation also does — see PredictedOutcome's doc comment for why —
// rather than sharing a helper across a package boundary that would then
// need to justify importing the store.
func ScoreBrier(pairs []PredictedOutcome, minSample, bootstrapIterations int, seed int64) BrierResult {
	r := BrierResult{N: len(pairs)}
	if len(pairs) == 0 {
		return r
	}
	for _, p := range pairs {
		if p.Outcome {
			r.Positives++
		}
	}
	r.BaseRate = float64(r.Positives) / float64(len(pairs))
	r.Degenerate = r.Positives == 0 || r.Positives == len(pairs)
	r.Interpretable = len(pairs) >= minSample && !r.Degenerate

	r.Brier, r.BrierBaseline = brierAndBaseline(pairs, r.BaseRate)
	if r.BrierBaseline > 0 {
		r.Skill = 1 - r.Brier/r.BrierBaseline
	}
	if r.Interpretable && bootstrapIterations > 0 {
		r.SkillCILow, r.SkillCIHigh = BootstrapSkillCI(pairs, bootstrapIterations, seed)
	}
	return r
}

func brierAndBaseline(pairs []PredictedOutcome, baseRate float64) (brier, baseline float64) {
	var sq, baseSq float64
	for _, p := range pairs {
		obs := 0.0
		if p.Outcome {
			obs = 1.0
		}
		d := p.Predicted - obs
		sq += d * d
		bd := baseRate - obs
		baseSq += bd * bd
	}
	n := float64(len(pairs))
	return sq / n, baseSq / n
}

// BootstrapSkillCI computes a 95% percentile bootstrap confidence interval
// for Brier skill by resampling pairs with replacement `iterations` times.
// seed makes the resampling — and therefore the reported interval —
// reproducible from the same inputs: a published result that could not be
// regenerated bit-for-bit from its own manifest would fail design
// instruction §21.
//
// This is the simple, robust technique design instruction §9 asks for
// ("prefer a simple robust technique … such as bootstrap confidence
// intervals") rather than a parametric interval that would need to justify
// a normality assumption Brier skill does not obviously satisfy at small N.
func BootstrapSkillCI(pairs []PredictedOutcome, iterations int, seed int64) (low, high float64) {
	if len(pairs) == 0 || iterations <= 0 {
		return 0, 0
	}
	// Deliberately math/rand: a bootstrap confidence interval has to be
	// reproducible from its seed, which is what makes a reported interval
	// checkable by someone else. crypto/rand would make every run of the same
	// campaign disagree with the last.
	rng := rand.New(rand.NewSource(seed)) //nolint:gosec // G404: seeded for reproducibility, not secrecy
	skills := make([]float64, 0, iterations)
	n := len(pairs)
	sample := make([]PredictedOutcome, n)
	for it := 0; it < iterations; it++ {
		var positives int
		for i := 0; i < n; i++ {
			sample[i] = pairs[rng.Intn(n)]
			if sample[i].Outcome {
				positives++
			}
		}
		if positives == 0 || positives == n {
			// A resample that happens to be degenerate contributes no
			// meaningful skill figure; skip it rather than dividing by a
			// zero baseline, the same rule ScoreBrier applies to the whole
			// population.
			continue
		}
		baseRate := float64(positives) / float64(n)
		brier, baseline := brierAndBaseline(sample, baseRate)
		if baseline > 0 {
			skills = append(skills, 1-brier/baseline)
		}
	}
	if len(skills) == 0 {
		return 0, 0
	}
	sort.Float64s(skills)
	low = percentile(skills, 0.025)
	high = percentile(skills, 0.975)
	return low, high
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 1 {
		return sorted[0]
	}
	idx := p * float64(len(sorted)-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi {
		return sorted[lo]
	}
	frac := idx - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

// ClassificationResult is precision/recall/F1 for one label against every
// other label pooled, plus support — the standard one-vs-rest view design
// instruction §24 asks for, computed per class by the caller iterating its
// label set.
type ClassificationResult struct {
	Label     string  `json:"label"`
	TP        int     `json:"tp"`
	FP        int     `json:"fp"`
	FN        int     `json:"fn"`
	Support   int     `json:"support"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`
}

// ClassificationReport is a full multi-class result: per-class scores, a
// confusion matrix, and — separately, per §13/§24 — the count of cases
// annotated ambiguous, which are excluded from every metric above rather
// than folded into any class.
type ClassificationReport struct {
	Classes []ClassificationResult `json:"classes"`
	// Confusion[predicted][actual] = count. Both axes list every label seen
	// on either side, sorted, so a reader can print it as a square table.
	Confusion map[string]map[string]int `json:"confusion"`
	// Ambiguous is how many predicted/actual pairs were excluded because
	// the ground truth (or, in principle, the prediction) was
	// LabelAmbiguous.
	Ambiguous int `json:"ambiguous_excluded"`
	Total     int `json:"total"`
}

// Classify scores predicted against actual, one string label per case.
// Pairs where actual is LabelAmbiguous are excluded from scoring and
// counted separately, never treated as a class of their own and never
// silently counted as a hit or a miss — design instruction §13's rule
// applied to every classification metric in the campaign.
func Classify(predicted, actual []string) ClassificationReport {
	rep := ClassificationReport{Confusion: map[string]map[string]int{}}
	labels := map[string]bool{}
	type row struct{ p, a string }
	var rows []row
	for i := range predicted {
		rep.Total++
		if actual[i] == LabelAmbiguous {
			rep.Ambiguous++
			continue
		}
		rows = append(rows, row{predicted[i], actual[i]})
		labels[predicted[i]] = true
		labels[actual[i]] = true
	}
	sortedLabels := make([]string, 0, len(labels))
	for l := range labels {
		sortedLabels = append(sortedLabels, l)
	}
	sort.Strings(sortedLabels)

	for _, r := range rows {
		if rep.Confusion[r.p] == nil {
			rep.Confusion[r.p] = map[string]int{}
		}
		rep.Confusion[r.p][r.a]++
	}

	for _, label := range sortedLabels {
		var res ClassificationResult
		res.Label = label
		for _, r := range rows {
			switch {
			case r.p == label && r.a == label:
				res.TP++
			case r.p == label && r.a != label:
				res.FP++
			case r.p != label && r.a == label:
				res.FN++
			}
			if r.a == label {
				res.Support++
			}
		}
		if res.TP+res.FP > 0 {
			res.Precision = float64(res.TP) / float64(res.TP+res.FP)
		}
		if res.TP+res.FN > 0 {
			res.Recall = float64(res.TP) / float64(res.TP+res.FN)
		}
		if res.Precision+res.Recall > 0 {
			res.F1 = 2 * res.Precision * res.Recall / (res.Precision + res.Recall)
		}
		rep.Classes = append(rep.Classes, res)
	}
	return rep
}

// StabilityResult is repeat-evaluation stability for one case, or aggregated
// across many — design instruction §17. FlipRate needs a decision threshold
// to define a "label"; callers pass it in via FlipThreshold below rather
// than this package guessing one, since the threshold that matters is
// whatever the call site's own EffectThreshold is (judgment.SiteInfo).
type StabilityResult struct {
	Repeats  int     `json:"repeats"`
	Mean     float64 `json:"mean_probability"`
	StdDev   float64 `json:"stddev"`
	MaxDelta float64 `json:"max_delta"`
	// FlipRate is the fraction of repeats whose side of FlipThreshold
	// differs from the majority side. 0 for a single repeat.
	FlipRate float64 `json:"flip_rate"`
}

// Stability computes mean, standard deviation, max pairwise delta and flip
// rate over repeated probability answers to what should be the same
// question against the same state. threshold is the decision boundary a
// flip is measured against (typically the site's EffectThreshold).
func Stability(probabilities []float64, threshold float64) StabilityResult {
	r := StabilityResult{Repeats: len(probabilities)}
	if len(probabilities) == 0 {
		return r
	}
	var sum float64
	for _, p := range probabilities {
		sum += p
	}
	r.Mean = sum / float64(len(probabilities))

	var sq float64
	for _, p := range probabilities {
		d := p - r.Mean
		sq += d * d
	}
	if len(probabilities) > 1 {
		r.StdDev = math.Sqrt(sq / float64(len(probabilities)-1))
	}

	min, max := probabilities[0], probabilities[0]
	for _, p := range probabilities {
		if p < min {
			min = p
		}
		if p > max {
			max = p
		}
	}
	r.MaxDelta = max - min

	if len(probabilities) > 1 {
		var above int
		for _, p := range probabilities {
			if p >= threshold {
				above++
			}
		}
		majority := above >= len(probabilities)-above
		var flips int
		for _, p := range probabilities {
			side := p >= threshold
			if side != majority {
				flips++
			}
		}
		r.FlipRate = float64(flips) / float64(len(probabilities))
	}
	return r
}

// RetrievalResult is Recall@K / MRR / hit-rate for a ranked-candidate site
// (M9's failure-keyed rerank) — design instruction §24's ranking metrics.
type RetrievalResult struct {
	N         int     `json:"n"`
	RecallAtK float64 `json:"recall_at_k"`
	K         int     `json:"k"`
	MRR       float64 `json:"mrr"`
	MissRate  float64 `json:"miss_rate"`
}

// ScoreRetrieval scores a set of queries, each described by the rank
// (1-based) of the first item that satisfies the query's target, or 0 if
// none of the returned candidates did.
func ScoreRetrieval(ranksOfFirstHit []int, k int) RetrievalResult {
	r := RetrievalResult{N: len(ranksOfFirstHit), K: k}
	if len(ranksOfFirstHit) == 0 {
		return r
	}
	var hitsAtK, misses int
	var rrSum float64
	for _, rank := range ranksOfFirstHit {
		if rank <= 0 {
			misses++
			continue
		}
		if rank <= k {
			hitsAtK++
		}
		rrSum += 1.0 / float64(rank)
	}
	n := float64(len(ranksOfFirstHit))
	r.RecallAtK = float64(hitsAtK) / n
	r.MRR = rrSum / n
	r.MissRate = float64(misses) / n
	return r
}
