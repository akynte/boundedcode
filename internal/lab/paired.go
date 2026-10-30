package lab

import (
	"fmt"
	"math"

	"github.com/akynte/boundedcode/internal/benchmark"
)

// Arms of an experiment run the same tasks, so they are compared task by
// task: each task is better, worse or tied in the candidate, and an exact
// one-sided sign test asks whether "better" outnumbers "worse" beyond
// chance. Under no effect, noise makes a task differ in either direction
// alike, so the test is valid however noisy a task is, and it is far more
// sensitive than asking two suite-wide intervals not to overlap. It is the
// laboratory's pre-registered test of the primary metric; regressions are
// still also caught by the gate's conservative rules.

// Alpha is the one-sided significance level of the paired test.
const Alpha = 0.05

// PairedResult is a paired comparison over tasks.
type PairedResult struct {
	Metric string  `json:"metric"`
	Tasks  int     `json:"tasks"` // tasks run by both sides
	Better int     `json:"better"`
	Worse  int     `json:"worse"`
	Ties   int     `json:"ties"`
	P      float64 `json:"p"` // one-sided sign test for the observed direction
	// Effect is the mean per-task difference in pass rate (success_rate),
	// or the geometric mean per-task ratio (figures).
	Effect  float64 `json:"effect"`
	Outcome string  `json:"outcome"` // improved, regressed, unchanged, inconclusive
	Why     string  `json:"why"`
}

// taskValue is a task's value of a metric over its runs on one side.
func taskValues(runs []benchmark.RunValue, metric string, keep func(benchmark.RunKey) bool) map[string]float64 {
	type acc struct {
		n, pass       int
		sum           float64
		k             int
		passTok, nTok float64
	}
	by := map[string]*acc{}
	for _, r := range runs {
		if keep != nil && !keep(r.RunKey) {
			continue
		}
		a := by[r.Task]
		if a == nil {
			a = &acc{}
			by[r.Task] = a
		}
		a.n++
		if r.Passed {
			a.pass++
			a.passTok += r.Tokens
			a.nTok++
		}
		switch metric {
		case "median_seconds", "p90_seconds", "p95_seconds":
			a.sum += r.Seconds
			a.k++
		case "model_calls_per_run":
			a.sum += r.ModelCalls
			a.k++
		case "tokens_per_run":
			a.sum += r.Tokens
			a.k++
		}
	}
	out := map[string]float64{}
	for t, a := range by {
		switch metric {
		case "success_rate":
			out[t] = float64(a.pass) / float64(a.n)
		case "tokens_per_success":
			if a.nTok > 0 {
				out[t] = a.passTok / a.nTok
			}
		default:
			if a.k > 0 {
				out[t] = a.sum / float64(a.k)
			}
		}
	}
	return out
}

// pairable reports whether the paired test covers a metric.
func pairable(metric string) bool {
	switch metric {
	case "success_rate", "tokens_per_success", "median_seconds", "p90_seconds", "p95_seconds", "model_calls_per_run", "tokens_per_run":
		return true
	}
	return false
}

// Paired compares cand with base on m task by task. tol is the smallest
// effect worth calling: an absolute pass-rate difference for success, a
// relative change for figures.
func Paired(m Metric, tol float64, base, cand []benchmark.RunValue, keep func(benchmark.RunKey) bool) PairedResult {
	res := PairedResult{Metric: m.Name, Outcome: "unchanged"}
	if !pairable(m.Name) {
		res.Outcome, res.Why = "inconclusive", "no paired test for "+m.Name
		return res
	}
	bv, cv := taskValues(base, m.Name, keep), taskValues(cand, m.Name, keep)
	var diffs []float64
	logSum, nLog := 0.0, 0
	for _, t := range sortedKeys(bv) {
		c, ok := cv[t]
		if !ok {
			continue
		}
		b := bv[t]
		res.Tasks++
		gain := c - b // positive: the candidate is better when higher is
		if m.Name == "success_rate" {
			diffs = append(diffs, c-b)
		} else {
			if b <= 0 || c <= 0 {
				res.Ties++
				continue
			}
			logSum += math.Log(c / b)
			nLog++
			if math.Abs(c/b-1) < 0.02 {
				gain = 0 // within 2%: the same
			}
		}
		if m.Better == "lower" {
			gain = -gain
		}
		switch {
		case gain > 0:
			res.Better++
		case gain < 0:
			res.Worse++
		default:
			res.Ties++
		}
	}
	if res.Tasks == 0 {
		res.Outcome, res.Why = "inconclusive", "no task run by both sides"
		return res
	}
	if m.Name == "success_rate" {
		s := 0.0
		for _, d := range diffs {
			s += d
		}
		res.Effect = round3(s / float64(len(diffs)))
	} else if nLog > 0 {
		res.Effect = round3(math.Exp(logSum / float64(nLog)))
	}
	n := res.Better + res.Worse
	pBetter, pWorse := signTail(res.Better, n), signTail(res.Worse, n)
	big := false
	if m.Name == "success_rate" {
		big = math.Abs(res.Effect) >= tol
	} else {
		big = math.Abs(res.Effect-1) >= tol
	}
	switch {
	case res.Better > res.Worse && pBetter <= Alpha && big:
		res.Outcome, res.P = "improved", round3(pBetter)
	case res.Worse > res.Better && pWorse <= Alpha && big:
		res.Outcome, res.P = "regressed", round3(pWorse)
	default:
		res.P = round3(math.Min(pBetter, pWorse))
	}
	effect := fmt.Sprintf("mean pass-rate change %+.3f", res.Effect)
	if m.Name != "success_rate" {
		effect = fmt.Sprintf("per-task ratio ×%.2f", res.Effect)
	}
	res.Why = fmt.Sprintf("%d tasks better, %d worse, %d tied of %d; %s; sign test p=%.3f (needs ≤%.2f and an effect of at least %g)", res.Better, res.Worse, res.Ties, res.Tasks, effect, res.P, Alpha, tol)
	// Too few tasks for the test ever to decide: not "unchanged", which
	// would claim a difference was looked for and not found.
	if min := MinTasksToShow(); res.Tasks < min {
		res.Outcome, res.Why = "inconclusive", fmt.Sprintf("%s; %d tasks cannot show a difference (at least %d are needed)", res.Why, res.Tasks, min)
	}
	return res
}

// signTail is P(X ≥ k) for X ~ Binomial(n, 1/2).
func signTail(k, n int) float64 {
	if n == 0 || k <= 0 {
		return 1
	}
	p := 0.0
	for i := k; i <= n; i++ {
		p += math.Exp(lchoose(n, i) - float64(n)*math.Ln2)
	}
	return math.Min(1, p)
}

func lchoose(n, k int) float64 {
	a, _ := math.Lgamma(float64(n + 1))
	b, _ := math.Lgamma(float64(k + 1))
	c, _ := math.Lgamma(float64(n - k + 1))
	return a - b - c
}

// MinTasksToShow is the fewest tasks that must all differ in the
// candidate's favour (none against it) for the sign test to call it.
func MinTasksToShow() int {
	for n := 1; ; n++ {
		if signTail(n, n) <= Alpha {
			return n
		}
	}
}

// pairedTolerance is the smallest effect a rule calls: its tolerance, or
// a default (3 points for a rate, 10% for a figure).
func pairedTolerance(r benchmark.GateRule) float64 {
	if r.Tolerance > 0 {
		return r.Tolerance
	}
	if r.Test == "wilson" {
		return 0.03
	}
	return 0.10
}
