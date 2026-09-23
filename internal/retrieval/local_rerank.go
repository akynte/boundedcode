package retrieval

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/llm"
)

// A fully local semantic reranker, as the control arm for the judged one.
//
// The comparison that matters is not "does semantic reranking beat bm25". It
// is "does sending paths and symbol names to a third party beat doing the same
// job on this machine". Without this arm a positive result for the judged
// reranker would establish only the first, and would be quoted as if it
// established the second.
//
// It is embedding cosine similarity between the objective and a rendering of
// each candidate. That is a weaker method than a trained relevance model and
// it is meant to be: the point is a floor that costs no egress, not a rival
// implementation. If it turns out to be close, that is the finding.
//
// It implements the same all-or-nothing contract as the judged reranker, uses
// the same Slice.Relevance field, and is subject to the same eligibility
// filter — not because a local call discloses anything, but so the two arms
// see exactly the same candidate sets and a difference between them cannot be
// a difference in what they were allowed to look at.

// LocalRerankSource names this reranker in telemetry, so an artifact says
// which of the two produced a run's ordering.
const LocalRerankSource = "local_embedding"

// Embedder is the slice of llm.Provider a local reranker needs.
type Embedder interface {
	Embed(ctx context.Context, req llm.EmbedRequest) (*llm.EmbedResponse, error)
}

// LocalReranker scores candidates by embedding similarity to the objective.
type LocalReranker struct {
	Provider Embedder
	// Model names the embedding model, for the provider that wants one.
	Model string
	// Budget bounds the whole rerank, as the judged one is bounded. Zero uses
	// the judged default, so the two arms wait the same amount before giving
	// up.
	Budget time.Duration
}

// Available reports whether this reranker can run.
func (l *LocalReranker) Available() bool { return l != nil && l.Provider != nil }

// Rerank scores every candidate, or none.
//
// Like the judged reranker it returns no error: a local reranker that failed
// is a rerank that did not happen, and retrieval carries on with the
// deterministic ordering.
func (l *LocalReranker) Rerank(ctx context.Context, objective string, slices []Slice,
	logf func(string, ...any)) RerankResult {

	if logf == nil {
		logf = func(string, ...any) {}
	}
	res := RerankResult{Total: len(slices), Source: LocalRerankSource}
	switch {
	case !l.Available():
		res.SkipReason = SkipNoJudge
		return res
	case objective == "":
		res.SkipReason = SkipNoObjective
		return res
	case len(slices) == 0:
		res.SkipReason = SkipNoCandidates
		return res
	}
	res.Attempted = true

	// The same eligibility rule as the judged arm, for comparability rather
	// than for secrecy. An arm that saw candidates the other could not would
	// be measuring a different retrieval problem.
	for _, s := range slices {
		if !externalizable(s) {
			res.SkipReason = SkipIneligible
			return res
		}
	}

	budget := l.Budget
	if budget <= 0 {
		budget = DefaultRerankBudget
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	input := make([]string, 0, len(slices)+1)
	input = append(input, objective)
	for _, s := range slices {
		input = append(input, renderCandidate(s))
	}

	out, err := l.Provider.Embed(ctx, llm.EmbedRequest{Model: l.Model, Input: input})
	res.Latency = time.Since(started)
	res.Requests = 1
	if err != nil {
		res.SkipReason = SkipUnavailable
		logf("local rerank: %v", err)
		return res
	}
	if len(out.Vectors) != len(input) {
		res.SkipReason = SkipPartialAnswer
		logf("local rerank: %d vectors for %d inputs; keeping the deterministic order",
			len(out.Vectors), len(input))
		return res
	}

	query := out.Vectors[0]
	scores := make([]float64, len(slices))
	for i := range slices {
		// Cosine similarity runs -1..1; the judged arm's scale is a
		// probability. Mapping to 0..1 keeps RelevanceFloor meaning the same
		// thing in both arms, which is what lets one budget-fill rule serve
		// both. It does not make the two numbers interchangeable, and nothing
		// compares them across arms.
		scores[i] = (cosine(query, out.Vectors[i+1]) + 1) / 2
	}
	for i := range slices {
		slices[i].Relevance = scores[i]
		slices[i].Judged = true
	}
	res.Judged = len(slices)
	res.Applied = true
	return res
}

// renderCandidate is what the embedding sees.
//
// Deliberately the same fields the judged arm sends under strict redaction —
// path, symbol, kind and line range — so that the two arms are asked about
// the same evidence and a difference between them is attributable to the
// relevance signal rather than to one having been shown more. `bcode eval
// parity` prints the comparison and will report the pair as system-level
// only if this drifts.
//
// The line range is here for parity rather than because an embedding can use
// it well; a text embedder will make little of "41-68". Including it costs a
// few tokens and buys the stronger claim.
func renderCandidate(s Slice) string {
	var b strings.Builder
	b.WriteString(s.Path)
	if s.Symbol != "" {
		b.WriteString(" ")
		b.WriteString(s.Symbol)
	}
	if s.Kind != "" {
		b.WriteString(" (")
		b.WriteString(string(s.Kind))
		b.WriteString(")")
	}
	if s.StartLine > 0 && s.EndLine >= s.StartLine {
		fmt.Fprintf(&b, " lines %d-%d", s.StartLine, s.EndLine)
	}
	// Identifier spellings are the signal here: accountLimit and account_limit
	// embed poorly as single tokens, and splitting them is the cheapest thing
	// that makes a path comparable to an English objective.
	if split := splitIdentifiers(s.Path + " " + s.Symbol); split != "" {
		b.WriteString(" — ")
		b.WriteString(split)
	}
	return b.String()
}

func splitIdentifiers(s string) string {
	var out []string
	var word strings.Builder
	flush := func() {
		if word.Len() > 1 {
			out = append(out, strings.ToLower(word.String()))
		}
		word.Reset()
	}
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			flush()
			word.WriteRune(r)
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			word.WriteRune(r)
		default:
			flush()
		}
	}
	flush()
	sort.Strings(out)
	return strings.Join(dedupe(out), " ")
}

func dedupe(in []string) []string {
	out := in[:0]
	var last string
	for _, s := range in {
		if s != last {
			out = append(out, s)
		}
		last = s
	}
	return out
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// Describe reports the reranker for a log line.
func (l *LocalReranker) Describe() string {
	if !l.Available() {
		return "local reranker: none"
	}
	return fmt.Sprintf("local reranker: embeddings (%s)", l.Model)
}
