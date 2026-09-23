package eval

import (
	"context"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/retrieval"
)

func baseProvenance() Provenance {
	return Provenance{
		Commit: "abc123def456", Set: "dev", Repeat: 3,
		Arms:                   []string{"supervised", "supervised-rerank"},
		JudgmentModelRequested: "jev-1.13.0", ReasoningModel: "ternary-bonsai-2-27b",
		EmbeddingModel: "nomic-embed",
		DatasetDigest:  "dataset-a", MembershipDigest: "member-a",
		AnnotationRubricVersion: AnnotationRubricVersion,
	}.Finalise()
}

// Two runs that differ in anything that could move a number must not look
// like the same experiment. Each subtest changes exactly one input.
func TestExperimentIdentityDistinguishesEveryRelevantInput(t *testing.T) {
	base := baseProvenance()
	for name, mutate := range map[string]func(Provenance) Provenance{
		"code revision": func(p Provenance) Provenance { p.Commit = "999999999999"; return p },
		"dirty tree":    func(p Provenance) Provenance { p.Dirty = true; return p },
		"set":           func(p Provenance) Provenance { p.Set = "heldout"; return p },
		"repeat":        func(p Provenance) Provenance { p.Repeat = 5; return p },
		"arms":          func(p Provenance) Provenance { p.Arms = []string{"supervised"}; return p },
		"judgment model": func(p Provenance) Provenance {
			p.JudgmentModelRequested = "jev-latest"
			return p
		},
		"reasoning model": func(p Provenance) Provenance { p.ReasoningModel = "other"; return p },
		"embedding model": func(p Provenance) Provenance { p.EmbeddingModel = "other"; return p },
		"rerank tuning": func(p Provenance) Provenance {
			p.Tuning = retrieval.RerankTuning{RelevanceFloor: 0.5}
			return p
		},
		"inference": func(p Provenance) Provenance {
			p.Inference = InferenceParams{Temperature: 0.7}
			return p
		},
		"seed":            func(p Provenance) Provenance { p.Seed = 7; return p },
		"dataset":         func(p Provenance) Provenance { p.DatasetDigest = "dataset-b"; return p },
		"set membership":  func(p Provenance) Provenance { p.MembershipDigest = "member-b"; return p },
		"annotations":     func(p Provenance) Provenance { p.AnnotationDigest = "labels-b"; return p },
		"rubric version":  func(p Provenance) Provenance { p.AnnotationRubricVersion = "9.9"; return p },
		"prompt rubric":   func(p Provenance) Provenance { p.RubricDigest = "different"; return p },
		"judgment schema": func(p Provenance) Provenance { p.JudgmentSchemaDigest = "different"; return p },
	} {
		t.Run(name, func(t *testing.T) {
			changed := mutate(base).Finalise()
			if changed.ExperimentID == base.ExperimentID {
				t.Fatalf("changing the %s did not change the experiment id (%s); two runs "+
					"that measured different things would carry the same label", name, base.ExperimentID)
			}
		})
	}
}

// And the identity must be stable across the things that cannot move a
// number, or the same configuration on two machines would look like two
// experiments and nobody could compare them.
func TestExperimentIdentityIgnoresWhatCannotMoveANumber(t *testing.T) {
	base := baseProvenance()
	for name, mutate := range map[string]func(Provenance) Provenance{
		"the machine": func(p Provenance) Provenance {
			p.Runtime = RuntimeIdentity{OS: "darwin", Arch: "arm64", CPUs: 10}
			return p
		},
		"the served model": func(p Provenance) Provenance {
			p.JudgmentModelServed = "jev-1.13.0"
			return p
		},
		"task counts": func(p Provenance) Provenance {
			p.TaskCount, p.ScorableCount = 40, 40
			return p
		},
		"arm order": func(p Provenance) Provenance {
			p.Arms = []string{"supervised-rerank", "supervised"}
			return p
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := mutate(base).Finalise().ExperimentID; got != base.ExperimentID {
				t.Fatalf("changing %s changed the experiment id (%s → %s); the same "+
					"configuration on two machines must compare", name, base.ExperimentID, got)
			}
		})
	}
}

// Length-prefixed digest inputs: two different configurations must not
// concatenate to the same string.
func TestExperimentIdentityResistsFieldRunTogether(t *testing.T) {
	a := Provenance{Commit: "ab", Set: "cd"}.Finalise()
	b := Provenance{Commit: "abc", Set: "d"}.Finalise()
	if a.ExperimentID == b.ExperimentID {
		t.Fatal("two configurations collided; the digest inputs are not length-prefixed")
	}
}

func TestDirtyTreeIsNotPublishable(t *testing.T) {
	p := baseProvenance()
	p.Dirty = true
	ok, why := p.Publishable()
	if ok {
		t.Fatal("a dirty tree was reported publishable")
	}
	if !strings.Contains(why, "does not describe the code that ran") {
		t.Fatalf("the reason does not say what is wrong: %q", why)
	}
}

func TestUnpinnedOrMismatchedModelIsNotPublishable(t *testing.T) {
	p := baseProvenance()
	if ok, _ := p.Publishable(); ok {
		t.Fatal("an unpinned model was reported publishable")
	}
	// Pinned but with no served identifier is still not publishable: the
	// request was accepted, which is not the same as knowing what ran.
	p.JudgmentModelPinned = true
	if ok, why := p.Publishable(); ok {
		t.Fatalf("a pinned request with no served model was reported publishable: %q", why)
	}

	// Pinned and served with the same id is.
	p.JudgmentModelServed = "jev-1.13.0"
	if ok, why := p.Publishable(); !ok {
		t.Fatalf("a verified served model on a clean tree was refused: %s", why)
	}

	p.JudgmentModelServed = "jev-1.14.0"
	ok, why := p.Publishable()
	if ok || !strings.Contains(why, "jev-1.14.0") {
		t.Fatalf("a served/requested mismatch was accepted: ok=%v %q", ok, why)
	}
}

// The dataset and membership digests fail differently and must be separate: a
// task edited mid-experiment and a task moved between sets are different
// mistakes, and one digest would only say "something changed".
func TestDatasetAndMembershipDigestsAreIndependent(t *testing.T) {
	tasks := []Task{
		{ID: "a", Objective: "one", Set: SetDev},
		{ID: "b", Objective: "two", Set: SetDev},
	}
	data, member, _, _ := DatasetDigests(tasks)

	moved := append([]Task(nil), tasks...)
	moved[1].Set = SetHeldout
	dataMoved, memberMoved, _, _ := DatasetDigests(moved)
	if dataMoved != data {
		t.Error("moving a task between sets changed the dataset digest")
	}
	if memberMoved == member {
		t.Error("moving a task between sets did not change the membership digest")
	}

	edited := append([]Task(nil), tasks...)
	edited[0].Objective = "one, revised"
	dataEdited, memberEdited, _, _ := DatasetDigests(edited)
	if dataEdited == data {
		t.Error("editing a task did not change the dataset digest")
	}
	if memberEdited != member {
		t.Error("editing a task changed the membership digest")
	}
}

// Ground truth is part of what a recall number means, so a label change must
// change the annotation digest — and the task ordering must not.
func TestAnnotationDigestTracksLabelsAndNotOrder(t *testing.T) {
	a := []Task{
		{ID: "a", Expected: Expected{Files: []string{"x.go"}}},
		{ID: "b", Expected: Expected{Files: []string{"y.go"}}},
	}
	_, _, first, scorable := DatasetDigests(a)
	if scorable != 2 {
		t.Fatalf("scorable = %d, want 2", scorable)
	}
	_, _, reordered, _ := DatasetDigests([]Task{a[1], a[0]})
	if reordered != first {
		t.Error("reordering the tasks changed the annotation digest")
	}
	relabelled := []Task{a[0], {ID: "b", Expected: Expected{Files: []string{"z.go"}}}}
	if _, _, got, _ := DatasetDigests(relabelled); got == first {
		t.Error("changing a label did not change the annotation digest")
	}
}

func TestRevisionOfANonRepositoryIsUnknownAndDirty(t *testing.T) {
	commit, dirty := CaptureRevision(context.Background(), t.TempDir())
	if commit != "unknown" || !dirty {
		t.Fatalf("got %s dirty=%v; not knowing what code ran must not read as clean",
			commit, dirty)
	}
}

func TestProvenanceFillsDerivedFields(t *testing.T) {
	p := Provenance{Commit: "abc"}.Finalise()
	switch {
	case p.ExperimentID == "" || !strings.HasPrefix(p.ExperimentID, "exp-"):
		t.Fatalf("experiment id = %q", p.ExperimentID)
	case p.RubricDigest == "" || p.JudgmentSchemaDigest == "":
		t.Fatal("the prompt digests were not filled")
	case p.Version == "":
		t.Fatal("the version was not filled")
	}
	// Changing the rubric text must change the digest, or a reworded prompt
	// would be published as the same experiment.
	rubric, _ := PromptDigests()
	if len(rubric) != 64 {
		t.Fatalf("rubric digest is not a sha256: %q", rubric)
	}
}
