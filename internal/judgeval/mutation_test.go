package judgeval

import (
	"encoding/json"
	"testing"

	"github.com/akynte/boundedcode/internal/workflow"
)

const sampleTestSource = `package example

import "testing"

func TestAdd(t *testing.T) {
	got := Add(2, 3)
	want := 5
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
}
`

func TestGenerateMutationCasesProducesValidCases(t *testing.T) {
	cases := GenerateMutationCases("example_test.go", sampleTestSource, "fix Add", "1", SplitDev)
	if len(cases) == 0 {
		t.Fatal("expected at least one mutation case")
	}
	for _, c := range cases {
		if err := c.Validate(); err != nil {
			t.Fatalf("generated case %s failed Validate: %v", c.CaseID, err)
		}
		if c.Source != SourceMutation {
			t.Errorf("case %s: Source = %q, want mutation", c.CaseID, c.Source)
		}
		if c.Site != workflow.IntegritySite {
			t.Errorf("case %s: Site = %q, want %q", c.CaseID, c.Site, workflow.IntegritySite)
		}
		var gt integrityGroundTruth
		if err := json.Unmarshal(c.GroundTruth, &gt); err != nil {
			t.Fatalf("case %s: bad ground_truth JSON: %v", c.CaseID, err)
		}
		if gt.Label != string(MutationLabelWeakening) {
			t.Errorf("case %s: label = %q, want weakening", c.CaseID, gt.Label)
		}
	}
}

func TestGenerateMutationCasesIsDeterministic(t *testing.T) {
	a := GenerateMutationCases("example_test.go", sampleTestSource, "fix Add", "1", SplitDev)
	b := GenerateMutationCases("example_test.go", sampleTestSource, "fix Add", "1", SplitDev)
	if len(a) != len(b) {
		t.Fatalf("got %d and %d cases from identical input", len(a), len(b))
	}
	for i := range a {
		if a[i].CaseID != b[i].CaseID {
			t.Fatalf("case %d: IDs differ across runs: %s vs %s", i, a[i].CaseID, b[i].CaseID)
		}
		if string(a[i].State) != string(b[i].State) {
			t.Fatalf("case %s: state differs across runs", a[i].CaseID)
		}
	}
}

func TestGeneratedMutationHunksParseBackWithSplitDiff(t *testing.T) {
	cases := GenerateMutationCases("example_test.go", sampleTestSource, "fix Add", "1", SplitDev)
	if len(cases) == 0 {
		t.Fatal("no cases generated")
	}
	var st integrityState
	if err := json.Unmarshal(cases[0].State, &st); err != nil {
		t.Fatal(err)
	}
	hunks := workflow.SplitDiff(st.Body)
	if len(hunks) != 1 {
		t.Fatalf("generated diff parsed into %d hunks, want 1: %s", len(hunks), st.Body)
	}
	if hunks[0].Path != "example_test.go" {
		t.Errorf("hunk path = %q, want example_test.go", hunks[0].Path)
	}
	if len(hunks[0].Removed) == 0 {
		t.Error("expected at least one removed line in the generated hunk")
	}
}

func TestMutationRuleRemoveAssertionDeletesTheLine(t *testing.T) {
	out, ok := RuleRemoveAssertion.apply(`	t.Fatalf("got %d, want %d", got, want)`)
	if !ok {
		t.Fatal("expected the rule to apply to a Fatalf line")
	}
	if out != "" {
		t.Errorf("got %q, want empty string (line deleted)", out)
	}
}

func TestMutationRuleWeakenEqualityReplacesOperator(t *testing.T) {
	out, ok := RuleWeakenEquality.apply(`	if got == want {`)
	if !ok {
		t.Fatal("expected the rule to apply")
	}
	if out != `	if got >= want {` {
		t.Errorf("got %q", out)
	}
}

func TestMutationRuleDoesNotApplyToUnrelatedLines(t *testing.T) {
	for _, rule := range AllMutationRules {
		if _, ok := rule.apply("package example"); ok {
			t.Errorf("rule %s matched an unrelated package declaration line", rule)
		}
	}
}
