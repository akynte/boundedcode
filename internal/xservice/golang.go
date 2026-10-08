package xservice

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// goModule is one Go module inside a repository.
type goModule struct {
	dir  string // absolute
	path string // module path from go.mod
}

// goFile is a parsed (non-test) source file.
type goFile struct {
	rel       string // repo-relative
	pkgPath   string // import path
	module    string // module path ("" outside a module)
	file      *ast.File
	imports   map[string]string // local name -> import path
	generated bool              // "Code generated ... DO NOT EDIT."
	mayGRPC   bool              // the source mentions a client constructor (cheap pre-check)
	grpcLike  bool              // imports a gRPC or Connect package
	testPath  bool              // under a test directory: no gRPC, proto or SQL contracts
	// grpcFields are struct fields assigned a gRPC client (s.payments =
	// pb.NewPaymentServiceClient(conn)), so calls through them are RPCs.
	grpcFields map[string]grpcClient
	sqlLib     string // an imported SQL builder or ORM (gorm, squirrel, goqu, bun)
}

// grpcClient is a gRPC client constructor's service and package.
type grpcClient struct{ service, ref string }

// goAnalyzer extracts endpoints from Go sources of one repository.
type goAnalyzer struct {
	repo   string
	root   string
	fset   *token.FileSet
	files  []*goFile
	consts map[string]ast.Expr // "importpath.Name" -> value expression
	diags  []Diagnostic
	out    []Endpoint
	budget evalBudget // of the current top-level evaluation (see budget.go)
	// sqlDone marks string expressions already analyzed as part of a larger
	// one (a concatenation or a Sprintf format).
	sqlDone map[ast.Node]bool
	// protoUses records generated-code imports per package directory.
	protoUses map[string]bool
}

func analyzeGo(repo, root string, relFiles []string) ([]Endpoint, []Diagnostic) {
	a := &goAnalyzer{repo: repo, root: root, fset: token.NewFileSet(), consts: map[string]ast.Expr{}, sqlDone: map[ast.Node]bool{},
		protoUses: map[string]bool{}}
	mods := findGoModules(root, relFiles)
	for _, rel := range relFiles {
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		f, err := parser.ParseFile(a.fset, rel, src, parser.SkipObjectResolution)
		if err != nil {
			a.diags = append(a.diags, Diagnostic{File: rel, Message: "go parse: " + err.Error()})
			continue
		}
		dir := filepath.Join(root, filepath.Dir(rel))
		gf := &goFile{rel: filepath.ToSlash(rel), pkgPath: importPathFor(mods, dir), module: moduleFor(mods, dir), file: f,
			imports: map[string]string{}, generated: isGeneratedGo(f), grpcFields: map[string]grpcClient{},
			mayGRPC: bytes.Contains(src, []byte("Client(")), testPath: sourceTestPath(rel)}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			name := path.Base(p)
			if v := majorSuffix(p); v != "" { // github.com/x/y/v2 -> y
				name = path.Base(strings.TrimSuffix(p, "/"+v))
			}
			if imp.Name != nil {
				name = imp.Name.Name
			}
			gf.imports[name] = p
			if hasAnyPrefix(p, grpcPackages) {
				gf.grpcLike = true
			}
			for _, lib := range goSQLLibs {
				if p == lib || strings.HasPrefix(p, lib+"/") {
					gf.sqlLib = path.Base(lib)
				}
			}
		}
		a.files = append(a.files, gf)
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
				continue
			}
			for _, sp := range gd.Specs {
				vs := sp.(*ast.ValueSpec)
				for i, n := range vs.Names {
					if i < len(vs.Values) {
						a.consts[gf.pkgPath+"."+n.Name] = vs.Values[i]
					}
				}
			}
		}
	}
	for _, gf := range a.files {
		a.sqlDone = map[ast.Node]bool{} // per file: nodes are not shared
		a.protoImports(gf)
		a.collectGRPCFields(gf)
		a.walkFile(gf)
	}
	return a.out, a.diags
}

// grpcPackages are the gRPC and Connect runtimes; a file importing one
// makes NewXClient constructors in it gRPC clients.
var grpcPackages = []string{"google.golang.org/grpc", "connectrpc.com/connect", "github.com/bufbuild/connect-go",
	"github.com/grpc-ecosystem/grpc-gateway"}

// generatedPkgRE matches import paths that hold generated protobuf code.
var generatedPkgRE = regexp.MustCompile(`(^|/)(gen|genproto|proto|protos|pb)(/|$)|pb(/v\d+)?$|proto$|connect$`)

// goSQLLibs are query builders and ORMs whose table arguments are tables.
var goSQLLibs = []string{"gorm.io/gorm", "github.com/jinzhu/gorm", "github.com/Masterminds/squirrel", "github.com/doug-martin/goqu",
	"github.com/uptrace/bun", "github.com/go-pg/pg"}

func moduleFor(mods []goModule, dir string) string {
	best := goModule{}
	for _, m := range mods {
		if (dir == m.dir || strings.HasPrefix(dir, m.dir+string(filepath.Separator))) && len(m.dir) > len(best.dir) {
			best = m
		}
	}
	return best.path
}

func isGeneratedGo(f *ast.File) bool {
	for _, cg := range f.Comments {
		if cg.Pos() > f.Package {
			break
		}
		for _, c := range cg.List {
			if strings.HasPrefix(c.Text, "// Code generated ") && strings.HasSuffix(c.Text, " DO NOT EDIT.") {
				return true
			}
		}
	}
	return false
}

// protoImports records imports that may be generated protobuf code: any
// import outside the standard library and the file's own module, once per
// package directory. Linking keeps those whose path is a .proto file's
// go_package.
func (a *goAnalyzer) protoImports(gf *goFile) {
	if gf.generated || gf.testPath {
		return
	}
	for _, imp := range gf.file.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		first, _, _ := strings.Cut(p, "/")
		if !strings.Contains(first, ".") || gf.module != "" && (p == gf.module || strings.HasPrefix(p, gf.module+"/")) {
			continue
		}
		k := path.Dir(gf.rel) + "|" + p
		if a.protoUses[k] {
			continue
		}
		a.protoUses[k] = true
		a.out = append(a.out, Endpoint{Kind: ProtoUse, Repo: a.repo, File: gf.rel, Line: a.fset.Position(imp.Pos()).Line,
			Ref: "go:" + p, Confidence: Exact, Detail: "import"})
	}
}

// grpcConstructor recognizes pkg.NewXClient(conn) (grpc-go, connect-go).
func (a *goAnalyzer) grpcConstructor(gf *goFile, e ast.Expr) (grpcClient, bool) {
	c, ok := e.(*ast.CallExpr)
	if !ok {
		return grpcClient{}, false
	}
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return grpcClient{}, false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return grpcClient{}, false
	}
	p, ok := gf.imports[id.Name]
	if !ok {
		return grpcClient{}, false
	}
	name := sel.Sel.Name
	svc, ok := strings.CutPrefix(name, "New")
	if !ok || !strings.HasSuffix(svc, "Client") || len(svc) == len("Client") {
		return grpcClient{}, false
	}
	// NewXClient is a common constructor name: it is a gRPC client when the
	// file uses gRPC, the argument is a connection, or the package is
	// generated code.
	conn := len(c.Args) > 0 && strings.Contains(strings.ToLower(exprString(c.Args[0])), "conn") ||
		len(c.Args) > 0 && exprString(c.Args[0]) == "cc"
	if !gf.grpcLike && !conn && !generatedPkgRE.MatchString(p) {
		return grpcClient{}, false
	}
	return grpcClient{service: strings.TrimSuffix(svc, "Client"), ref: "go:" + p}, true
}

// collectGRPCFields finds struct fields holding gRPC clients.
func (a *goAnalyzer) collectGRPCFields(gf *goFile) {
	if gf.generated || gf.testPath || !gf.mayGRPC {
		return
	}
	ast.Inspect(gf.file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			for i, l := range x.Lhs {
				if sel, ok := l.(*ast.SelectorExpr); ok && i < len(x.Rhs) {
					if gc, ok := a.grpcConstructor(gf, x.Rhs[i]); ok {
						gf.grpcFields[sel.Sel.Name] = gc
					}
				}
			}
		case *ast.KeyValueExpr:
			if k, ok := x.Key.(*ast.Ident); ok {
				if gc, ok := a.grpcConstructor(gf, x.Value); ok {
					gf.grpcFields[k.Name] = gc
				}
			}
		}
		return true
	})
}

func majorSuffix(p string) string {
	b := path.Base(p)
	if len(b) >= 2 && b[0] == 'v' {
		if _, err := strconv.Atoi(b[1:]); err == nil {
			return b
		}
	}
	return ""
}

func findGoModules(root string, relFiles []string) []goModule {
	seen := map[string]bool{}
	var mods []goModule
	for _, rel := range relFiles {
		for dir := filepath.Join(root, filepath.Dir(rel)); strings.HasPrefix(dir, root); dir = filepath.Dir(dir) {
			if seen[dir] {
				break
			}
			seen[dir] = true
			if b, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
				for l := range strings.SplitSeq(string(b), "\n") {
					if mp, ok := strings.CutPrefix(strings.TrimSpace(l), "module "); ok {
						mods = append(mods, goModule{dir: dir, path: strings.Trim(strings.TrimSpace(mp), `"`)})
						break
					}
				}
			}
			if dir == root {
				break
			}
		}
	}
	return mods
}

func importPathFor(mods []goModule, dir string) string {
	best := goModule{}
	for _, m := range mods {
		if (dir == m.dir || strings.HasPrefix(dir, m.dir+string(filepath.Separator))) && len(m.dir) > len(best.dir) {
			best = m
		}
	}
	if best.dir == "" {
		return "local/" + filepath.ToSlash(dir)
	}
	rel, _ := filepath.Rel(best.dir, dir)
	if rel == "." {
		return best.path
	}
	return best.path + "/" + filepath.ToSlash(rel)
}

// scope holds per-function bindings used during value resolution.
type scope struct {
	gf       *goFile
	locals   map[string]ast.Expr // local string bindings
	prefixes map[string]string   // router var -> path prefix
	symbol   string
	methods  map[*ast.CallExpr][]string // gorilla .Methods(...) by inner HandleFunc call
}

func (a *goAnalyzer) walkFile(gf *goFile) {
	for _, d := range gf.file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if ok {
			sym := fd.Name.Name
			if fd.Recv != nil && len(fd.Recv.List) == 1 {
				sym = recvName(fd.Recv.List[0].Type) + "." + sym
			}
			if fd.Name.Name == "TableName" && !gf.testPath && fd.Recv != nil && fd.Body != nil && len(fd.Body.List) == 1 {
				if ret, ok := fd.Body.List[0].(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
					if lit, ok := ret.Results[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if t, err := strconv.Unquote(lit.Value); err == nil && sqlTableNameRE.MatchString(t) {
							a.out = append(a.out, Endpoint{Kind: SQLAccess, Repo: a.repo, File: gf.rel, Line: a.fset.Position(fd.Pos()).Line,
								Symbol: sym, Table: strings.ToLower(t), Confidence: Exact, Detail: "TableName (ORM model)"})
						}
					}
				}
			}
			if fd.Body != nil {
				a.walkBody(&scope{gf: gf, locals: map[string]ast.Expr{}, prefixes: map[string]string{}, symbol: sym, methods: map[*ast.CallExpr][]string{}}, fd.Body)
			}
			continue
		}
		// Package-level: struct tags (env) and composite literals in var decls.
		gd, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		sc := &scope{gf: gf, locals: map[string]ast.Expr{}, prefixes: map[string]string{}, methods: map[*ast.CallExpr][]string{}}
		ast.Inspect(gd, func(n ast.Node) bool { a.visit(sc, n); return true })
	}
}

func recvName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return "(*" + recvName(t.X) + ")"
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return recvName(t.X)
	case *ast.IndexListExpr:
		return recvName(t.X)
	}
	return "?"
}

func (a *goAnalyzer) walkBody(sc *scope, body ast.Node) {
	// Pass 1: gorilla .Methods chains and local bindings.
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Methods" {
				if inner, ok := sel.X.(*ast.CallExpr); ok {
					for _, arg := range x.Args {
						if m := a.method(sc, arg); m != "" {
							sc.methods[inner] = append(sc.methods[inner], m)
						}
					}
				}
			}
		case *ast.AssignStmt:
			if len(x.Lhs) == len(x.Rhs) {
				for i, l := range x.Lhs {
					if id, ok := l.(*ast.Ident); ok && id.Name != "_" {
						sc.locals[id.Name] = x.Rhs[i]
						a.bindPrefix(sc, id.Name, x.Rhs[i])
					}
				}
			}
		case *ast.DeclStmt:
			if gd, ok := x.Decl.(*ast.GenDecl); ok {
				for _, sp := range gd.Specs {
					if vs, ok := sp.(*ast.ValueSpec); ok {
						for i, nm := range vs.Names {
							if i < len(vs.Values) {
								sc.locals[nm.Name] = vs.Values[i]
								a.bindPrefix(sc, nm.Name, vs.Values[i])
							}
						}
					}
				}
			}
		}
		return true
	})
	// Pass 2: endpoints. chi Route closures get their own prefixed scope.
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Route" && len(call.Args) == 2 {
				if fl, ok := call.Args[1].(*ast.FuncLit); ok && len(fl.Type.Params.List) == 1 && len(fl.Type.Params.List[0].Names) == 1 {
					if p, conf := a.eval(sc, call.Args[0]); conf != Partial {
						inner := &scope{gf: sc.gf, locals: sc.locals, prefixes: copyMap(sc.prefixes), symbol: sc.symbol, methods: sc.methods}
						inner.prefixes[fl.Type.Params.List[0].Names[0].Name] = joinPath(a.prefixOf(sc, sel.X), p)
						a.walkBody(inner, fl.Body)
						return false
					}
				}
			}
		}
		a.visit(sc, n)
		return true
	})
}

func copyMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// bindPrefix records `g := r.Group("/v1")` (gin/echo/fiber/chi-like) prefixes.
func (a *goAnalyzer) bindPrefix(sc *scope, name string, rhs ast.Expr) {
	call, ok := rhs.(*ast.CallExpr)
	if !ok {
		return
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (len(call.Args) == 0 && sel.Sel.Name != "Subrouter") {
		return
	}
	switch sel.Sel.Name {
	case "Group", "PathPrefix", "Subrouter":
	default:
		return
	}
	if sel.Sel.Name == "Subrouter" { // gorilla: r.PathPrefix("/v1").Subrouter()
		if inner, ok := sel.X.(*ast.CallExpr); ok {
			if isel, ok := inner.Fun.(*ast.SelectorExpr); ok && isel.Sel.Name == "PathPrefix" && len(inner.Args) == 1 {
				if p, conf := a.eval(sc, inner.Args[0]); conf != Partial {
					sc.prefixes[name] = joinPath(a.prefixOf(sc, isel.X), p)
				}
			}
		}
		return
	}
	if p, conf := a.eval(sc, call.Args[0]); conf != Partial && strings.HasPrefix(p, "/") {
		sc.prefixes[name] = joinPath(a.prefixOf(sc, sel.X), p)
	}
}

func (a *goAnalyzer) prefixOf(sc *scope, x ast.Expr) string {
	if id, ok := x.(*ast.Ident); ok {
		return sc.prefixes[id.Name]
	}
	return ""
}

func joinPath(prefix, p string) string {
	if prefix == "" {
		return p
	}
	return strings.TrimRight(prefix, "/") + "/" + strings.TrimLeft(p, "/")
}

var (
	httpMethodNames = map[string]string{"Get": "GET", "Post": "POST", "Put": "PUT", "Patch": "PATCH", "Delete": "DELETE",
		"Head": "HEAD", "Options": "OPTIONS", "GET": "GET", "POST": "POST", "PUT": "PUT", "PATCH": "PATCH",
		"DELETE": "DELETE", "HEAD": "HEAD", "OPTIONS": "OPTIONS", "Any": "", "All": "", "Handle": "", "HandleFunc": ""}
	stdMethodConsts = map[string]string{"MethodGet": "GET", "MethodPost": "POST", "MethodPut": "PUT", "MethodPatch": "PATCH",
		"MethodDelete": "DELETE", "MethodHead": "HEAD", "MethodOptions": "OPTIONS"}
)

func (a *goAnalyzer) emit(sc *scope, pos token.Pos, e Endpoint) {
	p := a.fset.Position(pos)
	e.Repo, e.File, e.Line = a.repo, sc.gf.rel, p.Line
	if e.Symbol == "" {
		e.Symbol = sc.symbol
	}
	a.out = append(a.out, e)
}

func (a *goAnalyzer) visit(sc *scope, n ast.Node) {
	switch x := n.(type) {
	case *ast.BasicLit:
		if x.Kind == token.STRING && !a.sqlDone[x] {
			if v, err := strconv.Unquote(x.Value); err == nil && looksLikeSQL(v) {
				a.emitSQL(sc, x, v, Exact)
			}
		}
	case *ast.CallExpr:
		a.visitCall(sc, x)
	case *ast.CompositeLit:
		a.visitComposite(sc, x)
	case *ast.BinaryExpr:
		if x.Op == token.ADD && !a.sqlDone[x] && concatMayBeSQL(x) {
			// A query concatenated from parts: analyze the whole once.
			ast.Inspect(x, func(c ast.Node) bool {
				if c != nil {
					a.sqlDone[c] = true
				}
				return true
			})
			if v, conf := a.eval(sc, x); looksLikeSQL(v) {
				a.emitSQL(sc, x, v, worse(conf, Resolved))
			} else {
				// Not SQL as a whole: its literals are judged on their own.
				ast.Inspect(x, func(c ast.Node) bool {
					if lit, ok := c.(*ast.BasicLit); ok {
						delete(a.sqlDone, lit)
					}
					return true
				})
			}
		}
		// `msg.Topic == "orders"` / `!=` in a consumer: heuristic consume.
		if x.Op == token.EQL || x.Op == token.NEQ {
			for _, pair := range [][2]ast.Expr{{x.X, x.Y}, {x.Y, x.X}} {
				if sel, ok := pair[0].(*ast.SelectorExpr); ok && sel.Sel.Name == "Topic" {
					if v, conf := a.eval(sc, pair[1]); conf != Partial && v != "" {
						a.emit(sc, x.Pos(), Endpoint{Kind: TopicConsume, Topic: v, Confidence: Partial, Detail: "topic comparison"})
					}
				}
			}
		}
	case *ast.Field:
		// Struct tags: `env:"X"` (caarlos0/env), `envconfig:"X"`.
		if x.Tag != nil {
			tag, _ := strconv.Unquote(x.Tag.Value)
			st := reflect.StructTag(tag)
			for _, key := range []string{"env", "envconfig"} {
				if v, ok := st.Lookup(key); ok {
					name := strings.Split(v, ",")[0]
					if name != "" && name != "-" {
						a.emit(sc, x.Pos(), Endpoint{Kind: EnvRead, Env: name, Confidence: Exact, Detail: "struct tag " + key})
					}
				}
			}
		}
	}
}

func (a *goAnalyzer) visitCall(sc *scope, c *ast.CallExpr) {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	recv, _ := sel.X.(*ast.Ident)
	recvName := ""
	if recv != nil {
		recvName = recv.Name
	}
	pkg := ""
	if recv != nil {
		pkg = sc.gf.imports[recvName]
	}
	name := sel.Sel.Name
	if a.visitGRPC(sc, c, sel, pkg) || a.visitSQLCall(sc, c, sel, pkg) {
		return
	}
	switch {
	// Environment variables.
	case (pkg == "os" || pkg == "syscall") && (name == "Getenv" || name == "LookupEnv") && len(c.Args) == 1:
		if v, conf := a.eval(sc, c.Args[0]); conf != Partial {
			a.emit(sc, c.Pos(), Endpoint{Kind: EnvRead, Env: v, Confidence: conf, Detail: pkg + "." + name})
		}
		return
	// HTTP client: net/http constructors.
	case pkg == "net/http" && (name == "NewRequest" || name == "NewRequestWithContext"):
		off := 0
		if name == "NewRequestWithContext" {
			off = 1
		}
		if len(c.Args) >= off+2 {
			method := a.method(sc, c.Args[off])
			a.emitCall(sc, c, method, c.Args[off+1], "http."+name)
		}
		return
	case pkg == "net/http" && (name == "Get" || name == "Post" || name == "Head" || name == "PostForm"):
		if len(c.Args) >= 1 {
			a.emitCall(sc, c, strings.ToUpper(strings.TrimSuffix(name, "Form")), c.Args[0], "http."+name)
		}
		return
	// Message topics.
	case name == "ConsumePartition" && len(c.Args) >= 1:
		a.emitTopic(sc, c.Args[0], c.Pos(), TopicConsume, "ConsumePartition")
		return
	case name == "Consume" && len(c.Args) == 3:
		a.emitTopicList(sc, c.Args[1], c.Pos(), TopicConsume, "ConsumerGroup.Consume")
		return
	case name == "SubscribeTopics" && len(c.Args) >= 1:
		a.emitTopicList(sc, c.Args[0], c.Pos(), TopicConsume, "SubscribeTopics")
		return
	case (name == "ConsumeTopics" || name == "ConsumeRegex") && pkg != "":
		for _, arg := range c.Args {
			a.emitTopic(sc, arg, c.Pos(), TopicConsume, pkg+"."+name)
		}
		return
	}
	// Routes: method-named registrations with a path and a handler.
	method, isRoute := httpMethodNames[name]
	if !isRoute || len(c.Args) < 2 || pkg == "net/http" && name != "Handle" && name != "HandleFunc" {
		// http.Client-style calls: client.Get/Post/Head(url ...).
		if (name == "Get" || name == "Post" || name == "Head") && len(c.Args) >= 1 && pkg == "" {
			if v, _ := a.eval(sc, c.Args[0]); strings.Contains(v, "://") {
				a.emitCall(sc, c, strings.ToUpper(name), c.Args[0], "client."+name)
			}
		}
		return
	}
	pattern, conf := a.eval(sc, c.Args[0])
	if conf == Partial {
		// A dynamic base URL ("{}/v1/x") is a client call; routes are literal.
		if strings.HasPrefix(pattern, "{}/") && pkg == "" && (name == "Get" || name == "Post" || name == "Head") {
			a.emitCall(sc, c, method, c.Args[0], "client."+name)
		}
		return
	}
	if strings.Contains(pattern, "://") { // a client call, not a route
		a.emitCall(sc, c, method, c.Args[0], "client."+name)
		return
	}
	if name == "Handle" || name == "HandleFunc" {
		// Go 1.22: "[METHOD ][HOST]/[PATH]".
		if m, rest, ok := strings.Cut(pattern, " "); ok && isHTTPMethod(m) {
			method, pattern = m, strings.TrimSpace(rest)
		}
		if i := strings.IndexByte(pattern, '/'); i > 0 { // host-qualified pattern
			pattern = pattern[i:]
		}
	}
	if !strings.HasPrefix(pattern, "/") {
		return
	}
	full := joinPath(a.prefixOf(sc, sel.X), pattern)
	methods := sc.methods[c]
	if len(methods) == 0 {
		methods = []string{method}
	}
	for _, m := range methods {
		a.emit(sc, c.Pos(), Endpoint{Kind: HTTPRoute, Method: m, Path: NormalizePath(full), Symbol: exprString(c.Args[len(c.Args)-1]),
			Confidence: conf, Detail: "route " + name})
	}
}

func isHTTPMethod(s string) bool {
	switch s {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "CONNECT", "TRACE":
		return true
	}
	return false
}

func (a *goAnalyzer) method(sc *scope, e ast.Expr) string {
	if sel, ok := e.(*ast.SelectorExpr); ok {
		if m, ok := stdMethodConsts[sel.Sel.Name]; ok {
			return m
		}
	}
	if v, conf := a.eval(sc, e); conf != Partial && isHTTPMethod(strings.ToUpper(v)) {
		return strings.ToUpper(v)
	}
	return ""
}

func (a *goAnalyzer) emitCall(sc *scope, c *ast.CallExpr, method string, urlExpr ast.Expr, detail string) {
	v, conf := a.eval(sc, urlExpr)
	p := NormalizePath(v)
	if p == "" {
		a.diags = append(a.diags, Diagnostic{File: sc.gf.rel, Line: a.fset.Position(c.Pos()).Line, Message: "http call with unresolved URL: " + exprString(urlExpr)})
		return
	}
	if strings.Contains(v, "{}") {
		conf = Partial
	}
	a.emit(sc, c.Pos(), Endpoint{Kind: HTTPCall, Method: method, Path: p, Confidence: conf, Detail: detail})
}

func (a *goAnalyzer) emitTopic(sc *scope, e ast.Expr, pos token.Pos, k Kind, detail string) {
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
		e = u.X
	}
	v, conf := a.eval(sc, e)
	if conf == Partial || v == "" {
		a.diags = append(a.diags, Diagnostic{File: sc.gf.rel, Line: a.fset.Position(pos).Line, Message: "unresolved topic: " + exprString(e)})
		return
	}
	a.emit(sc, pos, Endpoint{Kind: k, Topic: v, Confidence: conf, Detail: detail})
}

func (a *goAnalyzer) emitTopicList(sc *scope, e ast.Expr, pos token.Pos, k Kind, detail string) {
	if id, ok := e.(*ast.Ident); ok {
		if bound, ok := sc.locals[id.Name]; ok {
			e = bound
		}
	}
	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		a.emitTopic(sc, e, pos, k, detail)
		return
	}
	for _, el := range cl.Elts {
		a.emitTopic(sc, el, pos, k, detail)
	}
}

// visitComposite handles message/config structs with a Topic field:
// sarama.ProducerMessage, kafka-go Message/Writer/ReaderConfig, franz-go
// kgo.Record, confluent TopicPartition, and lookalike wrappers.
func (a *goAnalyzer) visitComposite(sc *scope, cl *ast.CompositeLit) {
	tname := typeName(cl.Type)
	if tname == "" {
		return
	}
	for _, el := range cl.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "Topic":
			if k, ok := classifyTopicType(tname); ok {
				a.emitTopic(sc, kv.Value, kv.Pos(), k, tname)
			}
		case "GroupTopics", "Topics":
			if k, ok := classifyTopicType(tname); ok && k == TopicConsume {
				a.emitTopicList(sc, kv.Value, kv.Pos(), TopicConsume, tname)
			}
		}
	}
}

func typeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return exprString(t.X) + "." + t.Sel.Name
	case *ast.StarExpr:
		return typeName(t.X)
	}
	return ""
}

func classifyTopicType(t string) (Kind, bool) {
	base := t
	if i := strings.LastIndexByte(t, '.'); i >= 0 {
		base = t[i+1:]
	}
	switch {
	case strings.Contains(base, "Reader"), strings.Contains(base, "Consumer"), strings.Contains(base, "Subscription"):
		return TopicConsume, true
	case strings.Contains(base, "Message"), strings.Contains(base, "Writer"), strings.Contains(base, "Producer"),
		base == "Record", base == "TopicPartition", strings.Contains(base, "Publish"):
		return TopicProduce, true
	}
	return "", false
}

// eval resolves an expression to a string. Unresolvable parts become "{}"
// and the confidence is Partial.
func (a *goAnalyzer) eval(sc *scope, e ast.Expr) (string, Confidence) {
	a.budget = evalBudget{}
	v, c := a.evalDepth(sc, e, 0)
	if a.budget.exhausted {
		a.diags = append(a.diags, Diagnostic{File: a.fset.Position(e.Pos()).Filename, Line: a.fset.Position(e.Pos()).Line,
			Message: "constant evaluation budget exceeded; value left unresolved"})
		return "{}", Partial
	}
	return v, c
}

func (a *goAnalyzer) evalDepth(sc *scope, e ast.Expr, depth int) (string, Confidence) {
	if depth > 12 || e == nil || !a.budget.step() {
		return "{}", Partial
	}
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind == token.STRING {
			if v, err := strconv.Unquote(x.Value); err == nil {
				return v, Exact
			}
		}
		if x.Kind == token.INT {
			return x.Value, Exact
		}
	case *ast.ParenExpr:
		return a.evalDepth(sc, x.X, depth+1)
	case *ast.Ident:
		if b, ok := sc.locals[x.Name]; ok {
			v, c := a.evalDepth(sc, b, depth+1)
			return v, worse(c, Resolved)
		}
		if b, ok := a.consts[sc.gf.pkgPath+"."+x.Name]; ok {
			v, c := a.evalDepth(&scope{gf: sc.gf, locals: map[string]ast.Expr{}}, b, depth+1)
			return v, worse(c, Resolved)
		}
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			if p, ok := sc.gf.imports[id.Name]; ok {
				if b, ok := a.consts[p+"."+x.Sel.Name]; ok {
					target := a.fileForPkg(p)
					if target == nil {
						target = sc.gf
					}
					v, c := a.evalDepth(&scope{gf: target, locals: map[string]ast.Expr{}}, b, depth+1)
					return v, worse(c, Resolved)
				}
			}
		}
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			l, lc := a.evalDepth(sc, x.X, depth+1)
			r, rc := a.evalDepth(sc, x.Y, depth+1)
			return capConf(l+r, worse(worse(lc, rc), Resolved))
		}
	case *ast.CallExpr:
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok {
				switch sc.gf.imports[id.Name] + "." + sel.Sel.Name {
				case "fmt.Sprintf":
					return a.sprintf(sc, x.Args, depth)
				case "path.Join", "net/url.JoinPath":
					parts := make([]string, 0, len(x.Args))
					conf := Resolved
					for _, arg := range x.Args {
						v, c := a.evalDepth(sc, arg, depth+1)
						parts = append(parts, strings.Trim(v, "/"))
						conf = worse(conf, c)
					}
					joined := strings.Join(parts, "/")
					if s, _ := a.evalDepth(sc, x.Args[0], depth+1); strings.HasPrefix(s, "/") {
						joined = "/" + joined
					}
					return capConf(joined, conf)
				case "strings.ToUpper", "strings.ToLower", "strings.TrimSpace":
					if len(x.Args) == 1 {
						v, c := a.evalDepth(sc, x.Args[0], depth+1)
						switch sel.Sel.Name {
						case "ToUpper":
							v = strings.ToUpper(v)
						case "ToLower":
							v = strings.ToLower(v)
						default:
							v = strings.TrimSpace(v)
						}
						return v, worse(c, Resolved)
					}
				}
			}
		}
	}
	return "{}", Partial
}

func (a *goAnalyzer) fileForPkg(p string) *goFile {
	for _, f := range a.files {
		if f.pkgPath == p {
			return f
		}
	}
	return nil
}

// sprintf substitutes resolvable arguments into a format string; any verb
// with an unresolvable argument becomes "{}".
func (a *goAnalyzer) sprintf(sc *scope, args []ast.Expr, depth int) (string, Confidence) {
	if len(args) == 0 {
		return "{}", Partial
	}
	format, fc := a.evalDepth(sc, args[0], depth+1)
	if fc == Partial {
		return "{}", Partial
	}
	conf := Resolved
	var b strings.Builder
	argi := 1
	for i := 0; i < len(format); i++ {
		if format[i] != '%' || i+1 >= len(format) {
			b.WriteByte(format[i])
			continue
		}
		j := i + 1
		for j < len(format) && strings.IndexByte("+-# 0123456789.", format[j]) >= 0 {
			j++
		}
		if j >= len(format) {
			break
		}
		if format[j] == '%' {
			b.WriteByte('%')
			i = j
			continue
		}
		val, c := "{}", Partial
		if argi < len(args) {
			val, c = a.evalDepth(sc, args[argi], depth+1)
		}
		argi++
		if format[j] == 'q' && c != Partial {
			val = strconv.Quote(val)
		}
		b.WriteString(val)
		conf = worse(conf, c)
		i = j
		if b.Len() > maxEvalLen {
			break
		}
	}
	return capConf(b.String(), conf)
}

// capConf bounds an evaluated string; a cut value is Partial.
func capConf(s string, c Confidence) (string, Confidence) {
	if v, fits := capLen(s); !fits {
		return v, Partial
	}
	return s, c
}

func worse(a, b Confidence) Confidence {
	rank := map[Confidence]int{Exact: 0, Resolved: 1, Partial: 2}
	if rank[a] >= rank[b] {
		return a
	}
	return b
}

func exprString(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return exprString(x.X) + "." + x.Sel.Name
	case *ast.CallExpr:
		return exprString(x.Fun) + "(…)"
	case *ast.BasicLit:
		return x.Value
	case *ast.FuncLit:
		return "func literal"
	case *ast.StarExpr:
		return "*" + exprString(x.X)
	case *ast.UnaryExpr:
		return x.Op.String() + exprString(x.X)
	}
	return "expr"
}

var (
	grpcGatewayRE = regexp.MustCompile(`^Register(\w+)Handler(?:FromEndpoint|Server|Client)?$`)
	grpcServerRE  = regexp.MustCompile(`^Register(\w+)Server$`)
	connectRE     = regexp.MustCompile(`^New(\w+)Handler$`)
)

// visitGRPC recognizes gRPC servers and clients by the functions generated
// code exports: RegisterXServer (grpc-go), NewXHandler (connect-go),
// RegisterXHandler... (grpc-gateway, a client of the service), NewXClient,
// and calls through a client held in a variable or struct field.
func (a *goAnalyzer) visitGRPC(sc *scope, c *ast.CallExpr, sel *ast.SelectorExpr, pkg string) bool {
	if sc.gf.generated || sc.gf.testPath {
		return false
	}
	name := sel.Sel.Name
	ref := "go:" + pkg
	if pkg != "" {
		switch {
		case grpcGatewayRE.MatchString(name):
			svc := grpcGatewayRE.FindStringSubmatch(name)[1]
			a.emit(sc, c.Pos(), Endpoint{Kind: GRPCCall, Service: svc, Ref: ref, Confidence: Resolved, Detail: "grpc-gateway " + name})
			return true
		case grpcServerRE.MatchString(name) && len(c.Args) == 2:
			svc := grpcServerRE.FindStringSubmatch(name)[1]
			a.emit(sc, c.Pos(), Endpoint{Kind: GRPCServe, Service: svc, Ref: ref, Confidence: Resolved, Detail: name})
			return true
		case connectRE.MatchString(name) && strings.HasSuffix(pkg, "connect"):
			svc := connectRE.FindStringSubmatch(name)[1]
			a.emit(sc, c.Pos(), Endpoint{Kind: GRPCServe, Service: svc, Ref: ref, Confidence: Resolved, Detail: "connect " + name})
			return true
		}
		if gc, ok := a.grpcConstructor(sc.gf, c); ok {
			a.emit(sc, c.Pos(), Endpoint{Kind: GRPCCall, Service: gc.service, Ref: gc.ref, Confidence: Resolved, Detail: name})
			return true
		}
		return false
	}
	// client.Method(ctx, req) through a local or a field.
	var gc grpcClient
	found := false
	switch x := sel.X.(type) {
	case *ast.Ident:
		if b, ok := sc.locals[x.Name]; ok {
			gc, found = a.grpcConstructor(sc.gf, b)
		}
	case *ast.SelectorExpr:
		gc, found = sc.gf.grpcFields[x.Sel.Name]
	case *ast.CallExpr: // pb.NewXClient(conn).Method(ctx, req)
		gc, found = a.grpcConstructor(sc.gf, x)
	}
	if !found || len(c.Args) == 0 || !ast.IsExported(name) {
		return false
	}
	a.emit(sc, c.Pos(), Endpoint{Kind: GRPCCall, Service: gc.service, RPC: name, Ref: gc.ref, Confidence: Resolved, Detail: "client call"})
	return true
}

// sqlBuilderMethods take a table name as their first argument in the
// query builders and ORMs of goSQLLibs.
var sqlBuilderMethods = map[string]bool{"Table": true, "From": true, "Into": true, "Insert": true, "Update": true, "Delete": true,
	"ModelTableExpr": true, "TableExpr": true}

var sqlTableNameRE = regexp.MustCompile(`^[A-Za-z_][\w$]*(\.[A-Za-z_][\w$]*)?$`)

// visitSQLCall handles fmt.Sprintf formats that are SQL, and table
// arguments of query builders.
func (a *goAnalyzer) visitSQLCall(sc *scope, c *ast.CallExpr, sel *ast.SelectorExpr, pkg string) bool {
	if sc.gf.testPath {
		return false
	}
	if pkg == "fmt" && sel.Sel.Name == "Sprintf" && len(c.Args) > 0 {
		if f, ok := c.Args[0].(*ast.BasicLit); ok && f.Kind == token.STRING {
			if v, err := strconv.Unquote(f.Value); err == nil && looksLikeSQL(v) {
				a.sqlDone[f] = true
				full, conf := a.eval(sc, c)
				a.emitSQL(sc, c, full, worse(conf, Resolved))
			}
		}
		return false
	}
	if sc.gf.sqlLib == "" || !sqlBuilderMethods[sel.Sel.Name] || len(c.Args) == 0 {
		return false
	}
	lit, ok := c.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	t, err := strconv.Unquote(lit.Value)
	if err != nil {
		return false
	}
	t = strings.TrimSpace(strings.Fields(t + " ")[0]) // "payments AS p"
	if !sqlTableNameRE.MatchString(t) {
		return false
	}
	a.sqlDone[lit] = true
	a.emit(sc, c.Pos(), Endpoint{Kind: SQLAccess, Table: strings.ToLower(t), Confidence: Resolved, Detail: sc.gf.sqlLib + " " + sel.Sel.Name})
	return false
}

// emitSQL emits the tables of a SQL string found at n.
func (a *goAnalyzer) emitSQL(sc *scope, n ast.Node, v string, conf Confidence) {
	if sc.gf.testPath {
		return
	}
	line := a.fset.Position(n.Pos()).Line
	a.out = append(a.out, sqlEndpoints(a.repo, sc.gf.rel, line, v, conf, sc.symbol, "query")...)
}

// concatMayBeSQL is a cheap check that a + expression has a string part
// with a SQL keyword, before it is evaluated as a whole.
func concatMayBeSQL(x *ast.BinaryExpr) bool {
	found := false
	ast.Inspect(x, func(n ast.Node) bool {
		if found {
			return false
		}
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			u := strings.ToUpper(lit.Value)
			for _, kw := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "FROM", "INTO", "TABLE", "WITH", "MERGE"} {
				if strings.Contains(u, kw) {
					found = true
					return false
				}
			}
		}
		return true
	})
	return found
}
