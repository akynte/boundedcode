package eval

import (
	"math"
	"strings"
	"testing"
	"time"
)

// --- admission -------------------------------------------------------------

// A task nobody can show is a real engineering problem must not silently
// count as one, and the rules this code cannot decide must say so rather than
// pass.
func TestAdmissionSeparatesFailFromNeedsReview(t *testing.T) {
	dir := t.TempDir()
	fixture := fixtureWith(t, map[string]string{"main.go": "package main"})

	bare := Task{ID: "bare", Fixture: fixture,
		Acceptance: Acceptance{Argv: []string{"true"}, TimeoutSeconds: 10}}
	a := Admit(dir, bare)
	if !a.Eligible() {
		t.Fatalf("a dev task with no origin was refused outright: %v", a.Failures())
	}
	if a.NeedsReview() == 0 {
		t.Fatal("a task with no origin passed with nothing flagged; an unverified claim " +
			"laundered into an automated verdict")
	}

	// The same task in held-out is not admissible: a number quoted from it
	// would describe the benchmark's author.
	bare.Set = SetHeldout
	if Admit(dir, bare).Eligible() {
		t.Fatal("a held-out task with no origin was admitted")
	}
}

func TestAdmissionRefusesWhatCannotBeReproduced(t *testing.T) {
	dir := t.TempDir()
	fixture := fixtureWith(t, map[string]string{"main.go": "package main"})
	for name, mutate := range map[string]func(Task) Task{
		"no acceptance command": func(t Task) Task { t.Acceptance.Argv = nil; return t },
		"no acceptance timeout": func(t Task) Task { t.Acceptance.TimeoutSeconds = 0; return t },
		"a missing fixture":     func(t Task) Task { t.Fixture = "/nonexistent/fixture"; return t },
		"an external dependency": func(t Task) Task {
			t.Origin.ExternalDependencies = []string{"a staging database"}
			return t
		},
	} {
		t.Run(name, func(t2 *testing.T) {
			task := mutate(historicalTask(t2, fixture))
			a := Admit(dir, task)
			if a.Eligible() {
				t2.Fatalf("admitted despite %s", name)
			}
		})
	}
}

func TestAdmissionFlagsAnObjectiveThatNamesTheFix(t *testing.T) {
	dir := t.TempDir()
	task := historicalTask(t, fixtureWith(t, map[string]string{"main.go": "package main"}))
	task.Objective = "replace the call to Foo on line 42 with Bar"
	a := Admit(dir, task)
	var flagged bool
	for _, f := range a.Findings {
		if f.Rule == RuleObjectiveSilent && f.Verdict == AdmissionReview {
			flagged = true
		}
	}
	if !flagged {
		t.Fatal("an objective written as a patch note was not flagged; a task that says " +
			"what to do measures typing")
	}
}

func TestAWellFormedHistoricalTaskIsAdmitted(t *testing.T) {
	dir := t.TempDir()
	task := historicalTask(t, fixtureWith(t, map[string]string{"main.go": "package main"}))
	a := Admit(dir, task)
	if !a.Eligible() {
		t.Fatalf("a complete task was refused: %v", a.Failures())
	}
	if a.NeedsReview() != 0 {
		t.Fatalf("a complete task still needs review: %+v", a.Findings)
	}
}

// --- split protocol --------------------------------------------------------

// Assignment must be a function of the task and the seed, so nobody can
// nudge it after seeing results, and adding a task must not reassign the rest.
func TestAssignmentIsDeterministicAndStable(t *testing.T) {
	ids := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	first := map[string]Set{}
	for _, id := range ids {
		first[id] = AssignSet(7, id, 0.3)
	}
	for _, id := range ids {
		if AssignSet(7, id, 0.3) != first[id] {
			t.Fatalf("%s was assigned differently on a second call", id)
		}
	}
	// Adding a task leaves the others where they were.
	for _, id := range append(ids, "new-task") {
		if want, ok := first[id]; ok && AssignSet(7, id, 0.3) != want {
			t.Fatalf("adding a task reassigned %s", id)
		}
	}
	// A different seed produces a different split, or the seed does nothing.
	var differs bool
	for _, id := range ids {
		if AssignSet(8, id, 0.3) != first[id] {
			differs = true
		}
	}
	if !differs {
		t.Fatal("changing the seed changed no assignment")
	}
}

// The audit must surface a declared family split across the sets, and a
// likely pair the metadata does not declare.
func TestAuditSurfacesLeakageAcrossTheSplit(t *testing.T) {
	tasks := []Task{
		{ID: "dev-1", Set: SetDev, Category: CategoryBugFix,
			Objective: "stock reserved by live orders goes missing during reconciliation",
			Traits:    TaskTraits{Subsystem: "inventory", Family: "reconcile"},
			Expected:  Expected{Files: []string{"internal/inventory/reconcile.go"}}},
		{ID: "held-1", Set: SetHeldout, Category: CategoryBugFix,
			Objective: "reconciliation loses stock reserved by live orders",
			Traits:    TaskTraits{Subsystem: "inventory", Family: "reconcile"},
			Expected:  Expected{Files: []string{"internal/inventory/reconcile.go"}}},
		{ID: "held-2", Set: SetHeldout, Category: CategoryFeature,
			Objective: "chilled goods ship to customers outside the cold zone",
			Origin:    TaskOrigin{Synthetic: true}},
	}
	a := AuditSplit(tasks)

	if len(a.SplitFamilies) != 1 || !strings.Contains(a.SplitFamilies[0], "reconcile") {
		t.Fatalf("a declared family on both sides was not reported: %v", a.SplitFamilies)
	}
	if len(a.CrossSetPairs) == 0 {
		t.Fatal("no related pair was surfaced across the split")
	}
	var found bool
	for _, p := range a.CrossSetPairs {
		if p.A == "dev-1" && p.B == "held-1" {
			found = true
			if p.Score < 0.5 {
				t.Errorf("two near-identical tasks scored only %.2f", p.Score)
			}
		}
	}
	if !found {
		t.Fatalf("the near-duplicate pair was not among %v", a.CrossSetPairs)
	}
	if len(a.SyntheticHeldout) != 1 || a.SyntheticHeldout[0] != "held-2" {
		t.Fatalf("a synthetic held-out task was not reported: %v", a.SyntheticHeldout)
	}
	if len(a.Undeclared) != 1 || a.Undeclared[0] != "held-2" {
		t.Fatalf("a task with no traits was not reported: %v", a.Undeclared)
	}
}

// The audit reports and never reassigns: it returns findings, not tasks.
func TestAuditDoesNotMoveAnything(t *testing.T) {
	tasks := []Task{
		{ID: "a", Set: SetDev, Traits: TaskTraits{Family: "f"}},
		{ID: "b", Set: SetHeldout, Traits: TaskTraits{Family: "f"}},
	}
	AuditSplit(tasks)
	if tasks[0].Set != SetDev || tasks[1].Set != SetHeldout {
		t.Fatal("the audit changed a task's set; whether two tasks are the same problem " +
			"is a judgment about the work")
	}
}

// --- annotator reliability -------------------------------------------------

// A second reading must not be able to see the first, or it is a review
// rather than an independent reading.
func TestAnnotatorsCannotSeeEachOther(t *testing.T) {
	dir := t.TempDir()
	first := Annotation{
		TaskID: "t1", RubricVersion: AnnotationRubricVersion, Annotator: "alice@example.test",
		AnnotatedAt: time.Now().UTC(), Files: map[string]Label{"a.go": LabelRequired},
	}
	if _, err := SaveForAnnotator(dir, first); err != nil {
		t.Fatal(err)
	}
	got, err := LoadForAnnotator(dir, "t1", "bob@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("bob was shown alice's labels: %+v", got.Files)
	}
	mine, err := LoadForAnnotator(dir, "t1", "alice@example.test")
	if err != nil || mine == nil {
		t.Fatalf("alice could not see her own labels: %v", err)
	}
}

// Agreement must distinguish a disagreement from a gap: a file only one
// annotator considered is not a dispute, and counting it as one would punish
// thoroughness.
func TestAgreementSeparatesDisagreementFromGap(t *testing.T) {
	a := Annotation{TaskID: "t", Annotator: "a", Files: map[string]Label{
		"same.go": LabelRequired, "differs.go": LabelRequired, "only-a.go": LabelUseful,
	}}
	b := Annotation{TaskID: "t", Annotator: "b", Files: map[string]Label{
		"same.go": LabelRequired, "differs.go": LabelNotRequired,
	}}
	ag := CompareAnnotations(a, b)
	if ag.Compared != 2 || ag.Agreed != 1 || ag.Disagreed != 1 {
		t.Fatalf("compared=%d agreed=%d disagreed=%d", ag.Compared, ag.Agreed, ag.Disagreed)
	}
	if ag.OnlyOneRated != 1 {
		t.Fatalf("only-one-rated = %d", ag.OnlyOneRated)
	}
	if ag.RawAgreement != 0.5 {
		t.Fatalf("raw agreement = %v", ag.RawAgreement)
	}
}

// Kappa must not be quoted on a sample too small or a distribution too
// skewed to carry it.
func TestKappaIsFlaggedWhenItCannotBeRead(t *testing.T) {
	small := CompareAnnotations(
		Annotation{Annotator: "a", Files: map[string]Label{"x.go": LabelRequired, "y.go": LabelNotRequired}},
		Annotation{Annotator: "b", Files: map[string]Label{"x.go": LabelRequired, "y.go": LabelNotRequired}},
	)
	if small.KappaInterpretable {
		t.Fatal("kappa over two files was reported as interpretable")
	}
	if small.KappaCaveat == "" {
		t.Fatal("no caveat was given")
	}

	// One category for everything: kappa is undefined, and reporting zero
	// would read as no agreement when what happened is total agreement.
	files := map[string]Label{}
	for i := range 40 {
		files[string(rune('a'+i%26))+string(rune('0'+i/26))+".go"] = LabelNotRequired
	}
	degenerate := CompareAnnotations(
		Annotation{Annotator: "a", Files: files},
		Annotation{Annotator: "b", Files: files},
	)
	if degenerate.KappaInterpretable {
		t.Fatal("kappa on a single-category set was reported as interpretable")
	}
	if degenerate.RawAgreement != 1 {
		t.Fatalf("raw agreement = %v; the honest figure here is 1", degenerate.RawAgreement)
	}
}

// --- calibration -----------------------------------------------------------

// USEFUL must not be forced into either class: the figure would become an
// artefact of the coercion.
func TestCalibrationExcludesUsefulRatherThanCoercingIt(t *testing.T) {
	points := []CalibrationPoint{
		{Score: 0.9, Label: LabelRequired}, {Score: 0.8, Label: LabelRequired},
		{Score: 0.1, Label: LabelNotRequired}, {Score: 0.2, Label: LabelNotRequired},
		{Score: 0.5, Label: LabelUseful}, {Score: 0.6, Label: LabelUseful},
	}
	c := Calibrate("all", points, 5)
	if c.Positives != 2 || c.Negatives != 2 || c.Excluded != 2 {
		t.Fatalf("positives=%d negatives=%d excluded=%d", c.Positives, c.Negatives, c.Excluded)
	}
	if c.BaseRate != 0.5 {
		t.Fatalf("base rate = %v; USEFUL must not be in the denominator", c.BaseRate)
	}
	if c.MeanUseful < 0.54 || c.MeanUseful > 0.56 {
		t.Fatalf("the USEFUL distribution was not reported: %v", c.MeanUseful)
	}
}

// A Brier score is unreadable without the baseline it is compared against.
func TestCalibrationReportsSkillAgainstTheBaseRate(t *testing.T) {
	// A model that always says 0.5 on a balanced set scores 0.25 — exactly
	// the baseline, so no skill.
	var points []CalibrationPoint
	for i := range 60 {
		label := LabelRequired
		if i%2 == 0 {
			label = LabelNotRequired
		}
		points = append(points, CalibrationPoint{Score: 0.5, Label: label})
	}
	c := Calibrate("all", points, 10)
	if math.Abs(c.Brier-0.25) > 0.001 {
		t.Fatalf("Brier = %v, want 0.25", c.Brier)
	}
	if math.Abs(c.Skill) > 0.001 {
		t.Fatalf("skill = %v; predicting the base rate has none", c.Skill)
	}
	if c.Separation != 0 {
		t.Fatalf("separation = %v for a constant predictor", c.Separation)
	}
}

// A skewed base rate makes every calibration figure misleading, and the
// report must say so rather than letting it be quoted.
func TestCalibrationFlagsASkewedBaseRate(t *testing.T) {
	var points []CalibrationPoint
	for i := range 100 {
		label := LabelNotRequired
		score := 0.02
		if i == 0 {
			label, score = LabelRequired, 0.9
		}
		points = append(points, CalibrationPoint{Score: score, Label: label})
	}
	c := Calibrate("all", points, 10)
	if c.ECEInterpretable {
		t.Fatal("ECE was reported as interpretable at a 1% base rate")
	}
	if !strings.Contains(c.Caveat, "base rate") {
		t.Fatalf("caveat = %q", c.Caveat)
	}
}

// The cross-origin assumption is the one the whole ordering rests on, so the
// diagnostic must split by origin.
func TestCalibrationStratifiesByOrigin(t *testing.T) {
	points := []CalibrationPoint{
		{Score: 0.9, Label: LabelRequired, Origin: "lexical_anchor"},
		{Score: 0.1, Label: LabelNotRequired, Origin: "lexical_anchor"},
		{Score: 0.4, Label: LabelRequired, Origin: "graph_expansion"},
		{Score: 0.3, Label: LabelNotRequired, Origin: "graph_expansion"},
	}
	results := CalibrationByOrigin(points, 5)
	if len(results) != 3 {
		t.Fatalf("got %d populations, want all + two origins", len(results))
	}
	if results[0].Population != "all" {
		t.Fatalf("the first population is %q", results[0].Population)
	}
	byName := map[string]Calibration{}
	for _, c := range results {
		byName[c.Population] = c
	}
	lex, graph := byName["lexical_anchor"], byName["graph_expansion"]
	if lex.Separation <= graph.Separation {
		t.Fatalf("the two origins separate identically (%.2f vs %.2f); this fixture was "+
			"built so they do not, so the split is not happening",
			lex.Separation, graph.Separation)
	}
}

// --- cache -----------------------------------------------------------------

// A warm cache disqualifies the cost figures and not the quality ones, and
// the report must distinguish them.
func TestWarmCacheDisqualifiesCostButNotQuality(t *testing.T) {
	warm := DefaultCachePolicy()
	if !warm.AffectsCost() {
		t.Fatal("a warm judgment cache was reported as not affecting cost")
	}
	if !strings.Contains(warm.CostCaveat(), "understate the real cost") {
		t.Fatalf("caveat = %q", warm.CostCaveat())
	}
	if !strings.Contains(warm.CostCaveat(), "Semantic quality is unaffected") {
		t.Fatal("the caveat does not say what remains valid, so a reader discards " +
			"everything")
	}
	cold := ColdCachePolicy()
	if cold.AffectsCost() || cold.CostCaveat() != "" {
		t.Fatal("a cold cache was reported as affecting cost")
	}
}

// Two runs differing only in cache policy measured different things, so they
// must not carry the same experiment id.
func TestCachePolicyIsPartOfTheExperimentIdentity(t *testing.T) {
	warm := Provenance{Commit: "abc", Cache: DefaultCachePolicy()}.Finalise()
	cold := Provenance{Commit: "abc", Cache: ColdCachePolicy()}.Finalise()
	if warm.ExperimentID == cold.ExperimentID {
		t.Fatal("a warm and a cold run carry the same experiment id")
	}
}

// --- arm order -------------------------------------------------------------

// No arm may be systematically first: the later one gets warmer caches, a
// warmer server and whatever thermal state the first pass left.
func TestArmOrderIsCounterbalanced(t *testing.T) {
	arms := []Arm{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	var orders []ArmOrder
	for rep := 1; rep <= 3; rep++ {
		for i := range 30 {
			task := "task-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
			ordered := OrderArms(1, task, rep, arms)
			if len(ordered) != len(arms) {
				t.Fatalf("the rotation dropped an arm: %v", ordered)
			}
			seen := map[string]bool{}
			for _, a := range ordered {
				seen[a.Name] = true
			}
			if len(seen) != len(arms) {
				t.Fatalf("the rotation repeated an arm: %v", ordered)
			}
			names := []string{ordered[0].Name, ordered[1].Name, ordered[2].Name}
			orders = append(orders, ArmOrder{Task: task, Repetition: rep, Order: names})
		}
	}
	b := BalanceOf(orders)
	if b.Skew > 0.5 {
		t.Fatalf("one arm led %.0f%% of cells; order is a candidate explanation for any "+
			"result (%v)", b.Skew*100, b.FirstCount)
	}
	// And it must be reproducible, or a run cannot be repeated. Compared
	// across the whole rotation rather than one position: a rotation that
	// agreed on its first element and differed after would still make a
	// second run a different experiment.
	first := OrderArms(1, "task-a0", 1, arms)
	second := OrderArms(1, "task-a0", 1, arms)
	for i := range first {
		if first[i].Name != second[i].Name {
			t.Fatalf("the rotation is not deterministic: %v then %v", first, second)
		}
	}
}

// --- readiness -------------------------------------------------------------

func TestReadinessStagesGateOnTheRightThings(t *testing.T) {
	empty := Assess(ReadinessInput{})
	if empty.Stage != StageDatasetConstruction {
		t.Fatalf("an empty dataset is %q", empty.Stage)
	}

	// Enough labelled dev tasks and a working smoke call reaches dev tuning.
	var tasks []Task
	for i := range 12 {
		tasks = append(tasks, Task{ID: string(rune('a' + i)), Set: SetDev})
	}
	dev := Assess(ReadinessInput{
		Tasks:    tasks,
		Coverage: AnnotationStatus{Total: 12, Annotated: 12, BySet: map[Set]int{SetDev: 12}},
		SmokeOK:  true,
	})
	if dev.Stage != StageDevTuning {
		t.Fatalf("stage = %q, blockers %v", dev.Stage, dev.Blockers)
	}
	if len(dev.Blockers) == 0 {
		t.Fatal("dev tuning reported no blockers to held-out with an empty held-out set")
	}

	// Held-out needs the bar, the labels, the adjudication and a frozen
	// configuration.
	notFrozen := Assess(ReadinessInput{
		Tasks: tasks, SmokeOK: true,
		Coverage:     AnnotationStatus{Total: 12, Annotated: 12, BySet: map[Set]int{SetDev: 12}},
		TuningFrozen: false,
	})
	if !strings.Contains(strings.Join(notFrozen.Blockers, " "), "frozen") {
		t.Fatalf("an unfrozen configuration was not a blocker: %v", notFrozen.Blockers)
	}
}
