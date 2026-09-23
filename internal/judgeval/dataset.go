// Package judgeval is the Judgment Validation Campaign.
//
// internal/ledger's calibration layer (Calibrate, Report, Population) scores
// a judgment site from whatever predictions ordinary task runs happened to
// pair with an outcome. That is real evidence, but it is opportunistic: it
// says nothing about a site until enough tasks have happened to run it, it
// cannot be steered toward the hard cases a promotion decision most needs to
// see, and several sites (intake_profile, context_injection, note_relevance,
// fact_relevance) have no runtime outcome to pair with at all — see
// judgment.SiteInfo.Paired.
//
// This package is the other half: an offline, dataset-driven campaign that
// can be run against a labeled corpus, on demand, without waiting for
// production traffic to accumulate evidence. It answers the question
// design.md poses and repeated audits of this codebase have kept unanswered
// on purpose — "does this site actually provide measurable predictive
// skill, and has it earned more authority than logged" — with reproducible
// evidence rather than a design's own plausibility.
//
// It promotes nothing. Every computation here produces a report; the
// judgment.yaml line that grants a site more authority is still a person's
// decision, made after reading what this package found. See eligibility.go.
package judgeval

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
)

// DatasetSchemaVersion is the current version of the Case format. A
// breaking change to the fields below — not an additive one — must bump
// this, so a loader reading an old file can say so instead of silently
// misinterpreting a field that changed meaning.
const DatasetSchemaVersion = 1

// Split is which half of a site's dataset a Case belongs to.
//
// The rule this type exists to make impossible to get wrong by accident
// (never to enforce against a determined caller — see docs on split
// discipline): SplitDev may be used to tune question wording, thresholds,
// and site implementation. SplitHeldout may not, and a promotion decision
// must read only SplitHeldout results. See judgment-validation.md
// "Development versus held-out" for the full rule and its one exception
// (a material site-version bump invalidates a prior held-out use and
// requires a fresh one).
type Split string

const (
	SplitDev     Split = "dev"
	SplitHeldout Split = "heldout"
)

// Valid reports whether s is one of the two defined splits.
func (s Split) Valid() bool { return s == SplitDev || s == SplitHeldout }

// Source says where a Case's state and ground truth came from.
type Source string

const (
	// SourceReal is a case built from an actual repository event: a real
	// diff hunk, a real failure, a real plan. Ground truth for these
	// usually needs a human label (see Annotation) because no deterministic
	// rule can derive it after the fact.
	SourceReal Source = "real"
	// SourceMutation is a case a deterministic generator produced by
	// transforming a real input under a named, seeded rule — see
	// mutation.go. Ground truth is exact, because the rule that produced the
	// case also defines its label; a mutation generator must never use a
	// model to invent the label it is graded against (design instruction:
	// ground truth must not depend on the system under test).
	SourceMutation Source = "mutation"
	// SourceFixture is a hand-authored synthetic case, written to exercise
	// one specific behavior (an adversarial injection string, an edge case
	// no real trace has produced yet).
	SourceFixture Source = "fixture"
)

// Valid reports whether src is one of the three defined sources.
func (src Source) Valid() bool {
	return src == SourceReal || src == SourceMutation || src == SourceFixture
}

// Case is one labeled example for one judgment site.
//
// State and GroundTruth are json.RawMessage rather than a shared struct
// because every site's state shape and label shape are different — an M4
// case's state is a diff hunk and objective; an M6 case's state is a failure
// transcript excerpt. A Site-specific adapter (see runner.go) is what
// interprets them; this type only carries them intact, deterministically,
// and under version control.
type Case struct {
	SchemaVersion int    `json:"schema_version"`
	Site          string `json:"site"`
	// SiteVersion pins the case to the question version it was labeled
	// against. A case built for site version "1" is not evidence about
	// version "2": if the proposition changed, the ground truth may no
	// longer answer the question actually being asked. Calibration already
	// partitions live predictions this way (ledger.CalibrateGrouped); a
	// dataset must partition the same way or a report could silently mix
	// two different questions the way pooling shadow and operational rows
	// would mix two different populations.
	SiteVersion string `json:"site_version"`
	// CaseID is stable and unique within one site's dataset. It is what an
	// Annotation and a manifest reference, so it must not be reused for a
	// different case even after the original is removed.
	CaseID string `json:"case_id"`
	Source Source `json:"source"`
	Split  Split  `json:"split"`
	// State is the exact structured input the site's production code would
	// build into a judgment.State — kept as the adapter's own JSON shape so
	// a dataset file is inspectable without this package's code.
	State json.RawMessage `json:"state"`
	// GroundTruth is the label(s) this case is graded against. Its shape is
	// site-specific: a Noul site's ground truth is typically a bool or
	// "ambiguous"; a Choice site's is one of its option names or
	// "ambiguous". See Annotation.Label for the same vocabulary applied to
	// a human-in-the-loop case.
	GroundTruth json.RawMessage `json:"ground_truth"`
	// AllowedAnswers restates the site's own answer set for this case, so a
	// reader of the dataset file does not have to cross-reference the
	// site's Go source to know what GroundTruth could legally contain.
	AllowedAnswers []string `json:"allowed_answers,omitempty"`
	// Tags are free-form, e.g. "adversarial", "edge-case", "distractor".
	Tags []string `json:"tags,omitempty"`
	// Provenance says where State came from in enough detail to trace it
	// back — a commit hash and file path, a task ID, or "hand-authored" for
	// SourceFixture.
	Provenance string `json:"provenance,omitempty"`
	// TaskRef is the internal task ID this case was derived from, when one
	// exists. Empty for SourceFixture and for SourceMutation cases derived
	// from something other than a task run.
	TaskRef string `json:"task_ref,omitempty"`
	// Difficulty is an optional free-form tag ("edge-case", "clear") a
	// human annotator or the mutation generator may set. Never required.
	Difficulty string `json:"difficulty,omitempty"`
}

// Validate reports a case that cannot be used — a structural problem, not a
// judgment about whether the case is a *good* one.
func (c Case) Validate() error {
	if c.SchemaVersion != DatasetSchemaVersion {
		return fmt.Errorf("case %s: schema_version %d, this build reads %d",
			c.CaseID, c.SchemaVersion, DatasetSchemaVersion)
	}
	if c.Site == "" {
		return fmt.Errorf("case %s: site is required", c.CaseID)
	}
	if c.SiteVersion == "" {
		return fmt.Errorf("case %s: site_version is required", c.CaseID)
	}
	if c.CaseID == "" {
		return fmt.Errorf("case with no case_id in site %s dataset", c.Site)
	}
	if !c.Source.Valid() {
		return fmt.Errorf("case %s: unknown source %q", c.CaseID, c.Source)
	}
	if !c.Split.Valid() {
		return fmt.Errorf("case %s: unknown split %q; use dev or heldout", c.CaseID, c.Split)
	}
	if len(c.State) == 0 {
		return fmt.Errorf("case %s: state is required", c.CaseID)
	}
	if len(c.GroundTruth) == 0 {
		return fmt.Errorf("case %s: ground_truth is required", c.CaseID)
	}
	return nil
}

// Dataset is every case for one site, as loaded from disk.
type Dataset struct {
	Site  string
	Cases []Case
}

// Dev returns the development-split subset.
func (d Dataset) Dev() []Case { return d.filter(SplitDev) }

// Heldout returns the held-out subset.
func (d Dataset) Heldout() []Case { return d.filter(SplitHeldout) }

func (d Dataset) filter(s Split) []Case {
	var out []Case
	for _, c := range d.Cases {
		if c.Split == s {
			out = append(out, c)
		}
	}
	return out
}

// LoadDataset reads one site's dataset from a JSONL file: one Case per line,
// blank lines and lines starting with "#" ignored so a dataset file can
// carry comments the way a fixture would. Cases are sorted by CaseID after
// loading, so the file's on-disk line order never affects a result — two
// people re-ordering their working copies must not get different reports
// from the same case set.
func LoadDataset(path string) (Dataset, error) {
	f, err := os.Open(path)
	if err != nil {
		return Dataset{}, err
	}
	defer f.Close()
	return decodeDataset(f)
}

func decodeDataset(r io.Reader) (Dataset, error) {
	var ds Dataset
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		raw := sc.Bytes()
		trimmed := trimSpaceBytes(raw)
		if len(trimmed) == 0 || trimmed[0] == '#' {
			continue
		}
		var c Case
		if err := json.Unmarshal(trimmed, &c); err != nil {
			return Dataset{}, fmt.Errorf("line %d: %w", line, err)
		}
		if err := c.Validate(); err != nil {
			return Dataset{}, fmt.Errorf("line %d: %w", line, err)
		}
		if ds.Site == "" {
			ds.Site = c.Site
		} else if c.Site != ds.Site {
			return Dataset{}, fmt.Errorf("line %d: case %s is for site %q, dataset is for %q "+
				"(one dataset file is one site's cases, never mixed)", line, c.CaseID, c.Site, ds.Site)
		}
		if seen[c.CaseID] {
			return Dataset{}, fmt.Errorf("line %d: duplicate case_id %q", line, c.CaseID)
		}
		seen[c.CaseID] = true
		ds.Cases = append(ds.Cases, c)
	}
	if err := sc.Err(); err != nil {
		return Dataset{}, err
	}
	sort.Slice(ds.Cases, func(i, j int) bool { return ds.Cases[i].CaseID < ds.Cases[j].CaseID })
	return ds, nil
}

func trimSpaceBytes(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && (b[i] == ' ' || b[i] == '\t' || b[i] == '\r') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\t' || b[j-1] == '\r') {
		j--
	}
	return b[i:j]
}

// SaveDataset writes cases as JSONL, sorted by CaseID, so a diff of a
// regenerated dataset file shows only the actual content change.
func SaveDataset(path string, cases []Case) error {
	sorted := append([]Case(nil), cases...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].CaseID < sorted[j].CaseID })
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, c := range sorted {
		if err := c.Validate(); err != nil {
			return err
		}
		if err := enc.Encode(c); err != nil {
			return err
		}
	}
	return nil
}
