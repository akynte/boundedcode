package native

import (
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/workflow"
)

// The evidence cache records what a call found, so a continuation is not
// reduced to re-reading everything to recover it. These tests pin the two
// properties that make it safe to rely on: it is bounded, and it never
// disagrees with the loop guard about what counts as the same call.

// Case 6. A different range is a different question, and must stay allowed.
func TestDifferentRangeIsNotARepeat(t *testing.T) {
	p := newProgress([]string{"read_file"}, nil)

	if v, _ := p.observeResult("read_file", `{"path":"a.go","start":1,"end":240}`, "first chunk"); v != learned {
		t.Fatal("the first read was not counted as new")
	}
	if v, _ := p.observeResult("read_file", `{"path":"a.go","start":400,"end":600}`, "second chunk"); v != learned {
		t.Error("reading a different range of the same file was treated as a repeat")
	}
	// Both ranges are remembered separately, so the model can tell which part
	// of the file it still has not seen.
	reads := p.Reads()
	if len(reads) != 2 {
		t.Fatalf("expected two records for two ranges, got %d", len(reads))
	}
	var details []string
	for _, r := range reads {
		details = append(details, r.Detail)
	}
	joined := strings.Join(details, " ")
	if !strings.Contains(joined, "lines 1-240") || !strings.Contains(joined, "lines 400-600") {
		t.Errorf("the ranges were not recorded distinctly: %v", details)
	}
}

// Case 7. A file whose contents changed answers differently, so re-reading it
// is new information rather than a loop. This is what lets a model check its
// own edit.
func TestReReadingAChangedFileIsNotARepeat(t *testing.T) {
	p := newProgress([]string{"read_file"}, nil)
	args := `{"path":"a.go"}`

	p.observeResult("read_file", args, "package a\nfunc Add() {}")
	if v, _ := p.observeResult("read_file", args, "package a\nfunc Add() {}"); v != repeated {
		t.Error("an identical re-read was not counted as a repeat")
	}
	if v, _ := p.observeResult("read_file", args, "package a\nfunc Add() { return 1 }"); v != learned {
		t.Error("re-reading a file that changed was treated as a repeat")
	}
	// The cache keeps one record per distinct call, refreshed to the latest
	// answer, rather than growing with every invocation.
	if got := len(p.Reads()); got != 1 {
		t.Errorf("expected one record for one call, got %d", got)
	}
}

// Declarations are pulled out of the result by a fixed scan, so a
// continuation can say what is in a file rather than only that it was opened.
func TestDeclarationsAreExtractedDeterministically(t *testing.T) {
	p := newProgress([]string{"read_file"}, nil)
	body := "package gin\n\nimport \"net/http\"\n\n" +
		"type RouterGroup struct {\n\tHandlers HandlersChain\n}\n\n" +
		"func (group *RouterGroup) createStaticHandler(p string) HandlerFunc {\n\treturn nil\n}\n"
	p.observeResult("read_file", `{"path":"routergroup.go"}`, body)

	reads := p.Reads()
	if len(reads) != 1 {
		t.Fatalf("expected one record, got %d", len(reads))
	}
	decls := strings.Join(reads[0].Decls, " | ")
	if !strings.Contains(decls, "createStaticHandler") {
		t.Errorf("the function was not extracted: %q", decls)
	}
	if !strings.Contains(decls, "RouterGroup") {
		t.Errorf("the type was not extracted: %q", decls)
	}
	if reads[0].Bytes != len(body) {
		t.Errorf("the size of the result was not recorded: %d", reads[0].Bytes)
	}
}

// A result that says nothing is recorded as barren rather than dropped: where
// not to look is worth carrying across a boundary too.
func TestEmptyResultsAreRecordedAsBarren(t *testing.T) {
	p := newProgress([]string{"search_code"}, nil)
	p.observeResult("search_code", `{"query":"nowhere"}`, "   \n  ")
	reads := p.Reads()
	if len(reads) != 1 {
		t.Fatalf("expected one record, got %d", len(reads))
	}
	if !reads[0].Empty {
		t.Error("an empty result was not marked barren")
	}
}

// Case 4 at the engine boundary: evidence seeded from a previous phase is
// still there, and the call it describes is still recognised as already made.
func TestSeededEvidenceSurvivesAndKeepsItsIdentity(t *testing.T) {
	first := newProgress([]string{"read_file"}, nil)
	first.observeResult("read_file", `{"path":"a.go","start":1,"end":240}`, "chunk")
	carried := first.Reads()
	if len(carried) != 1 {
		t.Fatalf("expected one record to carry, got %d", len(carried))
	}

	// A fresh conversation, seeded the way continueEdit seeds one.
	second := newProgress([]string{"read_file"}, first.Tried())
	second.seedReads(carried)

	if got := len(second.Reads()); got != 1 {
		t.Fatalf("the evidence did not survive the boundary: %d record(s)", got)
	}
	// The same call after the boundary is recognised as the one already made,
	// which is what stops the fresh budget going on recovering it.
	if v, _ := second.observeResult("read_file", `{"path":"a.go","start":1,"end":240}`, "chunk"); v != repeated {
		t.Error("the call was not recognised as already made after the boundary")
	}
	if got := len(second.Reads()); got != 1 {
		t.Errorf("the repeat created a second record: %d", got)
	}
}

// The cache cannot grow without bound however long the model loops.
func TestEvidenceCacheIsBounded(t *testing.T) {
	p := newProgress([]string{"read_file"}, nil)
	for i := range maxEvidenceRecords * 3 {
		p.observeResult("read_file", `{"path":"f`+itoa(i)+`.go"}`, "body of f"+itoa(i))
	}
	if got := len(p.Reads()); got > maxEvidenceRecords {
		t.Fatalf("the cache holds %d records, past its %d bound", got, maxEvidenceRecords)
	}
	// Eviction is by recency, so the newest survives.
	reads := p.Reads()
	last := reads[len(reads)-1]
	if !strings.Contains(last.Path, itoa(maxEvidenceRecords*3-1)) {
		t.Errorf("the most recent record was evicted: %q", last.Path)
	}
}

// Seeding must not lose the ordering that recency depends on.
func TestSeedingPreservesRecencyOrdering(t *testing.T) {
	// Keys as observeRead computes them: a restored record carries the
	// fingerprint of the call it describes.
	seed := []workflow.ReadEvidence{
		{Key: fingerprint("read_file", `{"path":"old.go"}`),
			Tool: "read_file", Path: "old.go", Digest: "d1", Seq: 1},
		{Key: fingerprint("read_file", `{"path":"new.go"}`),
			Tool: "read_file", Path: "new.go", Digest: "d2", Seq: 9},
	}
	p := newProgress([]string{"read_file"}, nil)
	p.seedReads(seed)
	p.observeResult("read_file", `{"path":"newest.go"}`, "body")

	reads := p.Reads()
	if len(reads) != 3 {
		t.Fatalf("expected three records, got %d", len(reads))
	}
	if reads[len(reads)-1].Path != "newest.go" {
		t.Errorf("the newest record is not last: %v", reads[len(reads)-1].Path)
	}
	if reads[0].Path != "old.go" {
		t.Errorf("the oldest record is not first: %v", reads[0].Path)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
