package lab

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/config"
)

// diff builds a one-hunk git diff of file at line, adding lines.
func diff(file string, line int, created bool, added ...string) string {
	var b strings.Builder
	b.WriteString("diff --git a/" + file + " b/" + file + "\n")
	if created {
		b.WriteString("new file mode 100644\n--- /dev/null\n")
	} else {
		b.WriteString("--- a/" + file + "\n")
	}
	b.WriteString("+++ b/" + file + "\n")
	if created {
		b.WriteString("@@ -0,0 +1," + itoa(len(added)) + " @@\n")
	} else {
		b.WriteString("@@ -" + itoa(line) + ",3 +" + itoa(line) + "," + itoa(3+len(added)) + " @@\n context\n")
	}
	for _, a := range added {
		b.WriteString("+" + a + "\n")
	}
	if !created {
		b.WriteString(" context\n context\n")
	}
	return b.String()
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestCheckPatch(t *testing.T) {
	const repo = "../.." // the real source: protected functions at their real lines
	ok := func(name, patch string) {
		t.Helper()
		if _, err := CheckPatch([]byte(patch), repo, EditBudget{}, []string{"fix-balance-query"}); err != nil {
			t.Errorf("%s: refused: %v", name, err)
		}
	}
	refused := func(name, patch, why string) {
		t.Helper()
		_, err := CheckPatch([]byte(patch), repo, EditBudget{}, []string{"fix-balance-query"})
		if err == nil || !strings.Contains(err.Error(), why) {
			t.Errorf("%s: %v, want a refusal saying %q", name, err, why)
		}
	}
	ok("observation code", diff("internal/direct/observe.go", 61, false, "\t// note"))
	ok("a new test", diff("internal/direct/loopguard_test.go", 0, true, "package direct"))
	ok("retrieval", diff("internal/retrieve/rank.go", 1, false, "// x"))
	refused("the guard", diff("internal/direct/guard.go", 10, false, "// x"), "security or verification code")
	refused("the sandbox", diff("internal/execbox/execbox.go", 10, false, "// x"), "not under")
	refused("the graders", diff("internal/benchmark/grade.go", 10, false, "// x"), "not under")
	refused("the tasks", diff("benchmarks/direct/fix-balance-query.yaml", 1, false, "x"), "not under")
	refused("the laboratory", diff("internal/lab/decide.go", 10, false, "// x"), "not under")
	refused("configuration defaults", diff("internal/config/config.go", 10, false, "// x"), "not under")
	refused("the module", diff("go.mod", 1, false, "require x v1"), "not under")
	refused("policy inside a mixed file", diff("internal/direct/tools.go", 240, false, "\treturn c"), "protected function")
	refused("an existing test", diff("internal/direct/tools_more_test.go", 10, false, "// x"), "existing test")
	refused("tuned to a task", diff("internal/direct/observe.go", 61, false, `	if task == "fix-balance-query" {`), "names \"fix-balance-query\"")
	refused("binary", "diff --git a/x b/x\nGIT binary patch\n", "binary")
	big := diff("internal/direct/observe.go", 61, false, strings.Split(strings.Repeat("// x\n", 450), "\n")...)
	refused("over the edit budget", big, "edit budget")
	if pathRefusal("../outside.go") == "" || pathRefusal("/etc/passwd") == "" {
		t.Error("paths outside the repository allowed")
	}
}

// The fingerprint ignores every experiment setting and nothing else: two
// arms differing only in tunables match, and any other difference (here
// approval and the sandbox) does not.
func TestFingerprint(t *testing.T) {
	base := config.Defaults()
	fp := Fingerprint(base)
	// Set every field of ExperimentSettings to a non-zero value.
	var s config.ExperimentSettings
	fill(reflect.ValueOf(&s).Elem())
	if s.Direct.Retrieval == "" || s.Budgets.MaxAttempts == 0 {
		t.Fatal("fill did not set the settings")
	}
	c := config.Defaults()
	s.ApplyTo(&c)
	if Fingerprint(c) != fp {
		t.Fatal("an experiment setting changed the fingerprint: Fingerprint and ExperimentSettings are out of step")
	}
	c = config.Defaults()
	c.Direct.Approval = "auto"
	if Fingerprint(c) == fp {
		t.Fatal("approval is not in the fingerprint")
	}
	c = config.Defaults()
	c.Direct.Sandbox.Mode = "off"
	if Fingerprint(c) == fp {
		t.Fatal("the sandbox is not in the fingerprint")
	}
}

func fill(v reflect.Value) {
	switch v.Kind() {
	case reflect.Struct:
		for i := range v.NumField() {
			fill(v.Field(i))
		}
	case reflect.String:
		v.SetString("x")
	case reflect.Int:
		v.SetInt(7)
	case reflect.Pointer:
		if v.Type().Elem().Kind() == reflect.Bool {
			b := true
			v.Set(reflect.ValueOf(&b))
		}
	}
}

func TestExperimentValidate(t *testing.T) {
	good := func() *Experiment {
		e := experiment(Arm{Name: "cand", Role: "candidate", Set: config.ExperimentSettings{Direct: config.Tunables{Retrieval: "basic"}}})
		return e
	}
	if err := good().Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Experiment){
		"lowercase slug":       func(e *Experiment) { e.ID = "Bad ID" },
		"hypothesis":           func(e *Experiment) { e.Hypothesis = "" },
		"at least 2":           func(e *Experiment) { e.Trials = 1 },
		"server tuning":        func(e *Experiment) { e.Env = map[string]string{"ANTHROPIC_API_KEY": "x"} },
		"exactly one baseline": func(e *Experiment) { e.Arms = append(e.Arms, Arm{Name: "b2", Role: "baseline"}) },
		"changes nothing":      func(e *Experiment) { e.Arms[0].Set.Direct.Retrieval = "basic" },
		"edit budget": func(e *Experiment) {
			e.Arms[1].Set.Direct = config.Tunables{Retrieval: "basic", MaxTurns: 9, Routing: "adaptive"}
		},
		"not both": func(e *Experiment) { e.Arms[1].Patch = "x.patch" },
		"security tasks": func(e *Experiment) {
			e.Arms[1].Set = config.ExperimentSettings{}
			e.Arms[1].Patch = "x.patch"
			e.Tasks.Only = []string{"bug-fix"}
		},
		"bounded suite":       func(e *Experiment) { e.Arms[1].Set.Budgets.MaxAttempts = 3 },
		"no such arm":         func(e *Experiment) { e.Arms[1].Against = []string{"ghost"} },
		"heldout or dev":      func(e *Experiment) { e.DecideOn = "all" },
		"at least one candid": func(e *Experiment) { e.Arms = e.Arms[:1] },
	}
	for want, mutate := range cases {
		e := good()
		mutate(e)
		if err := e.Validate(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", want, err)
		}
	}
}
