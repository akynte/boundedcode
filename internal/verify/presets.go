package verify

import (
	"path"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/buildinfo"
	"github.com/akynte/boundedcode/internal/config"
)

// Built-in verification presets. Without a .boundedcode/verification.yaml,
// the stages are chosen from the files at the base commit, for every
// language found (a repository can have several). Each language's toolchain
// is in the sandbox image (adapters/openhands/Dockerfile); dependencies come
// from the host's package caches and the checkout's installed dependency
// directories (sandbox.PackageCaches, sandbox.DependencyMounts), never from
// the network.
//
// Every preset compiles the project where the language has a build step and
// runs its tests. Formatters and linters are only run for Go: their findings
// on unchanged code (a newer formatter, a new lint rule) would fail the
// untouched base. A stage whose dependencies are not installed fails with
// how to install them; skipping it would pass a change with no tests run.
//
// Repositories whose languages have no preset still get the project's own
// `make test` or `make check` when the Makefile has one.

// presetFor returns built-in stages for the languages found in files
// (repository-relative paths at the base commit). A repository whose root
// is no project of its own and has no Makefile, a monorepo of projects in
// subdirectories, gets each outermost project's stages, run in its
// directory.
//
// A root that runs tests of its own still gets the stages of nested
// projects in ecosystems the root's stages do not cover (a web/ package
// beside a root Go module) and of nested Go modules (./... stops at a
// module boundary): a polyglot repository is verified in every language,
// not only its root's.
func presetFor(files map[string]bool) Config {
	c := presetRoot(files)
	switch {
	case !c.runsTests() && !files["Makefile"]:
		c.Stages = append(c.Stages, subprojectStages(files)...)
	case c.runsTests() && !files["Makefile"]: // a root Makefile orchestrates its subprojects
		covered := map[string]bool{}
		for _, st := range c.Stages {
			covered[st.Name] = true
		}
		for _, st := range subprojectStages(files) {
			base := strings.TrimPrefix(st.Name, st.Dir+"/")
			if !covered[base] || strings.HasPrefix(base, "go-") && files[path.Join(st.Dir, "go.mod")] {
				c.Stages = append(c.Stages, st)
			}
		}
	}
	return c
}

// presetRoot returns the built-in stages for a project at the root of files.
func presetRoot(files map[string]bool) Config {
	c := Config{Version: 1, preset: true}
	add := func(st ...Stage) {
		for i := range st {
			st[i].preset = true
		}
		c.Stages = append(c.Stages, st...)
	}
	anySuffix := func(suffix ...string) bool {
		for f := range files {
			for _, s := range suffix {
				if strings.HasSuffix(f, s) {
					return true
				}
			}
		}
		return false
	}
	if files["go.mod"] {
		add(
			// Only the Go files the change touched: a file already
			// unformatted at the base is not the change's doing.
			Stage{Name: "gofmt", Run: []string{"sh", "-c", gofmtChanged, "gofmt", "{changed}"}, Requires: []string{"go.mod"}},
			Stage{Name: "go-build", Run: []string{"go", "build", "./..."}, Requires: []string{"go.mod"}},
			Stage{Name: "go-vet", Run: []string{"go", "vet", "{packages}"}, Requires: []string{"go.mod"}},
			Stage{Name: "go-test", Run: []string{"go", "test", "-count=1", "{packages}"}, Requires: []string{"go.mod"}, Timeout: config.Duration(20 * time.Minute), Tests: true},
			Stage{Name: "golangci-lint", Run: []string{"golangci-lint", "run", "./..."}, Scope: "full", Optional: true, Requires: []string{"go.mod"}},
		)
	}
	if files["package.json"] {
		// Dependencies are never installed by verification (no network): they
		// come read-only from the repository's own checkout (see
		// sandbox.DependencyMounts). A script of a project that declares
		// dependencies, with none installed, fails: skipping it would pass the
		// change with no tests run. A project that declares no dependencies
		// needs no node_modules. A script the project lacks is skipped.
		const missingDeps = `{ echo "node_modules missing: install the project's dependencies in the repository checkout (e.g. npm ci) so verification can run them"; exit 1; }`
		npmScript := func(name string) string {
			// Exit 3: no such script; 4: dependencies are declared.
			return `node -e 'const p=require("./package.json"); if(!p.scripts?.["` + name + `"]) process.exit(3); ` +
				`process.exit(["dependencies","devDependencies","optionalDependencies"].some(k=>Object.keys(p[k]||{}).length) ? 4 : 0)'; rc=$?; ` +
				`[ $rc = 3 ] && { echo "no ` + name + ` script"; exit 127; }; [ $rc = 0 ] || [ $rc = 4 ] || exit $rc; ` +
				`[ $rc = 4 ] && [ ! -d node_modules ] && ` + missingDeps + `; npm run --silent ` + name
		}
		tsc := `if [ -x node_modules/.bin/tsc ]; then node_modules/.bin/tsc --noEmit; ` +
			`elif [ ! -d node_modules ] && grep -q '"typescript"' package.json; then ` + missingDeps + `; ` +
			`else echo "tsc not installed"; exit 127; fi`
		add(
			Stage{Name: "tsc", Run: []string{"sh", "-c", tsc}, Optional: true, Requires: []string{"tsconfig.json"}},
			Stage{Name: "npm-lint", Run: []string{"sh", "-c", npmScript("lint")}, Optional: true, Requires: []string{"package.json"}},
			Stage{Name: "npm-test", Run: []string{"sh", "-c", npmScript("test")}, Optional: true, Requires: []string{"package.json"}, Timeout: config.Duration(20 * time.Minute), Tests: true},
			Stage{Name: "npm-build", Run: []string{"sh", "-c", npmScript("build")}, Scope: "full", Optional: true, Requires: []string{"package.json"}},
		)
	}
	if req := pythonMarker(files); req != nil {
		add(Stage{Name: "python-test", Run: []string{"sh", "-c", pythonTest}, Requires: req, Timeout: config.Duration(20 * time.Minute), Tests: true})
	}
	if files["Cargo.toml"] {
		add(
			Stage{Name: "cargo-build", Run: []string{"sh", "-c", rustToolchain + "cargo build --workspace"}, Requires: []string{"Cargo.toml"}, Timeout: config.Duration(20 * time.Minute)},
			Stage{Name: "cargo-test", Run: []string{"sh", "-c", rustToolchain + "cargo test --workspace --no-fail-fast"}, Requires: []string{"Cargo.toml"}, Timeout: config.Duration(30 * time.Minute), Tests: true},
		)
	}
	switch {
	case files["pom.xml"]:
		add(
			Stage{Name: "maven-compile", Run: []string{"sh", "-c", need("mvn") + pickJDK + "mvn -q test-compile"}, Requires: []string{"pom.xml"}, Timeout: config.Duration(20 * time.Minute)},
			Stage{Name: "maven-test", Run: []string{"sh", "-c", need("mvn") + pickJDK + "mvn -fae test"}, Requires: []string{"pom.xml"}, Timeout: config.Duration(30 * time.Minute), Tests: true},
		)
	case files["build.gradle"] || files["build.gradle.kts"] || files["settings.gradle"] || files["settings.gradle.kts"]:
		req := firstPresent(files, "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts")
		add(
			Stage{Name: "gradle-compile", Run: []string{"sh", "-c", pickJDK + gradle("testClasses")}, Requires: req, Timeout: config.Duration(20 * time.Minute)},
			Stage{Name: "gradle-test", Run: []string{"sh", "-c", pickJDK + gradle("test")}, Requires: req, Timeout: config.Duration(30 * time.Minute), Tests: true},
		)
	}
	// A Makefile or configure script of a repository in another language is
	// not necessarily how it builds its C code (and its default target can
	// do anything); only C/C++ repositories are built with them.
	cOnly := anySuffix(".c", ".cc", ".cpp", ".cxx", ".h", ".hpp") && len(c.Stages) == 0
	switch {
	case files["CMakeLists.txt"]:
		add(
			Stage{Name: "cmake-build", Run: []string{"sh", "-c", need("cmake") + cmakeBuild}, Requires: []string{"CMakeLists.txt"}, Timeout: config.Duration(20 * time.Minute)},
			Stage{Name: "ctest", Run: []string{"sh", "-c", need("cmake") + cmakeBuild + ` && ctest --test-dir "$b" --output-on-failure -j "$(nproc)"`}, Requires: []string{"CMakeLists.txt"}, Timeout: config.Duration(30 * time.Minute), Tests: true},
		)
	case files["meson.build"]:
		add(
			Stage{Name: "meson-build", Run: []string{"sh", "-c", need("meson") + mesonBuild}, Requires: []string{"meson.build"}, Timeout: config.Duration(20 * time.Minute)},
			Stage{Name: "meson-test", Run: []string{"sh", "-c", need("meson") + mesonBuild + ` && meson test -C "$b" --print-errorlogs`}, Requires: []string{"meson.build"}, Timeout: config.Duration(30 * time.Minute), Tests: true},
		)
	case (files["configure.ac"] || files["configure.in"] || files["configure"]) && !files["Makefile"] && cOnly:
		req := firstPresent(files, "configure.ac", "configure.in", "configure")
		add(
			Stage{Name: "autotools-build", Run: []string{"sh", "-c", autotoolsBuild}, Requires: req, Timeout: config.Duration(20 * time.Minute)},
			Stage{Name: "make-check", Run: []string{"sh", "-c", autotoolsBuild + ` && make check`}, Requires: req, Timeout: config.Duration(30 * time.Minute), Tests: true},
		)
	case files["Makefile"] && cOnly:
		add(Stage{Name: "make-build", Run: []string{"sh", "-c", `make -j"$(nproc)"`}, Requires: []string{"Makefile"}, Timeout: config.Duration(20 * time.Minute)})
	}
	if files["Gemfile"] || files["Rakefile"] || anySuffix(".gemspec") && hasRoot(files, ".gemspec") {
		req := firstPresent(files, "Gemfile", "Rakefile")
		add(Stage{Name: "ruby-test", Run: []string{"sh", "-c", need("ruby") + rubyTest}, Requires: req, Timeout: config.Duration(20 * time.Minute), Tests: true})
	}
	if files["composer.json"] {
		add(Stage{Name: "php-test", Run: []string{"sh", "-c", need("php") + phpTest}, Requires: []string{"composer.json"}, Timeout: config.Duration(20 * time.Minute), Tests: true})
	}
	if anySuffix(".tf") {
		// Offline checks only: validate/plan need providers and credentials.
		add(Stage{Name: "terraform-fmt", Optional: true,
			Run: []string{"sh", "-c", `command -v terraform >/dev/null || exit 127; terraform fmt -check -recursive -diff`}})
	}
	if anySuffix("Chart.yaml") {
		add(Stage{Name: "helm-lint", Optional: true,
			Run: []string{"sh", "-c", `command -v helm >/dev/null || exit 127; for c in $(git ls-files '*Chart.yaml'); do helm lint "$(dirname "$c")" || exit 1; done`}})
	}
	if !c.runsTests() && files["Makefile"] {
		// Languages without a preset: the project's own test target, if any.
		add(Stage{Name: "make-test", Run: []string{"sh", "-c", makeTest}, Optional: true, Requires: []string{"Makefile"}, Timeout: config.Duration(30 * time.Minute), Tests: true})
	}
	return c
}

// gofmtChanged lists the change's Go files (its arguments) that gofmt
// would rewrite; deleted and vendored files are skipped.
const gofmtChanged = `set -- $(for f in "$@"; do case "$f" in vendor/*) ;; *.go) [ -f "$f" ] && printf '%s\n' "$f";; esac; done); ` +
	`[ $# -eq 0 ] && exit 0; out=$(gofmt -l "$@" 2>&1); [ -z "$out" ] || { echo "files need gofmt:"; echo "$out"; exit 1; }`

// projectManifests make a directory a project of its own.
var projectManifests = map[string]bool{
	"go.mod": true, "package.json": true, "Cargo.toml": true, "pom.xml": true, "build.gradle": true, "build.gradle.kts": true,
	"settings.gradle": true, "settings.gradle.kts": true, "pyproject.toml": true, "setup.py": true, "setup.cfg": true,
	"Gemfile": true, "composer.json": true, "CMakeLists.txt": true, "meson.build": true, "Makefile": true, "configure.ac": true,
}

const (
	maxSubprojects     = 16 // projects given stages, in path order
	maxSubprojectDepth = 3  // directory levels below the root
)

// notProjectDirs hold other people's code or test material, not projects
// of the repository.
var notProjectDirs = map[string]bool{"vendor": true, "node_modules": true, "third_party": true, "testdata": true, "fixtures": true}

// subprojectStages finds the outermost projects below the root and returns
// their built-in stages, named and run in their directory. Checks that
// already cover the whole tree from the root (Terraform, Helm) are left out.
func subprojectStages(files map[string]bool) []Stage {
	dirs := map[string]bool{}
	for f := range files {
		dir, base := path.Split(f)
		dir = strings.TrimSuffix(dir, "/")
		if dir == "" || !projectManifests[base] || strings.Count(dir, "/")+1 > maxSubprojectDepth {
			continue
		}
		skip := false
		for _, part := range strings.Split(dir, "/") {
			if notProjectDirs[part] || strings.HasPrefix(part, ".") {
				skip = true
				break
			}
		}
		if !skip {
			dirs[dir] = true
		}
	}
	var outer []string
	for d := range dirs {
		nested := false
		for a := path.Dir(d); a != "."; a = path.Dir(a) {
			if dirs[a] {
				nested = true
				break
			}
		}
		if !nested {
			outer = append(outer, d)
		}
	}
	sort.Strings(outer)
	if len(outer) > maxSubprojects {
		outer = outer[:maxSubprojects]
	}
	var out []Stage
	for _, d := range outer {
		sub := map[string]bool{}
		for f := range files {
			if rest, ok := strings.CutPrefix(f, d+"/"); ok {
				sub[rest] = true
			}
		}
		for _, st := range presetRoot(sub).Stages {
			if len(st.Requires) == 0 {
				continue
			}
			st.Dir, st.Name = d, d+"/"+st.Name
			out = append(out, st)
		}
	}
	return out
}

// runsTests reports whether a config has a stage that runs tests.
func (c Config) runsTests() bool {
	for _, st := range c.Stages {
		if isTestStage(st) {
			return true
		}
	}
	return false
}

func firstPresent(files map[string]bool, names ...string) []string {
	for _, n := range names {
		if files[n] {
			return []string{n}
		}
	}
	return nil
}

// hasRoot reports a file with the suffix at the repository root.
func hasRoot(files map[string]bool, suffix string) bool {
	for f := range files {
		if !strings.Contains(f, "/") && strings.HasSuffix(f, suffix) {
			return true
		}
	}
	return false
}

// pythonMarker returns the python-test stage's required file, or nil when
// the repository has no Python tests. Projects are recognised by their
// packaging or test configuration at the root, but the stage needs Python
// test files: a Go or JavaScript repository with a requirements.txt for its
// tooling must not fail for a missing virtual environment.
func pythonMarker(files map[string]bool) []string {
	hasTests := false
	for f := range files {
		if pythonTestFile(f) {
			hasTests = true
			break
		}
	}
	if !hasTests {
		return nil
	}
	if req := firstPresent(files, "pyproject.toml", "setup.py", "setup.cfg", "tox.ini", "pytest.ini", "requirements.txt", "Pipfile", "conftest.py"); req != nil {
		return req
	}
	return []string{} // a test file, no packaging: still Python tests
}

// pythonTestFile reports a pytest/unittest test module.
func pythonTestFile(f string) bool {
	b := path.Base(f)
	if !strings.HasSuffix(b, ".py") {
		return false
	}
	return strings.HasPrefix(b, "test_") || strings.HasSuffix(b, "_test.py") || b == "tests.py" || b == "conftest.py"
}

// need fails a stage whose tool is missing from the sandbox image (one built
// before the tool was added).
func need(tool string) string {
	return `command -v ` + tool + ` >/dev/null 2>&1 || { echo "` + tool + ` is not installed in the sandbox image; rebuild it with ` +
		"`" + buildinfo.Command() + " sandbox build`" + `"; exit 127; }; `
}

// buildDir is a per-task build directory for this source tree, outside it
// (BC_BUILD_DIR, see sandbox.PackageCaches.Apply): the evidence check builds
// several trees of one task, and a CMake build directory belongs to one.
func buildDir(kind string) string {
	return `b="${BC_BUILD_DIR:-${TMPDIR:-/tmp}/bc-build}/` + kind + `-$(pwd | cksum | cut -d' ' -f1)"; mkdir -p "$b" || exit 1; `
}

// pythonTest runs pytest, or unittest when the project does not use pytest,
// with the checkout's virtual environment (.venv or venv) when it has one.
// The tree under test comes first on the import path, so a copy of the
// project installed in the venv (editable or not) is never what is tested.
const pythonTest = `py=; ` +
	`base=/usr/local/bin/python3; [ -x "$base" ] || base=$(command -v python3); ` +
	`if [ -d src ] && [ ! -f src/__init__.py ]; then export PYTHONPATH="$PWD/src"; else export PYTHONPATH="$PWD"; fi; ` +
	`for v in .venv venv; do [ -f "$v/pyvenv.cfg" ] || continue; ` +
	`if "$v/bin/python" -c '' 2>/dev/null; then py="$PWD/$v/bin/python"; export VIRTUAL_ENV="$PWD/$v" PATH="$PWD/$v/bin:$PATH"; break; fi; ` +
	// The venv's interpreter is not in the sandbox (a system Python on the
	// host): the image's Python can use its packages if the versions match.
	`sp=$(ls -d "$v"/lib/python3.*/site-packages 2>/dev/null | head -n1); ` +
	`iv=$("$base" -c 'import sys; print("python%d.%d" % sys.version_info[:2])'); ` +
	`if [ -n "$sp" ] && [ "$(basename "$(dirname "$sp")")" = "$iv" ]; then py=$base; export PYTHONPATH="$PYTHONPATH:$PWD/$sp" PATH="$PWD/$v/bin:$PATH"; break; fi; ` +
	`echo "the checkout's $v uses a Python that is not in the sandbox ($(sed -n 's/^home *= *//p' "$v/pyvenv.cfg")); recreate it with uv (uv venv --python 3.X), which installs a self-contained Python verification can mount"; exit 1; done; ` +
	`[ -n "$py" ] || py=$base; ` +
	`if "$py" -c 'import pytest' 2>/dev/null; then "$py" -m pytest -p no:cacheprovider -rfE; rc=$?; ` +
	`[ $rc = 5 ] && { echo "no tests collected"; exit 127; }; exit $rc; fi; ` +
	`if [ -f conftest.py ] || grep -qsi pytest pyproject.toml setup.cfg setup.py tox.ini pytest.ini requirements*.txt requirements/*.txt Pipfile; then ` +
	`echo "pytest is not installed: create a virtual environment with the project's test dependencies in the repository checkout (e.g. uv venv && uv pip install -e . pytest, or uv pip install -r requirements.txt)"; exit 1; fi; ` +
	`set -- -v; [ -d tests ] && set -- -v -s tests -t .; ` +
	`"$py" -m unittest discover "$@"; rc=$?; [ $rc = 5 ] && { echo "no tests ran"; exit 127; }; exit $rc`

// rustToolchain falls back to the image's toolchain when the project pins
// one that is not installed (installing it needs the network).
const rustToolchain = `command -v cargo >/dev/null 2>&1 || { echo "cargo is not installed in the sandbox image; rebuild it"; exit 1; }; ` +
	`if [ -f rust-toolchain.toml ] || [ -f rust-toolchain ]; then rustup which cargo >/dev/null 2>&1 || { ` +
	`d=$(rustup default 2>/dev/null | cut -d' ' -f1); echo "note: the toolchain the project pins is not installed; using $d"; export RUSTUP_TOOLCHAIN=$d; }; fi; `

// pickJDK selects the JDK a Java project builds with: what its Gradle
// wrapper version runs on, or the Java level its pom declares (11 for 8 and
// earlier, which newer JDKs no longer compile for, 17, or 21).
const pickJDK = `v=21; ` +
	`if [ -f gradle/wrapper/gradle-wrapper.properties ]; then ` +
	`set -- $(sed -n 's/.*gradle-\([0-9][0-9]*\)\.\([0-9][0-9]*\).*/\1 \2/p' gradle/wrapper/gradle-wrapper.properties | head -n1); ` +
	`if [ -n "$1" ]; then if [ "$1" -lt 7 ] || { [ "$1" -eq 7 ] && [ "$2" -lt 3 ]; }; then v=11; ` +
	`elif [ "$1" -lt 8 ] || { [ "$1" -eq 8 ] && [ "$2" -lt 5 ]; }; then v=17; fi; fi; ` +
	`elif [ -f pom.xml ]; then ` +
	`r=$(grep -o -E '<(maven\.compiler\.(release|source|target)|java\.version|release|source)>[0-9.]+<' pom.xml | head -n1 | sed -E 's/.*>([0-9.]+)<$/\1/; s/^1\.//; s/\..*//'); ` +
	`if [ -n "$r" ]; then if [ "$r" -le 11 ]; then v=11; elif [ "$r" -le 17 ]; then v=17; fi; fi; fi; ` +
	`if [ -d /opt/jdk/$v ]; then export JAVA_HOME=/opt/jdk/$v PATH=/opt/jdk/$v/bin:$PATH; fi; `

// gradle runs a Gradle task offline with the project's wrapper (its
// distribution comes from the host, see sandbox.PrepareGradleWrapper), or
// the image's Gradle when the project has none.
func gradle(task string) string {
	return `if [ -f gradlew ]; then g="sh ./gradlew"; else ` + need("gradle") + `g=gradle; fi; ` +
		`$g --offline --no-daemon --console=plain ` + task
}

var cmakeBuild = buildDir("cmake") +
	`{ [ -f "$b/CMakeCache.txt" ] || cmake -S . -B "$b" -G Ninja -DCMAKE_BUILD_TYPE=Debug -DBUILD_TESTING=ON; } && cmake --build "$b" -j "$(nproc)"`

var mesonBuild = buildDir("meson") +
	`{ [ -f "$b/build.ninja" ] || meson setup "$b"; } && meson compile -C "$b"`

// autotoolsBuild builds out of tree. Regenerating configure writes into the
// tree; verification undoes that afterwards.
var autotoolsBuild = buildDir("autotools") + `src=$PWD; ` +
	`{ [ -x configure ] || { command -v autoreconf >/dev/null 2>&1 || { echo "autoreconf is not installed in the sandbox image; rebuild it"; exit 1; }; autoreconf -fi; }; } && ` +
	`cd "$b" && { [ -f Makefile ] || "$src/configure"; } && make -j"$(nproc)"`

// makeTest runs the Makefile's test (or check) target.
const makeTest = `for t in test check; do if grep -qE "^$t[[:space:]]*:" Makefile; then make $t; exit $?; fi; done; echo "no test or check target"; exit 127`

const rubyTest = `run=; ` +
	`if [ -f Gemfile ]; then ` +
	`if [ -z "$BUNDLE_PATH" ] && [ -d vendor/bundle ] && [ ! -f .bundle/config ]; then export BUNDLE_PATH="$PWD/vendor/bundle"; fi; ` +
	`bundle check >/dev/null 2>&1 || { bundle check 2>&1 | tail -n 5; echo "gems missing: install them in the repository checkout (bundle config set --local path vendor/bundle && bundle install) so verification can use them"; exit 1; }; ` +
	`run="bundle exec"; fi; ` +
	`if [ -d spec ] && { [ -f .rspec ] || grep -qs rspec Gemfile Gemfile.lock *.gemspec; }; then $run rspec; exit $?; fi; ` +
	`if [ -f Rakefile ] && $run rake -P 2>/dev/null | grep -q '^rake test$'; then $run rake test; exit $?; fi; ` +
	`if [ -d test ]; then $run ruby -Ilib -Itest -e 'Dir.glob("test/**/{*_test,test_*}.rb").sort.each { |f| require File.expand_path(f) }'; exit $?; fi; ` +
	`echo "no tests"; exit 127`

const phpTest = `set --; [ -f phpunit.xml ] || [ -f phpunit.xml.dist ] || [ -f phpunit.dist.xml ] || { [ -d tests ] && set -- tests; }; ` +
	`if [ -f vendor/bin/phpunit ]; then php vendor/bin/phpunit "$@"; exit $?; fi; ` +
	`if [ -f vendor/bin/pest ]; then php vendor/bin/pest; exit $?; fi; ` +
	`if grep -qsE '"(phpunit/phpunit|pestphp/pest)"' composer.json; then echo "vendor/ missing: install the project's dependencies in the repository checkout (composer install) so verification can run them"; exit 1; fi; ` +
	`echo "no test runner"; exit 127`
