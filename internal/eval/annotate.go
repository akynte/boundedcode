package eval

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Evidence gathering for a human annotator.
//
// Nothing here decides anything. It finds the candidates a person should rule
// on and the material they need to rule with, and stops. The one thing this
// file must never do is propose a default label: an annotation the tool
// suggested and a person clicked through is not ground truth, it is the
// tool's opinion with a name attached, and a benchmark scored against it
// measures the tool against itself.

// AnnotationMode decides what an annotator is allowed to see.
//
// The two modes are not a convenience. An independent annotation that was
// made while looking at the gold diff, or at somebody else's labels, is
// anchored to the patch rather than to the rubric — and the rubric asks a
// different question from "what did the fix change". The whole reason for
// two readings is that they are two readings.
type AnnotationMode string

const (
	// ModeIndependent: one annotator's own reading. No gold revision, no
	// other annotator's labels, no adjudicated labels, no suggestions.
	ModeIndependent AnnotationMode = "independent"
	// ModeAdjudication: after both readings are in. Everything is visible,
	// because the job now is to resolve a difference rather than to form an
	// opinion.
	ModeAdjudication AnnotationMode = "adjudication"
)

// AnnotationEvidence is what an annotator is shown.
type AnnotationEvidence struct {
	Mode AnnotationMode
	Task Task
	// Candidates are every file in the fixture a person might have to rule
	// on, with whatever is known about each.
	Candidates []CandidateEvidence
	// Existing is this annotator's own earlier reading, and only ever
	// theirs. In independent mode it is loaded through LoadForAnnotator,
	// which refuses to return anybody else's.
	Existing *Annotation
	// Others are the other annotators' labels. Populated in adjudication
	// mode and nil in independent mode, which is the blind.
	Others []Annotation
	// GoldRevision is the fixing commit. Empty in independent mode: a
	// reading made while looking at the patch is a reading of the patch.
	GoldRevision string
	// Stale says the existing annotation predates the current rubric or task.
	Stale       bool
	StaleReason string
	// AcceptanceFiles are the hidden test files the task is graded on. They
	// are shown to the *annotator* — who is deciding what an engineer would
	// need to read — and never to the system under test.
	AcceptanceFiles []string
	// Scope is what the task permits writing, a strong hint about where the
	// work is and not a substitute for judgment.
	Scope []string
}

// CandidateEvidence is one file the annotator must rule on.
type CandidateEvidence struct {
	Path string
	// Lines and Bytes give a sense of what reading it would cost.
	Lines int
	Bytes int64
	// InScope says the task's write scope covers it.
	InScope bool
	// IsTest marks a file that looks like a test, so the annotator is
	// prompted about the case the rubric is most often got wrong on: a test
	// that defines required behaviour is REQUIRED reading even when the
	// objective never mentions tests.
	IsTest bool
	// Existing is this annotator's own earlier label, empty when new. Never
	// anybody else's.
	Existing Label
	// Given is the other annotators' labels, by annotator. Populated in
	// adjudication mode only.
	Given map[string]Label
}

// GatherAnnotationEvidence collects what a person needs to label one task.
//
// The gold diff, where the operator supplies one, is passed through as text
// for them to read. It is evidence and it is the annotator's to weigh: this
// function does not extract paths from it and does not mark anything from it,
// because a tool that pre-filled REQUIRED from a diff would have defined
// ground truth as "files the patch changed" by the back door.
func GatherAnnotationEvidence(taskDir string, t Task, annotator string,
	mode AnnotationMode) (AnnotationEvidence, error) {

	ev := AnnotationEvidence{Mode: mode, Task: t, Scope: t.Scope}

	// Independent mode loads only this annotator's own earlier reading.
	// LoadForAnnotator refuses to return another's, which is what makes the
	// second reading a reading rather than a review.
	existing, err := LoadForAnnotator(taskDir, t.ID, annotator)
	if err != nil {
		return ev, err
	}
	ev.Existing = existing
	if existing != nil {
		ev.Stale, ev.StaleReason = existing.Stale(TaskDigestOf(t))
	}

	if mode == ModeAdjudication {
		// Now everything is visible: the job is to resolve a difference
		// between two readings that have already been made.
		ev.GoldRevision = t.Origin.GoldRevision
		all, err := LoadIndependentAnnotations(taskDir, t.ID)
		if err != nil {
			return ev, err
		}
		for _, a := range all {
			if !sameAnnotator(a.Annotator, annotator) {
				ev.Others = append(ev.Others, a)
			}
		}
	}
	for path := range t.Acceptance.Files {
		ev.AcceptanceFiles = append(ev.AcceptanceFiles, path)
	}
	sort.Strings(ev.AcceptanceFiles)

	root := t.FixturePath()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "dist", "target", ".bc":
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil //nolint:nilerr // a path outside the fixture is not a candidate
		}
		rel = filepath.ToSlash(rel)
		info, statErr := d.Info()
		if statErr != nil {
			return nil //nolint:nilerr // an unreadable entry is not a candidate
		}
		c := CandidateEvidence{
			Path: rel, Bytes: info.Size(),
			InScope: inScope(t.Scope, rel),
			IsTest:  looksLikeTest(rel),
		}
		if body, readErr := os.ReadFile(path); readErr == nil { //nolint:gosec // inside the fixture
			c.Lines = strings.Count(string(body), "\n") + 1
		}
		if existing != nil {
			c.Existing = existing.Files[rel]
		}
		if mode == ModeAdjudication {
			c.Given = map[string]Label{}
			for _, other := range ev.Others {
				if label, ok := other.Files[rel]; ok {
					c.Given[other.Annotator] = label
				}
			}
		}
		ev.Candidates = append(ev.Candidates, c)
		return nil
	})
	if err != nil {
		return ev, err
	}
	sort.Slice(ev.Candidates, func(i, j int) bool { return ev.Candidates[i].Path < ev.Candidates[j].Path })
	return ev, nil
}

// inScope reports whether the task's write scope covers a path. It is a hint
// for the annotator and nothing more: the scope says where the fix may write,
// and the rubric asks what must be read.
func inScope(scope []string, rel string) bool {
	for _, s := range scope {
		s = strings.TrimSuffix(filepath.ToSlash(s), "/")
		if rel == s || strings.HasPrefix(rel, s+"/") {
			return true
		}
	}
	return false
}

func looksLikeTest(rel string) bool {
	base := strings.ToLower(filepath.Base(rel))
	return strings.HasSuffix(base, "_test.go") ||
		strings.HasSuffix(base, ".test.ts") || strings.HasSuffix(base, ".test.js") ||
		strings.HasSuffix(base, ".spec.ts") || strings.HasSuffix(base, ".spec.js") ||
		strings.HasPrefix(base, "test_") ||
		strings.Contains(filepath.ToSlash(rel), "/tests/")
}

// AnnotationStatus summarises how much of a set is labelled.
type AnnotationStatus struct {
	Total      int
	Annotated  int
	Stale      int
	Unlabelled []string
	StaleTasks []string
	// BySet counts annotated tasks per evaluation set, which is what decides
	// whether a held-out evaluation can be scored at all.
	BySet map[Set]int
	// TotalBySet counts every task per set.
	TotalBySet map[Set]int
}

// AnnotationCoverage reports how much of a task set carries usable labels.
func AnnotationCoverage(taskDir string, tasks []Task) (AnnotationStatus, error) {
	st := AnnotationStatus{Total: len(tasks), BySet: map[Set]int{}, TotalBySet: map[Set]int{}}
	for _, t := range tasks {
		st.TotalBySet[t.Membership()]++
		a, err := LoadAnnotation(taskDir, t.ID)
		if err != nil {
			return st, err
		}
		if a == nil {
			st.Unlabelled = append(st.Unlabelled, t.ID)
			continue
		}
		if stale, _ := a.Stale(TaskDigestOf(t)); stale {
			st.Stale++
			st.StaleTasks = append(st.StaleTasks, t.ID)
			continue
		}
		st.Annotated++
		st.BySet[t.Membership()]++
	}
	sort.Strings(st.Unlabelled)
	sort.Strings(st.StaleTasks)
	return st, nil
}
