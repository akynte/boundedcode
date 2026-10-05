package verify

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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
// data: a conventional test file, or anything under a directory whose name
// marks tests (test, tests, __tests__, spec, testdata, or a *test* directory
// such as caddytest).
func IsTestFile(p string) bool {
	if testFileRE.MatchString(p) {
		return true
	}
	parts := strings.Split(filepath.ToSlash(p), "/")
	for _, d := range parts[:len(parts)-1] {
		l := strings.ToLower(d)
		if l == "spec" || l == "specs" || l == "__tests__" || strings.Contains(l, "test") {
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

var goTestFuncRE = regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]*)\(`)
var goFailRE = regexp.MustCompile(`(?m)^\s*--- FAIL: (Test[A-Za-z0-9_]*)`)

// BehaviourEvidence checks whether the change's own tests demonstrate it:
// the changed test files are laid over an export of the base commit and the
// repository's test stages run there. For Go, a test defined in a changed
// test file must fail on base; elsewhere, the same stage must pass on the
// untouched base (control) and fail with the changed tests.
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
	var goTests []string
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
	control := filepath.Join(root, "control")
	needControl := false
	pkgSet := map[string]bool{}
	for _, f := range ev.TestFiles {
		if strings.HasSuffix(f, "_test.go") {
			pkgSet["./"+filepath.ToSlash(filepath.Dir(f))] = true
			if b, err := os.ReadFile(filepath.Join(t.Worktree, f)); err == nil {
				for _, m := range goTestFuncRE.FindAllStringSubmatch(string(b), -1) {
					goTests = append(goTests, m[1])
				}
			}
		} else {
			needControl = true
		}
	}
	if needControl {
		if err := gitops.ExportTree(ctx, t.Worktree, t.Base, control); err != nil {
			return ev, err
		}
	}
	if err := overlay(t.Worktree, base, ev.TestFiles); err != nil {
		return ev, err
	}
	if e.Sandbox != nil && !e.Sandbox.Isolated() {
		// No container to mount dependencies into (development sandbox):
		// link the checkout's installed dependencies instead.
		for _, d := range []string{base, control} {
			if err := linkDependencies(t.Source, d); err != nil {
				return ev, err
			}
		}
	}
	packages := make([]string, 0, len(pkgSet))
	for p := range pkgSet {
		packages = append(packages, p)
	}
	sort.Strings(packages)
	if len(packages) == 0 {
		packages = []string{"./..."}
	}
	isGoTest := map[string]bool{}
	for _, n := range goTests {
		isGoTest[n] = true
	}
	baseTarget := RepoTarget{Name: t.Name, Worktree: base, TaskID: t.TaskID, Source: t.Source}
	for _, st := range stages {
		goStage := len(st.Requires) > 0 && st.Requires[0] == "go.mod"
		if goStage && len(pkgSet) == 0 {
			continue // no Go test changed
		}
		if !goStage && !needControl {
			continue // only Go tests changed
		}
		r, out := e.runStageFull(ctx, baseTarget, st, packages)
		if r.Status != "fail" {
			continue
		}
		if goStage {
			for _, m := range goFailRE.FindAllStringSubmatch(out, -1) {
				if isGoTest[m[1]] {
					ev.Tests = append(ev.Tests, m[1])
				}
			}
			// A change whose new tests do not compile against the base
			// (they call the new code) also depends on the change.
			if len(ev.Tests) == 0 && strings.Contains(out, "[build failed]") {
				ev.Tests = append(ev.Tests, st.Name+": changed tests do not build against the base")
			}
			continue
		}
		c := e.runStage(ctx, RepoTarget{Name: t.Name, Worktree: control, TaskID: t.TaskID, Source: t.Source}, st, []string{"./..."})
		if c.Status == "pass" {
			ev.Tests = append(ev.Tests, st.Name+": fails on base with the changed tests, passes without them")
		}
	}
	ev.Verified = len(ev.Tests) > 0
	if ev.Verified {
		ev.Reason = fmt.Sprintf("%d test(s) fail on the base and pass on the change", len(ev.Tests))
	} else {
		ev.Reason = "the changed tests also pass on the base commit, so they do not demonstrate the change"
	}
	return ev, nil
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
