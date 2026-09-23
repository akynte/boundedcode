package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func goodAnnotation() Annotation {
	return Annotation{
		TaskID: "t1", RubricVersion: AnnotationRubricVersion,
		Annotator: "someone@example.test", AnnotatedAt: time.Now().UTC(),
		TaskDigest: "digest", GoldRevision: "deadbeef",
		Files: map[string]Label{
			"internal/billing/limit.go":      LabelRequired,
			"internal/billing/limit_test.go": LabelRequired,
			"internal/billing/doc.go":        LabelUseful,
			"internal/unrelated/thing.go":    LabelNotRequired,
		},
	}
}

// An unattributed label is one nobody can question, and an annotation where
// nothing is REQUIRED is more likely unfinished than a task needing no
// reading.
func TestAnnotationRefusesWhatCannotBeTrusted(t *testing.T) {
	for name, mutate := range map[string]func(Annotation) Annotation{
		"no annotator":  func(a Annotation) Annotation { a.Annotator = ""; return a },
		"no rubric":     func(a Annotation) Annotation { a.RubricVersion = ""; return a },
		"no timestamp":  func(a Annotation) Annotation { a.AnnotatedAt = time.Time{}; return a },
		"no task":       func(a Annotation) Annotation { a.TaskID = ""; return a },
		"no files":      func(a Annotation) Annotation { a.Files = nil; return a },
		"unknown label": func(a Annotation) Annotation { a.Files["x.go"] = "MAYBE"; return a },
		"nothing required": func(a Annotation) Annotation {
			for k := range a.Files {
				a.Files[k] = LabelNotRequired
			}
			return a
		},
	} {
		t.Run(name, func(t *testing.T) {
			if problems := mutate(goodAnnotation()).Validate(); len(problems) == 0 {
				t.Fatal("accepted")
			}
		})
	}
	if problems := goodAnnotation().Validate(); len(problems) > 0 {
		t.Fatalf("a good annotation was refused: %v", problems)
	}
}

// A rubric change must not silently reinterpret labels somebody else wrote.
func TestStaleAnnotationsAreIgnoredNotReinterpreted(t *testing.T) {
	dir := t.TempDir()
	// Written directly rather than through SaveAnnotation: the point is a
	// file that legitimately predates this build, which the saver would
	// never produce.
	body := "task_id: t1\nrubric_version: \"0.9\"\nannotator: someone\n" +
		"annotated_at: 2026-01-01T00:00:00Z\ntask_digest: digest\n" +
		"files:\n  internal/billing/limit.go: REQUIRED\n"
	if err := os.MkdirAll(filepath.Join(dir, AnnotationDir), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(AnnotationPath(dir, "t1"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	task := Task{ID: "t1", Objective: "o", Fixture: "f"}
	tasks, warnings, err := ApplyAnnotations(dir, []Task{task})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks[0].Expected.Files) != 0 {
		t.Fatalf("a stale annotation was applied: %v", tasks[0].Expected.Files)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "rubric") {
		t.Fatalf("the operator was not told: %v", warnings)
	}
}

// An edited task invalidates the labels written against the old one.
func TestEditedTaskMakesItsAnnotationStale(t *testing.T) {
	a := goodAnnotation()
	a.TaskDigest = TaskDigestOf(Task{ID: "t1", Objective: "original", Fixture: "f"})
	edited := Task{ID: "t1", Objective: "revised", Fixture: "f"}
	stale, why := a.Stale(TaskDigestOf(edited))
	if !stale || !strings.Contains(why, "task definition has changed") {
		t.Fatalf("stale=%v %q", stale, why)
	}
}

// Recall is over REQUIRED alone; USEFUL is carried separately and is excluded
// from precision's denominator rather than counted against the packet.
func TestLabelsFeedTheMetricsAsDocumented(t *testing.T) {
	dir := t.TempDir()
	a := goodAnnotation()
	task := Task{ID: "t1", Objective: "o", Fixture: "f"}
	a.TaskDigest = TaskDigestOf(task)
	if _, err := SaveAnnotation(dir, a); err != nil {
		t.Fatal(err)
	}
	tasks, warnings, err := ApplyAnnotations(dir, []Task{task})
	if err != nil || len(warnings) > 0 {
		t.Fatalf("err=%v warnings=%v", err, warnings)
	}
	exp := tasks[0].Expected
	if len(exp.Files) != 2 {
		t.Fatalf("REQUIRED files = %v, want the two labelled REQUIRED", exp.Files)
	}
	if len(exp.Useful) != 1 || exp.Useful[0] != "internal/billing/doc.go" {
		t.Fatalf("USEFUL files = %v", exp.Useful)
	}
	if exp.Annotator == "" || exp.RubricVersion != AnnotationRubricVersion {
		t.Fatal("provenance did not travel with the labels")
	}

	// A packet holding one REQUIRED file and the USEFUL one: recall is a
	// half, and precision is 1/1 because the USEFUL file is not an error.
	score := ScoreLocalization(exp,
		[]string{"internal/billing/limit.go", "internal/billing/doc.go"},
		[]string{"internal/billing/limit.go", "internal/billing/doc.go"})
	if score.Recall != 0.5 {
		t.Fatalf("recall = %v, want 0.5 over REQUIRED alone", score.Recall)
	}
	if score.PrecisionDenominator != 1 || score.Precision != 1 {
		t.Fatalf("precision = %v over %d; a USEFUL file must not count as a wrong answer",
			score.Precision, score.PrecisionDenominator)
	}
	if score.UsefulCarried != 1 {
		t.Fatalf("useful carried = %d", score.UsefulCarried)
	}
}

// The saved file must say, on its face, that it is not machine-derived.
func TestSavedAnnotationCarriesItsProvenance(t *testing.T) {
	dir := t.TempDir()
	a := goodAnnotation()
	path, err := SaveAnnotation(dir, a)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path) //nolint:gosec // a path this test constructed
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		"Written by a person", "rubric", a.Annotator, "annotated_at", "gold_revision",
		"evaluator-only",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the saved annotation does not record %q:\n%s", want, text)
		}
	}
}

// The rubric text is what an annotator is held to, and the metrics consume the
// three labels in ways that must stay written down.
func TestRubricDefinesTheRightProposition(t *testing.T) {
	for _, want := range []string{
		"need to READ this",
		"Not \"does the fix change it\"",
		"interfaces or contracts",
		"consumers and callers",
		"tests that define the required behaviour",
		"configuration, schema or migrations",
		"Evidence, never truth",
		"REQUIRED", "USEFUL", "NOT_REQUIRED",
	} {
		if !strings.Contains(RubricText, want) {
			t.Errorf("the rubric no longer says %q", want)
		}
	}
	if strings.Contains(RubricText, "files changed by the gold patch is the ground truth") {
		t.Error("the rubric defines ground truth as the diff")
	}
}

func TestAnnotationCoverageCountsBySet(t *testing.T) {
	dir := t.TempDir()
	dev := Task{ID: "d1", Objective: "o", Fixture: "f", Set: SetDev}
	held := Task{ID: "h1", Objective: "o", Fixture: "f", Set: SetHeldout}

	a := goodAnnotation()
	a.TaskID, a.TaskDigest = "d1", TaskDigestOf(dev)
	if _, err := SaveAnnotation(dir, a); err != nil {
		t.Fatal(err)
	}

	st, err := AnnotationCoverage(dir, []Task{dev, held})
	if err != nil {
		t.Fatal(err)
	}
	if st.Annotated != 1 || st.BySet[SetDev] != 1 || st.BySet[SetHeldout] != 0 {
		t.Fatalf("coverage = %+v", st)
	}
	if len(st.Unlabelled) != 1 || st.Unlabelled[0] != "h1" {
		t.Fatalf("unlabelled = %v", st.Unlabelled)
	}
}
