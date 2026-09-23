package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The four invariants this audit was about, each proven by the behaviour
// rather than by the comment next to it.

// --- 1. declared families must not cross the split ------------------------

func heldoutPreflight(t *testing.T, tasks []Task) PreflightInput {
	t.Helper()
	judged, err := ArmByName("supervised-rerank")
	if err != nil {
		t.Fatal(err)
	}
	return PreflightInput{
		Tasks: tasks, Arms: []Arm{judged}, Set: string(SetHeldout),
		Benchmark: true, JudgeAvailable: true, SmokeServed: "jev-1.13.0",
		Annotated:       AnnotationStatus{Total: len(tasks), Annotated: len(tasks)},
		HeldoutRequired: 1,
		Provenance: Provenance{
			Commit: "abc123", JudgmentModelRequested: "jev-1.13.0",
			JudgmentModelPinned: true,
		}.Finalise(),
	}
}

// A family declared across dev and held-out is tuning leakage: fitting on the
// dev member partly fits the held-out one.
func TestDeclaredFamilyCrossingTheSplitFailsHeldoutPreflight(t *testing.T) {
	split := []Task{
		{ID: "dev-1", Set: SetDev, Origin: TaskOrigin{Synthetic: true},
			Traits: TaskTraits{Family: "reconcile"}},
		{ID: "held-1", Set: SetHeldout, Origin: TaskOrigin{Synthetic: true},
			Traits: TaskTraits{Family: "reconcile"}},
	}
	p := RunPreflight(context.Background(), heldoutPreflight(t, split))
	if !p.Blocked() {
		t.Fatal("a declared family spanning dev and held-out was allowed into a held-out " +
			"benchmark")
	}
	if !strings.Contains(strings.Join(p.Blockers(), " "), "declared families") {
		t.Fatalf("blockers = %v", p.Blockers())
	}

	// The same collision outside a held-out run is advisory, not fatal: a dev
	// run is where the curator notices it.
	dev := heldoutPreflight(t, split)
	dev.Set = string(SetDev)
	if q := RunPreflight(context.Background(), dev); q.Blocked() {
		t.Fatalf("a dev run was blocked by a family collision: %v", q.Blockers())
	}
}

// And a family wholly on one side is fine, or the check would refuse every
// set that uses families at all.
func TestFamilyWithinOneSetIsFine(t *testing.T) {
	together := []Task{
		{ID: "held-1", Set: SetHeldout, Origin: TaskOrigin{Synthetic: true},
			Traits: TaskTraits{Family: "reconcile"}},
		{ID: "held-2", Set: SetHeldout, Origin: TaskOrigin{Synthetic: true},
			Traits: TaskTraits{Family: "reconcile"}},
	}
	p := RunPreflight(context.Background(), heldoutPreflight(t, together))
	if p.Blocked() {
		t.Fatalf("a family wholly inside held-out was refused: %v", p.Blockers())
	}
}

// Nothing may move a task. The check reports; a curator resolves.
func TestFamilyCheckDoesNotReassign(t *testing.T) {
	tasks := []Task{
		{ID: "a", Set: SetDev, Traits: TaskTraits{Family: "f"}},
		{ID: "b", Set: SetHeldout, Traits: TaskTraits{Family: "f"}},
	}
	RunPreflight(context.Background(), heldoutPreflight(t, tasks))
	if tasks[0].Set != SetDev || tasks[1].Set != SetHeldout {
		t.Fatal("preflight reassigned a task")
	}
}

// --- 2. independent annotators are blind ----------------------------------

func fixtureTask(t *testing.T) (string, Task) {
	t.Helper()
	dir := t.TempDir()
	fixture := filepath.Join(dir, "fixture")
	if err := os.MkdirAll(fixture, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		if err := os.WriteFile(filepath.Join(fixture, name),
			[]byte("package x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir, Task{
		ID: "t1", Objective: "make the limit configurable", Fixture: fixture,
		Origin: TaskOrigin{
			BaseRevision: "aaaa1111", GoldRevision: "bbbb2222dddd",
			Reference: "https://example.test/pull/42", Derived: true,
		},
	}
}

// An independent reading must not see the fixing revision, anything derived
// from it, another annotator's labels, or a suggestion.
func TestIndependentAnnotationIsBlind(t *testing.T) {
	dir, task := fixtureTask(t)

	first := Annotation{
		TaskID: "t1", RubricVersion: AnnotationRubricVersion, Annotator: "alice@example.test",
		AnnotatedAt: time.Now().UTC(), TaskDigest: TaskDigestOf(task),
		Files: map[string]Label{"a.go": LabelRequired, "b.go": LabelNotRequired},
	}
	if _, err := SaveForAnnotator(dir, first); err != nil {
		t.Fatal(err)
	}

	ev, err := GatherAnnotationEvidence(dir, task, "bob@example.test", ModeIndependent)
	if err != nil {
		t.Fatal(err)
	}
	if ev.GoldRevision != "" {
		t.Errorf("the fixing revision was shown: %q", ev.GoldRevision)
	}
	if ev.Existing != nil {
		t.Errorf("bob was shown alice's reading: %+v", ev.Existing.Files)
	}
	if len(ev.Others) != 0 {
		t.Errorf("another annotator's labels were included: %+v", ev.Others)
	}
	for _, c := range ev.Candidates {
		if c.Existing != "" {
			t.Errorf("%s carried a suggested label %q", c.Path, c.Existing)
		}
		if len(c.Given) != 0 {
			t.Errorf("%s carried another annotator's label %v", c.Path, c.Given)
		}
	}

	// Nothing gold-derived may appear anywhere in what is rendered, which is
	// the form the leak would actually take.
	rendered := renderEvidence(ev)
	for _, leak := range []string{"bbbb2222dddd", "example.test/pull/42", "aaaa1111"} {
		if strings.Contains(rendered, leak) {
			t.Errorf("the evidence renders %q:\n%s", leak, rendered)
		}
	}
	for _, leak := range []string{"alice", "REQUIRED", "NOT_REQUIRED"} {
		if strings.Contains(rendered, leak) {
			t.Errorf("the evidence renders %q, anchoring the reading:\n%s", leak, rendered)
		}
	}
}

// An annotator does see their own earlier reading: revising your own work is
// not anchoring.
func TestAnnotatorSeesTheirOwnEarlierReading(t *testing.T) {
	dir, task := fixtureTask(t)
	mine := Annotation{
		TaskID: "t1", RubricVersion: AnnotationRubricVersion, Annotator: "alice@example.test",
		AnnotatedAt: time.Now().UTC(), TaskDigest: TaskDigestOf(task),
		Files: map[string]Label{"a.go": LabelRequired},
	}
	if _, err := SaveForAnnotator(dir, mine); err != nil {
		t.Fatal(err)
	}
	ev, err := GatherAnnotationEvidence(dir, task, "alice@example.test", ModeIndependent)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Existing == nil {
		t.Fatal("alice could not see her own earlier reading")
	}
}

// Adjudication is the mode where all of it is legitimate.
func TestAdjudicationModeSeesEverything(t *testing.T) {
	dir, task := fixtureTask(t)
	for _, who := range []string{"alice@example.test", "bob@example.test"} {
		if _, err := SaveForAnnotator(dir, Annotation{
			TaskID: "t1", RubricVersion: AnnotationRubricVersion, Annotator: who,
			AnnotatedAt: time.Now().UTC(), TaskDigest: TaskDigestOf(task),
			Files: map[string]Label{"a.go": LabelRequired},
		}); err != nil {
			t.Fatal(err)
		}
	}
	ev, err := GatherAnnotationEvidence(dir, task, "carol@example.test", ModeAdjudication)
	if err != nil {
		t.Fatal(err)
	}
	if ev.GoldRevision != "bbbb2222dddd" {
		t.Errorf("the adjudicator was not shown the fixing revision: %q", ev.GoldRevision)
	}
	if len(ev.Others) != 2 {
		t.Errorf("the adjudicator saw %d reading(s), want 2", len(ev.Others))
	}
}

// renderEvidence is everything the independent annotator could be shown,
// flattened. The test asserts against this rather than against struct fields
// so that a future change which prints a leaked value still fails.
func renderEvidence(ev AnnotationEvidence) string {
	var b strings.Builder
	b.WriteString(ev.Task.ID + " " + ev.Task.Objective + " " + ev.Task.Notes + " ")
	b.WriteString(ev.GoldRevision + " " + ev.StaleReason + " ")
	b.WriteString(strings.Join(ev.Scope, " ") + " ")
	b.WriteString(strings.Join(ev.AcceptanceFiles, " ") + " ")
	if ev.Existing != nil {
		b.WriteString(ev.Existing.Annotator + " " + ev.Existing.GoldRevision + " ")
		for path, label := range ev.Existing.Files {
			b.WriteString(path + "=" + string(label) + " ")
		}
	}
	for _, other := range ev.Others {
		b.WriteString(other.Annotator + " ")
		for path, label := range other.Files {
			b.WriteString(path + "=" + string(label) + " ")
		}
	}
	for _, c := range ev.Candidates {
		b.WriteString(c.Path + " " + string(c.Existing) + " ")
		for who, label := range c.Given {
			b.WriteString(who + "=" + string(label) + " ")
		}
	}
	return b.String()
}

// --- 3. annotation gaps must be closed, and stay distinct from disagreements

func twoReadings(t *testing.T, dir string, task Task, a, b map[string]Label) {
	t.Helper()
	for who, files := range map[string]map[string]Label{
		"alice@example.test": a, "bob@example.test": b,
	} {
		if _, err := SaveForAnnotator(dir, Annotation{
			TaskID: task.ID, RubricVersion: AnnotationRubricVersion, Annotator: who,
			AnnotatedAt: time.Now().UTC(), TaskDigest: TaskDigestOf(task), Files: files,
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGapsAreTrackedSeparatelyAndMustBeClosed(t *testing.T) {
	dir, task := fixtureTask(t)
	twoReadings(t, dir, task,
		map[string]Label{"a.go": LabelRequired, "b.go": LabelRequired, "c.go": LabelUseful},
		map[string]Label{"a.go": LabelRequired, "b.go": LabelNotRequired})

	c, err := CloseOut(dir, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Union != 3 {
		t.Fatalf("union = %d, want 3", c.Union)
	}
	if c.AgreedBoth != 1 {
		t.Fatalf("agreed = %d, want 1 (a.go)", c.AgreedBoth)
	}
	if c.Disagreements != 1 || len(c.UnresolvedDisagreements) != 1 {
		t.Fatalf("disagreements = %d %v", c.Disagreements, c.UnresolvedDisagreements)
	}
	if c.Gaps != 1 || len(c.UnresolvedGaps) != 1 || c.UnresolvedGaps[0] != "c.go" {
		t.Fatalf("gaps = %d %v; c.go was considered by one annotator only",
			c.Gaps, c.UnresolvedGaps)
	}
	if c.Closed() {
		t.Fatal("a task with an unresolved gap reported itself closed")
	}

	// A gap is not a disagreement, and must not be counted as one: doing so
	// would punish thoroughness and drag kappa down for it.
	ag := CompareAnnotations(
		Annotation{Annotator: "a", Files: map[string]Label{
			"a.go": LabelRequired, "b.go": LabelRequired, "c.go": LabelUseful}},
		Annotation{Annotator: "b", Files: map[string]Label{
			"a.go": LabelRequired, "b.go": LabelNotRequired}})
	if ag.Disagreed != 1 {
		t.Fatalf("disagreed = %d; the gap was counted as a conflict", ag.Disagreed)
	}
	if ag.OnlyOneRated != 1 {
		t.Fatalf("only-one-rated = %d", ag.OnlyOneRated)
	}
}

// Adjudicating every open item closes the task.
func TestAdjudicatingEveryOpenItemClosesTheTask(t *testing.T) {
	dir, task := fixtureTask(t)
	twoReadings(t, dir, task,
		map[string]Label{"a.go": LabelRequired, "b.go": LabelRequired, "c.go": LabelUseful},
		map[string]Label{"a.go": LabelRequired, "b.go": LabelNotRequired})

	if _, err := SaveAdjudication(dir, Adjudication{
		TaskID: task.ID, RubricVersion: AnnotationRubricVersion,
		Annotators:  []string{"alice@example.test", "bob@example.test"},
		Adjudicator: "carol@example.test", AdjudicatedAt: time.Now().UTC().Format(time.RFC3339),
		Note:          "b.go defines the required error; c.go is a sibling pattern only",
		Files:         map[string]Label{"b.go": LabelRequired, "c.go": LabelNotRequired},
		Disagreements: []Disagreement{{Path: "b.go", Final: LabelRequired}},
	}); err != nil {
		t.Fatal(err)
	}
	c, err := CloseOut(dir, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Closed() {
		t.Fatalf("still open: disagreements %v gaps %v",
			c.UnresolvedDisagreements, c.UnresolvedGaps)
	}
	if c.Resolved != 2 {
		t.Fatalf("resolved = %d, want 2", c.Resolved)
	}
}

// An unresolved gap must stop a held-out benchmark rather than disappear.
func TestUnresolvedGapFailsHeldoutPreflight(t *testing.T) {
	in := heldoutPreflight(t, []Task{
		{ID: "h1", Set: SetHeldout, Origin: TaskOrigin{Synthetic: true}},
	})
	in.Reliability = ReliabilityReport{
		Compared: 4, Agreed: 4, UnresolvedGaps: 1, OpenTasks: []string{"h1"},
	}
	p := RunPreflight(context.Background(), in)
	if !p.Blocked() {
		t.Fatal("a held-out task with an unresolved annotation gap was allowed")
	}
	if !strings.Contains(strings.Join(p.Blockers(), " "), "annotation gaps") {
		t.Fatalf("blockers = %v", p.Blockers())
	}
}

// --- 4. experiment identity vs run identity -------------------------------

// The experiment says what was chosen; the run says what occurred. They must
// come apart on exactly the observed properties.
func TestRunIDVariesWhereExperimentIDDoesNot(t *testing.T) {
	base := Provenance{
		Commit: "abc123", JudgmentModelRequested: "jev-1.13.0", JudgmentModelPinned: true,
		ReasoningModel: "local-27b", DatasetDigest: "d", MembershipDigest: "m",
	}.Finalise()
	at := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	baseRun := base.RunFingerprint(at)

	for name, mutate := range map[string]func(Provenance) Provenance{
		"the machine": func(p Provenance) Provenance {
			p.Runtime = RuntimeIdentity{OS: "darwin", Arch: "arm64", CPUs: 10}
			return p
		},
		"the served model": func(p Provenance) Provenance {
			p.JudgmentModelServed = "jev-1.13.0"
			return p
		},
		"the arm order": func(p Provenance) Provenance {
			p.ArmOrders = []ArmOrder{{Task: "a", Repetition: 1, Order: []string{"x", "y"}}}
			return p
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := mutate(base).Finalise()
			if changed.ExperimentID != base.ExperimentID {
				t.Errorf("%s changed the experiment id; the same configuration on two "+
					"machines must compare", name)
			}
			if changed.RunFingerprint(at) == baseRun {
				t.Errorf("%s did not change the run id; a specific set of numbers cannot "+
					"be traced back to what produced it", name)
			}
		})
	}

	// And two executions of one configuration are two runs.
	later := base.RunFingerprint(at.Add(time.Hour))
	if later == baseRun {
		t.Error("two executions at different times share a run id")
	}

	// The cache policy is the case that belongs in *both*. It is a chosen
	// configuration rather than an observed property — a warm run and a cold
	// run measured different things and are different experiments — and it
	// also describes what this execution did.
	cold := base
	cold.Cache = ColdCachePolicy()
	cold = cold.Finalise()
	if cold.ExperimentID == base.ExperimentID {
		t.Error("a warm and a cold run share an experiment id")
	}
	if cold.RunFingerprint(at) == baseRun {
		t.Error("a warm and a cold run share a run id")
	}
}

// A pinned model the service honoured is a valid benchmark; one it did not is
// not, and the difference is refusal rather than a recorded observation.
func TestServedModelMismatchInvalidatesABenchmark(t *testing.T) {
	pinned := Provenance{
		Commit: "abc", JudgmentModelRequested: "jev-1.13.0", JudgmentModelPinned: true,
	}

	match := pinned
	match.JudgmentModelServed = "jev-1.13.0"
	if ok, why := match.Finalise().ValidForBenchmark(); !ok {
		t.Fatalf("a served model equal to the pinned request was refused: %s", why)
	}

	mismatch := pinned
	mismatch.JudgmentModelServed = "jev-1.14.0"
	ok, why := mismatch.Finalise().ValidForBenchmark()
	if ok {
		t.Fatal("a served/requested mismatch was accepted; the artifact would name a " +
			"model that did not produce the numbers")
	}
	if !strings.Contains(why, "jev-1.14.0") || !strings.Contains(why, "jev-1.13.0") {
		t.Fatalf("the reason does not name both models: %q", why)
	}

	// An alias cannot be validated at all.
	alias := Provenance{Commit: "abc", JudgmentModelRequested: "jev-latest"}
	if ok, _ := alias.Finalise().ValidForBenchmark(); ok {
		t.Fatal("an unpinned model was accepted for a benchmark")
	}

	// A run with no external judge has nothing to mismatch.
	none := Provenance{Commit: "abc"}.Finalise()
	if ok, why := none.ValidForBenchmark(); !ok {
		t.Fatalf("a run with no judge was refused: %s", why)
	}

	// The service naming no model is now refused for a publication
	// benchmark: acceptance of an identifier is not evidence that the
	// identifier served the request, and the vendor guarantees nothing that
	// would let one be inferred from the other.
	silent := pinned
	ok, why = silent.Finalise().ValidForBenchmark()
	if ok {
		t.Fatal("a benchmark proceeded with no observable served model")
	}
	if !strings.Contains(why, string(ModelIdentityUnverifiable)) {
		t.Fatalf("the refusal does not report the status: %q", why)
	}

	// The development escape lets it run and does not make it quotable.
	escaped := pinned
	escaped.AllowUnverifiedModel = true
	if ok, why := escaped.Finalise().ValidForBenchmark(); !ok {
		t.Fatalf("--allow-unverified-model did not permit the run: %s", why)
	}
	if ok, why := escaped.Finalise().Publishable(); ok {
		t.Fatal("a run with an unverifiable served model was reported publishable")
	} else if !strings.Contains(why, string(ModelIdentityUnverifiable)) {
		t.Fatalf("the refusal does not report the status: %q", why)
	}
}

// --- 5. strict-mode information parity ------------------------------------

// The primary comparison must be an equal-information one, or it is not a
// claim about relevance signals.
func TestStrictModeParityBetweenLocalAndJudged(t *testing.T) {
	local, err := ArmByName("supervised-rerank-local")
	if err != nil {
		t.Fatal(err)
	}
	judged, err := ArmByName("supervised-rerank")
	if err != nil {
		t.Fatal(err)
	}
	m := DescribeEvidence([]Arm{local, judged}, "strict")

	if !m.ModelLevel(local.Name, judged.Name) {
		t.Fatalf("the primary comparison is not equal-information under strict mode: %s",
			m.PermittedClaim(local.Name, judged.Name))
	}

	// Field by field, so a future change to either request builder shows up
	// here rather than silently downgrading a published claim.
	want := map[CandidateEvidenceKind]bool{
		CandPath: true, CandSymbol: true, CandKind: true, CandLines: true,
		CandSignature: false, CandExcerpt: false, CandFullSource: false,
		CandOriginScore: false, CandGraphMeta: false,
	}
	for _, arm := range m.Arms {
		for kind, expected := range want {
			if arm.Sees[kind] != expected {
				t.Errorf("%s sees %s = %v, want %v", arm.Arm, kind, arm.Sees[kind], expected)
			}
		}
	}

	// Under repo_text the judged arm sees more, and the claim must downgrade
	// automatically rather than by anybody remembering to.
	rich := DescribeEvidence([]Arm{local, judged}, "repo_text")
	if rich.ModelLevel(local.Name, judged.Name) {
		t.Fatal("repo_text still reported as an equal-information comparison")
	}
	if !strings.Contains(rich.PermittedClaim(local.Name, judged.Name), "system-level") {
		t.Fatalf("permitted claim = %q", rich.PermittedClaim(local.Name, judged.Name))
	}
}

// --- served-model identity: the publication bar ---------------------------

// The four cases the publication invariant turns on, and the production path
// beside them so the two cannot be conflated.
func TestServedModelIdentityGatesPublicationNotProduction(t *testing.T) {
	pinned := func() Provenance {
		return Provenance{
			Commit: "abc123", JudgmentModelRequested: "jev-1.13.0",
			JudgmentModelPinned: true,
		}
	}

	for name, tc := range map[string]struct {
		mutate      func(Provenance) Provenance
		wantStatus  ModelIdentityStatus
		wantBench   bool
		wantPublish bool
	}{
		"pinned and served the same": {
			mutate:     func(p Provenance) Provenance { p.JudgmentModelServed = "jev-1.13.0"; return p },
			wantStatus: ModelIdentityVerified, wantBench: true, wantPublish: true,
		},
		"pinned and served something else": {
			mutate:     func(p Provenance) Provenance { p.JudgmentModelServed = "jev-1.14.0"; return p },
			wantStatus: ModelIdentityMismatch, wantBench: false, wantPublish: false,
		},
		"pinned and served absent": {
			mutate:     func(p Provenance) Provenance { return p },
			wantStatus: ModelIdentityUnverifiable, wantBench: false, wantPublish: false,
		},
		"pinned, served absent, development escape": {
			mutate:     func(p Provenance) Provenance { p.AllowUnverifiedModel = true; return p },
			wantStatus: ModelIdentityUnverifiable, wantBench: true, wantPublish: false,
		},
		"an alias": {
			mutate: func(p Provenance) Provenance {
				p.JudgmentModelRequested, p.JudgmentModelPinned = "jev-latest", false
				return p
			},
			wantStatus: ModelIdentityUnpinned, wantBench: false, wantPublish: false,
		},
		"no external judge at all": {
			mutate: func(p Provenance) Provenance {
				p.JudgmentModelRequested, p.JudgmentModelPinned = "", false
				return p
			},
			wantStatus: ModelIdentityNotApplicable, wantBench: true, wantPublish: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := tc.mutate(pinned()).Finalise()

			status, detail := p.ModelIdentity()
			if status != tc.wantStatus {
				t.Errorf("status = %q, want %q (%s)", status, tc.wantStatus, detail)
			}
			if ok, why := p.ValidForBenchmark(); ok != tc.wantBench {
				t.Errorf("valid for benchmark = %v, want %v (%s)", ok, tc.wantBench, why)
			}
			if ok, why := p.Publishable(); ok != tc.wantPublish {
				t.Errorf("publishable = %v, want %v (%s)", ok, tc.wantPublish, why)
			}
		})
	}
}

// The status string is the one the brief asked to be reported, because
// somebody will grep for it.
func TestUnverifiableStatusIsReportedByName(t *testing.T) {
	if got := string(ModelIdentityUnverifiable); got != "MODEL_IDENTITY_UNVERIFIABLE" {
		t.Fatalf("status = %q", got)
	}
	p := Provenance{
		Commit: "abc", JudgmentModelRequested: "jev-1.13.0", JudgmentModelPinned: true,
	}.Finalise()
	_, why := p.ValidForBenchmark()
	if !strings.Contains(why, "MODEL_IDENTITY_UNVERIFIABLE") {
		t.Fatalf("the refusal does not name the status: %q", why)
	}
}

// Production must not have acquired the publication bar. A task whose judge
// answered from an unnamed model got a slightly different ordering, which is
// advisory; there is nothing to invalidate, and the whole pipeline must still
// run.
func TestProductionPathUnaffectedByAnAbsentServedModel(t *testing.T) {
	in := heldoutPreflight(t, []Task{
		{ID: "a", Set: SetDev, Origin: TaskOrigin{Synthetic: true},
			Expected: Expected{Files: []string{"x.go"}}},
	})
	in.Set = string(SetDev)
	in.Benchmark = false
	in.SmokeServed = "" // the service named no model

	p := RunPreflight(context.Background(), in)
	if p.Blocked() {
		t.Fatalf("a non-benchmark run was blocked by an unverifiable served model: %v",
			p.Blockers())
	}
}

// And a benchmark is stopped before it spends anything, not after.
func TestBenchmarkPreflightBlocksOnAnAbsentServedModel(t *testing.T) {
	in := heldoutPreflight(t, []Task{
		{ID: "h1", Set: SetHeldout, Origin: TaskOrigin{Synthetic: true}},
	})
	in.SmokeServed = ""

	p := RunPreflight(context.Background(), in)
	if !p.Blocked() {
		t.Fatal("a publication benchmark proceeded with no observable served model")
	}
	joined := strings.Join(p.Blockers(), " ")
	if !strings.Contains(joined, "MODEL_IDENTITY_UNVERIFIABLE") {
		t.Fatalf("blockers = %v", p.Blockers())
	}

	// The escape permits it and says the run is not publishable.
	in.Provenance.AllowUnverifiedModel = true
	in.Provenance = in.Provenance.Finalise()
	q := RunPreflight(context.Background(), in)
	if q.Blocked() {
		t.Fatalf("--allow-unverified-model did not permit the run: %v", q.Blockers())
	}
	if !strings.Contains(q.Format(), "NOT be publishable") {
		t.Fatalf("the preflight does not mark the run non-publishable:\n%s", q.Format())
	}
}
