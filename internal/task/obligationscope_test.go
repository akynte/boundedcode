package task

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/akynte/boundedcode/internal/analyzers/golang"
	"github.com/akynte/boundedcode/internal/graph"
	"github.com/akynte/boundedcode/internal/index"
	"github.com/akynte/boundedcode/internal/retrieval"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/workflow"
	"github.com/akynte/boundedcode/internal/workspace"
)

// A widely used type beside the one method a fix changes, as on the recorded
// task: LOCALIZE named domain.Order next to Reconciler.RunOnce, and every
// consumer of Order became an obligation.
func obligationRepo(t *testing.T) *retrieval.Retriever {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":                   "module example.test/wh\n\ngo 1.26\n",
		"internal/domain/order.go": "package domain\n\n// Order is an order.\ntype Order struct{ N int }\n",
		"internal/api/api.go": "package api\n\nimport \"example.test/wh/internal/domain\"\n\n" +
			"// Handle serves an order.\nfunc Handle(o domain.Order) int { return o.N }\n",
		"internal/report/report.go": "package report\n\nimport \"example.test/wh/internal/domain\"\n\n" +
			"// Sum reports an order.\nfunc Sum(o domain.Order) int { return o.N }\n",
		"internal/worker/reconcile.go": "package worker\n\nimport \"example.test/wh/internal/domain\"\n\n" +
			"// Reconciler frees reservations.\ntype Reconciler struct{}\n\n" +
			"// RunOnce runs one pass.\nfunc (r *Reconciler) RunOnce(o domain.Order) int { return o.N }\n\n" +
			"// Tick drives the reconciler.\nfunc Tick(r *Reconciler) int { return r.RunOnce(domain.Order{}) }\n",
	} {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := store.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.CloseAll() })
	ctx := context.Background()
	st, err := root.OpenWorkspace(ctx, workspace.DeriveID(dir, "", t.Name()))
	if err != nil {
		t.Fatal(err)
	}
	ix := index.New(st, index.Options{Analyzers: []index.Analyzer{golang.New()}})
	if err := ix.RegisterRepository(ctx, workspace.Repository{ID: "r", Name: "r", Path: dir, DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ix.Repository(ctx, "r", dir); err != nil {
		t.Fatal(err)
	}
	return retrieval.New(st)
}

func consumerNames(imp *graph.Impact) map[string]bool {
	names := map[string]bool{}
	if imp == nil {
		return names
	}
	actionable, _ := workflow.ActionableConsumers(imp)
	for _, c := range actionable {
		names[c.Node.Name] = true
	}
	return names
}

func TestObligationsAreTheConsumersOfWhatThePlanChanges(t *testing.T) {
	ctx := context.Background()
	ret := obligationRepo(t)
	r := &Runner{Retriever: ret}
	localized, err := ret.Build(ctx, retrieval.Request{ImpactOf: []string{"Order", "Reconciler.RunOnce"}})
	if err != nil {
		t.Fatal(err)
	}
	all := consumerNames(localized.Impact)
	if !all["Handle"] || !all["Sum"] {
		t.Fatalf("the fixture's localized impact should reach every consumer of Order, got %v", all)
	}

	worker := []string{"internal/worker/reconcile.go"}
	owed := consumerNames(r.obligationImpact(ctx, workflow.Plan{
		Symbols: []string{"Reconciler.RunOnce"}, WriteAllowlist: worker}, localized.Impact))
	if owed["Handle"] || owed["Sum"] {
		t.Errorf("a fix inside RunOnce was made to answer for every consumer of Order: %v", owed)
	}
	if !owed["Tick"] {
		t.Errorf("RunOnce's own caller is not an obligation: %v", owed)
	}

	// The recorded plan also listed a declaration it only reads. A plan cannot
	// change a file it may not write, so Order's consumers are still not owed.
	owed = consumerNames(r.obligationImpact(ctx, workflow.Plan{
		Symbols: []string{"Reconciler.RunOnce", "Order"}, WriteAllowlist: worker}, localized.Impact))
	if owed["Handle"] || owed["Sum"] || !owed["Tick"] {
		t.Errorf("a symbol outside the write allowlist changed the obligations: %v", owed)
	}

	// A plan whose symbols name nothing the graph knows does not escape: the
	// localized impact stands, under the same direct-caller rule.
	runOnce, err := ret.Build(ctx, retrieval.Request{ImpactOf: []string{"Reconciler.RunOnce"}})
	if err != nil {
		t.Fatal(err)
	}
	fallback := consumerNames(r.obligationImpact(ctx, workflow.Plan{
		Symbols: []string{"Invented.Method"}, WriteAllowlist: worker}, runOnce.Impact))
	if !fallback["Tick"] {
		t.Errorf("a plan naming no real symbol was excused from the localized obligations: %v", fallback)
	}
}

// Consumers of a changed type are not owed up front. The recorded plan added
// a field to domain.Order and was made to justify every declaration that
// accepts an Order; a breaking change to it is the post-edit signature
// check's to catch.
func TestTheConsumersOfAChangedTypeAreNotOwed(t *testing.T) {
	ctx := context.Background()
	ret := obligationRepo(t)
	r := &Runner{Retriever: ret}
	localized, err := ret.Build(ctx, retrieval.Request{ImpactOf: []string{"Order"}})
	if err != nil {
		t.Fatal(err)
	}
	if all := consumerNames(localized.Impact); !all["Handle"] {
		t.Fatalf("the fixture should show Handle consuming Order, got %v", all)
	}
	owed := consumerNames(r.obligationImpact(ctx, workflow.Plan{
		Symbols: []string{"domain.Order"}, WriteAllowlist: []string{"internal/domain/order.go"}}, localized.Impact))
	if owed["Handle"] || owed["Sum"] || owed["RunOnce"] {
		t.Errorf("consumers of a changed type were made obligations: %v", owed)
	}
}

// EDIT's packet does not depend on the planner filling in symbols. Recorded
// plans left them empty or listed file paths in them, and EDIT got no code.
func TestEditRetrievesThePlannedFilesWhenSymbolsNameNothing(t *testing.T) {
	ctx := context.Background()
	r := &Runner{Retriever: obligationRepo(t)}
	const worker = "internal/worker/reconcile.go"
	for name, plan := range map[string]workflow.Plan{
		"empty symbols":        {WriteAllowlist: []string{worker}},
		"a file as a symbol":   {Symbols: []string{worker}, WriteAllowlist: []string{worker}},
		"an unresolved symbol": {Symbols: []string{"Invented.Thing"}, WriteAllowlist: []string{worker}},
	} {
		pkt, err := r.Retriever.Build(ctx, retrieval.Request{Symbols: r.editSymbols(ctx, plan), TokenBudget: 20000})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, sl := range pkt.Slices {
			found = found || (sl.Path == worker && sl.Symbol == "RunOnce")
		}
		if !found {
			t.Errorf("%s: EDIT's packet does not show RunOnce from the planned file", name)
		}
	}
	// A plan whose symbols resolve keeps exactly them.
	plan := workflow.Plan{Symbols: []string{"Reconciler.RunOnce"}, WriteAllowlist: []string{worker}}
	if got := r.editSymbols(ctx, plan); len(got) != 1 || got[0] != "Reconciler.RunOnce" {
		t.Errorf("resolvable symbols were widened: %v", got)
	}
}
