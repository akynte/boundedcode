package lab

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// LoadRecord reads a provenance record.
func LoadRecord(path string) (*Record, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Record
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if r.Schema != RecordSchema {
		return nil, fmt.Errorf("%s: schema %d is not %d", path, r.Schema, RecordSchema)
	}
	return &r, nil
}

// ReportsOf returns a record's reports by arm, in trial order (dir is the
// run's directory).
func ReportsOf(r *Record, dir string) map[string][]string {
	out := map[string][]string{}
	for _, run := range r.Runs {
		if run.Report.Path != "" {
			out[run.Arm] = append(out[run.Arm], resolve(dir, run.Report.Path))
		}
	}
	return out
}

// EvidenceFor pins what a decision rests on: the provenance record (when
// the laboratory ran the experiment), every report and the decision, with
// paths relative to dir (where the proposals are written).
func EvidenceFor(dir string, rec *Record, reports map[string][]string) ([]FileRef, error) {
	var out []FileRef
	add := func(p string) error {
		ref, err := pin(p)
		if err != nil {
			return err
		}
		abs, _ := filepath.Abs(p)
		absDir, _ := filepath.Abs(dir)
		if r, err := filepath.Rel(absDir, abs); err == nil {
			ref.Path = filepath.ToSlash(r)
		}
		out = append(out, ref)
		return nil
	}
	if rec != nil {
		if err := add(filepath.Join(dir, "provenance.json")); err != nil {
			return nil, err
		}
	}
	for _, arm := range sortedKeys(reports) {
		for _, p := range reports[arm] {
			if err := add(p); err != nil {
				return nil, err
			}
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "decision.json")); err == nil {
		if err := add(filepath.Join(dir, "decision.json")); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// PinOutputs records the decision and proposals in the provenance record.
// The record itself is evidence of the proposals, pinned before this
// update; the update adds only the outputs' hashes.
func PinOutputs(dir string, rec *Record, decision string, proposals []string) error {
	ref, err := pin(decision)
	if err != nil {
		return err
	}
	ref.Path = relTo(dir, ref.Path)
	rec.Decision = &ref
	for _, p := range proposals {
		pr, err := pin(p)
		if err != nil {
			return err
		}
		pr.Path = relTo(dir, pr.Path)
		rec.Proposals = append(rec.Proposals, pr)
	}
	return writeJSON(filepath.Join(dir, "outputs.json"), struct {
		Decision  *FileRef  `json:"decision"`
		Proposals []FileRef `json:"proposals,omitempty"`
	}{rec.Decision, rec.Proposals})
}

// DraftExperiment writes an experiment file for a hypothesis, to be
// reviewed and edited before it runs.
func DraftExperiment(h Hypothesis, trials int) []byte {
	type arm struct {
		Name        string         `yaml:"name"`
		Role        string         `yaml:"role"`
		Description string         `yaml:"description,omitempty"`
		Set         map[string]any `yaml:"set,omitempty"`
		Patch       string         `yaml:"patch,omitempty"`
		Predicts    map[string]any `yaml:"predicts,omitempty"`
	}
	set := func(iv Intervention) map[string]any {
		vals, _ := settingsValues(iv.Settings)
		if len(vals) == 0 {
			return nil
		}
		out := map[string]any{}
		for k, v := range vals {
			sec, key, _ := strings.Cut(k, ".")
			m, _ := out[sec].(map[string]any)
			if m == nil {
				m = map[string]any{}
				out[sec] = m
			}
			m[key] = v
		}
		return out
	}
	iv := h.Intervention
	// Budgets and escalation are bounded mode's; everything else, and code,
	// is tried in direct mode.
	suite := "direct"
	if !iv.Settings.IsZero() && iv.Settings.Direct.IsZero() && iv.Settings.Inference.ReasoningHistory == "" {
		suite = "bounded"
	}
	arms := []arm{{Name: "baseline", Role: "baseline"}}
	for _, alt := range iv.Simpler {
		arms = append(arms, arm{Name: alt.Name, Role: "simpler", Description: alt.Why, Set: set(Intervention{Settings: alt.Settings})})
	}
	cand := arm{Name: iv.ID, Role: "candidate", Description: iv.Summary, Set: set(iv)}
	if iv.Code != "" {
		cand.Patch = iv.ID + ".patch"
		cand.Description += " Write the patch with `bcode lab worktree " + iv.ID + "`: " + iv.Code
	}
	if len(h.Tasks) > 0 {
		cand.Predicts = map[string]any{"fixes": h.Tasks}
	}
	arms = append(arms, cand)
	dirs := []string{"benchmarks/direct"}
	if suite == "bounded" {
		dirs = []string{"benchmarks/tasks"}
	}
	doc := map[string]any{
		"id":         iv.ID,
		"hypothesis": h.Statement + "\nEvidence: " + h.Evidence,
		"pattern":    string(iv.Pattern),
		"suite":      suite,
		"tasks":      map[string]any{"dirs": dirs},
		"primary":    map[string]any{"metric": iv.Primary.Name, "better": iv.Primary.Better},
		"trials":     trials,
		"arms":       arms,
	}
	b, _ := yaml.Marshal(doc)
	return append([]byte("# Draft from bcode lab hypothesize: review the hypothesis, the tasks (the held-out\n# ones decide it) and the arms before running it with bcode lab run.\n"), b...)
}
