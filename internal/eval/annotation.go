package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Localization ground truth, and the discipline around producing it.
//
// The label has to match the proposition being measured, and the obvious
// shortcut does not. "Files the gold patch changed" is a different question
// from "files an engineer must read": the interface that constrained the fix,
// the caller whose expectations it had to preserve, the test that defined the
// required behaviour and the migration it depended on are all things a careful
// engineer reads and none of them appear in the diff. Scoring retrieval
// against the diff would penalise it for finding exactly the evidence the
// change was derived from.
//
// So the gold patch is *evidence for* an annotation and never the annotation
// itself. Every label is written by a person, carries who wrote it and under
// which rubric, and nothing in this package infers or approves one.

// AnnotationRubricVersion is the version of the labelling rubric below.
//
// It travels with every label and with every benchmark artifact. A rubric
// change must not silently reinterpret labels written under the old one: when
// this changes, existing annotations are stale until a person revisits them,
// and Stale() says so rather than letting the numbers quietly shift meaning.
const AnnotationRubricVersion = "1.0"

// Label is one annotator's verdict on one candidate.
type Label string

const (
	// LabelRequired: reading this candidate is necessary to reliably
	// implement or verify the correct solution. An engineer who did not read
	// it would be guessing about something the task depends on.
	LabelRequired Label = "REQUIRED"
	// LabelUseful: reading it makes the work easier or faster, but a correct
	// solution can be reached without it.
	//
	// The third category exists because the two-way split forces a bad
	// choice on the most common case — a sibling implementation that shows
	// the house pattern, a neighbouring test that suggests the shape. Calling
	// those REQUIRED inflates recall's denominator with things retrieval is
	// not failing by missing; calling them NOT_REQUIRED punishes a reranker
	// for surfacing genuinely helpful context.
	//
	// It is never silently collapsed. Recall is reported over REQUIRED alone,
	// which is the number that means "the engineer could not have done this";
	// USEFUL candidates are excluded from precision's denominator rather than
	// counted against it, because a packet carrying helpful context is not
	// making an error. Metrics that consume it say which of the two they did.
	LabelUseful Label = "USEFUL"
	// LabelNotRequired: a correct engineer does not need this candidate for
	// this task.
	LabelNotRequired Label = "NOT_REQUIRED"
)

// Labels lists the rubric's categories, for CLI validation.
func Labels() []Label { return []Label{LabelRequired, LabelUseful, LabelNotRequired} }

// Valid reports a label the rubric defines.
func (l Label) Valid() bool {
	for _, known := range Labels() {
		if l == known {
			return true
		}
	}
	return false
}

// RubricText is the definition an annotator is held to, kept next to the code
// that consumes it so the two cannot drift.
const RubricText = `Localization annotation rubric ` + AnnotationRubricVersion + `

The question, for each candidate file:

    Would a careful engineer need to READ this file to implement or verify
    the objective correctly?

Not "does the fix change it". Reading and changing are different acts, and the
metric is about what retrieval had to surface.

REQUIRED
    An engineer who did not read this would be guessing about something the
    task depends on. This includes files the fix changes, and also:
      - interfaces or contracts that constrain what the change may do
      - consumers and callers whose expectations the change must not break
      - tests that define the required behaviour, including hidden ones the
        task is graded on where their content is discoverable from the repo
      - configuration, schema or migrations the change depends on
      - an existing implementation the change must stay consistent with,
        where inconsistency would be a defect rather than a style difference

USEFUL
    Reading it helps — a sibling implementation showing the house pattern, a
    neighbouring test suggesting the shape — but a correct solution is
    reachable without it.

NOT_REQUIRED
    A correct engineer does not need it for this task. Shares vocabulary,
    mentions the same names in passing, or is unrelated.

Evidence, never truth:
    A gold or fixing diff tells you what changed. It is the strongest single
    hint about what had to be read and it is not the answer: a file the diff
    touches may be a mechanical rename (still REQUIRED to read? usually yes,
    but say so), and a file it does not touch may be the contract the whole
    change turns on.

When in doubt:
    Prefer NOT_REQUIRED and write a note. An over-broad REQUIRED set inflates
    the denominator of recall and makes retrieval look worse than it is, in a
    way nobody can audit later.`

// Annotation is one task's localization ground truth.
//
// It lives beside the task rather than inside it because it has its own
// provenance and its own lifecycle: a task definition is a problem statement,
// and this is a person's judgment about it, written at a time, under a rubric,
// against a particular revision of the fixture.
type Annotation struct {
	// TaskID ties this to a task. Required.
	TaskID string `yaml:"task_id"`
	// RubricVersion is the rubric these labels were written against.
	RubricVersion string `yaml:"rubric_version"`
	// Annotator identifies who decided. Required: an unattributed label is
	// one nobody can question.
	Annotator string `yaml:"annotator"`
	// AnnotatedAt is when.
	AnnotatedAt time.Time `yaml:"annotated_at"`
	// TaskDigest pins the task definition these labels describe, so a task
	// edited afterwards shows up as stale rather than silently mislabelled.
	TaskDigest string `yaml:"task_digest"`
	// GoldRevision is the fixing commit the annotator consulted, where one
	// exists. Evidence, not truth.
	GoldRevision string `yaml:"gold_revision,omitempty"`
	// Notes record the reasoning where a call was not obvious. Ambiguous
	// labels without notes are how a dataset becomes unarguable-with.
	Notes string `yaml:"notes,omitempty"`
	// Files maps a repository-relative path to its label. Every candidate the
	// annotator considered appears, including NOT_REQUIRED ones: the absence
	// of a path would otherwise be ambiguous between "judged irrelevant" and
	// "never looked at".
	Files map[string]Label `yaml:"files"`
	// Symbols is the same at declaration granularity, optional.
	Symbols map[string]Label `yaml:"symbols,omitempty"`
}

// Validate reports an annotation that cannot be trusted.
func (a Annotation) Validate() []string {
	var problems []string
	if strings.TrimSpace(a.TaskID) == "" {
		problems = append(problems, "task_id is empty")
	}
	if strings.TrimSpace(a.Annotator) == "" {
		problems = append(problems, "annotator is empty; an unattributed label is one nobody can question")
	}
	if a.RubricVersion == "" {
		problems = append(problems, "rubric_version is empty")
	}
	if a.AnnotatedAt.IsZero() {
		problems = append(problems, "annotated_at is unset")
	}
	if len(a.Files) == 0 {
		problems = append(problems, "no file was labelled")
	}
	for path, label := range a.Files {
		if !label.Valid() {
			problems = append(problems, fmt.Sprintf("%s: %q is not a rubric label", path, label))
		}
	}
	for symbol, label := range a.Symbols {
		if !label.Valid() {
			problems = append(problems, fmt.Sprintf("symbol %s: %q is not a rubric label", symbol, label))
		}
	}
	if a.Required() == 0 {
		problems = append(problems, "no file is REQUIRED; a task where an engineer must read "+
			"nothing cannot score retrieval, and is more likely an unfinished annotation")
	}
	return problems
}

// Stale reports an annotation written against a different rubric or a
// different task definition.
//
// It is a question rather than an automatic invalidation: a rubric revision
// may not affect a given task at all. What it must never do is nothing —
// silently reinterpreting old labels under new rules is the failure this
// field exists to prevent.
func (a Annotation) Stale(taskDigest string) (bool, string) {
	switch {
	case a.RubricVersion != AnnotationRubricVersion:
		return true, fmt.Sprintf("written against rubric %s, current is %s",
			a.RubricVersion, AnnotationRubricVersion)
	case a.TaskDigest != "" && taskDigest != "" && a.TaskDigest != taskDigest:
		return true, "the task definition has changed since it was annotated"
	}
	return false, ""
}

// Required lists the paths an engineer must read, sorted.
func (a Annotation) RequiredFiles() []string { return a.filesLabelled(LabelRequired) }

// UsefulFiles lists the paths that help without being necessary.
func (a Annotation) UsefulFiles() []string { return a.filesLabelled(LabelUseful) }

// Required counts the REQUIRED files.
func (a Annotation) Required() int { return len(a.RequiredFiles()) }

func (a Annotation) filesLabelled(want Label) []string {
	var out []string
	for path, label := range a.Files {
		if label == want {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

// TaskDigestOf pins a task definition, for detecting edits after annotation.
func TaskDigestOf(t Task) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%v\x00", t.ID, t.Objective, t.Fixture, t.Scope)
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// AnnotationDir is where labels live, beside the tasks they describe.
const AnnotationDir = "annotations"

// AnnotationPath is where one task's labels live.
func AnnotationPath(taskDir, taskID string) string {
	return filepath.Join(taskDir, AnnotationDir, taskID+".annotation.yaml")
}

// LoadAnnotation reads one task's labels. A missing file is not an error: an
// unannotated task is the ordinary state, and the caller reports it as
// unscorable rather than as a failure.
func LoadAnnotation(taskDir, taskID string) (*Annotation, error) {
	body, err := os.ReadFile(AnnotationPath(taskDir, taskID)) //nolint:gosec // a path derived from the task set
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var a Annotation
	dec := yaml.NewDecoder(strings.NewReader(string(body)))
	dec.KnownFields(true)
	if err := dec.Decode(&a); err != nil {
		return nil, fmt.Errorf("eval: %s: %w", AnnotationPath(taskDir, taskID), err)
	}
	if problems := a.Validate(); len(problems) > 0 {
		return nil, fmt.Errorf("eval: %s is not a usable annotation: %s",
			AnnotationPath(taskDir, taskID), strings.Join(problems, "; "))
	}
	return &a, nil
}

// SaveAnnotation writes one task's labels.
func SaveAnnotation(taskDir string, a Annotation) (string, error) {
	if problems := a.Validate(); len(problems) > 0 {
		return "", fmt.Errorf("eval: refusing to save: %s", strings.Join(problems, "; "))
	}
	path := AnnotationPath(taskDir, a.TaskID)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", err
	}
	body, err := yaml.Marshal(a)
	if err != nil {
		return "", err
	}
	header := fmt.Sprintf("# Localization ground truth for %s.\n"+
		"#\n"+
		"# Written by a person under rubric %s. Not inferred, not derived from a\n"+
		"# diff: see `bcode eval rubric` for what each label means and why the gold\n"+
		"# patch is evidence rather than truth.\n"+
		"#\n"+
		"# This file is evaluator-only. Nothing in the pipeline may read it: the\n"+
		"# leakage tests in internal/eval assert that.\n", a.TaskID, a.RubricVersion)
	if err := os.WriteFile(path, append([]byte(header), body...), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// ApplyAnnotations folds labels into the tasks' Expected fields.
//
// Expected.Files becomes the REQUIRED set and nothing else: recall is "did
// retrieval surface what an engineer had to read", and a USEFUL candidate
// missing is not a failure of that. Useful is carried separately so precision
// can decline to count it as a wrong answer.
//
// Stale annotations are refused rather than applied. A label written under an
// older rubric may still be right, but deciding that is a person's job.
func ApplyAnnotations(taskDir string, tasks []Task) ([]Task, []string, error) {
	out := make([]Task, 0, len(tasks))
	var warnings []string
	for _, t := range tasks {
		a, err := LoadAnnotation(taskDir, t.ID)
		if err != nil {
			return nil, nil, err
		}
		if a == nil {
			out = append(out, t)
			continue
		}
		if stale, why := a.Stale(TaskDigestOf(t)); stale {
			warnings = append(warnings, fmt.Sprintf("%s: annotation ignored — %s; "+
				"re-run `bcode eval annotate %s`", t.ID, why, t.ID))
			out = append(out, t)
			continue
		}
		t.Expected.Files = a.RequiredFiles()
		t.Expected.Useful = a.UsefulFiles()
		t.Expected.RubricVersion = a.RubricVersion
		t.Expected.Annotator = a.Annotator
		out = append(out, t)
	}
	return out, warnings, nil
}
