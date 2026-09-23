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

// A Go file is written in gofmt form, so a format check cannot fail on it.
// The recorded attempt ended with every check green but gofmt.
func TestWrittenGoIsFormatted(t *testing.T) {
	unformatted := []byte("package inv\n\nfunc F() int {\n  return 1\n}\n")
	out, note := gofmtOnWrite("inv.go", unformatted)
	if string(out) != "package inv\n\nfunc F() int {\n\treturn 1\n}\n" {
		t.Errorf("not formatted:\n%s", out)
	}
	if !strings.Contains(note, "formatted with gofmt") {
		t.Errorf("the model was not told its text changed: %q", note)
	}
	// Already formatted: untouched, and nothing to say.
	if out2, note2 := gofmtOnWrite("inv.go", out); string(out2) != string(out) || note2 != "" {
		t.Errorf("a formatted file was changed or annotated: %q", note2)
	}
	// Not Go, or not parseable yet: written as given.
	if out3, _ := gofmtOnWrite("notes.md", unformatted); string(out3) != string(unformatted) {
		t.Error("a non-Go file was formatted")
	}
	broken := []byte("package inv\n\nfunc F( {\n")
	out4, note4 := gofmtOnWrite("inv.go", broken)
	if string(out4) != string(broken) || !strings.Contains(note4, "does not parse") {
		t.Errorf("an unparseable file was changed, or the model was not told: %q", note4)
	}
}
