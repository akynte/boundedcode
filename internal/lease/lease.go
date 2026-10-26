// Package lease derives, from the text of a request, the part of a project
// the request is about: the declarations it names, their files and their
// directories. That set is a lease. The direct-mode agent can use it to
// notice edits far outside what was asked (a small model wandering) and to
// list the obligations a change leaves behind (the callers of a changed
// signature).
//
// A lease is only useful if it is accurate, so this package also maps a
// change (a diff) to the declarations it touched, and the benchmark scores
// derived leases against real changes before anything is built on them.
//
// Go is indexed by declaration (functions, methods as Type.Method, types,
// and the names of top-level vars and consts); other files only by path.
package lease

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/akynte/boundedcode/internal/contextplan"
)

// Decl is one top-level Go declaration.
type Decl struct {
	Dir  string // slash-separated directory of the file ("" at the root)
	File string // slash-separated path from the root
	Recv string // receiver type of a method
	Name string
	Lo   int // first line, including the doc comment
	Hi   int // last line
}

// Key identifies a declaration across versions of a file: directory,
// receiver and name.
func (d Decl) Key() string {
	if d.Recv != "" {
		return d.Dir + ":" + d.Recv + "." + d.Name
	}
	return d.Dir + ":" + d.Name
}

// Index is a project's Go declarations and file paths.
type Index struct {
	Decls  []Decl
	Files  map[string]bool // every indexed path, Go or not
	Dirs   map[string]bool // directories holding indexed files
	byName map[string][]int
}

// NewIndex builds an index over paths; parse returns the declarations of
// a Go file (ParseGo, or a cache in front of it).
func NewIndex(paths []string, parse func(path string) []Decl) *Index {
	ix := &Index{Files: map[string]bool{}, Dirs: map[string]bool{}, byName: map[string][]int{}}
	for _, p := range paths {
		if Skipped(p) {
			continue
		}
		ix.Files[p] = true
		for d := path.Dir(p); ; d = path.Dir(d) {
			if d == "." || d == "/" {
				break
			}
			ix.Dirs[d] = true
		}
		if strings.HasSuffix(p, ".go") {
			for _, d := range parse(p) {
				ix.byName[d.Name] = append(ix.byName[d.Name], len(ix.Decls))
				ix.Decls = append(ix.Decls, d)
			}
		}
	}
	return ix
}

// Skipped reports paths that are not part of the project's own code:
// vendored and generated trees, test data, hidden directories.
func Skipped(p string) bool {
	for _, part := range strings.Split(p, "/") {
		switch {
		case part == "vendor" || part == "testdata" || part == "node_modules" || part == "third_party":
			return true
		case strings.HasPrefix(part, ".") && part != "." && part != "..":
			return true
		}
	}
	return false
}

// ParseGo returns the top-level declarations of a Go file (nil when it
// does not parse).
func ParseGo(rel string, src []byte) []Decl {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution|parser.ParseComments)
	if err != nil {
		return nil
	}
	dir := path.Dir(rel)
	if dir == "." {
		dir = ""
	}
	line := func(p token.Pos) int { return fset.Position(p).Line }
	var out []Decl
	add := func(recv, name string, lo, hi int) {
		if name == "" || name == "_" {
			return
		}
		out = append(out, Decl{Dir: dir, File: rel, Recv: recv, Name: name, Lo: lo, Hi: hi})
	}
	for _, d := range f.Decls {
		switch x := d.(type) {
		case *ast.FuncDecl:
			lo := line(x.Pos())
			if x.Doc != nil {
				lo = line(x.Doc.Pos())
			}
			recv := ""
			if x.Recv != nil && len(x.Recv.List) > 0 {
				recv = recvName(x.Recv.List[0].Type)
			}
			add(recv, x.Name.Name, lo, line(x.End()))
		case *ast.GenDecl:
			for _, s := range x.Specs {
				lo, hi := line(s.Pos()), line(s.End())
				if len(x.Specs) == 1 || !x.Lparen.IsValid() {
					lo, hi = line(x.Pos()), line(x.End())
					if x.Doc != nil {
						lo = line(x.Doc.Pos())
					}
				}
				switch sp := s.(type) {
				case *ast.TypeSpec:
					add("", sp.Name.Name, lo, hi)
				case *ast.ValueSpec:
					for _, n := range sp.Names {
						add("", n.Name, lo, hi)
					}
				}
			}
		}
	}
	return out
}

func recvName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.StarExpr:
		return recvName(x.X)
	case *ast.Ident:
		return x.Name
	case *ast.IndexExpr:
		return recvName(x.X)
	case *ast.IndexListExpr:
		return recvName(x.X)
	}
	return ""
}

// Lease is what a request is about.
type Lease struct {
	Symbols map[string]bool // Decl.Key values
	Files   map[string]bool
	Dirs    map[string]bool
	// Matched lists the request's words that resolved, for display.
	Matched []string
}

// Empty reports a lease that names nothing.
func (l Lease) Empty() bool { return len(l.Symbols) == 0 && len(l.Files) == 0 && len(l.Dirs) == 0 }

// Covers reports whether a file is inside the lease: leased itself, or in
// a leased directory.
func (l Lease) Covers(file string) bool {
	if l.Files[file] {
		return true
	}
	return l.Dirs[dirOf(file)]
}

const (
	// maxAmbiguous is how many declarations one bare name may resolve to
	// before it is too common to say anything (String, Close, New).
	maxAmbiguous = 4
	// minPlainName is the shortest bare word looked up.
	minPlainName = 3
)

var (
	wordRE = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*`)
	pathRE = regexp.MustCompile(`[\w.-]+(?:/[\w.-]+)+/?`)
	fileRE = regexp.MustCompile(`\b[\w-]+(?:\.[\w-]+)*\.(?:go|py|ts|tsx|js|jsx|rs|java|kt|rb|php|c|h|cc|cpp|cs|swift|sh|sql|proto|ya?ml|json|toml|md|mod)\b`)
	// codeLike: a word that is unlikely to be English: a capital after the
	// first letter (SetHelpFunc, getCompletions), an underscore or a digit.
	// A plain capitalised word (Print, Usage, Stream) is English as often
	// as it is code, so it needs the request to mark it (backticks, a call).
	codeLike = regexp.MustCompile(`^.+[A-Z]|_|[0-9]`)
)

// Derive returns the lease of a request against an index. A word counts
// when it names a declaration or path that exists: a path or directory
// (by suffix), Type.Method or pkg.Name, or a bare name that looks like
// code (exported, camelCase, snake_case) or that the request marked as
// code (backticks, a call), unless it is too common to point anywhere.
func Derive(ix *Index, request string) Lease {
	l := Lease{Symbols: map[string]bool{}, Files: map[string]bool{}, Dirs: map[string]bool{}}
	matched := map[string]bool{}
	hit := func(w string) {
		if !matched[w] {
			matched[w] = true
			l.Matched = append(l.Matched, w)
		}
	}
	// Words the request itself marks as code (backticks, calls). Marking a
	// name by its kind ("the method Entries") was measured and dropped: it
	// found the rename in the direct suite but took symbol precision on
	// golang.org/x/tools from 38% to 28% ("the type checker").
	marked := map[string]bool{}
	for _, s := range contextplan.Seeds(request) {
		if s.Kind == "name" || s.Kind == "sample" {
			marked[strings.TrimSuffix(s.Text, "()")] = true
		}
	}
	// Paths: files and directories, by suffix; a bare file name with a
	// source or document extension too (cart_test.go, README.md).
	for _, p := range append(pathRE.FindAllString(request, -1), fileRE.FindAllString(request, -1)...) {
		p = strings.Trim(strings.TrimSuffix(p, "/"), ".")
		if p == "" || strings.Contains(p, "://") {
			continue
		}
		if f := ix.suffixFile(p); f != "" {
			l.Files[f] = true
			hit(p)
		} else if d := ix.suffixDir(p); d != "" {
			l.Dirs[d] = true
			hit(p)
		}
	}
	for _, w := range wordRE.FindAllString(request, -1) {
		w = strings.TrimRight(w, ".")
		if parts := strings.Split(w, "."); len(parts) >= 2 {
			// Type.Method, pkg.Name, pkg.Type.Method: the last two parts.
			q, n := parts[len(parts)-2], parts[len(parts)-1]
			var found []Decl
			for _, i := range ix.byName[n] {
				d := ix.Decls[i]
				if d.Recv == q || (d.Recv == "" && path.Base(d.Dir) == q) {
					found = append(found, d)
				}
			}
			if len(found) > 0 && len(found) <= maxAmbiguous {
				for _, d := range found {
					l.Symbols[d.Key()] = true
				}
				hit(w)
			}
			continue
		}
		if len(w) < minPlainName || (!codeLike.MatchString(w) && !marked[w]) || isCommonWord(w) {
			continue
		}
		idx := ix.byName[w]
		if len(idx) == 0 {
			continue
		}
		if len(idx) > maxAmbiguous {
			// Too common on its own; keep the ones in directories the
			// request also named.
			n := 0
			for _, i := range idx {
				if l.Dirs[ix.Decls[i].Dir] {
					l.Symbols[ix.Decls[i].Key()] = true
					n++
				}
			}
			if n > 0 {
				hit(w)
			}
			continue
		}
		for _, i := range idx {
			l.Symbols[ix.Decls[i].Key()] = true
		}
		hit(w)
	}
	for _, d := range ix.Decls {
		if l.Symbols[d.Key()] {
			l.Files[d.File] = true
			l.Dirs[d.Dir] = true
		}
	}
	for f := range l.Files {
		l.Dirs[dirOf(f)] = true
	}
	return l
}

func (ix *Index) suffixFile(p string) string {
	if ix.Files[p] {
		return p
	}
	var best string
	n := 0
	for f := range ix.Files {
		if strings.HasSuffix(f, "/"+p) {
			best = f
			n++
		}
	}
	if n == 1 {
		return best
	}
	return ""
}

func (ix *Index) suffixDir(p string) string {
	if ix.Dirs[p] {
		return p
	}
	var best string
	n := 0
	for d := range ix.Dirs {
		if strings.HasSuffix(d, "/"+p) {
			best = d
			n++
		}
	}
	if n == 1 {
		return best
	}
	return ""
}

func dirOf(file string) string {
	d := path.Dir(file)
	if d == "." {
		return ""
	}
	return d
}

// isCommonWord filters capitalised English at the start of a sentence
// ("Fix", "Add", "The") that happens to name a declaration.
func isCommonWord(w string) bool {
	if !unicode.IsUpper(rune(w[0])) {
		return false
	}
	rest := w[1:]
	if strings.ToLower(rest) != rest {
		return false // camel case is code
	}
	return commonWords[strings.ToLower(w)]
}

var commonWords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`a an the this that these those it its is are was were be been and or not no
		add adds added fix fixes fixed make makes use uses used update updates remove removes delete move rename
		change changes support allow allows avoid handle handles return returns check checks test tests run runs
		set sets get gets new old all any each every some more less only also when if then else for from with
		without into onto over under after before since until while because so but yet do does did done can
		could should would will may might must need needs file files code type types func function method value
		values error errors string bytes name names list map key keys data info config option options default
		revert reverts merge initial cleanup refactor document docs doc comment comments note todo internal
		go gopls cmd tools api client server`) {
		m[w] = true
	}
	return m
}()

// Touched returns the declarations of a Go file whose lines intersect any
// of the given line ranges (inclusive), sorted by line. Lines outside any
// declaration (the package clause, imports) touch nothing.
func Touched(decls []Decl, lines [][2]int) []Decl {
	var out []Decl
	for _, d := range decls {
		for _, r := range lines {
			lo, hi := r[0], r[1]
			if hi < lo { // a pure insertion point: the line it follows
				lo, hi = hi, lo
			}
			if lo <= d.Hi && d.Lo <= hi {
				out = append(out, d)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Lo < out[j].Lo })
	return out
}
