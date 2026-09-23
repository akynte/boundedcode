package workflow_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/graph"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/workflow"
)

func plan() workflow.Plan {
	return workflow.Plan{
		RootCause:      "the account limit check reads committed totals alone",
		Files:          workflow.Targets{workflow.Target{Path: "checks/order_check.go"}},
		Symbols:        []string{"accountLimitExceeded"},
		Tests:          []string{"TestOrderCheck_Check"},
		WriteAllowlist: []string{"checks/order_check.go"},
	}
}

// Hand-editing generated output is a change the next generator run discards,
// so the plan has to name the generator instead of the file.
func TestPlanCannotGrantWritesToGeneratedFiles(t *testing.T) {
	for _, file := range []string{"api/events.pb.go", "risk/mock_gen.go", "dist/bundle.js", "zz_generated_deepcopy.go"} {
		p := plan()
		p.Files = append(p.Files, workflow.Target{Path: file})
		p.WriteAllowlist = append(p.WriteAllowlist, file)
		err := p.Validate(nil)
		if err == nil {
			t.Fatalf("%s was granted as a plain write", file)
		}
		if !strings.Contains(err.Error(), "regenerate") {
			t.Fatalf("the refusal does not say what to do instead: %v", err)
		}
	}
}

func TestPlanWithoutGeneratedFilesStillValidates(t *testing.T) {
	if err := plan().Validate(nil); err != nil {
		t.Fatal(err)
	}
}

// A model selects a generator; it never supplies one. The presets were frozen
// during INTAKE from the operator's own configuration.
func TestRegenerationResolvesOnlyFrozenGeneratePresets(t *testing.T) {
	presets := []recipe.Preset{
		{Name: "proto", Kind: recipe.KindGenerate, Argv: []string{"buf", "generate"}, TimeoutSeconds: 120},
		{Name: "go test", Kind: recipe.KindTest, Argv: []string{"go", "test", "./..."}, TimeoutSeconds: 600},
	}

	p := plan()
	p.Regenerate = []string{"proto"}
	if err := p.ValidateRegeneration(presets); err != nil {
		t.Fatal(err)
	}
	if !p.RegeneratesGenerated() {
		t.Fatal("a plan that declared a generator does not report one")
	}

	p.Regenerate = []string{"buf generate --template custom.yaml"}
	if err := p.ValidateRegeneration(presets); err == nil {
		t.Fatal("a command the operator never froze was accepted as a generator")
	}

	// Selecting the test preset as a "generator" would run an arbitrary frozen
	// command under the exemption that makes its output in-scope.
	p.Regenerate = []string{"go test"}
	err := p.ValidateRegeneration(presets)
	if err == nil || !strings.Contains(err.Error(), "generate") {
		t.Fatalf("a non-generate preset was accepted: %v", err)
	}
}

func TestPlanWithoutRegenerationReportsNone(t *testing.T) {
	p := plan()
	if p.RegeneratesGenerated() {
		t.Fatal("a plan with no generators reports one")
	}
	if err := p.ValidateRegeneration(nil); err != nil {
		t.Fatal(err)
	}
}

// A count says a plan failed validation; it does not say why.
//
// conflict-status and restock-silence both failed with "N plan target(s) do
// not exist", and the names were not persisted anywhere — so the failures
// could not be diagnosed after the fact, only re-run. Whether the planner
// invented a path, named a symbol the graph lacks, or reached outside the
// operator scope are different causes with different fixes.
func TestRefusedTargetsAreRecordedForDiagnosis(t *testing.T) {
	var o workflow.PlanTargetOutcome
	o.Refuse("file:internal/api/nope.go")
	o.Refuse("symbol:ErrConflict")
	o.Refuse("file:internal/api/nope.go") // a repeat across correction rounds
	if len(o.Refused) != 2 {
		t.Fatalf("Refused = %v, want the two distinct targets", o.Refused)
	}
	if o.Refused[0] != "file:internal/api/nope.go" || o.Refused[1] != "symbol:ErrConflict" {
		t.Errorf("Refused = %v, want file then symbol in order", o.Refused)
	}
}

// The list is bounded: a pathological plan must not grow the workflow state
// without limit.
func TestRefusedTargetsAreBounded(t *testing.T) {
	var o workflow.PlanTargetOutcome
	for i := 0; i < workflow.MaxRefusedTargets*3; i++ {
		o.Refuse("file:f" + string(rune('a'+i%26)) + string(rune('0'+i/26)))
	}
	if len(o.Refused) > workflow.MaxRefusedTargets {
		t.Errorf("kept %d refused targets, want at most %d",
			len(o.Refused), workflow.MaxRefusedTargets)
	}
}

// An empty target is not a refusal and must not take a slot.
func TestAnEmptyRefusedTargetIsIgnored(t *testing.T) {
	var o workflow.PlanTargetOutcome
	o.Refuse("")
	if len(o.Refused) != 0 {
		t.Errorf("Refused = %v, want empty", o.Refused)
	}
}

// A write grant the plan left out of files is declared for it, once, and
// nothing else changes. The recorded failure refused a whole plan because
// its new regression test was granted but not also listed under files.
func TestDeclareWriteGrantsAddsOnlyWhatIsMissing(t *testing.T) {
	p := workflow.Plan{
		RootCause:      "a cancelled line is released on every pass",
		Tests:          []string{"go test ./internal/worker"},
		Files:          workflow.Targets{{Path: "internal/worker/reconcile.go", Reason: "the fix"}},
		WriteAllowlist: []string{"internal/worker/reconcile.go", "internal/worker/reconcile_test.go", "internal/worker/reconcile_test.go"},
	}
	added := p.DeclareWriteGrants()
	if len(added) != 1 || added[0] != "internal/worker/reconcile_test.go" {
		t.Fatalf("added = %v", added)
	}
	if got := p.Files.Paths(); len(got) != 2 || got[0] != "internal/worker/reconcile.go" || got[1] != "internal/worker/reconcile_test.go" {
		t.Fatalf("files = %v", got)
	}
	if p.Files[0].Reason != "the fix" {
		t.Error("an existing declaration was rewritten")
	}
	if err := p.Validate(nil); err != nil {
		t.Errorf("the plan is still refused: %v", err)
	}
	if again := p.DeclareWriteGrants(); len(again) != 0 {
		t.Errorf("a second pass added %v", again)
	}
}

// A scope refusal says what the scope is. The recorded correction named only
// the offending file, and the next plan named it again.
func TestAScopeRefusalStatesTheScope(t *testing.T) {
	p := workflow.Plan{
		RootCause: "r", Tests: []string{"go test ./internal/worker"},
		Files:          workflow.Targets{{Path: "internal/inventory/inventory.go"}},
		WriteAllowlist: []string{"internal/inventory/inventory.go"},
	}
	err := p.Validate([]string{"internal/worker"})
	var scope *workflow.ScopeError
	if !errors.As(err, &scope) {
		t.Fatalf("an out-of-scope plan returned %v, not a ScopeError", err)
	}
	msg := scope.Correction()
	if !strings.Contains(msg, "internal/worker") || !strings.Contains(msg, "internal/inventory/inventory.go") {
		t.Errorf("the correction does not state the file and the scope: %s", msg)
	}
}

// An obligation written with an annotation still names its consumer.
func TestAnAnnotatedObligationNamesItsConsumer(t *testing.T) {
	n := graph.Node{Name: "New", FQN: "example.com/wh/internal/inventory.New", Path: "internal/inventory/inventory.go"}
	for _, symbol := range []string{"New", "New (inventory)", "inventory.New"} {
		if !workflow.NamesConsumer(symbol, n) {
			t.Errorf("%q does not name %s", symbol, n.FQN)
		}
	}
	if workflow.NamesConsumer("Newer (inventory)", n) {
		t.Error("a different name matched after its annotation was removed")
	}
}
