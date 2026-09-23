package judgment_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The boundary this package is not allowed to cross.
//
// A judgment is advisory. §10.1 requires that candidates be "evidence-judged,
// not model-voted", and internal/task/candidates.go says the same in its own
// words: "Nothing here asks a model which candidate is better". A probability
// from an external service is not evidence, so the packages that decide
// whether work is acceptable, whether a path may be written, or what a person
// is asked to approve must not be able to reach one.
//
// Stating that in a comment is not enough — the whole point of a boundary is
// that it holds when somebody who has not read the comment adds an import. So
// it is a test, and it walks the transitive import graph rather than the
// import block, because a dependency two packages away is the same breach.
var forbidden = []string{
	// Acceptance. The completion contract and candidate ranking decide
	// whether work is done, on evidence alone.
	"github.com/akynte/boundedcode/internal/policy",
	// The write firewall and the protected-path policy are refusals, and a
	// refusal that a model can widen is one a model can narrow.
	"github.com/akynte/boundedcode/internal/firewall",
	// The human gate presents evidence. What it shows may be ordered by a
	// judgment, but the broker itself holds no opinion.
	"github.com/akynte/boundedcode/internal/broker",
	// The verification recipes are the evidence. Nothing may grade them.
	"github.com/akynte/boundedcode/internal/recipe",
}

func TestDecisionPackagesCannotReachJudgment(t *testing.T) {
	const judgmentPkg = "github.com/akynte/boundedcode/internal/judgment"
	for _, pkg := range forbidden {
		t.Run(pkg, func(t *testing.T) {
			out, err := exec.Command("go", "list", "-deps", pkg).Output()
			if err != nil {
				t.Fatalf("go list -deps %s: %v", pkg, err)
			}
			for _, line := range strings.Split(string(out), "\n") {
				if strings.TrimSpace(line) == judgmentPkg {
					t.Fatalf("%s reaches %s.\n\n"+
						"That package decides something — acceptance, a refusal, or what a "+
						"person is shown at the gate — and a judgment is advisory. If the "+
						"new dependency is genuinely one-way, move the judged value into a "+
						"field the caller fills in, rather than letting the deciding package "+
						"ask.", pkg, judgmentPkg)
				}
			}
		})
	}
}

// TestJudgmentDoesNotImportTheStore keeps the other direction honest.
//
// Recorder and AnswerCache are interfaces declared in this package, and the
// supervisor supplies the real ones. That is what keeps judgment usable from
// `bcode doctor` and from a test with no workspace open, and it is why §2.3's
// rule that only internal/store writes files is not this package's problem.
//
// Direct imports, not transitive ones: internal/firewall legitimately reaches
// the store, and this package legitimately reaches internal/firewall for the
// credential check on its way out. What must not happen is this package
// opening a database itself.
func TestJudgmentDoesNotImportTheStore(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}{{"\n"}}{{join .TestImports "\n"}}`,
		"github.com/akynte/boundedcode/internal/judgment").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		switch strings.TrimSpace(line) {
		case "github.com/akynte/boundedcode/internal/store",
			"github.com/akynte/boundedcode/internal/ledger",
			"github.com/akynte/boundedcode/internal/cache":
			t.Fatalf("internal/judgment imports %s directly; it declares those as "+
				"interfaces so the supervisor owns the wiring", strings.TrimSpace(line))
		}
	}
}

// Judge.Ask must not be called outside this package.
//
// It is the one entry point that skips everything the contract is made of:
// question validation, the empty-state check, the all-or-nothing answer
// normalisation, the fallback Note, and the journal entry for a refusal.
// Consult and AskAll exist so that a caller gets all of it by default.
//
// Go cannot express "exported within the module only", so the rule is a test.
// It is an AST walk rather than a grep because a grep over `.Ask(` matches
// broker.Ask in half a dozen files and would have to be taught to ignore
// them — a rule with exceptions nobody can see is a rule that decays.
func TestJudgeAskIsNotCalledOutsideThisPackage(t *testing.T) {
	root := moduleRoot(t)
	// consult.go is the one legitimate caller: it is what AskAll is.
	allowed := map[string]bool{
		filepath.Join("internal", "judgment", "consult.go"): true,
	}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", "sidecars":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			// A path outside the root cannot be attributed to this module,
			// so there is nothing for this rule to say about it.
			//nolint:nilerr // skipping is the outcome, not an error to propagate
			return nil
		}
		if allowed[rel] {
			return nil
		}

		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			// A file that does not parse is one the compiler will reject
			// long before this rule matters.
			//nolint:nilerr // the same: unparseable is skipped, not failed
			return nil
		}
		// Only files that can see a judgment.Judge are candidates. Inside
		// this package the type is unqualified; elsewhere it arrives through
		// the import.
		inPackage := strings.HasPrefix(rel, filepath.Join("internal", "judgment"))
		if !inPackage && !importsJudgment(file) {
			return nil
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Ask" {
				return true
			}
			// broker.Ask takes six arguments and is a different thing
			// entirely; Judge.Ask takes three.
			if len(call.Args) != 3 {
				return true
			}
			t.Errorf("%s:%d calls Ask directly. Use judgment.AskAll or judgment.Consult: "+
				"Ask skips question validation, the empty-state check, answer "+
				"normalisation, the fallback Note and the refusal journal entry.",
				rel, fset.Position(call.Pos()).Line)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func importsJudgment(file *ast.File) bool {
	for _, imp := range file.Imports {
		if strings.Contains(imp.Path.Value, "internal/judgment") {
			return true
		}
	}
	return false
}

// moduleRoot walks up to the directory holding go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for range 6 {
		if _, err := filepath.Glob(filepath.Join(dir, "go.mod")); err == nil {
			if matches, _ := filepath.Glob(filepath.Join(dir, "go.mod")); len(matches) == 1 {
				return dir
			}
		}
		dir = filepath.Dir(dir)
	}
	t.Skip("module root not found")
	return ""
}

// Acceptance must not be able to see a judgment, and the import test above
// cannot prove it: Accept lives in package task, which legitimately holds
// Runner.Judge. So the property is asserted directly on the function's
// signature — it takes evidence and nothing else — and on the absence of any
// judged field in the candidate ranking.
func TestAcceptanceTakesNoJudgedInput(t *testing.T) {
	root := moduleRoot(t)
	for _, check := range []struct {
		file    string
		forbids []string
	}{
		{filepath.Join("internal", "task", "candidates.go"),
			[]string{"Relevance", "Judged", "judgment.", "Rerank"}},
	} {
		body, err := readFile(filepath.Join(root, check.file))
		if err != nil {
			t.Fatalf("%s: %v", check.file, err)
		}
		for _, forbidden := range check.forbids {
			if strings.Contains(body, forbidden) {
				t.Errorf("%s mentions %q; candidate ranking is evidence-judged, not "+
					"model-voted", check.file, forbidden)
			}
		}
	}

	// Accept's parameters, read from the source rather than asserted in prose.
	fset := token.NewFileSet()
	path := filepath.Join(root, "internal", "task", "runner.go")
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != "Accept" {
			return true
		}
		found = true
		for _, param := range fn.Type.Params.List {
			if got := exprText(param.Type); strings.Contains(got, "judgment") ||
				strings.Contains(got, "Rerank") {
				t.Errorf("Accept takes %s; acceptance must rest on evidence alone", got)
			}
		}
		return true
	})
	if !found {
		t.Fatal("task.Accept was not found; this test no longer checks what it claims to")
	}
}

func exprText(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.StarExpr:
		return exprText(v.X)
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return exprText(v.X) + "." + v.Sel.Name
	case *ast.ArrayType:
		return "[]" + exprText(v.Elt)
	default:
		return ""
	}
}

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path) //nolint:gosec // a path this test constructed
	return string(b), err
}
