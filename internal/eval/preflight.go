package eval

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/retrieval"
	"github.com/akynte/boundedcode/internal/task"
)

// What must be true before a benchmark is worth running.
//
// The distinction this enforces is the one that decides whether a number
// means anything:
//
//	production: the judge is unavailable  → fall back, carry on
//	benchmark:  the treatment is unavailable → the experiment is invalid, stop
//
// Falling back is correct behaviour in a task and a silent lie in an
// experiment. An arm named supervised-rerank that quietly produced the
// baseline's ordering would publish the baseline's numbers under the
// treatment's name, and nothing downstream could tell.

// PreflightLevel says how a finding should be read.
type PreflightLevel string

const (
	// PreflightOK: nothing to say.
	PreflightOK PreflightLevel = "ok"
	// PreflightWarn: the run will proceed and a reader should know.
	PreflightWarn PreflightLevel = "warn"
	// PreflightBlock: the run would produce a number nobody can interpret.
	PreflightBlock PreflightLevel = "block"
)

// PreflightCheck is one finding.
type PreflightCheck struct {
	Name   string         `json:"name"`
	Level  PreflightLevel `json:"level"`
	Detail string         `json:"detail"`
	Fix    string         `json:"fix,omitempty"`
}

// Preflight is the whole report.
type Preflight struct {
	Checks []PreflightCheck `json:"checks"`
	// Provenance is what the run would record, including the experiment id,
	// so an operator sees the identity before spending hours producing
	// numbers under it.
	Provenance Provenance `json:"provenance"`
}

// Blocked reports whether anything would invalidate the experiment.
func (p Preflight) Blocked() bool {
	for _, c := range p.Checks {
		if c.Level == PreflightBlock {
			return true
		}
	}
	return false
}

// Blockers lists the reasons, for an error message.
func (p Preflight) Blockers() []string {
	var out []string
	for _, c := range p.Checks {
		if c.Level == PreflightBlock {
			out = append(out, c.Name+": "+c.Detail)
		}
	}
	return out
}

// PreflightInput is what the caller knows before anything runs.
type PreflightInput struct {
	Tasks []Task
	Arms  []Arm
	Set   string
	// TaskDir is where the tasks and their annotations live, for the
	// integrity scan.
	TaskDir string
	// Benchmark raises the bar. Outside it, a missing treatment is a warning
	// and the run proceeds; inside it, the same finding stops the run.
	Benchmark bool
	// RequireClean refuses a dirty working tree, for a run whose numbers will
	// be published.
	RequireClean bool
	// JudgeAvailable and SmokeErr describe the external judge. SmokeErr
	// non-nil means an authenticated minimal call was attempted and failed.
	JudgeAvailable bool
	SmokeErr       error
	SmokeServed    string
	// EmbeddingAvailable describes the local control arm's provider.
	EmbeddingAvailable bool
	// Annotated is the annotation coverage across the selected tasks.
	Annotated AnnotationStatus
	// HeldoutRequired is the qualification bar, so preflight can say how far
	// short a held-out run is before it starts rather than after.
	HeldoutRequired int
	// Admissions are the per-task eligibility results.
	Admissions []Admission
	// Reliability is annotator agreement across the set.
	Reliability ReliabilityReport
	// Provenance is the frozen configuration, already finalised.
	Provenance Provenance
}

// RunPreflight checks a configuration against what it is about to measure.
func RunPreflight(ctx context.Context, in PreflightInput) Preflight {
	p := Preflight{Provenance: in.Provenance}
	add := func(name string, level PreflightLevel, detail, fix string) {
		p.Checks = append(p.Checks, PreflightCheck{Name: name, Level: level, Detail: detail, Fix: fix})
	}
	// Outside a benchmark a missing treatment is worth saying and not worth
	// stopping for; inside one it is the whole problem.
	severity := func(benchmarkLevel PreflightLevel) PreflightLevel {
		if in.Benchmark {
			return benchmarkLevel
		}
		return PreflightWarn
	}

	// --- code revision ---
	switch {
	case in.Provenance.Dirty && in.RequireClean:
		add("working tree", PreflightBlock,
			"uncommitted changes; the recorded commit does not describe the code that would run",
			"Commit or stash, or drop --require-clean if these numbers will not be published.")
	case in.Provenance.Dirty:
		add("working tree", PreflightWarn,
			"uncommitted changes; the recorded commit does not describe the code that would run",
			"Pass --require-clean for a run whose numbers will be published.")
	default:
		add("working tree", PreflightOK, "clean at "+shortSHA(in.Provenance.Commit), "")
	}

	// --- dataset ---
	add("dataset", PreflightOK, fmt.Sprintf("%d task(s), digest %s",
		len(in.Tasks), shortSHA(in.Provenance.DatasetDigest)), "")

	sets := map[Set]int{}
	for _, t := range in.Tasks {
		sets[t.Membership()]++
	}
	add("sets", PreflightOK, fmt.Sprintf("dev=%d heldout=%d", sets[SetDev], sets[SetHeldout]), "")

	// --- ground truth ---
	switch {
	case in.Annotated.Annotated == 0:
		add("localization ground truth", severity(PreflightBlock),
			fmt.Sprintf("0 of %d task(s) carry usable labels; no localization metric can "+
				"be computed", in.Annotated.Total),
			"`bcode eval annotate <task>` walks one task by hand. `bcode eval rubric` says what "+
				"the labels mean. Nothing may infer them.")
	case in.Annotated.Annotated < in.Annotated.Total:
		add("localization ground truth", PreflightWarn,
			fmt.Sprintf("%d of %d task(s) carry labels; the rest are unscorable and "+
				"contribute to the solve rate only",
				in.Annotated.Annotated, in.Annotated.Total), "")
	default:
		add("localization ground truth", PreflightOK,
			fmt.Sprintf("%d task(s), rubric %s", in.Annotated.Annotated, AnnotationRubricVersion), "")
	}
	if in.Annotated.Stale > 0 {
		add("stale annotations", PreflightWarn,
			fmt.Sprintf("%d annotation(s) predate the current rubric or an edited task and "+
				"are ignored", in.Annotated.Stale),
			"Re-run `bcode eval annotate` on: "+strings.Join(in.Annotated.StaleTasks, ", "))
	}

	// --- held-out sufficiency ---
	if in.Set == string(SetHeldout) && in.HeldoutRequired > 0 && sets[SetHeldout] < in.HeldoutRequired {
		add("held-out size", severity(PreflightBlock),
			fmt.Sprintf("%d held-out task(s); qualification requires %d",
				sets[SetHeldout], in.HeldoutRequired),
			"Below the bar a single task flipping moves the headline rate by more than "+
				"any effect worth claiming.")
	}

	// --- arms and their treatments ---
	var wantsJudge, wantsLocal bool
	names := make([]string, 0, len(in.Arms))
	for _, a := range in.Arms {
		names = append(names, a.Name)
		switch a.Rerank {
		case RerankJudged:
			wantsJudge = true
		case RerankLocal:
			wantsLocal = true
		}
	}
	sort.Strings(names)
	add("arms", PreflightOK, strings.Join(names, ", "), "")

	if wantsJudge {
		switch {
		case !in.JudgeAvailable:
			add("external judge", severity(PreflightBlock),
				"an arm reranks with the external judge and none is configured or reachable",
				"Configure judgment.yaml and export the key, or drop the supervised-rerank arm.")
		case in.SmokeErr != nil:
			add("external judge", severity(PreflightBlock),
				"the authenticated smoke call failed: "+in.SmokeErr.Error(),
				"A benchmark must not start against a model the service has not accepted. "+
					"`bcode judgment smoke` reproduces this on its own.")
		case !in.Provenance.JudgmentModelPinned:
			add("external judge", severity(PreflightBlock),
				fmt.Sprintf("the model %q is an alias, so the run could not say which "+
					"model produced its numbers", in.Provenance.JudgmentModelRequested),
				"Pin a concrete version in judgment.yaml.")
		case in.SmokeServed != "" && in.SmokeServed != in.Provenance.JudgmentModelRequested:
			add("external judge", severity(PreflightBlock),
				fmt.Sprintf("the service ran %s, not the requested %s",
					in.SmokeServed, in.Provenance.JudgmentModelRequested), "")
		case in.SmokeServed == "" && in.Provenance.AllowUnverifiedModel:
			add("served model identity", PreflightWarn,
				string(ModelIdentityUnverifiable)+": the service named no model, and "+
					"--allow-unverified-model was passed",
				"This run will proceed and will NOT be publishable. A pinned request "+
					"identifier and a verified served identifier are two different pieces "+
					"of evidence; only the first is established here.")
		case in.SmokeServed == "":
			// The smoke call proves the identifier was accepted. It does not
			// prove the identifier served the request, and the vendor
			// documents no guarantee that it did — so a publication
			// benchmark cannot say which model produced its numbers.
			add("served model identity", severity(PreflightBlock),
				fmt.Sprintf("%s: %s was accepted but the service named no model in its "+
					"response", ModelIdentityUnverifiable, in.Provenance.JudgmentModelRequested),
				"Acceptance of an identifier is not evidence that the identifier served "+
					"the request. Pass --allow-unverified-model for a development run, "+
					"whose results are marked non-publishable.")
		default:
			add("external judge", PreflightOK,
				fmt.Sprintf("%s (served %s), smoke call accepted",
					in.Provenance.JudgmentModelRequested, in.SmokeServed), "")
		}
	}

	if wantsLocal {
		if !in.EmbeddingAvailable {
			add("local semantic control", severity(PreflightBlock),
				"an arm reranks locally and no embedding provider is configured or reachable",
				"Route the embedding role in providers.yaml, or drop the "+
					"supervised-rerank-local arm.")
		} else {
			add("local semantic control", PreflightOK,
				"embedding cosine over "+orUnknown(in.Provenance.EmbeddingModel), "")
		}
	}

	// --- task eligibility ---
	if len(in.Admissions) > 0 {
		summary := Summarise(in.Admissions)
		switch {
		case summary.Ineligible > 0:
			var names []string
			for _, a := range in.Admissions {
				if !a.Eligible() {
					names = append(names, a.TaskID)
				}
			}
			sort.Strings(names)
			add("task eligibility", severity(PreflightBlock),
				fmt.Sprintf("%d of %d task(s) fail the admission protocol: %s",
					summary.Ineligible, summary.Total, strings.Join(names, ", ")),
				"`bcode eval admit` reports which rule and why. A benchmark whose tasks "+
					"cannot be shown to be real engineering problems measures its own author.")
		case summary.NeedReview > 0:
			add("task eligibility", PreflightWarn,
				fmt.Sprintf("%d of %d task(s) have findings this code cannot decide",
					summary.NeedReview, summary.Total),
				"`bcode eval admit` lists them. They are judgments about the work, not "+
					"something a checker can confirm.")
		default:
			add("task eligibility", PreflightOK,
				fmt.Sprintf("%d task(s) admitted", summary.Eligible), "")
		}
	}

	// --- snapshot integrity ---
	var broken []string
	var checked int
	for _, t := range in.Tasks {
		if strings.TrimSpace(t.Fixture) == "" {
			// Nothing on disk to verify. Whether that is acceptable is the
			// admission protocol's question, not this one's.
			continue
		}
		checked++
		w := CheckWorkspace(ctx, in.TaskDir, t, t.FixturePath())
		if !w.OK() {
			broken = append(broken, t.ID+": "+strings.Join(w.Problems, "; "))
		}
	}
	if len(broken) > 0 {
		add("snapshot integrity", severity(PreflightBlock),
			fmt.Sprintf("%d task(s) cannot be shown to start from their declared pre-fix "+
				"state, or hold gold material where the solver can reach it", len(broken)),
			strings.Join(broken, " | "))
	} else if checked > 0 {
		add("snapshot integrity", PreflightOK,
			fmt.Sprintf("%d fixture(s) verified against their declared base", checked), "")
	}

	// --- declared family collisions ---
	//
	// A family is somebody's explicit statement that two tasks are
	// variations of one problem. One member in dev and another in held-out
	// means tuning on the first partly tunes on the second, and the held-out
	// number is not held out. Heuristic near-duplicates stay advisory; this
	// is the declared case, and it is refused rather than reported.
	audit := AuditSplit(in.Tasks)
	switch {
	case len(audit.SplitFamilies) > 0 && in.Set == string(SetHeldout):
		add("declared families", severity(PreflightBlock),
			fmt.Sprintf("%d declared family/families span dev and held-out",
				len(audit.SplitFamilies)),
			"A curator must resolve this before tuning or final evaluation, by moving "+
				"the whole family to one side and re-freezing the split — never after "+
				"seeing results. "+strings.Join(audit.SplitFamilies, "; "))
	case len(audit.SplitFamilies) > 0:
		add("declared families", PreflightWarn,
			fmt.Sprintf("%d declared family/families span dev and held-out",
				len(audit.SplitFamilies)),
			"Held-out evaluation will refuse this. `bcode eval audit` lists them.")
	}

	// --- annotator agreement and gap closure ---
	if in.Set == string(SetHeldout) {
		switch {
		case in.Reliability.UnresolvedGaps > 0:
			add("annotation gaps", severity(PreflightBlock),
				fmt.Sprintf("%d file(s) across %d task(s) were considered by one annotator "+
					"and never resolved", in.Reliability.UnresolvedGaps,
					len(in.Reliability.OpenTasks)),
				"A gap is not a disagreement, and it is not nothing either: the union of "+
					"files the annotators considered must carry two matching labels or an "+
					"adjudicated one. `bcode eval adjudicate <task>`.")
		case len(in.Reliability.Unadjudicated) > 0:
			add("annotator agreement", severity(PreflightBlock),
				fmt.Sprintf("%d held-out task(s) have unresolved disagreements",
					len(in.Reliability.Unadjudicated)),
				"Publication-quality labels are read twice and the differences "+
					"adjudicated: "+strings.Join(in.Reliability.Unadjudicated, ", "))
		case in.Reliability.Compared > 0:
			add("annotator agreement", PreflightOK,
				fmt.Sprintf("%.0f%% raw over %d jointly labelled file(s), %d task(s) closed",
					in.Reliability.RawAgreement*100, in.Reliability.Compared,
					in.Reliability.FullyClosed), "")
		}
	}

	// --- cache ---
	if caveat := in.Provenance.Cache.CostCaveat(); caveat != "" {
		add("cache", PreflightWarn, in.Provenance.Cache.Describe(), caveat)
	} else {
		add("cache", PreflightOK, in.Provenance.Cache.Describe(), "")
	}

	// --- information parity ---
	parity := DescribeEvidence(in.Arms, in.Provenance.Redact)
	if len(parity.Unequal) > 0 && len(in.Arms) > 1 {
		add("information parity", PreflightWarn,
			fmt.Sprintf("%d arm pair(s) do not see the same evidence", len(parity.Unequal)),
			"`bcode eval parity` prints the table. Those comparisons are system-level: "+
				"a conclusion of the form \"X is a better reranker\" is not available "+
				"for them.")
	} else if len(parity.Comparable) > 0 {
		add("information parity", PreflightOK,
			fmt.Sprintf("%d arm pair(s) see the same evidence", len(parity.Comparable)), "")
	}

	// --- models and tuning ---
	add("reasoning model", PreflightOK, orUnknown(in.Provenance.ReasoningModel), "")
	add("frozen tuning", PreflightOK, describeTuning(in.Provenance), "")
	add("experiment", PreflightOK, in.Provenance.ExperimentID, "")
	return p
}

// Zero values for the tuning structs, so describeTuning can tell "shipped
// defaults" from "overridden" without reflection.
var (
	zeroRerankTuning   = retrieval.RerankTuning{}
	zeroLocalizeTuning = task.LocalizeTuning{}
)

func describeTuning(p Provenance) string {
	var parts []string
	if p.Tuning != (zeroRerankTuning) {
		parts = append(parts, "rerank overridden")
	}
	if p.LocalizeTuning != (zeroLocalizeTuning) {
		parts = append(parts, "localize overridden")
	}
	if len(parts) == 0 {
		return "shipped defaults"
	}
	return strings.Join(parts, ", ")
}

func orUnknown(s string) string {
	if s == "" {
		return "(not recorded)"
	}
	return s
}

func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	if s == "" {
		return "(none)"
	}
	return s
}

// Format renders the preflight for a terminal.
func (p Preflight) Format() string {
	var b strings.Builder
	b.WriteString("Benchmark preflight\n\n")
	for _, c := range p.Checks {
		mark := map[PreflightLevel]string{
			PreflightOK: "ok  ", PreflightWarn: "warn", PreflightBlock: "STOP",
		}[c.Level]
		fmt.Fprintf(&b, "[%s] %-28s %s\n", mark, c.Name, c.Detail)
		for _, line := range wrapPreflight(c.Fix, 62) {
			fmt.Fprintf(&b, "       %-28s %s\n", "", line)
		}
	}
	if p.Blocked() {
		b.WriteString("\nThis configuration would produce numbers nobody can interpret.\n" +
			"In a benchmark a missing treatment is not something to fall back from:\n" +
			"an arm that quietly produced the baseline's ordering would publish the\n" +
			"baseline's result under the treatment's name.\n")
	}
	return b.String()
}

func wrapPreflight(s string, width int) []string {
	if s == "" {
		return nil
	}
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		if line == "" {
			line = word
			continue
		}
		if len(line)+1+len(word) > width {
			out = append(out, line)
			line = word
			continue
		}
		line += " " + word
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}
