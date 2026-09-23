package task

// Obligation-reason plausibility: a judgment site over waivers the plan
// already wrote.
//
// workflow.Plan.ValidateObligations already requires a reason string for
// every consumer a plan marks no_change_needed — the plan schema calls it
// "required for no_change_needed: the concrete compatibility reason it keeps
// working". Nothing before this checked whether that reason was actually
// true. signatureObligations (phases.go) is the deterministic backstop: after
// an edit, if a changed signature has a caller no changed file accounts for,
// it forces feedback regardless of what any obligation said. What that check
// cannot see is a waiver that is wrong for a reason no signature diff would
// show — a vague or generic reason that does not address the specific
// consumer, written before any edit exists to compare against. That gap is
// what this checks, at plan-validation time, before an attempt is spent
// building on a plan whose waiver will not hold up.
//
// Like every judgment in this system, this never waives or rejects an
// obligation itself. It produces findings for a caller to use however
// design.md's tier system permits (see judgment.Tier and
// ObligationReasonSite).

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/akynte/boundedcode/internal/graph"
	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/workflow"
)

// ObligationReasonSite names this judgment site in judgment.yaml's `sites`
// map.
const ObligationReasonSite = "obligation_reason"

func init() {
	judgment.RegisterSite(judgment.SiteInfo{
		Name:            ObligationReasonSite,
		Description:     "judges whether a plan's stated reason for waiving an obligation actually addresses the consumer it names",
		Mechanism:       "M3",
		Version:         "1",
		MaxEffect:       judgment.TierRouting,
		Redaction:       judgment.RedactStrict,
		Outcome:         "the task's terminal state",
		EffectThreshold: 0,
	})
}

// DefaultObligationReasonFloor is the plausibility probability below which a
// waiver is reported. It is deliberately low: a Noul near 0.5 is the judge
// finding the question genuinely unclear, which is not the same as finding
// the reason implausible, and design.md's jaggedness notes warn against
// reading a Noul's midpoint as anything but "unsure". Only a reason the judge
// leans toward calling weak is reported.
const DefaultObligationReasonFloor = 0.35

// ObligationReasonFinding is one no_change_needed waiver a judgment found
// weak, given what the consumer is and what the plan's reason says about it.
type ObligationReasonFinding struct {
	Symbol    string  `json:"symbol"`
	Path      string  `json:"path"`
	Reason    string  `json:"reason"`
	Plausible float64 `json:"plausible"`
	Detail    string  `json:"detail"`
}

// ObligationReasonResult is one check's outcome, in the same shape
// workflow.IntegrityResult and retrieval.RerankResult already use.
type ObligationReasonResult struct {
	// Total is every no_change_needed obligation this plan declares that also
	// matched a consumer in impact. Waivers naming a consumer impact does not
	// report are not counted — nothing here re-derives what the impact
	// analysis already decided.
	Total                               int
	Attempted, Applied                  bool
	Tier                                judgment.Tier
	SkipReason                          string
	Judged                              int
	Findings                            []ObligationReasonFinding
	Requests, InputTokens, OutputTokens int
}

// plausibleProposition names the consumer and its reason field by id inside
// the instructions, so one batched request can carry many waivers without the
// map key (never sent to the model) doing any of the identifying work.
func plausibleProposition(id string) string {
	return "The plan changes something `objective` describes, and for the consumer with " +
		"id `" + id + "` in `consumers`, declares that it needs no change for the reason " +
		"given in `reason_" + id + "`. Given what that consumer is — its kind, name and " +
		"location — is `reason_" + id + "` a concrete, specific explanation of why this " +
		"particular consumer keeps working unchanged, as opposed to a generic statement " +
		"that does not actually address this consumer?"
}

// ObligationReasonCorrection renders findings as an instruction, the same way
// ObligationCorrection does for obligations the deterministic pass left open
// — a planner corrected by one is corrected by the same kind of message from
// the other.
func ObligationReasonCorrection(findings []ObligationReasonFinding) string {
	var b strings.Builder
	b.WriteString("One or more `no_change_needed` waivers do not look supported by what the " +
		"named consumer actually is. Either change the resolution to `edit` (and add the " +
		"file to write_allowlist), or restate the reason so it concretely explains why this " +
		"specific consumer keeps working — not a generic statement that would apply to any " +
		"consumer.\n\n")
	for _, f := range findings {
		fmt.Fprintf(&b, "- symbol `%s` in `%s`: current reason %q\n", f.Symbol, f.Path, f.Reason)
	}
	return b.String()
}

// CheckObligationReasons judges every no_change_needed waiver in plan against
// the consumer it names.
//
// The error is non-nil when the waivers could not be judged. Having no
// waivers to judge is not one of those: it returns nil, because a plan that
// waives nothing has nothing for this site to decide. judgment.FailClosed
// turns a failure into a stop, and only where the tier says it should.
func CheckObligationReasons(ctx context.Context, j judgment.Judge, objective string,
	plan workflow.Plan, impact *graph.Impact, logf func(string, ...any)) (ObligationReasonResult, error) {

	if logf == nil {
		logf = func(string, ...any) {}
	}
	res := ObligationReasonResult{Tier: judgment.SiteTier(j, ObligationReasonSite)}
	if impact == nil {
		res.SkipReason = "no_impact"
		return res, nil
	}

	type waiver struct {
		node graph.Node
		ob   workflow.Obligation
	}
	var waivers []waiver
	for _, ob := range plan.Obligations {
		if ob.Resolution.Action != workflow.ActionNoChange {
			continue
		}
		for _, c := range impact.Consumers {
			// The same match ValidateObligations applies: path exact, symbol
			// the short name, the fully-qualified one, or a dotted tail of it,
			// because a planner may have written any of them.
			if c.Node.Path != ob.Path {
				continue
			}
			if !workflow.NamesConsumer(ob.Symbol, c.Node) {
				continue
			}
			waivers = append(waivers, waiver{node: c.Node, ob: ob})
			break
		}
	}
	res.Total = len(waivers)

	switch {
	case len(waivers) == 0:
		// Nothing to decide: checked before the judge, so a plan that waives
		// nothing never depends on Jev being reachable.
		res.SkipReason = "no_waivers"
		return res, nil
	case j == nil || !j.Available():
		res.SkipReason = "no_judge"
		return res, judgment.Unmet(ObligationReasonSite, judgment.FailureNotConfigured,
			judgment.Unready(j))
	case strings.TrimSpace(objective) == "":
		res.SkipReason = "no_objective"
		return res, nil
	}
	res.Attempted = true

	// The state's redaction mode follows the judge's configuration, not a
	// fixed choice: under strict this asks about path, symbol and kind alone,
	// which is enough to judge a reason's specificity; under repo_text it
	// also includes the consumer's signature where the graph has one, which
	// is strictly more for the same question. Either way nothing here
	// requires repo_text to run at all — unlike CheckTestIntegrity, whose
	// subject (a diff hunk) has no metadata-only form.
	mode := judgment.RedactModeOf(j)

	st := judgment.NewState(mode)
	if err := st.Objective(objective); err != nil {
		logf("obligation reason: %v", err)
		res.SkipReason = "state_error"
		return res, judgment.Unmet(ObligationReasonSite, judgment.FailureRefused, err.Error())
	}

	items := make([]*judgment.RepoItem, 0, len(waivers))
	ids := make([]string, 0, len(waivers))
	// used parallels ids, the same way checkIntegrityBatch's does: a waiver
	// is skipped in the loop below whenever its reason is empty or its state
	// fields refuse, so ids and waivers diverge and an answer must be matched
	// back through this rather than through waivers' own index.
	used := make([]waiver, 0, len(waivers))
	qs := make(map[string]judgment.Question, len(waivers))
	for i, w := range waivers {
		reason := strings.TrimSpace(w.ob.Resolution.Reason)
		if reason == "" {
			// ValidateObligations already refuses a no_change_needed with an
			// empty reason before a plan reaches here; an empty one at this
			// point means the caller skipped that validation, and asking a
			// judgment to grade nothing would be a confident answer about
			// evidence that does not exist.
			continue
		}
		id := "c" + strconv.Itoa(i)
		if err := st.ClaimText("reason_"+id, reason); err != nil {
			logf("obligation reason: %v", err)
			continue
		}
		item, err := st.NewRepoItem(id, w.node.Path)
		if err != nil {
			logf("obligation reason: %v", err)
			continue
		}
		item.Meta("symbol", w.node.Name).Meta("kind", string(w.node.Kind))
		if w.node.StartLine > 0 {
			item.Lines(w.node.StartLine, w.node.EndLine)
		}
		if mode.AtLeast(judgment.RedactRepoText) && w.node.Signature != "" {
			// Best-effort: a signature over the field cap or one that trips
			// the credential scan is skipped rather than aborting the
			// question, since path/symbol/kind alone still answer it.
			_ = item.Text("signature", w.node.Signature, st)
		}
		items = append(items, item)
		ids = append(ids, id)
		used = append(used, w)
		qs[id] = judgment.Noul(plausibleProposition(id))
	}
	if len(items) == 0 {
		// Every waiver was dropped building the state, so there is no
		// question left to ask. That is a local refusal, not an absent
		// subject: the waivers existed and this could not put them in a
		// question, so a mandatory caller must hear about it.
		res.SkipReason = "no_eligible_waivers"
		return res, judgment.Unmet(ObligationReasonSite, judgment.FailureRefused,
			"no waiver could be put into a question this site may send")
	}
	if err := st.AddItems("consumers", items); err != nil {
		logf("obligation reason: %v", err)
		res.SkipReason = "state_error"
		return res, judgment.Unmet(ObligationReasonSite, judgment.FailureRefused, err.Error())
	}

	answers, note, askErr := judgment.RequireAll(ctx, j, ObligationReasonSite, st, qs)
	res.Requests += note.Usage.Requests
	res.InputTokens += note.Usage.InputTokens
	res.OutputTokens += note.Usage.OutputTokens

	for n, id := range ids {
		a := answers[id]
		if !a.Answered {
			continue
		}
		res.Judged++
		if a.Noul < DefaultObligationReasonFloor {
			w := used[n]
			res.Findings = append(res.Findings, ObligationReasonFinding{
				Symbol: w.ob.Symbol, Path: w.ob.Path, Reason: strings.TrimSpace(w.ob.Resolution.Reason),
				Plausible: a.Noul,
				Detail: fmt.Sprintf(
					"the stated reason for leaving %s in %s unchanged looks weak (p=%.2f); "+
						"read it before trusting this waiver", w.ob.Symbol, w.ob.Path, a.Noul),
			})
		}
	}
	res.Applied = res.Judged > 0
	sort.Slice(res.Findings, func(i, j int) bool {
		if res.Findings[i].Plausible != res.Findings[j].Plausible {
			return res.Findings[i].Plausible < res.Findings[j].Plausible
		}
		return res.Findings[i].Path < res.Findings[j].Path
	})
	return res, askErr
}
