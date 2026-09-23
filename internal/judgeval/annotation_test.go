package judgeval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnnotationsRoundTripAndSort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anns.jsonl")
	anns := []Annotation{
		{CaseID: "b", Label: "weakening", Annotator: "x", AnnotationVersion: "1"},
		{CaseID: "a", Label: "weakening", Annotator: "y", AnnotationVersion: "1"},
		{CaseID: "a", Label: "weakening", Annotator: "x", AnnotationVersion: "1"},
	}
	if err := SaveAnnotations(path, anns); err != nil {
		t.Fatal(err)
	}
	got, err := LoadAnnotations(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d annotations, want 3", len(got))
	}
	if got[0].CaseID != "a" || got[0].Annotator != "x" {
		t.Fatalf("not sorted by (case_id, annotator): %+v", got)
	}
}

func TestAnnotationValidateRequiresEveryField(t *testing.T) {
	valid := Annotation{CaseID: "a", Label: "weakening", Annotator: "x", AnnotationVersion: "1"}
	tests := []struct {
		name   string
		mutate func(*Annotation)
	}{
		{"case id", func(a *Annotation) { a.CaseID = "" }},
		{"label", func(a *Annotation) { a.Label = "" }},
		{"annotator", func(a *Annotation) { a.Annotator = "" }},
		{"version", func(a *Annotation) { a.AnnotationVersion = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := valid
			tt.mutate(&a)
			if err := a.Validate(); err == nil {
				t.Fatalf("expected Validate to reject a missing %s", tt.name)
			}
		})
	}
}

func TestResolveUnanimousAgreementWins(t *testing.T) {
	got := Resolve([]Annotation{
		{CaseID: "a", Label: "weakening", Annotator: "x", AnnotationVersion: "1"},
		{CaseID: "a", Label: "weakening", Annotator: "y", AnnotationVersion: "1"},
	})
	if got["a"] != "weakening" {
		t.Fatalf("got %q, want weakening", got["a"])
	}
}

func TestResolveDisagreementBecomesAmbiguous(t *testing.T) {
	got := Resolve([]Annotation{
		{CaseID: "a", Label: "weakening", Annotator: "x", AnnotationVersion: "1"},
		{CaseID: "a", Label: "legitimate", Annotator: "y", AnnotationVersion: "1"},
	})
	if got["a"] != LabelAmbiguous {
		t.Fatalf("got %q, want ambiguous — disagreement must never resolve to a side by "+
			"majority or first-seen order", got["a"])
	}
}

func TestResolveOneAnnotatorSayingAmbiguousMakesTheCaseAmbiguous(t *testing.T) {
	got := Resolve([]Annotation{
		{CaseID: "a", Label: "weakening", Annotator: "x", AnnotationVersion: "1"},
		{CaseID: "a", Label: LabelAmbiguous, Annotator: "y", AnnotationVersion: "1"},
	})
	if got["a"] != LabelAmbiguous {
		t.Fatalf("got %q, want ambiguous", got["a"])
	}
}

func TestAnnotationFilesDoNotRequireIdentitySensitiveFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anon.jsonl")
	a := Annotation{CaseID: "a", Label: "weakening", Annotator: "annotator-1", AnnotationVersion: "1"}
	if err := SaveAnnotations(path, []Annotation{a}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Nothing in the schema forces a real name or email; a free-form
	// annotator string round-trips exactly as written.
	if !strings.Contains(string(body), "annotator-1") {
		t.Fatalf("annotator string not preserved: %s", body)
	}
}
