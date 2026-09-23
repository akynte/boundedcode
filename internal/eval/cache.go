package eval

import (
	"fmt"
	"sort"
	"strings"
)

// Cache state, made explicit because it silently decides half the numbers.
//
// A judgment answered from cache costs no request, no tokens and almost no
// latency. Mixed with live calls in one arm, it makes the arm look cheaper
// and faster than it is by an amount nobody can recover afterwards — and the
// mixture is invisible: the answers are identical, so quality is unaffected
// and only the cost measurements are wrong.
//
// The split that matters is between what a cache can and cannot change. For
// semantic quality, deterministic cached answers are fine and arguably better
// — the same question gets the same answer twice, which is what reproducing a
// result means. For latency, request count and cost they are disqualifying.
// So the policy is declared, recorded, and folded into the experiment
// identity when it could move a measurement.

// CacheMode is how one cache was treated for a run.
type CacheMode string

const (
	// CacheCold: cleared before the run, so every call is live and the cost
	// figures are real.
	CacheCold CacheMode = "cold"
	// CacheWarm: whatever was there is reused. Fine for quality, wrong for
	// latency and cost.
	CacheWarm CacheMode = "warm"
	// CacheDisabled: not consulted and not written.
	CacheDisabled CacheMode = "disabled"
)

// Valid reports a mode this understands.
func (m CacheMode) Valid() bool {
	return m == CacheCold || m == CacheWarm || m == CacheDisabled
}

// CacheModes lists them, for CLI validation.
func CacheModes() []CacheMode { return []CacheMode{CacheCold, CacheWarm, CacheDisabled} }

// CachePolicy is the state of every cache that could move a number.
type CachePolicy struct {
	// Judgment is the external judgment answer cache.
	Judgment CacheMode `json:"judgment"`
	// Embedding is the local reranker's, where one exists.
	Embedding CacheMode `json:"embedding"`
	// Retrieval is the analyzer and index cache, which moves indexing time
	// and therefore whole-task duration.
	Retrieval CacheMode `json:"retrieval"`
}

// DefaultCachePolicy is what an ordinary run does: warm everywhere, because
// that is what production does and an unqualified run is not a benchmark.
func DefaultCachePolicy() CachePolicy {
	return CachePolicy{Judgment: CacheWarm, Embedding: CacheWarm, Retrieval: CacheWarm}
}

// ColdCachePolicy is the controlled condition for cost and latency.
func ColdCachePolicy() CachePolicy {
	return CachePolicy{Judgment: CacheCold, Embedding: CacheCold, Retrieval: CacheCold}
}

// Validate refuses a policy this cannot honour.
func (p CachePolicy) Validate() error {
	for name, mode := range map[string]CacheMode{
		"judgment": p.Judgment, "embedding": p.Embedding, "retrieval": p.Retrieval,
	} {
		if !mode.Valid() {
			return fmt.Errorf("eval: %s cache mode %q is not one of cold, warm, disabled",
				name, mode)
		}
	}
	return nil
}

// AffectsCost reports whether this policy makes the cost and latency figures
// uninterpretable.
//
// Warm is the only mode that does. Cold and disabled both mean every call was
// made; warm means an unknown share were not, and no amount of care
// afterwards recovers which.
func (p CachePolicy) AffectsCost() bool {
	return p.Judgment == CacheWarm || p.Embedding == CacheWarm
}

// CostCaveat is the sentence a report prints beside its cost figures, or
// empty when they can be read as they stand.
func (p CachePolicy) CostCaveat() string {
	var warm []string
	if p.Judgment == CacheWarm {
		warm = append(warm, "judgment")
	}
	if p.Embedding == CacheWarm {
		warm = append(warm, "embedding")
	}
	if len(warm) == 0 {
		return ""
	}
	return fmt.Sprintf("The %s cache was warm, so request counts, token totals and "+
		"latencies mix cache hits with live calls and understate the real cost by an "+
		"unknown amount. Semantic quality is unaffected: a cached answer is the answer "+
		"the service gave. Re-run with --cache cold for cost figures.",
		strings.Join(warm, " and "))
}

// Identity is the part of the policy that belongs in the experiment digest.
//
// Only what could change a measured *behaviour* goes in. A warm judgment
// cache changes the cost figures, so two runs differing in it are different
// experiments. The retrieval cache changes indexing time, which is inside the
// task duration, so it counts too. Nothing here is about the answers, because
// a cached answer is by construction the answer that was given.
func (p CachePolicy) Identity() string {
	return fmt.Sprintf("judgment=%s;embedding=%s;retrieval=%s",
		p.Judgment, p.Embedding, p.Retrieval)
}

// Describe renders the policy for a report.
func (p CachePolicy) Describe() string {
	parts := []string{
		"judgment " + string(p.Judgment),
		"embedding " + string(p.Embedding),
		"retrieval " + string(p.Retrieval),
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}
