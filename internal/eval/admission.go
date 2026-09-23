package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The task-admission protocol.
//
// A benchmark of engineering assistance is only as good as its claim to
// contain engineering problems. The failure mode is quiet and total: tasks
// written to be solvable measure whether the system can do what their author
// imagined, the numbers look fine, and nobody can tell afterwards. The
// protocol below is what makes that claim checkable.
//
// Two things it deliberately does not do. It does not turn a commit into a
// task — most commits are not self-contained problems, and a harness that
// mined them would fill the set with renames and dependency bumps. And it
// does not admit anything on its own: every rule reports evidence for a
// person, and the ones that need judgment say so rather than guessing.

// AdmissionRule identifies one requirement.
type AdmissionRule string

const (
	RuleReproducibleBase AdmissionRule = "reproducible_base_state"
	RuleRealOrigin       AdmissionRule = "objective_from_real_history"
	RuleKnownOutcome     AdmissionRule = "known_correct_outcome"
	RuleAcceptance       AdmissionRule = "reproducible_acceptance"
	RuleNoExternalDeps   AdmissionRule = "no_mandatory_external_dependency"
	RuleNoFutureLeak     AdmissionRule = "no_future_fix_visible"
	RuleObjectiveSilent  AdmissionRule = "objective_does_not_name_the_fix"
)

// AdmissionVerdict is how one rule came out.
type AdmissionVerdict string

const (
	// AdmissionPass: the evidence is present and checkable.
	AdmissionPass AdmissionVerdict = "pass"
	// AdmissionFail: the task is not eligible.
	AdmissionFail AdmissionVerdict = "fail"
	// AdmissionReview: this code cannot decide, and a person must.
	//
	// The category exists because collapsing it into either of the others is
	// how a protocol becomes theatre. "A human says this objective came from
	// real history" is not something a checker can confirm, and reporting it
	// as a pass would launder an unverified claim into an automated verdict.
	AdmissionReview AdmissionVerdict = "needs_review"
)

// AdmissionFinding is one rule's outcome for one task.
type AdmissionFinding struct {
	Rule     AdmissionRule    `json:"rule"`
	Verdict  AdmissionVerdict `json:"verdict"`
	Detail   string           `json:"detail"`
	Evidence string           `json:"evidence,omitempty"`
}

// Admission is the whole report for one task.
type Admission struct {
	TaskID   string             `json:"task_id"`
	Set      Set                `json:"set"`
	Findings []AdmissionFinding `json:"findings"`
}

// Eligible reports whether nothing failed. It is not the same as admitted: a
// task with outstanding review items is eligible only once a person has
// signed off on them, which is why Review is counted separately.
func (a Admission) Eligible() bool {
	for _, f := range a.Findings {
		if f.Verdict == AdmissionFail {
			return false
		}
	}
	return true
}

// NeedsReview counts the judgments this code declined to make.
func (a Admission) NeedsReview() int {
	n := 0
	for _, f := range a.Findings {
		if f.Verdict == AdmissionReview {
			n++
		}
	}
	return n
}

// Failures lists the reasons a task is not eligible.
func (a Admission) Failures() []string {
	var out []string
	for _, f := range a.Findings {
		if f.Verdict == AdmissionFail {
			out = append(out, string(f.Rule)+": "+f.Detail)
		}
	}
	return out
}

// Admit checks one task against the protocol.
//
// heldout raises the bar: a synthetic task is a legitimate dev fixture and
// not a publishable held-out result, and an unreviewed origin is tolerable
// while tuning and not in a number anybody quotes.
func Admit(taskDir string, t Task) Admission {
	a := Admission{TaskID: t.ID, Set: t.Membership()}
	heldout := t.Membership() == SetHeldout
	add := func(rule AdmissionRule, v AdmissionVerdict, detail, evidence string) {
		a.Findings = append(a.Findings, AdmissionFinding{
			Rule: rule, Verdict: v, Detail: detail, Evidence: evidence,
		})
	}

	// 1. A reproducible pre-fix state. The fixture must exist and, for a
	//    historical task, must say which revision it is.
	fixture := t.FixturePath()
	switch info, err := os.Stat(fixture); {
	case err != nil:
		add(RuleReproducibleBase, AdmissionFail, "the fixture directory is missing", fixture)
	case !info.IsDir():
		add(RuleReproducibleBase, AdmissionFail, "the fixture is not a directory", fixture)
	case t.Origin.BaseRevision == "":
		v := AdmissionReview
		detail := "the fixture exists but names no base revision, so the starting state " +
			"cannot be reconstructed from the project it came from"
		if t.Origin.Synthetic {
			v, detail = AdmissionPass, "synthetic fixture; the directory is the whole state"
		} else if heldout {
			v = AdmissionFail
		}
		add(RuleReproducibleBase, v, detail, fixture)
	default:
		add(RuleReproducibleBase, AdmissionPass, "base "+shortRev(t.Origin.BaseRevision), fixture)
	}

	// 2. An objective derived from real history rather than written to be
	//    solvable. Nothing here can confirm that; it can only report what
	//    the author claimed and what evidence they left.
	switch {
	case t.Origin.Empty():
		v := AdmissionReview
		if heldout {
			v = AdmissionFail
		}
		add(RuleRealOrigin, v, "no origin recorded; nobody can tell whether this is a real "+
			"engineering problem or one written for the benchmark", "")
	case t.Origin.Synthetic && heldout:
		add(RuleRealOrigin, AdmissionFail,
			"synthetic tasks are dev fixtures; a held-out number quoted from one describes "+
				"the benchmark's author rather than the project", "")
	case t.Origin.Synthetic:
		add(RuleRealOrigin, AdmissionPass, "declared synthetic; dev only", "")
	case !t.Origin.Derived:
		add(RuleRealOrigin, AdmissionReview,
			"the origin is recorded but derived_from_history is not set; a reviewer should "+
				"confirm the objective was written from the real change",
			t.Origin.Reference)
	default:
		add(RuleRealOrigin, AdmissionPass, "derived from history", originEvidence(t.Origin))
	}

	// 3. A known correct outcome. Either a fixing revision, or an acceptance
	//    check strong enough to stand in for one.
	switch {
	case t.Origin.GoldRevision != "":
		add(RuleKnownOutcome, AdmissionPass, "gold "+shortRev(t.Origin.GoldRevision), t.Origin.Reference)
	case len(t.Acceptance.Argv) > 0 && len(t.Acceptance.Files) > 0:
		add(RuleKnownOutcome, AdmissionPass,
			"no fixing revision, but the hidden acceptance files define the outcome", "")
	case heldout:
		add(RuleKnownOutcome, AdmissionFail, "neither a fixing revision nor hidden acceptance "+
			"files; there is nothing to be correct against", "")
	default:
		add(RuleKnownOutcome, AdmissionReview, "no fixing revision recorded", "")
	}

	// 4. Reproducible acceptance evidence.
	switch {
	case len(t.Acceptance.Argv) == 0:
		add(RuleAcceptance, AdmissionFail, "no acceptance command; the only signal would be "+
			"the model's own claim, which is what this measures against", "")
	case t.Acceptance.TimeoutSeconds <= 0:
		add(RuleAcceptance, AdmissionFail, "the acceptance check has no timeout", "")
	case len(t.Acceptance.Files) == 0:
		add(RuleAcceptance, AdmissionReview,
			"the acceptance command runs against the worktree with no hidden files; confirm "+
				"a solution cannot satisfy it by editing the test", strings.Join(t.Acceptance.Argv, " "))
	default:
		add(RuleAcceptance, AdmissionPass,
			fmt.Sprintf("%d hidden file(s), `%s`", len(t.Acceptance.Files),
				strings.Join(t.Acceptance.Argv, " ")), "")
	}

	// 5. No mandatory external dependency. A task that needs a network
	//    service is one nobody else can reproduce, and the sandbox has no
	//    route out in any case.
	if len(t.Origin.ExternalDependencies) > 0 {
		add(RuleNoExternalDeps, AdmissionFail,
			"the acceptance check needs something outside the fixture, so nobody else can "+
				"reproduce it — and a task sandbox has no network in any configuration",
			strings.Join(t.Origin.ExternalDependencies, ", "))
	} else {
		add(RuleNoExternalDeps, AdmissionPass, "nothing outside the fixture is declared", "")
	}

	// 6. No future-fix information visible to the solver. Checked for real
	//    rather than asserted: see integrity.go.
	if leaks := ScanFixtureForLeaks(taskDir, t); len(leaks) > 0 {
		add(RuleNoFutureLeak, AdmissionFail,
			fmt.Sprintf("%d item(s) in the solver-visible fixture reveal the fix or the "+
				"ground truth", len(leaks)), strings.Join(leaks, "; "))
	} else {
		add(RuleNoFutureLeak, AdmissionPass, "no gold or annotation material inside the fixture", "")
	}

	// 7. The objective must not name the fix. A crude check, and it catches
	//    the crude mistake: an objective that reads like a patch note.
	if hits := objectiveNamesTheFix(t.Objective); len(hits) > 0 {
		add(RuleObjectiveSilent, AdmissionReview,
			"the objective may state the change rather than the problem; a task that says "+
				"what to do measures typing", strings.Join(hits, ", "))
	} else {
		add(RuleObjectiveSilent, AdmissionPass, "states a problem, not a change", "")
	}
	return a
}

// objectiveNamesTheFix looks for an objective written as an instruction to
// make a specific edit. It is a prompt for review, never a refusal: "change
// the default to 30" is sometimes exactly what a user would say.
func objectiveNamesTheFix(objective string) []string {
	lower := strings.ToLower(objective)
	var hits []string
	for _, phrase := range []string{
		"replace the", "change the line", "rename the function", "add a call to",
		"delete the line", "swap the", "the bug is", "the fix is", "on line ",
	} {
		if strings.Contains(lower, phrase) {
			hits = append(hits, phrase)
		}
	}
	return hits
}

func originEvidence(o TaskOrigin) string {
	var parts []string
	if o.Repository != "" {
		parts = append(parts, o.Repository)
	}
	if o.Date != "" {
		parts = append(parts, o.Date)
	}
	if o.Reference != "" {
		parts = append(parts, o.Reference)
	}
	return strings.Join(parts, " · ")
}

func shortRev(r string) string {
	if len(r) > 12 {
		return r[:12]
	}
	return r
}

// AdmitAll checks a whole set.
func AdmitAll(taskDir string, tasks []Task) []Admission {
	out := make([]Admission, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, Admit(taskDir, t))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TaskID < out[j].TaskID })
	return out
}

// AdmissionSummary counts a set's eligibility.
type AdmissionSummary struct {
	Total      int            `json:"total"`
	Eligible   int            `json:"eligible"`
	Ineligible int            `json:"ineligible"`
	NeedReview int            `json:"need_review"`
	BySet      map[Set]int    `json:"eligible_by_set"`
	ByRule     map[string]int `json:"failures_by_rule"`
}

// Summarise counts admissions.
func Summarise(admissions []Admission) AdmissionSummary {
	s := AdmissionSummary{Total: len(admissions), BySet: map[Set]int{}, ByRule: map[string]int{}}
	for _, a := range admissions {
		if a.Eligible() {
			s.Eligible++
			s.BySet[a.Set]++
		} else {
			s.Ineligible++
		}
		if a.NeedsReview() > 0 {
			s.NeedReview++
		}
		for _, f := range a.Findings {
			if f.Verdict == AdmissionFail {
				s.ByRule[string(f.Rule)]++
			}
		}
	}
	return s
}

// fixtureRelative reports whether a path sits inside the fixture the solver
// gets. Shared by the admission scan and the integrity check.
func fixtureRelative(fixture, path string) bool {
	fixture, err := filepath.Abs(fixture)
	if err != nil {
		return false
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return false
	}
	return path == fixture || strings.HasPrefix(path, fixture+string(filepath.Separator))
}
