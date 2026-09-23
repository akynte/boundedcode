package python

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// What a Python repository is, and where its imports start from.
//
// Both questions are answered by looking at the tree, never by a list of
// project names. A repository is Python if it contains Python; an import root
// is a directory that Python code would be importable *from*. Getting the
// second one wrong is the whole of the src-layout problem, so it is computed
// here and asserted by tests rather than left to the indexer's defaults.

// manifests are the files that mark a Python project. Their presence is not
// required — a directory of scripts is still Python — but it is what makes a
// repository worth looking at before walking it.
var manifests = []string{"pyproject.toml", "setup.py", "setup.cfg", "tox.ini", "requirements.txt", "Pipfile"}

// Detected is what the walk found.
type Detected struct {
	// SourceFiles counts .py files outside excluded directories. It is the
	// denominator for coverage, so it counts what an indexer should see.
	SourceFiles int
	// ImportRoots are directories to add to the module search path, relative
	// to the repository root and always including ".".
	ImportRoots []string
	// Packages are the top-level importable package names found under the
	// import roots. They are the evidence for a root, reported so a wrong
	// root can be recognised rather than guessed at.
	Packages []string
	// HasPyrightConfig says the repository already configures pyright, in
	// which case nothing here overrides it.
	HasPyrightConfig bool
	// Manifest is the project file that was found, for the report.
	Manifest string
}

// excluded are directories neither the indexer nor the walk should enter.
// They are the conventional non-source trees; a Python project that keeps
// real code in one of these is not one this can help.
var excluded = map[string]bool{
	".git": true, ".hg": true, ".svn": true, ".bc": true, ".agent": true,
	"node_modules": true, ".venv": true, "venv": true, "__pycache__": true,
	".tox": true, ".nox": true, ".mypy_cache": true, ".pytest_cache": true,
	"build": true, "dist": true, ".eggs": true, "site-packages": true,
}

// maxImportRoots bounds how many directories are added to the search path.
//
// Every extra root is another place an ambiguous module name can resolve
// from, so this is a correctness bound rather than a performance one. A
// project with more than a handful of source roots is not following any
// layout convention, and the repository root plus its genuine package
// directories is the honest answer.
const maxImportRoots = 8

// Detect walks a repository and reports what a Python indexer needs to know.
func Detect(root string) Detected {
	d := Detected{ImportRoots: []string{"."}}
	for _, m := range manifests {
		if _, err := os.Stat(filepath.Join(root, m)); err == nil {
			d.Manifest = m
			break
		}
	}
	if _, err := os.Stat(filepath.Join(root, "pyrightconfig.json")); err == nil {
		d.HasPyrightConfig = true
	}
	if body, err := os.ReadFile(filepath.Join(root, "pyproject.toml")); err == nil &&
		strings.Contains(string(body), "[tool.pyright]") {
		d.HasPyrightConfig = true
	}

	// A directory is an import root when code inside it is imported by its
	// path *below* that directory: `src/pkg/__init__.py` means `pkg` is the
	// module name and `src` is where the search must start. The repository
	// root is always a root; a child is one when it holds a package and is
	// not itself a package.
	packages := map[string]bool{}
	roots := map[string]bool{".": true}
	entries, err := os.ReadDir(root)
	if err != nil {
		return d
	}
	var children []string
	for _, e := range entries {
		if !e.IsDir() || excluded[e.Name()] || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, e.Name(), "__init__.py")); err == nil {
			// A package sitting directly at the root: imports already start
			// here, and the repository root is in the set.
			packages[e.Name()] = true
			continue
		}
		children = append(children, e.Name())
	}

	// A root-layout project needs nothing more, and looking deeper actively
	// hurts: Django keeps two hundred packages under tests/, each one a
	// fixture app its own runner puts on the path at runtime. Adding that
	// directory asks the type checker to resolve a namespace an order of
	// magnitude larger than the project for no edge anybody wants, and it
	// took the index from minutes to not finishing. The extra roots exist
	// for the case the root has no package at all, which is what src-layout
	// is.
	if len(packages) == 0 {
		for _, name := range children {
			child := filepath.Join(root, name)
			inner, err := os.ReadDir(child)
			if err != nil {
				continue
			}
			for _, sub := range inner {
				if !sub.IsDir() || excluded[sub.Name()] {
					continue
				}
				if _, err := os.Stat(filepath.Join(child, sub.Name(), "__init__.py")); err == nil {
					roots[name] = true
					packages[sub.Name()] = true
				}
			}
		}
	}

	d.ImportRoots = d.ImportRoots[:0]
	for r := range roots {
		d.ImportRoots = append(d.ImportRoots, r)
	}
	sort.Strings(d.ImportRoots)
	if len(d.ImportRoots) > maxImportRoots {
		d.ImportRoots = d.ImportRoots[:maxImportRoots]
	}
	for p := range packages {
		d.Packages = append(d.Packages, p)
	}
	sort.Strings(d.Packages)

	d.SourceFiles = countPython(root)
	return d
}

// IsPython reports whether a repository is worth handing to a Python indexer.
func IsPython(root string) bool {
	for _, m := range manifests {
		if _, err := os.Stat(filepath.Join(root, m)); err == nil {
			return countPython(root) > 0
		}
	}
	return countPython(root) > 0
}

// countPython counts source files the indexer is expected to cover.
func countPython(root string) int {
	n := 0
	_ = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable entry is not a Python file
		}
		if e.IsDir() {
			if p != root && excluded[e.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(e.Name(), ".py") {
			n++
		}
		return nil
	})
	return n
}

// pyrightConfig is the subset of pyright's configuration this writes.
type pyrightConfig struct {
	ExtraPaths                 []string `json:"extraPaths,omitempty"`
	Exclude                    []string `json:"exclude,omitempty"`
	UseLibraryCodeForTypes     bool     `json:"useLibraryCodeForTypes"`
	ReportMissingImports       string   `json:"reportMissingImports"`
	ReportMissingModuleSource  string   `json:"reportMissingModuleSource"`
	AnalyzeUnannotatedFunction bool     `json:"analyzeUnannotatedFunctions"`
}

// WriteSearchPathConfig hands the indexer the import roots Detect found.
//
// It exists because of a real and specific defect: with a src-layout project
// and no configuration, pyright resolves `src/pkg/core.py` as the module
// `src.pkg.core`, while `from pkg.core import handler` in a test resolves to
// `pkg.core` — a different symbol, so every cross-package reference is lost
// and the index still looks fine, full of documents and definitions. The
// smallest deterministic correction is to tell pyright where imports start,
// which is exactly what extraPaths is.
//
// It writes nothing when the repository already configures pyright: an
// operator's configuration is the answer, not a starting point. The returned
// function restores the tree, so the worktree the solver sees is the one it
// would have seen.
func WriteSearchPathConfig(root string, d Detected, alsoExclude ...string) (restore func(), wrote bool, err error) {
	if d.HasPyrightConfig {
		return func() {}, false, nil
	}
	if d.HasPyrightConfig && len(alsoExclude) > 0 {
		// A repository that configures pyright still cannot be indexed past a
		// file that kills the process, so the exclusion is the one thing that
		// overrides an operator's configuration — and only after a crash.
		d.HasPyrightConfig = false
	}
	extra := make([]string, 0, len(d.ImportRoots))
	for _, r := range d.ImportRoots {
		if r != "." {
			extra = append(extra, r)
		}
	}
	if len(extra) == 0 && len(alsoExclude) == 0 {
		// Root layout and nothing to exclude: pyright's default is already
		// correct, and a file that changes nothing is one somebody has to
		// explain.
		return func() {}, false, nil
	}
	cfg := pyrightConfig{
		ExtraPaths: extra,
		Exclude: append([]string{"**/node_modules", "**/__pycache__", "**/.venv", "**/venv",
			"**/build", "**/dist", "**/.tox"}, alsoExclude...),
		// Third-party stubs are not fetched and their source is often absent;
		// reading what is installed is what makes references resolve, and a
		// missing import is a fact to record rather than an error to fail on.
		UseLibraryCodeForTypes:     true,
		ReportMissingImports:       "none",
		ReportMissingModuleSource:  "none",
		AnalyzeUnannotatedFunction: true,
	}
	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return func() {}, false, err
	}
	path := filepath.Join(root, "pyrightconfig.json")
	if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
		return func() {}, false, err
	}
	return func() { _ = os.Remove(path) }, true, nil
}
