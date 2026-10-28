package verify

import (
	"context"
	"os/exec"
	"testing"

	"github.com/akynte/boundedcode/internal/sandbox"
)

// Proposed tests are judged by running them, on the change and on the
// base: never by what the proposer claims.
func TestCounterexamples(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil || testing.Short() {
		t.Skip("go")
	}
	ctx := context.Background()
	e := &Engine{Sandbox: sandbox.None{}, CacheDir: t.TempDir()}
	// The change "fixes" negatives but breaks the upper bound.
	broken := "package calc\n\nfunc Clamp(v, max int) int {\n\tif v < 0 {\n\t\treturn 0\n\t}\n\treturn v\n}\n"
	repo, base := commitRepo(t, map[string]string{"go.mod": "module example.com/calc\n\ngo 1.22\n", "calc.go": buggyGo, "calc_test.go": "package calc\n"})
	writeFiles(t, repo, map[string]string{"calc.go": broken})
	test := func(body string) string { return "package calc\n\nimport \"testing\"\n\n" + body }
	rep, err := e.Counterexamples(ctx, RepoTarget{Name: "c", Worktree: repo, Base: base, TaskID: "t", Source: repo}, map[string]string{
		"regress_test.go":   test("func TestHigh(t *testing.T) {\n\tif Clamp(9, 5) != 5 {\n\t\tt.Fatal(Clamp(9, 5))\n\t}\n}\n"),
		"wrong/x_test.go":   "package wrong\n\nimport \"testing\"\n\nfunc TestW(t *testing.T) { t.Fatal(\"expects something else\") }\n",
		"ok_test.go":        test("func TestLow(t *testing.T) {\n\tif Clamp(-1, 5) != 0 {\n\t\tt.Fatal()\n\t}\n}\n"),
		"broken_test.go":    test("func TestB(t *testing.T) { undefinedThing() }\n"),
		"../escape_test.go": test(""),
		"calc_test.go":      test(""),
		"helper.go":         "package calc\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range rep.Tests {
		got[c.File] = c.Outcome
	}
	for f, want := range map[string]string{"regress_test.go": CounterRegression, "wrong/x_test.go": CounterFailing, "ok_test.go": CounterNone,
		"broken_test.go": CounterInvalid, "../escape_test.go": CounterInvalid, "calc_test.go": CounterInvalid, "helper.go": CounterInvalid} {
		if got[f] != want {
			t.Errorf("%s: %q, want %q", f, got[f], want)
		}
	}
	if rep.Status != CounterFound {
		t.Fatalf("status %s: %s", rep.Status, rep.Reason)
	}
}
