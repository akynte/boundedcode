package verify

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/ecosystem"
	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/lang"
)

// Mutation assessment (Layer 6). Tests that pass show little when they
// would also pass with the change broken. A mutant is the changed code with
// one small semantic change (a relational operator, a bound, a boolean, a
// negated condition, a return value): if the repository's tests fail on it,
// they constrain that code; if they pass, they may not, or the mutant may
// be equivalent to the original. Only lines the change added or modified
// are mutated, so the assessment is about the change, and it is bounded in
// mutants and time.
//
// A relational operator is negated (scored) and also shifted at its bound
// (< to <=): a boundary mutant finds an untested edge, but is often
// equivalent to the original, so it is reported and not scored.
//
// A mutant that does not parse, build or load says nothing about the tests
// and is set aside (invalid), never counted against them; one that times
// out is counted apart. A survivor is a hint, not proof of weak tests (it
// may be equivalent): the assessment informs the verification label
// (Classify), it never rejects a change.
//
// Providers by language (internal/lang Language.Mutation): Go from its
// syntax tree; Python from its own tokenizer, each mutant parsed by its
// own parser; C-like languages (JavaScript, TypeScript, Java, Kotlin, Rust,
// C, C++, C#, Swift, Dart, Scala, Groovy, PHP) from a lexer that never
// touches strings or comments and changes only operators written between
// spaces, booleans and comparison bounds: same types in, same types out.
// Other languages: unavailable, and said so.

// Mutant is one mutation of one file.
type Mutant struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Start    int    `json:"-"` // byte offsets in the file
	End      int    `json:"-"`
	Orig     string `json:"orig"`
	Repl     string `json:"repl"`
	Operator string `json:"operator"`
}

func (m Mutant) String() string {
	return fmt.Sprintf("%s:%d %s → %s (%s)", m.File, m.Line, trunc(m.Orig, 40), trunc(m.Repl, 40), m.Operator)
}

// Mutant outcomes.
const (
	MutantKilled   = "killed"   // a test failed
	MutantSurvived = "survived" // every test passed
	MutantInvalid  = "invalid"  // does not parse, build or load: says nothing
	MutantTimeout  = "timeout"  // the tests did not finish (often a loop the mutant made)
	MutantNotRun   = "not run"  // the time budget was spent
)

// MutantResult is a mutant's outcome.
type MutantResult struct {
	Mutant
	Status     string `json:"status"`
	Stage      string `json:"stage,omitempty"`
	Detail     string `json:"detail,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

// Assessment states.
const (
	MutationEstablished  = "established"  // enough valid mutants, and the tests kill most
	MutationWeak         = "weak"         // enough valid mutants, and many survive
	MutationInconclusive = "inconclusive" // too few valid mutants to say
	MutationUnavailable  = "unavailable"  // no provider, tool or passing baseline
	MutationNotRequired  = "not applicable"
)

// MutationReport is one repository's assessment.
type MutationReport struct {
	Status   string  `json:"status"`
	Score    float64 `json:"score"` // killed / (killed + survived)
	Killed   int     `json:"killed"`
	Survived int     `json:"survived"`
	Invalid  int     `json:"invalid"`
	TimedOut int     `json:"timed_out"`
	NotRun   int     `json:"not_run"`
	// Boundary mutants are reported but not scored (often equivalent).
	Boundary         int            `json:"boundary"`
	BoundaryKilled   int            `json:"boundary_killed"`
	BoundarySurvived int            `json:"boundary_survived"`
	Mutants          []MutantResult `json:"mutants,omitempty"`
	Unavailable      []string       `json:"unavailable,omitempty"` // files or languages with no provider, and why
	Reason           string         `json:"reason"`
	DurationMS       int64          `json:"duration_ms"`
}

// MutationOptions bound an assessment.
type MutationOptions struct {
	MaxMutants int           // default 8
	PerFile    int           // default 4
	Budget     time.Duration // default 10 minutes
	MinValid   int           // valid mutants needed to conclude (default 3)
	Threshold  float64       // killed share for "established" (default 0.6)
}

func (o *MutationOptions) defaults() {
	if o.MaxMutants <= 0 {
		o.MaxMutants = 8
	}
	if o.PerFile <= 0 {
		o.PerFile = 6
	}
	if o.Budget <= 0 {
		o.Budget = 10 * time.Minute
	}
	if o.MinValid <= 0 {
		o.MinValid = 3
	}
	if o.Threshold <= 0 {
		o.Threshold = 0.6
	}
}

// mutantsFor returns the mutation points of a file, or why there are none.
func mutantsFor(ctx context.Context, rel string, src []byte, lines map[int]bool) ([]Mutant, string) {
	first, _, _ := bytes.Cut(src, []byte("\n"))
	l := lang.ForPath(rel, string(first))
	if l == nil {
		return nil, "unknown language"
	}
	switch l.Mutation {
	case "go":
		return goMutants(rel, src, lines), ""
	case "python":
		ms, err := pythonMutants(ctx, rel, src, lines)
		if err != nil {
			return nil, "Python mutation needs python3: " + err.Error()
		}
		return ms, ""
	case "c-family", "c-family-hash":
		return lexMutants(rel, src, lines, l.Mutation == "c-family-hash", l.ID == "rust"), ""
	}
	return nil, "no mutation provider for " + l.Name
}

// changedLines are the lines of rel the change added or modified (all of a
// new file's).
func changedLines(ctx context.Context, worktree, base, rel string) map[int]bool {
	out := map[int]bool{}
	d, err := gitops.Run(ctx, worktree, "diff", "-U0", "--no-color", "--no-ext-diff", base, "--", rel)
	if err != nil || strings.TrimSpace(d) == "" {
		// Untracked (new): every line.
		b, err := os.ReadFile(filepath.Join(worktree, filepath.FromSlash(rel)))
		if err != nil {
			return out
		}
		for i := 1; i <= bytes.Count(b, []byte("\n"))+1; i++ {
			out[i] = true
		}
		return out
	}
	for _, m := range hunkRE.FindAllStringSubmatch(d, -1) {
		start, _ := strconv.Atoi(m[1])
		n := 1
		if m[2] != "" {
			n, _ = strconv.Atoi(m[2])
		}
		for i := start; i < start+n; i++ {
			out[i] = true
		}
	}
	return out
}

var hunkRE = regexp.MustCompile(`(?m)^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// compileFailRE are signs, in a failed test run, that the code did not
// compile or load: the mutant is invalid, not killed.
var compileFailRE = regexp.MustCompile(`(?m)(\[build failed\]|^# \S+\n.*\.go:\d+:\d+:|error TS\d+|SyntaxError|error\[E\d+\]|error: could not compile|COMPILATION ERROR|cannot find symbol|Compilation failed|compileKotlin FAILED|compileJava FAILED|ParseError|PHP Parse error|syntax error, unexpected|IndentationError|ImportError while importing)`)

// Mutation assesses the change's tests by mutating the lines it changed.
func (e *Engine) Mutation(ctx context.Context, t RepoTarget, opt MutationOptions) (MutationReport, error) {
	opt.defaults()
	start := time.Now()
	rep := MutationReport{}
	finish := func(status, reason string) (MutationReport, error) {
		rep.Status, rep.Reason, rep.DurationMS = status, reason, time.Since(start).Milliseconds()
		return rep, nil
	}
	if t.Base == "" {
		return finish(MutationUnavailable, "no base commit")
	}
	changed, err := gitops.ChangedFiles(ctx, t.Worktree, t.Base)
	if err != nil {
		return rep, err
	}
	// Mutation points, a few per file, spread over the files.
	perFile := map[string][]Mutant{}
	var files []string
	for _, f := range changed {
		if IsTestFile(f) || ecosystem.IsDocumentation(f) {
			continue
		}
		src, err := os.ReadFile(filepath.Join(t.Worktree, filepath.FromSlash(f)))
		if err != nil {
			continue // deleted
		}
		ms, why := mutantsFor(ctx, f, src, changedLines(ctx, t.Worktree, t.Base, f))
		if why != "" {
			if why != "unknown language" {
				rep.Unavailable = append(rep.Unavailable, f+": "+why)
			}
			continue
		}
		if len(ms) > 0 {
			perFile[f] = spread(ms, opt.PerFile)
			files = append(files, f)
		}
	}
	sort.Strings(files)
	var mutants []Mutant
	for i := 0; len(mutants) < opt.MaxMutants; i++ {
		took := false
		for _, f := range files {
			if i < len(perFile[f]) && len(mutants) < opt.MaxMutants {
				mutants = append(mutants, perFile[f][i])
				took = true
			}
		}
		if !took {
			break
		}
	}
	if len(mutants) == 0 {
		if len(rep.Unavailable) > 0 {
			return finish(MutationUnavailable, "no mutation provider for the changed code ("+trunc(strings.Join(rep.Unavailable, "; "), 300)+")")
		}
		return finish(MutationNotRequired, "the change has no mutable code (no operators, conditions or returns on its lines)")
	}
	cfg, err := LoadConfig(ctx, t.Worktree, t.Base)
	if err != nil {
		return rep, err
	}
	// The candidate's tree: the base with the change laid over it, in a
	// scratch directory, so nothing touches the task's worktree.
	root, err := os.MkdirTemp(e.scratchRoot(), "mutation-")
	if err != nil {
		return rep, err
	}
	defer os.RemoveAll(root)
	tree := filepath.Join(root, "tree")
	if err := gitops.ExportTree(ctx, t.Worktree, t.Base, tree); err != nil {
		return rep, err
	}
	var present []string
	for _, f := range changed {
		if _, err := os.Stat(filepath.Join(t.Worktree, filepath.FromSlash(f))); err == nil {
			present = append(present, f)
		} else {
			_ = os.Remove(filepath.Join(tree, filepath.FromSlash(f)))
		}
	}
	if err := overlay(t.Worktree, tree, present); err != nil {
		return rep, err
	}
	if err := e.linkDeps(t.Source, tree); err != nil {
		return rep, err
	}
	target := RepoTarget{Name: t.Name, Worktree: tree, TaskID: t.TaskID, Source: t.Source}
	pkgs := []string{"./..."}
	if pk, err := e.goImpactedPackages(ctx, t, changed); err == nil && len(pk) > 0 {
		pkgs = pk
	}
	stagesFor := func(file string, kind string) []Stage {
		var out []Stage
		for _, st := range cfg.Stages {
			if st.Scope == "targeted" || StageKind(st) != kind || !stageCovers(st, file) {
				continue
			}
			out = append(out, st)
		}
		return out
	}
	// The tests must pass on the unmutated candidate, or nothing can be
	// concluded; their durations bound each mutant's run.
	baseline := map[string]time.Duration{}
	for _, f := range files {
		for _, st := range stagesFor(f, KindTest) {
			if _, ok := baseline[st.Name]; ok {
				continue
			}
			t0 := time.Now()
			r, _ := e.runStageFull(ctx, target, st, stagePackagesFor(st, pkgs))
			if r.Status != "pass" {
				return finish(MutationUnavailable, "the tests do not pass on a clean copy of the change ("+st.Name+": "+r.Status+")")
			}
			baseline[st.Name] = time.Since(t0)
		}
	}
	if len(baseline) == 0 {
		return finish(MutationUnavailable, "no test stage runs the changed code")
	}
	for _, m := range mutants {
		if time.Since(start) > opt.Budget || ctx.Err() != nil {
			rep.Mutants = append(rep.Mutants, MutantResult{Mutant: m, Status: MutantNotRun})
			rep.NotRun++
			continue
		}
		mr := e.runMutant(ctx, target, tree, m, stagesFor(m.File, KindBuild), stagesFor(m.File, KindTest), pkgs, baseline)
		rep.Mutants = append(rep.Mutants, mr)
		switch mr.Status {
		case MutantKilled:
			rep.Killed++
		case MutantSurvived:
			rep.Survived++
		case MutantInvalid:
			rep.Invalid++
		case MutantTimeout:
			rep.TimedOut++
		}
	}
	// Boundary mutants (< to <=, a bound + 1) are often equivalent to the
	// original (clamp(v) at v == max returns max either way): reported, not
	// scored.
	for _, m := range rep.Mutants {
		if m.Operator == "boundary" {
			rep.Boundary++
			switch m.Status {
			case MutantKilled:
				rep.Killed--
				rep.BoundaryKilled++
			case MutantSurvived:
				rep.Survived--
				rep.BoundarySurvived++
			}
		}
	}
	valid := rep.Killed + rep.Survived
	if valid > 0 {
		rep.Score = float64(rep.Killed) / float64(valid)
	}
	// Enough valid mutants to conclude counts the boundary ones too (they
	// ran against the tests); the score does not.
	total := valid + rep.BoundaryKilled + rep.BoundarySurvived
	switch {
	case valid == 0 || total < opt.MinValid:
		return finish(MutationInconclusive, fmt.Sprintf("%d valid mutant(s) of %d (at least %d, one of them scored, are needed to conclude)", total, len(mutants), opt.MinValid))
	case rep.Score >= opt.Threshold:
		return finish(MutationEstablished, fmt.Sprintf("the tests kill %d of %d valid mutants of the changed code", rep.Killed, valid))
	}
	return finish(MutationWeak, fmt.Sprintf("the tests kill only %d of %d valid mutants of the changed code; survivors may be equivalent, "+
		"or behaviour the tests do not check", rep.Killed, valid))
}

// spread picks up to n mutants of a file: scored ones first (different
// operators and lines first), then boundary mutants with what is left.
func spread(ms []Mutant, n int) []Mutant {
	var scored, boundary []Mutant
	for _, m := range ms {
		if m.Operator == "boundary" {
			boundary = append(boundary, m)
		} else {
			scored = append(scored, m)
		}
	}
	out := diverse(scored, n)
	return append(out, diverse(boundary, n-len(out))...)
}

func diverse(ms []Mutant, n int) []Mutant {
	var out []Mutant
	usedOp, usedLine := map[string]bool{}, map[int]bool{}
	for pass := 0; pass < 3 && len(out) < n; pass++ {
		for _, m := range ms {
			if len(out) >= n {
				break
			}
			taken := false
			for _, o := range out {
				taken = taken || o.Start == m.Start && o.Repl == m.Repl
			}
			if taken {
				continue
			}
			if pass == 0 && (usedOp[m.Operator] || usedLine[m.Line]) || pass == 1 && usedLine[m.Line] {
				continue
			}
			out = append(out, m)
			usedOp[m.Operator], usedLine[m.Line] = true, true
		}
	}
	return out
}

// runMutant applies one mutant to the tree, checks that it is valid, runs
// the tests and restores the file.
func (e *Engine) runMutant(ctx context.Context, target RepoTarget, tree string, m Mutant, builds, tests []Stage, pkgs []string,
	baseline map[string]time.Duration) MutantResult {
	t0 := time.Now()
	mr := MutantResult{Mutant: m}
	done := func(status, stage, detail string) MutantResult {
		mr.Status, mr.Stage, mr.Detail, mr.DurationMS = status, stage, trunc(detail, 300), time.Since(t0).Milliseconds()
		return mr
	}
	p := filepath.Join(tree, filepath.FromSlash(m.File))
	orig, err := os.ReadFile(p)
	if err != nil {
		return done(MutantInvalid, "", err.Error())
	}
	defer os.WriteFile(p, orig, 0o644) //nolint:errcheck // best effort; the tree is scratch
	if m.Start < 0 || m.End > len(orig) || string(orig[m.Start:m.End]) != m.Orig {
		return done(MutantInvalid, "", "the mutation point moved")
	}
	mutated := append(append(append([]byte{}, orig[:m.Start]...), m.Repl...), orig[m.End:]...)
	if path.Ext(m.File) == ".go" && !goParses(m.File, mutated) {
		return done(MutantInvalid, "", "does not parse")
	}
	if err := os.WriteFile(p, mutated, 0o644); err != nil {
		return done(MutantInvalid, "", err.Error())
	}
	for _, st := range builds {
		r, out := e.runStageFull(ctx, target, st, stagePackagesFor(st, pkgs))
		if r.Status == "fail" || r.Status == "error" {
			return done(MutantInvalid, st.Name, "does not build: "+firstLine(failureLine(out, r.Output)))
		}
	}
	for _, st := range tests {
		run := st
		limit := 3*baseline[st.Name] + 30*time.Second
		if run.Timeout.D() == 0 || run.Timeout.D() > limit {
			run.Timeout = config.Duration(limit)
		}
		r, out := e.runStageFull(ctx, target, run, stagePackagesFor(st, pkgs))
		switch {
		case r.Cause == CauseTimeout:
			return done(MutantTimeout, st.Name, "the tests did not finish within "+limit.Round(time.Second).String())
		case r.Status == "error":
			return done(MutantInvalid, st.Name, firstLine(r.Output))
		case r.Status == "fail" && (compileFailRE.MatchString(out) || goBuildFailed(out)):
			return done(MutantInvalid, st.Name, "does not compile or load: "+firstLine(failureLine(out, r.Output)))
		case r.Status == "fail":
			return done(MutantKilled, st.Name, firstLine(failureLine(out, r.Output)))
		}
	}
	return done(MutantSurvived, "", "every test passed")
}

// stageCovers reports whether a stage tests a file: the file is under its
// directory, and a Go stage tests Go code, another stage the rest.
func stageCovers(st Stage, file string) bool {
	if st.Dir != "" && !strings.HasPrefix(file, st.Dir+"/") {
		return false
	}
	return isGoStage(st) == (path.Ext(file) == ".go")
}

func stagePackagesFor(st Stage, pkgs []string) []string {
	if isGoStage(st) {
		return pkgs
	}
	return []string{"./..."}
}

func lineAt(src []byte, off int) int { return bytes.Count(src[:off], []byte("\n")) + 1 }

func intLit(s string) (int64, bool) {
	v, err := strconv.ParseInt(strings.ReplaceAll(s, "_", ""), 0, 64)
	return v, err == nil
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
