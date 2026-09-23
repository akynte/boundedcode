package task

// A plan that names something the repository does not contain must be caught
// before anything downstream trusts it. These are the exact targets the
// planner invented during the pilot.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/graph"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/workflow"
)

func repoWith(t *testing.T, files ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range files {
		full := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestNonexistentFileIsRejectedWithRealAlternatives(t *testing.T) {
	root := repoWith(t, "testing/python/fixtures.py", "src/_pytest/compat.py", "src/_pytest/fixtures.py")
	p := PlanTargets{Root: root}
	// The path the planner actually invented, twice.
	bad, checked := p.Validate(context.Background(), workflow.Plan{
		Files: workflow.Targets{workflow.Target{Path: "nowhere/util.py"}}, WriteAllowlist: []string{"nowhere/util.py"},
	})
	if checked == 0 {
		t.Fatal("nothing was checked")
	}
	if len(bad) != 1 {
		t.Fatalf("expected one contradiction, got %d: %+v", len(bad), bad)
	}
	if bad[0].Kind != TargetFile || bad[0].Target != "nowhere/util.py" {
		t.Errorf("wrong contradiction: %+v", bad[0])
	}
	// Evidence must be real files, and the plan must not have been rewritten.
	if len(bad[0].Evidence) == 0 {
		t.Error("no repository evidence was offered")
	}
	for _, e := range bad[0].Evidence {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(e))); err != nil {
			t.Errorf("evidence %q is not a real file", e)
		}
	}
	msg := CorrectionFor(bad)
	if !strings.Contains(msg, "nowhere/util.py") || !strings.Contains(msg, "Closest repository evidence") {
		t.Errorf("the correction is not evidence-driven: %s", msg)
	}
	if strings.Contains(msg, "I have replaced") || strings.Contains(msg, "corrected to") {
		t.Error("the correction rewrote the plan instead of returning it")
	}
}

// A new file in a directory that exists is how a plan adds a module, and must
// not be refused.
func TestNewFileInAnExistingDirectoryIsAccepted(t *testing.T) {
	root := repoWith(t, "src/pkg/core.py")
	bad, _ := PlanTargets{Root: root}.Validate(context.Background(), workflow.Plan{
		Files: workflow.Targets{workflow.Target{Path: "src/pkg/new_module.py"}}, WriteAllowlist: []string{"src/pkg/new_module.py"},
	})
	if len(bad) != 0 {
		t.Errorf("a new file in a real package was refused: %+v", bad)
	}
}

func TestExactExistingTargetIsAccepted(t *testing.T) {
	root := repoWith(t, "src/_pytest/compat.py")
	bad, checked := PlanTargets{Root: root}.Validate(context.Background(), workflow.Plan{
		Files: workflow.Targets{workflow.Target{Path: "src/_pytest/compat.py"}}, WriteAllowlist: []string{"src/_pytest/compat.py"},
	})
	if len(bad) != 0 {
		t.Errorf("a real file was refused: %+v", bad)
	}
	if checked != 2 {
		t.Errorf("checked %d targets, expected 2", checked)
	}
}

// A preset the operator never froze is not a verification command.
func TestUnknownPresetIsRejected(t *testing.T) {
	root := repoWith(t, "a.py")
	presets := []recipe.Preset{{Name: ".: pytest", Kind: recipe.KindTest, Argv: []string{"pytest"}}}
	bad, _ := PlanTargets{Root: root, Presets: presets}.Validate(context.Background(), workflow.Plan{
		Files: workflow.Targets{workflow.Target{Path: "a.py"}}, WriteAllowlist: []string{"a.py"},
		Regenerate: []string{"make generate"},
	})
	if len(bad) != 1 || bad[0].Kind != TargetPreset {
		t.Fatalf("an invented preset was accepted: %+v", bad)
	}
	if len(bad[0].Evidence) == 0 || bad[0].Evidence[0] != ".: pytest" {
		t.Errorf("the correction does not offer the real presets: %+v", bad[0].Evidence)
	}
}

// Absence of an index is not evidence of absence: a repository with no
// semantic graph must not have every symbol refused.
func TestSymbolsAreNotRefusedWhenTheGraphKnowsNothing(t *testing.T) {
	root := repoWith(t, "a.py")
	bad, _ := PlanTargets{Root: root, Graph: &emptyGraph{}}.Validate(context.Background(), workflow.Plan{
		Files: workflow.Targets{workflow.Target{Path: "a.py"}}, WriteAllowlist: []string{"a.py"},
		Symbols: []string{"does_not_resolve", "neither_does_this"},
	})
	for _, c := range bad {
		if c.Kind == TargetSymbol {
			t.Errorf("a symbol was refused by a graph that knows nothing: %+v", c)
		}
	}
}

func TestRegenerationBudgetIsBounded(t *testing.T) {
	if maxPlanRegenerations < 1 || maxPlanRegenerations > 3 {
		t.Errorf("the regeneration budget is %d; it must be small and non-zero", maxPlanRegenerations)
	}
}

// The recorded pytest failure: the correct file with rationale appended to
// the same field. Refused, with a correction that names the real path, and
// nothing rewritten.
func TestCorrectPathWithProseIsRefusedWithDidYouMean(t *testing.T) {
	root := repoWith(t, "src/_pytest/compat.py", "src/_pytest/fixtures.py")
	target := "src/_pytest/compat.py (num_mock_patch_args, lines 62-73: replace " +
		"`p.new in sentinels` with an identity test)"
	bad, _ := PlanTargets{Root: root}.Validate(context.Background(), workflow.Plan{
		Files:          workflow.Targets{workflow.Target{Path: target}},
		WriteAllowlist: []string{"src/_pytest/compat.py"},
	})
	if len(bad) == 0 {
		t.Fatal("a path with prose appended was accepted")
	}
	c := bad[0]
	if c.Target != target {
		t.Errorf("the contradiction is about %q, not the target the model wrote", c.Target)
	}
	if !strings.Contains(c.Reason, "path field must contain only") {
		t.Errorf("the correction does not say what the field is for: %s", c.Reason)
	}
	joined := strings.Join(c.Evidence, " ")
	if !strings.Contains(joined, "Did you mean: src/_pytest/compat.py") {
		t.Errorf("the correction does not name the real path inside it: %v", c.Evidence)
	}
	// And the plan itself is untouched: no silent rewrite.
	msg := CorrectionFor(bad)
	if !strings.Contains(msg, "Generate a corrected plan") {
		t.Error("the correction does not ask the planner to regenerate")
	}
}

// An invalid symbol in a file the graph knows must yield that file's real
// declarations.
func TestInvalidSymbolOffersDeclarationsFromTheClaimedFile(t *testing.T) {
	root := repoWith(t, "django/db/models/query.py")
	g := &fileGraph{byPath: map[string][]graph.Node{
		"django/db/models/query.py": {
			{Name: "prefetch_related_objects", Kind: graph.KindFunction,
				Path: "django/db/models/query.py", StartLine: 10},
			{Name: "get_prefetch_queryset", Kind: graph.KindMethod,
				Path: "django/db/models/query.py", StartLine: 20},
			// An artifact, which must not be offered as an alternative.
			{Name: "209", Kind: graph.KindVariable, Path: "django/db/models/query.py",
				StartLine: 30, FQN: "django/db/models/query.py::local 209"},
		},
	}, byName: map[string][]graph.Node{
		"prefetch_related_objects": {{Name: "prefetch_related_objects", Kind: graph.KindFunction,
			Path: "django/db/models/query.py", StartLine: 10}},
	}}
	bad, _ := PlanTargets{Root: root, Graph: g}.Validate(context.Background(), workflow.Plan{
		Files:          workflow.Targets{workflow.Target{Path: "django/db/models/query.py"}},
		WriteAllowlist: []string{"django/db/models/query.py"},
		// One resolves, so refusals are permitted; one does not.
		Symbols: []string{"prefetch_related_objects", "Query._slice"},
	})
	var sym *Contradiction
	for i := range bad {
		if bad[i].Kind == TargetSymbol {
			sym = &bad[i]
		}
	}
	if sym == nil {
		t.Fatal("the invented symbol was accepted")
	}
	if len(sym.Evidence) == 0 {
		t.Fatal("the symbol correction carried no alternatives, which is the recorded defect")
	}
	joined := strings.Join(sym.Evidence, " ")
	if !strings.Contains(joined, "get_prefetch_queryset") {
		t.Errorf("the alternatives do not include the claimed file's declarations: %v", sym.Evidence)
	}
	if strings.Contains(joined, "209") {
		t.Errorf("an artifact was offered as an alternative: %v", sym.Evidence)
	}
}

// fileGraph answers by path and by name, and counts queries.
type fileGraph struct {
	graph.Graph
	byPath map[string][]graph.Node
	byName map[string][]graph.Node
}

func (g *fileGraph) NodesInFile(_ context.Context, path string, _ int) ([]graph.Node, error) {
	return g.byPath[path], nil
}

func (g *fileGraph) NodesByName(_ context.Context, name string, _ []graph.NodeKind, _ int) ([]graph.Node, error) {
	return g.byName[name], nil
}

// An obligation may name a file the plan itself creates. The recorded refusal
// ("is named by an obligation but is not a file in this repository … do not
// keep a target that was refused") talked a planner out of the new regression
// test the task needed. An invented file in an invented directory is still
// refused, and so is a missing file the plan does not declare.
func TestAnObligationMayNameAFileThePlanCreates(t *testing.T) {
	root := repoWith(t, "internal/worker/reconcile.go")
	obligation := func(p string) workflow.Obligation {
		return workflow.Obligation{Symbol: "RunOnce", Path: p, Reason: "tested here",
			Resolution: workflow.Resolution{Action: workflow.ActionEdit}}
	}
	plan := func(files []string, ob string) workflow.Plan {
		var targets workflow.Targets
		for _, f := range files {
			targets = append(targets, workflow.Target{Path: f})
		}
		return workflow.Plan{Files: targets, WriteAllowlist: files, Obligations: []workflow.Obligation{obligation(ob)}}
	}
	const newTest = "internal/worker/reconcile_test.go"
	if bad, _ := (PlanTargets{Root: root}).Validate(context.Background(),
		plan([]string{"internal/worker/reconcile.go", newTest}, newTest)); len(bad) != 0 {
		t.Errorf("an obligation on a file the plan creates was refused: %+v", bad)
	}
	if bad, _ := (PlanTargets{Root: root}).Validate(context.Background(),
		plan([]string{"internal/worker/reconcile.go"}, newTest)); len(bad) != 1 {
		t.Errorf("an obligation on an undeclared missing file was accepted: %+v", bad)
	}
	if bad, _ := (PlanTargets{Root: root}).Validate(context.Background(),
		plan([]string{"internal/invented/x_test.go"}, "internal/invented/x_test.go")); len(bad) == 0 {
		t.Error("an obligation on a file in an invented directory was accepted")
	}
}

// A symbol written with a single-colon file prefix names the declaration after
// the colon. It was refused as `go:Reconciler`.
func TestAFilePrefixedSymbolResolves(t *testing.T) {
	if got := lastComponent("internal/worker/reconcile.go:Reconciler"); got != "Reconciler" {
		t.Errorf("lastComponent = %q, want Reconciler", got)
	}
	if got := lastComponent("internal/service/user.go::UserService.Email"); got != "Email" {
		t.Errorf("lastComponent = %q, want Email", got)
	}
}

// A declaration the plan will add, named with a file the plan writes, is not
// an invention. The recorded plans named their new regression test that way
// and were refused until the corrections ran out.
func TestANewDeclarationInAPlannedFileIsNotRefused(t *testing.T) {
	root := repoWith(t, "internal/service/user.go", "internal/service/user_test.go")
	email := graph.Node{ID: 1, Name: "Email", Path: "internal/service/user.go", Kind: graph.KindMethod}
	g := &fileGraph{
		byName: map[string][]graph.Node{"Email": {email}},
		byPath: map[string][]graph.Node{"internal/service/user.go": {email}},
	}
	plan := workflow.Plan{
		Files: workflow.Targets{{Path: "internal/service/user.go"}, {Path: "internal/service/user_test.go"}},
		Symbols: []string{
			"UserService.Email",
			"internal/service/user_test.go:TestEmailMissingUser", // added by the plan
			"TestInvented", // named nowhere
			"internal/other/other_test.go:TestElsewhere", // a file the plan does not write
		},
	}
	bad, _ := PlanTargets{Root: root, Graph: g}.Validate(context.Background(), plan)
	refused := map[string]bool{}
	for _, c := range bad {
		if c.Kind == TargetSymbol {
			refused[c.Target] = true
		}
	}
	if refused["internal/service/user_test.go:TestEmailMissingUser"] {
		t.Error("the new test in a planned file was refused")
	}
	if refused["UserService.Email"] {
		t.Error("an existing qualified symbol was refused")
	}
	if !refused["TestInvented"] || !refused["internal/other/other_test.go:TestElsewhere"] {
		t.Errorf("an invented symbol was accepted; refused = %v", refused)
	}
}

// A new member of a type the plan edits is not an invention; a member of a
// type in a file the plan does not write still is.
func TestANewMemberOfAPlannedTypeIsNotRefused(t *testing.T) {
	root := repoWith(t, "internal/domain/order.go", "internal/store/store.go")
	order := graph.Node{ID: 1, Name: "Order", FQN: "example.com/wh/internal/domain.Order",
		Path: "internal/domain/order.go", Kind: graph.KindType}
	stock := graph.Node{ID: 2, Name: "Stock", FQN: "example.com/wh/internal/store.Stock",
		Path: "internal/store/store.go", Kind: graph.KindType}
	g := &fileGraph{
		byName: map[string][]graph.Node{"Order": {order}, "Stock": {stock}},
		byPath: map[string][]graph.Node{order.Path: {order}, stock.Path: {stock}},
	}
	bad, _ := PlanTargets{Root: root, Graph: g}.Validate(context.Background(), workflow.Plan{
		Files:   workflow.Targets{{Path: "internal/domain/order.go"}},
		Symbols: []string{"Order", "domain.Order.ReservationReleasedAt", "Stock.Invented"},
	})
	refused := map[string]bool{}
	for _, c := range bad {
		refused[c.Target] = true
	}
	if refused["domain.Order.ReservationReleasedAt"] {
		t.Error("a new field on a type the plan edits was refused")
	}
	if !refused["Stock.Invented"] {
		t.Error("a member of a type in a file the plan does not write was accepted")
	}
}
