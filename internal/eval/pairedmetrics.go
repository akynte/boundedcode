package eval

import (
	"math/rand"
	"sort"
)

// Paired comparison across every metric an arm can move, not just the binary
// one.
//
// Coding runs are stochastic, so an unpaired difference between two arms
// mixes the effect with which tasks each happened to draw. Pairing removes
// that, and clustering the bootstrap by task removes the other half of it:
// five passes over one task are five looks at one problem, not five problems,
// and resampling runs directly would produce an interval far narrower than the
// evidence supports.
//
// The reason this exists beside HierarchicalCompare, which already does the
// binary case, is that the metrics a reranker actually moves are continuous.
// Localization recall, packet tokens and duration are where an effect should
// show first; the solve rate is the last thing to move and the noisiest place
// to look for it.

// MetricDelta is one metric's paired difference between two arms.
type MetricDelta struct {
	// Metric names what was differenced, and Unit says how to read it.
	Metric string `json:"metric"`
	Unit   string `json:"unit"`
	// Baseline and Variant are the arms; every figure is variant minus
	// baseline.
	Baseline string `json:"baseline"`
	Variant  string `json:"variant"`

	// Tasks is the clustering unit and the real sample size; Runs is how many
	// observations went in. Reporting only Runs invites a reader to believe
	// the sample is larger than it is.
	Tasks int `json:"tasks"`
	Runs  int `json:"runs"`

	// Wins, Losses and Ties count tasks, not runs: a task where the variant's
	// mean beat the baseline's is one win however many passes it took.
	Wins   int `json:"wins"`
	Losses int `json:"losses"`
	Ties   int `json:"ties"`

	// Delta is the mean within-task difference.
	Delta float64 `json:"delta"`
	// Low and High bound it at 95% by a task-clustered bootstrap.
	Low  float64 `json:"ci_low"`
	High float64 `json:"ci_high"`
	// BaselineMean and VariantMean are the levels the delta sits between, so
	// a reader can tell a two-point move on a base of five from one on a base
	// of ninety.
	BaselineMean float64 `json:"baseline_mean"`
	VariantMean  float64 `json:"variant_mean"`

	Resamples int   `json:"resamples"`
	Seed      int64 `json:"seed"`
	// Verdict is what this difference does and does not support. It is never
	// derived from the point estimate alone: an interval spanning zero is
	// "not shown", whatever the midpoint says.
	Verdict Verdict `json:"verdict"`
}

// Decided reports an interval that excludes no-difference.
func (m MetricDelta) Decided() bool {
	return m.Tasks > 1 && (m.Low > 0 || m.High < 0)
}

// MetricSelector extracts one run's value, and says whether the run has one.
//
// The boolean matters: a run on an unannotated task has no localization
// recall, and treating a missing value as zero would report retrieval as
// having found nothing rather than as not having been measured.
type MetricSelector func(Outcome) (float64, bool)

// MetricSpec names a metric and how to read it.
type MetricSpec struct {
	Name   string
	Unit   string
	Select MetricSelector
	// HigherIsBetter decides which direction counts as a win. Packet tokens
	// and duration are costs: less is better, and a comparison that got that
	// backwards would report an efficiency gain as a regression.
	HigherIsBetter bool
}

// BenchmarkMetrics are the metrics every arm comparison reports.
func BenchmarkMetrics() []MetricSpec {
	return []MetricSpec{
		{Name: "solve rate", Unit: "proportion", HigherIsBetter: true,
			Select: func(o Outcome) (float64, bool) { return boolValue(o.Solved), true }},
		{Name: "localization recall", Unit: "proportion", HigherIsBetter: true,
			Select: func(o Outcome) (float64, bool) {
				if !o.Localization.Scored() {
					return 0, false
				}
				return o.Localization.Recall, true
			}},
		{Name: "localization precision", Unit: "proportion", HigherIsBetter: true,
			Select: func(o Outcome) (float64, bool) {
				if !o.Localization.Scored() || o.Localization.PrecisionDenominator == 0 {
					return 0, false
				}
				return o.Localization.Precision, true
			}},
		{Name: "generator recall", Unit: "proportion", HigherIsBetter: true,
			Select: func(o Outcome) (float64, bool) {
				if !o.Localization.Scored() {
					return 0, false
				}
				return o.Localization.GeneratorRecall, true
			}},
		{Name: "packet tokens", Unit: "tokens", HigherIsBetter: false,
			Select: func(o Outcome) (float64, bool) {
				if o.PacketTokens == 0 {
					return 0, false
				}
				return float64(o.PacketTokens), true
			}},
		{Name: "duration", Unit: "seconds", HigherIsBetter: false,
			Select: func(o Outcome) (float64, bool) {
				if o.Duration <= 0 {
					return 0, false
				}
				return o.Duration.Seconds(), true
			}},
	}
}

func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// CompareMetric runs the paired, task-clustered comparison for one metric.
func CompareMetric(outcomes []Outcome, baseline, variant string, spec MetricSpec,
	seed int64, resamples int) MetricDelta {

	if resamples <= 0 {
		resamples = defaultResamples
	}
	d := MetricDelta{
		Metric: spec.Name, Unit: spec.Unit, Baseline: baseline, Variant: variant,
		Resamples: resamples, Seed: seed, Verdict: VerdictInsufficient,
	}

	base, vary := map[string][]float64{}, map[string][]float64{}
	for _, o := range outcomes {
		if !o.EvidenceRun() {
			continue
		}
		v, ok := spec.Select(o)
		if !ok {
			continue
		}
		switch o.Arm {
		case baseline:
			base[o.TaskID] = append(base[o.TaskID], v)
		case variant:
			vary[o.TaskID] = append(vary[o.TaskID], v)
		}
	}

	var tasks []string
	for id := range base {
		if len(vary[id]) > 0 {
			tasks = append(tasks, id)
		}
	}
	sort.Strings(tasks)
	if len(tasks) == 0 {
		return d
	}
	d.Tasks = len(tasks)

	var baseSum, varySum float64
	for _, id := range tasks {
		d.Runs += len(base[id]) + len(vary[id])
		b, v := meanOf(base[id]), meanOf(vary[id])
		baseSum, varySum = baseSum+b, varySum+v
		switch {
		case v == b:
			d.Ties++
		case (v > b) == spec.HigherIsBetter:
			d.Wins++
		default:
			d.Losses++
		}
	}
	d.BaselineMean = baseSum / float64(len(tasks))
	d.VariantMean = varySum / float64(len(tasks))
	d.Delta = d.VariantMean - d.BaselineMean

	// Resample tasks, then runs within each drawn task. Documented here
	// because a bootstrap whose procedure is not written down is a number
	// nobody can check: 10,000 resamples, a fixed seed so rendering the same
	// result twice gives the same interval, and the 2.5th/97.5th percentiles
	// of the resampled within-task mean difference.
	rng := rand.New(rand.NewSource(seed)) //nolint:gosec // reproducibility, not secrecy
	deltas := make([]float64, 0, resamples)
	for i := 0; i < resamples; i++ {
		var sum float64
		for range tasks {
			id := tasks[rng.Intn(len(tasks))]
			sum += meanOf(resampleFloats(vary[id], rng)) - meanOf(resampleFloats(base[id], rng))
		}
		deltas = append(deltas, sum/float64(len(tasks)))
	}
	sort.Float64s(deltas)
	d.Low, d.High = percentile(deltas, 0.025), percentile(deltas, 0.975)
	d.Verdict = verdictFromInterval(d.Low, d.High)

	// A bootstrap over one cluster is degenerate: every resample draws the
	// same task, so the interval collapses onto the observed difference and
	// excludes zero however small that difference is. Left alone it reports
	// a one-point gap on a single task as positive evidence, which is the
	// exact failure — a tiny numerical difference called an improvement —
	// that reporting intervals was supposed to prevent.
	//
	// The guard is on the number of *tasks*, not runs, because tasks are the
	// unit that was resampled. Repeats add precision within a task; they do
	// not add a second look at the population.
	if d.Tasks < minClustersForVerdict {
		d.Verdict = VerdictInsufficient
	}
	return d
}

// minClustersForVerdict is the smallest number of tasks a paired interval may
// rest a verdict on. Two is the least that can produce a non-degenerate
// resample; it is a floor against nonsense, not a claim that two is enough.
const minClustersForVerdict = 2

func meanOf(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	var sum float64
	for _, x := range v {
		sum += x
	}
	return sum / float64(len(v))
}

func resampleFloats(in []float64, rng *rand.Rand) []float64 {
	if len(in) == 0 {
		return nil
	}
	out := make([]float64, len(in))
	for i := range out {
		out[i] = in[rng.Intn(len(in))]
	}
	return out
}

// CompareAllMetrics runs every benchmark metric for one arm pair.
func CompareAllMetrics(outcomes []Outcome, baseline, variant string, seed int64) []MetricDelta {
	var out []MetricDelta
	for _, spec := range BenchmarkMetrics() {
		out = append(out, CompareMetric(outcomes, baseline, variant, spec, seed, 0))
	}
	return out
}
