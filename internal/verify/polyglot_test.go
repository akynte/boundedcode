package verify

import (
	"context"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/sandbox"
)

// polyglotCase is one language's project: a clamp function missing its
// lower bound, an existing test, and a change that fixes it and adds a
// reproduction test.
type polyglotCase struct {
	name string
	base map[string]string // committed
	fix  map[string]string // the code fix
	test map[string]string // the change's new or modified tests
	// prep installs dependencies with the network, as a user would in their
	// checkout: it runs in the sandbox image in src (holding base, fix and
	// test files), with HOME and the package caches under cache.
	prep string
	// lockfiles produced by prep that belong in the base commit.
	lockfiles []string
	stage     string // the preset test stage
	evidence  string // a test evidence must name (substring)
}

var polyglotCases = []polyglotCase{
	{
		name: "python",
		base: map[string]string{
			"pyproject.toml":     "[project]\nname = \"calc\"\nversion = \"0\"\n\n[tool.pytest.ini_options]\ntestpaths = [\"tests\"]\n",
			"calc.py":            "def clamp(v, mx):\n    return mx if v > mx else v\n",
			"tests/test_calc.py": "from calc import clamp\n\n\ndef test_clamp_high():\n    assert clamp(9, 5) == 5\n",
			".gitignore":         ".venv/\n",
		},
		fix:      map[string]string{"calc.py": "def clamp(v, mx):\n    return mx if v > mx else 0 if v < 0 else v\n"},
		test:     map[string]string{"tests/test_calc.py": "from calc import clamp\n\n\ndef test_clamp_high():\n    assert clamp(9, 5) == 5\n\n\ndef test_clamp_negative():\n    assert clamp(-1, 5) == 0\n"},
		prep:     `uv venv -q .venv --python /usr/local/bin/python3 && uv pip install -q --python .venv/bin/python pytest`,
		stage:    "python-test",
		evidence: "tests/test_calc.py::test_clamp_negative",
	},
	{
		name: "rust",
		base: map[string]string{
			"Cargo.toml":     "[package]\nname = \"calc\"\nversion = \"0.1.0\"\nedition = \"2021\"\n\n[dependencies]\nitoa = \"1\"\n",
			"src/lib.rs":     "pub fn clamp(v: i64, max: i64) -> i64 {\n    let _ = itoa::Buffer::new().format(v).len();\n    if v > max { max } else { v }\n}\n",
			"tests/clamp.rs": "#[test]\nfn clamp_high() {\n    assert_eq!(calc::clamp(9, 5), 5);\n}\n",
			".gitignore":     "target/\n",
		},
		fix:       map[string]string{"src/lib.rs": "pub fn clamp(v: i64, max: i64) -> i64 {\n    let _ = itoa::Buffer::new().format(v).len();\n    if v > max { max } else if v < 0 { 0 } else { v }\n}\n"},
		test:      map[string]string{"tests/clamp.rs": "#[test]\nfn clamp_high() {\n    assert_eq!(calc::clamp(9, 5), 5);\n}\n\n#[test]\nfn clamp_negative() {\n    assert_eq!(calc::clamp(-1, 5), 0);\n}\n"},
		prep:      `CARGO_NET_OFFLINE=false CARGO_HOME=$CACHE/cargo cargo generate-lockfile -q && CARGO_NET_OFFLINE=false CARGO_HOME=$CACHE/cargo cargo fetch -q`,
		lockfiles: []string{"Cargo.lock"},
		stage:     "cargo-test",
		evidence:  "clamp_negative",
	},
	{
		name: "maven",
		base: map[string]string{
			"pom.xml": `<project xmlns="http://maven.apache.org/POM/4.0.0"><modelVersion>4.0.0</modelVersion>
<groupId>t</groupId><artifactId>calc</artifactId><version>0</version>
<properties><maven.compiler.release>17</maven.compiler.release><project.build.sourceEncoding>UTF-8</project.build.sourceEncoding></properties>
<dependencies><dependency><groupId>junit</groupId><artifactId>junit</artifactId><version>4.13.2</version><scope>test</scope></dependency></dependencies>
<build><plugins><plugin><groupId>org.apache.maven.plugins</groupId><artifactId>maven-surefire-plugin</artifactId><version>3.2.5</version></plugin></plugins></build>
</project>
`,
			"src/main/java/calc/Calc.java":     "package calc;\n\npublic class Calc {\n  public static int clamp(int v, int max) { return v > max ? max : v; }\n}\n",
			"src/test/java/calc/CalcTest.java": "package calc;\n\nimport static org.junit.Assert.assertEquals;\nimport org.junit.Test;\n\npublic class CalcTest {\n  @Test public void clampHigh() { assertEquals(5, Calc.clamp(9, 5)); }\n}\n",
			".gitignore":                       "target/\n",
		},
		fix:      map[string]string{"src/main/java/calc/Calc.java": "package calc;\n\npublic class Calc {\n  public static int clamp(int v, int max) { return v > max ? max : v < 0 ? 0 : v; }\n}\n"},
		test:     map[string]string{"src/test/java/calc/CalcTest.java": "package calc;\n\nimport static org.junit.Assert.assertEquals;\nimport org.junit.Test;\n\npublic class CalcTest {\n  @Test public void clampHigh() { assertEquals(5, Calc.clamp(9, 5)); }\n  @Test public void clampNegative() { assertEquals(0, Calc.clamp(-1, 5)); }\n}\n"},
		prep:     `mvn -B -q -Dmaven.repo.local=$CACHE/m2 test`,
		stage:    "maven-test",
		evidence: "clampNegative",
	},
	{
		name: "gradle",
		base: map[string]string{
			"settings.gradle":                  "rootProject.name = 'calc'\n",
			"build.gradle":                     "plugins { id 'java' }\nrepositories { mavenCentral() }\ndependencies { testImplementation 'junit:junit:4.13.2' }\n",
			"src/main/java/calc/Calc.java":     "package calc;\n\npublic class Calc {\n  public static int clamp(int v, int max) { return v > max ? max : v; }\n}\n",
			"src/test/java/calc/CalcTest.java": "package calc;\n\nimport static org.junit.Assert.assertEquals;\nimport org.junit.Test;\n\npublic class CalcTest {\n  @Test public void clampHigh() { assertEquals(5, Calc.clamp(9, 5)); }\n}\n",
			".gitignore":                       "build/\n.gradle/\n",
		},
		fix:       map[string]string{"src/main/java/calc/Calc.java": "package calc;\n\npublic class Calc {\n  public static int clamp(int v, int max) { return v > max ? max : v < 0 ? 0 : v; }\n}\n"},
		test:      map[string]string{"src/test/java/calc/CalcTest.java": "package calc;\n\nimport static org.junit.Assert.assertEquals;\nimport org.junit.Test;\n\npublic class CalcTest {\n  @Test public void clampHigh() { assertEquals(5, Calc.clamp(9, 5)); }\n  @Test public void clampNegative() { assertEquals(0, Calc.clamp(-1, 5)); }\n}\n"},
		prep:      `export GRADLE_USER_HOME=$CACHE/gradle; gradle -q --no-daemon wrapper --gradle-version 8.10.2 && ./gradlew -q --no-daemon test`,
		lockfiles: []string{"gradlew", "gradle/wrapper/gradle-wrapper.properties", "gradle/wrapper/gradle-wrapper.jar"},
		stage:     "gradle-test",
		evidence:  "clampNegative",
	},
	{
		name: "ruby",
		base: map[string]string{
			"Gemfile":           "source \"https://rubygems.org\"\ngem \"minitest\"\n",
			"lib/calc.rb":       "module Calc\n  def self.clamp(v, max) = v > max ? max : v\nend\n",
			"test/calc_test.rb": "require \"minitest/autorun\"\nrequire \"calc\"\n\nclass CalcTest < Minitest::Test\n  def test_clamp_high = assert_equal(5, Calc.clamp(9, 5))\nend\n",
			".gitignore":        "vendor/\n.bundle/\n",
		},
		fix:       map[string]string{"lib/calc.rb": "module Calc\n  def self.clamp(v, max) = v > max ? max : (v < 0 ? 0 : v)\nend\n"},
		test:      map[string]string{"test/calc_test.rb": "require \"minitest/autorun\"\nrequire \"calc\"\n\nclass CalcTest < Minitest::Test\n  def test_clamp_high = assert_equal(5, Calc.clamp(9, 5))\n  def test_clamp_negative = assert_equal(0, Calc.clamp(-1, 5))\nend\n"},
		prep:      `bundle config set --local path vendor/bundle && bundle install --quiet`,
		lockfiles: []string{"Gemfile.lock"},
		stage:     "ruby-test",
		evidence:  "CalcTest#test_clamp_negative",
	},
	{
		name: "php",
		base: map[string]string{
			"composer.json":      `{"autoload":{"psr-4":{"Calc\\":"src/"}},"require-dev":{"phpunit/phpunit":"^11"}}` + "\n",
			"src/Calc.php":       "<?php\nnamespace Calc;\n\nfinal class Calc {\n    public static function clamp(int $v, int $max): int { return $v > $max ? $max : $v; }\n}\n",
			"tests/CalcTest.php": "<?php\nuse Calc\\Calc;\nuse PHPUnit\\Framework\\TestCase;\n\nfinal class CalcTest extends TestCase {\n    public function testClampHigh(): void { $this->assertSame(5, Calc::clamp(9, 5)); }\n}\n",
			".gitignore":         "vendor/\n",
		},
		fix:       map[string]string{"src/Calc.php": "<?php\nnamespace Calc;\n\nfinal class Calc {\n    public static function clamp(int $v, int $max): int { return $v > $max ? $max : ($v < 0 ? 0 : $v); }\n}\n"},
		test:      map[string]string{"tests/CalcTest.php": "<?php\nuse Calc\\Calc;\nuse PHPUnit\\Framework\\TestCase;\n\nfinal class CalcTest extends TestCase {\n    public function testClampHigh(): void { $this->assertSame(5, Calc::clamp(9, 5)); }\n    public function testClampNegative(): void { $this->assertSame(0, Calc::clamp(-1, 5)); }\n}\n"},
		prep:      `COMPOSER_HOME=$CACHE/composer composer install -q --no-interaction`,
		lockfiles: []string{"composer.lock"},
		stage:     "php-test",
		evidence:  "testClampNegative",
	},
	{
		name: "cmake",
		base: map[string]string{
			"CMakeLists.txt":    "cmake_minimum_required(VERSION 3.20)\nproject(calc C)\nenable_testing()\nadd_library(calc calc.c)\nadd_executable(test_calc tests/test_calc.c)\ntarget_link_libraries(test_calc calc)\nadd_test(NAME calc COMMAND test_calc)\n",
			"calc.h":            "int clamp(int v, int max);\n",
			"calc.c":            "#include \"calc.h\"\nint clamp(int v, int max) { return v > max ? max : v; }\n",
			"tests/test_calc.c": "#include \"../calc.h\"\nint main(void) { return clamp(9, 5) == 5 ? 0 : 1; }\n",
		},
		fix:      map[string]string{"calc.c": "#include \"calc.h\"\nint clamp(int v, int max) { return v > max ? max : v < 0 ? 0 : v; }\n"},
		test:     map[string]string{"tests/test_calc.c": "#include \"../calc.h\"\nint main(void) { return clamp(9, 5) == 5 && clamp(-1, 5) == 0 ? 0 : 1; }\n"},
		stage:    "ctest",
		evidence: "calc",
	},
	{
		// A language without a preset: the Makefile's test target.
		name: "make",
		base: map[string]string{
			"Makefile":      "test:\n\tsh tests/test.sh\n",
			"clamp.sh":      "clamp() { if [ \"$1\" -gt \"$2\" ]; then echo \"$2\"; else echo \"$1\"; fi; }\n",
			"tests/test.sh": ". ./clamp.sh\n[ \"$(clamp 9 5)\" = 5 ]\n",
		},
		fix:      map[string]string{"clamp.sh": "clamp() { if [ \"$1\" -gt \"$2\" ]; then echo \"$2\"; elif [ \"$1\" -lt 0 ]; then echo 0; else echo \"$1\"; fi; }\n"},
		test:     map[string]string{"tests/test.sh": ". ./clamp.sh\n[ \"$(clamp 9 5)\" = 5 ] && [ \"$(clamp -1 5)\" = 0 ]\n"},
		stage:    "make-test",
		evidence: "make-test",
	},
}

// TestPolyglotVerification runs every language preset in the real sandbox
// image, offline, against a project per language: the fixed change passes
// with its test stage run (not skipped), a change that only adds the
// reproduction test fails it, and behavioural evidence names the new test.
// Dependencies are installed once with the network (as a user does in their
// checkout) and cached under ~/.cache/bc-polyglot-test.
func TestPolyglotVerification(t *testing.T) {
	image := os.Getenv("BC_TEST_DOCKER_IMAGE")
	if image == "" || testing.Short() {
		t.Skip("set BC_TEST_DOCKER_IMAGE")
	}
	home, _ := os.UserHomeDir()
	cache := filepath.Join(home, ".cache", "bc-polyglot-test") // Docker Desktop cannot see /tmp
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	only := os.Getenv("BC_TEST_POLYGLOT")
	for _, c := range polyglotCases {
		t.Run(c.name, func(t *testing.T) {
			if only != "" && !slices.Contains(strings.Split(only, ","), c.name) {
				t.Skip("not selected by BC_TEST_POLYGLOT")
			}
			root, err := os.MkdirTemp(cache, c.name+"-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(root) })
			src, wt := filepath.Join(root, "checkout"), filepath.Join(root, "wt")
			base := c.base
			if c.prep != "" {
				writeFiles(t, src, base)
				writeFiles(t, src, c.fix)
				writeFiles(t, src, c.test)
				prepare(t, image, src, cache, c.prep)
				base = maps.Clone(base)
				for _, f := range c.lockfiles {
					b, err := os.ReadFile(filepath.Join(src, f))
					if err != nil {
						t.Fatalf("prep did not produce %s: %v", f, err)
					}
					base[f] = string(b)
				}
			} else {
				src = ""
			}
			writeFiles(t, wt, base)
			ctx := context.Background()
			for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "base"}} {
				if _, err := gitops.Run(ctx, wt, a...); err != nil {
					t.Fatal(err)
				}
			}
			if f := filepath.Join(wt, "gradlew"); fileExists(f) {
				_ = os.Chmod(f, 0o755)
			}
			baseSHA, _ := gitops.Run(ctx, wt, "rev-parse", "HEAD")
			e := &Engine{
				Sandbox:  &sandbox.Container{Engine: "docker", Image: image, Network: "none", PIDs: 4096, UID: os.Getuid(), GID: os.Getgid()}, // as in production
				CacheDir: filepath.Join(root, "build"),
				Packages: sandbox.PackageCaches{Cargo: filepath.Join(cache, "cargo"), Maven: filepath.Join(cache, "m2"), Gradle: filepath.Join(cache, "gradle")},
			}
			target := RepoTarget{Name: c.name, Worktree: wt, Base: strings.TrimSpace(baseSHA), TaskID: "t", Source: src}

			// Only the reproduction test: the test stage must fail.
			writeFiles(t, wt, c.test)
			res, err := e.Run(ctx, target, Targeted)
			if err != nil {
				t.Fatal(err)
			}
			if st := stageNamed(res, c.stage); st.Status != "fail" {
				t.Fatalf("test only: %s = %s, want fail\n%s", c.stage, st.Status, st.Output)
			}

			// With the fix: everything passes, and the test stage ran.
			writeFiles(t, wt, c.fix)
			res, err = e.Run(ctx, target, Targeted)
			if err != nil {
				t.Fatal(err)
			}
			for _, st := range res.Stages {
				if st.Status == "fail" || st.Status == "error" {
					t.Fatalf("fixed: %s = %s\n%s", st.Name, st.Status, st.Output)
				}
			}
			if st := stageNamed(res, c.stage); st.Status != "pass" {
				t.Fatalf("fixed: %s = %q, want pass (stages %v)\n%s", c.stage, st.Status, stageNames(res), st.Output)
			}
			ev, err := e.BehaviourEvidence(ctx, target)
			if err != nil {
				t.Fatal(err)
			}
			if !ev.Verified || !slices.ContainsFunc(ev.Tests, func(s string) bool { return strings.Contains(s, c.evidence) }) {
				t.Fatalf("evidence = %+v, want a test naming %q", ev, c.evidence)
			}
			t.Logf("evidence: %v", ev.Tests)
		})
	}
}

// prepare installs a project's dependencies in the sandbox image, with the
// network.
func prepare(t *testing.T, image, dir, cache, script string) {
	t.Helper()
	cmd := exec.Command("docker", "run", "--rm", "--network", "bridge",
		"-u", strconv.Itoa(os.Getuid())+":"+strconv.Itoa(os.Getgid()),
		"-v", dir+":"+dir, "-v", cache+":"+cache, "-w", dir,
		"-e", "HOME="+cache+"/home", "-e", "CACHE="+cache, "-e", "CARGO_NET_OFFLINE=false",
		image, "sh", "-c", "mkdir -p \"$HOME\" && "+script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("installing dependencies (needs the network): %v\n%s", err, out)
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func stageNamed(r Result, name string) StageResult {
	for _, s := range r.Stages {
		if s.Name == name {
			return s
		}
	}
	return StageResult{}
}

func stageNames(r Result) []string {
	var out []string
	for _, s := range r.Stages {
		out = append(out, s.Name+"="+s.Status)
	}
	return out
}
