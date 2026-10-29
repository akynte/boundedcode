// Package ecosystem finds the buildable projects in a directory tree, in
// any language: where each one is, what builds it, how it is built and
// tested, and whether the tool that does it is installed. A repository may
// hold several (a Go service beside a TypeScript front end, a Cargo
// workspace, a Maven multi-module build); members of a workspace are
// counted under it rather than listed one by one.
//
// Detection reads directory names and a few manifests (capped), never runs
// project code, and does not follow symbolic links. It is a starting point
// for a model or a person, not a guarantee: a project with an unusual
// layout may be missed, and a command may need set-up the manifest does
// not show. When nothing is recognised the report still names the CI
// configuration, which says how the project is built.
package ecosystem

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

// Project is one buildable unit.
type Project struct {
	Dir     string   // slash-separated, relative to the root; "." for the root
	Kind    string   // "Go module", "Node package", "Rust workspace", …
	Name    string   // from the manifest, when it has one
	Manager string   // the package manager, when the ecosystem has several
	Build   string   // command run in Dir; "" when none is known
	Test    string   // command run in Dir; "" when none is known
	Other   []string // further commands, "lint: npm run lint"
	Tool    string   // the executable the commands need
	Missing bool     // Tool is not installed (not on PATH, no wrapper script)
	Members int      // nested projects of the same build counted under this one
	Note    string   // a detail, such as the targets of a Makefile
	// System is the build system's ID ("go", "node", "maven"); Languages
	// are the internal/lang IDs its code is in (none for a Makefile).
	System    string
	Languages []string

	group      string // projects fold into an aggregate of the same group
	aggregates bool
}

// Report is what Detect found.
type Report struct {
	Projects  []Project // shallowest first, at most Options.MaxProjects
	More      int       // projects found beyond the cap
	CI        []string  // CI configuration at the root, slash-separated
	Truncated bool      // the walk stopped at its depth or entry budget
}

// Options bound the walk.
type Options struct {
	MaxDepth    int // directory levels below the root; default 4
	MaxEntries  int // directory entries read; default 20000
	MaxProjects int // projects reported; default 12
	// LookPath finds an executable; default exec.LookPath.
	LookPath func(string) (string, error)
}

// skipDirs are dependency, build and generated-output directories, and
// fixtures that hold projects only for tests. A Python virtual environment
// is recognised by its pyvenv.cfg, whatever its name.
var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "third_party": true, "target": true, "dist": true, "build": true, "out": true,
	"bin": true, "obj": true, "__pycache__": true, "site-packages": true, "bower_components": true,
	"testdata": true, "vendor-bin": true, "_build": true, "deps": true, "Pods": true, "DerivedData": true, "zig-cache": true, "zig-out": true,
}

const manifestCap = 64 << 10

// Detect reports the projects under root.
func Detect(root string, opt Options) Report {
	if opt.MaxDepth <= 0 {
		opt.MaxDepth = 4
	}
	if opt.MaxEntries <= 0 {
		opt.MaxEntries = 20000
	}
	if opt.MaxProjects <= 0 {
		opt.MaxProjects = 12
	}
	if opt.LookPath == nil {
		opt.LookPath = exec.LookPath
	}
	d := &detector{root: root, opt: opt, byDir: map[string][]*Project{}, found: map[string]bool{}}
	d.visit(".", 0)
	rep := Report{CI: ciFiles(root), Truncated: d.truncated}
	sort.SliceStable(d.all, func(i, j int) bool {
		di, dj := depth(d.all[i].Dir), depth(d.all[j].Dir)
		if di != dj {
			return di < dj
		}
		return d.all[i].Dir < d.all[j].Dir
	})
	for i, p := range d.all {
		if i == opt.MaxProjects {
			rep.More = len(d.all) - i
			break
		}
		rep.Projects = append(rep.Projects, *p)
	}
	return rep
}

func depth(dir string) int {
	if dir == "." {
		return 0
	}
	return strings.Count(dir, "/") + 1
}

type detector struct {
	root      string
	opt       Options
	entries   int
	truncated bool
	all       []*Project
	byDir     map[string][]*Project
	found     map[string]bool // LookPath results
}

func (d *detector) visit(rel string, level int) {
	abs := filepath.Join(d.root, filepath.FromSlash(rel))
	ents, err := os.ReadDir(abs)
	if err != nil {
		return
	}
	d.entries += len(ents)
	names := map[string]bool{}
	var subdirs []string
	for _, e := range ents {
		n := e.Name()
		names[n] = true
		// Symbolic links are not followed: e.IsDir is false for them.
		if e.IsDir() && !strings.HasPrefix(n, ".") && !skipDirs[n] {
			subdirs = append(subdirs, n)
		}
	}
	if names["pyvenv.cfg"] {
		return
	}
	for _, p := range d.detectDir(abs, rel, names) {
		d.add(p)
	}
	if d.entries >= d.opt.MaxEntries {
		d.truncated = true
		return
	}
	if level >= d.opt.MaxDepth {
		if len(subdirs) > 0 {
			d.truncated = true
		}
		return
	}
	sort.Strings(subdirs)
	for _, s := range subdirs {
		if d.entries >= d.opt.MaxEntries {
			d.truncated = true
			return
		}
		d.visit(path.Join(rel, s), level+1)
	}
}

// add keeps p, or counts it under an aggregating ancestor of its group.
func (d *detector) add(p *Project) {
	if p.group != "" {
		for dir := p.Dir; dir != "."; {
			dir = path.Dir(dir)
			for _, q := range d.byDir[dir] {
				if q.group == p.group && q.aggregates {
					q.Members++
					return
				}
			}
		}
	}
	d.byDir[p.Dir] = append(d.byDir[p.Dir], p)
	d.all = append(d.all, p)
}

// have reports whether tool is installed; the result is cached.
func (d *detector) have(tool string) bool {
	ok, seen := d.found[tool]
	if !seen {
		_, err := d.opt.LookPath(tool)
		ok = err == nil
		d.found[tool] = ok
	}
	return ok
}

// needs sets the tool a project's commands run and whether it is missing.
func (d *detector) needs(p *Project, tool string) {
	p.Tool = tool
	p.Missing = !d.have(tool)
}

// wrapper returns the wrapper script in dir or an ancestor (./gradlew,
// ./mvnw), relative to dir, or "".
func (d *detector) wrapper(abs, unix, windows string) string {
	name := unix
	if runtime.GOOS == "windows" {
		name = windows
	}
	rootAbs := filepath.Clean(d.root)
	prefix := "./"
	for dir := abs; ; dir = filepath.Dir(dir) {
		if st, err := os.Stat(filepath.Join(dir, name)); err == nil && st.Mode().IsRegular() {
			if runtime.GOOS == "windows" {
				return filepath.FromSlash(prefix) + name
			}
			return prefix + name
		}
		if filepath.Clean(dir) == rootAbs || filepath.Dir(dir) == dir {
			return ""
		}
		if prefix == "./" {
			prefix = "../"
		} else {
			prefix += "../"
		}
	}
}

// inAncestors reports a file named name in dir or one of its ancestors up
// to the root.
func (d *detector) inAncestors(abs, name string) bool {
	rootAbs := filepath.Clean(d.root)
	for dir := abs; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
		if filepath.Clean(dir) == rootAbs || filepath.Dir(dir) == dir {
			return false
		}
	}
}

func read(abs, name string) string {
	f, err := os.Open(filepath.Join(abs, name))
	if err != nil {
		return ""
	}
	defer f.Close()
	b := make([]byte, manifestCap)
	n, _ := io.ReadFull(f, b)
	return string(b[:n])
}

var (
	goModuleRE  = regexp.MustCompile(`(?m)^module\s+(\S+)`)
	tomlNameRE  = regexp.MustCompile(`(?m)^name\s*=\s*["']([^"']+)["']`)
	cargoPkgRE  = regexp.MustCompile(`(?ms)^\[package\][^\[]*?^name\s*=\s*"([^"]+)"`)
	tomlTableRE = func(table string) *regexp.Regexp {
		return regexp.MustCompile(`(?m)^\[` + regexp.QuoteMeta(table) + `\]`)
	}
	cargoWsRE    = tomlTableRE("workspace")
	poetryRE     = tomlTableRE("tool.poetry")
	pubNameRE    = regexp.MustCompile(`(?m)^name:\s*(\S+)`)
	mixAppRE     = regexp.MustCompile(`app:\s*:(\w+)`)
	targetRE     = regexp.MustCompile(`^([A-Za-z][\w-]*)(?:\s+[^:=]*)?:(?:[^=]|$)`)
	pomModulesRE = regexp.MustCompile(`<modules>`)
)

// dirInfo is one directory being examined.
type dirInfo struct {
	abs, rel string
	names    map[string]bool
}

func (di dirInfo) has(n ...string) bool {
	for _, x := range n {
		if di.names[x] {
			return true
		}
	}
	return false
}

func (di dirInfo) anyExt(ext ...string) bool {
	for n := range di.names {
		for _, e := range ext {
			if strings.HasSuffix(n, e) {
				return true
			}
		}
	}
	return false
}

func (di dirInfo) read(name string) string { return read(di.abs, name) }

// BuildSystem is one way projects are built: how its projects are
// recognised, the languages they hold, the tools they run and the package
// registries those download from. Adding a build system is adding one;
// nothing else changes.
type BuildSystem struct {
	ID         string
	Languages  []string // internal/lang IDs
	Tools      []string // executables its commands run
	Registries []string // hosts its package managers download from
	// Wrappers decide which tool a build or test command runs (gradlew,
	// mvnw, .cargo/config.toml, .npmrc): a change to one can make tests
	// "pass" without running them. Paths relative to a project, a trailing
	// "/" for a directory.
	Wrappers []string
	// RunnerConfig configure what the test runner runs and skips
	// (pytest.ini, conftest.py, jest.config.*): a change to one is legitimate
	// sometimes, and needs a reviewer's eye before evidence is trusted. "*"
	// matches within a name.
	RunnerConfig []string
	detect       func(d *detector, di dirInfo) []*Project
}

// BuildSystems returns the known build systems, in detection order.
func BuildSystems() []BuildSystem { return append([]BuildSystem(nil), buildSystems...) }

// Registries returns the package registries a tool downloads from.
func Registries(tool string) []string {
	var out []string
	for _, bs := range buildSystems {
		for _, t := range bs.Tools {
			if t == tool {
				out = append(out, bs.Registries...)
			}
		}
	}
	return out
}

func one(p *Project) []*Project { return []*Project{p} }

var (
	pypi    = []string{"pypi.org", "files.pythonhosted.org"}
	npmjs   = []string{"registry.npmjs.org", "registry.yarnpkg.com", "repo.yarnpkg.com"}
	maven   = []string{"repo.maven.apache.org", "repo1.maven.org"}
	gradleR = []string{"repo.maven.apache.org", "repo1.maven.org", "plugins.gradle.org", "services.gradle.org", "downloads.gradle.org"}
)

var buildSystems = []BuildSystem{
	{ID: "go", RunnerConfig: []string{"go.work"}, Languages: []string{"go"}, Tools: []string{"go"}, Registries: []string{"proxy.golang.org", "sum.golang.org", "storage.googleapis.com"},
		detect: func(d *detector, di dirInfo) []*Project {
			if !di.has("go.mod") {
				return nil
			}
			p := &Project{Kind: "Go module", Build: "go build ./...", Test: "go test ./..."}
			if m := goModuleRE.FindStringSubmatch(di.read("go.mod")); m != nil {
				p.Name = m[1]
			}
			d.needs(p, "go")
			return one(p)
		}},
	{ID: "node", Wrappers: []string{".npmrc", ".yarnrc", ".yarnrc.yml", ".pnpmfile.cjs", "bunfig.toml", ".yarn/releases/"}, RunnerConfig: []string{"jest.config.*", "vitest.config.*", "vitest.workspace.*", ".mocharc*", "karma.conf.*", "playwright.config.*", "ava.config.*", ".c8rc*", ".nycrc*"}, Languages: []string{"javascript", "typescript"}, Tools: []string{"npm", "npx", "pnpm", "yarn", "bun"}, Registries: npmjs,
		detect: func(d *detector, di dirInfo) []*Project {
			if !di.has("package.json") {
				return nil
			}
			p := &Project{Kind: "Node package", group: "node"}
			d.node(p, di.abs)
			if fileIn(di.abs, "tsconfig.json") {
				p.Languages = []string{"typescript", "javascript"}
			}
			return one(p)
		}},
	{ID: "deno", RunnerConfig: []string{"deno.json", "deno.jsonc"}, Languages: []string{"typescript", "javascript"}, Tools: []string{"deno"}, Registries: []string{"deno.land", "jsr.io", "registry.npmjs.org"},
		detect: func(d *detector, di dirInfo) []*Project {
			if di.has("package.json") || !di.has("deno.json", "deno.jsonc") {
				return nil
			}
			p := &Project{Kind: "Deno project", Build: "deno check .", Test: "deno test"}
			d.needs(p, "deno")
			return one(p)
		}},
	// A requirements.txt alone marks a Python project at the root or beside
	// Python code, not a docs or tooling directory.
	{ID: "python", RunnerConfig: []string{"pytest.ini", "conftest.py", "tox.ini", "noxfile.py", ".coveragerc"}, Languages: []string{"python"}, Tools: []string{"pip", "pip3", "python", "python3", "uv", "poetry", "pipenv", "pdm"}, Registries: pypi,
		detect: func(d *detector, di dirInfo) []*Project {
			if !di.has("pyproject.toml", "setup.py", "setup.cfg", "Pipfile") && (!di.has("requirements.txt") || (di.rel != "." && !di.anyExt(".py"))) {
				return nil
			}
			p := &Project{Kind: "Python project"}
			d.python(p, di.abs, di.names)
			return one(p)
		}},
	{ID: "cargo", Wrappers: []string{".cargo/config", ".cargo/config.toml", "rust-toolchain", "rust-toolchain.toml"}, Languages: []string{"rust"}, Tools: []string{"cargo"}, Registries: []string{"index.crates.io", "static.crates.io", "crates.io"},
		detect: func(d *detector, di dirInfo) []*Project {
			if !di.has("Cargo.toml") {
				return nil
			}
			s := di.read("Cargo.toml")
			p := &Project{Kind: "Rust crate", Build: "cargo build", Test: "cargo test", group: "cargo"}
			if cargoWsRE.MatchString(s) {
				p.Kind, p.aggregates = "Rust workspace", true
			}
			if m := cargoPkgRE.FindStringSubmatch(s); m != nil {
				p.Name = m[1]
			}
			d.needs(p, "cargo")
			return one(p)
		}},
	{ID: "maven", Wrappers: []string{"mvnw", "mvnw.cmd", ".mvn/"}, Languages: []string{"java", "kotlin"}, Tools: []string{"mvn", "mvnw"}, Registries: maven,
		detect: func(d *detector, di dirInfo) []*Project {
			if !di.has("pom.xml") {
				return nil
			}
			p := &Project{Kind: "Maven project", group: "maven", aggregates: pomModulesRE.MatchString(di.read("pom.xml"))}
			mvn := d.wrapper(di.abs, "mvnw", "mvnw.cmd")
			if mvn == "" {
				mvn = "mvn"
				d.needs(p, "mvn")
			}
			p.Build, p.Test = mvn+" compile", mvn+" test"
			return one(p)
		}},
	{ID: "gradle", Wrappers: []string{"gradlew", "gradlew.bat", "gradle/wrapper/"}, RunnerConfig: []string{"gradle.properties"}, Languages: []string{"java", "kotlin", "groovy"}, Tools: []string{"gradle", "gradlew"}, Registries: gradleR,
		detect: func(d *detector, di dirInfo) []*Project {
			if !di.has("build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts") {
				return nil
			}
			p := &Project{Kind: "Gradle project", group: "gradle", aggregates: di.has("settings.gradle", "settings.gradle.kts")}
			gradle := d.wrapper(di.abs, "gradlew", "gradlew.bat")
			if gradle == "" {
				gradle = "gradle"
				d.needs(p, "gradle")
			}
			p.Build, p.Test = gradle+" assemble", gradle+" test"
			if di.has("build.gradle.kts", "settings.gradle.kts") {
				p.Languages = []string{"kotlin", "java"}
			}
			return one(p)
		}},
	{ID: "sbt", Languages: []string{"scala"}, Tools: []string{"sbt"}, Registries: maven,
		detect: func(d *detector, di dirInfo) []*Project {
			if !di.has("build.sbt") {
				return nil
			}
			p := &Project{Kind: "sbt project", Build: "sbt compile", Test: "sbt test"}
			d.needs(p, "sbt")
			return one(p)
		}},
	{ID: "dotnet", Wrappers: []string{"global.json", "Directory.Build.rsp"}, RunnerConfig: []string{"*.runsettings", "Directory.Build.props", "Directory.Build.targets"}, Languages: []string{"csharp", "fsharp"}, Tools: []string{"dotnet"}, Registries: []string{"api.nuget.org"},
		detect: func(d *detector, di dirInfo) []*Project {
			sln := di.anyExt(".sln", ".slnx")
			if !sln && !di.anyExt(".csproj", ".fsproj", ".vbproj") {
				return nil
			}
			p := &Project{Kind: ".NET project", Build: "dotnet build", Test: "dotnet test", group: "dotnet", aggregates: sln}
			if sln {
				p.Kind = ".NET solution"
			}
			if di.anyExt(".fsproj") && !di.anyExt(".csproj") {
				p.Languages = []string{"fsharp"}
			}
			d.needs(p, "dotnet")
			return one(p)
		}},
	{ID: "bundler", Wrappers: []string{".bundle/config"}, RunnerConfig: []string{".rspec", "Rakefile"}, Languages: []string{"ruby"}, Tools: []string{"bundle", "gem"}, Registries: []string{"rubygems.org", "index.rubygems.org"},
		detect: func(d *detector, di dirInfo) []*Project {
			if !di.has("Gemfile") {
				return nil
			}
			p := &Project{Kind: "Ruby project", Test: "bundle exec rake test"}
			if di.has("spec", ".rspec") {
				p.Test = "bundle exec rspec"
			}
			d.needs(p, "bundle")
			return one(p)
		}},
	{ID: "composer", RunnerConfig: []string{"phpunit.xml", "phpunit.xml.dist", "phpunit.dist.xml"}, Languages: []string{"php"}, Tools: []string{"composer"}, Registries: []string{"repo.packagist.org", "packagist.org"},
		detect: func(d *detector, di dirInfo) []*Project {
			if !di.has("composer.json") {
				return nil
			}
			p := &Project{Kind: "PHP project"}
			d.php(p, di.abs, di.names)
			return one(p)
		}},
	{ID: "mix", Languages: []string{"elixir"}, Tools: []string{"mix"}, Registries: []string{"repo.hex.pm", "hex.pm"},
		detect: func(d *detector, di dirInfo) []*Project {
			if !di.has("mix.exs") {
				return nil
			}
			s := di.read("mix.exs")
			p := &Project{Kind: "Elixir project", Build: "mix compile", Test: "mix test", group: "mix", aggregates: strings.Contains(s, "apps_path")}
			if m := mixAppRE.FindStringSubmatch(s); m != nil {
				p.Name = m[1]
			}
			d.needs(p, "mix")
			return one(p)
		}},
	{ID: "swiftpm", Languages: []string{"swift"}, Tools: []string{"swift"},
		detect: func(d *detector, di dirInfo) []*Project {
			if !di.has("Package.swift") {
				return nil
			}
			p := &Project{Kind: "Swift package", Build: "swift build", Test: "swift test"}
			d.needs(p, "swift")
			return one(p)
		}},
	{ID: "pub", Languages: []string{"dart"}, Tools: []string{"dart", "flutter"}, Registries: []string{"pub.dev"},
		detect: func(d *detector, di dirInfo) []*Project {
			if !di.has("pubspec.yaml") {
				return nil
			}
			s := di.read("pubspec.yaml")
			p := &Project{Kind: "Dart package", Build: "dart analyze", Test: "dart test"}
			tool := "dart"
			if strings.Contains(s, "sdk: flutter") {
				p.Kind, p.Build, p.Test, tool = "Flutter package", "flutter analyze", "flutter test", "flutter"
			}
			if m := pubNameRE.FindStringSubmatch(s); m != nil {
				p.Name = m[1]
			}
			d.needs(p, tool)
			return one(p)
		}},
	{ID: "zig", Languages: []string{"zig"}, Tools: []string{"zig"},
		detect: func(d *detector, di dirInfo) []*Project {
			if !di.has("build.zig") {
				return nil
			}
			p := &Project{Kind: "Zig project", Build: "zig build", Test: "zig build test"}
			d.needs(p, "zig")
			return one(p)
		}},
	{ID: "haskell", Languages: []string{"haskell"}, Tools: []string{"stack", "cabal"}, Registries: []string{"hackage.haskell.org"},
		detect: func(d *detector, di dirInfo) []*Project {
			switch {
			case di.has("stack.yaml"):
				p := &Project{Kind: "Haskell project", Build: "stack build", Test: "stack test", group: "haskell", aggregates: true}
				d.needs(p, "stack")
				return one(p)
			case di.has("cabal.project") || di.anyExt(".cabal"):
				p := &Project{Kind: "Haskell project", Build: "cabal build", Test: "cabal test", group: "haskell", aggregates: true}
				d.needs(p, "cabal")
				return one(p)
			}
			return nil
		}},
	{ID: "dune", Languages: []string{"ocaml"}, Tools: []string{"dune"},
		detect: func(d *detector, di dirInfo) []*Project {
			if !di.has("dune-project") {
				return nil
			}
			p := &Project{Kind: "OCaml project", Build: "dune build", Test: "dune test", group: "dune", aggregates: true}
			d.needs(p, "dune")
			return one(p)
		}},
	{ID: "clojure", Languages: []string{"clojure"}, Tools: []string{"lein", "clojure"}, Registries: []string{"repo.clojars.org", "repo.maven.apache.org"},
		detect: func(d *detector, di dirInfo) []*Project {
			switch {
			case di.has("project.clj"):
				p := &Project{Kind: "Clojure project", Test: "lein test"}
				d.needs(p, "lein")
				return one(p)
			case di.has("deps.edn"):
				p := &Project{Kind: "Clojure project"}
				d.needs(p, "clojure")
				return one(p)
			}
			return nil
		}},
	{ID: "bazel", Wrappers: []string{".bazelrc", ".bazelversion"}, Tools: []string{"bazel", "bazelisk"},
		detect: func(d *detector, di dirInfo) []*Project {
			if !di.has("MODULE.bazel", "WORKSPACE", "WORKSPACE.bazel") {
				return nil
			}
			p := &Project{Kind: "Bazel workspace", Build: "bazel build //...", Test: "bazel test //...", group: "bazel", aggregates: true}
			d.needs(p, "bazel")
			return one(p)
		}},
	{ID: "cmake", RunnerConfig: []string{"CTestTestfile.cmake", "CTestConfig.cmake"}, Languages: []string{"c", "cpp"}, Tools: []string{"cmake", "ctest"},
		detect: func(d *detector, di dirInfo) []*Project {
			if !di.has("CMakeLists.txt") {
				return nil
			}
			p := &Project{Kind: "CMake project", Build: "cmake -S . -B build && cmake --build build", Test: "ctest --test-dir build", group: "cmake", aggregates: true}
			d.needs(p, "cmake")
			return one(p)
		}},
	{ID: "meson", Languages: []string{"c", "cpp"}, Tools: []string{"meson"},
		detect: func(d *detector, di dirInfo) []*Project {
			if !di.has("meson.build") {
				return nil
			}
			p := &Project{Kind: "Meson project", Build: "meson setup build && meson compile -C build", Test: "meson test -C build", group: "meson", aggregates: true}
			d.needs(p, "meson")
			return one(p)
		}},
	{ID: "autotools", Languages: []string{"c", "cpp"}, Tools: []string{"make"},
		detect: func(d *detector, di dirInfo) []*Project {
			if !di.has("configure.ac", "configure.in") {
				return nil
			}
			p := &Project{Kind: "Autotools project", Build: "./configure && make", Test: "make check", group: "make", aggregates: true}
			d.needs(p, "make")
			return one(p)
		}},
	// A Makefile builds whatever its rules say: no language is assumed.
	{ID: "make", Tools: []string{"make"},
		detect: func(d *detector, di dirInfo) []*Project {
			if !di.has("Makefile", "makefile", "GNUmakefile") {
				return nil
			}
			name := "Makefile"
			for _, n := range []string{"GNUmakefile", "makefile"} {
				if di.names[n] {
					name = n
				}
			}
			targets := targetsOf(filepath.Join(di.abs, name))
			p := &Project{Kind: "Makefile", Build: "make", group: "make", aggregates: true}
			p.Note = "targets: none found"
			if len(targets) > 0 {
				p.Note = "targets: " + strings.Join(targets, ", ")
			}
			for _, t := range targets {
				if t == "test" || t == "check" {
					p.Test = "make " + t
					break
				}
			}
			d.needs(p, "make")
			return one(p)
		}},
	{ID: "just", Tools: []string{"just"},
		detect: func(d *detector, di dirInfo) []*Project {
			if !di.has("justfile", "Justfile", ".justfile") {
				return nil
			}
			name := "justfile"
			for _, n := range []string{"Justfile", ".justfile"} {
				if di.names[n] {
					name = n
				}
			}
			p := &Project{Kind: "justfile"}
			if recipes := targetsOf(filepath.Join(di.abs, name)); len(recipes) > 0 {
				p.Note = "recipes: " + strings.Join(recipes, ", ")
			}
			d.needs(p, "just")
			return one(p)
		}},
	{ID: "terraform", Languages: []string{"terraform"}, Tools: []string{"terraform", "tofu"}, Registries: []string{"registry.terraform.io", "releases.hashicorp.com"},
		detect: func(d *detector, di dirInfo) []*Project {
			if !di.anyExt(".tf") {
				return nil
			}
			p := &Project{Kind: "Terraform configuration", Build: "terraform init -backend=false && terraform validate", group: "terraform", aggregates: true}
			if di.anyExt(".tftest.hcl") {
				p.Test = "terraform test"
			}
			d.needs(p, "terraform")
			return one(p)
		}},
	{ID: "helm", Languages: []string{"yaml"}, Tools: []string{"helm"},
		detect: func(d *detector, di dirInfo) []*Project {
			if !di.has("Chart.yaml") {
				return nil
			}
			p := &Project{Kind: "Helm chart", Build: "helm lint ."}
			d.needs(p, "helm")
			return one(p)
		}},
}

// RegisterBuildSystem adds a build system recognised by marker files (from
// the user's configuration): projects in directories with any of markers
// (file names, or "*.ext"),
// built and tested with the commands given.
func RegisterBuildSystem(id, kind string, markers, languages []string, tool, build, test string) {
	var names, exts []string
	for _, m := range markers {
		if e, ok := strings.CutPrefix(m, "*"); ok {
			exts = append(exts, e) // "*.nimble"
		} else {
			names = append(names, m)
		}
	}
	bs := BuildSystem{ID: id, Languages: languages, Tools: []string{tool}, detect: func(d *detector, di dirInfo) []*Project {
		if !di.has(names...) && (len(exts) == 0 || !di.anyExt(exts...)) {
			return nil
		}
		p := &Project{Kind: kind, Build: build, Test: test}
		d.needs(p, tool)
		return one(p)
	}}
	for i, b := range buildSystems {
		if b.ID == id {
			buildSystems[i] = bs
			return
		}
	}
	buildSystems = append(buildSystems, bs)
}

// detectDir returns the projects whose manifests are in one directory.
func (d *detector) detectDir(abs, rel string, names map[string]bool) []*Project {
	di := dirInfo{abs: abs, rel: rel, names: names}
	var out []*Project
	for _, bs := range buildSystems {
		for _, p := range bs.detect(d, di) {
			p.Dir, p.System = rel, bs.ID
			if p.Languages == nil {
				p.Languages = bs.Languages
			}
			out = append(out, p)
		}
	}
	return out
}

// npmPlaceholder is the test script npm init writes.
const npmPlaceholder = "no test specified"

func (d *detector) node(p *Project, abs string) {
	var pkg struct {
		Name           string            `json:"name"`
		Scripts        map[string]string `json:"scripts"`
		Workspaces     json.RawMessage   `json:"workspaces"`
		PackageManager string            `json:"packageManager"`
	}
	if err := json.Unmarshal([]byte(read(abs, "package.json")), &pkg); err != nil {
		p.Note = "package.json does not parse"
		p.Manager = "npm"
		d.needs(p, "npm")
		return
	}
	p.Name = pkg.Name
	p.aggregates = len(pkg.Workspaces) > 0 && string(pkg.Workspaces) != "null" ||
		fileIn(abs, "pnpm-workspace.yaml") || fileIn(abs, "lerna.json")
	p.Manager = nodeManager(d, abs, pkg.PackageManager)
	run := func(script string) string {
		if p.Manager == "npm" && script == "test" {
			return "npm test"
		}
		return p.Manager + " run " + script
	}
	if s, ok := pkg.Scripts["test"]; ok && !strings.Contains(s, npmPlaceholder) {
		p.Test = run("test") + " (" + clip(s, 60) + ")"
	}
	if s, ok := pkg.Scripts["build"]; ok {
		p.Build = run("build") + " (" + clip(s, 60) + ")"
	}
	for _, k := range []string{"typecheck", "type-check", "lint", "check"} {
		if s, ok := pkg.Scripts[k]; ok {
			p.Other = append(p.Other, k+": "+run(k)+" ("+clip(s, 60)+")")
		}
	}
	if p.Test == "" && fileIn(abs, "tsconfig.json") && len(p.Other) == 0 {
		p.Note = "TypeScript (tsconfig.json), no test script"
	}
	d.needs(p, p.Manager)
}

// nodeManager picks the package manager from packageManager or the
// nearest lockfile.
func nodeManager(d *detector, abs, declared string) string {
	if declared != "" {
		name, _, _ := strings.Cut(declared, "@")
		switch name {
		case "npm", "pnpm", "yarn", "bun":
			return name
		}
	}
	for _, l := range []struct{ file, mgr string }{
		{"pnpm-lock.yaml", "pnpm"}, {"yarn.lock", "yarn"}, {"bun.lockb", "bun"}, {"bun.lock", "bun"}, {"package-lock.json", "npm"}, {"npm-shrinkwrap.json", "npm"},
	} {
		if d.inAncestors(abs, l.file) {
			return l.mgr
		}
	}
	return "npm"
}

func (d *detector) python(p *Project, abs string, names map[string]bool) {
	py := read(abs, "pyproject.toml")
	if m := tomlNameRE.FindStringSubmatch(py); m != nil {
		p.Name = m[1]
	}
	switch {
	case names["uv.lock"] || strings.Contains(py, "[tool.uv"):
		p.Manager = "uv"
	case names["poetry.lock"] || poetryRE.MatchString(py):
		p.Manager = "poetry"
	case names["pdm.lock"] || strings.Contains(py, "[tool.pdm"):
		p.Manager = "pdm"
	case names["Pipfile"]:
		p.Manager = "pipenv"
	default:
		p.Manager = "pip"
	}
	pytest := names["pytest.ini"] || names["conftest.py"] || strings.Contains(py, "pytest")
	if !pytest {
		for _, f := range []string{"setup.cfg", "tox.ini", "requirements.txt", "requirements-dev.txt", "requirements_dev.txt", "Pipfile"} {
			if names[f] && strings.Contains(read(abs, f), "pytest") {
				pytest = true
				break
			}
		}
	}
	runner := "unittest"
	if pytest {
		runner = "pytest"
	}
	if p.Manager == "pip" {
		python := "python3"
		if !d.have(python) && d.have("python") {
			python = "python"
		}
		p.Test = python + " -m " + runner
		d.needs(p, python)
		return
	}
	if runner == "unittest" {
		runner = "python -m unittest"
	}
	p.Test = p.Manager + " run " + runner
	d.needs(p, p.Manager)
}

func (d *detector) php(p *Project, abs string, names map[string]bool) {
	var c struct {
		Name    string                     `json:"name"`
		Scripts map[string]json.RawMessage `json:"scripts"`
	}
	s := read(abs, "composer.json")
	_ = json.Unmarshal([]byte(s), &c)
	p.Name = c.Name
	switch {
	case c.Scripts["test"] != nil:
		p.Test = "composer test"
		d.needs(p, "composer")
	case names["phpunit.xml"] || names["phpunit.xml.dist"] || strings.Contains(s, "phpunit"):
		p.Test = "vendor/bin/phpunit"
		d.needs(p, "php")
	default:
		d.needs(p, "php")
	}
}

func fileIn(abs, name string) bool {
	_, err := os.Stat(filepath.Join(abs, name))
	return err == nil
}

// targetsOf lists the first targets of a Makefile or recipes of a justfile.
func targetsOf(file string) []string {
	f, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer f.Close()
	var targets []string
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() && len(targets) < 12 {
		if m := targetRE.FindStringSubmatch(sc.Text()); m != nil && !seen[m[1]] {
			seen[m[1]] = true
			targets = append(targets, m[1])
		}
	}
	return targets
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// ciFiles lists the CI configuration at root.
func ciFiles(root string) []string {
	var out []string
	if ents, err := os.ReadDir(filepath.Join(root, ".github", "workflows")); err == nil {
		for _, e := range ents {
			if n := e.Name(); !e.IsDir() && (strings.HasSuffix(n, ".yml") || strings.HasSuffix(n, ".yaml")) {
				out = append(out, ".github/workflows/"+n)
			}
		}
	}
	for _, f := range []string{".gitlab-ci.yml", ".circleci/config.yml", "azure-pipelines.yml", "Jenkinsfile", ".travis.yml",
		"bitbucket-pipelines.yml", ".buildkite/pipeline.yml", ".woodpecker.yml", ".drone.yml", "appveyor.yml"} {
		if st, err := os.Stat(filepath.Join(root, filepath.FromSlash(f))); err == nil && st.Mode().IsRegular() {
			out = append(out, f)
		}
	}
	return out
}

// Describe renders a project in one line, without its directory:
// "Go module example.com/shop (build: go build ./..., test: go test ./...)".
func (p Project) Describe() string {
	if p.Kind == "Makefile" && p.Note != "" {
		s := "Makefile " + p.Note
		if p.Missing {
			s += " [make not found on PATH]"
		}
		return s
	}
	var b strings.Builder
	b.WriteString(p.Kind)
	if p.Name != "" {
		b.WriteString(" " + p.Name)
	}
	var parts []string
	if p.Manager != "" && p.Kind != "Node package" {
		parts = append(parts, p.Manager)
	}
	if p.Build != "" {
		parts = append(parts, "build: "+p.Build)
	}
	if p.Test != "" {
		parts = append(parts, "test: "+p.Test)
	}
	parts = append(parts, p.Other...)
	if p.Members > 0 {
		parts = append(parts, fmt.Sprintf("%d member projects", p.Members))
	}
	if p.Note != "" {
		parts = append(parts, p.Note)
	}
	if len(parts) > 0 {
		b.WriteString(" (" + strings.Join(parts, ", ") + ")")
	}
	if p.Missing && p.Tool != "" {
		b.WriteString(" [" + p.Tool + " not found on PATH]")
	}
	return b.String()
}
