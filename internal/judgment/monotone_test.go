package judgment_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The monotone-scrutiny rule, asserted against the source rather than
// described in it (design.md R1).
//
// A judgment may move execution toward more scrutiny, more evidence, more
// conservative routing, or an earlier bounded stop. It may never move it the
// other way, and "the other way" has a specific list. Each item on that list
// is a specific identifier this codebase actually uses for that widening, so
// a match is a real thing happening rather than a word appearing.
//
// What this proves and what it does not: it cannot prove a judgment failed to
// influence a widening three calls away. What it proves is that no widening
// is written inside the block a tier check opens — which is where such a
// change would be written if someone made it — and it fails loudly enough
// that a person adding one has to come here and argue for it.
var forbiddenNearJudgment = map[string]string{
	"StateAccepted":        "a judgment may not mark a task accepted",
	"broker.Approved":      "a judgment may not approve a gate",
	"WriteScope =":         "a judgment may not widen the write scope",
	"WriteAllowlist =":     "a judgment may not widen the write allowlist",
	"MaxTokens =":          "a judgment may not raise a token budget",
	"MaxAttempts =":        "a judgment may not raise an attempt budget",
	"MaxSteps =":           "a judgment may not raise a step budget",
	"Status = recipe.Pass": "a judgment may not turn a check into a pass",
}

// judgedLine matches the point at which a judged value is about to be acted
// on: a call site asking whether its site's authority permits an effect.
var judgedLine = regexp.MustCompile(`\.Tier\.Permits\(|SiteTier\(`)

func TestNoJudgedEffectWidensAnything(t *testing.T) {
	root := moduleRoot(t)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", "sidecars", "evals":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, readErr := os.ReadFile(path) //nolint:gosec // this module's own source
		if readErr != nil {
			return readErr
		}
		lines := strings.Split(string(body), "\n")
		for i, line := range lines {
			if !judgedLine.MatchString(line) {
				continue
			}
			end := i + 25
			if end > len(lines) {
				end = len(lines)
			}
			window := strings.Join(lines[i:end], "\n")
			for needle, why := range forbiddenNearJudgment {
				if strings.Contains(window, needle) {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("%s:%d gates %q on a judgment tier check.\n\n%s.\n"+
						"A judgment may only move execution toward more scrutiny, more "+
						"evidence, or an earlier stop — design.md R1.",
						rel, i+1, needle, why)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestTaintDoesNotChangeAVerdict reads recipe.Result's own source: Passed
// must not consult Tainted. A taint is a note for a person, and a note that
// silently failed a check would be a judgment grading evidence.
func TestTaintDoesNotChangeAVerdict(t *testing.T) {
	root := moduleRoot(t)
	path := filepath.Join(root, "internal", "recipe", "recipe.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var checked bool
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Passed" || fn.Recv == nil {
			return true
		}
		checked = true
		ast.Inspect(fn, func(inner ast.Node) bool {
			if id, ok := inner.(*ast.Ident); ok && (id.Name == "Tainted" || id.Name == "TaintReason") {
				t.Errorf("recipe.Result.Passed reads %s; a taint is advisory and must never "+
					"change a deterministic verdict", id.Name)
			}
			return true
		})
		return false
	})
	if !checked {
		t.Fatalf("recipe.Result.Passed not found; this test is asserting nothing")
	}
}

// TestTheCompletionContractIgnoresTaint is the same property at the level
// that actually decides acceptance.
func TestTheCompletionContractIgnoresTaint(t *testing.T) {
	root := moduleRoot(t)
	matches, _ := filepath.Glob(filepath.Join(root, "internal", "recipe", "*.go"))
	var found bool
	for _, m := range matches {
		if strings.HasSuffix(m, "_test.go") {
			continue
		}
		body, err := os.ReadFile(m) //nolint:gosec // this module's own source
		if err != nil {
			continue
		}
		src := string(body)
		start := strings.Index(src, "func CheckPresets(")
		if start < 0 {
			continue
		}
		found = true
		end := strings.Index(src[start:], "\nfunc ")
		if end < 0 {
			end = len(src) - start
		}
		if strings.Contains(src[start:start+end], "Tainted") {
			t.Errorf("%s: CheckPresets reads Tainted; the completion contract rests on "+
				"evidence alone", filepath.Base(m))
		}
	}
	if !found {
		t.Fatalf("CheckPresets not found in internal/recipe; this test is asserting nothing")
	}
}
