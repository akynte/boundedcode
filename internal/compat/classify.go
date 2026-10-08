package compat

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/akynte/boundedcode/internal/verify"
	"github.com/akynte/boundedcode/internal/xservice"
)

// need is one side of a link that a check must exercise.
type need struct {
	role     string // client, server, consumer, route
	kind     string // go-cover | knockout
	repo     string // the repository whose checks run
	provider string // the repository composed with it (definitions or spec)
	stage    verify.Stage
	// go-cover: the files whose execution proves the side ran, with the
	// spans that must run (nil: any statement of the file), and a fallback
	// (the enclosing function) for a line no coverage block contains.
	spans    map[string][]span
	fallback map[string]span
	pkg      string // the generated Go package the side uses
	// knockout: the operation, and the stages (expanded commands) to run.
	specFile, method, opPath string
	runs                     []stageRun

	// outcome
	done      bool
	exercised bool
	exText    string
	broken    string // reason
	untested  string // reason
	checks    []Check
}

type stageRun struct {
	stage verify.Stage
	argv  []string
}

type pendingLink struct {
	res      LinkResult
	involved []string
	needs    []*need
}

func (p *pendingLink) done() bool {
	for _, n := range p.needs {
		if !n.done {
			return false
		}
	}
	return true
}

func (p *pendingLink) untested(reason string) {
	p.res.Result, p.res.Reason = Untested, reason
	p.needs = nil
}

// decide sets the result from the needs' outcomes.
func (p *pendingLink) decide() {
	for _, n := range p.needs {
		p.res.Checks = append(p.res.Checks, n.checks...)
	}
	for _, n := range p.needs {
		if n.broken != "" {
			p.res.Result, p.res.Reason = Broken, n.broken
			return
		}
	}
	for _, n := range p.needs {
		if n.untested != "" {
			p.res.Result, p.res.Reason = Untested, n.untested
			return
		}
	}
	var roles []string
	for _, n := range p.needs {
		if !n.exercised {
			p.res.Result, p.res.Gap = Untested, GapNotExercised
			p.res.Reason = fmt.Sprintf("the checks pass but never exercise the %s side: %s", n.role, n.exText)
			return
		}
		roles = append(roles, n.role+" ("+n.exText+")")
	}
	if len(p.res.Breaking) > 0 {
		p.res.Result, p.res.Gap = Untested, GapBreaking
		p.res.Reason = "the task's repositories pass against the candidate, but the definition change is breaking for code built from the base definition " +
			"(services already deployed, or consumers outside the task); nothing tests that compatibility"
		return
	}
	p.res.Result = Compatible
	p.res.Reason = "built against the candidate commits and exercised by passing checks: " + strings.Join(roles, "; ")
}

// classify decides whether a link is affected by the change, and either
// decides it statically (a removed definition still in use, an ambiguous or
// unsupported link) or plans the checks that must exercise it.
func (ev *evaluation) classify(id string, lb xservice.Link, inBase bool, lh xservice.Link, inHead bool, ambiguous map[string][]string) (*pendingLink, error) {
	l, change := lh, ChangeModified
	switch {
	case inBase && !inHead:
		l, change = lb, ChangeRemoved
	case !inBase:
		change = ChangeAdded
	}
	p := &pendingLink{res: LinkResult{ID: id, Kind: l.Kind, Contract: l.Contract, Change: change,
		From: side(l.From, ev.commitOf(l.From.Repo, inHead)), To: side(l.To, ev.commitOf(l.To.Repo, inHead))}}
	p.involved = []string{l.From.Repo, l.To.Repo}

	// The definition the link depends on, and how the change altered it.
	svc, rpc := contractService(l.Contract)
	var def xservice.ProtoChange
	defRepo, defFile := "", ""
	switch l.Kind {
	case "grpc_def", "proto":
		defRepo, defFile = l.To.Repo, l.To.File
	case "grpc":
		if d, ok := ev.serviceDefinition(svc); ok {
			defRepo, defFile = d.Repo, d.File
		}
	}
	if defFile != "" {
		if l.Kind == "proto" {
			def = xservice.CompareFile(ev.proto(defRepo, defFile, false), ev.proto(defRepo, defFile, true))
		} else {
			def = xservice.CompareRPC(ev.proto(defRepo, defFile, false), ev.proto(defRepo, defFile, true), svc, rpc)
		}
		p.res.Breaking, p.res.Notes = def.Breaking, def.Notes
		p.involved = append(p.involved, defRepo)
	}
	opChanged := false
	if l.Kind == "openapi" || l.Kind == "openapi_impl" {
		b, okB := ev.operation(l.To, false)
		h, okH := ev.operation(l.To, true)
		opChanged = okB != okH || b.Digest != h.Digest
	}

	if change == ChangeModified {
		affected := false
		switch l.Kind {
		case "grpc_def":
			affected = ev.codeChanged(l.From) || def.Changed
		case "proto":
			affected = def.Changed
		case "grpc":
			affected = ev.codeChanged(l.From) || ev.serverChanged(l.To, svc, rpc) || def.Changed
		case "openapi", "openapi_impl":
			affected = ev.codeChanged(l.From) || opChanged
		}
		if !affected {
			if ev.fileChanged(l.From) || ev.fileChanged(l.To) || defFile != "" && ev.repos[defRepo] != nil && ev.repos[defRepo].Changed[defFile] {
				p.res.Reason = "files of this link changed, but not the link: the change is outside the functions, RPCs, messages or operation it uses"
				return p, errUnaffected
			}
			return nil, nil
		}
	}
	if change == ChangeRemoved {
		if ev.movedTo(l) {
			return nil, nil // the same contract between the same repositories exists at head: that link is evaluated
		}
		ev.classifyRemoved(p, l)
		return p, nil
	}

	// Static reasons the link cannot be checked.
	if amb := ambiguous[l.From.Where()+"|"+string(l.From.Kind)]; len(amb) > 0 {
		p.untested(fmt.Sprintf("ambiguous link: the endpoint at %s:%d matches the services %s; the gate cannot tell which one it uses",
			l.From.File, l.From.Line, strings.Join(amb, ", ")))
		return p, nil
	}
	for _, e := range []xservice.Endpoint{l.From, l.To} {
		if ev.repos[e.Repo] == nil {
			p.untested(fmt.Sprintf("%s is not a task repository (its side comes from the workspace index); add it to the task to check this link", e.Repo))
			return p, nil
		}
	}

	switch l.Kind {
	case "proto":
		ev.planGo(p, l.From, "consumer", defRepo, defFile, def.Changed, svc, "")
	case "grpc_def":
		ev.planGo(p, l.From, roleOf(l.From), defRepo, defFile, def.Changed, svc, rpc)
	case "grpc":
		if defFile == "" {
			p.untested("no .proto definition of " + svc + " was found in the task repositories or the index")
			return p, nil
		}
		ev.planGo(p, l.From, "client", defRepo, defFile, def.Changed, svc, rpc)
		if p.res.Result == "" {
			ev.planGo(p, l.To, "server", defRepo, defFile, def.Changed, svc, rpc)
		}
	case "openapi", "openapi_impl":
		ev.planKnockout(p, l.From, l.To)
	}
	return p, nil
}

// classifyRemoved handles a link that existed at the base commits only.
func (ev *evaluation) classifyRemoved(p *pendingLink, l xservice.Link) {
	fromNow, fromOK := ev.atHead(l.From)
	_, toOK := ev.atHead(l.To)
	switch {
	case fromOK && toOK:
		// Both sides still exist (the link moved, or resolves differently):
		// the head link, if any, is evaluated on its own.
		p.untested("the link no longer resolves although both sides still exist; check how " + l.Contract + " is now reached")
	case fromOK:
		p.res.From = side(fromNow, ev.commitOf(fromNow.Repo, true))
		p.res.To.Commit = ev.commitOf(l.To.Repo, true)
		switch l.To.Kind {
		case xservice.GRPCDefine, xservice.OpenAPIOperation, xservice.ProtoDefine:
			p.res.Result = Broken
			p.res.Reason = fmt.Sprintf("the candidate %s@%s no longer defines %s, which %s still uses at %s:%d",
				l.To.Repo, short(p.res.To.Commit), l.Contract, fromNow.Repo, fromNow.File, fromNow.Line)
		default:
			p.untested(fmt.Sprintf("%s no longer registers a server for %s (or registers it in a form the analyzer does not recognize); %s still calls it at %s:%d",
				l.To.Repo, l.Contract, fromNow.Repo, fromNow.File, fromNow.Line))
		}
	default:
		p.untested(fmt.Sprintf("the change removed this use of %s from %s:%d; nothing shows the consumer no longer needs it", l.Contract, l.From.File, l.From.Line))
	}
}

// movedTo reports whether a head link has the same kind and contract
// between the same repositories, with sides of the same kinds (a file was
// renamed or code moved).
func (ev *evaluation) movedTo(l xservice.Link) bool {
	for _, h := range ev.headLinks {
		if h.Kind == l.Kind && h.Contract == l.Contract && h.From.Repo == l.From.Repo && h.To.Repo == l.To.Repo &&
			h.From.Kind == l.From.Kind && h.To.Kind == l.To.Kind {
			return true
		}
	}
	return false
}

// atHead finds an endpoint with the same repository, kind and contract key
// in the head version (in any file: code may move).
func (ev *evaluation) atHead(e xservice.Endpoint) (xservice.Endpoint, bool) {
	if ev.repos[e.Repo] == nil {
		return e, true // outside the task: unchanged
	}
	var best xservice.Endpoint
	found := false
	for _, h := range ev.headEps {
		if h.Repo == e.Repo && h.Kind == e.Kind && h.Key() == e.Key() {
			if !found || h.File == e.File {
				best, found = h, true
			}
		}
	}
	return best, found
}

func roleOf(e xservice.Endpoint) string {
	switch e.Kind {
	case xservice.GRPCServe:
		return "server"
	case xservice.GRPCCall:
		return "client"
	case xservice.HTTPRoute:
		return "route"
	}
	return "consumer"
}

func (ev *evaluation) serviceDefinition(svc string) (xservice.Endpoint, bool) {
	for _, eps := range [][]xservice.Endpoint{ev.headEps, ev.baseEps} {
		for _, e := range eps {
			if e.Kind == xservice.GRPCDefine && e.Service == svc && e.RPC == "" {
				return e, true
			}
		}
	}
	return xservice.Endpoint{}, false
}

// proto parses a task repository's .proto file at base or head (an empty
// schema when absent or outside the task).
func (ev *evaluation) proto(repo, file string, head bool) xservice.ProtoSchema {
	k := fmt.Sprint(repo, "|", file, "|", head)
	if s, ok := ev.protoCache[k]; ok {
		return s
	}
	st := ev.repos[repo]
	var s xservice.ProtoSchema
	if st != nil {
		dir := st.BaseDir
		if head {
			dir = st.HeadDir
		}
		b, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file)))
		s = xservice.ParseProtoSchema(string(b))
	} else {
		// Outside the task the definition does not change.
		s = xservice.ParseProtoSchema("")
		if !head {
			return ev.proto(repo, file, true)
		}
	}
	ev.protoCache[k] = s
	return s
}

func (ev *evaluation) operation(op xservice.Endpoint, head bool) (xservice.OpenAPIOp, bool) {
	st := ev.repos[op.Repo]
	if st == nil {
		return xservice.OpenAPIOp{}, true
	}
	dir := st.BaseDir
	if head {
		dir = st.HeadDir
	}
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(op.File)))
	if err != nil {
		return xservice.OpenAPIOp{}, false
	}
	for _, o := range xservice.OpenAPIOperations(b) {
		if o.Method == op.Method && o.Path == op.Path {
			return o, true
		}
	}
	return xservice.OpenAPIOp{}, false
}

func (ev *evaluation) fileChanged(e xservice.Endpoint) bool {
	st := ev.repos[e.Repo]
	return st != nil && st.Changed[e.File]
}

// codeChanged reports whether the change touches an endpoint's code: the
// enclosing function for Go (at base or head), the whole file otherwise.
func (ev *evaluation) codeChanged(e xservice.Endpoint) bool {
	st := ev.repos[e.Repo]
	if st == nil || !st.Changed[e.File] {
		return false
	}
	if !strings.HasSuffix(e.File, ".go") {
		return true
	}
	// The endpoint's line is from the head version: find its function there,
	// and the same function (by receiver and name) at the base.
	headFuncs, _ := goFuncs(filepath.Join(st.HeadDir, filepath.FromSlash(e.File)), e.File)
	if headFuncs == nil {
		return true // unparsable: assume affected
	}
	at := enclosing(headFuncs, e.Line)
	if st.Hunks.touches(e.File, true, []span{at}) {
		return true
	}
	baseFuncs, _ := goFuncs(filepath.Join(st.BaseDir, filepath.FromSlash(e.File)), e.File)
	for _, hf := range headFuncs {
		if hf.Span != at {
			continue
		}
		for _, bf := range baseFuncs {
			if bf.Name == hf.Name && bf.Recv == hf.Recv {
				return st.Hunks.touches(e.File, false, []span{bf.Span})
			}
		}
	}
	return false
}

// serverChanged reports whether the change touches a Go server's
// registration or the methods implementing the RPC (any RPC of the service
// for a service-level link).
func (ev *evaluation) serverChanged(e xservice.Endpoint, svc, rpc string) bool {
	if ev.codeChanged(e) {
		return true
	}
	st := ev.repos[e.Repo]
	if st == nil || !strings.HasSuffix(e.File, ".go") {
		return false
	}
	pkg := strings.TrimPrefix(e.Ref, "go:")
	for _, head := range []bool{false, true} {
		dir := st.BaseDir
		if head {
			dir = st.HeadDir
		}
		for _, m := range rpcImplementations(dir, pkg, ev.rpcNames(svc, rpc)) {
			if st.Hunks.touches(m.File, head, []span{m.Span}) {
				return true
			}
		}
	}
	return false
}

// rpcNames is the RPC, or every RPC of the service (base and head).
func (ev *evaluation) rpcNames(svc, rpc string) []string {
	if rpc != "" {
		return []string{rpc}
	}
	d, ok := ev.serviceDefinition(svc)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, head := range []bool{false, true} {
		for n := range ev.proto(d.Repo, d.File, head).Services[svc].RPCs {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	slices.Sort(out)
	return out
}

// goTestStage is the repository's `go test` stage from its verification
// config at the base commit.
func (ev *evaluation) testStages(repo string) ([]verify.Stage, error) {
	if s, ok := ev.stages[repo]; ok || ev.stageErr[repo] != nil {
		return s, ev.stageErr[repo]
	}
	st := ev.repos[repo]
	s, err := verify.TestStages(ev.ctx, st.Worktree, st.Base)
	ev.stages[repo], ev.stageErr[repo] = s, err
	return s, err
}

// planGo plans a Go side: the repository's own `go test` stage, run in a
// Go workspace that resolves the generated package's module to its
// candidate tree, with coverage proving the side's code ran.
func (ev *evaluation) planGo(p *pendingLink, e xservice.Endpoint, role, defRepo, defFile string, defChanged bool, svc, rpc string) {
	st := ev.repos[e.Repo]
	if !strings.HasSuffix(e.File, ".go") {
		p.untested(fmt.Sprintf("the %s side %s/%s is %s code: the gate checks Go sides only (other languages are reported untested)", role, e.Repo, e.File, langOf(e.File)))
		return
	}
	if st.Module == "" {
		p.untested(fmt.Sprintf("%s has no go.mod at its root (nested modules are not supported)", e.Repo))
		return
	}
	stages, err := ev.testStages(e.Repo)
	if err != nil {
		p.untested(fmt.Sprintf("%s's verification config could not be read at its base commit: %v", e.Repo, err))
		return
	}
	idx := slices.IndexFunc(stages, verify.IsGoTest)
	if idx < 0 {
		p.untested(fmt.Sprintf("%s's verification config (at its base commit) has no `go test` stage, so its checks cannot be measured for coverage", e.Repo))
		return
	}
	ref := e.Ref
	if !strings.HasPrefix(ref, "go:") {
		ref = ev.goRef(defRepo, defFile)
	}
	if ref == "" {
		p.untested(fmt.Sprintf("the Go package generated from %s/%s is unknown (no go_package option)", defRepo, defFile))
		return
	}
	pkg := strings.TrimPrefix(ref, "go:")
	prov, provMod := "", ""
	for _, name := range ev.order {
		m := ev.repos[name].Module
		if m != "" && (pkg == m || strings.HasPrefix(pkg, m+"/")) && len(m) > len(provMod) {
			prov, provMod = name, m
		}
	}
	switch {
	case prov == "":
		p.untested(fmt.Sprintf("missing dependency: the generated Go package %s is not provided by any task repository (add the repository holding it to the task)", pkg))
		return
	case prov == e.Repo && defRepo != e.Repo:
		p.untested(fmt.Sprintf("%s uses its own generated copy of %s; the gate does not regenerate code from %s's candidate %s", e.Repo, pkg, defRepo, defFile))
		return
	}
	if defChanged {
		dir := strings.TrimPrefix(strings.TrimPrefix(pkg, provMod), "/")
		regenerated := false
		for f := range ev.repos[prov].Changed {
			if path.Dir(f) == path.Clean("./"+dir) || dir == "" && path.Dir(f) == "." {
				regenerated = regenerated || strings.HasSuffix(f, ".go")
			}
		}
		if !regenerated {
			p.untested(fmt.Sprintf("%s/%s changed but the generated package %s in %s did not: regenerate it (the gate does not run protoc)", defRepo, defFile, pkg, prov))
			return
		}
	}
	n := &need{role: role, kind: "go-cover", repo: e.Repo, provider: prov, stage: stages[idx], pkg: pkg,
		spans: map[string][]span{}, fallback: map[string]span{}}
	switch e.Kind {
	case xservice.GRPCCall:
		n.spans[e.File] = []span{{e.Line, e.Line}}
		funcs, _ := goFuncs(filepath.Join(st.HeadDir, filepath.FromSlash(e.File)), e.File)
		n.fallback[e.File] = enclosing(funcs, e.Line)
	case xservice.GRPCServe:
		ms := rpcImplementations(st.HeadDir, pkg, ev.rpcNames(svc, rpc))
		if len(ms) == 0 {
			p.untested(fmt.Sprintf("no method implementing %s with a request type from %s was found in %s, so no check can be shown to run it", strings.TrimPrefix(p.res.Contract, "grpc "), pkg, e.Repo))
			return
		}
		for _, m := range ms {
			n.spans[m.File] = append(n.spans[m.File], m.Span)
		}
	default:
		n.spans[e.File] = nil
	}
	p.needs = append(p.needs, n)
	p.involved = append(p.involved, prov)
}

func (ev *evaluation) goRef(repo, file string) string {
	for _, eps := range [][]xservice.Endpoint{ev.headEps, ev.baseEps} {
		for _, d := range eps {
			if d.Kind == xservice.ProtoDefine && d.Repo == repo && d.File == file && strings.HasPrefix(d.Ref, "go:") {
				return d.Ref
			}
		}
	}
	return ""
}

// planKnockout plans an OpenAPI side: the dependent repository's test
// stages that run tests reading the specification file, run against the
// candidate spec and again with the operation removed. The side is
// exercised only if removing the operation makes them fail.
func (ev *evaluation) planKnockout(p *pendingLink, from, op xservice.Endpoint) {
	st := ev.repos[from.Repo]
	specName := path.Base(op.File)
	var goDirs []string
	other := false
	_ = filepath.WalkDir(st.HeadDir, func(fp string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // unreadable entries are skipped
		}
		if d.IsDir() {
			if fp != st.HeadDir && (d.Name() == "vendor" || d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(st.HeadDir, fp)
		rel = filepath.ToSlash(rel)
		if !d.Type().IsRegular() || !verify.IsTestFile(rel) || underTestdata(rel) {
			return nil
		}
		b, err := os.ReadFile(fp)
		if err != nil || !strings.Contains(string(b), specName) {
			return nil //nolint:nilerr // unreadable files are skipped
		}
		if strings.HasSuffix(rel, "_test.go") {
			if d := "./" + path.Dir(rel); !slices.Contains(goDirs, d) {
				goDirs = append(goDirs, strings.TrimSuffix(d, "/."))
			}
		} else {
			other = true
		}
		return nil
	})
	if len(goDirs) == 0 && !other {
		p.untested(fmt.Sprintf("no test in %s refers to %s, so no existing check reads the operation (a contract test that loads the specification would)", from.Repo, specName))
		p.res.Gap = GapNotExercised
		return
	}
	stages, err := ev.testStages(from.Repo)
	if err != nil {
		p.untested(fmt.Sprintf("%s's verification config could not be read at its base commit: %v", from.Repo, err))
		return
	}
	n := &need{role: roleOf(from), kind: "knockout", repo: from.Repo, provider: op.Repo, specFile: op.File, method: op.Method, opPath: op.Path}
	slices.Sort(goDirs)
	for _, s := range stages {
		goStage := verify.IsGoTest(s) || slices.Contains(s.Requires, "go.mod")
		if goStage && len(goDirs) == 0 || !goStage && !other {
			continue
		}
		var argv []string
		for _, a := range s.Run {
			switch {
			case a == "{packages}" && goStage:
				argv = append(argv, goDirs...)
			case a == "{packages}":
				argv = append(argv, "./...")
			default:
				argv = append(argv, a)
			}
		}
		n.runs = append(n.runs, stageRun{stage: s, argv: argv})
	}
	if len(n.runs) == 0 {
		p.untested(fmt.Sprintf("%s's test stages (at its base commit) do not run the tests that read %s", from.Repo, specName))
		return
	}
	p.needs = append(p.needs, n)
}

func underTestdata(p string) bool { return strings.Contains("/"+p, "/testdata/") }

func langOf(file string) string {
	switch path.Ext(file) {
	case ".py":
		return "Python"
	case ".java", ".kt", ".scala":
		return "JVM"
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		return "JavaScript/TypeScript"
	case ".cs":
		return "C#"
	case ".rs":
		return "Rust"
	case ".rb":
		return "Ruby"
	case ".php":
		return "PHP"
	}
	return "non-Go"
}
