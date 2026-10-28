package lang

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SkipDirs are dependency, build-output and tool directories: not the
// project's own code.
var SkipDirs = map[string]bool{".git": true, ".hg": true, ".svn": true, "node_modules": true, "vendor": true, ".venv": true, "venv": true,
	"__pycache__": true, "dist": true, "build": true, "target": true, "out": true, "bin": true, "obj": true, ".gradle": true, ".idea": true,
	".tox": true, ".mypy_cache": true, ".pytest_cache": true, ".next": true, ".nuxt": true, "bower_components": true, "Pods": true,
	"DerivedData": true, ".build": true, ".dart_tool": true, "zig-cache": true, "zig-out": true, "_build": true, "deps": true,
	".terraform": true, ".serena": true, ".boundedcode": true, "site-packages": true, "coverage": true}

// Count is how many files of a language a repository has.
type Count struct {
	Language *Language
	Files    int
	Tests    int
}

// CensusOptions bound the walk.
type CensusOptions struct {
	MaxFiles int // default 50000
}

// Census counts the files of each language under root, most files first.
// Files of no known language are counted under Unknown. Symbolic links are
// not followed.
func Census(root string, opt CensusOptions) (counts []Count, unknown int, truncated bool) {
	if opt.MaxFiles <= 0 {
		opt.MaxFiles = 50000
	}
	by := map[string]*Count{}
	n := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // unreadable entries are skipped
		}
		if d.IsDir() {
			if p != root && (SkipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") && d.Name() != ".github") {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		n++
		if n > opt.MaxFiles {
			truncated = true
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		first := ""
		if filepath.Ext(d.Name()) == "" {
			first = firstLine(p)
		}
		l := ForPath(rel, first)
		if l == nil {
			unknown++
			return nil
		}
		c := by[l.ID]
		if c == nil {
			c = &Count{Language: l}
			by[l.ID] = c
		}
		c.Files++
		if l.IsTest(rel) {
			c.Tests++
		}
		return nil
	})
	for _, c := range by {
		counts = append(counts, *c)
	}
	sort.Slice(counts, func(i, j int) bool {
		if counts[i].Files != counts[j].Files {
			return counts[i].Files > counts[j].Files
		}
		return counts[i].Language.ID < counts[j].Language.ID
	})
	return counts, unknown, truncated
}

// firstLine reads a file's first line (up to 200 bytes).
func firstLine(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	b := make([]byte, 200)
	n, _ := io.ReadFull(f, b)
	s := string(b[:n])
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// FirstLine is a file's first line, for ForPath on files without an
// extension.
func FirstLine(p string) string { return firstLine(p) }
