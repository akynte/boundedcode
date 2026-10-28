package verify

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/ecosystem"
	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/lang"
)

// Test integrity. The verification configuration comes from the base commit
// (LoadConfig), but the commands it runs read the candidate's files: its
// package.json scripts, Makefile, wrappers (gradlew, mvnw), tool
// configuration (.cargo/config.toml, .mvn/maven.config, .npmrc) and test
// runner settings (pytest.ini, conftest.py, jest.config.js). A change to
// these, the deletion of a test, a skip or focus marker added to a test, or
// assertions removed from one, can make verification pass without testing
// the change. The integrity layer finds them, from the build systems' and
// languages' descriptors (internal/ecosystem, internal/lang), not from any
// one language:
//
//   - block: the change redefines what runs as the tests (a test script, a
//     wrapper, a runner switch such as -DskipTests or ignoreFailures);
//     verification fails, and the agent is told to revert it;
//   - review: the change touches what the runner runs or skips, deletes a
//     test, adds a skip marker or removes assertions; verification may pass,
//     but evidence from the tests is not trusted (no task_verified).

// Severities of an integrity finding.
const (
	IntegrityBlock  = "block"
	IntegrityReview = "review"
)

// IntegrityFinding is one way the change may weaken verification.
type IntegrityFinding struct {
	File     string `json:"file"`
	Severity string `json:"severity"`
	Reason   string `json:"reason"`
}

func (f IntegrityFinding) String() string { return f.File + ": " + f.Reason }

// integrityFiles are the wrapper and runner-config entries of every build
// system.
func integrityFiles() (wrappers, runner []string) {
	for _, bs := range ecosystem.BuildSystems() {
		wrappers = append(wrappers, bs.Wrappers...)
		runner = append(runner, bs.RunnerConfig...)
	}
	return wrappers, runner
}

// matchProjectFile reports whether rel (repository-relative) is pattern in
// some project directory: "gradlew", ".mvn/", "jest.config.*".
func matchProjectFile(rel, pattern string) bool {
	if strings.HasSuffix(pattern, "/") {
		return strings.HasPrefix(rel, pattern) || strings.Contains(rel, "/"+pattern)
	}
	if strings.Contains(pattern, "/") {
		return rel == pattern || strings.HasSuffix(rel, "/"+pattern)
	}
	ok, _ := path.Match(pattern, path.Base(rel))
	return ok
}

// Integrity checks the change for ways it weakens verification.
func (e *Engine) Integrity(ctx context.Context, t RepoTarget, changed []string) []IntegrityFinding {
	var out []IntegrityFinding
	add := func(file, sev, reason string) {
		out = append(out, IntegrityFinding{File: file, Severity: sev, Reason: reason})
	}
	wrappers, runner := integrityFiles()
	baseOf := func(rel string) (string, bool) {
		if t.Base == "" {
			return "", false
		}
		s, err := gitops.Run(ctx, t.Worktree, "cat-file", "blob", t.Base+":"+rel)
		return s, err == nil
	}
	curOf := func(rel string) (string, bool) {
		b, err := os.ReadFile(filepath.Join(t.Worktree, filepath.FromSlash(rel)))
		return string(b), err == nil
	}
	addedTests := map[string]bool{}
	for _, f := range changed {
		if _, ok := baseOf(f); !ok && IsTestFile(f) {
			addedTests[path.Base(f)] = true
		}
	}
	for _, f := range changed {
		base, inBase := baseOf(f)
		cur, inCur := curOf(f)
		if base == cur && inBase == inCur {
			continue
		}
		name := path.Base(f)
		switch {
		case anyMatch(f, wrappers):
			add(f, IntegrityBlock, "changes what a build or test command runs (a wrapper or tool configuration); revert it")
			continue
		case name == "package.json":
			out = append(out, packageJSONFindings(f, base, cur)...)
			continue
		case name == "Makefile" || name == "GNUmakefile" || name == "makefile":
			for _, tgt := range []string{"test", "check"} {
				if recipe(base, tgt) != recipe(cur, tgt) {
					add(f, IntegrityBlock, "changes the recipe of the `"+tgt+"` target that verification runs; revert it")
				}
			}
			continue
		case name == "pyproject.toml" || name == "setup.cfg":
			for _, sec := range []string{"tool:pytest", "tool.pytest"} { // tool.pytest covers [tool.pytest.ini_options]
				if section(base, sec) != section(cur, sec) {
					add(f, IntegrityReview, "changes the pytest configuration ["+sec+"]")
				}
			}
			continue
		case name == "pom.xml":
			out = append(out, lineFindings(f, base, cur, pomBlock, pomReview, "Maven test configuration")...)
			continue
		case name == "build.gradle" || name == "build.gradle.kts":
			out = append(out, lineFindings(f, base, cur, gradleBlock, gradleReview, "Gradle test configuration")...)
			continue
		case name == "Cargo.toml":
			out = append(out, lineFindings(f, base, cur, cargoBlock, cargoReview, "Cargo test targets")...)
			continue
		case name == "CMakeLists.txt":
			out = append(out, lineFindings(f, base, cur, nil, cmakeReview, "CTest registration")...)
			continue
		case anyMatch(f, runner):
			add(f, IntegrityReview, "changes the test runner's configuration")
			continue
		}
		if !IsTestFile(f) {
			continue
		}
		if inBase && !inCur {
			if !addedTests[name] {
				add(f, IntegrityReview, "deletes a test file")
			}
			continue
		}
		l := lang.ForPath(f, "")
		if l == nil {
			continue
		}
		added, removed := diffLines(base, cur)
		for _, line := range added {
			for _, re := range l.Skips {
				if re.MatchString(line) {
					add(f, IntegrityReview, "adds a line that skips, disables or focuses tests: "+trunc(strings.TrimSpace(line), 120))
					break
				}
			}
		}
		if inBase {
			lost := 0
			for _, line := range removed {
				if assertionLine.MatchString(line) {
					lost++
				}
			}
			for _, line := range added {
				if assertionLine.MatchString(line) {
					lost--
				}
			}
			if lost > 0 {
				add(f, IntegrityReview, fmt.Sprintf("removes %d assertion(s) without adding as many", lost))
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Severity < out[j].Severity })
	return dedupeFindings(out)
}

func dedupeFindings(fs []IntegrityFinding) []IntegrityFinding {
	seen := map[string]bool{}
	var out []IntegrityFinding
	for _, f := range fs {
		k := f.File + "\x00" + f.Reason
		if !seen[k] {
			seen[k] = true
			out = append(out, f)
		}
	}
	return out
}

func anyMatch(rel string, patterns []string) bool {
	for _, p := range patterns {
		if matchProjectFile(rel, p) {
			return true
		}
	}
	return false
}

var assertionLine = regexp.MustCompile(`\b(assert\w*|expect|require\.\w+|should|XCTAssert\w*|EXPECT_\w+|ASSERT_\w+|assertThat|refute\w*|t\.(Error|Errorf|Fatal|Fatalf|Fail))\b`)

// diffLines returns the lines of cur not in base and of base not in cur
// (as multisets).
func diffLines(base, cur string) (added, removed []string) {
	count := map[string]int{}
	for _, l := range strings.Split(base, "\n") {
		count[strings.TrimSpace(l)]++
	}
	for _, l := range strings.Split(cur, "\n") {
		k := strings.TrimSpace(l)
		if count[k] > 0 {
			count[k]--
			continue
		}
		if k != "" {
			added = append(added, l)
		}
	}
	curCount := map[string]int{}
	for _, l := range strings.Split(cur, "\n") {
		curCount[strings.TrimSpace(l)]++
	}
	for _, l := range strings.Split(base, "\n") {
		k := strings.TrimSpace(l)
		if curCount[k] > 0 {
			curCount[k]--
			continue
		}
		if k != "" {
			removed = append(removed, l)
		}
	}
	return added, removed
}

// The scripts and configuration keys of package.json that decide what runs
// as the tests.
var (
	npmTestScripts = []string{"test", "pretest", "posttest", "test:unit", "test:ci"}
	npmRunnerKeys  = []string{"jest", "mocha", "ava", "vitest", "c8", "nyc", "tap"}
)

func packageJSONFindings(f, base, cur string) []IntegrityFinding {
	var b, c map[string]json.RawMessage
	if json.Unmarshal([]byte(base), &b) != nil || json.Unmarshal([]byte(cur), &c) != nil {
		return nil // a new package, or one that does not parse (the build says so)
	}
	var out []IntegrityFinding
	var bs, cs map[string]string
	_ = json.Unmarshal(b["scripts"], &bs)
	_ = json.Unmarshal(c["scripts"], &cs)
	for _, s := range npmTestScripts {
		if bs[s] != cs[s] {
			out = append(out, IntegrityFinding{File: f, Severity: IntegrityBlock, Reason: "changes the `" + s + "` script that verification runs (" +
				trunc(bs[s], 60) + " → " + trunc(cs[s], 60) + "); revert it"})
		}
	}
	for _, k := range npmRunnerKeys {
		if string(b[k]) != string(c[k]) {
			out = append(out, IntegrityFinding{File: f, Severity: IntegrityReview, Reason: "changes the `" + k + "` test runner configuration"})
		}
	}
	return out
}

// recipe returns a Makefile target's recipe (the target line and the
// indented lines after it).
func recipe(src, target string) string {
	var b strings.Builder
	in := false
	sc := bufio.NewScanner(strings.NewReader(src))
	for sc.Scan() {
		l := sc.Text()
		switch {
		case strings.HasPrefix(l, target+":") || strings.HasPrefix(l, target+" :"):
			in = true
			b.WriteString(l + "\n")
		case in && strings.TrimSpace(l) == "":
		case in && strings.HasPrefix(l, "\t"):
			b.WriteString(l + "\n")
		case in:
			in = false
		}
	}
	return b.String()
}

// section returns an INI or TOML section's text.
func section(src, name string) string {
	var b strings.Builder
	in := false
	for _, l := range strings.Split(src, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") {
			in = t == "["+name+"]" || strings.HasPrefix(t, "["+name+".")
		}
		if in && t != "" {
			b.WriteString(t + "\n")
		}
	}
	return b.String()
}

var (
	pomBlock     = regexp.MustCompile(`(?i)skipTests|maven\.test\.skip|testFailureIgnore|<skip>\s*true`)
	pomReview    = regexp.MustCompile(`(?i)surefire|failsafe|<excludes?>|<test>|<includes?>|<groups>|excludedGroups`)
	gradleBlock  = regexp.MustCompile(`ignoreFailures|enabled\s*=\s*false|onlyIf\s*\{|-x\s+test`)
	gradleReview = regexp.MustCompile(`\btest\s*\{|tasks\.test|withType<Test>|withType\(Test\)|useJUnit|exclude\b|filter\s*\{|includeTestsMatching`)
	cargoBlock   = regexp.MustCompile(`^\s*test\s*=\s*false|^\s*harness\s*=\s*false`)
	cargoReview  = regexp.MustCompile(`\[\[test\]\]|\[lib\]|\[\[bin\]\]|autotests`)
	cmakeReview  = regexp.MustCompile(`(?i)add_test|enable_testing|set_tests_properties|ctest`)
)

// lineFindings flags the changed lines of a build file that match block or
// review patterns.
func lineFindings(f, base, cur string, block, review *regexp.Regexp, what string) []IntegrityFinding {
	added, removed := diffLines(base, cur)
	var out []IntegrityFinding
	for _, l := range append(added, removed...) {
		switch {
		case block != nil && block.MatchString(l):
			return append(out, IntegrityFinding{File: f, Severity: IntegrityBlock, Reason: "changes the " + what + " so that tests may not run or not fail: " +
				trunc(strings.TrimSpace(l), 100) + "; revert it"})
		case review != nil && review.MatchString(l):
			out = []IntegrityFinding{{File: f, Severity: IntegrityReview, Reason: "changes the " + what + ": " + trunc(strings.TrimSpace(l), 100)}}
		}
	}
	return out
}

// integrityStage records the findings as a stage: blocking ones fail it.
func integrityStage(fs []IntegrityFinding) StageResult {
	sr := StageResult{Name: "test-integrity", Status: "pass", Command: "(built-in)"}
	var lines []string
	for _, f := range fs {
		lines = append(lines, "["+f.Severity+"] "+f.String())
		if f.Severity == IntegrityBlock {
			sr.Status, sr.ExitCode, sr.Cause = "fail", 1, CauseIntegrity
		}
	}
	sr.Output = strings.Join(lines, "\n")
	return sr
}
