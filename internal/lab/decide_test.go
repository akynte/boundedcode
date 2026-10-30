package lab

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/benchmark"
)

// task is a synthetic task: its split, languages and per-trial results.
type task struct {
	id, split string
	langs     []string
	pass      []bool // one per trial
	seconds   float64
}

// writeReports writes one direct-mode report per trial of tasks.
func writeReports(t *testing.T, dir, arm string, tasks []task, settings map[string]any) []string {
	t.Helper()
	var out []string
	for trial := range len(tasks[0].pass) {
		rep := benchmark.DirectReport{ID: fmt.Sprintf("%s-%d", arm, trial), Settings: settings,
			Environment: benchmark.Environment{Build: "b1", TasksHash: "set1", Model: map[string]any{"sha256": "m1"}}}
		for _, tk := range tasks {
			sec := tk.seconds
			if sec == 0 {
				sec = 30
			}
			rep.Results = append(rep.Results, benchmark.DirectResult{ID: tk.id, Category: "fix", Success: tk.pass[trial], Split: tk.split, Group: "everyday",
				Languages: tk.langs, PromptTokens: 5000, CachedTokens: 4000, GeneratedTokens: 500, WallSeconds: sec, Calls: 6})
		}
		b, _ := json.Marshal(rep)
		p := filepath.Join(dir, fmt.Sprintf("%s-%d.json", arm, trial))
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// suite returns n held-out tasks per language and two dev tasks; tasks
// listed in better pass in every trial, the others fail in every trial
// unless base passes them.
func suite(langs []string, n int, pass func(id, lang string) bool) []task {
	var out []task
	for _, l := range langs {
		for i := range n {
			id := fmt.Sprintf("%s-%d", l, i)
			p := pass(id, l)
			out = append(out, task{id: id, split: "heldout", langs: []string{l}, pass: []bool{p, p}})
		}
	}
	out = append(out, task{id: "dev-a", split: "dev", langs: []string{"go"}, pass: []bool{true, true}}, task{id: "dev-b", split: "dev", langs: []string{"go"}, pass: []bool{true, false}})
	return out
}

func policy(t *testing.T) *benchmark.GatePolicy {
	t.Helper()
	p, err := benchmark.LoadGatePolicy("../../benchmarks/gate-policy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func experiment(arms ...Arm) *Experiment {
	e := &Experiment{ID: "x", Hypothesis: "h", Suite: "direct", Primary: Metric{"success_rate", "higher"}, Trials: 2}
	e.Tasks.Dirs = []string{"benchmarks/direct"}
	e.Arms = append([]Arm{{Name: "base", Role: "baseline"}}, arms...)
	return e
}

func verdictOf(d *Decision, arm string) ArmResult {
	for _, a := range d.Arms {
		if a.Arm == arm {
			return a
		}
	}
	return ArmResult{}
}

func TestDecide(t *testing.T) {
	dir := t.TempDir()
	langs := []string{"go", "python", "rust", "java"}
	// The baseline fails index 0 and 1 of every language.
	baseline := suite(langs, 4, func(id, _ string) bool { return !strings.HasSuffix(id, "-0") && !strings.HasSuffix(id, "-1") })
	reports := map[string][]string{"base": writeReports(t, dir, "base", baseline, nil)}

	// A candidate that fixes the failing tasks in every language: accepted.
	good := suite(langs, 4, func(string, string) bool { return true })
	reports["good"] = writeReports(t, dir, "good", good, nil)
	// One that fixes them in rust only: the improvement is rust's.
	rust := suite(langs, 4, func(id, l string) bool {
		return l == "rust" || !strings.HasSuffix(id, "-0") && !strings.HasSuffix(id, "-1")
	})
	reports["rust"] = writeReports(t, dir, "rust", rust, nil)
	// One that breaks python: rejected, whatever it does elsewhere.
	broke := suite(langs, 4, func(_, l string) bool { return l != "python" })
	reports["broke"] = writeReports(t, dir, "broke", broke, nil)
	// A simpler change that does as well as "good".
	reports["simple"] = writeReports(t, dir, "simple", good, nil)
	// No change at all: inconclusive.
	reports["same"] = writeReports(t, dir, "same", baseline, nil)

	e := experiment(Arm{Name: "good", Role: "candidate", Against: []string{}}, Arm{Name: "rust", Role: "candidate"}, Arm{Name: "broke", Role: "candidate"}, Arm{Name: "same", Role: "candidate"})
	d, err := Decide(e, reports, nil, policy(t))
	if err != nil {
		t.Fatal(err)
	}
	if v := verdictOf(d, "good"); v.Verdict != VerdictAccept || !v.Independent || v.Paired == nil || v.Paired.Better != 8 {
		t.Fatalf("good: %+v", v)
	}
	// Two rust tasks better and none worse is too few for the sign test,
	// overall or within rust: no claim, for rust or anyone.
	if v := verdictOf(d, "rust"); v.Verdict != VerdictInconclusive || v.Languages["rust"] != "inconclusive" {
		t.Fatalf("rust: %+v", v)
	}
	if v := verdictOf(d, "broke"); v.Verdict != VerdictReject {
		t.Fatalf("broke: %+v", v)
	}
	if v := verdictOf(d, "same"); v.Verdict != VerdictInconclusive || !strings.Contains(strings.Join(v.Reasons, " "), "needs at least 5 tasks") {
		t.Fatalf("same: %+v", v)
	}

	// A candidate no better than a simpler alternative that works: prefer
	// the simpler one.
	e2 := experiment(Arm{Name: "simple", Role: "simpler", Set: e.Arms[0].Set}, Arm{Name: "good", Role: "candidate"})
	d2, err := Decide(e2, reports, nil, policy(t))
	if err != nil {
		t.Fatal(err)
	}
	if v := verdictOf(d2, "simple"); v.Verdict != VerdictAccept {
		t.Fatalf("simple: %+v", v)
	}
	if v := verdictOf(d2, "good"); v.Verdict != VerdictPreferSimpler {
		t.Fatalf("good against a simpler equal: %+v", v)
	}

	// Decided on development tasks: never independent, never a proposal.
	e3 := experiment(Arm{Name: "good", Role: "candidate"})
	e3.DecideOn = "dev"
	d3, err := Decide(e3, reports, nil, policy(t))
	if err != nil {
		t.Fatal(err)
	}
	if v := verdictOf(d3, "good"); v.Independent {
		t.Fatalf("dev decision independent: %+v", v)
	}
	ps, err := Proposals(t.TempDir(), e3, d3, nil, nil)
	if err != nil || len(ps) != 0 {
		t.Fatalf("proposals from a dev decision: %v %v", ps, err)
	}
	// The decision is a function of the reports: deciding again gives the
	// same bytes.
	again, _ := Decide(e, reports, nil, policy(t))
	a, _ := json.Marshal(d)
	b, _ := json.Marshal(again)
	if string(a) != string(b) {
		t.Fatal("the decision is not reproducible")
	}
}

// One language's big gain must not make a change global.
func TestDecideOneLanguageDrives(t *testing.T) {
	dir := t.TempDir()
	langs := []string{"go", "python", "rust"}
	base := suite(langs, 6, func(id, l string) bool { return l != "go" })
	cand := suite(langs, 6, func(string, string) bool { return true })
	reports := map[string][]string{"base": writeReports(t, dir, "base", base, nil), "cand": writeReports(t, dir, "cand", cand, nil)}
	d, err := Decide(experiment(Arm{Name: "cand", Role: "candidate"}), reports, nil, policy(t))
	if err != nil {
		t.Fatal(err)
	}
	v := verdictOf(d, "cand")
	if v.Verdict != VerdictWorkload || strings.Join(v.Scope, ",") != "go" || v.WithoutLanguage["go"] != "no gain in the others" || v.WithoutLanguage["rust"] != "the others still gain" {
		t.Fatalf("go-driven gain: %+v", v)
	}
}

// A candidate that costs much more is rejected unless the experiment
// allows the cost.
func TestDecideCost(t *testing.T) {
	dir := t.TempDir()
	langs := []string{"go", "python", "rust"}
	base := suite(langs, 3, func(id, _ string) bool { return !strings.HasSuffix(id, "-0") })
	cand := suite(langs, 3, func(string, string) bool { return true })
	for i := range cand {
		cand[i].seconds = 90 // three times as slow
	}
	reports := map[string][]string{"base": writeReports(t, dir, "base", base, nil), "slow": writeReports(t, dir, "slow", cand, nil)}
	e := experiment(Arm{Name: "slow", Role: "candidate"})
	e.MinLanguages = 3
	d, err := Decide(e, reports, nil, policy(t))
	if err != nil {
		t.Fatal(err)
	}
	if v := verdictOf(d, "slow"); v.Verdict != VerdictReject || !strings.Contains(strings.Join(v.Reasons, " "), "costs more") {
		t.Fatalf("slow: %+v", v)
	}
	e.AllowCost = map[string]float64{"median_seconds": 3.5, "p90_seconds": 3.5}
	d, _ = Decide(e, reports, nil, policy(t))
	if v := verdictOf(d, "slow"); v.Verdict == VerdictReject && strings.Contains(strings.Join(v.Reasons, " "), "costs more") {
		t.Fatalf("allowed cost still rejected: %+v", v)
	}
}

func TestPairedAndSignTest(t *testing.T) {
	if p := signTail(5, 5); p > Alpha || signTail(4, 4) <= Alpha {
		t.Fatalf("sign test: 5/5 %.4f, 4/4 %.4f", signTail(5, 5), signTail(4, 4))
	}
	if MinTasksToShow() != 5 {
		t.Fatalf("min tasks %d", MinTasksToShow())
	}
	var base, cand []benchmark.RunValue
	for i := range 8 {
		k := benchmark.RunKey{Task: fmt.Sprintf("t%d", i), Split: "heldout"}
		base = append(base, benchmark.RunValue{RunKey: k, Seconds: 100, Passed: true})
		cand = append(cand, benchmark.RunValue{RunKey: k, Seconds: 70, Passed: true})
	}
	r := Paired(Metric{"median_seconds", "lower"}, 0.15, base, cand, nil)
	if r.Outcome != "improved" || r.Better != 8 || r.Effect != 0.7 {
		t.Fatalf("faster: %+v", r)
	}
	r = Paired(Metric{"median_seconds", "lower"}, 0.15, cand, base, nil)
	if r.Outcome != "regressed" {
		t.Fatalf("slower: %+v", r)
	}
	if r := Paired(Metric{"success_rate", "higher"}, 0.03, base, base, nil); r.Outcome != "unchanged" || r.Ties != 8 {
		t.Fatalf("same: %+v", r)
	}
}

func TestMinDetectable(t *testing.T) {
	if d := MinDetectable(0.9, 50); d < 0.1 || d > 0.25 {
		t.Fatalf("min detectable at 0.9, n=50: %.3f", d)
	}
	if MinDetectable(0.5, 400) >= MinDetectable(0.5, 40) {
		t.Fatal("more runs should detect smaller differences")
	}
}

// A predicted fix that passes less often is reported as worse, not merely
// as not fixed: the change hurt the task it was written for.
func TestCheckPredictions(t *testing.T) {
	var a Arm
	a.Predicts.Fixes = []string{"loops", "flaky", "helped"}
	a.Predicts.AtRisk = []string{"risky"}
	tally := func(passed int) benchmark.TaskTally { return benchmark.TaskTally{Runs: 2, Passed: passed} }
	base := &benchmark.Scorecard{Tasks: map[string]benchmark.TaskTally{"loops": tally(2), "flaky": tally(1), "helped": tally(0), "risky": tally(2), "other": tally(2)}}
	cand := &benchmark.Scorecard{Tasks: map[string]benchmark.TaskTally{"loops": tally(0), "flaky": tally(1), "helped": tally(2), "risky": tally(1), "other": tally(1)}}
	pc := checkPredictions(a, base, cand)
	got := fmt.Sprint(pc.Fixed, pc.NotFixed, pc.Worsened, pc.Broke, pc.Unforeseen)
	if want := "[helped] [flaky] [loops] [risky] [other]"; got != want {
		t.Fatalf("predictions %s, want %s", got, want)
	}
}
