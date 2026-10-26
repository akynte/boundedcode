package direct

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
)

// Some mistakes compile, pass a quick test and are still wrong. The ones
// here are made often by small models and can be recognised from the
// syntax alone with almost no false alarms; they are reported after an
// edit as notes, not errors, and only when the edit introduced them.

// pitfall is one finding: a stable key (function and variable, so a
// finding the file already had is not reported again) and the note.
type pitfall struct {
	key  string
	line int
	note string
}

// goPitfalls returns the findings in a Go file (nil when it does not
// parse).
func goPitfalls(rel string, src []byte) []pitfall {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	var out []pitfall
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		fn := fd.Name.Name
		if fd.Recv != nil && len(fd.Recv.List) > 0 {
			fn = recvTypeName(fd.Recv.List[0].Type) + "." + fn
		}
		locals := localNames(fd.Body)
		seen := map[token.Pos]bool{}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			rs, ok := n.(*ast.RangeStmt)
			if !ok || rs.Tok != token.DEFINE {
				return true
			}
			v, ok := rs.Value.(*ast.Ident)
			if !ok || v.Name == "_" {
				return true
			}
			// A slice the function built itself: a copy of its element
			// cannot surprise anyone (net.InterfaceByName does this).
			if id, ok := rs.X.(*ast.Ident); ok && locals[id.Name] {
				return true
			}
			for _, ret := range loopReturns(rs.Body) {
				for _, res := range ret.Results {
					u, ok := res.(*ast.UnaryExpr)
					if !ok || u.Op != token.AND {
						continue
					}
					if id, ok := u.X.(*ast.Ident); ok && id.Name == v.Name && !seen[u.Pos()] {
						seen[u.Pos()] = true
						line := fset.Position(u.Pos()).Line
						over := exprString(fset, rs.X)
						idx := "i"
						if k, ok := rs.Key.(*ast.Ident); ok && k.Name != "_" {
							idx = k.Name
						}
						out = append(out, pitfall{
							key:  fn + ":&" + v.Name,
							line: line,
							note: fmt.Sprintf("%s:%d: %s returns &%s, a pointer to the range loop's copy of the element, not into %s: changes made through it do not reach %s. If the caller should get the element itself, range over the index and return &%s[%s].",
								rel, line, fn, v.Name, over, over, over, idx),
						})
					}
				}
			}
			return true
		})
	}
	return out
}

// localNames are the names a function body declares (:= and var),
// outside function literals.
func localNames(body *ast.BlockStmt) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE {
				for _, l := range x.Lhs {
					if id, ok := l.(*ast.Ident); ok {
						out[id.Name] = true
					}
				}
			}
		case *ast.ValueSpec:
			for _, id := range x.Names {
				out[id.Name] = true
			}
		}
		return true
	})
	return out
}

// loopReturns lists the return statements of a loop body, not those of
// function literals inside it.
func loopReturns(body *ast.BlockStmt) []*ast.ReturnStmt {
	var out []*ast.ReturnStmt
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			out = append(out, x)
		}
		return true
	})
	return out
}

func recvTypeName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.StarExpr:
		return recvTypeName(x.X)
	case *ast.Ident:
		return x.Name
	case *ast.IndexExpr:
		return recvTypeName(x.X)
	case *ast.IndexListExpr:
		return recvTypeName(x.X)
	}
	return "?"
}

func exprString(fset *token.FileSet, e ast.Expr) string {
	var b strings.Builder
	if err := printer.Fprint(&b, fset, e); err != nil {
		return "the slice"
	}
	return b.String()
}

// newPitfalls returns the notes for findings in after that before (the
// file's content before the edit, nil for a new file) did not have.
func newPitfalls(rel string, before, after []byte) []string {
	had := map[string]bool{}
	for _, p := range goPitfalls(rel, before) {
		had[p.key] = true
	}
	var notes []string
	for _, p := range goPitfalls(rel, after) {
		if !had[p.key] {
			notes = append(notes, p.note)
		}
	}
	return notes
}
