package retrieval_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/retrieval"
	"github.com/akynte/boundedcode/internal/workspace"
)

// What must remain true of a judged packet.
//
// The reranker may change the order slices are read in and how the budget is
// divided. It may not change which slices exist, where they came from, what
// the packet says about itself, or what leaves the machine — and when it
// cannot do its job it must say so rather than half-doing it.

func slice(path, symbol string, origin retrieval.Origin, score float64) retrieval.Slice {
	return retrieval.Slice{
		WorkspaceID: workspace.ID("ws"), RepositoryID: "r", WorktreeID: "wt",
		Path: path, PathKnown: true, Symbol: symbol, ContentHash: "h", IndexVersion: 1,
		Origin: origin, Score: score, StartLine: 1, EndLine: 10,
	}
}

func rerank(t *testing.T, j judgment.Judge, objective string, in []retrieval.Slice) retrieval.RerankResult {
	t.Helper()
	return retrieval.Rerank(context.Background(), j, objective, in, retrieval.RerankTuning{}, nil)
}

func TestRerankWithNoJudgeChangesNothing(t *testing.T) {
	in := []retrieval.Slice{
		slice("a.go", "A", retrieval.OriginAnchor, 3),
		slice("b.go", "B", retrieval.OriginGraph, 0.5),
	}
	before, _ := json.Marshal(in)
	res := rerank(t, judgment.Off(), "do the thing", in)
	after, _ := json.Marshal(in)
	if string(before) != string(after) {
		t.Fatalf("an off judge changed the candidates:\n%s\n%s", before, after)
	}
	if res.Attempted || res.Applied || res.SkipReason != retrieval.SkipNoJudge {
		t.Fatalf("result = %+v", res)
	}
}

func TestCompleteJudgmentAppliesReranking(t *testing.T) {
	in := []retrieval.Slice{
		slice("a.go", "A", retrieval.OriginAnchor, 3),
		slice("b.go", "B", retrieval.OriginGraph, 0.5),
	}
	j := &judgment.Fake{Answers: map[string]judgment.Answer{
		"c0": {Noul: 0.2}, "c1": {Noul: 0.9},
	}}
	res := rerank(t, j, "do the thing", in)

	if !res.Attempted || !res.Applied {
		t.Fatalf("a complete answer was not applied: %+v", res)
	}
	if res.Judged != 2 || res.Total != 2 {
		t.Fatalf("judged %d of %d", res.Judged, res.Total)
	}
	for i, s := range in {
		if !s.Judged {
			t.Fatalf("candidate %d was not judged", i)
		}
	}
	if in[0].Score != 3 || in[1].Score != 0.5 {
		t.Fatalf("deterministic Score was overwritten: %v, %v", in[0].Score, in[1].Score)
	}
	if in[0].Relevance != 0.2 || in[1].Relevance != 0.9 {
		t.Fatalf("relevance = %v, %v", in[0].Relevance, in[1].Relevance)
	}
}

// A partial answer is no answer. The candidate set must come back untouched
// and the result must say the treatment was not applied, or a benchmark will
// count a run that received the baseline's ordering as a treated one.
func TestPartialJudgmentFallsBackAndReportsNotApplied(t *testing.T) {
	in := []retrieval.Slice{
		slice("a.go", "A", retrieval.OriginAnchor, 3),
		slice("b.go", "B", retrieval.OriginGraph, 0.5),
		slice("c.go", "C", retrieval.OriginAnchor, 1),
	}
	before, _ := json.Marshal(in)
	j := &judgment.Fake{Answers: map[string]judgment.Answer{"c0": {Noul: 0.9}}}
	res := rerank(t, j, "obj", in)
	after, _ := json.Marshal(in)

	if string(before) != string(after) {
		t.Fatalf("a partial answer altered the candidates:\n%s\n%s", before, after)
	}
	if res.Applied {
		t.Fatal("a partial answer reported itself applied")
	}
	if !res.Attempted {
		t.Fatal("the attempt was not recorded, so the cost is invisible")
	}
	if res.SkipReason != retrieval.SkipPartialAnswer {
		t.Fatalf("skip reason = %q", res.SkipReason)
	}
	if res.Judged == 0 {
		t.Fatal("the answers that did arrive were not counted; the waste is invisible")
	}
}

// Over the limit, the rerank must not happen at all. The earlier version
// judged the first 80, discarded every answer because the tail was unjudged,
// and billed for them.
func TestOverLimitSetMakesNoCallAndSaysWhy(t *testing.T) {
	var in []retrieval.Slice
	for i := range 90 {
		in = append(in, slice("f"+string(rune('a'+i%26))+".go", "S", retrieval.OriginAnchor, float64(i)))
	}
	j := &judgment.Fake{}
	res := retrieval.Rerank(context.Background(), j, "obj", in,
		retrieval.RerankTuning{MaxCandidates: 80}, nil)

	if len(j.Calls()) != 0 {
		t.Fatalf("%d request(s) were made for a set that could not be judged completely",
			len(j.Calls()))
	}
	if res.Applied || res.SkipReason != retrieval.SkipTooMany {
		t.Fatalf("result = %+v", res)
	}
	if res.Requests != 0 {
		t.Fatalf("%d request(s) were billed", res.Requests)
	}
}

// A timeout is an unavailable judge, not a partial one, and it must leave the
// candidates alone.
func TestTimeoutFallsBack(t *testing.T) {
	in := []retrieval.Slice{slice("a.go", "A", retrieval.OriginAnchor, 3)}
	before, _ := json.Marshal(in)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := retrieval.Rerank(ctx, &judgment.Fake{Err: context.Canceled}, "obj", in,
		retrieval.RerankTuning{}, nil)
	after, _ := json.Marshal(in)
	if string(before) != string(after) {
		t.Fatal("a cancelled rerank changed the candidates")
	}
	if res.Applied {
		t.Fatal("a cancelled rerank reported itself applied")
	}
}

func TestDisabledJudgeIsNotAnAttempt(t *testing.T) {
	in := []retrieval.Slice{slice("a.go", "A", retrieval.OriginAnchor, 3)}
	res := rerank(t, &judgment.Fake{Unavailable: true}, "obj", in)
	if res.Attempted {
		t.Fatal("an unavailable judge counted as an attempt, inflating the denominator")
	}
}

// --- what leaves the machine ----------------------------------------------

func TestSensitiveCandidateBlocksTheWholeRequest(t *testing.T) {
	in := []retrieval.Slice{
		slice("internal/task/runner.go", "Run", retrieval.OriginAnchor, 3),
		slice("deploy/.env", "", retrieval.OriginAnchor, 2),
	}
	j := &judgment.Fake{Answers: map[string]judgment.Answer{"c0": {Noul: 0.9}, "c1": {Noul: 0.9}}}
	res := rerank(t, j, "obj", in)

	if len(j.Calls()) != 0 {
		t.Fatalf("a request was made with an egress-sensitive candidate in scope: %v", j.Sent())
	}
	if res.SkipReason != retrieval.SkipIneligible {
		t.Fatalf("skip reason = %q", res.SkipReason)
	}
	for _, sent := range j.Sent() {
		if strings.Contains(sent, ".env") {
			t.Fatalf("a sensitive path reached the state: %s", sent)
		}
	}
}

func TestStrictModeSendsNoRepositorySource(t *testing.T) {
	in := []retrieval.Slice{slice("a.go", "A", retrieval.OriginAnchor, 3)}
	in[0].Signature = "func A(secretArgument string) error"
	in[0].Body = "the body of the function, which is repository source"

	j := &judgment.Fake{Answers: map[string]judgment.Answer{"c0": {Noul: 0.5}}}
	rerank(t, j, "objective", in)

	if j.RepoTextSent() != 0 {
		t.Fatalf("%d repository source field(s) were sent under the default redaction mode",
			j.RepoTextSent())
	}
	if j.RepoMetadataSent() == 0 {
		t.Fatal("no repository metadata was accounted for; the journal would under-report")
	}
	for _, sent := range j.Sent() {
		if strings.Contains(sent, "secretArgument") || strings.Contains(sent, "repository source") {
			t.Fatalf("repository source reached the state:\n%s", sent)
		}
		if !strings.Contains(sent, "a.go") || !strings.Contains(sent, `"A"`) {
			t.Fatalf("the path and symbol were not sent, so the question is about nothing:\n%s", sent)
		}
	}
}

func TestRerankDoesNotTellTheJudgeHowTheSliceWasFound(t *testing.T) {
	in := []retrieval.Slice{slice("b.go", "B", retrieval.OriginGraph, 0.5)}
	in[0].Depth = 2
	j := &judgment.Fake{Answers: map[string]judgment.Answer{"c0": {Noul: 0.5}}}
	rerank(t, j, "objective", in)

	for _, sent := range j.Sent() {
		for _, leak := range []string{"graph_expansion", "lexical_anchor", "origin", "depth", "score"} {
			if strings.Contains(sent, leak) {
				t.Fatalf("the state carries %q, which is the ranking the judgment is "+
					"supposed to be independent of:\n%s", leak, sent)
			}
		}
	}
}

// --- the wire contract ----------------------------------------------------

// The candidate a question is about must be named in the proposition. The map
// key is never sent, and TypeSafe's noul criteria are a true/false pair — a
// per-candidate binding anywhere else is a question the service never got.
func TestCandidateIdentityIsInTheProposition(t *testing.T) {
	in := []retrieval.Slice{
		slice("a.go", "A", retrieval.OriginAnchor, 3),
		slice("b.go", "B", retrieval.OriginAnchor, 2),
	}
	j := &judgment.Fake{Answers: map[string]judgment.Answer{"c0": {Noul: 0.5}, "c1": {Noul: 0.5}}}
	rerank(t, j, "objective", in)

	calls := j.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected one batched request, got %d", len(calls))
	}
	for id, q := range calls[0].Questions {
		if q.Kind() != judgment.KindNoul {
			t.Fatalf("question %q is %q, not a noul", id, q.Kind())
		}
		if !strings.Contains(q.Instructions(), "`"+id+"`") {
			t.Fatalf("question %q does not name its candidate in the instructions:\n%s",
				id, q.Instructions())
		}
		if err := q.Validate(); err != nil {
			t.Fatalf("question %q is malformed: %v", id, err)
		}
	}
}

// The rubric must not down-rank tests for being tests. This project's own
// nil-deref task turns on a test that defines the required behaviour while the
// objective never mentions tests.
func TestRelevanceRubricDoesNotExcludeTestsByCategory(t *testing.T) {
	rubric := retrieval.RelevanceRubric()
	for _, required := range []string{
		"implement or verify",
		"tests that define the required behaviour",
		"contracts and interfaces",
		"consumers and callers",
		"configuration or migrations",
	} {
		if !strings.Contains(rubric, required) {
			t.Errorf("the rubric no longer says %q", required)
		}
	}
	for _, forbidden := range []string{
		"unless the objective is about them",
		"unless the task is about tests",
	} {
		if strings.Contains(rubric, forbidden) {
			t.Errorf("the rubric has reintroduced %q, which down-ranks the artefact that "+
				"defines correctness whenever the task text does not mention it", forbidden)
		}
	}
}

// --- mutation invariants ---------------------------------------------------

// A slice whose path is a fallback FQN rather than a real repository path
// cannot be egress-checked, so it is never externalized.
func TestSliceWithoutAKnownPathIsNotExternalized(t *testing.T) {
	in := []retrieval.Slice{slice("pkg.Ordinary", "Ordinary", retrieval.OriginGraph, 1)}
	in[0].PathKnown = false
	j := &judgment.Fake{Answers: map[string]judgment.Answer{"c0": {Noul: 0.9}}}
	res := rerank(t, j, "obj", in)
	if len(j.Calls()) != 0 {
		t.Fatalf("a slice with no real path was described to the judge: %v", j.Sent())
	}
	if res.SkipReason != retrieval.SkipIneligible {
		t.Fatalf("skip reason = %q", res.SkipReason)
	}
}

func TestRerankNeverAddsRemovesOrAltersProvenance(t *testing.T) {
	in := []retrieval.Slice{
		slice("a.go", "A", retrieval.OriginAnchor, 3),
		slice("b.go", "B", retrieval.OriginGraph, 0.5),
	}
	original := append([]retrieval.Slice(nil), in...)
	j := &judgment.Fake{Answers: map[string]judgment.Answer{"c0": {Noul: 0}, "c1": {Noul: 0}}}
	rerank(t, j, "obj", in)

	if len(in) != len(original) {
		t.Fatalf("rerank changed the candidate count: %d", len(in))
	}
	// Relevance zero — the strongest possible "no" — must still leave every
	// slice present with every deterministic field intact.
	for i := range in {
		got, want := in[i], original[i]
		got.Relevance, got.Judged = 0, false
		if got != want {
			t.Fatalf("slice %d was altered beyond its judgment fields:\ngot  %+v\nwant %+v",
				i, got, want)
		}
	}
}
