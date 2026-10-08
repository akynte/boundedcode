package verify

import (
	"context"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/sandbox"
)

func presetStages(files ...string) []string {
	m := map[string]bool{}
	for _, f := range files {
		m[f] = true
	}
	var out []string
	for _, s := range presetFor(m).Stages {
		out = append(out, s.Name)
	}
	return out
}

// TestPresetsPerLanguage: every language with a toolchain in the sandbox
// image gets a build and a test stage without any configuration.
func TestPresetsPerLanguage(t *testing.T) {
	for _, c := range []struct {
		files []string
		want  []string
	}{
		{[]string{"pyproject.toml", "pkg/mod.py", "tests/test_mod.py"}, []string{"python-test"}},
		{[]string{"setup.py", "pkg/mod_test.py"}, []string{"python-test"}},
		{[]string{"tools/run.py", "test_tool.py"}, []string{"python-test"}}, // no packaging, still tests
		{[]string{"Cargo.toml", "src/lib.rs"}, []string{"cargo-build", "cargo-test"}},
		{[]string{"pom.xml", "src/main/java/A.java"}, []string{"maven-compile", "maven-test"}},
		{[]string{"build.gradle.kts", "gradlew"}, []string{"gradle-compile", "gradle-test"}},
		{[]string{"settings.gradle", "app/build.gradle"}, []string{"gradle-compile", "gradle-test"}},
		{[]string{"CMakeLists.txt", "src/a.cpp"}, []string{"cmake-build", "ctest"}},
		{[]string{"meson.build", "src/a.c"}, []string{"meson-build", "meson-test"}},
		{[]string{"configure.ac", "Makefile.am", "src/jq.c"}, []string{"autotools-build", "make-check"}},
		{[]string{"Makefile", "src/redis.c"}, []string{"make-build", "make-test"}},
		{[]string{"Gemfile", "lib/a.rb"}, []string{"ruby-test"}},
		{[]string{"a.gemspec", "lib/a.rb"}, []string{"ruby-test"}},
		{[]string{"composer.json", "src/A.php"}, []string{"php-test"}},
		// A language without a preset: the project's own make target.
		{[]string{"Makefile", "src/main.zig"}, []string{"make-test"}},
		// Nothing to run at all.
		{[]string{"README.md"}, nil},
	} {
		if got := presetStages(c.files...); !slices.Equal(got, c.want) {
			t.Errorf("%v: stages %v, want %v", c.files, got, c.want)
		}
	}
}

// TestPresetsDoNotOverreach: secondary files of another language must not
// add stages that fail a repository which was verified fine before.
func TestPresetsDoNotOverreach(t *testing.T) {
	got := presetStages("go.mod", "main.go", "requirements.txt", "scripts/gen.py", "Makefile", "cgo/x.c", "configure")
	for _, s := range got {
		if s == "python-test" || s == "make-build" || s == "autotools-build" || s == "make-test" {
			t.Errorf("Go repository got %s: %v", s, got)
		}
	}
	// Go's own stages are unchanged.
	if want := []string{"gofmt", "go-build", "go-vet", "go-test", "golangci-lint"}; !slices.Equal(got, want) {
		t.Errorf("go stages = %v, want %v", got, want)
	}
}

// TestPresetTestStages: only the stages that run tests are rerun for
// behavioural evidence, whatever their commands mention (mvn test-compile).
func TestPresetTestStages(t *testing.T) {
	for _, s := range presetFor(map[string]bool{"pom.xml": true, "Cargo.toml": true, "CMakeLists.txt": true, "x.c": true}).Stages {
		want := s.Name == "maven-test" || s.Name == "cargo-test" || s.Name == "ctest"
		if isTestStage(s) != want {
			t.Errorf("isTestStage(%s) = %v", s.Name, !want)
		}
	}
	// User configs keep the name/command heuristic.
	if !isTestStage(Stage{Name: "unit-tests", Run: []string{"pytest"}}) || !isTestStage(Stage{Name: "x", Run: []string{"make", "test"}}) {
		t.Error("heuristic lost")
	}
}

// TestPresetScriptsParse runs every preset script through sh -n.
func TestPresetScriptsParse(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh required")
	}
	all := map[string]bool{"go.mod": true, "package.json": true, "tsconfig.json": true, "pyproject.toml": true, "tests/test_a.py": true,
		"Cargo.toml": true, "pom.xml": true, "Gemfile": true, "composer.json": true, "a.tf": true, "Chart.yaml": true}
	stages := presetFor(all).Stages
	for _, f := range []map[string]bool{{"build.gradle": true}, {"CMakeLists.txt": true}, {"meson.build": true},
		{"configure.ac": true, "a.c": true}, {"Makefile": true, "a.c": true}, {"Makefile": true}} {
		stages = append(stages, presetFor(f).Stages...)
	}
	for _, s := range stages {
		if len(s.Run) != 3 || s.Run[0] != "sh" {
			continue
		}
		if out, err := exec.Command("sh", "-n", "-c", s.Run[2]).CombinedOutput(); err != nil {
			t.Errorf("%s: %v\n%s\n%s", s.Name, err, out, s.Run[2])
		}
		if err := (Config{Stages: []Stage{s}}).validate(); err != nil {
			t.Errorf("%s: %v", s.Name, err)
		}
	}
}

func TestIsTestFileOtherLanguages(t *testing.T) {
	for p, want := range map[string]bool{
		"test_calc.py":                          true,
		"pkg/calc_test.py":                      true,
		"conftest.py":                           true,
		"src/test/java/a/CalcTest.java":         true,
		"core/src/main/java/a/CalcTests.java":   true,
		"a/CalcIT.java":                         true,
		"lib/CalcSpec.scala":                    true,
		"calc_test.rb":                          true,
		"spec_helper_spec.rb":                   true,
		"CalcTest.php":                          true,
		"Calc.Tests/CalcTests.cs":               true,
		"calc_unittest.cc":                      true,
		"test_calc.c":                           true,
		"calc_test.exs":                         true,
		"calc.py":                               false,
		"src/main/java/a/Calc.java":             false,
		"src/main/java/a/Testing.java":          false,
		"lib/calc.rb":                           false,
		"src/Calc.php":                          false,
		"src/calc.c":                            false,
		"src/main/java/a/LatestController.java": false,
	} {
		if got := IsTestFile(p); got != want {
			t.Errorf("IsTestFile(%q) = %v, want %v", p, got, want)
		}
	}
}

// TestTestFailures parses each runner's failure report.
func TestTestFailures(t *testing.T) {
	for runner, c := range map[string]struct{ out, want string }{
		"pytest":   {"tests/test_a.py .F\n=== short test summary info ===\nFAILED tests/test_a.py::test_neg - assert 1 == 0\nFAILED tests/test_a.py::TestC::test_x[1-2]\nERROR tests/test_b.py - ImportError\n", "tests/test_a.py::TestC::test_x[1-2],tests/test_a.py::test_neg"},
		"unittest": {"FAIL: test_neg (tests.test_a.CalcTest.test_neg)\nERROR: test_a (unittest.loader._FailedTest.test_a)\n", "test_neg"},
		"cargo":    {"test clamp_high ... ok\ntest clamp_negative ... FAILED\ntest m::tests::inner ... FAILED\n", "clamp_negative,m::tests::inner"},
		"surefire3": {"[ERROR] Tests run: 2, Failures: 1, Errors: 0, Skipped: 0, Time elapsed: 0.05 s <<< FAILURE! -- in calc.CalcTest\n" +
			"[ERROR] calc.CalcTest.clampNegative -- Time elapsed: 0.01 s <<< FAILURE!\n", "calc.CalcTest.clampNegative"},
		"surefire2": {"[ERROR] clampNegative(calc.CalcTest)  Time elapsed: 0.01 s  <<< ERROR!\n", "clampNegative(calc.CalcTest)"},
		"gradle":    {"> Task :test FAILED\n\nCalcTest > clampNegative() FAILED\n    java.lang.AssertionError at CalcTest.java:9\n", "CalcTest > clampNegative()"},
		"minitest":  {"  1) Failure:\nCalcTest#test_clamp_negative [test/calc_test.rb:6]:\nExpected: 0\n", "CalcTest#test_clamp_negative"},
		"rspec":     {"Failed examples:\n\nrspec ./spec/calc_spec.rb:12 # Calc clamps negatives\n", "./spec/calc_spec.rb:12"},
		"phpunit":   {"There was 1 failure:\n\n1) Tests\\CalcTest::testClampNegative\nFailed asserting that -1 is identical to 0.\n", "Tests\\CalcTest::testClampNegative"},
		"ctest":     {"The following tests FAILED:\n\t  2 - clamp_negative (Failed)\n\t  3 - crash (SEGFAULT)\n", "clamp_negative,crash"},
		"meson":     {"1/2 calc:clamp_high          OK      0.01s\n2/2 calc:clamp_negative      FAIL    0.01s   exit status 1\n", "clamp_negative"},
	} {
		got := slices.Sorted(func(yield func(string) bool) {
			for k := range testFailures(c.out) {
				if !yield(k) {
					return
				}
			}
		})
		if strings.Join(got, ",") != c.want {
			t.Errorf("%s: failures %q, want %q", runner, got, c.want)
		}
	}
}

func TestAttribution(t *testing.T) {
	a := attribution{paths: []string{"tests/test_a.py", "spec/calc_spec.rb"}, contents: []string{"def test_neg():\n", "it 'clamps' do\n", "@Test public void clampNegative()"}}
	for name, want := range map[string]bool{
		"tests/test_a.py::test_other":  true, // its file changed
		"./spec/calc_spec.rb:12":       true,
		"tests/test_b.py::test_neg[1]": true, // its name is in a changed file
		"calc.CalcTest.clampNegative":  true,
		"CalcTest > clampNegative()":   true,
		"clampNegative(calc.CalcTest)": true,
		"tests/test_b.py::test_flaky":  false,
		"other.FlakyTest.timing":       false,
		"x":                            false,
	} {
		if got := a.owns(name); got != want {
			t.Errorf("owns(%q) = %v, want %v", name, got, want)
		}
	}
	if !(attribution{data: true}).owns("anything") {
		t.Error("changed test data can affect any test")
	}
}

// TestNoTestRunnerIsReported: a repository in a language without a preset
// does not pass silently as if its tests had run.
func TestNoTestRunnerIsReported(t *testing.T) {
	wt, base := commitRepo(t, map[string]string{"main.zig": "pub fn main() void {}\n"})
	e := &Engine{Sandbox: sandbox.None{}, Gitleaks: fakeGitleaks(t)}
	res, err := e.Run(context.Background(), RepoTarget{Name: "zig", Worktree: wt, Base: strings.TrimSpace(base)}, Targeted)
	if err != nil {
		t.Fatal(err)
	}
	st := stageNamed(res, "tests")
	if st.Status != "skipped" || !strings.Contains(st.Output, ConfigPath) {
		t.Fatalf("stages = %v", stageNames(res))
	}
}
