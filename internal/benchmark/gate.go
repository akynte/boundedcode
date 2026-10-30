package benchmark

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/config"
)

// The regression gate compares a candidate scorecard with a baseline under
// a policy. Benchmarks of an agent on a local model are noisy: a task that
// passes about half the time flips between runs of the same build. A rule
// therefore calls a metric regressed only when it is worse beyond the
// rule's tolerance and beyond the noise the data shows:
//   - a rate, when the 90% Wilson intervals of the two sides do not
//     overlap, or (success) when at least min_flips tasks passed in every
//     baseline trial and failed in every candidate trial;
//   - a figure such as tokens or seconds, when the candidate is worse by
//     more than the tolerance widened to the baseline trials' own spread;
//   - a count (security failures), when it is higher than the baseline's
//     by more than the tolerance (zero: any new failure).
//
// Sides that are not comparable (another model file, task set or budget)
// are refused before any rule is applied.

// GatePolicy is the policy file (benchmarks/gate-policy.yaml).
type GatePolicy struct {
	Version    int        `yaml:"version"`
	Comparable Comparable `yaml:"comparable"`
	Rules      []GateRule `yaml:"rules"`
}

// Comparable says what must be equal on both sides.
type Comparable struct {
	SameModel   bool     `yaml:"same_model"`
	SameTaskSet bool     `yaml:"same_task_set"`
	SameBudgets []string `yaml:"same_budgets"`
}

// GateRule is one metric's rule.
type GateRule struct {
	Metric string `yaml:"metric"` // a Metrics JSON name, e.g. success_rate
	// Scope is "overall" (the default), "language", "group" or "split":
	// the rule is applied to each key with at least MinRuns runs on both
	// sides.
	Scope   string `yaml:"scope"`
	MinRuns int    `yaml:"min_runs"`
	// Better is "higher" or "lower".
	Better string `yaml:"better"`
	// Test is "wilson" (a rate), "relative" (a positive figure) or
	// "count".
	Test string `yaml:"test"`
	// Tolerance: absolute for wilson and count, a fraction for relative.
	Tolerance float64 `yaml:"tolerance"`
	// MinFlips (success_rate, wilson): consistent per-task flips that make
	// a regression on their own (0: not used).
	MinFlips int `yaml:"min_flips"`
	// Blocking rules fail the gate; the others warn.
	Blocking bool `yaml:"blocking"`
}

// LoadGatePolicy reads and checks a policy file.
func LoadGatePolicy(path string) (*GatePolicy, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p GatePolicy
	if err := config.DecodeStrict(b, &p); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if p.Version != 1 {
		return nil, fmt.Errorf("%s: version %d is not 1", path, p.Version)
	}
	known := metricNames()
	for i, r := range p.Rules {
		switch {
		case !known[r.Metric]:
			return nil, fmt.Errorf("%s: rule %d: unknown metric %q", path, i+1, r.Metric)
		case r.Better != "higher" && r.Better != "lower":
			return nil, fmt.Errorf("%s: rule %d: better is higher or lower", path, i+1)
		case r.Test != "wilson" && r.Test != "relative" && r.Test != "count":
			return nil, fmt.Errorf("%s: rule %d: test is wilson, relative or count", path, i+1)
		case r.Scope != "" && r.Scope != "overall" && r.Scope != "language" && r.Scope != "group" && r.Scope != "split":
			return nil, fmt.Errorf("%s: rule %d: scope is overall, language, group or split", path, i+1)
		}
	}
	return &p, nil
}

func metricNames() map[string]bool {
	out := map[string]bool{}
	t := reflect.TypeOf(Metrics{})
	for i := range t.NumField() {
		if tag := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]; tag != "" && tag != "-" {
			out[tag] = true
		}
	}
	return out
}

// metricValue reads a metric by its JSON name.
func metricValue(m Metrics, name string) float64 {
	b, _ := json.Marshal(m)
	var all map[string]any
	_ = json.Unmarshal(b, &all)
	v, _ := all[name].(float64)
	return v
}

// Verdict is one rule on one key.
type Verdict struct {
	Rule     string  `json:"rule"`
	Key      string  `json:"key"` // "overall", or the language, group or split
	Baseline float64 `json:"baseline"`
	Cand     float64 `json:"candidate"`
	// Outcome: regressed, improved, unchanged, inconclusive.
	Outcome  string `json:"outcome"`
	Why      string `json:"why"`
	Blocking bool   `json:"blocking"`
}

// GateResult is the gate's answer.
type GateResult struct {
	Comparable bool      `json:"comparable"`
	Problems   []string  `json:"not_comparable,omitempty"`
	Verdicts   []Verdict `json:"verdicts"`
	Failed     bool      `json:"failed"` // a blocking rule regressed, or the sides are not comparable
}

// ErrGateFailed is returned by callers when the gate fails.
var ErrGateFailed = errors.New("regression gate failed")

// Gate compares cand with base under p.
func Gate(base, cand *Scorecard, p *GatePolicy) GateResult {
	var res GateResult
	res.Problems = comparability(base, cand, p.Comparable)
	res.Comparable = len(res.Problems) == 0
	if !res.Comparable {
		res.Failed = true
		return res
	}
	for _, r := range p.Rules {
		for _, kv := range scopePairs(base, cand, r) {
			v := judge(r, kv.key, kv.base, kv.cand, base, cand)
			res.Verdicts = append(res.Verdicts, v)
			if v.Outcome == "regressed" && r.Blocking {
				res.Failed = true
			}
		}
	}
	return res
}

func comparability(base, cand *Scorecard, c Comparable) []string {
	var out []string
	if base.Suite != cand.Suite {
		out = append(out, fmt.Sprintf("suites differ: %s against %s", base.Suite, cand.Suite))
	}
	if c.SameModel {
		bs, _ := base.Model["sha256"].(string)
		cs, _ := cand.Model["sha256"].(string)
		if bs != cs {
			out = append(out, fmt.Sprintf("model files differ: %s against %s", dash(bs), dash(cs)))
		}
	}
	if c.SameTaskSet && base.TaskSet != cand.TaskSet {
		out = append(out, fmt.Sprintf("task sets differ: %s against %s", dash(base.TaskSet), dash(cand.TaskSet)))
	}
	// The hash covers every definition; a run of some of them (--only) is
	// another task set too.
	if c.SameTaskSet {
		if only, extra := taskDiff(base.Tasks, cand.Tasks); len(only)+len(extra) > 0 {
			out = append(out, fmt.Sprintf("the runs cover different tasks: %d only in the baseline (%s), %d only in the candidate (%s)",
				len(only), dash(firstFew(only)), len(extra), dash(firstFew(extra))))
		}
	}
	for _, k := range c.SameBudgets {
		bv, cv := fmt.Sprint(base.Settings[k]), fmt.Sprint(cand.Settings[k])
		if bv != cv {
			out = append(out, fmt.Sprintf("budget %s differs: %s against %s", k, bv, cv))
		}
	}
	return out
}

// taskDiff lists the tasks only in a and only in b, sorted.
func taskDiff(a, b map[string]TaskTally) (onlyA, onlyB []string) {
	for k := range a {
		if _, ok := b[k]; !ok {
			onlyA = append(onlyA, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			onlyB = append(onlyB, k)
		}
	}
	sort.Strings(onlyA)
	sort.Strings(onlyB)
	return onlyA, onlyB
}

func firstFew(ids []string) string {
	if len(ids) > 3 {
		return strings.Join(ids[:3], ", ") + ", …"
	}
	return strings.Join(ids, ", ")
}

type pair struct {
	key        string
	base, cand Metrics
}

func scopePairs(base, cand *Scorecard, r GateRule) []pair {
	pick := func(s *Scorecard) map[string]Metrics {
		switch r.Scope {
		case "language":
			return s.ByLanguage
		case "group":
			return s.ByGroup
		case "split":
			return s.BySplit
		}
		return map[string]Metrics{"overall": s.Overall}
	}
	bm, cm := pick(base), pick(cand)
	var keys []string
	for k := range bm {
		if _, ok := cm[k]; ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var out []pair
	for _, k := range keys {
		out = append(out, pair{k, bm[k], cm[k]})
	}
	return out
}

func judge(r GateRule, key string, b, c Metrics, base, cand *Scorecard) Verdict {
	v := Verdict{Rule: r.Metric, Key: key, Baseline: metricValue(b, r.Metric), Cand: metricValue(c, r.Metric), Blocking: r.Blocking, Outcome: "unchanged"}
	if r.MinRuns > 0 && (b.Runs < r.MinRuns || c.Runs < r.MinRuns) {
		v.Outcome, v.Why = "inconclusive", fmt.Sprintf("fewer than %d runs on a side (%d, %d)", r.MinRuns, b.Runs, c.Runs)
		return v
	}
	worse := func(delta float64) bool { // delta = cand - base
		if r.Better == "higher" {
			return delta < 0
		}
		return delta > 0
	}
	delta := v.Cand - v.Baseline
	switch r.Test {
	case "count":
		if worse(delta) && math.Abs(delta) > r.Tolerance {
			v.Outcome, v.Why = "regressed", fmt.Sprintf("%.0f against %.0f (tolerance %.0f)", v.Cand, v.Baseline, r.Tolerance)
		} else if !worse(delta) && delta != 0 {
			v.Outcome, v.Why = "improved", fmt.Sprintf("%.0f against %.0f", v.Cand, v.Baseline)
		}
	case "wilson":
		bk, bn, ck, cn := rateCounts(r.Metric, b), rateCounts2(r.Metric, b), rateCounts(r.Metric, c), rateCounts2(r.Metric, c)
		blo, bhi := wilson90(bk, bn)
		clo, chi := wilson90(ck, cn)
		separated := chi < blo || clo > bhi
		flips := 0
		if r.Metric == "success_rate" && key == "overall" {
			flips = consistentFlips(base, cand, r.Better)
		}
		switch {
		case worse(delta) && math.Abs(delta) > r.Tolerance && separated:
			v.Outcome, v.Why = "regressed", fmt.Sprintf("%.3f against %.3f; 90%% intervals [%.2f, %.2f] and [%.2f, %.2f] do not overlap", v.Cand, v.Baseline, clo, chi, blo, bhi)
		case r.MinFlips > 0 && flips >= r.MinFlips:
			v.Outcome, v.Why = "regressed", fmt.Sprintf("%d tasks passed in every baseline trial and failed in every candidate trial", flips)
		case !worse(delta) && math.Abs(delta) > r.Tolerance && separated:
			v.Outcome, v.Why = "improved", fmt.Sprintf("%.3f against %.3f, beyond chance", v.Cand, v.Baseline)
		default:
			v.Why = fmt.Sprintf("%.3f against %.3f: within tolerance %.2f or chance (intervals [%.2f, %.2f] and [%.2f, %.2f])", v.Cand, v.Baseline, r.Tolerance, clo, chi, blo, bhi)
		}
	case "relative":
		if v.Baseline <= 0 {
			v.Outcome, v.Why = "inconclusive", "no baseline value"
			return v
		}
		tol := math.Max(r.Tolerance, trialSpread(base, r.Metric))
		ratio := v.Cand / v.Baseline
		switch {
		case worse(delta) && math.Abs(ratio-1) > tol:
			v.Outcome, v.Why = "regressed", fmt.Sprintf("%.3g against %.3g (×%.2f; allowed ±%.0f%%, the larger of the tolerance and the baseline trials' spread)", v.Cand, v.Baseline, ratio, 100*tol)
		case !worse(delta) && math.Abs(ratio-1) > tol:
			v.Outcome, v.Why = "improved", fmt.Sprintf("×%.2f, beyond ±%.0f%%", ratio, 100*tol)
		default:
			v.Why = fmt.Sprintf("×%.2f within ±%.0f%%", ratio, 100*tol)
		}
	}
	return v
}

// rateCounts and rateCounts2 are a rate's numerator and denominator.
func rateCounts(metric string, m Metrics) int {
	switch metric {
	case "false_acceptance_rate":
		return m.FalseAcceptance
	case "false_rejection_rate":
		return m.FalseRejection
	}
	return m.Passed
}

func rateCounts2(metric string, m Metrics) int {
	switch metric {
	case "false_acceptance_rate":
		return m.Runs - m.Passed
	case "false_rejection_rate":
		return m.Passed
	}
	return m.Runs
}

// consistentFlips counts tasks that passed in every baseline trial and
// failed in every candidate trial (better "higher"), each side run at
// least once.
func consistentFlips(base, cand *Scorecard, better string) int {
	n := 0
	for id, b := range base.Tasks {
		c, ok := cand.Tasks[id]
		if !ok || b.Runs == 0 || c.Runs == 0 {
			continue
		}
		if better == "higher" && b.Passed == b.Runs && c.Passed == 0 {
			n++
		}
	}
	return n
}

// trialSpread is the baseline trials' relative spread of a metric: the
// largest distance of a trial from the trials' mean, over the mean.
func trialSpread(s *Scorecard, metric string) float64 {
	if len(s.PerTrial) < 2 {
		return 0
	}
	var vs []float64
	for _, t := range s.PerTrial {
		vs = append(vs, metricValue(t, metric))
	}
	m := meanF(vs)
	if m <= 0 {
		return 0
	}
	spread := 0.0
	for _, v := range vs {
		spread = math.Max(spread, math.Abs(v-m)/m)
	}
	return spread
}

func meanF(v []float64) float64 {
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

// WriteGateMarkdown writes the gate's verdicts.
func WriteGateMarkdown(w io.Writer, g GateResult) {
	if !g.Comparable {
		fmt.Fprintln(w, "**Not comparable:**")
		for _, p := range g.Problems {
			fmt.Fprintln(w, "- "+p)
		}
		return
	}
	verdict := "passed"
	if g.Failed {
		verdict = "**failed**"
	}
	fmt.Fprintf(w, "Gate %s.\n\n| Metric | Key | Baseline | Candidate | Outcome | Blocking | Why |\n|---|---|---|---|---|---|---|\n", verdict)
	for _, v := range g.Verdicts {
		fmt.Fprintf(w, "| %s | %s | %.4g | %.4g | %s | %v | %s |\n", v.Rule, v.Key, v.Baseline, v.Cand, v.Outcome, v.Blocking, v.Why)
	}
}
