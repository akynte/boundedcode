package judgeval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validCase(id string, split Split) Case {
	return Case{
		SchemaVersion: DatasetSchemaVersion,
		Site:          "verification_integrity",
		SiteVersion:   "1",
		CaseID:        id,
		Source:        SourceFixture,
		Split:         split,
		State:         []byte(`{"objective":"o"}`),
		GroundTruth:   []byte(`{"label":"weakening"}`),
	}
}

func TestDatasetRoundTripsThroughDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "verification_integrity.jsonl")
	cases := []Case{validCase("b", SplitHeldout), validCase("a", SplitDev)}
	if err := SaveDataset(path, cases); err != nil {
		t.Fatal(err)
	}
	ds, err := LoadDataset(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.Cases) != 2 {
		t.Fatalf("got %d cases, want 2", len(ds.Cases))
	}
	// Sorted by CaseID regardless of the order passed to SaveDataset or
	// the file's on-disk order — a reordered working copy must not change
	// a result.
	if ds.Cases[0].CaseID != "a" || ds.Cases[1].CaseID != "b" {
		t.Fatalf("cases not sorted by CaseID: %+v", ds.Cases)
	}
	if got := ds.Dev(); len(got) != 1 || got[0].CaseID != "a" {
		t.Fatalf("Dev() = %+v, want [a]", got)
	}
	if got := ds.Heldout(); len(got) != 1 || got[0].CaseID != "b" {
		t.Fatalf("Heldout() = %+v, want [b]", got)
	}
}

func TestDatasetRejectsMixedSites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mixed.jsonl")
	c1 := validCase("a", SplitDev)
	c2 := validCase("b", SplitDev)
	c2.Site = "review_rubric"
	body := mustJSONLine(t, c1) + mustJSONLine(t, c2)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDataset(path); err == nil {
		t.Fatal("expected an error mixing two sites in one dataset file")
	}
}

func TestDatasetRejectsDuplicateCaseID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dup.jsonl")
	c := validCase("a", SplitDev)
	body := mustJSONLine(t, c) + mustJSONLine(t, c)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDataset(path); err == nil {
		t.Fatal("expected an error for a duplicate case_id")
	}
}

func TestDatasetIgnoresBlankAndCommentLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commented.jsonl")
	body := "# a comment\n\n" + mustJSONLine(t, validCase("a", SplitDev))
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	ds, err := LoadDataset(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.Cases) != 1 {
		t.Fatalf("got %d cases, want 1", len(ds.Cases))
	}
}

func TestCaseValidateCatchesEveryRequiredField(t *testing.T) {
	base := validCase("a", SplitDev)
	tests := []struct {
		name   string
		mutate func(*Case)
	}{
		{"schema version", func(c *Case) { c.SchemaVersion = 99 }},
		{"site", func(c *Case) { c.Site = "" }},
		{"site version", func(c *Case) { c.SiteVersion = "" }},
		{"case id", func(c *Case) { c.CaseID = "" }},
		{"source", func(c *Case) { c.Source = "bogus" }},
		{"split", func(c *Case) { c.Split = "bogus" }},
		{"state", func(c *Case) { c.State = nil }},
		{"ground truth", func(c *Case) { c.GroundTruth = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base
			tt.mutate(&c)
			if err := c.Validate(); err == nil {
				t.Fatalf("expected Validate to reject a missing/invalid %s", tt.name)
			}
		})
	}
}

func mustJSONLine(t *testing.T, c Case) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "x.jsonl")
	if err := SaveDataset(p, []Case{c}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(body))
	return line + "\n"
}
