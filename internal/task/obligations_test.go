package task

// Obligations, and the one bounded pass that stands between "the graph could
// have told you" and refusing the plan.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/graph"
	"github.com/akynte/boundedcode/internal/workflow"
	"github.com/akynte/boundedcode/internal/workspace"
)

// emptyGraph knows nothing and is asked nothing more than once.
type emptyGraph struct {
	graph.Graph
	calls int
}

func (g *emptyGraph) WorkspaceID() workspace.ID { return workspace.ID("test") }
func (g *emptyGraph) NodesByName(context.Context, string, []graph.NodeKind, int) ([]graph.Node, error) {
	g.calls++
	return nil, nil
}

// knownGraph answers with a fixed set, and counts how often it is asked so an
// unbounded traversal would be visible.
type knownGraph struct {
	graph.Graph
	nodes map[string][]graph.Node
	calls int
}

func (g *knownGraph) WorkspaceID() workspace.ID { return workspace.ID("test") }
func (g *knownGraph) NodesByName(_ context.Context, name string, _ []graph.NodeKind, _ int) ([]graph.Node, error) {
	g.calls++
	return g.nodes[name], nil
}

func consumer(name, path string) graph.Consumer {
	return graph.Consumer{Node: graph.Node{
		ID: 1, Name: name, FQN: "scip-python python workspace 0 `_pytest.fixtures`/FixtureDef#" + name + ".",
		Path: path, Kind: graph.KindField, StartLine: 42,
	}}
}

// The plan accounts for the consumer: nothing to do.
func TestObligationResolvedDirectly(t *testing.T) {
	plan := workflow.Plan{
		WriteAllowlist: []string{"src/_pytest/fixtures.py"},
		Obligations: []workflow.Obligation{{
			Symbol: "argnames", Path: "src/_pytest/fixtures.py",
			Reason: "signature changed", Resolution: workflow.Resolution{Action: workflow.ActionEdit},
		}},
	}
	imp := &graph.Impact{Consumers: []graph.Consumer{consumer("argnames", "src/_pytest/fixtures.py")}}
	reps := ResolveObligations(context.Background(), &emptyGraph{}, plan, imp)
	if len(reps) != 1 || reps[0].Status != ObligationResolved {
		t.Fatalf("a discharged obligation was not recognised: %+v", reps)
	}
	if open, _ := OpenObligations(reps); open != 0 {
		t.Errorf("a resolved obligation left %d open", open)
	}
}

// The plan does not, but the graph can describe the consumer, so the planner
// gets evidence rather than a fully-qualified name it cannot type.
func TestUnresolvedButSearchableProducesGraphEvidence(t *testing.T) {
	g := &knownGraph{nodes: map[string][]graph.Node{
		"argnames": {
			{ID: 1, Name: "argnames", Path: "src/_pytest/fixtures.py"},
			{ID: 2, Name: "argnames", Path: "src/_pytest/compat.py"},
		},
	}}
	imp := &graph.Impact{Consumers: []graph.Consumer{consumer("argnames", "src/_pytest/fixtures.py")}}
	reps := ResolveObligations(context.Background(), g, workflow.Plan{}, imp)
	if len(reps) != 1 || reps[0].Status != ObligationSearchable {
		t.Fatalf("expected a searchable obligation, got %+v", reps)
	}
	if len(reps[0].Evidence) == 0 {
		t.Fatal("no graph evidence was gathered")
	}
	if len(reps[0].Attempts) < 2 {
		t.Errorf("the attempts were not recorded: %+v", reps[0].Attempts)
	}
	msg := ObligationCorrection(reps)
	// The correction must use the short symbol and the path, not the SCIP FQN.
	if !strings.Contains(msg, "`argnames`") || !strings.Contains(msg, "src/_pytest/fixtures.py") {
		t.Errorf("the correction does not name the consumer usably: %s", msg)
	}
	if strings.Contains(msg, "scip-python python workspace") {
		t.Error("the correction quotes the indexer's fully-qualified name, which nobody can type back")
	}
	// One bounded expansion: exactly one graph query per obligation.
	if g.calls != 1 {
		t.Errorf("the graph was consulted %d times for one obligation; the expansion is not bounded", g.calls)
	}
}

// A consumer the graph cannot describe is genuinely unresolvable, and the
// plan is still refused.
func TestUnresolvableObligationStillRefusesThePlan(t *testing.T) {
	g := &emptyGraph{}
	imp := &graph.Impact{Consumers: []graph.Consumer{
		{Node: graph.Node{ID: 9, Name: "ghost", Path: "src/x.py", Kind: graph.KindFunction, StartLine: 3}},
	}}
	reps := ResolveObligations(context.Background(), g, workflow.Plan{}, imp)
	if len(reps) != 1 {
		t.Fatalf("expected one report, got %+v", reps)
	}
	// The node itself is evidence enough to describe, so this is searchable;
	// what matters is that it is not resolved and the plan is refused.
	open, _ := OpenObligations(reps)
	if open != 1 {
		t.Errorf("an undischarged obligation did not refuse the plan (open=%d)", open)
	}
	if reps[0].Status == ObligationResolved {
		t.Error("an obligation nobody discharged was reported as resolved")
	}
}

// Many consumers must not become many traversals.
func TestObligationResolutionDoesNotTraverseUnbounded(t *testing.T) {
	g := &knownGraph{nodes: map[string][]graph.Node{}}
	var consumers []graph.Consumer
	for i := range 20 {
		consumers = append(consumers, consumer("sym"+string(rune('a'+i)), "src/f.py"))
	}
	reps := ResolveObligations(context.Background(), g, workflow.Plan{}, &graph.Impact{Consumers: consumers})
	if len(reps) != 20 {
		t.Fatalf("expected 20 reports, got %d", len(reps))
	}
	if g.calls > 20 {
		t.Errorf("%d graph queries for 20 obligations: the pass is recursing", g.calls)
	}
}

// The artifact obligations, as the pytest reproduction produced them.
//
// Sixteen consumers were demanded; ten could not be answered by anyone. The
// filter must remove exactly those ten and keep the six real declarations.
func TestArtifactConsumersNeverBecomeObligations(t *testing.T) {
	impact := &graph.Impact{Consumers: []graph.Consumer{
		// The six real declarations from the recorded run.
		{Node: graph.Node{ID: 1, Name: "argnames", Kind: graph.KindField, Path: "src/_pytest/fixtures.py", StartLine: 10,
			FQN: "scip-python python workspace 0 `_pytest.fixtures`/FixtureDef#argnames."}},
		{Node: graph.Node{ID: 2, Name: "test_getfuncargnames", Kind: graph.KindFunction, Path: "testing/python/fixtures.py", StartLine: 20}},
		{Node: graph.Node{ID: 3, Name: "test_getfuncargnames_patching", Kind: graph.KindMethod, Path: "testing/python/integration.py", StartLine: 30}},
		{Node: graph.Node{ID: 4, Name: "test_wrapped_getfuncargnames", Kind: graph.KindMethod, Path: "testing/python/integration.py", StartLine: 40}},
		{Node: graph.Node{ID: 5, Name: "execute", Kind: graph.KindMethod, Path: "src/_pytest/fixtures.py", StartLine: 50}},
		{Node: graph.Node{ID: 6, Name: "Metafunc", Kind: graph.KindMethod, Path: "testing/python/metafunc.py", StartLine: 60}},
		// The ten artifacts: nine SCIP locals and a file node.
		{Node: graph.Node{ID: 7, Name: "209", Kind: graph.KindVariable, Path: "src/_pytest/fixtures.py", StartLine: 70,
			FQN: "src/_pytest/fixtures.py::local 209"}},
		{Node: graph.Node{ID: 8, Name: "11", Kind: graph.KindVariable, Path: "testing/python/integration.py", StartLine: 71,
			FQN: "testing/python/integration.py::local 11"}},
		{Node: graph.Node{ID: 9, Name: "13", Kind: graph.KindVariable, Path: "testing/python/integration.py", StartLine: 72,
			FQN: "testing/python/integration.py::local 13"}},
		{Node: graph.Node{ID: 10, Name: "5", Kind: graph.KindVariable, Path: "testing/python/metafunc.py", StartLine: 73,
			FQN: "testing/python/metafunc.py::local 5"}},
		{Node: graph.Node{ID: 11, Name: "167", Kind: graph.KindVariable, Path: "src/_pytest/fixtures.py", StartLine: 74,
			FQN: "src/_pytest/fixtures.py::local 167"}},
		{Node: graph.Node{ID: 12, Name: "213", Kind: graph.KindVariable, Path: "src/_pytest/fixtures.py", StartLine: 75,
			FQN: "src/_pytest/fixtures.py::local 213"}},
		{Node: graph.Node{ID: 13, Name: "6", Kind: graph.KindVariable, Path: "testing/python/metafunc.py", StartLine: 76,
			FQN: "testing/python/metafunc.py::local 6"}},
		{Node: graph.Node{ID: 14, Name: "168", Kind: graph.KindVariable, Path: "src/_pytest/fixtures.py", StartLine: 77,
			FQN: "src/_pytest/fixtures.py::local 168"}},
		{Node: graph.Node{ID: 15, Name: "215", Kind: graph.KindVariable, Path: "src/_pytest/fixtures.py", StartLine: 78,
			FQN: "src/_pytest/fixtures.py::local 215"}},
		{Node: graph.Node{ID: 16, Name: "fixtures.py", Kind: graph.KindFile, Path: "src/_pytest/fixtures.py", StartLine: 1,
			FQN: "src/_pytest/fixtures.py"}},
		// And the two directory nodes, which have no path at all.
		{Node: graph.Node{ID: 17, Name: "_pytest", Kind: graph.KindDirectory, FQN: "src/_pytest"}},
		{Node: graph.Node{ID: 18, Name: "src", Kind: graph.KindDirectory, FQN: "src"}},
	}}

	kept, dropped := workflow.ActionableConsumers(impact)
	if len(kept) != 6 {
		var names []string
		for _, c := range kept {
			names = append(names, c.Node.Name)
		}
		t.Fatalf("kept %d consumers (%v), want the 6 real declarations", len(kept), names)
	}
	if dropped != 12 {
		t.Errorf("dropped %d, want 12 (9 locals, 1 file, 2 directories)", dropped)
	}
	for _, c := range kept {
		if workflow.PlannerActionable(c.Node) != true {
			t.Errorf("%s was kept but is not actionable", c.Node.Name)
		}
		for _, r := range c.Node.Name {
			if r >= '0' && r <= '9' && len(c.Node.Name) <= 3 {
				t.Errorf("a numerically-named symbol survived the filter: %q", c.Node.Name)
			}
			break
		}
	}
	// And the whole pipeline agrees: no obligation is demanded for an artifact.
	reps := ResolveObligations(context.Background(), &emptyGraph{}, workflow.Plan{}, impact)
	if len(reps) != 6 {
		t.Errorf("ResolveObligations produced %d reports, want 6", len(reps))
	}
	for _, r := range reps {
		if r.Symbol == "209" || r.Symbol == "fixtures.py" {
			t.Errorf("an artifact reached the planner obligation interface: %+v", r)
		}
	}
}

// The SCIP → graph → path → short symbol → planner mapping, per kind.
func TestScipToPlannerMappingByKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		node graph.Node
		want bool
	}{
		{"module-level function", graph.Node{Name: "handler", Kind: graph.KindFunction,
			Path: "src/pkg/core.py", StartLine: 12,
			FQN: "scip-python python probe 0 `pkg.core`/handler()."}, true},
		{"class method", graph.Node{Name: "describe", Kind: graph.KindMethod,
			Path: "src/pkg/core.py", StartLine: 9,
			FQN: "scip-python python probe 0 `pkg.core`/Handler#describe()."}, true},
		{"class", graph.Node{Name: "Handler", Kind: graph.KindClass,
			Path: "src/pkg/core.py", StartLine: 6,
			FQN: "scip-python python probe 0 `pkg.core`/Handler#"}, true},
		{"field", graph.Node{Name: "name", Kind: graph.KindField,
			Path: "src/pkg/core.py", StartLine: 8,
			FQN: "scip-python python probe 0 `pkg.core`/Handler#name."}, true},
		{"local variable", graph.Node{Name: "1", Kind: graph.KindVariable,
			Path: "tests/test_core.py", StartLine: 5,
			FQN: "tests/test_core.py::local 1"}, false},
		{"nested local", graph.Node{Name: "42", Kind: graph.KindVariable,
			Path: "src/pkg/core.py", StartLine: 7, FQN: "local 42"}, false},
		{"file node", graph.Node{Name: "core.py", Kind: graph.KindFile,
			Path: "src/pkg/core.py", StartLine: 1, FQN: "src/pkg/core.py"}, false},
		{"directory node", graph.Node{Name: "pkg", Kind: graph.KindDirectory, FQN: "src/pkg"}, false},
		{"module node", graph.Node{Name: "core", Kind: graph.KindModule,
			Path: "src/pkg/core.py", StartLine: 1}, false},
		{"declaration with no source line", graph.Node{Name: "orphan", Kind: graph.KindFunction,
			Path: "src/pkg/core.py"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := workflow.PlannerActionable(tc.node); got != tc.want {
				t.Errorf("PlannerActionable(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// The obligation vocabulary is closed, and the schema and the validator
// describe the same domain.
func TestObligationVocabularyIsClosed(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(planSchemaV2, &doc); err != nil {
		t.Fatal(err)
	}
	obl := doc["properties"].(map[string]any)["obligations"].(map[string]any)
	res := obl["items"].(map[string]any)["properties"].(map[string]any)["resolution"].(map[string]any)
	enum, ok := res["properties"].(map[string]any)["action"].(map[string]any)["enum"].([]any)
	if !ok {
		t.Fatal("resolution.action carries no enum, so the model may write anything")
	}
	got := map[string]bool{}
	for _, v := range enum {
		got[v.(string)] = true
	}
	for _, want := range []string{string(workflow.ActionEdit), string(workflow.ActionNoChange)} {
		if !got[want] {
			t.Errorf("the schema omits the action %q the validator accepts", want)
		}
	}
	if len(enum) != 2 {
		t.Errorf("the schema offers %d actions; the validator accepts 2", len(enum))
	}
	// The exact values the model produced in the recorded runs must now be
	// unrepresentable.
	for _, invented := range []string{"fix", "verify", "keep_passing", "no change needed:"} {
		if got[invented] {
			t.Errorf("%q is expressible in the schema but is not a valid action", invented)
		}
		if (workflow.Resolution{Action: workflow.ResolutionAction(invented)}).Valid() {
			t.Errorf("the validator accepts %q, which the schema does not offer", invented)
		}
	}
	// And the two valid ones behave as documented.
	if !(workflow.Resolution{Action: workflow.ActionEdit}).Valid() {
		t.Error("edit was refused")
	}
	if (workflow.Resolution{Action: workflow.ActionNoChange}).Valid() {
		t.Error("no_change_needed without a reason was accepted")
	}
	if !(workflow.Resolution{Action: workflow.ActionNoChange, Reason: "signature unchanged"}).Valid() {
		t.Error("no_change_needed with a reason was refused")
	}
}

// A file target must carry its rationale beside the path, not inside it.
func TestFileTargetSeparatesPathFromRationale(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(planSchemaV2, &doc); err != nil {
		t.Fatal(err)
	}
	files := doc["properties"].(map[string]any)["files"].(map[string]any)
	item := files["items"].(map[string]any)
	props := item["properties"].(map[string]any)
	for _, want := range []string{"path", "reason"} {
		if _, ok := props[want]; !ok {
			t.Errorf("the file target has no %q field", want)
		}
	}
	if item["additionalProperties"] != false {
		t.Error("the file target accepts extra fields")
	}
	// A bare string still decodes, so existing task files keep working.
	var tgt workflow.Target
	if err := json.Unmarshal([]byte(`"a/b.py"`), &tgt); err != nil || tgt.Path != "a/b.py" {
		t.Errorf("a bare string target did not decode: %v %+v", err, tgt)
	}
	if err := json.Unmarshal([]byte(`{"path":"a/b.py","reason":"why"}`), &tgt); err != nil ||
		tgt.Path != "a/b.py" || tgt.Reason != "why" {
		t.Errorf("a structured target did not decode: %v %+v", err, tgt)
	}
}

// goMethod is a Go method as the Go analyzer records it: FQN is
// <package path>.<receiver>.<method>.
func goMethod(receiver, name, path string) graph.Consumer {
	return graph.Consumer{Node: graph.Node{
		ID: 7, Name: name, FQN: "example.com/warehouse/internal/pricing." + receiver + "." + name,
		Path: path, Kind: graph.KindMethod, StartLine: 17,
	}}
}

// The recorded failure: two methods named Discount in one file, answered as
// BulkDiscount.Discount and LoyaltyDiscount.Discount — the only unambiguous way
// to name them — and both refused, because the rule accepted only the short
// name or the whole module-path FQN.
func TestAReceiverQualifiedObligationResolvesItsMethod(t *testing.T) {
	const rules = "internal/pricing/rules.go"
	waive := func(symbol, path string) workflow.Obligation {
		return workflow.Obligation{Symbol: symbol, Path: path, Reason: "caller",
			Resolution: workflow.Resolution{Action: workflow.ActionNoChange, Reason: "signature unchanged"}}
	}
	imp := &graph.Impact{Consumers: []graph.Consumer{
		goMethod("BulkDiscount", "Discount", rules),
		goMethod("LoyaltyDiscount", "Discount", rules),
	}}
	for _, symbol := range []string{"BulkDiscount.Discount", "pricing.BulkDiscount.Discount"} {
		plan := workflow.Plan{Obligations: []workflow.Obligation{waive(symbol, rules), waive("LoyaltyDiscount.Discount", rules)}}
		if err := plan.ValidateObligations(imp); err != nil {
			t.Errorf("%s was refused: %v", symbol, err)
		}
		if open, _ := OpenObligations(ResolveObligations(context.Background(), &emptyGraph{}, plan, imp)); open != 0 {
			t.Errorf("%s left %d obligation(s) open", symbol, open)
		}
	}
	// A tail is not a licence to match anything that ends the same way: the
	// dot boundary and the path are both still required.
	for _, bad := range []workflow.Obligation{
		waive("kDiscount.Discount", rules),
		waive("BulkDiscount.Discount", "internal/pricing/other.go"),
	} {
		plan := workflow.Plan{Obligations: []workflow.Obligation{bad, waive("LoyaltyDiscount.Discount", rules)}}
		if plan.ValidateObligations(imp) == nil {
			t.Errorf("%s in %s was accepted for BulkDiscount.Discount in %s", bad.Symbol, bad.Path, rules)
		}
	}
}

// A correction has to name same-named methods apart, and has to say that
// what the plan already resolved stays. The recorded correction listed
// "symbol `Discount`" twice and "each one below must appear"; the planner
// answered with a plan holding only those two, dropping 33 it had resolved.
func TestACorrectionNamesMethodsByReceiverAndKeepsWhatWasResolved(t *testing.T) {
	const rules = "internal/pricing/rules.go"
	imp := &graph.Impact{Consumers: []graph.Consumer{
		goMethod("BulkDiscount", "Discount", rules),
		goMethod("LoyaltyDiscount", "Discount", rules),
	}}
	reps := ResolveObligations(context.Background(), &emptyGraph{}, workflow.Plan{}, imp)
	msg := ObligationCorrection(reps)
	for _, want := range []string{"`BulkDiscount.Discount`", "`LoyaltyDiscount.Discount`", "Keep every obligation"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the correction is missing %s:\n%s", want, msg)
		}
	}
	// What the correction asks for must be accepted when written back.
	plan := workflow.Plan{}
	for _, r := range reps {
		plan.Obligations = append(plan.Obligations, workflow.Obligation{Symbol: r.Symbol, Path: r.Path,
			Reason: "caller", Resolution: workflow.Resolution{Action: workflow.ActionNoChange, Reason: "unchanged"}})
	}
	if err := plan.ValidateObligations(imp); err != nil {
		t.Errorf("the names the correction asked for were refused: %v", err)
	}
}

// When the plan wrote an obligation for a consumer that does not count, the
// correction says why. The recorded correction only repeated "add
// Service.Place" to a plan that had added it — resolved as an edit of a file
// it could not write — and the planner's guess at what else was wanted
// invented a symbol.
func TestACorrectionSaysWhyAnExistingObligationDoesNotCount(t *testing.T) {
	place := graph.Consumer{Node: graph.Node{
		ID: 3, Name: "Place", FQN: "example.com/warehouse/internal/orders.Service.Place",
		Path: "internal/orders/orders.go", Kind: graph.KindMethod, StartLine: 68,
	}}
	imp := &graph.Impact{Consumers: []graph.Consumer{place}}
	for _, c := range []struct {
		ob   workflow.Obligation
		want string
	}{
		{workflow.Obligation{Symbol: "Service.Place", Path: "internal/orders/orders.go",
			Resolution: workflow.Resolution{Action: workflow.ActionEdit}}, "not in write_allowlist"},
		{workflow.Obligation{Symbol: "Service.Place", Path: "internal/orders/orders_test.go",
			Resolution: workflow.Resolution{Action: workflow.ActionNoChange, Reason: "unchanged"}}, "is in internal/orders/orders.go"},
		{workflow.Obligation{Symbol: "Service.Place", Path: "internal/orders/orders.go",
			Resolution: workflow.Resolution{Action: workflow.ActionNoChange}}, "without a reason"},
	} {
		plan := workflow.Plan{WriteAllowlist: []string{"internal/pricing/pricing.go"}, Obligations: []workflow.Obligation{c.ob}}
		msg := ObligationCorrection(ResolveObligations(context.Background(), &emptyGraph{}, plan, imp))
		if !strings.Contains(msg, "does not count") || !strings.Contains(msg, c.want) {
			t.Errorf("for %+v the correction does not say %q:\n%s", c.ob, c.want, msg)
		}
	}
}
