package direct

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The first message of a session carries a short map of the project:
// what it is (module, package, build files), its top-level layout with
// source-file counts per directory, and how to build and test it. A
// model otherwise spends its first calls on ls, glob and reading the
// README to learn the same things; the map is deterministic, costs
// milliseconds and a few hundred tokens, and stays in the stable prefix.

const (
	projectMapDirs    = 24  // top-level entries listed
	projectMapMaxFile = 400 // files counted per directory before stopping
)

var sourceExt = map[string]string{
	".go": "Go", ".ts": "TypeScript", ".tsx": "TypeScript", ".js": "JavaScript", ".jsx": "JavaScript", ".mjs": "JavaScript", ".cjs": "JavaScript",
	".py": "Python", ".rs": "Rust", ".java": "Java", ".kt": "Kotlin", ".rb": "Ruby", ".php": "PHP", ".cs": "C#", ".c": "C", ".h": "C", ".cc": "C++", ".cpp": "C++", ".hpp": "C++",
	".swift": "Swift", ".scala": "Scala", ".ex": "Elixir", ".exs": "Elixir", ".sh": "shell", ".sql": "SQL", ".proto": "protobuf", ".tf": "Terraform", ".yaml": "YAML", ".yml": "YAML",
}

// projectMap describes the project at root in a few lines ("" when root
// cannot be read).
func (r *Runner) projectMap() string {
	root, err := filepath.EvalSymlinks(r.Opts.Root)
	if err != nil {
		root = r.Opts.Root
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("Project map (top-level layout, source files per directory):\n")
	for _, l := range buildLines(root) {
		b.WriteString("- " + l + "\n")
	}
	type dirInfo struct {
		name  string
		files int
		langs map[string]int
	}
	var dirs []dirInfo
	var files []string
	for _, e := range entries {
		name := e.Name()
		if skippedDirs[name] || strings.HasPrefix(name, ".") {
			continue
		}
		if !e.IsDir() {
			files = append(files, name)
			continue
		}
		di := dirInfo{name: name, langs: map[string]int{}}
		_ = filepath.WalkDir(filepath.Join(root, name), func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil //nolint:nilerr // unreadable entries are skipped
			}
			if d.IsDir() {
				if p != filepath.Join(root, name) && (skippedDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
					return filepath.SkipDir
				}
				return nil
			}
			if lang, ok := sourceExt[strings.ToLower(filepath.Ext(d.Name()))]; ok {
				di.files++
				di.langs[lang]++
				if di.files >= projectMapMaxFile {
					return errStop
				}
			}
			return nil
		})
		dirs = append(dirs, di)
	}
	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].files > dirs[j].files || (dirs[i].files == dirs[j].files && dirs[i].name < dirs[j].name)
	})
	shown := 0
	for _, d := range dirs {
		if shown == projectMapDirs {
			fmt.Fprintf(&b, "- … %d more directories\n", len(dirs)-shown)
			break
		}
		shown++
		if d.files == 0 {
			fmt.Fprintf(&b, "- %s/\n", d.name)
			continue
		}
		var langs []string
		for l, n := range d.langs {
			langs = append(langs, fmt.Sprintf("%s %d", l, n))
		}
		sort.Strings(langs)
		more := ""
		if d.files >= projectMapMaxFile {
			more = "+"
		}
		fmt.Fprintf(&b, "- %s/: %d%s source files (%s)\n", d.name, d.files, more, strings.Join(langs, ", "))
	}
	if len(files) > 0 {
		sort.Strings(files)
		if len(files) > 16 {
			files = append(files[:16], fmt.Sprintf("… %d more", len(files)-16))
		}
		fmt.Fprintf(&b, "- top-level files: %s\n", strings.Join(files, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
}

var (
	goModuleRE  = regexp.MustCompile(`(?m)^module\s+(\S+)`)
	pkgNameRE   = regexp.MustCompile(`"name"\s*:\s*"([^"]+)"`)
	pkgScriptRE = regexp.MustCompile(`"(test|build|lint)"\s*:\s*"([^"]{1,80})"`)
	cargoNameRE = regexp.MustCompile(`(?m)^name\s*=\s*"([^"]+)"`)
	pyNameRE    = regexp.MustCompile(`(?m)^name\s*=\s*"([^"]+)"`)
	makeTarget  = regexp.MustCompile(`^([A-Za-z][\w-]*):`)
)

// buildLines names the build system(s) found at root and how to test.
func buildLines(root string) []string {
	var out []string
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return ""
		}
		if len(b) > 64<<10 {
			b = b[:64<<10]
		}
		return string(b)
	}
	if s := read("go.mod"); s != "" {
		mod := "?"
		if m := goModuleRE.FindStringSubmatch(s); m != nil {
			mod = m[1]
		}
		out = append(out, fmt.Sprintf("Go module %s (build: go build ./..., test: go test ./...)", mod))
	}
	if s := read("package.json"); s != "" {
		line := "Node package"
		if m := pkgNameRE.FindStringSubmatch(s); m != nil {
			line += " " + m[1]
		}
		var scripts []string
		for _, m := range pkgScriptRE.FindAllStringSubmatch(s, -1) {
			scripts = append(scripts, m[1]+": "+m[2])
		}
		if len(scripts) > 0 {
			line += " (scripts " + strings.Join(scripts, "; ") + ")"
		}
		out = append(out, line)
	}
	if s := read("Cargo.toml"); s != "" {
		line := "Rust crate"
		if m := cargoNameRE.FindStringSubmatch(s); m != nil {
			line += " " + m[1]
		}
		out = append(out, line+" (test: cargo test)")
	}
	if s := read("pyproject.toml"); s != "" {
		line := "Python project"
		if m := pyNameRE.FindStringSubmatch(s); m != nil {
			line += " " + m[1]
		}
		test := "pytest"
		if !strings.Contains(s, "pytest") {
			test = "python -m unittest"
		}
		out = append(out, line+" (test: "+test+")")
	} else if read("setup.py") != "" || read("requirements.txt") != "" {
		out = append(out, "Python project (test: pytest or python -m unittest)")
	}
	for _, name := range []string{"pom.xml", "build.gradle", "build.gradle.kts", "Gemfile", "composer.json", "CMakeLists.txt", "Makefile"} {
		if read(name) == "" {
			continue
		}
		switch name {
		case "pom.xml":
			out = append(out, "Maven project (test: mvn test)")
		case "build.gradle", "build.gradle.kts":
			out = append(out, "Gradle project (test: gradle test)")
		case "Gemfile":
			out = append(out, "Ruby project (test: bundle exec rake test or rspec)")
		case "composer.json":
			out = append(out, "PHP project (test: vendor/bin/phpunit)")
		case "CMakeLists.txt":
			out = append(out, "CMake project")
		case "Makefile":
			out = append(out, "Makefile targets: "+makeTargets(filepath.Join(root, name)))
		}
	}
	return out
}

// makeTargets lists the first targets of a Makefile.
func makeTargets(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "?"
	}
	defer f.Close()
	var targets []string
	sc := bufio.NewScanner(f)
	for sc.Scan() && len(targets) < 12 {
		if m := makeTarget.FindStringSubmatch(sc.Text()); m != nil && !strings.HasPrefix(m[1], ".") {
			targets = append(targets, m[1])
		}
	}
	if len(targets) == 0 {
		return "none found"
	}
	return strings.Join(targets, ", ")
}
