package verify

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/sandbox"
)

// Behavioural evidence. A green build and green tests do not show that a
// task's requested behaviour exists: in the 2026-10-04 validation, four of
// eight changes passed every configured check and were still wrong, two of
// them doing nothing at all (the existing tests already passed on the base).
// Evidence is a test the change added or modified that fails on the base
// commit and passes on the candidate. Without it a task is "tests_green",
// not "task_verified".

// Evidence is the result of the behavioural-delta check of one repository.
type Evidence struct {
	Verified  bool     `json:"verified"`
	TestFiles []string `json:"test_files,omitempty"` // changed test files considered
	Tests     []string `json:"tests,omitempty"`      // tests that fail on base and pass on the candidate
	Reason    string   `json:"reason"`
}

var testFileRE = regexp.MustCompile(`(^|/)[^/]*(_test\.go|\.(test|spec)\.[cm]?[jt]sx?)$`)

// IsTestFile reports whether a workspace-relative path is test code or test
// data: a conventional test file, or anything under a directory that marks
// tests (test, tests, testdata, spec, __tests__, or a name such as caddytest,
// test_utils or e2e-tests). A directory name is split on "-", "_" and ".";
// a part marks tests when it starts or ends with "test" and is not an
// ordinary word that happens to (latest, contest, ...).
func IsTestFile(p string) bool {
	if testFileRE.MatchString(p) {
		return true
	}
	parts := strings.Split(filepath.ToSlash(p), "/")
	for _, d := range parts[:len(parts)-1] {
		if testDirName(d) {
			return true
		}
	}
	return false
}

// notTestWords end (or start) with "test" but name no tests.
var notTestWords = map[string]bool{
	"latest": true, "greatest": true, "contest": true, "contests": true, "protest": true,
	"attest": true, "attests": true, "detest": true, "testament": true, "testimony": true,
	"fastest": true, "shortest": true, "smartest": true, "strictest": true, "lightest": true,
	"tightest": true, "brightest": true, "softest": true, "hottest": true, "slightest": true,
}

func testDirName(d string) bool {
	l := strings.ToLower(d)
	switch l {
	case "spec", "specs", "__tests__", "__mocks__", "e2e", "fixtures":
		return true
	}
	for _, part := range strings.FieldsFunc(l, func(r rune) bool { return r == '-' || r == '_' || r == '.' }) {
		if notTestWords[part] {
			continue
		}
		if strings.HasPrefix(part, "test") || strings.HasSuffix(part, "test") || strings.HasSuffix(part, "tests") {
			return true
		}
	}
	return false
}

// isTestStage reports whether a verification stage runs tests.
func isTestStage(st Stage) bool {
	if st.Tests {
		return true
	}
	if strings.Contains(strings.ToLower(st.Name), "test") {
		return true
	}
	for _, a := range st.Run {
		if strings.Contains(a, "test") {
			return true
		}
	}
	return false
}

var goFailRE = regexp.MustCompile(`(?m)^\s*--- FAIL: (Test[A-Za-z0-9_]*)`)

// BehaviourEvidence checks whether the change's own tests demonstrate it:
// the changed test files (code and data) are laid over an export of the base
// commit, and the repository's test stages run there and on the untouched
// base (control). A test counts when it fails with the changed tests and not
// without them. For Go, failures are compared per test function, in the
// packages whose test code or test data changed. Other stages have no per-test
// names: the stage must fail with the changed tests and pass without them.
//
// A test that cannot be observed on the base does not count: one that does
// not compile or load there (it uses code the change adds) shows that the API
// exists, not that it behaves as requested, and a timeout shows nothing.
func (e *Engine) BehaviourEvidence(ctx context.Context, t RepoTarget) (Evidence, error) {
	var ev Evidence
	if t.Base == "" {
		ev.Reason = "no base commit"
		return ev, nil
	}
	changed, err := gitops.ChangedFiles(ctx, t.Worktree, t.Base)
	if err != nil {
		return ev, err
	}
	for _, f := range changed {
		if IsTestFile(f) {
			ev.TestFiles = append(ev.TestFiles, f)
		}
	}
	if len(ev.TestFiles) == 0 {
		ev.Reason = "the change adds or modifies no tests"
		return ev, nil
	}
	cfg, err := LoadConfig(ctx, t.Worktree, t.Base)
	if err != nil {
		return ev, err
	}
	var stages []Stage
	for _, st := range cfg.Stages {
		if st.Scope != "targeted" && isTestStage(st) {
			stages = append(stages, st)
		}
	}
	if len(stages) == 0 {
		ev.Reason = "no test stage is configured"
		return ev, nil
	}
	root, err := os.MkdirTemp(e.scratchRoot(), "evidence-")
	if err != nil {
		return ev, err
	}
	defer os.RemoveAll(root)
	base := filepath.Join(root, "base")
	if err := gitops.ExportTree(ctx, t.Worktree, t.Base, base); err != nil {
		return ev, err
	}
	if err := overlay(t.Worktree, base, ev.TestFiles); err != nil {
		return ev, err
	}
	control := filepath.Join(root, "control")
	controlReady := false
	ensureControl := func() error {
		if controlReady {
			return nil
		}
		if err := gitops.ExportTree(ctx, t.Worktree, t.Base, control); err != nil {
			return err
		}
		controlReady = true
		return e.linkDeps(t.Source, control)
	}
	if err := e.linkDeps(t.Source, base); err != nil {
		return ev, err
	}

	// Go packages whose test code or test data changed. A failing test
	// counts if a changed test file defines it; when test data changed, any
	// test in the packages that read it can (the data names no functions).
	pkgSet := map[string]bool{}
	goTests := map[string]bool{}
	goData := false
	nonGoTests := false
	for _, f := range ev.TestFiles {
		if strings.HasSuffix(f, "_test.go") {
			pkgSet[goPackageArg(filepath.Dir(f))] = true
			if b, err := os.ReadFile(filepath.Join(t.Worktree, f)); err == nil {
				for _, m := range goTestFuncRE.FindAllStringSubmatch(string(b), -1) {
					goTests[m[1]] = true
				}
			}
			continue
		}
		if strings.HasSuffix(f, ".go") && !underTestdata(f) {
			continue // test helpers are compiled into the packages that import them
		}
		nonGoTests = nonGoTests || !strings.HasSuffix(f, ".go")
		if dir, ok := goTestDataPackage(t.Worktree, f); ok {
			pkgSet[goPackageArg(dir)] = true
			goData = true
		}
	}
	packages := make([]string, 0, len(pkgSet))
	for p := range pkgSet {
		packages = append(packages, p)
	}
	sort.Strings(packages)

	ran, notBuilt, timedOut := false, false, false
	envErr := "" // a stage that could not run (sandbox or engine problem)
	seen := map[string]bool{}
	controlTarget := RepoTarget{Name: t.Name, Worktree: control, TaskID: t.TaskID, Source: t.Source}
	baseTarget := RepoTarget{Name: t.Name, Worktree: base, TaskID: t.TaskID, Source: t.Source}
	for _, st := range stages {
		goStage := len(st.Requires) > 0 && st.Requires[0] == "go.mod"
		if goStage && len(packages) == 0 {
			continue // no Go test code or data changed
		}
		if !goStage && !nonGoTests {
			continue // only Go tests changed
		}
		pkgs := packages
		if !goStage {
			pkgs = []string{"./..."}
		}
		r, out := e.runStageFull(ctx, baseTarget, st, pkgs)
		if r.Status == "error" && envErr == "" {
			envErr = st.Name + ": " + firstLine(r.Output)
		}
		if r.Status == "skipped" || r.Status == "error" {
			continue
		}
		ran = true
		if stageTimedOut(r) {
			timedOut = true
			continue
		}
		if r.Status != "fail" {
			continue
		}
		if goStage {
			failing := goFailures(out)
			if len(failing) == 0 {
				notBuilt = notBuilt || goBuildFailed(out)
				continue
			}
			if err := ensureControl(); err != nil {
				return ev, err
			}
			c, cout := e.runStageFull(ctx, controlTarget, st, pkgs)
			switch {
			case stageTimedOut(c):
				timedOut = true
				continue
			case c.Status == "error" || c.Status == "skipped":
				if envErr == "" {
					envErr = st.Name + " (control): " + firstLine(c.Output)
				}
				continue
			case c.Status == "fail" && goBuildFailed(cout):
				// The untouched base does not build: its failures are unknown,
				// so nothing can be attributed to the changed tests.
				continue
			}
			before := goFailures(cout)
			for _, name := range slices.Sorted(maps.Keys(failing)) {
				if !before[name] && !seen[name] && (goData || goTests[name]) {
					seen[name] = true
					ev.Tests = append(ev.Tests, name)
				}
			}
			continue
		}
		if err := ensureControl(); err != nil {
			return ev, err
		}
		c, cout := e.runStageFull(ctx, controlTarget, st, []string{"./..."})
		if c.Status == "error" && envErr == "" {
			envErr = st.Name + " (control): " + firstLine(c.Output)
		}
		if c.Status != "pass" || stageTimedOut(c) {
			continue
		}
		// The stage passes without the changed tests; a load failure that
		// only appears with them means they import code the change adds.
		if newLoadFailure(out, cout) {
			notBuilt = true
			continue
		}
		ev.Tests = append(ev.Tests, st.Name+": fails on base with the changed tests, passes without them")
	}
	ev.Verified = len(ev.Tests) > 0
	switch {
	case ev.Verified:
		ev.Reason = fmt.Sprintf("%d test(s) fail on the base and pass on the change", len(ev.Tests))
	case notBuilt:
		ev.Reason = "the changed tests do not build or load against the base commit (they use code the change adds), " +
			"so they cannot show a change in behaviour; a test that also runs on the original code, for example through its existing API, is needed"
	case envErr != "":
		ev.Reason = "a test stage could not run on the base commit (" + trunc(envErr, 300) + "), so the changed tests could not be compared"
	case timedOut:
		ev.Reason = "a test stage timed out on the base commit, so the changed tests could not be compared"
	case !ran:
		ev.Reason = "no test stage runs the changed test files"
	default:
		ev.Reason = "the changed tests also pass on the base commit, so they do not demonstrate the change"
	}
	return ev, nil
}

// linkDeps links the checkout's installed dependencies into an exported tree
// when there is no container to mount them into (development sandbox).
func (e *Engine) linkDeps(source, dst string) error {
	if e.Sandbox == nil || e.Sandbox.Isolated() {
		return nil
	}
	return linkDependencies(source, dst)
}

// goPackageArg is the go test argument for a repository-relative directory.
func goPackageArg(dir string) string {
	dir = filepath.ToSlash(dir)
	if dir == "." || dir == "" {
		return "."
	}
	return "./" + dir
}

// goTestDataPackage finds the Go package whose tests read a changed data
// file: the directory holding the file's testdata/ directory (the Go
// convention), else the nearest enclosing directory with _test.go files.
// Directories are looked up in the candidate tree.
func goTestDataPackage(worktree, f string) (string, bool) {
	parts := strings.Split(filepath.ToSlash(f), "/")
	for i, p := range parts[:len(parts)-1] {
		if p == "testdata" {
			dir := filepath.Join(append([]string{"."}, parts[:i]...)...)
			return dir, hasGoFiles(filepath.Join(worktree, dir), "_test.go")
		}
	}
	for dir := filepath.Dir(f); ; dir = filepath.Dir(dir) {
		if hasGoFiles(filepath.Join(worktree, dir), "_test.go") {
			return dir, true
		}
		if dir == "." || dir == "/" {
			return "", false
		}
	}
}

func hasGoFiles(dir, suffix string) bool {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range ents {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), suffix) {
			return true
		}
	}
	return false
}

// goFailures are the top-level test functions reported failing in go test
// output (a failing subtest is reported under its parent too).
func goFailures(out string) map[string]bool {
	m := map[string]bool{}
	for _, f := range goFailRE.FindAllStringSubmatch(out, -1) {
		m[f[1]] = true
	}
	return m
}

var goTestFuncRE = regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]*)\(`)

var goBuildFailRE = regexp.MustCompile(`(?m)^FAIL\s+\S+\s+\[(build|setup) failed\]`)

// goBuildFailed reports whether a package failed to compile or load.
func goBuildFailed(out string) bool { return goBuildFailRE.MatchString(out) }

// loadFailureRE matches JavaScript/TypeScript test runs that failed before
// any test ran because a module or export is missing (the tests import code
// the change adds) or a file does not parse.
var loadFailureRE = regexp.MustCompile(`Cannot find module|ERR_MODULE_NOT_FOUND|Module not found|` +
	`does not provide an export named|Failed to resolve import|Failed to load url|` +
	`Test suite failed to run|error TS\d+:`)

// newLoadFailure reports a load failure in out on a line the control run
// did not print.
func newLoadFailure(out, control string) bool {
	known := map[string]bool{}
	for l := range strings.SplitSeq(control, "\n") {
		known[strings.TrimSpace(l)] = true
	}
	for l := range strings.SplitSeq(out, "\n") {
		if loadFailureRE.MatchString(l) && !known[strings.TrimSpace(l)] {
			return true
		}
	}
	return false
}

// underTestdata reports whether a path is inside a testdata directory
// (Go test inputs, which may themselves be .go files).
func underTestdata(p string) bool {
	parts := strings.Split(filepath.ToSlash(p), "/")
	return slices.Contains(parts[:len(parts)-1], "testdata")
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// stageTimedOut reports whether a stage was stopped by its timeout.
func stageTimedOut(r StageResult) bool {
	return r.Status == "fail" && r.ExitCode == 0 && strings.HasPrefix(r.Output, "timeout after ")
}

// scratchRoot is where evidence trees are exported: under the per-task cache
// (shared with the container engine), or the system temp dir.
func (e *Engine) scratchRoot() string {
	if e.CacheDir != "" {
		d := filepath.Join(e.CacheDir, "evidence")
		if os.MkdirAll(d, 0o700) == nil {
			return d
		}
	}
	return os.TempDir()
}

// overlay copies the candidate's version of each file into dst (removing it
// where the candidate deleted it). Only regular files are copied.
func overlay(src, dst string, files []string) error {
	for _, f := range files {
		if !safeRel(f) {
			continue
		}
		from, to := filepath.Join(src, f), filepath.Join(dst, f)
		fi, err := os.Lstat(from)
		switch {
		case err != nil && os.IsNotExist(err):
			if err := os.Remove(to); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		case err != nil:
			return err
		case !fi.Mode().IsRegular():
			continue
		}
		b, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(to, b, fs.FileMode(0o644)|fi.Mode().Perm()&0o111); err != nil {
			return err
		}
	}
	return nil
}

// linkDependencies symlinks source's installed dependency directories into
// dst at the same relative paths (unisolated sandbox only).
func linkDependencies(source, dst string) error {
	if _, err := os.Stat(dst); err != nil {
		return nil //nolint:nilerr // tree not exported (not needed)
	}
	deps, err := sandbox.DependencyMounts(source, dst)
	if err != nil {
		return err
	}
	for _, m := range deps.Mounts {
		if _, err := os.Lstat(m.Target); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(m.Target), 0o755); err != nil {
			return err
		}
		if err := os.Symlink(m.Host, m.Target); err != nil {
			return err
		}
	}
	return nil
}
