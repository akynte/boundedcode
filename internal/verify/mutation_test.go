package verify

import (
	"context"
	"os/exec"
	"testing"

	"github.com/akynte/boundedcode/internal/sandbox"
)

// The tests of a change are assessed by mutating the changed lines and
// running the repository's own test runner: strong tests kill the mutants,
// weak ones let them survive; neither rejects the change.
func TestMutationAssessment(t *testing.T) {
	ctx := context.Background()
	e := &Engine{Sandbox: sandbox.None{}, CacheDir: t.TempDir()}
	type tc struct {
		tool         string
		base, change map[string]string
		want         string
	}
	goMod := "module example.com/calc\n\ngo 1.22\n"
	strongGo := oldTest + "\nfunc TestClampLow(t *testing.T) {\n\tfor v, want := range map[int]int{-1: 0, 0: 0, 1: 1, 5: 5, 6: 5} {\n\t\tif got := Clamp(v, 5); got != want {\n\t\t\tt.Fatalf(\"%d: %d\", v, got)\n\t\t}\n\t}\n}\n"
	weakGo := oldTest + "\nfunc TestClampLow(t *testing.T) {\n\tt.Log(Clamp(-1, 5))\n}\n"
	// A grading function: three thresholds, so enough mutants to conclude.
	pyBase := map[string]string{"pyproject.toml": "[project]\nname='c'\n", "calc.py": "def grade(s):\n    return 'F'\n",
		"tests/__init__.py": "", "tests/test_calc.py": "import unittest\nfrom calc import grade\n\nclass T(unittest.TestCase):\n    def test_f(self):\n        self.assertEqual(grade(10), 'F')\n"}
	pyFixed := "def grade(s):\n    if s >= 90:\n        return 'A'\n    if s >= 80:\n        return 'B'\n    if s >= 70:\n        return 'C'\n    return 'F'\n"
	pyStrong := pyBase["tests/test_calc.py"] + "\n    def test_all(self):\n        for s, g in {95: 'A', 90: 'A', 85: 'B', 80: 'B', 75: 'C', 70: 'C', 69: 'F'}.items():\n            self.assertEqual(grade(s), g)\n"
	// Weak: the tests call the code and check nothing about it.
	pyWeakBase := map[string]string{"pyproject.toml": pyBase["pyproject.toml"], "calc.py": pyBase["calc.py"], "tests/__init__.py": "",
		"tests/test_calc.py": "import unittest\nfrom calc import grade\n\nclass T(unittest.TestCase):\n    def test_f(self):\n        grade(10)\n"}
	pyWeak := pyWeakBase["tests/test_calc.py"] + "\n    def test_all(self):\n        self.assertIsNotNone(grade(95))\n"
	jsBase := map[string]string{"package.json": `{"name":"c","scripts":{"test":"node --test"}}`, "calc.js": "exports.grade = (s) => 'F';\n",
		"calc.test.js": "const test = require('node:test');\nconst assert = require('node:assert');\nconst { grade } = require('./calc');\ntest('f', () => assert.strictEqual(grade(10), 'F'));\n"}
	jsFixed := "exports.grade = (s) => {\n  if (s >= 90) return 'A';\n  if (s >= 80) return 'B';\n  if (s >= 70) return 'C';\n  return 'F';\n};\n"
	jsStrong := jsBase["calc.test.js"] + "test('all', () => { for (const [s, g] of [[95, 'A'], [90, 'A'], [85, 'B'], [80, 'B'], [75, 'C'], [70, 'C'], [69, 'F']]) assert.strictEqual(grade(s), g); });\n"
	jsWeakBase := map[string]string{"package.json": jsBase["package.json"], "calc.js": jsBase["calc.js"],
		"calc.test.js": "const test = require('node:test');\nconst assert = require('node:assert');\nconst { grade } = require('./calc');\ntest('f', () => { grade(10); });\n"}
	jsWeak := jsWeakBase["calc.test.js"] + "test('all', () => { assert.ok(grade(95)); });\n"
	for name, c := range map[string]tc{
		"go strong":     {"go", map[string]string{"go.mod": goMod, "calc.go": buggyGo, "calc_test.go": oldTest}, map[string]string{"calc.go": fixedGo, "calc_test.go": strongGo}, MutationEstablished},
		"go weak":       {"go", map[string]string{"go.mod": goMod, "calc.go": buggyGo, "calc_test.go": oldTest}, map[string]string{"calc.go": fixedGo, "calc_test.go": weakGo}, MutationWeak},
		"python strong": {"python3", pyBase, map[string]string{"calc.py": pyFixed, "tests/test_calc.py": pyStrong}, MutationEstablished},
		"python weak":   {"python3", pyWeakBase, map[string]string{"calc.py": pyFixed, "tests/test_calc.py": pyWeak}, MutationWeak},
		"js strong":     {"node", jsBase, map[string]string{"calc.js": jsFixed, "calc.test.js": jsStrong}, MutationEstablished},
		"js weak":       {"node", jsWeakBase, map[string]string{"calc.js": jsFixed, "calc.test.js": jsWeak}, MutationWeak},
		"docs only":     {"go", map[string]string{"go.mod": goMod, "calc.go": buggyGo, "calc_test.go": oldTest, "README.md": "x\n"}, map[string]string{"README.md": "y\n"}, MutationNotRequired},
		"sql only":      {"go", map[string]string{"go.mod": goMod, "calc.go": buggyGo, "calc_test.go": oldTest}, map[string]string{"db/q.sql": "SELECT 1 WHERE a < b;\n"}, MutationUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := exec.LookPath(c.tool); err != nil || testing.Short() {
				t.Skip(c.tool)
			}
			repo, base := commitRepo(t, c.base)
			writeFiles(t, repo, c.change)
			rep, err := e.Mutation(ctx, RepoTarget{Name: "c", Worktree: repo, Base: base, TaskID: "t", Source: repo}, MutationOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if rep.Status != c.want {
				t.Errorf("status %s (%s), want %s", rep.Status, rep.Reason, c.want)
				for _, m := range rep.Mutants {
					t.Logf("  %s: %s %s", m.Mutant, m.Status, m.Detail)
				}
			}
			for _, m := range rep.Mutants {
				if m.Status == MutantInvalid {
					t.Logf("invalid (set aside): %s: %s", m.Mutant, m.Detail)
				}
			}
		})
	}
}
