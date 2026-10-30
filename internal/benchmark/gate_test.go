package benchmark

import (
	"fmt"
	"testing"
)

// card builds a scorecard of trials, each a map task -> passed.
func card(model string, trials []map[string]bool, secFail int, tokens float64) *Scorecard {
	sc := &Scorecard{Suite: "direct", Model: map[string]any{"sha256": model}, TaskSet: "t1", Settings: map[string]any{"timeout": "20m"},
		ByLanguage: map[string]Metrics{}, ByGroup: map[string]Metrics{}, BySplit: map[string]Metrics{}, Tasks: map[string]TaskTally{}}
	for _, tr := range trials {
		var m Metrics
		for id, ok := range tr {
			r := run{id: id, passed: ok, tokens: tokens, seconds: 30, langs: []string{"go"}}
			if id == "sec" {
				r.security = &SecurityOutcome{}
				for range secFail {
					r.security.Failures = append(r.security.Failures, "leak")
				}
			}
			sc.add(r)
			m.add(r)
		}
		m.finish()
		sc.PerTrial = append(sc.PerTrial, m)
	}
	sc.Overall.finish()
	for k, v := range sc.ByLanguage {
		v.finish()
		sc.ByLanguage[k] = v
	}
	sc.Trials = len(trials)
	return sc
}

func trialsOf(n, pass int, flaky []string, flakyPass bool) map[string]bool {
	m := map[string]bool{}
	for i := range n {
		m[fmt.Sprintf("t%02d", i)] = i < pass
	}
	for _, f := range flaky {
		m[f] = flakyPass
	}
	m["sec"] = true
	return m
}

func TestGate(t *testing.T) {
	p, err := LoadGatePolicy("../../benchmarks/gate-policy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	base := card("m1", []map[string]bool{trialsOf(40, 38, []string{"flaky"}, true), trialsOf(40, 38, []string{"flaky"}, false), trialsOf(40, 38, []string{"flaky"}, true)}, 0, 1000)

	// Noise: the flaky task and one more failure is not a regression.
	noisy := card("m1", []map[string]bool{trialsOf(40, 37, []string{"flaky"}, false), trialsOf(40, 38, []string{"flaky"}, false)}, 0, 1050)
	if g := Gate(base, noisy, p); g.Failed {
		t.Fatalf("noise failed the gate: %+v", g.Verdicts)
	}

	// Two tasks that always passed now always fail: a regression.
	worse := card("m1", []map[string]bool{trialsOf(40, 36, []string{"flaky"}, true), trialsOf(40, 36, []string{"flaky"}, true)}, 0, 1000)
	if g := Gate(base, worse, p); !g.Failed {
		t.Fatalf("consistent flips passed: %+v", g.Verdicts)
	}

	// Any new security failure blocks.
	leak := card("m1", []map[string]bool{trialsOf(40, 38, []string{"flaky"}, true), trialsOf(40, 38, []string{"flaky"}, true)}, 1, 1000)
	g := Gate(base, leak, p)
	if !g.Failed {
		t.Fatal("a security failure passed")
	}

	// Tokens worse by 40%: a warning, not a failure (the rule does not block).
	costly := card("m1", []map[string]bool{trialsOf(40, 38, []string{"flaky"}, true), trialsOf(40, 38, []string{"flaky"}, true)}, 0, 1400)
	g = Gate(base, costly, p)
	warned := false
	for _, v := range g.Verdicts {
		if v.Rule == "tokens_per_success" && v.Outcome == "regressed" && !v.Blocking {
			warned = true
		}
	}
	if g.Failed || !warned {
		t.Fatalf("tokens: failed=%v verdicts %+v", g.Failed, g.Verdicts)
	}

	// A run of fewer tasks (--only) is not comparable, although the
	// definitions' hash is the same.
	part := card("m1", []map[string]bool{trialsOf(30, 30, []string{"flaky"}, true), trialsOf(30, 30, []string{"flaky"}, true)}, 0, 1000)
	if g := Gate(base, part, p); g.Comparable || !g.Failed {
		t.Fatalf("a subset compared: %+v", g)
	}

	// Another model file is not comparable.
	other := card("m2", []map[string]bool{trialsOf(40, 38, nil, true)}, 0, 1000)
	if g := Gate(base, other, p); g.Comparable || !g.Failed {
		t.Fatalf("different models compared: %+v", g)
	}
}

func TestScorecardFromReports(t *testing.T) {
	sc, err := BuildScorecard([]string{"../../benchmarks/reports/20261029-direct-p9-base-r1.json", "../../benchmarks/reports/20261029-direct-p9-base-r2.json"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sc.Suite != "direct" || sc.Trials != 2 || sc.Overall.Runs != 52 || sc.Overall.Passed != 51 || sc.Overall.SuccessCI90[0] <= 0.8 {
		t.Fatalf("scorecard: %+v", sc.Overall)
	}
	if sc.Overall.MedianSeconds <= 0 || sc.Overall.P95Seconds < sc.Overall.MedianSeconds || sc.Overall.TokensPerSuccess <= 0 {
		t.Fatalf("figures: %+v", sc.Overall)
	}
	if _, ok := sc.ByLanguage["go"]; !ok {
		t.Fatal("no per-language figures")
	}
	b, err := BuildScorecard([]string{"../../benchmarks/reports/20261029-tasks-p8.json"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if b.Suite != "bounded" || b.Overall.FalseAcceptance != 2 || b.Overall.Runs != 19 {
		t.Fatalf("bounded: %+v", b.Overall)
	}
}
