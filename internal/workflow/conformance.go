package workflow

// Diff-to-plan conformance: a judgment site over what the diff actually does,
// asked against what the plan said it would do (design.md mechanism M5).
//
// wt.OutOfScope already catches a write to a file the plan did not declare.
// It cannot catch a hunk *inside* an allowed file that has nothing to do with
// the plan's stated reason for touching that file, and it cannot catch a
// diff that is entirely in scope and still does not address the objective —
// the model edited the right files, for the wrong change, or no material
// change at all. Both are invisible to a file-level scope check and both are
// exactly what a verify run then spends its budget discovering the slow way.

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/judgment"
)

// ConformanceSite names this judgment site in judgment.yaml's `sites` map.
const ConformanceSite = "diff_conformance"

func init() {
	judgment.RegisterSite(judgment.SiteInfo{
		Name:            ConformanceSite,
		Description:     "judges whether each diff hunk serves the plan's stated reason for its file, and whether the diff as a whole addresses the objective",
		Mechanism:       "M5",
		Version:         "1",
		MaxEffect:       judgment.TierRouting,
		Redaction:       judgment.RedactRepoText,
		Outcome:         "the gate decision on the change these hunks were part of",
		EffectThreshold: 0.7,
	})
}

// ConformanceCategory is the Choice this site asks per hunk.
type ConformanceCategory string

const (
	// ConformanceServesPlan: the hunk implements or tests a step the plan's
	// reason for this file describes.
	ConformanceServesPlan ConformanceCategory = "serves_plan"
	// ConformanceIncidental: formatting, import ordering, a comment or a
	// rename a plan step required as a side effect.
	ConformanceIncidental ConformanceCategory = "incidental"
	// ConformanceUnrelated: a change the plan's reasons do not describe.
	ConformanceUnrelated ConformanceCategory = "unrelated"
	// ConformanceUndermines: a change that removes or disables something the
	// plan said must keep working.
	ConformanceUndermines ConformanceCategory = "undermines"
)

var conformanceCriteria = map[string]string{
	string(ConformanceServesPlan): "The hunk implements or tests a step the plan's stated " +
		"reason for changing this file describes.",
	string(ConformanceIncidental): "The hunk is formatting, import ordering, a comment, or a " +
		"rename that a plan step required as a side effect, not a behaviour change of its own.",
	string(ConformanceUnrelated): "The hunk is a change the plan's stated reasons do not " +
		"describe or imply.",
	string(ConformanceUndermines): "The hunk removes, weakens or disables something the plan " +
		"said must keep working.",
}

// ConformanceFinding is one hunk's judged relationship to the plan.
type ConformanceFinding struct {
	Path       string              `json:"path"`
	Header     string              `json:"header"`
	Category   ConformanceCategory `json:"category"`
	Confidence float64             `json:"confidence"`
}

// ConformanceResult is one check's outcome.
type ConformanceResult struct {
	Total, Candidates, Judged int
	Attempted, Applied        bool
	Tier                      judgment.Tier
	SkipReason                string
	Findings                  []ConformanceFinding
	// AddressesObjective is P(taken together, the hunks address what the
	// objective asks for) — the whole-diff Noul. Answered is false when it
	// was not asked (no judge, no eligible hunks, and so on).
	AddressesObjective                  float64
	AddressesObjectiveAnswered          bool
	Requests, InputTokens, OutputTokens int
	Latency                             time.Duration
}

// ConformanceTuning bounds one check.
type ConformanceTuning struct {
	MaxHunks  int
	BatchSize int
	Budget    time.Duration
	// UndermineFloor is the probability at or above which a hunk's
	// ConformanceUndermines finding is treated as real.
	UndermineFloor float64
}

const (
	DefaultConformanceMaxHunks       = 40
	DefaultConformanceBatchSize      = 8
	DefaultConformanceBudget         = 10 * time.Second
	DefaultConformanceUndermineFloor = 0.7
)

func (t ConformanceTuning) withDefaults() ConformanceTuning {
	if t.MaxHunks <= 0 {
		t.MaxHunks = DefaultConformanceMaxHunks
	}
	if t.BatchSize <= 0 {
		t.BatchSize = DefaultConformanceBatchSize
	}
	if t.Budget <= 0 {
		t.Budget = DefaultConformanceBudget
	}
	if t.UndermineFloor <= 0 {
		t.UndermineFloor = DefaultConformanceUndermineFloor
	}
	return t
}

// PlanReason is one file's stated reason for changing, the input this site
// needs from a plan without importing task's Plan type (avoiding an import
// cycle: internal/task already imports internal/workflow).
type PlanReason struct {
	Path   string
	Reason string
}

const conformanceCollection = "hunks"

func conformanceProposition(id string) string {
	return "Considering the diff hunk with id `" + id + "` in `" + conformanceCollection + "`, " +
		"and the plan's stated reasons in `plan_reasons`: which category best describes this " +
		"hunk's relationship to the plan?"
}

func wholeDiffProposition() string {
	return "Considering every hunk in `" + conformanceCollection + "`, taken together, against " +
		"`objective`: do these changes address what the objective asks for?"
}

// CheckDiffConformance judges every hunk in hunks against plan's stated
// reasons, plus one whole-diff question against objective.
//
// The error is non-nil when the diff could not be judged; an empty diff is
// not that, and returns nil. Hunks in a file the plan gives no reason for are
// skipped per-hunk (an empty reason makes every hunk read as unrelated, which
// design.md's own risk note warns against), but still count toward the
// whole-diff question.
//
// As with CheckTestIntegrity, one failed batch fails the check rather than
// returning the part that succeeded: an unjudged hunk reads downstream exactly
// like a conforming one.
func CheckDiffConformance(ctx context.Context, j judgment.Judge, objective string, reasons []PlanReason,
	hunks []Hunk, tuning ConformanceTuning, logf func(string, ...any)) (ConformanceResult, error) {

	if logf == nil {
		logf = func(string, ...any) {}
	}
	tn := tuning.withDefaults()
	res := ConformanceResult{Total: len(hunks), Tier: judgment.SiteTier(j, ConformanceSite)}

	reasonByPath := make(map[string]string, len(reasons))
	for _, r := range reasons {
		if strings.TrimSpace(r.Reason) != "" {
			reasonByPath[r.Path] = r.Reason
		}
	}
	var candidates []Hunk
	for _, h := range hunks {
		if reasonByPath[h.Path] != "" {
			candidates = append(candidates, h)
		}
	}
	res.Candidates = len(candidates)

	switch {
	case len(hunks) == 0:
		// Nothing to decide: an empty diff never depends on the service.
		res.SkipReason = "no_hunks"
		return res, nil
	case j == nil || !j.Available():
		res.SkipReason = "no_judge"
		return res, judgment.Unmet(ConformanceSite, judgment.FailureNotConfigured,
			judgment.Unready(j))
	case strings.TrimSpace(objective) == "":
		res.SkipReason = "no_objective"
		return res, nil
	case !judgment.RedactModeOf(j).AtLeast(judgment.RedactRepoText):
		// Diff hunks are repository source. Preflight reports this
		// combination for a site configured at routing.
		res.SkipReason = "redact_strict"
		return res, judgment.Unmet(ConformanceSite, judgment.FailureRefused,
			"this site needs repo_text and the configured redact mode is stricter")
	}
	if len(candidates) > tn.MaxHunks {
		candidates = candidates[:tn.MaxHunks]
		res.SkipReason = "too_many_hunks"
	}
	res.Attempted = true

	ctx, cancel := context.WithTimeout(ctx, tn.Budget)
	defer cancel()
	started := time.Now()

	// The whole-diff question is asked once, in its own request, because it
	// is not per-hunk: batching it alongside the per-hunk Choice questions
	// would mean every batch re-asks it, paying for the same answer several
	// times over for no reason.
	var askErr error
	if len(hunks) > 0 {
		addresses, note, err := checkWholeDiff(ctx, j, objective, hunks, logf)
		res.Requests += note.Usage.Requests
		res.InputTokens += note.Usage.InputTokens
		res.OutputTokens += note.Usage.OutputTokens
		if addresses != nil {
			res.AddressesObjective = *addresses
			res.AddressesObjectiveAnswered = true
		}
		askErr = err
	}

	for start := 0; start < len(candidates) && askErr == nil; start += tn.BatchSize {
		end := min(start+tn.BatchSize, len(candidates))
		findings, judged, note, err := checkConformanceBatch(ctx, j, objective, reasonByPath,
			candidates[start:end], logf)
		res.Judged += judged
		res.Findings = append(res.Findings, findings...)
		res.Requests += note.Usage.Requests
		res.InputTokens += note.Usage.InputTokens
		res.OutputTokens += note.Usage.OutputTokens
		askErr = err
	}
	res.Latency = time.Since(started)
	res.Applied = res.Judged > 0 || res.AddressesObjectiveAnswered
	sort.Slice(res.Findings, func(i, j int) bool {
		if res.Findings[i].Category != res.Findings[j].Category {
			return res.Findings[i].Category < res.Findings[j].Category
		}
		return res.Findings[i].Path < res.Findings[j].Path
	})
	if askErr != nil {
		res.Applied = false
		return res, askErr
	}
	return res, nil
}

// checkWholeDiff asks the single whole-diff proposition. Reasons is not
// needed here: the question is about objective coverage, not per-file intent.
func checkWholeDiff(ctx context.Context, j judgment.Judge, objective string, hunks []Hunk,
	logf func(string, ...any)) (*float64, judgment.Note, error) {

	st := judgment.NewState(judgment.RedactRepoText)
	if err := st.Objective(objective); err != nil {
		logf("conformance: %v", err)
		return nil, judgment.Note{Source: judgment.SourceError, Detail: err.Error()},
			judgment.Unmet(ConformanceSite, judgment.FailureRefused, err.Error())
	}
	items := make([]*judgment.RepoItem, 0, len(hunks))
	for n, h := range hunks {
		id := "d" + strconv.Itoa(n)
		item, err := st.NewRepoItem(id, h.Path)
		if err != nil {
			continue
		}
		item.Meta("hunk_header", h.Header)
		if err := item.Text("diff", h.Body, st); err != nil {
			continue
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil, judgment.Note{Source: judgment.SourceRefused, Detail: "no eligible hunks"},
			judgment.Unmet(ConformanceSite, judgment.FailureRefused, "no hunk was eligible to send")
	}
	if err := st.AddItems(conformanceCollection, items); err != nil {
		logf("conformance: %v", err)
		return nil, judgment.Note{Source: judgment.SourceError, Detail: err.Error()},
			judgment.Unmet(ConformanceSite, judgment.FailureRefused, err.Error())
	}
	answers, note, err := judgment.RequireAll(ctx, j, ConformanceSite, st, map[string]judgment.Question{
		"addresses": judgment.Noul(wholeDiffProposition()),
	})
	if err != nil {
		return nil, note, err
	}
	if a := answers["addresses"]; a.Answered {
		v := a.Noul
		return &v, note, nil
	}
	return nil, note, judgment.Unmet(ConformanceSite, judgment.FailureInvalid,
		"the response carried no whole-diff answer")
}

// checkConformanceBatch asks the per-hunk Choice for one batch, the same
// shape workflow.checkIntegrityBatch and retrieval's rerankBatch use.
func checkConformanceBatch(ctx context.Context, j judgment.Judge, objective string,
	reasonByPath map[string]string, batch []Hunk, logf func(string, ...any)) ([]ConformanceFinding, int, judgment.Note, error) {

	st := judgment.NewState(judgment.RedactRepoText)
	if err := st.Objective(objective); err != nil {
		logf("conformance: %v", err)
		return nil, 0, judgment.Note{Source: judgment.SourceError, Detail: err.Error()},
			judgment.Unmet(ConformanceSite, judgment.FailureRefused, err.Error())
	}
	var reasonText strings.Builder
	seen := map[string]bool{}
	for _, h := range batch {
		if r := reasonByPath[h.Path]; r != "" && !seen[h.Path] {
			seen[h.Path] = true
			fmt.Fprintf(&reasonText, "%s: %s\n", h.Path, r)
		}
	}
	if err := st.ClaimText("plan_reasons", strings.TrimSpace(reasonText.String())); err != nil {
		logf("conformance: %v", err)
		return nil, 0, judgment.Note{Source: judgment.SourceError, Detail: err.Error()},
			judgment.Unmet(ConformanceSite, judgment.FailureRefused, err.Error())
	}

	items := make([]*judgment.RepoItem, 0, len(batch))
	ids := make([]string, 0, len(batch))
	used := make([]Hunk, 0, len(batch))
	qs := make(map[string]judgment.Question, len(batch))
	for n, h := range batch {
		id := "c" + strconv.Itoa(n)
		item, err := st.NewRepoItem(id, h.Path)
		if err != nil {
			logf("conformance: %v", err)
			continue
		}
		item.Meta("hunk_header", h.Header)
		if err := item.Text("diff", h.Body, st); err != nil {
			logf("conformance: %v", err)
			continue
		}
		items = append(items, item)
		ids = append(ids, id)
		used = append(used, h)
		qs[id] = judgment.Choice(conformanceProposition(id), conformanceCriteria)
	}
	if len(items) == 0 {
		return nil, 0, judgment.Note{Source: judgment.SourceRefused, Detail: "no eligible hunks in batch"},
			judgment.Unmet(ConformanceSite, judgment.FailureRefused, "no hunk in this batch was eligible to send")
	}
	if err := st.AddItems(conformanceCollection, items); err != nil {
		logf("conformance: %v", err)
		return nil, 0, judgment.Note{Source: judgment.SourceError, Detail: err.Error()},
			judgment.Unmet(ConformanceSite, judgment.FailureRefused, err.Error())
	}

	answers, note, askErr := judgment.RequireAll(ctx, j, ConformanceSite, st, qs)
	if askErr != nil {
		return nil, 0, note, askErr
	}
	floor := judgment.MinConfidenceOf(j)
	var findings []ConformanceFinding
	judged := 0
	for n, id := range ids {
		a := answers[id]
		if !a.Answered {
			continue
		}
		// The operator's floor, applied per hunk. A category this unsure is
		// not a category to route on, and AskAll cannot apply the floor to a
		// batch on the call site's behalf.
		if a.Confidence < floor {
			continue
		}
		judged++
		h := used[n]
		findings = append(findings, ConformanceFinding{
			Path: h.Path, Header: h.Header,
			Category: ConformanceCategory(a.Choice), Confidence: a.Confidence,
		})
	}
	return findings, judged, note, nil
}
