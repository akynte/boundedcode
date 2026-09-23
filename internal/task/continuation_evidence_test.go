package task

import (
	"fmt"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/workflow"
)

// What a continuation must carry besides the fact that work happened.
//
// The defect these cover is that the summary named the calls already made and
// nothing about what they found, so the model's only way to recover the
// contents of a file it had already read was to read it again — which refills
// the context and reaches the next boundary having learned nothing.

func evidenceState() *workflow.State {
	s := continuationState()
	s.Reads = []workflow.ReadEvidence{
		{Tool: "read_file", Path: "routergroup.go", Detail: "lines 1-240", Digest: "d1",
			Bytes: 8251, Seq: 1,
			Decls: []string{"func (group *RouterGroup) createStaticHandler(...)"}},
		{Tool: "read_file", Path: "logger.go", Digest: "d2", Bytes: 6633, Seq: 2,
			Decls: []string{"func LoggerWithConfig(conf LoggerConfig) HandlerFunc"}},
		{Tool: "search_code", Detail: `query="allNoRoute"`, Digest: "d3", Seq: 3, Empty: true},
		{Tool: "read_file", Path: "context.go", Digest: "d4", Bytes: 32781, Seq: 4,
			Excerpt: "package gin / type Context struct {"},
	}
	return s
}

// Case 4. The evidence survives the context reset and reaches the model.
func TestReadEvidenceSurvivesTheBoundary(t *testing.T) {
	s := evidenceState()
	got := continuationSummary(&Task{ID: "T1", Title: "static files log two 404 lines"},
		s, []string{"routergroup.go"}, "context limit reached")

	for _, want := range []string{
		"already inspected",
		"routergroup.go",
		"createStaticHandler", // what was found there, not just that it was read
		"logger.go",
		"LoggerWithConfig",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the summary omits %q\n---\n%s", want, got)
		}
	}
}

// Case 5. The range already read is named, so the model can tell what it has
// from what it has not.
func TestEvidenceNamesTheRangeAlreadyRead(t *testing.T) {
	s := evidenceState()
	got := continuationSummary(&Task{ID: "T1", Title: "x"}, s, nil, "")
	if !strings.Contains(got, "lines 1-240") {
		t.Errorf("the range already read was not carried across:\n%s", got)
	}
}

// An inspection that found nothing is evidence too, and is reported as such
// rather than silently dropped.
func TestBarrenInspectionsAreReported(t *testing.T) {
	s := evidenceState()
	got := continuationSummary(&Task{ID: "T1", Title: "x"}, s, nil, "")
	if !strings.Contains(got, "returned nothing useful") {
		t.Errorf("an empty search was not reported as barren:\n%s", got)
	}
	if !strings.Contains(got, "allNoRoute") {
		t.Errorf("the barren search was not named:\n%s", got)
	}
}

// A plan file nobody opened is the most useful thing to say next, so it is
// said explicitly rather than left for the model to infer from absence.
func TestUnreadPlanFilesAreNamed(t *testing.T) {
	s := evidenceState()
	got := continuationSummary(&Task{ID: "T1", Title: "x"}, s, nil, "")
	if !strings.Contains(got, "not inspected yet") {
		t.Fatalf("unread plan files were not reported:\n%s", got)
	}
	// routergroup_test.go is in the plan and absent from the evidence.
	if !strings.Contains(got, "routergroup_test.go") {
		t.Errorf("the unopened plan file was not named:\n%s", got)
	}
}

// A modified file outranks everything: re-reading it after a boundary is how
// an edit gets reverted, so it must be at the top of what the model is told.
func TestModifiedFilesRankFirstInEvidence(t *testing.T) {
	s := evidenceState()
	ranked := rankEvidence(s, []string{"context.go"})
	if len(ranked) == 0 {
		t.Fatal("no evidence ranked")
	}
	if ranked[0].Path != "context.go" {
		t.Errorf("the modified file did not rank first: got %q", ranked[0].Path)
	}
}

// Case 10, at the rendering boundary: the section is bounded however much
// evidence exists, and says what it left out rather than truncating silently.
func TestEvidenceSectionStaysBounded(t *testing.T) {
	s := evidenceState()
	var many []workflow.ReadEvidence
	for i := range 400 {
		many = append(many, workflow.ReadEvidence{
			Tool: "read_file", Path: fmt.Sprintf("pkg/file%03d.go", i),
			Digest: fmt.Sprintf("d%d", i), Bytes: 40000, Seq: i + 10,
			Decls: []string{strings.Repeat("func VeryLongDeclarationName", 4)},
		})
	}
	s.Reads = many

	got := renderEvidence(s, nil)
	if len(got) > evidenceByteBudget*2 {
		t.Fatalf("the evidence section is %d bytes, far past its %d budget",
			len(got), evidenceByteBudget)
	}
	if !strings.Contains(got, "further inspection(s)") {
		t.Errorf("the section truncated without saying so:\n%s", got)
	}
	// And the full cache is untouched by what the rendering chose to show.
	if len(s.Reads) != 400 {
		t.Errorf("rendering mutated the evidence cache: %d records left", len(s.Reads))
	}
}

// Case 8. The model-visible do-not-repeat list is bounded, the canonical set
// behind it is not, and what survives the clip is the expensive end.
func TestRepetitionMemoryIsBoundedButComplete(t *testing.T) {
	s := continuationState()
	var tried []workflow.TriedCall
	// One call repeated many times, and a long tail of cheap ones.
	tried = append(tried, workflow.TriedCall{
		Fingerprint: "read_file\x00{\"path\":\"the_expensive_one.go\"}",
		Digest:      "dx", Count: 9, Corrected: true,
	})
	for i := range 200 {
		tried = append(tried, workflow.TriedCall{
			Fingerprint: fmt.Sprintf("read_file\x00{\"path\":\"cheap%03d.go\"}", i),
			Digest:      "dc", Count: 2,
		})
	}
	s.Tried = tried

	got := continuationSummary(&Task{ID: "T1", Title: "x"}, s, nil, "")

	// The canonical set is untouched: this is what re-seeds the engine's
	// loop guard, and it must not be the clipped view.
	if len(s.Tried) != 201 {
		t.Fatalf("the canonical tried set was mutated: %d entries", len(s.Tried))
	}
	// The expensive repeat survived the clip.
	if !strings.Contains(got, "the_expensive_one.go") {
		t.Errorf("the most-repeated call was clipped out of the summary:\n%s", got)
	}
	// And the list is bounded, with the omission stated.
	if !strings.Contains(got, "and") || !strings.Contains(got, "more") {
		t.Errorf("the do-not-repeat list truncated without saying so:\n%s", got)
	}
	shown := strings.Count(got, "time(s), same answer")
	if shown > maxTriedShown+1 {
		t.Errorf("the do-not-repeat list showed %d entries, past its bound", shown)
	}
}

// A corrected call is never clipped out in favour of an uncorrected one at
// the same count: the supervisor already spent a turn on it.
func TestCorrectedCallsOutrankUncorrected(t *testing.T) {
	tried := []workflow.TriedCall{
		{Fingerprint: "read_file\x00{\"path\":\"z.go\"}", Digest: "d", Count: 3},
		{Fingerprint: "read_file\x00{\"path\":\"a.go\"}", Digest: "d", Count: 3, Corrected: true},
	}
	lines := describeTried(tried)
	if len(lines) < 2 {
		t.Fatalf("expected both calls described, got %v", lines)
	}
	if !strings.Contains(lines[0], "a.go") {
		t.Errorf("the corrected call did not rank first: %v", lines)
	}
}

// Case 9, at the summary level: the accepted plan crosses the boundary intact.
func TestAcceptedPlanSurvivesTheBoundary(t *testing.T) {
	s := evidenceState()
	got := continuationSummary(&Task{ID: "T1", Title: "x"}, s, []string{"routergroup.go"}, "")
	for _, want := range []string{
		"the filter writes a response", // root cause
		"You may write only:",          // write allowlist
		"go test",                      // how it is shown to work
		"already modified in the worktree",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the plan did not survive: %q missing\n%s", want, got)
		}
	}
}
