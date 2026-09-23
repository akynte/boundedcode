package eval

import (
	"fmt"
	"sort"
	"strings"
)

// What each reranking arm actually sees.
//
// This exists to forbid one sentence. If the Jev arm receives signatures and
// the local arm receives only paths, then "Jev is a better reranker" is not
// a conclusion the experiment supports — it supports "a system that sends
// signatures to Jev ranks better than a system that embeds paths locally",
// which is a different and much weaker claim about models.
//
// The distinction is between a *system-level* result, which compares the two
// configurations somebody could actually deploy, and a *model-level* result,
// which compares the relevance signals on equal evidence. Both are worth
// having. Confusing them is how a benchmark overclaims, so the report states
// which one it is entitled to and why.

// CandidateEvidenceKind is one kind of information a candidate can carry.
//
// Named at length to stay clear of eval.Evidence, which is the qualification
// bundle and an entirely different thing.
type CandidateEvidenceKind string

const (
	CandPath        CandidateEvidenceKind = "path"
	CandSymbol      CandidateEvidenceKind = "symbol"
	CandKind        CandidateEvidenceKind = "kind"
	CandLines       CandidateEvidenceKind = "lines"
	CandSignature   CandidateEvidenceKind = "signature"
	CandExcerpt     CandidateEvidenceKind = "excerpt"
	CandFullSource  CandidateEvidenceKind = "full source"
	CandOriginScore CandidateEvidenceKind = "origin score"
	CandGraphMeta   CandidateEvidenceKind = "graph metadata"
)

// EvidenceKinds lists them in report order.
func CandidateEvidenceKinds() []CandidateEvidenceKind {
	return []CandidateEvidenceKind{
		CandPath, CandSymbol, CandKind, CandLines,
		CandSignature, CandExcerpt, CandFullSource,
		CandOriginScore, CandGraphMeta,
	}
}

// ArmEvidence is what one arm's reranker receives per candidate.
type ArmEvidence struct {
	Arm  string                         `json:"arm"`
	Sees map[CandidateEvidenceKind]bool `json:"sees"`
	Note string                         `json:"note,omitempty"`
}

// EvidenceMatrix is the comparison across arms.
type EvidenceMatrix struct {
	Arms []ArmEvidence `json:"arms"`
	// Comparable lists arm pairs that see the same evidence, and therefore
	// support a model-level claim.
	Comparable [][2]string `json:"model_level_comparable,omitempty"`
	// Unequal lists pairs that do not, with what differs.
	Unequal []EvidenceGap `json:"system_level_only,omitempty"`
}

// EvidenceGap is what one arm sees that another does not.
type EvidenceGap struct {
	A         string                  `json:"a"`
	B         string                  `json:"b"`
	OnlyA     []CandidateEvidenceKind `json:"only_a,omitempty"`
	OnlyB     []CandidateEvidenceKind `json:"only_b,omitempty"`
	Permitted string                  `json:"permitted_claim"`
}

// DescribeEvidence reports what each arm's reranker is handed.
//
// The facts come from the code that builds the requests: rerankBatch for the
// judged arm, renderCandidate for the local one. Both are named here so that
// a change to either shows up as a change to this table rather than silently
// invalidating a published comparison.
func DescribeEvidence(arms []Arm, judgmentRedact string) EvidenceMatrix {
	m := EvidenceMatrix{}
	for _, a := range arms {
		e := ArmEvidence{Arm: a.Name, Sees: map[CandidateEvidenceKind]bool{}}
		switch a.Rerank {
		case RerankNone:
			e.Note = "no reranker: the packet is ordered by bm25 inside the anchors and " +
				"inverse depth inside the expansion, which are the scores retrieval " +
				"already had"
			e.Sees[CandOriginScore] = true
		case RerankLocal:
			// retrieval.renderCandidate.
			e.Sees[CandPath] = true
			e.Sees[CandSymbol] = true
			e.Sees[CandKind] = true
			e.Sees[CandLines] = true
			e.Note = "embedding cosine over path, symbol, kind and line range, with " +
				"identifier spellings split into words; one local method, not a claim " +
				"about every possible local reranker"
		case RerankJudged:
			// retrieval.rerankBatch.
			e.Sees[CandPath] = true
			e.Sees[CandSymbol] = true
			e.Sees[CandKind] = true
			e.Sees[CandLines] = true
			if judgmentRedact == "repo_text" {
				e.Sees[CandSignature] = true
				e.Sees[CandExcerpt] = true
				e.Note = "redact: repo_text — signatures and short excerpts are sent too"
			} else {
				e.Note = "redact: strict — metadata only, no repository source"
			}
		}
		m.Arms = append(m.Arms, e)
	}

	for i := 0; i < len(m.Arms); i++ {
		for j := i + 1; j < len(m.Arms); j++ {
			a, b := m.Arms[i], m.Arms[j]
			// The baseline has no reranker, so it is never an
			// equal-information comparison in the model sense; the comparison
			// against it is always system-level, and legitimately so.
			if a.Sees[CandOriginScore] || b.Sees[CandOriginScore] {
				m.Unequal = append(m.Unequal, gapBetween(a, b,
					"system-level only: one arm has no reranker, so this compares "+
						"configurations rather than relevance signals"))
				continue
			}
			onlyA, onlyB := difference(a, b)
			if len(onlyA) == 0 && len(onlyB) == 0 {
				m.Comparable = append(m.Comparable, [2]string{a.Arm, b.Arm})
				continue
			}
			m.Unequal = append(m.Unequal, gapBetween(a, b,
				fmt.Sprintf("system-level only: %s sees evidence %s does not, so a "+
					"difference cannot be attributed to the relevance signal",
					richer(a, b, onlyA, onlyB), poorer(a, b, onlyA, onlyB))))
		}
	}
	return m
}

func gapBetween(a, b ArmEvidence, permitted string) EvidenceGap {
	onlyA, onlyB := difference(a, b)
	return EvidenceGap{A: a.Arm, B: b.Arm, OnlyA: onlyA, OnlyB: onlyB, Permitted: permitted}
}

func difference(a, b ArmEvidence) (onlyA, onlyB []CandidateEvidenceKind) {
	for _, kind := range CandidateEvidenceKinds() {
		switch {
		case a.Sees[kind] && !b.Sees[kind]:
			onlyA = append(onlyA, kind)
		case b.Sees[kind] && !a.Sees[kind]:
			onlyB = append(onlyB, kind)
		}
	}
	return onlyA, onlyB
}

func richer(a, b ArmEvidence, onlyA, onlyB []CandidateEvidenceKind) string {
	if len(onlyA) >= len(onlyB) {
		return a.Arm
	}
	return b.Arm
}

func poorer(a, b ArmEvidence, onlyA, onlyB []CandidateEvidenceKind) string {
	if len(onlyA) >= len(onlyB) {
		return b.Arm
	}
	return a.Arm
}

// ModelLevel reports whether a specific pair may support a claim about the
// relevance signals rather than about the systems around them.
func (m EvidenceMatrix) ModelLevel(a, b string) bool {
	for _, pair := range m.Comparable {
		if (pair[0] == a && pair[1] == b) || (pair[0] == b && pair[1] == a) {
			return true
		}
	}
	return false
}

// PermittedClaim is the sentence a reader is entitled to for one pair.
func (m EvidenceMatrix) PermittedClaim(a, b string) string {
	if m.ModelLevel(a, b) {
		return "model-level: both arms saw the same evidence, so a difference is " +
			"attributable to the relevance signal"
	}
	for _, gap := range m.Unequal {
		if (gap.A == a && gap.B == b) || (gap.A == b && gap.B == a) {
			return gap.Permitted
		}
	}
	return "not compared"
}

// Format renders the matrix as the table the brief asked for.
func (m EvidenceMatrix) Format() string {
	var b strings.Builder
	names := make([]string, 0, len(m.Arms))
	for _, a := range m.Arms {
		names = append(names, a.Arm)
	}
	b.WriteString("What each reranking arm receives per candidate\n\n")
	fmt.Fprintf(&b, "%-16s", "")
	for _, name := range names {
		fmt.Fprintf(&b, " %-24s", name)
	}
	b.WriteString("\n")
	for _, kind := range CandidateEvidenceKinds() {
		fmt.Fprintf(&b, "%-16s", kind)
		for _, arm := range m.Arms {
			mark := "—"
			if arm.Sees[kind] {
				mark = "yes"
			}
			fmt.Fprintf(&b, " %-24s", mark)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	for _, arm := range m.Arms {
		if arm.Note != "" {
			fmt.Fprintf(&b, "%s: %s\n", arm.Arm, arm.Note)
		}
	}
	if len(m.Comparable) > 0 {
		b.WriteString("\nEqual information, so a difference is attributable to the signal:\n")
		for _, pair := range m.Comparable {
			fmt.Fprintf(&b, "  %s vs %s — model-level claim permitted\n", pair[0], pair[1])
		}
	}
	if len(m.Unequal) > 0 {
		b.WriteString("\nUnequal information:\n")
		sort.Slice(m.Unequal, func(i, j int) bool { return m.Unequal[i].A < m.Unequal[j].A })
		for _, gap := range m.Unequal {
			fmt.Fprintf(&b, "  %s vs %s — %s\n", gap.A, gap.B, gap.Permitted)
		}
		b.WriteString("\nA conclusion of the form \"X is a better reranker\" is not available " +
			"for\nthose pairs. What the run supports is a statement about the two systems " +
			"as\nconfigured.\n")
	}
	return b.String()
}
