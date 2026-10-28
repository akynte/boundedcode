package verify

import (
	"strings"
	"testing"
)

func gate(stages ...StageResult) Result { return Result{Stages: stages} }

func TestClassify(t *testing.T) {
	green := gate(StageResult{Name: "go-build", Status: "pass"}, StageResult{Name: "go-test", Status: "pass", Command: "go test ./..."},
		StageResult{Name: "test-integrity", Status: "pass"})
	shown := &Evidence{Verified: true, TestFiles: []string{"a_test.go"}, Reason: "1 test fails on the base"}
	notShown := &Evidence{TestFiles: []string{"a_test.go"}, Reason: "the changed tests also pass on the base"}
	strong := &MutationReport{Status: MutationEstablished, Reason: "kills 4 of 5"}
	weak := &MutationReport{Status: MutationWeak, Reason: "kills 1 of 5"}
	for name, c := range map[string]struct {
		in       LayerInputs
		label    string
		withheld string
		strength string
	}{
		"verified":            {LayerInputs{Full: green, EvidenceNeeded: true, Evidence: shown}, LabelTaskVerified, "", ""},
		"verified, mutation":  {LayerInputs{Full: green, EvidenceNeeded: true, Evidence: shown, Mutation: strong}, LabelTaskVerified, "", "mutation established"},
		"weak tests advisory": {LayerInputs{Full: green, EvidenceNeeded: true, Evidence: shown, Mutation: weak}, LabelTaskVerified, "", "mutation weak"},
		"weak tests required": {LayerInputs{Full: green, EvidenceNeeded: true, Evidence: shown, Mutation: weak, Require: Requirements{Mutation: true}},
			LabelTestsGreen, "mutation evidence is required and is weak", "mutation weak"},
		"strict, boundary survives": {LayerInputs{Full: green, EvidenceNeeded: true, Evidence: shown,
			Mutation: &MutationReport{Status: MutationEstablished, BoundarySurvived: 1}, Require: Requirements{Mutation: true, Boundaries: true}},
			LabelTestsGreen, "1 boundary mutant(s) survive", "mutation established, 1 boundary untested"},
		"mutation required, skipped": {LayerInputs{Full: green, EvidenceNeeded: true, Evidence: shown, Require: Requirements{Mutation: true}},
			LabelTestsGreen, "required and is skipped", ""},
		"no behavioural evidence": {LayerInputs{Full: green, EvidenceNeeded: true, Evidence: notShown, Mutation: strong}, LabelTestsGreen, "no test shows the change", "mutation established"},
		"tests only change":       {LayerInputs{Full: green, EvidenceNeeded: false}, LabelTaskVerified, "", ""},
		"no tests ran": {LayerInputs{Full: gate(StageResult{Name: "go-build", Status: "pass"}, StageResult{Name: "tests", Status: "skipped"}),
			EvidenceNeeded: false}, LabelTestsGreen, "no tests ran", ""},
		"integrity review": {LayerInputs{Full: Result{Stages: green.Stages, Integrity: []IntegrityFinding{{File: "conftest.py", Severity: IntegrityReview, Reason: "changes the test runner's configuration"}}},
			EvidenceNeeded: true, Evidence: shown}, LabelTestsGreen, "touches how its tests run", ""},
		"integrity block": {LayerInputs{Full: gate(StageResult{Name: "test-integrity", Status: "fail", Cause: CauseIntegrity}, StageResult{Name: "go-test", Status: "pass"}),
			EvidenceNeeded: true, Evidence: shown}, LabelFailing, "", ""},
		"failing tests": {LayerInputs{Full: gate(StageResult{Name: "go-test", Status: "fail", Cause: CauseCode})}, LabelFailing, "", ""},
		"missing tool":  {LayerInputs{Full: gate(StageResult{Name: "cargo-test", Status: "error", Cause: CauseMissingTool})}, LabelIncomplete, "", ""},
		"counterexample found, advisory": {LayerInputs{Full: green, EvidenceNeeded: true, Evidence: shown,
			Counter: &CounterexampleReport{Status: CounterFound, Tests: []CounterexampleTest{{File: "x_test.go", Outcome: CounterRegression}}}},
			LabelTaskVerified, "", "counterexample found"},
		"counterexamples required, found": {LayerInputs{Full: green, EvidenceNeeded: true, Evidence: shown, Require: Requirements{Counterexamples: true},
			Counter: &CounterexampleReport{Status: CounterFound}}, LabelTestsGreen, "counterexample checks are required and are found", "counterexample found"},
		"contracts broken": {LayerInputs{Full: green, EvidenceNeeded: true, Evidence: shown, Contracts: "untested"}, LabelTestsGreen, "contracts are not established", ""},
	} {
		t.Run(name, func(t *testing.T) {
			got := Classify(c.in)
			if got.Label != c.label {
				t.Errorf("label %s, want %s (%s)", got.Label, c.label, got.Summary())
			}
			if w := strings.Join(got.Withheld, "; "); c.withheld == "" && w != "" || !strings.Contains(w, c.withheld) {
				t.Errorf("withheld %q, want %q", w, c.withheld)
			}
			if s := strings.Join(got.Strength, ", "); s != c.strength {
				t.Errorf("strength %q, want %q", s, c.strength)
			}
			if len(got.Layers) != 9 {
				t.Errorf("%d layer rows", len(got.Layers))
			}
		})
	}
}
