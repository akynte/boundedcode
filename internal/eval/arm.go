package eval

import (
	"fmt"
	"sort"
	"strings"
)

// Arm is one configuration under comparison.
//
// The set below is chosen by what each arm isolates, not by what is convenient
// to run. In particular Unsupervised exists because it is the only comparison
// that says anything about *this project*: if the supervised system is not
// clearly better than the same model driven directly, the harness is not
// earning its complexity, and no amount of comparison against a frontier agent
// changes that.
type Arm struct {
	// Name identifies the arm in results.
	Name string
	// Description says what this arm isolates, and appears in the published
	// report so a reader knows what was compared.
	Description string
	// Supervised turns on the full pipeline: retrieval, the journal,
	// verification and the completion contract.
	Supervised bool
	// Graph turns on graph expansion and impact analysis inside retrieval.
	// Turning it off while leaving everything else on is the ablation §3.1
	// requires: "measure the graph's contribution with an ablation, not by
	// assumption".
	Graph bool
	// Verification turns on the compiler-and-test correction loop. Off means
	// the model gets one pass with no feedback.
	Verification bool
	// Rerank names the reranker this arm uses over retrieval candidates.
	//
	// Three values, because the experiment has three levels and collapsing
	// them to a boolean would make the interesting comparison unstateable. A
	// deterministic baseline against Jev says only that semantic reranking
	// helps; it takes the local arm in between to say whether sending
	// anything to a third party is what bought the difference.
	Rerank RerankKind
	// Role selects which provider role the arm routes to, so a frontier arm
	// can use a different model without changing anything else.
	Role string
}

// RerankKind names a reranking treatment.
type RerankKind string

const (
	// RerankNone: bm25 inside the anchors, inverse depth inside the
	// expansion, no cross-origin ordering.
	RerankNone RerankKind = ""
	// RerankLocal: embedding similarity computed on this machine.
	RerankLocal RerankKind = "local"
	// RerankJudged: TypeSafe relevance probabilities.
	RerankJudged RerankKind = "judged"
)

// Arms returns the comparison set of §10.2 plus the ablation of §3.1.
func Arms() []Arm {
	return []Arm{
		{
			Name: "unsupervised",
			Description: "The same local model given the objective and the worktree, with no " +
				"retrieval, no graph and no verification loop. The baseline that says whether " +
				"the harness earns its complexity.",
			Supervised: false, Graph: false, Verification: false, Role: "coding",
		},
		{
			Name: "supervised",
			Description: "The full system: retrieval, graph expansion, impact analysis, the " +
				"verification loop and the completion contract.",
			Supervised: true, Graph: true, Verification: true, Role: "coding",
		},
		{
			Name: "supervised-no-graph",
			Description: "The full system with graph expansion and impact analysis off, and " +
				"lexical retrieval left on. Isolates what the graph contributes.",
			Supervised: true, Graph: false, Verification: true, Role: "coding",
		},
		{
			Name: "supervised-no-verification",
			Description: "The full system with the compiler-and-test correction loop off. " +
				"Isolates what the feedback loop contributes, which §10.1 calls the core.",
			Supervised: true, Graph: true, Verification: false, Role: "coding",
		},
		{
			Name: "supervised-no-graph-no-verify",
			Description: "The supervised worktree and lexical retrieval only: no graph, no " +
				"verification loop. The ladder's second rung, so the supervisor's scaffolding " +
				"can be separated from the two components built on top of it.",
			Supervised: true, Graph: false, Verification: false, Role: "coding",
		},
		{
			Name: "supervised-rerank",
			Description: "The full system with judged reranking of retrieval candidates. " +
				"Isolates what one comparable relevance scale contributes over bm25 inside " +
				"the anchors and inverse depth inside the expansion.",
			Supervised: true, Graph: true, Verification: true, Rerank: RerankJudged, Role: "coding",
		},
		{
			Name: "supervised-rerank-local",
			Description: "The full system with a fully local embedding reranker over the same " +
				"candidates. The control for the judged arm: without it, a win for reranking " +
				"cannot be attributed to the external service rather than to semantic " +
				"reranking in general.",
			Supervised: true, Graph: true, Verification: true, Rerank: RerankLocal, Role: "coding",
		},
		{
			Name: "frontier",
			Description: "A hosted model through the supervised pipeline, on a non-sensitive " +
				"task set only. Calibrates the task set's difficulty; it is not the comparison " +
				"this project is judged on.",
			Supervised: true, Graph: true, Verification: true, Role: "verification",
		},
	}
}

// ArmByName finds an arm.
func ArmByName(name string) (Arm, error) {
	for _, a := range Arms() {
		if a.Name == name {
			return a, nil
		}
	}
	var names []string
	for _, a := range Arms() {
		names = append(names, a.Name)
	}
	sort.Strings(names)
	return Arm{}, fmt.Errorf("eval: no arm %q (one of %s)", name, strings.Join(names, ", "))
}

// Comparison names two arms whose difference answers a specific question.
type Comparison struct {
	Question string
	Baseline string
	Variant  string
	// WhatItIsolates says what differs between the two, so a reader knows
	// what the delta can and cannot be attributed to.
	WhatItIsolates string
}

// Comparisons are the questions the arm set is designed to answer. Stating
// them up front is what stops a result being reinterpreted after the fact to
// suit whatever it turned out to show.
func Comparisons() []Comparison {
	return []Comparison{
		{
			Question:       "Does the harness beat the same model driven directly?",
			Baseline:       "unsupervised",
			Variant:        "supervised",
			WhatItIsolates: "the whole pipeline: retrieval, the graph, and the verification loop together",
		},
		{
			Question:       "Does the code graph contribute anything beyond lexical search?",
			Baseline:       "supervised-no-graph",
			Variant:        "supervised",
			WhatItIsolates: "graph expansion and impact analysis, with everything else held constant",
		},
		{
			Question:       "Does semantic reranking beat ranking by origin at all?",
			Baseline:       "supervised",
			Variant:        "supervised-rerank-local",
			WhatItIsolates: "one semantic relevance signal per candidate, computed locally",
		},
		{
			Question: "Does the external judge beat a local semantic reranker?",
			Baseline: "supervised-rerank-local",
			Variant:  "supervised-rerank",
			WhatItIsolates: "the quality of the relevance signal, and nothing else: both arms " +
				"rerank the same candidates under the same budget rule. This is the only " +
				"comparison that speaks to whether the egress is justified.",
		},
		{
			Question:       "Does the verification loop contribute?",
			Baseline:       "supervised-no-verification",
			Variant:        "supervised",
			WhatItIsolates: "the compiler-and-test correction signal, with everything else held constant",
		},
		{
			Question:       "How hard is this task set?",
			Baseline:       "supervised",
			Variant:        "frontier",
			WhatItIsolates: "the model, with the pipeline held constant. Calibration, not a verdict on the project.",
		},
	}
}
