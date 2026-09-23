package eval

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/retrieval"
	"github.com/akynte/boundedcode/internal/task"
)

// Aggregation is where a set of runs becomes a number someone might act on,
// so the arithmetic is deliberately conservative.
//
// A rate reported without an interval invites being read as precise. With
// twenty tasks, a 60% success rate has a 95% interval of roughly 39%–78% —
// wide enough that a 10-point difference between two arms means nothing. The
// report prints the interval next to every rate, and Significant() refuses to
// call a difference real when the intervals overlap.

// Rate is a proportion with a confidence interval and the sample it came from.
type Rate struct {
	Successes int     `json:"successes"`
	Total     int     `json:"total"`
	Value     float64 `json:"value"`
	Low       float64 `json:"ci_low"`
	High      float64 `json:"ci_high"`
}

// NewRate computes a rate and its 95% Wilson score interval.
//
// Wilson rather than the normal approximation: at small samples and rates near
// 0 or 1 the normal interval produces bounds outside [0,1] and is badly
// miscalibrated, which is exactly the regime a task set of twenty cases sits in.
func NewRate(successes, total int) Rate {
	r := Rate{Successes: successes, Total: total}
	if total <= 0 {
		return r
	}
	p := float64(successes) / float64(total)
	r.Value = p

	const z = 1.96 // 95%
	n := float64(total)
	denom := 1 + z*z/n
	centre := (p + z*z/(2*n)) / denom
	margin := z * math.Sqrt(p*(1-p)/n+z*z/(4*n*n)) / denom

	r.Low = math.Max(0, centre-margin)
	r.High = math.Min(1, centre+margin)
	return r
}

// String renders a rate with its interval, because a bare percentage is the
// thing that gets quoted out of context.
func (r Rate) String() string {
	if r.Total == 0 {
		return "n/a (0 runs)"
	}
	return fmt.Sprintf("%.0f%% [%.0f–%.0f%%] (%d/%d)",
		r.Value*100, r.Low*100, r.High*100, r.Successes, r.Total)
}

// Overlaps reports whether two intervals overlap, which is the weakest useful
// test for "these might be the same".
func (r Rate) Overlaps(other Rate) bool {
	return r.Low <= other.High && other.Low <= r.High
}

// ArmResult aggregates one arm's runs.
type ArmResult struct {
	Arm string `json:"arm"`
	// Solved is the headline: the hidden acceptance command passed.
	Solved Rate `json:"solved"`
	// FalseAccept is how often the system claimed success and was wrong. A
	// high solved rate with a high false-accept rate is not a good result.
	FalseAccept Rate `json:"false_accept"`
	// MissedSuccess is how often the work was right but the contract rejected
	// it.
	MissedSuccess Rate `json:"missed_success"`
	// Tampered is how often a solution changed something it was told not to.
	Tampered Rate `json:"tampered"`
	// Errored counts runs that could not complete. They are excluded from
	// every rate above: a harness fault is not evidence about the system.
	Errored int `json:"errored"`

	// MeanScore is the mean fraction of hidden tests satisfied, over graded
	// runs. GradedRuns is how many those were: a mean over three runs and a
	// mean over thirty are different numbers to trust.
	// MeanScore is nil, and serializes as null, when GradedRuns is zero:
	// "not measured" is not the same fact as zero.
	MeanScore  *float64 `json:"mean_score"`
	GradedRuns int      `json:"graded_runs"`

	// Localization metrics, over the runs whose task records ground truth.
	//
	// LocalizedRuns is how many those were and Unscorable how many could not
	// be measured at all. Both are reported: a mean over two runs and a mean
	// over thirty are different claims, and a set where most tasks are
	// unscorable is not a localization measurement however good the mean is.
	//
	// Generator and reranker recall are separate because they fail
	// differently. A file no candidate set ever proposed cannot be ranked
	// into a packet, so a low reranker recall with an equally low generator
	// recall is retrieval's failure, not the ranking's. LostInRanking counts
	// the cases that are the ranking's: proposed, then dropped.
	MeanGeneratorRecall float64 `json:"mean_generator_recall,omitempty"`
	MeanRecall          float64 `json:"mean_recall,omitempty"`
	MeanPrecision       float64 `json:"mean_precision,omitempty"`
	LostInRanking       int     `json:"lost_in_ranking,omitempty"`
	LocalizedRuns       int     `json:"localized_runs,omitempty"`
	Unscorable          int     `json:"unscorable_for_localization,omitempty"`

	// Rerank telemetry. AppliedRate is the share of rerank attempts that
	// actually determined an ordering; anything below 1.0 means part of this
	// arm's runs received the baseline's treatment under this arm's name, and
	// SkipReasons says why.
	RerankAttempts    int            `json:"rerank_attempts,omitempty"`
	RerankApplied     int            `json:"rerank_applied,omitempty"`
	RerankAppliedRate float64        `json:"rerank_applied_rate,omitempty"`
	RerankSkips       map[string]int `json:"rerank_skips,omitempty"`

	// External judgment cost and latency, summed over the arm. Latency is
	// reported at the median and the 95th percentile: a mean hides the tail
	// that decides whether a 1.5s budget is being hit.
	JudgeRequests    int   `json:"judge_requests,omitempty"`
	JudgeInputTokens int   `json:"judge_input_tokens,omitempty"`
	JudgeLatencyP50  int64 `json:"judge_latency_p50_ms,omitempty"`
	JudgeLatencyP95  int64 `json:"judge_latency_p95_ms,omitempty"`

	// MeanPacketTokens is how much context each run was given.
	MeanPacketTokens int `json:"mean_packet_tokens,omitempty"`

	MedianDuration time.Duration `json:"median_duration"`
	MedianAttempts int           `json:"median_attempts"`
	TotalTokens    int           `json:"total_tokens,omitempty"`

	ByCategory map[Category]Rate `json:"by_category"`
}

// FrozenConfig is the exact experimental configuration a report was produced
// under.
//
// It is in the artifact because a benchmark that cannot state its parameters
// cannot be reproduced, and because the dev/held-out discipline is only real
// if the held-out run can be shown to have used the configuration the dev run
// settled on. A tuned value that appears in nobody's artifact is a tuned value
// nobody can check was frozen.
type FrozenConfig struct {
	// Set is the evaluation set these runs came from, empty for all of them.
	Set string `json:"set,omitempty"`
	// Repeat is how many passes each cell got.
	Repeat int `json:"repeat,omitempty"`
	// JudgmentModel is the exact external model id, empty when judgments were
	// off. An alias here makes the run unreproducible and `bcode doctor` warns
	// about it; recording it is what makes the warning checkable afterwards.
	JudgmentModel string `json:"judgment_model,omitempty"`
	// Tuning and LocalizeTuning are the retrieval and localization knobs in
	// force. Zero fields mean the shipped default was used.
	Tuning         retrieval.RerankTuning `json:"tuning,omitzero"`
	LocalizeTuning task.LocalizeTuning    `json:"localize_tuning,omitzero"`
}

// Report is a complete evaluation result.
type Report struct {
	// Provenance is everything needed to reproduce what was measured,
	// including the experiment id these numbers belong to.
	Provenance Provenance `json:"provenance,omitzero"`
	// Frozen is the configuration these numbers were produced under. It
	// overlaps Provenance and is kept because existing readers of the
	// artifact parse it.
	Frozen FrozenConfig `json:"frozen,omitzero"`
	// Arms holds each arm's aggregate, in the order they were run.
	Arms []ArmResult `json:"arms"`
	// Comparisons answers the questions the arm set was designed around.
	Comparisons []ComparisonResult `json:"comparisons"`
	// TaskCount and LeakMix describe the set the numbers came from. A result
	// without them is not interpretable.
	TaskCount int              `json:"task_count"`
	LeakMix   map[LeakRisk]int `json:"leak_mix"`
	// Caveats are the things a reader must know to read the numbers
	// correctly. They are generated, not written, so they cannot drift from
	// what was actually run.
	Caveats  []string  `json:"caveats"`
	RanAt    time.Time `json:"ran_at"`
	Outcomes []Outcome `json:"outcomes"`
}

// ComparisonResult is one arm-versus-arm answer.
type ComparisonResult struct {
	Question       string  `json:"question"`
	Baseline       string  `json:"baseline"`
	Variant        string  `json:"variant"`
	WhatItIsolates string  `json:"what_it_isolates"`
	BaselineRate   Rate    `json:"baseline_rate"`
	VariantRate    Rate    `json:"variant_rate"`
	Delta          float64 `json:"delta"`
	// Significant is false whenever the intervals overlap. It is kept because
	// the intervals are still what the rates are reported with, but it is no
	// longer what decides the verdict: it treats two arms given the same tasks
	// as independent samples, and answers "cannot tell" long after the data
	// could tell.
	Significant bool `json:"significant"`
	// Paired is what the verdict is drawn from. Every arm sees the same tasks,
	// so the comparison is made within a task and the difficulty of that task
	// cancels instead of being counted as noise.
	Paired Paired `json:"paired"`
	// Metrics are the paired, task-clustered differences across every metric
	// an arm can move — localization recall and packet tokens before the
	// solve rate, because the solve rate is the last thing to move and the
	// noisiest place to look for an effect.
	Metrics []MetricDelta `json:"metrics,omitempty"`
	// Score compares how far through each task's hidden tests the arms got.
	// It has more power than the binary test and answers a narrower question,
	// so it is reported beside that test rather than in place of it.
	Score   PairedScore `json:"score"`
	Verdict string      `json:"verdict"`
}

// Aggregate turns outcomes into a report.
func Aggregate(outcomes []Outcome, tasks []Task) Report {
	rep := Report{
		TaskCount: len(tasks),
		LeakMix:   map[LeakRisk]int{},
		RanAt:     time.Now().UTC(),
		Outcomes:  outcomes,
	}
	for _, t := range tasks {
		rep.LeakMix[t.LeakRisk]++
	}
	rep.derive()
	return rep
}

// derive recomputes everything that is a function of the outcomes.
//
// It is separate from Aggregate so that a saved result file can be re-analysed
// without being re-run. The outcomes are the measurement; the arms, the
// comparisons and the caveats are readings taken from it, and a reading that
// improves should improve for runs that have already been paid for. Eight GPU
// hours should not have to be spent again to apply a better test to them.
func (r *Report) derive() {
	outcomes := r.Outcomes
	r.Arms = nil
	r.Comparisons = nil

	byArm := map[string][]Outcome{}
	var order []string
	for _, o := range outcomes {
		if _, seen := byArm[o.Arm]; !seen {
			order = append(order, o.Arm)
		}
		byArm[o.Arm] = append(byArm[o.Arm], o)
	}

	rates := map[string]Rate{}
	for _, arm := range order {
		res := summarise(arm, byArm[arm])
		r.Arms = append(r.Arms, res)
		rates[arm] = res.Solved
	}

	for _, c := range Comparisons() {
		base, haveBase := rates[c.Baseline]
		variant, haveVariant := rates[c.Variant]
		if !haveBase || !haveVariant {
			continue
		}
		cr := ComparisonResult{
			Question: c.Question, Baseline: c.Baseline, Variant: c.Variant,
			WhatItIsolates: c.WhatItIsolates,
			BaselineRate:   base, VariantRate: variant,
			Delta:       variant.Value - base.Value,
			Significant: !base.Overlaps(variant),
			Paired:      pairArms(outcomes, c.Baseline, c.Variant),
			Score:       pairScores(outcomes, c.Baseline, c.Variant),
			// The seed is fixed and recorded, so rendering the same result
			// file twice produces the same interval. A report whose numbers
			// move when you read it again is not a report.
			Metrics: CompareAllMetrics(outcomes, c.Baseline, c.Variant, comparisonSeed),
		}
		cr.Verdict = verdictFor(cr)
		r.Comparisons = append(r.Comparisons, cr)
	}

	r.Caveats = caveats(*r)
}

// comparisonSeed fixes every bootstrap in a report. It is a constant rather
// than a parameter because two readings of one artifact must agree, and it is
// recorded in each MetricDelta so a reader can reproduce the interval.
const comparisonSeed int64 = 20260919

func summarise(arm string, outcomes []Outcome) ArmResult {
	res := ArmResult{Arm: arm, ByCategory: map[Category]Rate{}}

	var valid []Outcome
	for _, o := range outcomes {
		if o.Errored() {
			res.Errored++
			continue
		}
		valid = append(valid, o)
	}

	var solved, falseAccept, missed, tampered int
	var recall, precision, genRecall float64
	var judgeLatencies []int64
	var packetTokens []int
	var durations []time.Duration
	var attempts []int
	byCategory := map[Category][2]int{}

	for _, o := range valid {
		counts := byCategory[o.Category]
		counts[1]++
		if o.Solved {
			solved++
			counts[0]++
		}
		if o.FalseAccept {
			falseAccept++
		}
		if o.MissedSuccess {
			missed++
		}
		if o.Tampered {
			tampered++
		}
		byCategory[o.Category] = counts
		durations = append(durations, o.Duration)
		attempts = append(attempts, o.Attempts)
		res.TotalTokens += o.TokensUsed
		switch {
		case o.Localization == nil || o.Localization.Status == LocalizationUnscorable:
			res.Unscorable++
		default:
			recall += o.Localization.Recall
			precision += o.Localization.Precision
			genRecall += o.Localization.GeneratorRecall
			res.LostInRanking += len(o.Localization.LostInRanking)
			res.LocalizedRuns++
		}
		res.RerankAttempts += o.Rerank.Attempted
		res.RerankApplied += o.Rerank.Applied
		res.JudgeRequests += o.Rerank.Requests
		res.JudgeInputTokens += o.Rerank.InputTokens
		if o.Rerank.LatencyMS > 0 {
			judgeLatencies = append(judgeLatencies, o.Rerank.LatencyMS)
		}
		for _, reason := range o.Rerank.SkipReasons {
			if res.RerankSkips == nil {
				res.RerankSkips = map[string]int{}
			}
			res.RerankSkips[reason]++
		}
		if o.PacketTokens > 0 {
			packetTokens = append(packetTokens, o.PacketTokens)
		}
	}
	if res.LocalizedRuns > 0 {
		res.MeanRecall = recall / float64(res.LocalizedRuns)
		res.MeanPrecision = precision / float64(res.LocalizedRuns)
		res.MeanGeneratorRecall = genRecall / float64(res.LocalizedRuns)
	}
	if res.RerankAttempts > 0 {
		res.RerankAppliedRate = float64(res.RerankApplied) / float64(res.RerankAttempts)
	}
	res.JudgeLatencyP50 = percentileInt64(judgeLatencies, 0.50)
	res.JudgeLatencyP95 = percentileInt64(judgeLatencies, 0.95)
	res.MeanPacketTokens = meanInt(packetTokens)

	n := len(valid)
	res.Solved = NewRate(solved, n)
	res.FalseAccept = NewRate(falseAccept, n)
	res.MissedSuccess = NewRate(missed, n)
	res.Tampered = NewRate(tampered, n)
	res.MeanScore, res.GradedRuns = meanScore(valid)
	res.MedianDuration = medianDuration(durations)
	res.MedianAttempts = medianInt(attempts)

	for cat, counts := range byCategory {
		res.ByCategory[cat] = NewRate(counts[0], counts[1])
	}
	return res
}

// verdictFor reads the paired test, and says how many pairs it rests on.
//
// A verdict with three discordant pairs behind it and a verdict with thirty
// are different claims, and a reader who is shown only "better by 20 points"
// cannot tell them apart.
func verdictFor(c ComparisonResult) string {
	p := c.Paired
	switch {
	case c.BaselineRate.Total == 0 || c.VariantRate.Total == 0:
		return "not run"
	case p.Pairs() == 0:
		return "no shared runs to compare"
	case p.Decided() && c.Delta > 0:
		return fmt.Sprintf("%s is better: it solved %d that %s did not and lost %d the other way "+
			"(exact p=%.3f over %d disagreeing pair(s))",
			c.Variant, p.VariantOnly, c.Baseline, p.BaselineOnly, p.P, p.Discordant())
	case p.Decided() && c.Delta < 0:
		return fmt.Sprintf("%s is WORSE: it lost %d that %s solved and won %d back "+
			"(exact p=%.3f over %d disagreeing pair(s))",
			c.Variant, p.BaselineOnly, c.Baseline, p.VariantOnly, p.P, p.Discordant())
	case p.Discordant() == 0:
		return fmt.Sprintf("the two arms agreed on all %d shared run(s); this set did not "+
			"separate them at all", p.Pairs())
	default:
		return fmt.Sprintf("no detectable difference (%.0f-point gap; the arms disagreed on "+
			"%d of %d shared run(s), %d–%d, exact p=%.2f)",
			c.Delta*100, p.Discordant(), p.Pairs(), p.VariantOnly, p.BaselineOnly, p.P)
	}
}

// caveats are generated from what was actually run, so they cannot drift from
// the numbers they qualify.
func caveats(rep Report) []string {
	var out []string

	// Instability is reported before anything else, because it governs how
	// much weight every other number can carry. A cell that comes out solved
	// on one pass and a false accept on the next has not been measured, and a
	// confidence interval computed over it assumes a stability the data
	// contradicts.
	if passes, unstable, cells := stability(rep.Outcomes); passes > 1 && unstable > 0 {
		out = append(out, fmt.Sprintf(
			"%d of %d task/arm cells changed verdict between passes. The set was run %d times; "+
				"a cell that disagrees with itself is not evidence about an arm, and comparisons "+
				"drawn across arms are weaker than the intervals alone suggest.",
			unstable, cells, passes))
	} else if passes == 1 {
		out = append(out, "The set was run once. A single run of a cell is one sample, not a "+
			"measurement of it: re-running can change a verdict. Pass --repeat to see the spread.")
	}

	if rep.TaskCount < 30 {
		out = append(out, fmt.Sprintf(
			"The task set has %d tasks. Confidence intervals at this size are wide enough that "+
				"only large differences are detectable; treat every rate as indicative.", rep.TaskCount))
	}
	if n := rep.LeakMix[LeakPublic]; n > 0 {
		out = append(out, fmt.Sprintf(
			"%d task(s) come from public sources. Results over those are an upper bound: the model "+
				"may have seen them in training.", n))
	}
	if n := rep.LeakMix[LeakUnknown]; n > 0 {
		out = append(out, fmt.Sprintf(
			"%d task(s) have unestablished provenance. Their contribution to these numbers cannot "+
				"be interpreted.", n))
	}
	for _, arm := range rep.Arms {
		if arm.Errored > 0 {
			out = append(out, fmt.Sprintf(
				"%s: %d run(s) errored and are excluded from its rates. A harness fault is not "+
					"evidence about the system, but a high count means the numbers cover fewer tasks "+
					"than the set size suggests.", arm.Arm, arm.Errored))
		}
		if arm.FalseAccept.Successes > 0 {
			out = append(out, fmt.Sprintf(
				"%s claimed success and was wrong on %d run(s) (%s). A solved rate should never be "+
					"read without this number beside it.",
				arm.Arm, arm.FalseAccept.Successes, arm.FalseAccept))
		}
		if arm.Tampered.Successes > 0 {
			out = append(out, fmt.Sprintf(
				"%s changed protected files on %d run(s); those are counted as unsolved.",
				arm.Arm, arm.Tampered.Successes))
		}
	}
	return out
}

// Format renders a report for a terminal and for docs/benchmarks/results/.
func (r Report) Format() string {
	var b strings.Builder

	fmt.Fprintf(&b, "Evaluation — %d tasks, %d arms, %s\n",
		r.TaskCount, len(r.Arms), r.RanAt.Format(time.RFC3339))
	b.WriteString(r.provenanceHeader())

	fmt.Fprintf(&b, "%-28s %-22s %-22s %-7s %-7s %-7s %-7s %s\n",
		"ARM", "SOLVED", "FALSE ACCEPT", "TESTS", "GEN-R", "PKT-R", "RERANK", "MEDIAN")
	var anyRecall, anyRerank bool
	for _, arm := range r.Arms {
		tests := "—"
		if arm.GradedRuns > 0 {
			tests = fmt.Sprintf("%.0f%%", *arm.MeanScore*100)
		}
		genR, pktR := "—", "—"
		if arm.LocalizedRuns > 0 {
			genR = fmt.Sprintf("%.0f%%", arm.MeanGeneratorRecall*100)
			pktR = fmt.Sprintf("%.0f%%", arm.MeanRecall*100)
			anyRecall = true
		}
		applied := "—"
		if arm.RerankAttempts > 0 {
			applied = fmt.Sprintf("%.0f%%", arm.RerankAppliedRate*100)
			anyRerank = true
		}
		fmt.Fprintf(&b, "%-28s %-22s %-22s %-7s %-7s %-7s %-7s %s\n",
			arm.Arm, arm.Solved.String(), arm.FalseAccept.String(), tests, genR, pktR, applied,
			arm.MedianDuration.Round(time.Second))
	}
	if graded := gradedArms(r.Arms); graded > 0 {
		b.WriteString("\nTESTS is the mean share of each task's hidden tests a run satisfied. " +
			"SOLVED is\nthe only measure of whether the task was done; TESTS exists because it " +
			"separates\narms from far fewer runs.\n")
	}
	if anyRecall {
		b.WriteString("\nGEN-R is the share of the files the real fixing commit touched that the " +
			"candidate\nset held before reranking; PKT-R the share the packet finally carried. " +
			"GEN-R bounds\nPKT-R: a file nothing proposed cannot be ranked in, so a gap between " +
			"them is the\nranking's failure and a low GEN-R is retrieval's.\n")
	}
	if anyRerank {
		b.WriteString("\nRERANK is the share of rerank attempts that actually determined an " +
			"ordering.\nBelow 100% some of this arm's runs received the baseline's treatment " +
			"under this\narm's name; the skip reasons say why.\n")
	}
	if unscorable := unscorableRuns(r.Arms); unscorable > 0 {
		fmt.Fprintf(&b, "\n%d run(s) could not be scored for localization: their task records "+
			"no\nexpected files. Those runs contribute to SOLVED and to nothing else.\n", unscorable)
	}
	if r.Frozen.JudgmentModel != "" {
		fmt.Fprintf(&b, "\nJudgment model: %s\n", r.Frozen.JudgmentModel)
	}
	for _, arm := range r.Arms {
		if arm.JudgeRequests == 0 {
			continue
		}
		fmt.Fprintf(&b, "%s: %d external request(s), %d input token(s), "+
			"latency p50 %dms / p95 %dms\n",
			arm.Arm, arm.JudgeRequests, arm.JudgeInputTokens,
			arm.JudgeLatencyP50, arm.JudgeLatencyP95)
		if len(arm.RerankSkips) > 0 {
			reasons := make([]string, 0, len(arm.RerankSkips))
			for reason, n := range arm.RerankSkips {
				reasons = append(reasons, fmt.Sprintf("%s×%d", reason, n))
			}
			sort.Strings(reasons)
			fmt.Fprintf(&b, "%s: skipped — %s\n", arm.Arm, strings.Join(reasons, ", "))
		}
	}

	if len(r.Comparisons) > 0 {
		b.WriteString("\nComparisons\n")
		for _, c := range r.Comparisons {
			fmt.Fprintf(&b, "\n  %s\n", c.Question)
			fmt.Fprintf(&b, "    %s: %s\n", c.Baseline, c.BaselineRate)
			fmt.Fprintf(&b, "    %s: %s\n", c.Variant, c.VariantRate)
			fmt.Fprintf(&b, "    → %s\n", c.Verdict)
			if sc := c.Score; sc.Recorded && sc.Pairs > 1 {
				tail := "which does not exclude no difference"
				if sc.Decided() {
					tail = "which excludes no difference"
				}
				fmt.Fprintf(&b, "    → on hidden tests: %+.1f points over %d pair(s), "+
					"95%% [%+.1f, %+.1f], %s\n",
					sc.MeanDelta*100, sc.Pairs, sc.Low*100, sc.High*100, tail)
			}
			for _, m := range c.Metrics {
				if m.Tasks == 0 {
					continue
				}
				fmt.Fprintf(&b, "      %-24s %+8.3f %-8s  95%% [%+.3f, %+.3f]  "+
					"W/L/T %d/%d/%d over %d task(s)  %s\n",
					m.Metric, m.Delta, m.Unit, m.Low, m.High,
					m.Wins, m.Losses, m.Ties, m.Tasks, m.Verdict)
			}
			fmt.Fprintf(&b, "      isolates: %s\n", c.WhatItIsolates)
		}
		b.WriteString("\nEvery delta is variant minus baseline, averaged within each task " +
			"before\nacross tasks, with a 95% interval from a task-clustered bootstrap " +
			"(10,000\nresamples, fixed seed). W/L/T counts tasks, not runs. An interval " +
			"spanning\nzero is \"not shown\" whatever the midpoint says — a numerical " +
			"difference\nwithout uncertainty beside it is not an improvement.\n" +
			"For packet tokens and duration, lower is better and a win means less.\n")
	}

	byCategory := map[Category]bool{}
	for _, arm := range r.Arms {
		for cat := range arm.ByCategory {
			byCategory[cat] = true
		}
	}
	if len(byCategory) > 0 {
		b.WriteString("\nBy category\n")
		cats := make([]Category, 0, len(byCategory))
		for c := range byCategory {
			cats = append(cats, c)
		}
		sort.Slice(cats, func(i, j int) bool { return cats[i] < cats[j] })
		for _, arm := range r.Arms {
			fmt.Fprintf(&b, "\n  %s\n", arm.Arm)
			for _, cat := range cats {
				if rate, ok := arm.ByCategory[cat]; ok {
					fmt.Fprintf(&b, "    %-16s %s\n", cat, rate)
				}
			}
		}
	}

	if len(r.Caveats) > 0 {
		b.WriteString("\nHow to read these numbers\n")
		for _, c := range r.Caveats {
			for i, line := range wrap(c, 74) {
				if i == 0 {
					fmt.Fprintf(&b, "  - %s\n", line)
					continue
				}
				fmt.Fprintf(&b, "    %s\n", line)
			}
		}
	}
	return b.String()
}

// percentileInt64 is the nearest-rank percentile. It is used for latency,
// where the tail is the number that decides whether a budget is being hit and
// a mean would hide it.
func percentileInt64(v []int64, p float64) int64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]int64(nil), v...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	idx := int(math.Ceil(p*float64(len(s)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(s) {
		idx = len(s) - 1
	}
	return s[idx]
}

func meanInt(v []int) int {
	if len(v) == 0 {
		return 0
	}
	total := 0
	for _, n := range v {
		total += n
	}
	return total / len(v)
}

// provenanceHeader states what produced these numbers, before any of them.
//
// The dirty-tree line comes first and is not softened. A run against modified
// code is not invalid, but the commit it records does not describe what
// executed, and a reader who learns that in a footnote has already believed
// the table.
func (r Report) provenanceHeader() string {
	p := r.Provenance
	if p.ExperimentID == "" {
		return "\n"
	}
	var b strings.Builder
	if p.Dirty {
		b.WriteString("\n!! WORKING TREE DIRTY — the commit below does not describe the code\n" +
			"!! that ran. These numbers are not publishable; re-run from a clean tree\n" +
			"!! with --require-clean.\n")
	}
	fmt.Fprintf(&b, "\nexperiment %s  commit %s%s\n", p.ExperimentID, shortSHA(p.Commit),
		map[bool]string{true: " (dirty)", false: ""}[p.Dirty])
	if p.RunID != "" {
		fmt.Fprintf(&b, "run %s\n", p.RunID)
	}
	if p.Set != "" {
		fmt.Fprintf(&b, "set %s, %d pass(es) per cell\n", p.Set, p.Repeat)
	}
	if p.ReasoningModel != "" {
		fmt.Fprintf(&b, "reasoning %s\n", p.ReasoningModel)
	}
	if p.JudgmentModelRequested != "" {
		// The requested identifier and the served one are two separate
		// pieces of evidence, and the report prints the second as a status
		// rather than letting the first stand in for it.
		status, detail := p.ModelIdentity()
		fmt.Fprintf(&b, "judgment  requested %s, served identity %s\n",
			p.JudgmentModelRequested, status)
		if status != ModelIdentityVerified {
			for _, line := range wrap("          "+detail, 76) {
				fmt.Fprintf(&b, "%s\n", line)
			}
		}
		if p.AllowUnverifiedModel {
			fmt.Fprintf(&b, "          ran with --allow-unverified-model: a development "+
				"diagnostic, not a\n          publishable result\n")
		}
	}
	if p.EmbeddingModel != "" {
		fmt.Fprintf(&b, "embedding %s\n", p.EmbeddingModel)
	}
	fmt.Fprintf(&b, "dataset %s  membership %s  rubric %s\n",
		shortSHA(p.DatasetDigest), shortSHA(p.MembershipDigest),
		orUnknown(p.AnnotationRubricVersion))
	if ok, why := p.Publishable(); !ok {
		fmt.Fprintf(&b, "NOT PUBLISHABLE: %s\n", why)
	}
	b.WriteString("\n")
	return b.String()
}

// unscorableRuns totals the runs no localization score could be computed for.
func unscorableRuns(arms []ArmResult) int {
	total := 0
	for _, a := range arms {
		total += a.Unscorable
	}
	return total
}

func medianDuration(d []time.Duration) time.Duration {
	if len(d) == 0 {
		return 0
	}
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	return d[len(d)/2]
}

func medianInt(v []int) int {
	if len(v) == 0 {
		return 0
	}
	sort.Ints(v)
	return v[len(v)/2]
}

func wrap(s string, width int) []string {
	var lines []string
	var cur string
	for _, word := range strings.Fields(s) {
		switch {
		case cur == "":
			cur = word
		case len(cur)+1+len(word) > width:
			lines = append(lines, cur)
			cur = word
		default:
			cur += " " + word
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// Save writes a report as JSON.
//
// It lives here rather than in the CLI because a report knows its own format,
// and because §2.3 confines file writes to the packages allowed to perform
// them — of which this is one, for evaluation output.
func (r Report) Save(path string) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("eval: create %s: %w", dir, err)
		}
	}
	body, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(body, '\n'), 0o640) //nolint:gosec // an operator-chosen output path
}

// LoadReport reads a saved report.
func LoadReport(path string) (Report, error) {
	body, err := os.ReadFile(path) //nolint:gosec // an operator-supplied results file
	if err != nil {
		return Report{}, err
	}
	var r Report
	if err := json.Unmarshal(body, &r); err != nil {
		return Report{}, err
	}
	// Re-derive rather than trust what the file recorded. The outcomes are the
	// measurement and they do not change; the arms, comparisons and caveats
	// are a reading of them, and this is what lets a sharper test be applied
	// to a run that has already been paid for.
	if len(r.Outcomes) > 0 {
		r.derive()
	}
	return r, nil
}

// stability reports how many task/arm cells did not agree with themselves
// across passes, and how many passes there were.
func stability(outcomes []Outcome) (passes, unstable, cells int) {
	type cell struct{ task, arm string }
	seen := map[cell]map[bool]int{}
	for _, o := range outcomes {
		if o.Errored() {
			continue
		}
		if o.Repetition > passes {
			passes = o.Repetition
		}
		c := cell{o.TaskID, o.Arm}
		if seen[c] == nil {
			seen[c] = map[bool]int{}
		}
		seen[c][o.Solved]++
	}
	if passes == 0 {
		passes = 1
	}
	for _, verdicts := range seen {
		cells++
		if len(verdicts) > 1 {
			unstable++
		}
	}
	return passes, unstable, cells
}

// gradedArms counts the arms that have any graded runs, so the explanation of
// the TESTS column is printed only when there is a column to explain.
func gradedArms(arms []ArmResult) int {
	n := 0
	for _, a := range arms {
		if a.GradedRuns > 0 {
			n++
		}
	}
	return n
}
