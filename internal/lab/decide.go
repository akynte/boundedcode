package lab

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/benchmark"
)

// Verdicts of an arm.
const (
	VerdictBaseline      = "baseline"
	VerdictAccept        = "accept"            // better on held-out tasks, everywhere, than the baseline and every simpler alternative
	VerdictWorkload      = "workload-specific" // better only for some languages: proposed for those alone
	VerdictPreferSimpler = "prefer-simpler"    // a simpler alternative does as well
	VerdictReject        = "reject"            // worse, costlier, incomparable, or not better than a simpler change
	VerdictInconclusive  = "inconclusive"      // no difference the experiment could detect
)

// Promotable reports whether a verdict may become a proposal (when it was
// also reached on independent, held-out tasks).
func Promotable(v string) bool { return v == VerdictAccept || v == VerdictWorkload }

// Decision is an experiment's outcome, arm by arm.
type Decision struct {
	Experiment string      `json:"experiment"`
	Primary    Metric      `json:"primary"`
	DecidedOn  string      `json:"decided_on"` // heldout or dev
	Arms       []ArmResult `json:"arms"`
	// HeldoutLanguages are the languages the decided split's runs cover.
	HeldoutLanguages []string `json:"heldout_languages"`
	Notes            []string `json:"notes,omitempty"`
}

// ArmResult is one arm's outcome.
type ArmResult struct {
	Arm     string   `json:"arm"`
	Role    string   `json:"role"`
	Metric  Metric   `json:"metric"` // the arm's pre-registered primary metric
	Verdict string   `json:"verdict"`
	Reasons []string `json:"reasons"`
	// Scope are the languages a workload-specific verdict applies to.
	Scope []string `json:"scope,omitempty"`
	// Independent: decided on held-out tasks. A decision on development
	// tasks (a past experiment re-read) is never promoted.
	Independent bool `json:"independent"`
	// Decided is the primary metric against the baseline on the split the
	// experiment is decided on (held-out tasks unless decide_on says
	// otherwise); Other on the other split, for information.
	Decided benchmark.Verdict  `json:"decided"`
	Other   *benchmark.Verdict `json:"other_split,omitempty"`
	// Paired is the pre-registered test of the primary metric, task by
	// task on the decided split; the verdict rests on it when the metric
	// has one, and on Decided otherwise.
	Paired *PairedResult `json:"paired,omitempty"`
	// Gate is the held-out comparison under the policy, with per-language
	// regressions blocking.
	Gate benchmark.GateResult `json:"gate"`
	// VsSimpler is the primary metric against each simpler arm.
	VsSimpler map[string]Outcome `json:"vs_simpler,omitempty"`
	// WithoutLanguage says, for each language, whether the others still
	// gain without its runs: an improvement that is gone without one
	// language is that language's.
	WithoutLanguage map[string]string `json:"without_language,omitempty"`
	// Languages is the per-language outcome of the primary metric.
	Languages map[string]string `json:"languages,omitempty"`
	// Predictions checks the arm's predicted fixes and risks.
	Predictions *PredictionCheck `json:"predictions,omitempty"`
	// MinDetectable is the smallest success difference the held-out runs
	// could show beyond chance.
	MinDetectable float64 `json:"min_detectable_success_difference"`
	// Cards are the arm's held-out and development scorecards.
	Heldcard *benchmark.Scorecard `json:"-"`
}

// Outcome is a comparison's result and its reason.
type Outcome struct {
	Outcome string `json:"outcome"`
	Why     string `json:"why"`
}

// PredictionCheck compares an arm's predictions with what happened.
type PredictionCheck struct {
	Fixed      []string `json:"fixed"`      // predicted fixes that now pass more often
	NotFixed   []string `json:"not_fixed"`  // predicted fixes that do not
	Worsened   []string `json:"worsened"`   // predicted fixes that now pass less often
	Broke      []string `json:"broke"`      // predicted risks that now pass less often
	Unforeseen []string `json:"unforeseen"` // tasks that now pass less often and were not predicted
}

// Decide decides an experiment from its arms' reports (arm → report
// files). meta supplies task metadata older reports lack.
func Decide(e *Experiment, reports map[string][]string, meta map[string]benchmark.CatalogEntry, policy *benchmark.GatePolicy) (*Decision, error) {
	d := &Decision{Experiment: e.ID, Primary: e.Primary}
	split := func(s string) func(benchmark.RunKey) bool {
		return func(k benchmark.RunKey) bool { return k.Split == s }
	}
	card := func(arm string, keep func(benchmark.RunKey) bool) (*benchmark.Scorecard, error) {
		return benchmark.BuildScorecardWhere(reports[arm], meta, keep)
	}
	base := e.Baseline()
	on, other := "heldout", "dev"
	if e.DecideOn == "dev" {
		on, other = "dev", "heldout"
		d.Notes = append(d.Notes, "decided on development tasks (decide_on: dev): not independent of tuning, so no verdict here can become a proposal")
	}
	d.DecidedOn = on
	bh, err := card(base.Name, split(on))
	if err != nil {
		return nil, fmt.Errorf("arm %s: %w", base.Name, err)
	}
	bd, err := card(base.Name, split(other))
	if err != nil {
		return nil, err
	}
	ball, err := card(base.Name, nil)
	if err != nil {
		return nil, err
	}
	d.HeldoutLanguages = sortedKeys(langsWithRuns(bh))
	minLangs := e.MinLanguages
	if minLangs == 0 {
		minLangs = 3
	}
	vals := map[string][]benchmark.RunValue{}
	for _, a := range e.Arms {
		v, err := benchmark.RunValues(reports[a.Name], meta)
		if err != nil {
			return nil, fmt.Errorf("arm %s: %w", a.Name, err)
		}
		vals[a.Name] = v
	}
	// compare is the primary metric's outcome of cand against base over the
	// runs keep accepts: the paired test when there is one.
	compare := func(m Metric, base, cand string, keep func(benchmark.RunKey) bool) Outcome {
		rule := primaryRule(m, policy)
		if pairable(m.Name) {
			p := Paired(m, pairedTolerance(rule), vals[base], vals[cand], keep)
			return Outcome{p.Outcome, p.Why}
		}
		b1, err1 := benchmark.BuildScorecardWhere(reports[base], meta, keep)
		c1, err2 := benchmark.BuildScorecardWhere(reports[cand], meta, keep)
		if err1 != nil || err2 != nil || b1.Overall.Runs == 0 || c1.Overall.Runs == 0 {
			return Outcome{"inconclusive", "no runs to compare"}
		}
		v := benchmark.Judge(rule, b1, c1)
		return Outcome{v.Outcome, v.Why}
	}
	d.Arms = append(d.Arms, ArmResult{Arm: base.Name, Role: "baseline", Verdict: VerdictBaseline, Reasons: []string{"the fixed reference"}, Heldcard: bh})
	results := map[string]int{} // arm → index in d.Arms
	cards := map[string]*benchmark.Scorecard{}
	// Simpler arms first: candidates are compared with their verdicts.
	order := append([]Arm(nil), e.Arms...)
	sort.SliceStable(order, func(i, j int) bool { return order[i].Role == "simpler" && order[j].Role != "simpler" })
	for _, a := range order {
		if a.Role == "baseline" {
			continue
		}
		metric := e.MetricFor(a)
		rule := primaryRule(metric, policy)
		res := ArmResult{Arm: a.Name, Role: a.Role, Metric: metric, Independent: on == "heldout", VsSimpler: map[string]Outcome{}, WithoutLanguage: map[string]string{}, Languages: map[string]string{}}
		ah, err := card(a.Name, split(on))
		if err != nil {
			return nil, fmt.Errorf("arm %s: %w", a.Name, err)
		}
		res.Heldcard = ah
		cards[a.Name] = ah
		if bh.Overall.Runs == 0 || ah.Overall.Runs == 0 {
			res.Verdict, res.Reasons = VerdictInconclusive, []string{fmt.Sprintf("no %s runs: the experiment cannot be decided on independent tasks", on)}
			d.Arms = append(d.Arms, res)
			results[a.Name] = len(d.Arms) - 1
			continue
		}
		res.MinDetectable = MinDetectable(bh.Overall.SuccessRate, min(bh.Overall.Runs, ah.Overall.Runs))
		// The arm's own budget change is the point of the comparison.
		p := *policy
		p.Comparable.SameBudgets = without(policy.Comparable.SameBudgets, budgetKeys(a.Set))
		p.Rules = strictLanguages(policy.Rules)
		res.Gate = benchmark.Gate(bh, ah, &p)
		res.Decided = benchmark.Judge(rule, bh, ah)
		primary := Outcome{res.Decided.Outcome, res.Decided.Why}
		if pairable(metric.Name) {
			p := Paired(metric, pairedTolerance(rule), vals[base.Name], vals[a.Name], split(on))
			res.Paired = &p
			primary = Outcome{p.Outcome, p.Why}
		}
		if ad, err := card(a.Name, split(other)); err == nil && bd.Overall.Runs > 0 && ad.Overall.Runs > 0 {
			v := benchmark.Judge(rule, bd, ad)
			res.Other = &v
		}
		for _, l := range d.HeldoutLanguages {
			keep := func(k benchmark.RunKey) bool { return k.Split == on && contains(k.Languages, l) }
			res.Languages[l] = compare(metric, base.Name, a.Name, keep).Outcome
		}
		aall, err := card(a.Name, nil)
		if err != nil {
			return nil, err
		}
		res.Predictions = checkPredictions(a, ball, aall)
		var reasons []string
		verdict := ""
		switch {
		case !res.Gate.Comparable:
			verdict = VerdictReject
			reasons = append(reasons, "not comparable with the baseline: "+strings.Join(res.Gate.Problems, "; "))
		case aall.Overall.SecurityFailures > ball.Overall.SecurityFailures || aall.Overall.TamperedRuns > ball.Overall.TamperedRuns:
			verdict = VerdictReject
			reasons = append(reasons, fmt.Sprintf("security failures %d against %d, tampered runs %d against %d", aall.Overall.SecurityFailures, ball.Overall.SecurityFailures, aall.Overall.TamperedRuns, ball.Overall.TamperedRuns))
		case res.Gate.Failed:
			verdict = VerdictReject
			reasons = append(reasons, "regressed on held-out tasks: "+regressions(res.Gate, true))
		}
		if verdict == "" {
			if c := costRegressions(res.Gate, e, metric); c != "" {
				verdict = VerdictReject
				reasons = append(reasons, "costs more than the baseline allows: "+c)
			}
		}
		if verdict == "" {
			switch primary.Outcome {
			case "regressed":
				verdict = VerdictReject
				reasons = append(reasons, fmt.Sprintf("%s is worse: %s", rule.Metric, primary.Why))
			case "improved":
				reasons = append(reasons, fmt.Sprintf("%s improved on %s tasks: %s", rule.Metric, on, primary.Why))
				verdict = VerdictAccept
				// Not driven by a single language: without it, the other
				// languages must still gain on balance. (Significance is not
				// asked of them: leaving tasks out costs power, and a gain
				// spread over every language would otherwise look like each
				// language's alone.)
				var drivers []string
				for _, l := range d.HeldoutLanguages {
					keep := func(k benchmark.RunKey) bool { return k.Split == on && !contains(k.Languages, l) }
					gains := false
					if pairable(metric.Name) {
						p := Paired(metric, pairedTolerance(rule), vals[base.Name], vals[a.Name], keep)
						gains = p.Better > p.Worse
					} else {
						gains = compare(metric, base.Name, a.Name, keep).Outcome == "improved"
					}
					res.WithoutLanguage[l] = "the others still gain"
					if !gains {
						res.WithoutLanguage[l] = "no gain in the others"
						drivers = append(drivers, l)
					}
				}
				sort.Strings(drivers)
				if len(drivers) > 0 {
					verdict = VerdictWorkload
					res.Scope = drivers
					reasons = append(reasons, "the improvement disappears without "+strings.Join(drivers, ", ")+": it is not shown for the other languages")
				}
				if verdict == VerdictAccept && len(d.HeldoutLanguages) < minLangs {
					verdict = VerdictWorkload
					res.Scope = d.HeldoutLanguages
					reasons = append(reasons, fmt.Sprintf("the held-out runs cover %d languages (%s); %d are needed to accept a change for every workload", len(d.HeldoutLanguages), strings.Join(d.HeldoutLanguages, ", "), minLangs))
				}
			default:
				var better []string
				for l, o := range res.Languages {
					if o == "improved" {
						better = append(better, l)
					}
				}
				sort.Strings(better)
				if len(better) > 0 {
					verdict = VerdictWorkload
					res.Scope = better
					reasons = append(reasons, "no overall difference, but better for "+strings.Join(better, ", ")+" with no language worse")
				} else {
					verdict = VerdictInconclusive
					reasons = append(reasons, fmt.Sprintf("no difference beyond chance (%s); the paired test needs at least %d tasks all better and none worse", primary.Why, MinTasksToShow()))
				}
			}
		}
		// A candidate must beat every simpler alternative it is set against.
		if a.Role == "candidate" && Promotable(verdict) {
			against := a.Against
			if len(against) == 0 {
				for _, o := range e.Arms {
					if o.Role == "simpler" {
						against = append(against, o.Name)
					}
				}
			}
			for _, s := range against {
				if sc := cards[s]; sc == nil || sc.Overall.Runs == 0 {
					continue
				}
				// On the candidate's metric: the question is whether the
				// candidate's change does more than the simpler one.
				v := compare(metric, s, a.Name, split(on))
				res.VsSimpler[s] = v
				if v.Outcome == "improved" {
					continue
				}
				if i, ok := results[s]; ok && Promotable(d.Arms[i].Verdict) {
					verdict = VerdictPreferSimpler
					reasons = append(reasons, fmt.Sprintf("the simpler %s does as well (%s): prefer it", s, v.Why))
				} else {
					verdict = VerdictReject
					reasons = append(reasons, fmt.Sprintf("not better than the simpler %s (%s)", s, v.Why))
				}
				res.Scope = nil
				break
			}
		}
		res.Verdict, res.Reasons = verdict, reasons
		d.Arms = append(d.Arms, res)
		results[a.Name] = len(d.Arms) - 1
	}
	if len(d.HeldoutLanguages) < minLangs {
		d.Notes = append(d.Notes, fmt.Sprintf("held-out runs cover %d languages; fewer than %d", len(d.HeldoutLanguages), minLangs))
	}
	return d, nil
}

// primaryRule is the policy's overall rule for the metric, or a default:
// a Wilson test for a rate, a relative one for a figure.
func primaryRule(m Metric, p *benchmark.GatePolicy) benchmark.GateRule {
	for _, r := range p.Rules {
		if r.Metric == m.Name && (r.Scope == "" || r.Scope == "overall") {
			r.Better = m.Better
			return r
		}
	}
	if strings.HasSuffix(m.Name, "_rate") {
		return benchmark.GateRule{Metric: m.Name, Better: m.Better, Test: "wilson", Tolerance: 0.03, MinFlips: 2}
	}
	return benchmark.GateRule{Metric: m.Name, Better: m.Better, Test: "relative", Tolerance: 0.10}
}

// strictLanguages makes per-language and per-split rules blocking: a
// change that helps one language at another's expense is not accepted.
func strictLanguages(rules []benchmark.GateRule) []benchmark.GateRule {
	out := append([]benchmark.GateRule(nil), rules...)
	for i := range out {
		if out[i].Scope == "language" || out[i].Scope == "split" {
			out[i].Blocking = true
		}
	}
	return out
}

var resourceMetrics = []string{"tokens_per_success", "median_seconds", "p90_seconds", "model_calls_per_run", "peak_server_mib"}

// costRegressions lists resource metrics that regressed beyond what the
// experiment allows.
func costRegressions(g benchmark.GateResult, e *Experiment, m Metric) string {
	var out []string
	for _, v := range g.Verdicts {
		if v.Outcome != "regressed" || !contains(resourceMetrics, v.Rule) || v.Rule == m.Name {
			continue
		}
		if limit, ok := e.AllowCost[v.Rule]; ok && v.Baseline > 0 && v.Cand/v.Baseline <= limit {
			continue
		}
		out = append(out, fmt.Sprintf("%s %.4g against %.4g", v.Rule, v.Cand, v.Baseline))
	}
	return strings.Join(out, ", ")
}

func regressions(g benchmark.GateResult, blockingOnly bool) string {
	var out []string
	for _, v := range g.Verdicts {
		if v.Outcome == "regressed" && (v.Blocking || !blockingOnly) {
			out = append(out, fmt.Sprintf("%s (%s): %s", v.Rule, v.Key, v.Why))
		}
	}
	return strings.Join(out, "; ")
}

func langsWithRuns(s *benchmark.Scorecard) map[string]int {
	out := map[string]int{}
	for l, m := range s.ByLanguage {
		if m.Runs > 0 {
			out[l] = m.Runs
		}
	}
	return out
}

func without(s, drop []string) []string {
	var out []string
	for _, x := range s {
		if !contains(drop, x) {
			out = append(out, x)
		}
	}
	return out
}

// checkPredictions compares an arm's predicted fixes and risks with the
// per-task pass rates over all its runs.
func checkPredictions(a Arm, base, cand *benchmark.Scorecard) *PredictionCheck {
	pc := &PredictionCheck{}
	rateOf := func(t benchmark.TaskTally) float64 {
		if t.Runs == 0 {
			return -1
		}
		return float64(t.Passed) / float64(t.Runs)
	}
	for _, id := range sortedKeys(cand.Tasks) {
		b, c := rateOf(base.Tasks[id]), rateOf(cand.Tasks[id])
		if b < 0 || c < 0 {
			continue
		}
		switch {
		case contains(a.Predicts.Fixes, id) && c > b:
			pc.Fixed = append(pc.Fixed, id)
		case contains(a.Predicts.Fixes, id) && c < b:
			pc.Worsened = append(pc.Worsened, id)
		case contains(a.Predicts.Fixes, id):
			pc.NotFixed = append(pc.NotFixed, id)
		case contains(a.Predicts.AtRisk, id) && c < b:
			pc.Broke = append(pc.Broke, id)
		case c < b:
			pc.Unforeseen = append(pc.Unforeseen, id)
		}
	}
	return pc
}

// WriteDecisionMarkdown writes a decision as tables.
func WriteDecisionMarkdown(w io.Writer, d *Decision) {
	other := "dev"
	if d.DecidedOn == "dev" {
		other = "held-out"
	}
	fmt.Fprintf(w, "# Decision: %s\n\nDecided on %s tasks by %s (%s is better). Languages of those runs: %s.\n\n", d.Experiment, d.DecidedOn, d.Primary.Name, d.Primary.Better, strings.Join(d.HeldoutLanguages, ", "))
	fmt.Fprintf(w, "| Arm | Role | Verdict | Runs | Success | Primary (%s) | Primary (%s) | Smallest detectable success difference |\n", d.DecidedOn, other)
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|")
	for _, a := range d.Arms {
		runs, succ := 0, ""
		if a.Heldcard != nil {
			o := a.Heldcard.Overall
			runs = o.Runs
			succ = fmt.Sprintf("%.0f%% (%.0f–%.0f%%)", 100*o.SuccessRate, 100*o.SuccessCI90[0], 100*o.SuccessCI90[1])
		}
		prim, dev := "–", "–"
		if a.Role != "baseline" {
			prim = fmt.Sprintf("%.4g → %.4g, %s", a.Decided.Baseline, a.Decided.Cand, a.Decided.Outcome)
			if p := a.Paired; p != nil {
				prim = fmt.Sprintf("%.4g → %.4g; paired: %d better, %d worse (p=%.3f), **%s**", a.Decided.Baseline, a.Decided.Cand, p.Better, p.Worse, p.P, p.Outcome)
			}
			if a.Other != nil {
				dev = fmt.Sprintf("%.4g → %.4g, %s", a.Other.Baseline, a.Other.Cand, a.Other.Outcome)
			}
		}
		mde := "–"
		if a.MinDetectable > 0 {
			mde = fmt.Sprintf("%.0f points", 100*a.MinDetectable)
		}
		fmt.Fprintf(w, "| %s | %s | **%s** | %d | %s | %s | %s | %s |\n", a.Arm, a.Role, a.Verdict, runs, succ, prim, dev, mde)
	}
	for _, a := range d.Arms {
		if a.Role == "baseline" {
			continue
		}
		fmt.Fprintf(w, "\n## %s: %s\n\n", a.Arm, a.Verdict)
		for _, r := range a.Reasons {
			fmt.Fprintf(w, "- %s\n", r)
		}
		if len(a.Scope) > 0 {
			fmt.Fprintf(w, "- Scope: %s\n", strings.Join(a.Scope, ", "))
		}
		for _, s := range sortedKeys(a.VsSimpler) {
			v := a.VsSimpler[s]
			fmt.Fprintf(w, "- Against the simpler %s: %s (%s)\n", s, v.Outcome, v.Why)
		}
		if len(a.Languages) > 0 {
			var parts []string
			for _, l := range sortedKeys(a.Languages) {
				parts = append(parts, l+" "+a.Languages[l])
			}
			fmt.Fprintf(w, "- %s by language, paired over its %s tasks: %s\n", a.Metric.Name, d.DecidedOn, strings.Join(parts, ", "))
		}
		if len(a.WithoutLanguage) > 0 {
			var parts []string
			for _, l := range sortedKeys(a.WithoutLanguage) {
				parts = append(parts, "without "+l+": "+a.WithoutLanguage[l])
			}
			fmt.Fprintf(w, "- %s\n", strings.Join(parts, ", "))
		}
		if p := a.Predictions; p != nil && (len(p.Fixed)+len(p.NotFixed)+len(p.Worsened)+len(p.Broke)+len(p.Unforeseen) > 0) {
			fmt.Fprintf(w, "- Predictions: fixed %v, not fixed %v, **worse instead** %v; predicted risks that broke %v; unforeseen drops %v\n", p.Fixed, p.NotFixed, p.Worsened, p.Broke, p.Unforeseen)
		}
		if r := regressions(a.Gate, false); r != "" {
			fmt.Fprintf(w, "- Gate regressions: %s\n", r)
		}
	}
	for _, n := range d.Notes {
		fmt.Fprintf(w, "\nNote: %s\n", n)
	}
}
