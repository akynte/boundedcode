package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/akynte/boundedcode/internal/gitops"
)

// Grading against a clean snapshot. The agent's checkout can hold more
// than its change: ignored files, build caches, a test runner it
// reconfigured, a grading script it rewrote. Hidden checks therefore run in
// a fresh copy of the task's repository at its base, with the agent's
// change applied file by file: the files git sees as added, modified or
// deleted (ignored files are no part of a change).
//
// Test infrastructure is protected:
//   - runner configuration, CI files and the suite's grading scripts the
//     change modified are restored to the base (unless the task's
//     may_change allows them), and reported;
//   - a narrowed test script in package.json is restored, the rest of the
//     manifest kept;
//   - test sections of other build manifests, removed tests and newly
//     skipped tests are reported (restoring them could break a change that
//     needs them).

// Tamper is what grading found the change doing to test infrastructure.
type Tamper struct {
	// Restored are files (or package.json's test script) put back to the
	// base before grading.
	Restored []string `json:"restored,omitempty"`
	// Flagged are changes to test sections of manifests, kept.
	Flagged []string `json:"flagged,omitempty"`
	// RemovedTests and Skipped are test declarations the change removed,
	// and skip markers it added, per file.
	RemovedTests []string `json:"removed_tests,omitempty"`
	Skipped      []string `json:"skipped,omitempty"`
}

// Any reports whether grading found tampering of any kind.
func (t Tamper) Any() bool {
	return len(t.Restored)+len(t.Flagged)+len(t.RemovedTests)+len(t.Skipped) > 0
}

// Summary is one line for a report.
func (t Tamper) Summary() string {
	var parts []string
	add := func(label string, xs []string) {
		if len(xs) > 0 {
			parts = append(parts, label+" "+strings.Join(xs, ", "))
		}
	}
	add("restored:", t.Restored)
	add("flagged:", t.Flagged)
	add("tests removed:", t.RemovedTests)
	add("tests skipped:", t.Skipped)
	return strings.Join(parts, "; ")
}

// runnerConfig are files that decide how tests run and are not needed to
// build a change: restored when changed.
var runnerConfig = []string{
	".github/*", ".github/*/*", ".github/*/*/*", ".gitlab-ci.yml", ".circleci/*", "Jenkinsfile", "azure-pipelines.yml",
	".bench/*", ".bench/*/*",
	"pytest.ini", "conftest.py", "*/conftest.py", "*/*/conftest.py", "tox.ini", "noxfile.py",
	"jest.config.*", "vitest.config.*", ".mocharc*", "karma.conf.*", "playwright.config.*", "ava.config.*",
	"phpunit.xml", "phpunit.xml.dist", "phpunit.dist.xml", ".rspec",
	"CTestCustom.cmake", "*.runsettings",
}

// manifestTests are build manifests whose test sections matter; a change
// to that section is flagged, not restored.
var manifestTests = map[string]*regexp.Regexp{
	"Makefile":         regexp.MustCompile(`(?m)^(test|check)\s*:.*(\n\t.*)*`),
	"CMakeLists.txt":   regexp.MustCompile(`(?i)(add_test|enable_testing|ctest|gtest_discover_tests)[^\n]*`),
	"Rakefile":         regexp.MustCompile(`(?s)Rake::TestTask.*?end|task :test.*?end`),
	"pyproject.toml":   regexp.MustCompile(`(?s)\[tool\.(pytest|unittest)[^\]]*\][^\[]*`),
	"setup.cfg":        regexp.MustCompile(`(?s)\[tool:pytest\][^\[]*`),
	"Cargo.toml":       regexp.MustCompile(`(?s)\[\[test\]\][^\[]*|autotests\s*=\s*\w+`),
	"pom.xml":          regexp.MustCompile(`(?s)<artifactId>maven-surefire-plugin</artifactId>.*?</plugin>`),
	"build.gradle":     regexp.MustCompile(`(?s)test\s*\{.*?\}`),
	"build.gradle.kts": regexp.MustCompile(`(?s)tasks\.test.*?\}|test\s*\{.*?\}`),
}

// testDecl counts test declarations; skipDecl counts skip markers, in
// the test files of every language the suite covers.
var (
	testDecl = regexp.MustCompile(`(?m)^\s*(func (Test|Fuzz)\w+\(|def test_?\w*\(|async def test_\w*\(|(it|test)(\.only)?\(\s*['"\x60]|#\[(tokio::)?test\]|@Test\b|public function test\w+\(|def test_\w+|TEST(_F)?\(|\[(Fact|Test|TestMethod)\]|void test_\w+\()`)
	skipDecl = regexp.MustCompile(`(?m)\b(t\.Skip(Now|f)?\(|@unittest\.skip|pytest\.mark\.skip|(it|test|describe)\.skip\(|\bxit\(|#\[ignore\]|@Ignore\b|@Disabled\b|markTestSkipped\(|\bskip\(?\s*['"]|GTEST_SKIP\(|return 77;|\[Fact\(Skip|\[Ignore\])`)
)

func isTestFile(rel string) bool {
	base := path.Base(rel)
	switch {
	case strings.HasSuffix(base, "_test.go"), strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py"),
		strings.HasSuffix(base, "_test.py"), strings.Contains(base, ".test."), strings.Contains(base, ".spec."),
		strings.HasSuffix(base, "Test.java"), strings.HasSuffix(base, "Tests.java"), strings.HasSuffix(base, "Test.php"),
		strings.HasSuffix(base, "_test.rb"), strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".rb"),
		strings.HasSuffix(base, "_spec.rb"), strings.HasSuffix(base, "Tests.cs"), strings.HasSuffix(base, "Test.cs"):
		return true
	}
	for _, dir := range strings.Split(path.Dir(rel), "/") {
		switch dir {
		case "tests", "test", "__tests__", "spec":
			return true
		}
	}
	return false
}

func globMatch(globs []string, rel string) bool {
	for _, g := range globs {
		if ok, _ := path.Match(g, rel); ok {
			return true
		}
	}
	return false
}

// changeFiles lists the agent's change in agentRoot against base: paths
// added or modified, and paths deleted.
func changeFiles(ctx context.Context, agentRoot, base string) (changed, deleted []string, err error) {
	out, err := gitops.Run(ctx, agentRoot, "diff", "--no-renames", "--name-status", "-z", base)
	if err != nil {
		return nil, nil, err
	}
	f := strings.Split(strings.TrimRight(out, "\x00"), "\x00")
	for i := 0; i+1 < len(f); i += 2 {
		if strings.HasPrefix(f[i], "D") {
			deleted = append(deleted, f[i+1])
		} else {
			changed = append(changed, f[i+1])
		}
	}
	untracked, err := gitops.Run(ctx, agentRoot, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, nil, err
	}
	for _, p := range strings.Split(strings.TrimRight(untracked, "\x00"), "\x00") {
		if p != "" {
			changed = append(changed, p)
		}
	}
	return changed, deleted, nil
}

// overlayChange applies the agent's change (agentRoot against base) to
// snapshot, a fresh copy of the repository at its base, protecting test
// infrastructure as described above.
func overlayChange(ctx context.Context, agentRoot, base, snapshot string, mayChange []string) (Tamper, error) {
	var t Tamper
	changed, deleted, err := changeFiles(ctx, agentRoot, base)
	if err != nil {
		return t, err
	}
	baseFile := func(rel string) (string, bool) {
		b, err := os.ReadFile(filepath.Join(snapshot, filepath.FromSlash(rel)))
		return string(b), err == nil
	}
	for _, rel := range deleted {
		if globMatch(runnerConfig, rel) && !globMatch(mayChange, rel) {
			t.Restored = append(t.Restored, rel+" (deleted)")
			continue
		}
		if isTestFile(rel) {
			if before, ok := baseFile(rel); ok {
				if n := len(testDecl.FindAllString(before, -1)); n > 0 {
					t.RemovedTests = append(t.RemovedTests, fmt.Sprintf("%s (%d, file deleted)", rel, n))
				}
			}
		}
		if err := os.Remove(filepath.Join(snapshot, filepath.FromSlash(rel))); err != nil && !os.IsNotExist(err) {
			return t, err
		}
	}
	for _, rel := range changed {
		src := filepath.Join(agentRoot, filepath.FromSlash(rel))
		st, err := os.Lstat(src)
		if err != nil || !st.Mode().IsRegular() {
			continue // a symlink or a vanished file is not copied into grading
		}
		after, err := os.ReadFile(src)
		if err != nil {
			return t, err
		}
		before, existed := baseFile(rel)
		allowed := globMatch(mayChange, rel)
		switch {
		case globMatch(runnerConfig, rel) && !allowed:
			t.Restored = append(t.Restored, rel)
			continue
		case path.Base(rel) == "package.json" && existed && !allowed:
			if fixed, ok := restoreTestScript(before, string(after)); ok {
				t.Restored = append(t.Restored, rel+" (scripts.test)")
				after = []byte(fixed)
			}
		case existed && !allowed:
			if re, ok := manifestTests[path.Base(rel)]; ok && strings.Join(re.FindAllString(before, -1), "\n") != strings.Join(re.FindAllString(string(after), -1), "\n") {
				t.Flagged = append(t.Flagged, rel)
			}
		}
		if existed && isTestFile(rel) && !allowed {
			if b, a := len(testDecl.FindAllString(before, -1)), len(testDecl.FindAllString(string(after), -1)); a < b {
				t.RemovedTests = append(t.RemovedTests, fmt.Sprintf("%s (%d)", rel, b-a))
			}
			if b, a := len(skipDecl.FindAllString(before, -1)), len(skipDecl.FindAllString(string(after), -1)); a > b {
				t.Skipped = append(t.Skipped, fmt.Sprintf("%s (%d)", rel, a-b))
			}
		}
		dst := filepath.Join(snapshot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return t, err
		}
		if err := os.WriteFile(dst, after, st.Mode().Perm()); err != nil {
			return t, err
		}
	}
	return t, nil
}

// restoreTestScript puts package.json's scripts.test (and the jest and
// mocha keys, which configure test runs) back to the base's when the
// change altered them. It reports whether it did.
func restoreTestScript(before, after string) (string, bool) {
	var b, a map[string]any
	if json.Unmarshal([]byte(before), &b) != nil || json.Unmarshal([]byte(after), &a) != nil {
		return after, false
	}
	same := func(x, y any) bool {
		xb, _ := json.Marshal(x)
		yb, _ := json.Marshal(y)
		return string(xb) == string(yb)
	}
	changed := false
	bs, _ := b["scripts"].(map[string]any)
	as, _ := a["scripts"].(map[string]any)
	for _, k := range []string{"test", "pretest", "posttest"} {
		var bv, av any
		if bs != nil {
			bv = bs[k]
		}
		if as != nil {
			av = as[k]
		}
		if !same(bv, av) {
			if as == nil {
				as = map[string]any{}
				a["scripts"] = as
			}
			if bv == nil {
				delete(as, k)
			} else {
				as[k] = bv
			}
			changed = true
		}
	}
	for _, k := range []string{"jest", "mocha", "ava"} {
		if !same(b[k], a[k]) {
			if b[k] == nil {
				delete(a, k)
			} else {
				a[k] = b[k]
			}
			changed = true
		}
	}
	if !changed {
		return after, false
	}
	out, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return after, false
	}
	return string(out) + "\n", true
}
