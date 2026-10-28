package direct

import (
	"fmt"
	"github.com/akynte/boundedcode/internal/lang"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/ecosystem"
)

// The first message of a session carries a short map of the project: the
// projects in it, in any language and in the top directory levels
// (internal/ecosystem), with how each is built and tested and which tools
// are missing; its top-level layout with source-file counts per
// directory; and its CI configuration. A model otherwise spends its first
// calls on ls, glob and reading the README to learn the same things; the
// map is deterministic, costs milliseconds and a few hundred tokens, and
// stays in the stable prefix.

const (
	projectMapDirs    = 24  // top-level entries listed
	projectMapMaxFile = 400 // files counted per directory before stopping
)

// sourceLanguage is the language of a file the map counts as source: code,
// SQL, infrastructure and YAML, not prose or data formats.
func sourceLanguage(name string) *lang.Language {
	l := lang.ForPath(name, "")
	if l == nil || l.Kind == lang.KindMarkup {
		return nil
	}
	switch l.ID {
	case "json", "jsonc", "xml", "toml", "make", "cmake":
		return nil
	}
	return l
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
	for _, l := range projectLines(root) {
		b.WriteString("- " + l + "\n")
	}
	type dirInfo struct {
		name  string
		files int
		langs map[string]int
	}
	var dirs []dirInfo
	var files []string
	found := map[string]*lang.Language{}
	for _, e := range entries {
		name := e.Name()
		if skippedDirs[name] || strings.HasPrefix(name, ".") {
			continue
		}
		if !e.IsDir() {
			files = append(files, name)
			if l := sourceLanguage(name); l != nil {
				found[l.ID] = l
			}
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
			if l := sourceLanguage(d.Name()); l != nil {
				di.files++
				di.langs[l.Name]++
				found[l.ID] = l
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
	if line := r.checkCoverage(found); line != "" {
		b.WriteString("- " + line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// checkCoverage says which of the languages found are checked after each
// edit here, and which are not and why.
func (r *Runner) checkCoverage(found map[string]*lang.Language) string {
	var checked, missing, none []string
	ids := make([]string, 0, len(found))
	for id := range found {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		l := found[id]
		c, _, st := r.finder().FindCheck(l, "")
		switch {
		case st == lang.Available:
			checked = append(checked, l.Name)
		case st == lang.MissingTool:
			missing = append(missing, l.Name+" ("+strings.Join(c.Tools, "/")+" not installed)")
		case l.Kind == lang.KindProgramming:
			none = append(none, l.Name)
		}
	}
	var parts []string
	if len(checked) > 0 {
		parts = append(parts, "checked after each edit: "+strings.Join(checked, ", "))
	}
	if len(missing) > 0 {
		parts = append(parts, "not checked, missing tool: "+strings.Join(missing, ", "))
	}
	if len(none) > 0 {
		parts = append(parts, "no per-file check (build or test the project): "+strings.Join(none, ", "))
	}
	return strings.Join(parts, "; ")
}

// projectLines lists the projects found under root (in any language,
// nested ones included), how each is built and tested, the tools that are
// missing, and the CI configuration.
func projectLines(root string) []string {
	rep := ecosystem.Detect(root, ecosystem.Options{})
	var out []string
	for _, p := range rep.Projects {
		l := p.Describe()
		if p.Dir != "." {
			l = p.Dir + "/: " + l
		}
		out = append(out, l)
	}
	if rep.More > 0 {
		out = append(out, fmt.Sprintf("… %d more projects", rep.More))
	}
	if len(rep.CI) > 0 {
		ci := rep.CI
		if len(ci) > 4 {
			ci = append(ci[:4:4], fmt.Sprintf("… %d more", len(rep.CI)-4))
		}
		out = append(out, "CI configuration (how the project is built and tested): "+strings.Join(ci, ", "))
	}
	if len(rep.Projects) == 0 {
		out = append(out, "no build system recognised in the top directory levels; the README or the CI configuration says how to build and test")
	}
	return out
}
