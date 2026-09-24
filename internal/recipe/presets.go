package recipe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/policy"
	"github.com/akynte/boundedcode/internal/worktree"
	"github.com/pelletier/go-toml/v2"
)

// Preset is a serializable, supervisor-owned command frozen during INTAKE.
// A model selects a preset kind; it cannot supply argv, a directory or a shell.
type Preset struct {
	Name           string   `json:"name" toml:"name"`
	Kind           Kind     `json:"kind" toml:"kind"`
	Argv           []string `json:"argv" toml:"argv"`
	Dir            string   `json:"dir" toml:"dir"`
	TimeoutSeconds int      `json:"timeout_seconds" toml:"timeout_seconds"`
	Parser         string   `json:"parser" toml:"parser"`
}

func (p Preset) Validate(root string) error {
	if p.Name == "" || len(p.Argv) == 0 || p.Argv[0] == "" {
		return fmt.Errorf("verification preset needs name and argv")
	}
	switch p.Kind {
	case KindBuild, KindVet, KindTest, KindRace, KindFormat, KindLint, KindAnalyzer, KindGenerate, KindIntegration, KindCustom:
	default:
		return fmt.Errorf("unknown preset kind %q", p.Kind)
	}
	if p.TimeoutSeconds < 1 || p.TimeoutSeconds > 3600 {
		return fmt.Errorf("preset %s timeout must be 1–3600 seconds", p.Name)
	}
	if p.Dir != "" {
		if _, err := worktree.Resolve(root, p.Dir); err != nil {
			return err
		}
		if policy.Sensitive(p.Dir) {
			return fmt.Errorf("sensitive preset directory")
		}
	}
	switch p.Parser {
	case "", "generic", "go_build", "go_vet", "go_test_json", "gofmt":
	default:
		return fmt.Errorf("unknown preset parser %q", p.Parser)
	}
	return nil
}
func (p Preset) Recipe() Recipe {
	r := Recipe{Name: p.Name, Kind: p.Kind, Argv: append([]string(nil), p.Argv...), Dir: p.Dir, Timeout: time.Duration(p.TimeoutSeconds) * time.Second, Summarize: Generic}
	switch p.Parser {
	case "go_build":
		r.Summarize = GoBuild
	case "go_vet":
		r.Summarize = GoVet
	case "go_test_json":
		r.Summarize = GoTestJSON
	case "gofmt":
		r.Summarize = Gofmt
	}
	return r
}

// DiscoverPresets prefers .agent/verify.toml, otherwise uses installed repository
// manifests. It never installs dependencies or asks a model for commands.
func DiscoverPresets(root string, level Level) ([]Preset, error) {
	configPath, err := worktree.Resolve(root, ".agent/verify.toml")
	if err != nil {
		return nil, err
	}
	if body, err := os.ReadFile(configPath); err == nil {
		var config struct {
			Version int      `toml:"version"`
			Presets []Preset `toml:"presets"`
		}
		if err := toml.NewDecoder(bytes.NewReader(body)).DisallowUnknownFields().Decode(&config); err != nil {
			return nil, err
		}
		if config.Version != 1 || len(config.Presets) == 0 {
			return nil, fmt.Errorf("verify.toml requires version=1 and nonempty presets")
		}
		seen := map[string]bool{}
		for _, p := range config.Presets {
			if err := p.Validate(root); err != nil {
				return nil, err
			}
			if seen[p.Name] {
				return nil, fmt.Errorf("duplicate preset %q", p.Name)
			}
			seen[p.Name] = true
		}
		return selectPresets(config.Presets, level), nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	var out []Preset
	var nodeRoots, cargoRoots, pythonRoots []string
	under := func(dir string, roots []string) bool {
		for _, parent := range roots {
			if dir == parent || strings.HasPrefix(dir, parent+"/") || parent == "." {
				return true
			}
		}
		return false
	}
	err = filepath.WalkDir(root, func(full string, e fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, full)
		if err != nil {
			return err
		}
		if e.IsDir() {
			switch e.Name() {
			case ".git", ".bc", ".agent", "vendor", "node_modules", "target", "dist", ".nuxt", ".output":
				return filepath.SkipDir
			}
			return nil
		}
		if e.Type()&os.ModeSymlink != 0 {
			return nil
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		add := func(name string, kind Kind, parser string, argv ...string) {
			timeout := 120
			if kind == KindTest || kind == KindRace {
				timeout = 600
			}
			out = append(out, Preset{Name: dir + ": " + name, Kind: kind, Argv: argv, Dir: dir, TimeoutSeconds: timeout, Parser: parser})
		}
		switch e.Name() {
		case "go.mod":
			add("go build", KindBuild, "go_build", "go", "build", "./...")
			add("go vet", KindVet, "go_vet", "go", "vet", "./...")
			add("go test", KindTest, "go_test_json", "go", "test", "-json", "./...")
			add("gofmt", KindFormat, "gofmt", "gofmt", "-l", ".")
			add("go race", KindRace, "go_test_json", "go", "test", "-race", "-json", "./...")
		case "Cargo.toml":
			if under(dir, cargoRoots) {
				return nil
			}
			cargoRoots = append(cargoRoots, dir)
			add("cargo check", KindBuild, "", "cargo", "check", "--workspace", "--offline", "--jobs", "2")
			add("cargo clippy", KindVet, "", "cargo", "clippy", "--workspace", "--offline", "--jobs", "2", "--", "-D", "warnings")
			add("cargo test", KindTest, "", "cargo", "test", "--workspace", "--offline", "--jobs", "2")
			add("cargo fmt", KindFormat, "", "cargo", "fmt", "--all", "--", "--check")
		case "package.json":
			if under(dir, nodeRoots) {
				return nil
			}
			// An installed manifest, per this function's contract: it never
			// installs dependencies, so a package.json with no node_modules
			// beside it describes commands that cannot run. `npm run test`
			// then exits 127 on the first missing binary, which the runner
			// correctly reports as an environment failure and which stops a
			// task before it starts. Django is the case that found this: it
			// carries a package.json for its admin JavaScript, and a Python
			// repository was being blocked by an absent grunt.
			if _, statErr := os.Stat(filepath.Join(filepath.Dir(full), "node_modules")); statErr != nil {
				//nolint:nilerr // an absent node_modules is the answer, not an error
				return nil
			}
			// The walk skips symlinks before reaching here, so the path this
			// reads is the one the walk saw; a manifest swapped underneath it
			// yields a parse error rather than a read outside the tree.
			body, err := os.ReadFile(full) //nolint:gosec // G122: symlinks are skipped above
			if err != nil {
				return err
			}
			var pkg struct {
				PackageManager string            `json:"packageManager"`
				Scripts        map[string]string `json:"scripts"`
				Workspaces     json.RawMessage   `json:"workspaces"`
			}
			if err := json.Unmarshal(body, &pkg); err != nil {
				return fmt.Errorf("%s: %w", rel, err)
			}
			manager := "npm"
			if strings.HasPrefix(pkg.PackageManager, "pnpm@") {
				manager = "pnpm"
			} else if strings.HasPrefix(pkg.PackageManager, "yarn@") {
				manager = "yarn"
			}
			if len(pkg.Workspaces) > 0 || manager == "pnpm" {
				nodeRoots = append(nodeRoots, dir)
			}
			for _, spec := range []struct {
				script string
				kind   Kind
			}{{"build", KindBuild}, {"typecheck", KindVet}, {"test", KindTest}, {"lint", KindLint}, {"format:check", KindFormat}} {
				if pkg.Scripts[spec.script] != "" {
					add(manager+" "+spec.script, spec.kind, "", manager, "run", spec.script)
				}
			}
		case "pyproject.toml", "setup.py", "setup.cfg", "tox.ini":
			if under(dir, pythonRoots) {
				return nil
			}
			pythonRoots = append(pythonRoots, dir)
			for _, preset := range pythonPresets(filepath.Dir(full)) {
				add(preset.name, preset.kind, "", preset.argv...)
			}
		}
		if len(out) > 2000 {
			return fmt.Errorf("verification discovery exceeds 2000 presets")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// WalkDir may visit child directories before the root manifest. Remove
	// nested workspace commands after discovery so checks are not duplicated.
	filtered := out[:0]
	for _, p := range out {
		roots := nodeRoots
		switch p.Argv[0] {
		case "cargo":
			roots = cargoRoots
		case "python3":
			roots = pythonRoots
		case "go", "gofmt":
			roots = nil
		}
		nested := false
		for _, parent := range roots {
			if p.Dir != parent && under(p.Dir, []string{parent}) {
				nested = true
			}
		}
		if !nested {
			filtered = append(filtered, p)
		}
	}
	out = filtered
	if len(out) == 0 {
		return nil, fmt.Errorf("no verification presets: declare .agent/verify.toml")
	}
	return selectPresets(out, level), nil
}

func selectPresets(all []Preset, level Level) []Preset {
	var out []Preset
	for _, p := range all {
		if level.Includes(p.Kind) || (level == Standard && p.Kind == KindLint) {
			out = append(out, p)
		}
	}
	return out
}

// CheckPresets requires evidence for every frozen command, not merely one
// passing result per kind in a monorepo with several independent modules.
func CheckPresets(presets []Preset, results []Result, candidate string) (bool, []string) {
	var reasons []string
	byName := map[string]Result{}
	for _, r := range results {
		byName[r.Recipe] = r
	}
	for _, p := range presets {
		r, ok := byName[p.Name]
		if !ok || r.Status != Pass || candidate == "" || r.Candidate == "" || r.Candidate != candidate {
			reasons = append(reasons, "required preset did not pass on this candidate: "+p.Name)
		}
	}
	if len(presets) == 0 {
		reasons = append(reasons, "no required verification presets")
	}
	return len(reasons) == 0, reasons
}

// pythonPresets reads what a Python project says about itself.
//
// The rule is the same one the rest of discovery follows: emit a command the
// repository's own configuration evidences, and nothing else. A Python tree
// has no single manifest that names its checks the way package.json's scripts
// do — pyproject.toml, setup.cfg and tox.ini each carry a piece — so the
// evidence is gathered from all of them and each tool is emitted only where
// it is configured. Guessing `python -m mypy` at a repository that does not
// use mypy would produce a red check about a tool nobody chose.
//
// The compile check is the exception and is always emitted. It is the closest
// Python has to `go build`: it parses every module and needs no third-party
// package, so it is the one check that says something about the code in a
// tree whose dependencies are not installed.
func pythonPresets(dir string) []struct {
	name string
	kind Kind
	argv []string
} {
	type preset = struct {
		name string
		kind Kind
		argv []string
	}
	// `python3` rather than `python`: on a modern distribution `python` is
	// frequently absent, and a preset that exits 127 is reported as a broken
	// environment rather than as a failing check.
	out := []preset{{"python compile", KindBuild, []string{"python3", "-m", "compileall", "-q", "."}}}

	read := func(name string) string {
		body, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // a manifest the walk already visited
		if err != nil {
			return ""
		}
		return string(body)
	}
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	pyproject, setupCfg, toxIni := read("pyproject.toml"), read("setup.cfg"), read("tox.ini")
	configured := func(markers ...string) bool {
		for _, m := range markers {
			if strings.Contains(pyproject, m) || strings.Contains(setupCfg, m) || strings.Contains(toxIni, m) {
				return true
			}
		}
		return false
	}

	switch {
	case exists("pytest.ini"), exists("conftest.py"),
		configured("[tool.pytest.ini_options]", "[tool:pytest]", "[pytest]"):
		out = append(out, preset{"pytest", KindTest, []string{"python3", "-m", "pytest", "-q"}})
	case exists("tests"), exists("test"):
		// unittest discovery is in the standard library, so it runs wherever
		// the interpreter does. It is the fallback rather than the default
		// because a pytest suite run under unittest reports errors that are
		// about the runner, not the code.
		out = append(out, preset{"unittest", KindTest, []string{"python3", "-m", "unittest", "discover", "-q"}})
	}
	if configured("[tool.ruff]", "[tool.ruff.") || exists("ruff.toml") || exists(".ruff.toml") {
		out = append(out, preset{"ruff", KindLint, []string{"python3", "-m", "ruff", "check", "."}})
	}
	if configured("[flake8]") || exists(".flake8") {
		out = append(out, preset{"flake8", KindLint, []string{"python3", "-m", "flake8"}})
	}
	if configured("[tool.black]") {
		out = append(out, preset{"black", KindFormat, []string{"python3", "-m", "black", "--check", "."}})
	}
	if configured("[tool.isort]", "[isort]") {
		out = append(out, preset{"isort", KindFormat, []string{"python3", "-m", "isort", "--check-only", "."}})
	}
	if configured("[tool.mypy]", "[mypy]") || exists("mypy.ini") || exists(".mypy.ini") {
		out = append(out, preset{"mypy", KindVet, []string{"python3", "-m", "mypy", "."}})
	}
	return out
}

// PresetNames lists the ids of the frozen presets of one kind.
//
// It exists so a phase can hand a model the choices it has rather than hope
// it guesses one: a preset id is the only thing a model may say about a
// verification command, and the set of valid ids is not something the model
// can be expected to know.
// An empty kind means every preset, for a caller that needs the whole set of
// valid ids rather than one family of them.
func PresetNames(presets []Preset, kind Kind) []string {
	var out []string
	for _, p := range presets {
		if kind == "" || p.Kind == kind {
			out = append(out, p.Name)
		}
	}
	sort.Strings(out)
	return out
}
