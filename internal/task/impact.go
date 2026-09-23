package task

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/graph"
	"github.com/akynte/boundedcode/internal/policy"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/worktree"
)

// Change impact decides which tests a candidate owes evidence from.
//
// The repository's test preset answers "does the suite pass". It does not
// answer whether anything in the suite exercises the code that changed, and it
// cannot tell a test that passed from one the change taught to skip. This
// does: it finds the functions and methods the patch touches, finds the tests
// that reach them, runs exactly those tests with per-test results, and judges
// what came back.
//
// Reach has two sources, because neither is enough alone:
//
//   - the code graph, which knows callers across packages but describes the
//     base commit, so it cannot see a test the candidate adds;
//   - the candidate's own test files in the changed package, scanned for tests
//     whose bodies name the changed declaration, which sees new tests but
//     only by name and only within the package.
//
// Both are approximations and the result says which one found each test.
//
// Only Go is supported; other languages are not examined, and nothing is
// claimed about them.

// graphReachDepth bounds how far back through callers a test may be and still
// count as exercising a change: a test, a helper, the changed function's
// caller, the changed function.
const graphReachDepth = 3

// maxImpactTestsPerDir bounds one targeted run. Past it the run is truncated
// and the result says so rather than silently checking a sample.
const maxImpactTestsPerDir = 100

// ImpactRecipe names the synthesized result the completion contract judges.
const ImpactRecipe = "impact: tests reaching the change"

type changedDecl struct {
	Name        string
	Recv        string
	Path        string
	Requirement *policy.TestRequirement
	Tests       []reachingTest
}

func (d changedDecl) label() string {
	if d.Recv != "" {
		return d.Path + " " + d.Recv + "." + d.Name
	}
	return d.Path + " " + d.Name
}

type reachingTest struct {
	Dir  string `json:"dir"`
	Name string `json:"name"`
	File string `json:"file"`
	Via  string `json:"via"` // "graph" or "same package"
}

func (t reachingTest) key() string { return t.Dir + "\x00" + t.Name }

// changedGoDecls lists the functions and methods whose lines the patch
// touches in the snapshot, including new ones.
func changedGoDecls(snap *worktree.Snapshot) []changedDecl {
	paths := make([]string, 0, len(snap.NewRanges))
	for p := range snap.NewRanges {
		if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)

	var out []changedDecl
	for _, rel := range paths {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(snap.Path, filepath.FromSlash(rel)), nil, parser.SkipObjectResolution)
		if err != nil {
			continue // a file that does not parse is the build check's to report
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			start, end := fset.Position(fn.Pos()).Line, fset.Position(fn.End()).Line
			touched := false
			for _, r := range snap.NewRanges[rel] {
				touched = touched || r.Overlaps(start, end)
			}
			if !touched {
				continue
			}
			out = append(out, changedDecl{Name: fn.Name.Name, Recv: receiverTypeName(fn), Path: rel})
		}
	}
	return out
}

func receiverTypeName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	t := fn.Recv.List[0].Type
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
		case *ast.IndexExpr:
			t = x.X
		case *ast.IndexListExpr:
			t = x.X
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}

// graphTests finds tests anywhere in the repository that reach the
// declaration, through the base commit's graph.
func graphTests(ctx context.Context, g graph.Graph, d changedDecl) []reachingTest {
	if g == nil {
		return nil
	}
	nodes, err := g.NodesInFile(ctx, d.Path, 2000)
	if err != nil {
		return nil
	}
	var out []reachingTest
	for _, n := range nodes {
		if n.Name != d.Name || (n.Kind != graph.KindFunction && n.Kind != graph.KindMethod) {
			continue
		}
		if d.Recv != "" && !strings.HasSuffix(n.FQN, "."+d.Recv+"."+d.Name) {
			continue
		}
		reached, err := g.Traverse(ctx, graph.Query{
			Start: []int64{n.ID}, Dir: graph.Reverse,
			Kinds:    []graph.EdgeKind{graph.EdgeCalls, graph.EdgeTests},
			MaxDepth: graphReachDepth,
		})
		if err != nil {
			continue
		}
		for _, r := range reached {
			t := r.Node
			if t.Kind == graph.KindTest && strings.HasPrefix(t.Name, "Test") && strings.HasSuffix(t.Path, "_test.go") {
				out = append(out, reachingTest{Dir: path.Dir(t.Path), Name: t.Name, File: t.Path, Via: "graph"})
			}
		}
	}
	return out
}

// packageTests maps each test in a directory of the snapshot to the names its
// body mentions: identifiers and selected names, which is how a test in the
// same package reaches a function, a method or a new declaration the graph
// has never seen.
func packageTests(snapPath, dir string) map[reachingTest]map[string]bool {
	out := map[reachingTest]map[string]bool{}
	entries, err := os.ReadDir(filepath.Join(snapPath, filepath.FromSlash(dir)))
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		rel := path.Join(dir, e.Name())
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(snapPath, filepath.FromSlash(rel)), nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Test") {
				continue
			}
			names := map[string]bool{}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.Ident:
					names[x.Name] = true
				case *ast.SelectorExpr:
					names[x.Sel.Name] = true
				}
				return true
			})
			out[reachingTest{Dir: dir, Name: fn.Name.Name, File: rel, Via: "same package"}] = names
		}
	}
	return out
}

// impactReport is stored as the synthesized result's artifact, for a person.
type impactReport struct {
	Declarations []impactDecl      `json:"declarations"`
	Runs         []impactRun       `json:"runs"`
	Statuses     map[string]string `json:"statuses"`
	Problems     []string          `json:"problems,omitempty"`
	Notes        []string          `json:"notes,omitempty"`
}

type impactDecl struct {
	Declaration string         `json:"declaration"`
	Tests       []reachingTest `json:"tests"`
	Requirement string         `json:"requirement,omitempty"`
}

type impactRun struct {
	Dir          string `json:"dir"`
	Tests        int    `json:"tests"`
	Truncated    bool   `json:"truncated,omitempty"`
	Status       string `json:"status"`
	ArtifactHash string `json:"artifact_hash,omitempty"`
}

// runImpact derives, runs and judges the tests that reach the change. It
// returns nothing when the change touches no Go function or method.
func (r *Runner) runImpact(ctx context.Context, runner *recipe.Runner, snap *worktree.Snapshot,
	candidate string) []recipe.Result {

	decls := changedGoDecls(snap)
	if len(decls) == 0 {
		return nil
	}
	var g graph.Graph
	if r.Retriever != nil {
		g = r.Retriever.Graph()
	}

	scanned := map[string]map[reachingTest]map[string]bool{}
	byDir := map[string]map[string]reachingTest{}
	for i := range decls {
		d := &decls[i]
		if req, ok := r.Policies.RequiresTests(d.Path); ok {
			d.Requirement = &req
		}
		seen := map[string]bool{}
		add := func(t reachingTest) {
			if seen[t.key()] {
				return
			}
			seen[t.key()] = true
			d.Tests = append(d.Tests, t)
			if byDir[t.Dir] == nil {
				byDir[t.Dir] = map[string]reachingTest{}
			}
			byDir[t.Dir][t.Name] = t
		}
		for _, t := range graphTests(ctx, g, *d) {
			add(t)
		}
		dir := path.Dir(d.Path)
		if scanned[dir] == nil {
			scanned[dir] = packageTests(snap.Path, dir)
		}
		for t, names := range scanned[dir] {
			if names[d.Name] {
				add(t)
			}
		}
		sort.Slice(d.Tests, func(a, b int) bool { return d.Tests[a].key() < d.Tests[b].key() })
	}

	report := impactReport{Statuses: map[string]string{}}
	statuses := map[string]recipe.Status{}
	dirs := make([]string, 0, len(byDir))
	for dir := range byDir {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		names := make([]string, 0, len(byDir[dir]))
		for name := range byDir[dir] {
			names = append(names, name)
		}
		sort.Strings(names)
		run := impactRun{Dir: dir}
		if len(names) > maxImpactTestsPerDir {
			names, run.Truncated = names[:maxImpactTestsPerDir], true
		}
		run.Tests = len(names)
		res := runner.Run(ctx, recipe.Recipe{
			Name: "impact tests: ./" + dir, Kind: recipe.KindImpact,
			Argv: []string{"go", "test", "-count=1", "-json",
				"-run", "^(" + strings.Join(names, "|") + ")$", "./" + dir},
			Summarize: recipe.GoTestJSON,
		}, snap.Path, candidate)
		run.Status, run.ArtifactHash = string(res.Status), res.ArtifactHash
		report.Runs = append(report.Runs, run)
		for _, name := range names {
			key := dir + "\x00" + name
			for tk, st := range res.Summary.Tests {
				if strings.HasSuffix(tk, "/"+name) {
					statuses[key] = st
				}
			}
			if st, ok := statuses[key]; ok {
				report.Statuses[dir+"/"+name] = string(st)
			}
		}
	}

	changedFiles := map[string]bool{}
	for _, c := range snap.Changed {
		changedFiles[c] = true
	}
	var problems, notes []string
	covered := 0
	for _, d := range decls {
		entry := impactDecl{Declaration: d.label(), Tests: d.Tests}
		passed := false
		for _, t := range d.Tests {
			st, ran := statuses[t.key()]
			switch {
			case st == recipe.Pass:
				passed = true
			case !ran && changedFiles[t.File]:
				problems = append(problems, fmt.Sprintf("%s reached %s and did not run: this change removed or renamed it",
					t.Name, d.label()))
			case !ran:
				notes = append(notes, fmt.Sprintf("%s (%s) did not run; the index may be stale", t.Name, t.Via))
			case st == recipe.Skipped && changedFiles[t.File]:
				problems = append(problems, fmt.Sprintf("%s reaches %s and now skips; its file was changed by this task",
					t.Name, d.label()))
			case st == recipe.Skipped:
				notes = append(notes, fmt.Sprintf("%s reaches %s and skipped", t.Name, d.label()))
			case st == recipe.Fail:
				notes = append(notes, fmt.Sprintf("%s reaches %s and failed", t.Name, d.label()))
			}
		}
		if passed {
			covered++
		}
		if d.Requirement != nil {
			entry.Requirement = d.Requirement.Policy + ": " + d.Requirement.Rule
			if !passed {
				problems = append(problems, fmt.Sprintf("%s changed, and policy %s requires a passing test that "+
					"reaches it; none did. %s", d.label(), d.Requirement.Policy, strings.TrimSpace(d.Requirement.Reason)))
			}
		} else if len(d.Tests) == 0 {
			notes = append(notes, d.label()+" is not reached by any test")
		}
		report.Declarations = append(report.Declarations, entry)
	}
	report.Problems, report.Notes = problems, notes

	res := recipe.Result{
		Recipe: ImpactRecipe, Kind: recipe.KindImpact, Status: recipe.Pass, Candidate: candidate,
		Summary: recipe.Summary{Headline: fmt.Sprintf(
			"%d changed declaration(s); %d reached by a passing test, %d not", len(decls), covered, len(decls)-covered)},
	}
	if len(problems) > 0 {
		res.Status = recipe.Fail
		res.Summary.Headline = fmt.Sprintf("%d problem(s) with the tests that reach this change: %s",
			len(problems), problems[0])
	}
	for _, msg := range append(append([]string(nil), problems...), notes...) {
		if len(res.Summary.Findings) == recipe.MaxFindings {
			res.Summary.Truncated = true
			break
		}
		res.Summary.Findings = append(res.Summary.Findings, recipe.Finding{Message: msg})
	}
	if body, err := json.MarshalIndent(report, "", "  "); err == nil && r.Artifacts != nil {
		if hash, err := r.Artifacts.Put(body); err == nil {
			res.ArtifactHash = hash
		}
	}
	return []recipe.Result{res}
}

// CheckImpact is the completion contract's rule for change impact: the
// synthesized impact result must not have failed on this candidate. A test
// that fails while reaching the change is not judged here — the test preset
// already judges it, with the baseline allowance — but a test the change
// taught to skip, a test it removed, and a policy's demand for test evidence
// are.
func CheckImpact(results []recipe.Result, candidate string) (bool, []string) {
	for _, res := range results {
		if res.Kind != recipe.KindImpact || res.Recipe != ImpactRecipe || res.Status != recipe.Fail {
			continue
		}
		if candidate != "" && res.Candidate != "" && res.Candidate != candidate {
			continue
		}
		reasons := []string{"impact: " + res.Summary.Headline}
		for i, f := range res.Summary.Findings {
			if i == 0 {
				continue // already the headline
			}
			if i > 3 {
				break
			}
			reasons = append(reasons, "impact: "+f.Message)
		}
		return false, reasons
	}
	return true, nil
}
