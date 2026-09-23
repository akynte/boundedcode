package native

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/recipe"
)

// The recorded attempt was told only "inventory_test.go not gofmt'd", ran the
// check four times, and was stopped by the loop guard. The hint shows the line
// and what gofmt wants there.
func TestAFailedFormatCheckShowsTheLinesToChange(t *testing.T) {
	wt := t.TempDir()
	src := "package inv\n\nfunc F() int {\n  return 1\n}\n"
	if err := os.WriteFile(filepath.Join(wt, "inv_test.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	res := recipe.Result{Kind: recipe.KindFormat, Status: recipe.Fail,
		Summary: recipe.Summary{Findings: []recipe.Finding{{File: "inv_test.go", Message: "not gofmt'd"}}}}
	hint := formatHints(wt, res)
	for _, want := range []string{"inv_test.go", "line 4", `"  return 1"`, `"\treturn 1"`} {
		if !strings.Contains(hint, want) {
			t.Errorf("the hint does not contain %s:\n%s", want, hint)
		}
	}
	body, _ := os.ReadFile(filepath.Join(wt, "inv_test.go"))
	if string(body) != src {
		t.Error("the hint rewrote the file; it must only describe the change")
	}
	res.Status = recipe.Pass
	if formatHints(wt, res) != "" {
		t.Error("a passing check produced a hint")
	}
}
