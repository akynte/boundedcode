package task

// An obligation the graph can still answer is not a dead end.
//
// The impact check refuses a plan that leaves a consumer unaccounted for, and
// it is right to: changing a signature without saying what happens to its
// callers is how a green build becomes a broken one. But the refusal was
// terminal and its message was one line naming a fully-qualified symbol —
// with SCIP that reads `scip-python python workspace 0 \`_pytest.fixtures\`/
// FixtureDef#argnames.`, which is not something a planner can act on, and the
// pytest task died on it three times in a row.
//
// So before a plan is refused, the obligations it did not discharge are put
// through one bounded deterministic pass: resolve the symbol exactly, ask the
// graph what references it, and hand the planner that evidence in the words
// the repository uses. The model is consulted only afterwards, by being asked
// for a corrected plan. Nothing here waives an obligation.

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/graph"
	"github.com/akynte/boundedcode/internal/policy"
	"github.com/akynte/boundedcode/internal/retrieval"
	"github.com/akynte/boundedcode/internal/workflow"
)

// ObligationStatus is how far deterministic resolution got.
type ObligationStatus string

const (
	// ObligationResolved: the plan accounts for this consumer.
	ObligationResolved ObligationStatus = "resolved"
	// ObligationSearchable: the plan does not, and the graph has evidence
	// that would let a planner account for it. One expansion is spent on it.
	ObligationSearchable ObligationStatus = "unresolved_but_searchable"
	// ObligationUnresolvable: the plan does not, and the graph has nothing
	// further to offer. This is the only state that refuses a plan.
	ObligationUnresolvable ObligationStatus = "unresolvable"
)

// ObligationReport is one consumer's outcome, recorded so a reader can see
// what was tried rather than only that it failed.
type ObligationReport struct {
	ID       string           `json:"id"`
	Symbol   string           `json:"symbol"`
	Path     string           `json:"path"`
	Status   ObligationStatus `json:"status"`
	Attempts []string         `json:"attempts,omitempty"`
	Evidence []string         `json:"graph_evidence,omitempty"`
	// Refused says why an obligation the plan did write for this consumer
	// does not discharge it, when it wrote one.
	Refused string `json:"refused,omitempty"`
}

// maxObligationEvidence bounds one expansion. The point is to give a planner
// enough to name the consumer, not to hand it the call graph.
const maxObligationEvidence = 6

// ResolveObligations classifies every impact consumer the plan left open.
//
// Bounded by construction: one lookup per obligation and one neighbour query
// for the ones that need it, both capped, with no recursion. It uses the
// graph the impact analysis already produced rather than searching again.
func ResolveObligations(ctx context.Context, g graph.Graph, plan workflow.Plan, impact *graph.Impact) []ObligationReport {
	if impact == nil {
		return nil
	}
	var out []ObligationReport
	actionable, _ := workflow.ActionableConsumers(impact)
	for _, consumer := range actionable {
		node := consumer.Node
		rep := ObligationReport{
			ID:     node.Path + "::" + node.Name,
			Symbol: plannerSymbol(node),
			Path:   node.Path,
		}

		// 1. Exact resolution: did the plan already account for it? This is
		//    the same rule ValidateObligations applies, asked here so the
		//    report says which obligations were fine.
		if accountedFor(plan, node) {
			rep.Status = ObligationResolved
			rep.Attempts = append(rep.Attempts, "matched an obligation in the plan")
			out = append(out, rep)
			continue
		}
		if why := whyNotAccounted(plan, node); why != "" {
			rep.Refused = why
			rep.Attempts = append(rep.Attempts, "the plan's obligation for it does not count: "+why)
		} else {
			rep.Attempts = append(rep.Attempts, "no obligation in the plan names this consumer")
		}

		// 2. Deterministic evidence: what the repository can say about it.
		//    A consumer the graph can describe is one a planner can be asked
		//    about again; a consumer it cannot is one nobody can act on.
		rep.Evidence = consumerEvidence(ctx, g, node)
		if len(rep.Evidence) > 0 {
			rep.Status = ObligationSearchable
			rep.Attempts = append(rep.Attempts,
				fmt.Sprintf("graph supplied %d piece(s) of evidence", len(rep.Evidence)))
		} else {
			rep.Status = ObligationUnresolvable
			rep.Attempts = append(rep.Attempts, "the graph has no further evidence about it")
		}
		out = append(out, rep)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// accountedFor is ValidateObligations' rule, asked about one consumer.
func accountedFor(plan workflow.Plan, node graph.Node) bool {
	return workflow.Plan{
		RootCause: plan.RootCause, Files: plan.Files, Symbols: plan.Symbols,
		Tests: plan.Tests, Contracts: plan.Contracts, WriteAllowlist: plan.WriteAllowlist,
		Risks: plan.Risks, Obligations: plan.Obligations,
	}.ValidateObligations(&graph.Impact{Consumers: []graph.Consumer{{Node: node}}}) == nil
}

// whyNotAccounted explains why an obligation the plan wrote for this consumer
// does not discharge it, or returns "" when the plan wrote none.
//
// The recorded plan named Service.Place exactly, resolved it as an edit, and
// left orders.go out of write_allowlist. The correction only repeated "add
// Service.Place", so the planner, which had added it, had nothing to fix and
// guessed — and its guess invented a symbol that ended the task.
func whyNotAccounted(plan workflow.Plan, node graph.Node) string {
	for _, o := range plan.Obligations {
		if o.Path != node.Path || !workflow.NamesConsumer(o.Symbol, node) {
			continue
		}
		switch {
		case !o.Resolution.Valid() && o.Resolution.Action == workflow.ActionNoChange:
			return "it is resolved as no_change_needed without a reason; give the concrete " +
				"compatibility reason it keeps working"
		case !o.Resolution.Valid():
			return fmt.Sprintf("its resolution %q is not edit or no_change_needed", o.Resolution.Action)
		case o.Resolution.Action == workflow.ActionEdit && !policy.Covers(plan.WriteAllowlist, o.Path):
			return fmt.Sprintf("it is resolved as edit, but %s is not in write_allowlist; add the file "+
				"there, or resolve it as no_change_needed with the reason it keeps working", o.Path)
		}
	}
	for _, o := range plan.Obligations {
		if o.Path != node.Path && workflow.NamesConsumer(o.Symbol, node) {
			return fmt.Sprintf("it gives the path %s, but this declaration is in %s", o.Path, node.Path)
		}
	}
	return ""
}

// consumerEvidence describes a consumer in the terms the repository uses.
//
// One bounded neighbour query, not a traversal: what this declaration is,
// where it lives, and what else in the repository shares its name. That is
// enough for a planner to write an obligation about it, which is all this
// stage is for.
func consumerEvidence(ctx context.Context, g graph.Graph, node graph.Node) []string {
	ev := []string{fmt.Sprintf("%s is a %s declared in %s at line %d",
		node.Name, node.Kind, node.Path, node.StartLine)}
	if node.Signature != "" {
		ev = append(ev, "signature: "+strings.TrimSpace(node.Signature))
	}
	if g == nil {
		return ev
	}
	others, err := g.NodesByName(ctx, node.Name, nil, maxObligationEvidence)
	if err == nil {
		var where []string
		for _, n := range others {
			if n.ID == node.ID || n.Path == "" {
				continue
			}
			where = append(where, n.Path)
		}
		sort.Strings(where)
		if len(where) > 0 {
			if len(where) > maxObligationEvidence {
				where = where[:maxObligationEvidence]
			}
			ev = append(ev, "also declared in: "+strings.Join(where, ", "))
		}
	}
	return ev
}

// plannerSymbol is the name a correction asks the planner to write back.
//
// A method is named with its receiver. Its short name alone is ambiguous
// whenever two types in a file share a method name, and the recorded
// correction listed "symbol `Discount`" twice for one file — which the
// planner, told that "each one below must appear", answered by replacing a
// plan that had resolved 33 consumers with one that named only those two.
// workflow.NamesConsumer accepts the receiver form, so it round-trips.
func plannerSymbol(node graph.Node) string {
	if node.Kind != graph.KindMethod || node.FQN == "" {
		return node.Name
	}
	tail := node.FQN[strings.LastIndex(node.FQN, "/")+1:]
	parts := strings.Split(tail, ".")
	if len(parts) < 3 || parts[len(parts)-1] != node.Name {
		return node.Name
	}
	return parts[len(parts)-2] + "." + node.Name
}

// ObligationCorrection renders the open obligations as an instruction.
//
// It names the consumer the way a person would — the short symbol and its
// file — because the fully-qualified form a semantic indexer produces is not
// something anyone can type back.
func ObligationCorrection(reports []ObligationReport) string {
	var b strings.Builder
	b.WriteString("The plan does not say what happens to every consumer this change affects. " +
		"Keep every obligation the previous plan already had, and add each one below to " +
		"`obligations` with a resolution of either `edit` (and its file in write_allowlist) " +
		"or `no change needed: <concrete compatibility reason>`.\n\n")
	for _, r := range reports {
		if r.Status == ObligationResolved {
			continue
		}
		fmt.Fprintf(&b, "- symbol `%s` in `%s`\n", r.Symbol, r.Path)
		if r.Refused != "" {
			fmt.Fprintf(&b, "    The plan has an obligation for it that does not count: %s\n", r.Refused)
		}
		for _, e := range r.Evidence {
			fmt.Fprintf(&b, "    %s\n", e)
		}
	}
	b.WriteString("\nUse exactly the symbol and path shown for each obligation.")
	return b.String()
}

// OpenObligations reports whether any obligation still refuses the plan.
func OpenObligations(reports []ObligationReport) (open int, unresolvable int) {
	for _, r := range reports {
		switch r.Status {
		case ObligationSearchable:
			open++
		case ObligationUnresolvable:
			open++
			unresolvable++
		case ObligationResolved:
		}
	}
	return open, unresolvable
}

// obligationImpact is the impact a plan has to answer for: the consumers of
// what the plan says it changes.
//
// IMPACT is computed from LOCALIZE's symbols, which name everything that
// looked relevant — on the recorded task, domain.Order, inventory.Service and
// store.Stock alongside the one method that was wrong. Requiring a
// disposition for every consumer of those asked the planner about 45
// declarations, from main to every HTTP handler, for a fix inside
// Reconciler.RunOnce; its answer outgrew the output limit and the plan was
// refused. The plan's own symbols are the change, so their consumers are the
// obligations. That report stays in the planner's evidence as information.
//
// When the plan's symbols resolve to nothing, the localized impact stands: a
// plan cannot escape its obligations by naming nothing the graph knows. The
// post-edit check on changed exported signatures is separate and unaffected.
//
// Only the declarations in files the plan may write count as changed: a plan
// cannot change what it cannot write. The recorded plan listed
// Service.Release, the inventory method its fix calls, beside the
// Reconciler.RunOnce it edits, and every caller of Release became an
// obligation of a change confined to the worker package.
//
// Of that impact, only direct callers are owed: consumers one hop away that
// call a changed function or method. The recorded plan added a field to
// domain.Order, and every declaration that accepts, returns or embeds an Order
// — pricing, shipping, notify, the store — became an obligation to justify,
// none of them affected, until the plan outgrew the output limit. A breaking
// change to a type or a signature is enforced after EDIT by the
// exported-signature check, which reads the actual change; the wider impact
// stays in the planner's evidence as information.
func (r *Runner) obligationImpact(ctx context.Context, plan workflow.Plan, localized *graph.Impact) *graph.Impact {
	return directCallers(r.changedImpact(ctx, plan, localized))
}

// directCallers keeps the consumers that call a changed declaration directly.
func directCallers(imp *graph.Impact) *graph.Impact {
	if imp == nil {
		return nil
	}
	out := *imp
	out.Consumers = nil
	for _, c := range imp.Consumers {
		if c.Depth == 1 && c.Via == graph.EdgeCalls {
			out.Consumers = append(out.Consumers, c)
		}
	}
	return &out
}

// changedImpact is the impact of the declarations the plan changes, falling
// back to the localized impact when the plan names none the graph knows.
func (r *Runner) changedImpact(ctx context.Context, plan workflow.Plan, localized *graph.Impact) *graph.Impact {
	if len(plan.Symbols) == 0 || r.Retriever == nil || r.Retriever.Graph() == nil {
		return localized
	}
	var changed []string
	for _, sym := range plan.Symbols {
		nodes, err := graph.Resolve(ctx, r.Retriever.Graph(), sym, 10)
		if err != nil {
			return localized
		}
		for _, n := range nodes {
			if n.Path != "" && policy.Covers(plan.WriteAllowlist, n.Path) {
				changed = append(changed, n.Path+"::"+n.Name)
			}
		}
	}
	if len(changed) == 0 {
		return localized
	}
	pkt, err := r.Retriever.Build(ctx, retrieval.Request{ImpactOf: changed})
	if err != nil || pkt == nil || pkt.Impact == nil || len(pkt.Impact.Changed) == 0 {
		return localized
	}
	return pkt.Impact
}
