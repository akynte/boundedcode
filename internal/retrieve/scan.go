package retrieve

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/lang"
	"github.com/akynte/boundedcode/internal/policy"
)

// repo is the bounded view of a checkout that one retrieval shares: the
// list of its files (one walk) and content scans for the needles the
// selected strategies look for. Nothing is indexed ahead of time or kept
// between retrievals: a scan reads the files once per pass, within the
// file, byte and time budgets of Options, in path order, so a retrieval is
// deterministic unless the time budget cuts it short (Result.Truncated).
type repo struct {
	root     string
	files    []string            // slash-separated, relative to root, sorted
	byBase   map[string][]string // base name -> files
	langOf   map[string]*lang.Language
	opt      *Options
	deadline time.Time

	scanned   int
	bytesRead int64
	truncated string // why a walk or scan stopped early ("" = complete)
}

// skipFile: lock files, minified bundles and source maps repeat or
// generate code and only add noise.
func skipFile(rel string) bool {
	base := path.Base(rel)
	switch base {
	case "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "go.sum", "Cargo.lock", "poetry.lock", "Gemfile.lock",
		"composer.lock", "Pipfile.lock", "uv.lock", "bun.lockb", "packages.lock.json", "gradle.lockfile", "mix.lock":
		return true
	}
	return strings.Contains(base, ".min.") || strings.HasSuffix(base, ".map") || strings.HasSuffix(base, ".snap")
}

var errStop = errors.New("stop")

// openRepo lists the files under root that retrieval may read: text files
// of a known language or format, outside dependency and build directories,
// hidden directories and secret paths, without following symlinks.
func openRepo(ctx context.Context, root string, opt *Options) (*repo, error) {
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	r := &repo{root: real, byBase: map[string][]string{}, langOf: map[string]*lang.Language{}, opt: opt, deadline: time.Now().Add(opt.Budget)}
	err = filepath.WalkDir(real, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // unreadable entries are skipped
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if p != real && (lang.SkipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(real, p)
		if err != nil {
			return nil //nolint:nilerr // cannot happen under WalkDir
		}
		rel = filepath.ToSlash(rel)
		if policy.IsSecretPath(rel) || skipFile(rel) {
			return nil
		}
		l := lang.ForPath(rel, "")
		if l == nil {
			return nil
		}
		if len(r.files) == opt.MaxFiles {
			r.truncated = "file limit"
			return errStop
		}
		r.files = append(r.files, rel)
		r.langOf[rel] = l
		r.byBase[path.Base(rel)] = append(r.byBase[path.Base(rel)], rel)
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return nil, err
	}
	sort.Strings(r.files)
	return r, nil
}

// read returns a file's content if retrieval may read it: a listed file
// (so inside the root, not a symlink, not secret), within the size limit.
func (r *repo) read(rel string) ([]byte, bool) {
	if _, ok := r.langOf[rel]; !ok {
		return nil, false
	}
	p := filepath.Join(r.root, filepath.FromSlash(rel))
	fi, err := os.Lstat(p)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > r.opt.MaxFileBytes {
		return nil, false
	}
	b, err := os.ReadFile(p)
	if err != nil || bytes.IndexByte(b[:min(len(b), 8000)], 0) >= 0 {
		return nil, false
	}
	return b, true
}

// needle is a string a scan looks for.
type needle struct {
	text string
	word bool // only as a whole word (names), not inside a longer one
}

// hit is one occurrence of a needle.
type hit struct {
	file string
	line int    // 1-based
	text string // the line, trimmed
	def  bool   // the line defines the needle (its language's definition patterns)
}

// scanResult holds, per needle, its hits (definitions and, at most
// maxHitsPerNeedle, other uses, in path order) and the number of files it
// occurs in.
type scanResult struct {
	hits  map[string][]hit
	files map[string]int
	defs  map[string]int // definitions among hits
}

const (
	maxHitsPerNeedle = 60
	maxHitsPerFile   = 3
	maxOccurrences   = 400 // looked at per needle and file
)

// scan reads the listed files once and finds the needles in them.
func (r *repo) scan(ctx context.Context, needles []needle, only func(rel string) bool) scanResult {
	res := scanResult{hits: map[string][]hit{}, files: map[string]int{}, defs: map[string]int{}}
	if len(needles) == 0 {
		return res
	}
	pats := make([][]byte, len(needles))
	for i, n := range needles {
		pats[i] = []byte(n.text)
	}
	for _, rel := range r.files {
		if only != nil && !only(rel) {
			continue
		}
		if ctx.Err() != nil {
			r.truncated = "cancelled"
			return res
		}
		if time.Now().After(r.deadline) {
			r.truncated = "time budget"
			return res
		}
		if r.bytesRead >= r.opt.MaxBytes {
			r.truncated = "byte budget"
			return res
		}
		data, ok := r.read(rel)
		if !ok {
			continue
		}
		r.scanned++
		r.bytesRead += int64(len(data))
		l := r.langOf[rel]
		for i, n := range needles {
			if !bytes.Contains(data, pats[i]) {
				continue
			}
			// Every occurrence is looked at (a definition often follows
			// doc comments that use the name), up to a bound; uses are
			// kept up to maxHitsPerFile per file.
			found, usesHere := 0, 0
			for off, seen := 0, 0; seen < maxOccurrences; seen++ {
				j := bytes.Index(data[off:], pats[i])
				if j < 0 {
					break
				}
				at := off + j
				off = at + len(pats[i])
				if n.word && !wordAt(data, at, len(pats[i])) {
					continue
				}
				if found == 0 {
					res.files[n.text]++
				}
				found++
				ls := bytes.LastIndexByte(data[:at], '\n') + 1
				le := bytes.IndexByte(data[at:], '\n')
				if le < 0 {
					le = len(data)
				} else {
					le += at
				}
				line := string(data[ls:le])
				h := hit{file: rel, line: bytes.Count(data[:at], []byte{'\n'}) + 1, text: strings.TrimSpace(line)}
				if name, ok := l.Definition(line); ok && name != "" && (name == lastSegment(n.text) || strings.HasSuffix(name, "."+lastSegment(n.text))) {
					h.def = true
				}
				// Definitions and uses are capped apart: a name used
				// everywhere must not crowd out where it is defined.
				uses := len(res.hits[n.text]) - res.defs[n.text]
				switch {
				case h.def && res.defs[n.text] < maxHitsPerNeedle:
					res.defs[n.text]++
				case h.def || uses >= maxHitsPerNeedle || usesHere >= maxHitsPerFile:
					continue
				default:
					usesHere++
				}
				res.hits[n.text] = append(res.hits[n.text], h)
			}
		}
	}
	return res
}

func isWordByte(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// wordAt: the n bytes at i are not part of a longer identifier.
func wordAt(b []byte, i, n int) bool {
	if i > 0 && isWordByte(b[i-1]) && isWordByte(b[i]) {
		return false
	}
	if e := i + n; e < len(b) && isWordByte(b[e]) && isWordByte(b[e-1]) {
		return false
	}
	return true
}

// resolve maps a path from a trace, a diagnostic or the request onto the
// listed files: the file itself, or the files whose path ends with the
// longest part of it (traces print absolute paths from another machine,
// test runners paths relative to a package). hint (a JVM frame's symbol,
// "com.acme.Orders.total") narrows a bare file name by package. At most
// three equally good matches are returned.
func (r *repo) resolve(p, hint string) []string {
	p = filepath.ToSlash(strings.TrimSpace(p))
	p = strings.TrimPrefix(p, "file://")
	p = strings.TrimPrefix(p, "./")
	if _, ok := r.langOf[p]; ok {
		return []string{p}
	}
	if rel, err := filepath.Rel(r.root, filepath.FromSlash(p)); err == nil && !strings.HasPrefix(rel, "..") {
		if _, ok := r.langOf[filepath.ToSlash(rel)]; ok {
			return []string{filepath.ToSlash(rel)}
		}
	}
	base := path.Base(p)
	cands := r.byBase[base]
	if len(cands) == 0 {
		return nil
	}
	want := strings.Split(p, "/")
	if !strings.Contains(p, "/") && hint != "" {
		// com.acme.orders.Orders.total -> com/acme/orders/Orders.java
		parts := strings.Split(strings.ReplaceAll(hint, "/", "."), ".")
		if len(parts) > 2 {
			want = append(parts[:len(parts)-2:len(parts)-2], base)
		}
	}
	best, bestN := []string(nil), 0
	for _, c := range cands {
		n := commonSuffix(strings.Split(c, "/"), want)
		switch {
		case n > bestN:
			best, bestN = []string{c}, n
		case n == bestN:
			best = append(best, c)
		}
	}
	if len(best) > 3 {
		return nil // a common file name with nothing to tell the copies apart
	}
	return best
}

func commonSuffix(a, b []string) int {
	n := 0
	for i, j := len(a)-1, len(b)-1; i >= 0 && j >= 0 && a[i] == b[j]; i, j = i-1, j-1 {
		n++
	}
	return n
}

// enclosing returns the definition that contains line in rel ("" when
// none is found), from its language's definition patterns.
func (r *repo) enclosing(rel string, line int) (string, int) {
	data, ok := r.read(rel)
	if !ok {
		return "", 0
	}
	l := r.langOf[rel]
	lines := strings.Split(string(data), "\n")
	if line < 1 || line > len(lines) {
		return "", 0
	}
	// The nearest definition above that is indented less than the line (a
	// nested function that ended before it does not enclose it).
	indent := func(s string) int { return len(s) - len(strings.TrimLeft(s, " \t")) }
	target := indent(lines[line-1])
	if name, ok := l.Definition(lines[line-1]); ok && name != "" {
		return name, line
	}
	for i := line - 2; i >= 0 && line-i < 400; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		if name, ok := l.Definition(lines[i]); ok && name != "" && indent(lines[i]) < max(target, 1) {
			return name, i + 1
		}
	}
	return "", 0
}

// byStem lists the files named stem plus any extension ("OrdersTest" ->
// src/test/java/acme/OrdersTest.java), at most three.
func (r *repo) byStem(stem string) []string {
	var out []string
	for base, files := range r.byBase {
		if strings.TrimSuffix(base, path.Ext(base)) == stem {
			out = append(out, files...)
		}
	}
	sort.Strings(out)
	if len(out) > 3 {
		return nil
	}
	return out
}

// Enclosing returns the definition enclosing line in root/rel ("" when
// none is found), by the file's language's definition patterns.
func Enclosing(root, rel string, line int) string {
	opt := Options{Root: root}
	opt.defaults()
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return ""
	}
	l := lang.ForPath(rel, "")
	if l == nil || policy.IsSecretPath(rel) {
		return ""
	}
	r := &repo{root: real, langOf: map[string]*lang.Language{rel: l}, opt: &opt}
	name, _ := r.enclosing(rel, line)
	return name
}
