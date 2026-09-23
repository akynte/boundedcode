package judgeval

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
)

// LabelAmbiguous is the reserved label a human annotator uses when a case
// genuinely does not have a clear answer. It is a first-class outcome, not
// an escape hatch that silently becomes a negative: Score, and every metric
// built on it, must exclude ambiguous cases from precision/recall rather
// than counting them either way — see stats.go's ClassificationMetrics,
// which drops them into a separate count instead of a bucket.
const LabelAmbiguous = "ambiguous"

// Annotation is one person's label for one case.
//
// Disagreement is supported by recording every annotation rather than
// collapsing to one label per case: two annotators may label the same
// case_id, and a reader can see both rather than a system silently picking
// one. No identity-sensitive field exists; Annotator is a free-form string
// an operator chooses (a role, an initials convention, "annotator-1") and
// this package never validates or looks it up against anything.
type Annotation struct {
	CaseID string `json:"case_id"`
	// Label uses the case's own allowed-answer vocabulary, or LabelAmbiguous.
	Label string `json:"label"`
	// Annotator is free text the operator chooses; never required to be a
	// real identity.
	Annotator string `json:"annotator"`
	// AnnotationVersion lets an annotation guideline change without
	// silently mixing labels produced under two different instructions.
	AnnotationVersion string `json:"annotation_version"`
	Rationale         string `json:"rationale,omitempty"`
	// Timestamp is caller-supplied (RFC 3339) and optional; this package
	// never stamps wall-clock time into a file it writes, because that
	// would make two runs over the same inputs produce a different byte
	// stream — see judgment-validation.md's reproducibility section.
	Timestamp string `json:"timestamp,omitempty"`
}

// Validate reports an annotation that cannot be used.
func (a Annotation) Validate() error {
	if a.CaseID == "" {
		return fmt.Errorf("annotation has no case_id")
	}
	if a.Label == "" {
		return fmt.Errorf("annotation %s: label is required (use %q if genuinely unclear)",
			a.CaseID, LabelAmbiguous)
	}
	if a.Annotator == "" {
		return fmt.Errorf("annotation %s: annotator is required", a.CaseID)
	}
	if a.AnnotationVersion == "" {
		return fmt.Errorf("annotation %s: annotation_version is required", a.CaseID)
	}
	return nil
}

// LoadAnnotations reads a JSONL annotation file the same way LoadDataset
// reads cases: one per line, blank and "#" lines ignored, sorted for
// determinism — here by (CaseID, Annotator) since more than one annotation
// may legitimately share a CaseID.
func LoadAnnotations(path string) ([]Annotation, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return decodeAnnotations(f)
}

func decodeAnnotations(r io.Reader) ([]Annotation, error) {
	var out []Annotation
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		trimmed := trimSpaceBytes(sc.Bytes())
		if len(trimmed) == 0 || trimmed[0] == '#' {
			continue
		}
		var a Annotation
		if err := json.Unmarshal(trimmed, &a); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		if err := a.Validate(); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		out = append(out, a)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CaseID != out[j].CaseID {
			return out[i].CaseID < out[j].CaseID
		}
		return out[i].Annotator < out[j].Annotator
	})
	return out, nil
}

// SaveAnnotations writes annotations as JSONL, sorted the same way
// LoadAnnotations returns them.
func SaveAnnotations(path string, anns []Annotation) error {
	sorted := append([]Annotation(nil), anns...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].CaseID != sorted[j].CaseID {
			return sorted[i].CaseID < sorted[j].CaseID
		}
		return sorted[i].Annotator < sorted[j].Annotator
	})
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, a := range sorted {
		if err := a.Validate(); err != nil {
			return err
		}
		if err := enc.Encode(a); err != nil {
			return err
		}
	}
	return nil
}

// Resolve reduces every annotation for one case_id to a single ground-truth
// label: unanimous agreement wins, any disagreement (including a mix that
// includes LabelAmbiguous alongside a real label) resolves to
// LabelAmbiguous rather than picking a side by majority or first-seen order.
// A promotion decision must not rest on a label a coin flip in the loading
// code chose.
func Resolve(anns []Annotation) map[string]string {
	byCase := map[string]map[string]bool{}
	for _, a := range anns {
		if byCase[a.CaseID] == nil {
			byCase[a.CaseID] = map[string]bool{}
		}
		byCase[a.CaseID][a.Label] = true
	}
	out := make(map[string]string, len(byCase))
	for id, labels := range byCase {
		if len(labels) == 1 {
			for label := range labels {
				out[id] = label
			}
			continue
		}
		out[id] = LabelAmbiguous
	}
	return out
}
