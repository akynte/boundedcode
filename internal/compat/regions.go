package compat

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/akynte/boundedcode/internal/gitops"
)

// hunk is a changed line range of one file: Old lines at the base commit,
// New lines at the head commit (a zero length is an insertion or deletion
// point).
type hunk struct{ OldStart, OldLen, NewStart, NewLen int }

// fileHunks maps a changed file to its hunks; a file added or deleted has a
// single hunk covering it.
type fileHunks map[string][]hunk

var hunkRE = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// diffHunks reads the zero-context diff between two commits.
func diffHunks(ctx context.Context, worktree, base, head string) (fileHunks, error) {
	out := fileHunks{}
	if base == head {
		return out, nil
	}
	patch, err := gitops.Run(ctx, worktree, "diff", "-U0", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames", base, head)
	if err != nil {
		return nil, err
	}
	oldFile, newFile := "", ""
	for l := range strings.SplitSeq(patch, "\n") {
		switch {
		case strings.HasPrefix(l, "--- "):
			oldFile = strings.TrimPrefix(strings.TrimPrefix(l, "--- "), "a/")
		case strings.HasPrefix(l, "+++ "):
			newFile = strings.TrimPrefix(strings.TrimPrefix(l, "+++ "), "b/")
		case strings.HasPrefix(l, "@@"):
			m := hunkRE.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			num := func(s string, def int) int {
				if s == "" {
					return def
				}
				n, _ := strconv.Atoi(s)
				return n
			}
			h := hunk{num(m[1], 0), num(m[2], 1), num(m[3], 0), num(m[4], 1)}
			f := newFile
			if f == "/dev/null" {
				f = oldFile
			}
			out[f] = append(out[f], h)
		}
	}
	return out, nil
}

// span is an inclusive line range.
type span struct{ Lo, Hi int }

// touches reports whether a file's change intersects any of the spans, at
// the base (old) or head (new) version of the file.
func (fh fileHunks) touches(file string, head bool, spans []span) bool {
	for _, h := range fh[file] {
		start, n := h.OldStart, h.OldLen
		if head {
			start, n = h.NewStart, h.NewLen
		}
		lo, hi := start, start+n-1
		if n == 0 { // an insertion or deletion after line start
			lo, hi = start, start+1
		}
		for _, s := range spans {
			if lo <= s.Hi && s.Lo <= hi {
				return true
			}
		}
	}
	return false
}

// goFunc is a function or method declaration.
type goFunc struct {
	Name, Recv string
	Span       span
	File       string
	// ReqType is the second parameter's type when it is a pointer to a
	// selector (pkg.Type), as in a gRPC method implementation.
	ReqPkg, ReqType string
}

// goFuncs parses a Go file's declarations (nil when it does not parse).
func goFuncs(path, rel string) ([]goFunc, map[string]string) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, nil
	}
	imports := map[string]string{} // local name -> import path
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		name := p[strings.LastIndexByte(p, '/')+1:]
		if im.Name != nil {
			name = im.Name.Name
		}
		imports[name] = p
	}
	var out []goFunc
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		g := goFunc{Name: fd.Name.Name, File: rel, Span: span{fset.Position(fd.Pos()).Line, fset.Position(fd.End()).Line}}
		if fd.Recv != nil && len(fd.Recv.List) > 0 {
			g.Recv = recvName(fd.Recv.List[0].Type)
		}
		var params []ast.Expr
		for _, p := range fd.Type.Params.List {
			for range max(len(p.Names), 1) {
				params = append(params, p.Type)
			}
		}
		if len(params) >= 2 {
			if st, ok := params[1].(*ast.StarExpr); ok {
				if sel, ok := st.X.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok {
						g.ReqPkg, g.ReqType = imports[id.Name], sel.Sel.Name
					}
				}
			}
		}
		out = append(out, g)
	}
	return out, imports
}

func recvName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.StarExpr:
		return recvName(x.X)
	case *ast.Ident:
		return x.Name
	case *ast.IndexExpr:
		return recvName(x.X)
	}
	return ""
}

// enclosing returns the span of the function containing a line, or the line
// itself.
func enclosing(funcs []goFunc, line int) span {
	for _, f := range funcs {
		if f.Span.Lo <= line && line <= f.Span.Hi {
			return f.Span
		}
	}
	return span{line, line}
}

// goMethodName is the Go method protoc-gen-go generates for an RPC.
func goMethodName(rpc string) string {
	var b strings.Builder
	up := true
	for _, r := range rpc {
		if r == '_' {
			up = true
			continue
		}
		if up {
			r = unicode.ToUpper(r)
		}
		up = false
		b.WriteRune(r)
	}
	return b.String()
}

// rpcImplementations finds the methods of a Go repository tree that
// implement an RPC: methods named after it whose request parameter is a
// pointer to a type of the generated package (importPath; any when empty).
// Test files and vendored code are skipped.
func rpcImplementations(root, importPath string, rpcs []string) []goFunc {
	want := map[string]bool{}
	for _, r := range rpcs {
		want[goMethodName(r)] = true
	}
	var out []goFunc
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // unreadable entries are skipped
		}
		if d.IsDir() {
			if p != root && (d.Name() == "vendor" || d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil || generatedGo(b) {
			return nil //nolint:nilerr // unreadable files are skipped
		}
		rel, _ := filepath.Rel(root, p)
		funcs, _ := goFuncs(p, filepath.ToSlash(rel))
		for _, f := range funcs {
			if f.Recv != "" && want[f.Name] && f.ReqType != "" && (importPath == "" || f.ReqPkg == importPath) {
				out = append(out, f)
			}
		}
		return nil
	})
	return out
}

func generatedGo(b []byte) bool {
	head := b[:min(len(b), 2048)]
	return strings.Contains(string(head), "Code generated") && strings.Contains(string(head), "DO NOT EDIT")
}

// goModule reads the module path and go version of a tree's root go.mod.
func goModule(root string) (path, goVersion string, ok bool) {
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", "", false
	}
	for l := range strings.SplitSeq(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && f[0] == "module" {
			path = strings.Trim(f[1], `"`)
		}
		if len(f) >= 2 && f[0] == "go" {
			goVersion = f[1]
		}
	}
	return path, goVersion, path != ""
}

// importPathOf is the Go import path of a repository-relative file's
// package, for a module rooted at the repository root.
func importPathOf(module, rel string) string {
	dir := filepath.ToSlash(filepath.Dir(rel))
	if dir == "." {
		return module
	}
	return module + "/" + dir
}
