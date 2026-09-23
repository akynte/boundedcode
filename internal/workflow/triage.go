package workflow

// Failure triage: a judgment site over verification failures (design.md
// mechanism M6).
//
// Environmental is eight substrings and Status == Error. Classify's SAME is
// an exact fingerprint match, which keeps line numbers — its own comment
// says so — and therefore reads the same error after an unrelated insertion
// moved it down the file as a brand-new failure. Both are honest, bounded
// heuristics, and both leave a category of case they cannot reach: a failure
// whose *kind* (an environment problem, a defect in the test rather than the
// code, an incomplete change, a broken build) is not spelled the keyword
// list expects, and a failure that is the same underlying cause wearing a
// different fingerprint.
//
// Nothing here replaces the deterministic classification. Environmental's
// keyword list still runs and still pauses a task on its own; a judged
// "environment" only ever adds a pause, on top of it, never instead — R1's
// monotone-scrutiny rule applied literally. Same-cause only ever *promotes*
// a NEW fingerprint toward SAME for the ladder's purposes, which means
// escalating sooner, the conservative direction; it never demotes a real
// SAME to NEW.

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/recipe"
)

// TriageSite names this judgment site in judgment.yaml's `sites` map.
const TriageSite = "failure_triage"

func init() {
	judgment.RegisterSite(judgment.SiteInfo{
		Name:            TriageSite,
		Description:     "classifies a verification failure's kind and whether it shares an underlying cause with an earlier, differently-fingerprinted failure",
		Mechanism:       "M6",
		Version:         "1",
		MaxEffect:       judgment.TierRouting,
		Redaction:       judgment.RedactOutput,
		Outcome:         "a fresh-sandbox rerun of the same recipe; recurrence, for same-cause",
		EffectThreshold: 0.7,
	})
}

// TriageCategory is the Choice this site asks per failure. The categories
// are the concrete kinds recovery already treats differently, not a
// re-statement of Status or Class.
type TriageCategory string

const (
	TriageEnvironment TriageCategory = "environment"
	TriageTestDefect  TriageCategory = "test_defect"
	TriageCodeDefect  TriageCategory = "code_defect"
	TriageIncomplete  TriageCategory = "incomplete"
	TriageBuildBreak  TriageCategory = "build_break"
)

var triageCriteria = map[string]string{
	string(TriageEnvironment): "The failure is caused by the environment the command ran in " +
		"— resources, permissions, network, a missing toolchain or an empty module cache — " +
		"not by anything in the change itself.",
	string(TriageTestDefect): "The test itself is wrong, brittle, or asserts behaviour the " +
		"objective is deliberately changing, so the failure is a problem with the test.",
	string(TriageCodeDefect): "The change compiles and the test is a fair check, but the " +
		"change's own logic produces the wrong result.",
	string(TriageIncomplete): "The change is missing a part: an unimplemented reference, a " +
		"missing import, a symbol that does not resolve, a call the change should have made " +
		"but did not.",
	string(TriageBuildBreak): "The change does not compile or type-check at all, before any " +
		"test could run.",
}

// TriageFinding is one failure's judged classification.
type TriageFinding struct {
	// Fingerprint ties this finding back to the FailureRecord it was judged
	// alongside, the same fingerprint Classify already computed.
	Fingerprint string `json:"fingerprint"`
	Recipe      string `json:"recipe"`
	// Category is the judged kind, with its own confidence — a Choice
	// answer's, from the distribution over the five options.
	Category           TriageCategory `json:"category"`
	CategoryConfidence float64        `json:"category_confidence"`
	// SameCause is P(this failure and the immediately preceding one share an
	// underlying cause), asked only when a preceding record with a different
	// fingerprint exists in the same recipe. Zero, with HasSameCause false,
	// when there was nothing to compare against.
	SameCause    float64 `json:"same_cause,omitempty"`
	HasSameCause bool    `json:"has_same_cause,omitempty"`
	// ObjectiveChanges is P(the failing test exercises behaviour the
	// objective asks to change) — the signal that tells an expected
	// REGRESSION from an alarming one.
	ObjectiveChanges float64 `json:"objective_changes"`
}

// TriageResult is one triage's outcome, in the shape every other judgment
// site in this codebase already uses.
type TriageResult struct {
	Total, Judged                       int
	Attempted, Applied                  bool
	Tier                                judgment.Tier
	SkipReason                          string
	Findings                            []TriageFinding
	Requests, InputTokens, OutputTokens int
}

// TriageTuning bounds one triage call.
type TriageTuning struct {
	MaxFindingsPerFailure int
	MaxFailures           int
	Budget                time.Duration
}

const (
	DefaultTriageMaxFindingsPerFailure = 10
	DefaultTriageMaxFailures           = 12
	DefaultTriageBudget                = 10 * time.Second
)

func (t TriageTuning) withDefaults() TriageTuning {
	if t.MaxFindingsPerFailure <= 0 {
		t.MaxFindingsPerFailure = DefaultTriageMaxFindingsPerFailure
	}
	if t.MaxFailures <= 0 {
		t.MaxFailures = DefaultTriageMaxFailures
	}
	if t.Budget <= 0 {
		t.Budget = DefaultTriageBudget
	}
	return t
}

// categoryProposition, sameCauseProposition and objectiveProposition name the
// failure by id inside the instructions, the same convention every other
// batched site in this codebase uses.
func categoryProposition(id string) string {
	return "Considering the verification failure with id `" + id + "` in `failures`: which " +
		"of the categories best describes why it failed?"
}

func sameCauseProposition(id string) string {
	return "Considering the verification failure with id `" + id + "` in `failures` and the " +
		"preceding failure described in `" + id + "_previous`: do the two share the same " +
		"underlying cause, even though their exact wording or location differs — for example, " +
		"the same missing thing reported through a different message, or the same error moved " +
		"by an unrelated edit?"
}

func objectiveProposition(id string) string {
	return "Considering the verification failure with id `" + id + "` in `failures`, against " +
		"`objective`: does the failing check exercise behaviour that `objective` is asking to " +
		"change, as opposed to behaviour the change should have left alone?"
}

// TriageFailures judges every failure in failures, using previous (the
// FailureHistory entries recorded so far this task) to ask the same-cause
// question wherever a plausible predecessor exists.
//
// The error is non-nil when the failures could not be triaged. Having no
// failures to triage is not that — a passing verification has nothing for this
// site to categorise — and returns nil.
func TriageFailures(ctx context.Context, j judgment.Judge, objective string, failures []recipe.Result,
	previous []FailureRecord, root string, tuning TriageTuning, logf func(string, ...any)) (TriageResult, error) {

	if logf == nil {
		logf = func(string, ...any) {}
	}
	tn := tuning.withDefaults()
	res := TriageResult{Total: len(failures), Tier: judgment.SiteTier(j, TriageSite)}

	switch {
	case len(failures) == 0:
		// Nothing to decide: a passing verification never depends on the
		// service being reachable.
		res.SkipReason = "no_failures"
		return res, nil
	case j == nil || !j.Available():
		res.SkipReason = "no_judge"
		return res, judgment.Unmet(TriageSite, judgment.FailureNotConfigured,
			judgment.Unready(j))
	case strings.TrimSpace(objective) == "":
		res.SkipReason = "no_objective"
		return res, nil
	case !judgment.RedactModeOf(j).AtLeast(judgment.RedactOutput):
		// Verification output is neither source nor metadata; it gets its
		// own tier (State.Output) and this site has nothing it may send
		// below it. Preflight reports this for a site configured at routing.
		res.SkipReason = "redact_below_output"
		return res, judgment.Unmet(TriageSite, judgment.FailureRefused,
			"this site needs the output redact mode and the configured one is stricter")
	}
	batch := failures
	if len(batch) > tn.MaxFailures {
		batch = batch[:tn.MaxFailures]
		res.SkipReason = "too_many_failures"
	}
	res.Attempted = true

	ctx, cancel := context.WithTimeout(ctx, tn.Budget)
	defer cancel()

	st := judgment.NewState(judgment.RedactOutput)
	if err := st.Objective(objective); err != nil {
		logf("triage: %v", err)
		res.SkipReason = "state_error"
		return res, judgment.Unmet(TriageSite, judgment.FailureRefused, err.Error())
	}

	qs := make(map[string]judgment.Question, len(batch)*3)
	ids := make([]string, 0, len(batch))
	fps := make([]string, 0, len(batch))
	sameCauseAsked := make(map[string]bool, len(batch))

	for n, f := range batch {
		id := "t" + strconv.Itoa(n)
		fp, symbols := FingerprintWithSymbols(f, root)
		text := renderFailureText(f, root, tn.MaxFindingsPerFailure)
		if err := st.Output(id, f.Recipe, text); err != nil {
			logf("triage: %v", err)
			continue
		}
		ids = append(ids, id)
		fps = append(fps, fp)
		qs[id] = judgment.Choice(categoryProposition(id), triageCriteria)
		qs["obj_"+id] = judgment.Noul(objectiveProposition(id))

		if pred := precedingDifferentFailure(previous, f.Recipe, fp, symbols); pred != nil {
			predText := pred.Headline
			if len(pred.Symbols) > 0 {
				predText += " (" + strings.Join(pred.Symbols, ", ") + ")"
			}
			if err := st.Output(id+"_previous", f.Recipe, predText); err == nil {
				qs["same_"+id] = judgment.Noul(sameCauseProposition(id))
				sameCauseAsked[id] = true
			}
		}
	}
	if len(ids) == 0 {
		// There were failures and none could be sent.
		res.SkipReason = "no_eligible_failures"
		return res, judgment.Unmet(TriageSite, judgment.FailureRefused,
			"no failure could be put into a question this site may send")
	}

	answers, note, askErr := judgment.RequireAll(ctx, j, TriageSite, st, qs)
	res.Requests += note.Usage.Requests
	res.InputTokens += note.Usage.InputTokens
	res.OutputTokens += note.Usage.OutputTokens
	if askErr != nil {
		return res, askErr
	}

	floor := judgment.MinConfidenceOf(j)
	for n, id := range ids {
		cat := answers[id]
		if !cat.Answered {
			continue
		}
		// The operator's floor: an unsure category is not one to route a
		// failed verification on.
		if cat.Confidence < floor {
			continue
		}
		res.Judged++
		finding := TriageFinding{
			Fingerprint: fps[n], Recipe: batch[n].Recipe,
			Category: TriageCategory(cat.Choice), CategoryConfidence: cat.Confidence,
		}
		if a := answers["obj_"+id]; a.Answered {
			finding.ObjectiveChanges = a.Noul
		}
		if sameCauseAsked[id] {
			if a := answers["same_"+id]; a.Answered {
				finding.SameCause = a.Noul
				finding.HasSameCause = true
			}
		}
		res.Findings = append(res.Findings, finding)
	}
	res.Applied = res.Judged > 0
	sort.Slice(res.Findings, func(i, j int) bool { return res.Findings[i].Fingerprint < res.Findings[j].Fingerprint })
	return res, nil
}

// renderFailureText is the normalized text this site sends for one failure:
// the headline and up to max findings, already through Normalize so no path,
// timestamp, address or duration crosses the boundary raw.
func renderFailureText(r recipe.Result, root string, max int) string {
	var b strings.Builder
	b.WriteString(Normalize(r.Summary.Headline, root))
	findings := r.Summary.Findings
	if len(findings) > max {
		findings = findings[:max]
	}
	for _, f := range findings {
		fmt.Fprintf(&b, "\n%s: %s", Normalize(f.File, root), Normalize(f.Message, root))
	}
	return b.String()
}

// precedingDifferentFailure finds a record for the same recipe whose
// fingerprint differs from fp — a candidate the deterministic SAME/NEW check
// already ruled NEW, and so the one case where "same underlying cause" is
// actually informative. A record with the identical fingerprint is skipped:
// Classify has already called that SAME with certainty, and asking a
// judgment to confirm an exact hash match is asking it something code
// already knows, the flavour of question the jaggedness notes single out as
// wasted.
//
// A record sharing at least one primary symbol with the current failure is
// preferred — the moved-error case this whole site exists for is exactly a
// shared symbol under a changed fingerprint — falling back to the most
// recent different-fingerprint record when no symbol overlaps.
func precedingDifferentFailure(previous []FailureRecord, recipeName, fp string, symbols []string) *FailureRecord {
	candidate := func(p FailureRecord) bool {
		return p.Recipe == recipeName && p.Fingerprint != fp && p.Headline != ""
	}
	for i := len(previous) - 1; i >= 0; i-- {
		if p := previous[i]; candidate(p) && sharesSymbol(p.Symbols, symbols) {
			found := p
			return &found
		}
	}
	for i := len(previous) - 1; i >= 0; i-- {
		if p := previous[i]; candidate(p) {
			found := p
			return &found
		}
	}
	return nil
}

func sharesSymbol(a, b []string) bool {
	set := make(map[string]bool, len(a))
	for _, s := range a {
		set[s] = true
	}
	for _, s := range b {
		if set[s] {
			return true
		}
	}
	return false
}
