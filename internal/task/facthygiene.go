package task

// Confirmed-fact pruning: a judgment site over which of the compaction
// card's confirmed facts stay on it (design.md mechanism M8, third
// sub-site).
//
// contextpack.Card keeps every fact a tool ever confirmed, across every
// phase boundary this task has crossed. A fact that was central three
// attempts ago and has nothing to do with what the task is doing now still
// takes up space on the card the model reads at every phase boundary, and
// the card's own contract — a short, trustworthy rebuild from the ledger —
// degrades exactly the way a growing context always does.

import (
	"context"
	"strconv"
	"strings"

	"github.com/akynte/boundedcode/internal/contextpack"
	"github.com/akynte/boundedcode/internal/judgment"
)

// FactRelevanceSite names this judgment site in judgment.yaml's `sites` map.
const FactRelevanceSite = "fact_relevance"

func init() {
	judgment.RegisterSite(judgment.SiteInfo{
		Name:            FactRelevanceSite,
		Description:     "judges whether each of the compaction card's confirmed facts still bears on the working hypothesis or an open failure",
		Mechanism:       "M8",
		Version:         "1",
		MaxEffect:       judgment.TierOrdering,
		Redaction:       judgment.RedactStrict,
		Outcome:         "",
		EffectThreshold: 0,
	})
}

// DefaultFactRelevanceFloor is the probability below which a confirmed fact
// is dropped from the rendered card at ordering tier.
const DefaultFactRelevanceFloor = 0.3

// FactRelevanceResult is one pruning pass's outcome.
type FactRelevanceResult struct {
	Total, Judged      int
	Attempted, Applied bool
	Tier               judgment.Tier
	SkipReason         string
	// Relevant maps a fact's index in the slice it was given to its judged
	// relevance. An index absent from the map was not answered.
	Relevant                            map[int]float64
	Requests, InputTokens, OutputTokens int
}

func factRelevanceProposition(id string) string {
	return "Considering the fact with id `" + id + "` in `facts`, against the current " +
		"hypothesis in `hypothesis` and the open failures in `open_failures`: does this fact " +
		"still bear on either of them?"
}

// CheckFactRelevance judges every fact in facts against hypothesis and
// openFailures.
//
// It never returns an error: a check that failed is a check that did not
// happen, matching every other judgment call site in this codebase.
func CheckFactRelevance(ctx context.Context, j judgment.Judge, hypothesis string, openFailures []string,
	facts []contextpack.Fact, logf func(string, ...any)) FactRelevanceResult {

	if logf == nil {
		logf = func(string, ...any) {}
	}
	res := FactRelevanceResult{Total: len(facts), Tier: judgment.SiteTier(j, FactRelevanceSite)}

	switch {
	case j == nil || !j.Available():
		res.SkipReason = "no_judge"
		return res
	case len(facts) == 0:
		res.SkipReason = "no_facts"
		return res
	case strings.TrimSpace(hypothesis) == "" && len(openFailures) == 0:
		// Nothing to judge relevance against; see CheckHypothesis's
		// identical reasoning for the same case.
		res.SkipReason = "no_context"
		return res
	}
	res.Attempted = true

	st := judgment.NewState(judgment.RedactStrict)
	if hypothesis != "" {
		if err := st.TrustedFact("hypothesis", hypothesis); err != nil {
			logf("fact relevance: %v", err)
			return res
		}
	}
	if err := st.TrustedFact("open_failures", strings.Join(openFailures, "; ")); err != nil {
		logf("fact relevance: %v", err)
		return res
	}

	ids := make([]string, 0, len(facts))
	// origIdx[n] is the index into facts that ids[n] describes. A fact whose
	// TrustedFact call refused (over the scalar cap, or a stray credential)
	// is skipped, so ids and facts diverge and an answer must be matched
	// back through this rather than through facts' own index — the same
	// pattern workflow.checkIntegrityBatch's `used` slice uses.
	origIdx := make([]int, 0, len(facts))
	qs := make(map[string]judgment.Question, len(facts))
	for n, f := range facts {
		id := "fact" + strconv.Itoa(n)
		if err := st.TrustedFact(id, f.Fact); err != nil {
			logf("fact relevance: %v", err)
			continue
		}
		ids = append(ids, id)
		origIdx = append(origIdx, n)
		qs[id] = judgment.Noul(factRelevanceProposition(id))
	}
	if len(ids) == 0 {
		res.SkipReason = "no_eligible_facts"
		return res
	}

	answers, note := judgment.AskAll(ctx, j, st, qs)
	res.Requests += note.Usage.Requests
	res.InputTokens += note.Usage.InputTokens
	res.OutputTokens += note.Usage.OutputTokens

	res.Relevant = map[int]float64{}
	for n, id := range ids {
		if a := answers[id]; a.Answered {
			res.Judged++
			res.Relevant[origIdx[n]] = a.Noul
		}
	}
	res.Applied = res.Judged > 0
	return res
}
