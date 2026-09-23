package workflow

// Semantic progress monitor: a judgment site over the evidence window an
// EDIT attempt has accumulated (design.md mechanism M2).
//
// native/progress.go already ends an attempt after three byte-identical
// call/answer pairs or three steps that learned nothing new by that exact
// measure. What it cannot see is a paraphrase: a second search whose terms
// reword the first, a read of the range next to one already read, a file
// edited and then edited back. This reads the same bounded evidence window
// the byte-identity guard already keeps (ReadEvidence — real tool names,
// paths and excerpts, unlike TriedCall's opaque fingerprints) and asks for a
// semantic read of it.
//
// Adaptation from the design: the design asks this after every tool call
// inside EDIT. The engine abstraction (internal/engine.Engine) that the
// native and OpenCode adapters both implement runs its own bounded tool
// loop internally and returns once per attempt, not once per call — asking
// per call would mean threading a judge through that interface for every
// engine, a change design.md does not ask for and this package cannot make
// on its own authority. This runs once per Step() return instead, over the
// same evidence window, which is the granularity actually available at the
// supervisor boundary. The semantic content of the question is unchanged;
// only how often it can be asked is narrower than the design's ideal.

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/judgment"
)

// ProgressSite names this judgment site in judgment.yaml's `sites` map.
const ProgressSite = "progress_monitor"

func init() {
	judgment.RegisterSite(judgment.SiteInfo{
		Name:            ProgressSite,
		Description:     "reads the recent tool-call evidence window in EDIT and judges whether it is advancing, circling, oscillating, drifting or blocked",
		Mechanism:       "M2",
		Version:         "1",
		MaxEffect:       judgment.TierRouting,
		Redaction:       judgment.RedactRepoText,
		Outcome:         "whether that attempt's own verification passed",
		EffectThreshold: 0.7,
	})
}

// ProgressCategory is the Choice this site asks over a window of evidence.
type ProgressCategory string

const (
	// ProgressAdvancing: recent calls read or edit things the plan names
	// that were not already covered.
	ProgressAdvancing ProgressCategory = "advancing"
	// ProgressCircling: recent calls revisit things already read, with no
	// edit between them.
	ProgressCircling ProgressCategory = "circling"
	// ProgressOscillating: edits are being undone by later edits.
	ProgressOscillating ProgressCategory = "oscillating"
	// ProgressDrifting: recent calls concern paths outside the plan.
	ProgressDrifting ProgressCategory = "drifting"
	// ProgressBlocked: recent calls are repeated attempts at one failing
	// operation.
	ProgressBlocked ProgressCategory = "blocked"
)

var progressCriteria = map[string]string{
	string(ProgressAdvancing): "Recent calls read or edit things the plan names that were not " +
		"already covered by an earlier call in the window.",
	string(ProgressCircling): "Recent calls revisit files or ranges already read in the window, " +
		"with no edit made between the first read and the repeat.",
	string(ProgressOscillating): "An edit is followed by another edit that undoes or reverses it, " +
		"visible as the same file or declaration changing back and forth in the window.",
	string(ProgressDrifting): "Recent calls concern paths that are neither in the plan's files " +
		"nor its obligations nor a test of one of them.",
	string(ProgressBlocked): "Recent calls are repeated attempts at one operation that keeps " +
		"failing or returning nothing useful.",
}

// ProgressResult is one check's outcome.
type ProgressResult struct {
	WindowSize                          int
	Attempted, Applied                  bool
	Tier                                judgment.Tier
	SkipReason                          string
	Category                            ProgressCategory
	Confidence                          float64
	Requests, InputTokens, OutputTokens int
}

// ProgressTuning bounds one check.
type ProgressTuning struct {
	WindowSize int
	Budget     time.Duration
}

const (
	// DefaultProgressWindowSize is deliberately short: design.md calls for
	// "eight to twelve calls" so the window itself cannot be the source of
	// context rot the jaggedness notes warn about.
	DefaultProgressWindowSize = 10
	DefaultProgressBudget     = 3 * time.Second
)

func (t ProgressTuning) withDefaults() ProgressTuning {
	if t.WindowSize <= 0 {
		t.WindowSize = DefaultProgressWindowSize
	}
	if t.Budget <= 0 {
		t.Budget = DefaultProgressBudget
	}
	return t
}

func progressProposition() string {
	return "Considering the ordered window of recent tool calls in `window`, against the " +
		"plan's files and symbols in `plan_files` and `plan_symbols`: which category best " +
		"describes what the most recent calls are doing?"
}

// CheckProgress judges the most recent window of read evidence.
//
// The error is non-nil when the window could not be judged. An empty window
// is not one of those: there is then nothing to form an opinion about, and
// nil is returned. It sends no file bodies — only tool name, path, the
// distinguishing part of the arguments, declaration names and a short
// excerpt, the same fields ReadEvidence already carries for the byte-identity
// guard.
func CheckProgress(ctx context.Context, j judgment.Judge, planFiles, planSymbols []string,
	reads []ReadEvidence, tuning ProgressTuning, logf func(string, ...any)) (ProgressResult, error) {

	if logf == nil {
		logf = func(string, ...any) {}
	}
	tn := tuning.withDefaults()
	res := ProgressResult{Tier: judgment.SiteTier(j, ProgressSite)}

	window := recentReads(reads, tn.WindowSize)
	res.WindowSize = len(window)

	switch {
	case len(window) == 0:
		// Nothing to decide, checked first so an empty window never depends
		// on the service being reachable.
		res.SkipReason = "no_evidence"
		return res, nil
	case j == nil || !j.Available():
		res.SkipReason = "no_judge"
		return res, judgment.Unmet(ProgressSite, judgment.FailureNotConfigured,
			judgment.Unready(j))
	case !judgment.RedactModeOf(j).AtLeast(judgment.RedactRepoText):
		// Detail and Excerpt are repository text — a search's query terms
		// and a snippet of what was found. Under strict mode this site has
		// nothing it may send, which for a site configured at routing is a
		// contradiction Preflight reports before a task starts.
		res.SkipReason = "redact_strict"
		return res, judgment.Unmet(ProgressSite, judgment.FailureRefused,
			"this site needs repo_text and the configured redact mode is stricter")
	}
	res.Attempted = true

	ctx, cancel := context.WithTimeout(ctx, tn.Budget)
	defer cancel()

	st := judgment.NewState(judgment.RedactRepoText)
	if err := st.TrustedFact("plan_files", strings.Join(planFiles, ", ")); err != nil {
		logf("progress: %v", err)
		res.SkipReason = "state_error"
		return res, judgment.Unmet(ProgressSite, judgment.FailureRefused, err.Error())
	}
	if err := st.TrustedFact("plan_symbols", strings.Join(planSymbols, ", ")); err != nil {
		logf("progress: %v", err)
		res.SkipReason = "state_error"
		return res, judgment.Unmet(ProgressSite, judgment.FailureRefused, err.Error())
	}
	items := make([]*judgment.RepoItem, 0, len(window))
	for n, ev := range window {
		path := ev.Path
		if path == "" {
			// A search has no path; NewRepoItem needs one to key the record
			// on, so a synthetic label is used. It still goes through the
			// same eligibility check every origin does — a label is never
			// assumed safe just because it is not a real path.
			path = "search:" + strconv.Itoa(n)
		}
		item, err := st.NewRepoItem("w"+strconv.Itoa(n), path)
		if err != nil {
			continue
		}
		item.Meta("tool", ev.Tool).Meta("seq", strconv.Itoa(ev.Seq))
		if ev.Empty {
			item.Meta("empty", "true")
		}
		if len(ev.Decls) > 0 {
			item.Meta("declares", strings.Join(ev.Decls, ", "))
		}
		if ev.Detail != "" {
			_ = item.Text("detail", ev.Detail, st)
		}
		if ev.Excerpt != "" {
			_ = item.Text("excerpt", ev.Excerpt, st)
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		// There was a window and none of it could be put into a question.
		res.SkipReason = "no_eligible_evidence"
		return res, judgment.Unmet(ProgressSite, judgment.FailureRefused,
			"no evidence in the window could be sent under the configured redact mode")
	}
	if err := st.AddItems("window", items); err != nil {
		logf("progress: %v", err)
		res.SkipReason = "state_error"
		return res, judgment.Unmet(ProgressSite, judgment.FailureRefused, err.Error())
	}

	answers, note, err := judgment.RequireAll(ctx, j, ProgressSite, st, map[string]judgment.Question{
		"category": judgment.Choice(progressProposition(), progressCriteria),
	})
	res.Requests += note.Usage.Requests
	res.InputTokens += note.Usage.InputTokens
	res.OutputTokens += note.Usage.OutputTokens
	if err != nil {
		return res, err
	}

	a := answers["category"]
	if !a.Answered {
		return res, judgment.Unmet(ProgressSite, judgment.FailureInvalid,
			"the response carried no category")
	}
	// The operator's confidence floor. AskAll cannot apply it to a batch, so
	// a site that reads a Choice applies it here; without this the floor
	// would govern advisory sites and not the ones that change control flow.
	if floor := judgment.MinConfidenceOf(j); a.Confidence < floor {
		res.SkipReason = "low_confidence"
		return res, judgment.Unmet(ProgressSite, judgment.FailureLowConfidence,
			fmt.Sprintf("confidence %.2f below the configured floor %.2f", a.Confidence, floor))
	}
	res.Applied = true
	res.Category = ProgressCategory(a.Choice)
	res.Confidence = a.Confidence
	return res, nil
}

// recentReads returns the last n reads by Seq, oldest first — the order a
// reader (or a judgment) wants a narrative in.
func recentReads(reads []ReadEvidence, n int) []ReadEvidence {
	sorted := append([]ReadEvidence(nil), reads...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Seq < sorted[j].Seq })
	if len(sorted) > n {
		sorted = sorted[len(sorted)-n:]
	}
	return sorted
}
