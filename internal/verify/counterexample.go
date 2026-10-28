package verify

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/policy"
)

// Counterexamples (Layer 7, optional). An independent verifier (a model
// that sees the request and the diff, not the author's conversation)
// proposes tests meant to expose a wrong change. A proposal is a claim, not
// proof: each test is run under containment, on the change and on the
// base, and only what the runs show is recorded.
//
//   - passes on the change: no counterexample;
//   - fails on the change and passes on the base: a regression the change
//     introduced (strong);
//   - fails on both, or cannot run on the base: the change does not do what
//     the test expects; the expectation may itself be wrong (weak);
//   - does not compile or load: invalid, discarded.

// Counterexample outcomes.
const (
	CounterNone       = "none found"
	CounterRegression = "regression"
	CounterFailing    = "fails on the change"
	CounterInvalid    = "invalid"
)

// CounterexampleTest is one proposed test and what it showed.
type CounterexampleTest struct {
	File    string `json:"file"`
	Outcome string `json:"outcome"`
	Stage   string `json:"stage,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// Counterexample check states.
const (
	CounterCompleted    = "completed"    // ran, none found
	CounterFound        = "found"        // at least one test fails on the change
	CounterInconclusive = "inconclusive" // every proposed test was invalid
	CounterUnavailable  = "unavailable"
	CounterNotRun       = "not run"
)

// CounterexampleReport is a repository's check.
type CounterexampleReport struct {
	Status     string               `json:"status"`
	Tests      []CounterexampleTest `json:"tests,omitempty"`
	Reason     string               `json:"reason"`
	Generator  string               `json:"generator,omitempty"` // who proposed the tests
	DurationMS int64                `json:"duration_ms"`
}

// Counterexamples runs proposed test files (repository-relative path →
// content) on the change and on the base.
func (e *Engine) Counterexamples(ctx context.Context, t RepoTarget, proposed map[string]string) (CounterexampleReport, error) {
	start := time.Now()
	rep := CounterexampleReport{}
	finish := func(status, reason string) (CounterexampleReport, error) {
		rep.Status, rep.Reason, rep.DurationMS = status, reason, time.Since(start).Milliseconds()
		return rep, nil
	}
	if t.Base == "" {
		return finish(CounterUnavailable, "no base commit")
	}
	files := map[string]string{}
	for p, body := range proposed {
		clean := path.Clean(strings.ReplaceAll(p, "\\", "/"))
		switch {
		case path.IsAbs(clean) || strings.HasPrefix(clean, "../") || clean == "..":
			rep.Tests = append(rep.Tests, CounterexampleTest{File: p, Outcome: CounterInvalid, Detail: "outside the repository"})
		case !IsTestFile(clean) || policy.IsProtectedPath(clean) || policy.IsSecretPath(clean):
			rep.Tests = append(rep.Tests, CounterexampleTest{File: p, Outcome: CounterInvalid, Detail: "not a test file of the project"})
		default:
			if _, err := os.Stat(filepath.Join(t.Worktree, filepath.FromSlash(clean))); err == nil {
				rep.Tests = append(rep.Tests, CounterexampleTest{File: p, Outcome: CounterInvalid, Detail: "would replace an existing file"})
				continue
			}
			files[clean] = body
		}
	}
	if len(files) == 0 {
		return finish(CounterInconclusive, "no proposed test could be run")
	}
	changed, err := gitops.ChangedFiles(ctx, t.Worktree, t.Base)
	if err != nil {
		return rep, err
	}
	cfg, err := LoadConfig(ctx, t.Worktree, t.Base)
	if err != nil {
		return rep, err
	}
	root, err := os.MkdirTemp(e.scratchRoot(), "counterexample-")
	if err != nil {
		return rep, err
	}
	defer os.RemoveAll(root)
	tree := func(name string, withChange bool) (RepoTarget, error) {
		dir := filepath.Join(root, name)
		if err := gitops.ExportTree(ctx, t.Worktree, t.Base, dir); err != nil {
			return RepoTarget{}, err
		}
		if withChange {
			var present []string
			for _, f := range changed {
				if _, err := os.Stat(filepath.Join(t.Worktree, filepath.FromSlash(f))); err == nil {
					present = append(present, f)
				} else {
					_ = os.Remove(filepath.Join(dir, filepath.FromSlash(f)))
				}
			}
			if err := overlay(t.Worktree, dir, present); err != nil {
				return RepoTarget{}, err
			}
		}
		if err := e.linkDeps(t.Source, dir); err != nil {
			return RepoTarget{}, err
		}
		return RepoTarget{Name: t.Name, Worktree: dir, TaskID: t.TaskID, Source: t.Source}, nil
	}
	cand, err := tree("change", true)
	if err != nil {
		return rep, err
	}
	var base *RepoTarget
	found, valid := false, 0
	// Each proposed test runs alone, so one that does not compile does not
	// take the others down with it.
	place := func(dir, f string) (func(), error) {
		p := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, []byte(files[f]), 0o644); err != nil {
			return nil, err
		}
		return func() { _ = os.Remove(p) }, nil
	}
	for _, f := range sortedKeys(files) {
		var stages []Stage
		for _, st := range cfg.Stages {
			if st.Scope != "targeted" && StageKind(st) == KindTest && stageCovers(st, f) {
				stages = append(stages, st)
			}
		}
		if len(stages) == 0 {
			rep.Tests = append(rep.Tests, CounterexampleTest{File: f, Outcome: CounterInvalid, Detail: "no test stage runs it"})
			continue
		}
		pkgs := []string{"./" + path.Dir(f)}
		ct := CounterexampleTest{File: f, Outcome: CounterNone}
		undo, err := place(cand.Worktree, f)
		if err != nil {
			return rep, err
		}
		for _, st := range stages {
			r, out := e.runStageFull(ctx, cand, st, stagePackagesFor(st, pkgs))
			if r.Status == "error" || r.Status == "skipped" || r.Cause == CauseTimeout {
				ct = CounterexampleTest{File: f, Outcome: CounterInvalid, Stage: st.Name, Detail: "could not run: " + firstLine(r.Output)}
				break
			}
			if r.Status != "fail" {
				continue
			}
			if compileFailRE.MatchString(out) || goBuildFailed(out) {
				ct = CounterexampleTest{File: f, Outcome: CounterInvalid, Stage: st.Name, Detail: "does not compile or load: " + firstLine(failureLine(out, r.Output))}
				break
			}
			// Fails on the change: does it pass on the base?
			if base == nil {
				b, err := tree("base", false)
				if err != nil {
					return rep, err
				}
				base = &b
			}
			undoBase, err := place(base.Worktree, f)
			if err != nil {
				undo()
				return rep, err
			}
			br, bout := e.runStageFull(ctx, *base, st, stagePackagesFor(st, pkgs))
			undoBase()
			detail := firstLine(failureLine(out, r.Output))
			switch {
			case br.Status == "pass":
				ct = CounterexampleTest{File: f, Outcome: CounterRegression, Stage: st.Name, Detail: "fails on the change, passes on the base: " + detail}
			case br.Status == "fail" && (compileFailRE.MatchString(bout) || goBuildFailed(bout)):
				ct = CounterexampleTest{File: f, Outcome: CounterFailing, Stage: st.Name, Detail: "fails on the change (cannot run on the base): " + detail}
			default:
				ct = CounterexampleTest{File: f, Outcome: CounterFailing, Stage: st.Name, Detail: "fails on the change and on the base (the expectation may be wrong): " + detail}
			}
			break
		}
		undo()
		if ct.Outcome != CounterInvalid {
			valid++
		}
		found = found || ct.Outcome == CounterRegression || ct.Outcome == CounterFailing
		rep.Tests = append(rep.Tests, ct)
	}
	switch {
	case found:
		return finish(CounterFound, "a proposed test fails on the change (run under containment; a generated test's expectation can be wrong)")
	case valid == 0:
		return finish(CounterInconclusive, "no proposed test could be run")
	}
	return finish(CounterCompleted, fmt.Sprintf("%d proposed test(s) ran and pass on the change", valid))
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
