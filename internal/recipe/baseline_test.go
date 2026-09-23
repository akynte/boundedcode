package recipe

import (
	"reflect"
	"testing"
)

func failing(name string, tests map[string]Status, findings []Finding, exit int) Result {
	return Result{
		Recipe: name, Kind: KindTest, Status: Fail, ExitCode: exit, Candidate: "c1",
		Summary: Summary{Tests: tests, Findings: findings, Headline: "failed"},
	}
}

func passing(name string) Result {
	return Result{Recipe: name, Kind: KindTest, Status: Pass, Candidate: "c1",
		Summary: Summary{Headline: "ok"}}
}

func baselineOf(preset string, r Result, root string) BaselineEntry {
	ids, ok, note := NormalizeFailures(root, r)
	return BaselineEntry{
		Preset: preset, Status: r.Status, ExitCode: r.ExitCode,
		Failures: ids, Normalizable: ok, Note: note,
		Image: "img@sha256:aa", Command: "cmd", Env: "env",
	}
}

// The five semantics the mode is defined by, plus the two subset cases.
func TestBaselineRelativeVerdicts(t *testing.T) {
	tests := func(names ...string) map[string]Status {
		m := map[string]Status{}
		for _, n := range names {
			m[n] = Fail
		}
		return m
	}

	for name, tc := range map[string]struct {
		base, cand Result
		want       Verdict
		wantNew    []string
	}{
		"baseline pass, candidate pass": {
			passing("p"), passing("p"), VerdictPass, nil,
		},
		"baseline pass, candidate fail": {
			passing("p"), failing("p", tests("TestA"), nil, 1), VerdictRegression, []string{"test:TestA"},
		},
		"baseline fail A, candidate fail A": {
			failing("p", tests("TestA"), nil, 1), failing("p", tests("TestA"), nil, 1),
			VerdictBaselinePreserved, nil,
		},
		"baseline fail A, candidate pass": {
			failing("p", tests("TestA"), nil, 1), passing("p"), VerdictImprovement, nil,
		},
		"baseline fail A, candidate fail A and B": {
			failing("p", tests("TestA"), nil, 1), failing("p", tests("TestA", "TestB"), nil, 1),
			VerdictRegression, []string{"test:TestB"},
		},
		"baseline fail A and B, candidate fail A": {
			failing("p", tests("TestA", "TestB"), nil, 1), failing("p", tests("TestA"), nil, 1),
			VerdictBaselinePreserved, nil,
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := Compare("", baselineOf("p", tc.base, ""), tc.cand)
			if got.Verdict != tc.want {
				t.Errorf("verdict = %q, want %q (limitation: %s)", got.Verdict, tc.want, got.Limitation)
			}
			if tc.wantNew != nil && !reflect.DeepEqual(got.NewFailures, tc.wantNew) {
				t.Errorf("new failures = %v, want %v", got.NewFailures, tc.wantNew)
			}
			if tc.want != VerdictRegression && len(got.NewFailures) > 0 {
				t.Errorf("a non-regression reported new failures: %v", got.NewFailures)
			}
		})
	}
}

// The same failure, described with a different path, a different line, a
// different duration and a different address, is the same failure.
func TestNormalizationIgnoresUnstableOutput(t *testing.T) {
	root := "/work/run-1"
	base := failing("lint", nil, []Finding{{
		File: "/work/run-1/pkg/a.go", Line: 12, Rule: "E741",
		Message: "/work/run-1/pkg/a.go:12:4: ambiguous name at 0x1f4a took 1.20s",
	}}, 1)
	entry := baselineOf("lint", base, root)

	// Another run: the tree moved, a line was inserted above, the run was
	// quicker and the object landed elsewhere.
	cand := failing("lint", nil, []Finding{{
		File: "/work/run-2/pkg/a.go", Line: 19, Rule: "E741",
		Message: "/work/run-2/pkg/a.go:19:4: ambiguous name at 0x7ffe took 0.03s",
	}}, 1)

	got := Compare("/work/run-2", entry, cand)
	if got.Verdict != VerdictBaselinePreserved {
		t.Fatalf("verdict = %q, want %q; new = %v", got.Verdict, VerdictBaselinePreserved, got.NewFailures)
	}
	if len(got.NewFailures) != 0 {
		t.Errorf("unstable output produced phantom new failures: %v", got.NewFailures)
	}
}

// A tool that fails without naming anything cannot be compared, and the
// comparison has to say so rather than call it equivalent.
func TestUnnameableFailureRecordsItsLimitation(t *testing.T) {
	base := Result{Recipe: "p", Status: Fail, ExitCode: 1, Candidate: "c1",
		Summary: Summary{Headline: "it did not work"}}
	entry := baselineOf("p", base, "")
	if entry.Normalizable {
		t.Fatal("a failure naming no test and no diagnostic was called normalizable")
	}
	if entry.Note == "" {
		t.Error("the limitation was not recorded")
	}

	cand := Result{Recipe: "p", Status: Fail, ExitCode: 2, Candidate: "c1",
		Summary: Summary{Headline: "it did not work"}}
	got := Compare("", entry, cand)
	if got.Limitation == "" {
		t.Error("the comparison declared equivalence without recording that it could not compare")
	}
	if !contains(got.Limitation, "exit status changed from 1 to 2") {
		t.Errorf("the one comparison still available was not reported: %q", got.Limitation)
	}
}

// A baseline describes an experiment. Change the experiment and it no longer
// applies — silently reusing it would compare against a different runtime.
func TestChangedRuntimeOrCommandInvalidatesTheBaseline(t *testing.T) {
	p := Preset{Name: "p", Kind: KindTest, Argv: []string{"go", "test", "./..."}}
	b := &Baseline{Entries: map[string]BaselineEntry{"p": {
		Preset: "p", Image: "img@sha256:aa", Command: CommandDigest(p), Env: EnvDigest([]string{"A=1"}),
	}}}

	if _, ok := b.Applies(p, "img@sha256:aa", EnvDigest([]string{"A=1"})); !ok {
		t.Fatal("the baseline does not apply to the run that produced it")
	}
	if _, ok := b.Applies(p, "img@sha256:bb", EnvDigest([]string{"A=1"})); ok {
		t.Error("a different runtime image reused the baseline")
	}
	if _, ok := b.Applies(p, "img@sha256:aa", EnvDigest([]string{"A=2"})); ok {
		t.Error("a different environment reused the baseline")
	}
	other := Preset{Name: "p", Kind: KindTest, Argv: []string{"go", "test", "-race", "./..."}}
	if _, ok := b.Applies(other, "img@sha256:aa", EnvDigest([]string{"A=1"})); ok {
		t.Error("a different command reused the baseline")
	}
}

// Acceptance rejects what the candidate broke, and nothing else.
func TestAcceptanceRejectsOnlyRegressions(t *testing.T) {
	presets := []Preset{
		{Name: "lint", Kind: KindLint, Argv: []string{"lint"}},
		{Name: "test", Kind: KindTest, Argv: []string{"test"}},
	}
	img, env := "img@sha256:aa", EnvDigest(nil)
	mk := func(p Preset, r Result) BaselineEntry {
		e := baselineOf(p.Name, r, "")
		e.Image, e.Command, e.Env = img, CommandDigest(p), env
		return e
	}
	// lint was already red; test was green.
	base := &Baseline{Entries: map[string]BaselineEntry{
		"lint": mk(presets[0], failing("lint", nil, []Finding{{File: "a.go", Rule: "E1", Message: "x"}}, 1)),
		"test": mk(presets[1], passing("test")),
	}}

	// The candidate leaves lint exactly as red and keeps the tests green.
	ok, reasons, cmps := CheckAgainstBaseline("", presets, []Result{
		failing("lint", nil, []Finding{{File: "a.go", Rule: "E1", Message: "x"}}, 1),
		passing("test"),
	}, "c1", base, img, env)
	if !ok {
		t.Errorf("a candidate that broke nothing was rejected: %v", reasons)
	}
	if len(cmps) != 2 {
		t.Errorf("got %d comparisons, want one per preset", len(cmps))
	}

	// Now it breaks a test that was passing.
	ok, reasons, _ = CheckAgainstBaseline("", presets, []Result{
		failing("lint", nil, []Finding{{File: "a.go", Rule: "E1", Message: "x"}}, 1),
		failing("test", map[string]Status{"TestZ": Fail}, nil, 1),
	}, "c1", base, img, env)
	if ok {
		t.Error("a candidate that broke a passing test was accepted")
	}
	if len(reasons) != 1 || !contains(reasons[0], "test") {
		t.Errorf("reasons did not name the broken preset: %v", reasons)
	}

	// A missing result is not evidence of anything.
	ok, reasons, _ = CheckAgainstBaseline("", presets, []Result{passing("test")}, "c1", base, img, env)
	if ok {
		t.Error("a candidate missing a preset result was accepted")
	}
	if len(reasons) == 0 || !contains(reasons[0], "no result") {
		t.Errorf("reasons did not name the missing result: %v", reasons)
	}
}

// Without an applicable baseline the absolute rule still applies: a failure
// must not be assumed pre-existing.
func TestNoBaselineFallsBackToAbsolute(t *testing.T) {
	presets := []Preset{{Name: "test", Kind: KindTest, Argv: []string{"test"}}}
	ok, reasons, _ := CheckAgainstBaseline("", presets,
		[]Result{failing("test", map[string]Status{"TestA": Fail}, nil, 1)},
		"c1", nil, "img@sha256:aa", EnvDigest(nil))
	if ok {
		t.Error("a failing preset with no baseline was accepted")
	}
	if len(reasons) == 0 || !contains(reasons[0], "no applicable baseline") {
		t.Errorf("the reason did not say the baseline was missing: %v", reasons)
	}
}

func contains(h, n string) bool {
	return len(n) == 0 || (len(h) >= len(n) && indexOf(h, n) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
