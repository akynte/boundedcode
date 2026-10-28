package verify

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
)

// goMutants finds mutation points in a Go file from its syntax tree, on the
// lines in lines: relational, equality and logical operators, + and -,
// integer bounds in comparisons, boolean literals, if conditions (negated),
// ++/--, and bool or numeric return values.
func goMutants(rel string, src []byte, lines map[int]bool) []Mutant {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	off := func(p token.Pos) int { return fset.Position(p).Offset }
	line := func(p token.Pos) int { return fset.Position(p).Line }
	var out []Mutant
	add := func(start, end int, repl, op string) {
		if start < 0 || end > len(src) || start >= end {
			return
		}
		out = append(out, Mutant{File: rel, Line: lineAt(src, start), Start: start, End: end, Orig: string(src[start:end]), Repl: repl, Operator: op})
	}
	// A relational operator is negated (scored) and shifted at its bound
	// (reported only: often equivalent, see boundaryOperator).
	swap := map[token.Token]struct {
		to token.Token
		op string
	}{
		token.LSS: {token.GEQ, "relational"}, token.LEQ: {token.GTR, "relational"}, token.GTR: {token.LEQ, "relational"},
		token.GEQ: {token.LSS, "relational"}, token.EQL: {token.NEQ, "relational"}, token.NEQ: {token.EQL, "relational"},
		token.LAND: {token.LOR, "logical"}, token.LOR: {token.LAND, "logical"}, token.ADD: {token.SUB, "arithmetic"},
		token.SUB: {token.ADD, "arithmetic"},
	}
	shift := map[token.Token]token.Token{token.LSS: token.LEQ, token.LEQ: token.LSS, token.GTR: token.GEQ, token.GEQ: token.GTR}
	// The result types of the enclosing function, for return mutations.
	var results []string
	var walk func(n ast.Node) bool
	walk = func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncDecl:
			saved := results
			results = resultTypes(n.Type)
			if n.Body != nil {
				ast.Inspect(n.Body, walk)
			}
			results = saved
			return false
		case *ast.FuncLit:
			saved := results
			results = resultTypes(n.Type)
			ast.Inspect(n.Body, walk)
			results = saved
			return false
		case *ast.BinaryExpr:
			if s, ok := swap[n.Op]; ok && lines[line(n.OpPos)] {
				add(off(n.OpPos), off(n.OpPos)+len(n.Op.String()), s.to.String(), s.op)
				if to, ok := shift[n.Op]; ok {
					add(off(n.OpPos), off(n.OpPos)+len(n.Op.String()), to.String(), "boundary")
				}
			}
			if isComparison(n.Op) {
				for _, side := range []ast.Expr{n.X, n.Y} {
					if lit, ok := side.(*ast.BasicLit); ok && lit.Kind == token.INT && lines[line(lit.Pos())] {
						if v, ok := intLit(lit.Value); ok {
							add(off(lit.Pos()), off(lit.End()), itoa(v+1), "boundary")
						}
					}
				}
			}
		case *ast.Ident:
			if (n.Name == "true" || n.Name == "false") && lines[line(n.Pos())] {
				add(off(n.Pos()), off(n.End()), map[string]string{"true": "false", "false": "true"}[n.Name], "constant")
			}
		case *ast.IfStmt:
			if n.Cond != nil && lines[line(n.Cond.Pos())] {
				s, e := off(n.Cond.Pos()), off(n.Cond.End())
				add(s, e, "!("+string(src[s:e])+")", "negate")
			}
		case *ast.IncDecStmt:
			if lines[line(n.TokPos)] {
				add(off(n.TokPos), off(n.TokPos)+2, map[token.Token]string{token.INC: "--", token.DEC: "++"}[n.Tok], "arithmetic")
			}
		case *ast.ReturnStmt:
			if len(n.Results) == 1 && len(results) == 1 && lines[line(n.Pos())] {
				r := n.Results[0]
				s, e := off(r.Pos()), off(r.End())
				switch results[0] {
				case "bool":
					if id, ok := r.(*ast.Ident); !ok || (id.Name != "true" && id.Name != "false") { // literals: "constant" above
						add(s, e, "!("+string(src[s:e])+")", "return")
					}
				case "int", "int64", "int32", "uint", "uint64", "uint32", "float64", "float32":
					add(s, e, "("+string(src[s:e])+")+1", "return")
				}
			}
		}
		return true
	}
	ast.Inspect(f, walk)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

func resultTypes(ft *ast.FuncType) []string {
	if ft == nil || ft.Results == nil {
		return nil
	}
	var out []string
	for _, f := range ft.Results.List {
		name := ""
		if id, ok := f.Type.(*ast.Ident); ok {
			name = id.Name
		}
		n := len(f.Names)
		if n == 0 {
			n = 1
		}
		for i := 0; i < n; i++ {
			out = append(out, name)
		}
	}
	return out
}

func isComparison(t token.Token) bool {
	switch t {
	case token.LSS, token.LEQ, token.GTR, token.GEQ, token.EQL, token.NEQ:
		return true
	}
	return false
}

// goParses reports whether a mutated Go file still parses.
func goParses(rel string, src []byte) bool {
	_, err := parser.ParseFile(token.NewFileSet(), rel, src, parser.SkipObjectResolution)
	return err == nil
}
