package task

// Hypothesis check: a judgment site over the compaction card's working
// hypothesis (design.md mechanism M3, the card-audit slice).
//
// contextpack.Card's own comment says a compaction is assembled "from the
// ledger... never from the conversation", because "a summary of chat is a
// model's account of its own work". Hypothesis is the one field on the card
// that is exactly that: the model's own belief about the root cause, carried
// forward unlabelled next to facts a tool actually confirmed. This asks
// whether the belief is still consistent with the evidence the card already
// carries, using nothing the card does not already have — the question
// mirrors what a reader of the rendered card sees, never more.

import (
	"context"
	"fmt"
	"strings"

	"github.com/akynte/boundedcode/internal/contextpack"
	"github.com/akynte/boundedcode/internal/judgment"
)

// HypothesisSite names this judgment site in judgment.yaml's `sites` map.
const HypothesisSite = "hypothesis_check"

func init() {
	judgment.RegisterSite(judgment.SiteInfo{
		Name:            HypothesisSite,
		Description:     "judges the compaction card's working hypothesis against the facts and open failures already on the card",
		Mechanism:       "M3",
		Version:         "1",
		MaxEffect:       judgment.TierOrdering,
		Redaction:       judgment.RedactStrict,
		Outcome:         "the task's terminal state",
		EffectThreshold: 0,
	})
}

// HypothesisVerdict is the Choice this site asks.
type HypothesisVerdict string

const (
	HypothesisVerdictSupported    HypothesisVerdict = "supported"
	HypothesisVerdictContradicted HypothesisVerdict = "contradicted"
	HypothesisVerdictUnsupported  HypothesisVerdict = "unsupported"
)

var hypothesisCriteria = map[string]string{
	string(HypothesisVerdictSupported): "The evidence is consistent with the hypothesis; " +
		"nothing in it contradicts what the hypothesis claims.",
	string(HypothesisVerdictContradicted): "The evidence directly contradicts a specific claim " +
		"the hypothesis makes.",
	string(HypothesisVerdictUnsupported): "The evidence does not speak to the hypothesis either " +
		"way — it is neither confirmed nor contradicted by what is here.",
}

// HypothesisResult is one check's outcome.
type HypothesisResult struct {
	Attempted, Applied                  bool
	Tier                                judgment.Tier
	SkipReason                          string
	Verdict                             HypothesisVerdict
	Confidence                          float64
	Answered                            bool
	Requests, InputTokens, OutputTokens int
}

func hypothesisProposition() string {
	return "Considering `hypothesis` against the evidence in `confirmed_facts` and the open " +
		"failures in `open_failures`: which of these best describes the relationship?"
}

// CheckHypothesis judges hypothesis against facts and openFailures, both
// rendered exactly as the card already carries them.
//
// It never returns an error: a check that failed is a check that did not
// happen, matching every other judgment call site in this codebase.
func CheckHypothesis(ctx context.Context, j judgment.Judge, hypothesis string, facts []contextpack.Fact,
	openFailures []string, logf func(string, ...any)) HypothesisResult {

	if logf == nil {
		logf = func(string, ...any) {}
	}
	res := HypothesisResult{Tier: judgment.SiteTier(j, HypothesisSite)}

	switch {
	case j == nil || !j.Available():
		res.SkipReason = "no_judge"
		return res
	case strings.TrimSpace(hypothesis) == "":
		res.SkipReason = "no_hypothesis"
		return res
	case len(facts) == 0 && len(openFailures) == 0:
		// Nothing to check a claim against is not evidence the claim is
		// wrong, or even unsupported in the sense this site means it — it
		// is simply too early in the task to ask.
		res.SkipReason = "no_evidence"
		return res
	}
	res.Attempted = true

	st := judgment.NewState(judgment.RedactStrict)
	if err := st.ClaimText("hypothesis", hypothesis); err != nil {
		logf("hypothesis: %v", err)
		return res
	}
	var factText strings.Builder
	for _, f := range facts {
		fmt.Fprintf(&factText, "- %s [%s]\n", f.Fact, f.Evidence)
	}
	if err := st.TrustedFact("confirmed_facts", strings.TrimSpace(factText.String())); err != nil {
		logf("hypothesis: %v", err)
		return res
	}
	if err := st.TrustedFact("open_failures", strings.Join(openFailures, "; ")); err != nil {
		logf("hypothesis: %v", err)
		return res
	}

	answers, note := judgment.AskAll(ctx, j, st, map[string]judgment.Question{
		"verdict": judgment.Choice(hypothesisProposition(), hypothesisCriteria),
	})
	res.Requests += note.Usage.Requests
	res.InputTokens += note.Usage.InputTokens
	res.OutputTokens += note.Usage.OutputTokens

	if a := answers["verdict"]; a.Answered {
		res.Answered = true
		res.Applied = true
		res.Verdict = HypothesisVerdict(a.Choice)
		res.Confidence = a.Confidence
	}
	return res
}
