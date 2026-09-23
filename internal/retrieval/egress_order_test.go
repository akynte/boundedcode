package retrieval_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/analyzers/golang"
	"github.com/akynte/boundedcode/internal/index"
	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/llm"
	"github.com/akynte/boundedcode/internal/retrieval"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/workspace"
)

// The ordering these tests defend.
//
// An earlier version of Build reranked before Guard and before the sensitive
// path filter, so a candidate that was about to be rejected for belonging to
// another workspace — or dropped for being a secret — had already had its path
// and symbol name sent to a third party. Both are proven absent here by
// inspecting the outgoing state, not by checking that source text is missing:
// the leak was never source, it was the map of somebody's repository.

// indexedRepo indexes a real directory holding one ordinary source file and
// one secret, so the slices carry genuine file paths. A hand-built graph node
// has no file association, and sliceFromNode then substitutes its FQN — which
// is exactly the case Slice.PathKnown exists for and not the case these tests
// are about.
func indexedRepo(t *testing.T) *retrieval.Retriever {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":                "module example.test/egress\n\ngo 1.26\n",
		"internal/pkg/limit.go": "package pkg\n\n// Ordinary applies the account limit.\nfunc Ordinary() int { return 1 }\n",
		// A path policy.Sensitive refuses. The indexer already excludes these,
		// which is asserted below — so a fixture built only on one would pass
		// the egress tests without ever exercising the egress filter.
		"deploy/.env": "SECRET_TOKEN=hunter2\nACCOUNT_LIMIT=5\n",
		// The case that matters. policy.Sensitive does not match secrets.go,
		// so the indexer stores it and the local model may read it;
		// policy.EgressSensitive does match it, so its name must never leave.
		// That two-tier boundary is what this file tests.
		"internal/config/secrets.go": "package config\n\n// AccountLimit is the account limit.\n" +
			"func AccountLimit() int { return 5 }\n",
	} {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	root, err := store.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.CloseAll() })
	ctx := context.Background()
	st, err := root.OpenWorkspace(ctx, workspace.DeriveID(dir, "", t.Name()))
	if err != nil {
		t.Fatal(err)
	}
	// With the Go analyzer, so the fixture produces function nodes with real
	// file paths rather than filesystem containment nodes only.
	ix := index.New(st, index.Options{Analyzers: []index.Analyzer{golang.New()}})
	if err := ix.RegisterRepository(ctx, workspace.Repository{
		ID: "r", Name: "r", Path: dir, DefaultBranch: "main",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ix.Repository(ctx, "r", dir); err != nil {
		t.Fatal(err)
	}
	return retrieval.New(st)
}

// A candidate whose path is egress-sensitive must not contribute anything to
// a request — not its path, not its symbol, not its line range. Because a
// complete ordering is the only kind this uses, its presence skips the whole
// rerank rather than quietly omitting one candidate.
//
// Note what it must *not* do: drop the slice from the packet. The local model
// may read this file; it is only the third party that may not hear about it.
func TestEgressSensitiveCandidateNeverReachesTheJudge(t *testing.T) {
	r := indexedRepo(t)
	answers := map[string]judgment.Answer{}
	for i := range 40 {
		answers["c"+strconv.Itoa(i)] = judgment.Answer{Noul: 0.9}
	}
	j := &judgment.Fake{Answers: answers}
	r.WithJudge(j, nil)

	pkt, err := r.Build(context.Background(), retrieval.Request{
		Symbols:   []string{"Ordinary", "AccountLimit"},
		Objective: "make the account limit configurable", TokenBudget: 20000,
	})
	if err != nil {
		t.Fatal(err)
	}

	var inPacket bool
	for _, s := range pkt.Slices {
		if strings.Contains(s.Path, "secrets.go") {
			inPacket = true
		}
	}
	if !inPacket {
		var got []string
		for _, s := range pkt.Slices {
			got = append(got, s.Path)
		}
		t.Fatalf("the fixture never produced the sensitive candidate; packet holds %v "+
			"(generated %v)", got, pkt.Generated)
	}
	for _, sent := range j.Sent() {
		for _, leak := range []string{"secrets.go", "internal/config"} {
			if strings.Contains(sent, leak) {
				t.Fatalf("an egress-sensitive candidate contributed %q to the request:\n%s",
					leak, sent)
			}
		}
	}
	// The candidate is held out, not the rerank. One file named for a secret
	// used to make a whole repository unrankable — Django has
	// password_validation.py, so every packet in it contained one — and the
	// cost of that bluntness was paid by the candidates that were perfectly
	// safe to ask about.
	if pkt.Rerank.Protected == 0 {
		t.Error("the ineligible candidate was not recorded as protected")
	}
	if !pkt.Rerank.Applied {
		t.Errorf("the rerank was abandoned rather than run over the safe candidates: %q",
			pkt.Rerank.SkipReason)
	}
	// And it is still in the packet: the local model may read this file, and
	// dropping it would be a different decision than not disclosing it.
	var protectedJudged bool
	for _, s := range pkt.Slices {
		if strings.Contains(s.Path, "secrets.go") && s.Judged {
			protectedJudged = true
		}
	}
	if protectedJudged {
		t.Error("a protected candidate carries a relevance, so something judged it")
	}
}

// The protected lane must not be interleaved with the judged one, because a
// probability and a bm25 score are not the same quantity. Judged candidates
// lead; the protected lane keeps the order it arrived in.
func TestProtectedCandidatesFormATailNotAnInterleaving(t *testing.T) {
	r := indexedRepo(t)
	answers := map[string]judgment.Answer{}
	for i := range 40 {
		answers["c"+strconv.Itoa(i)] = judgment.Answer{Noul: float64(i%7) / 10, Answered: true}
	}
	j := &judgment.Fake{Answers: answers}
	r.WithJudge(j, nil)

	pkt, err := r.Build(context.Background(), retrieval.Request{
		Symbols:   []string{"Ordinary", "AccountLimit"},
		Objective: "make the account limit configurable", TokenBudget: 20000,
	})
	if err != nil {
		t.Fatal(err)
	}
	seenUnjudged := false
	for _, s := range pkt.Slices {
		if !s.Judged {
			seenUnjudged = true
			continue
		}
		if seenUnjudged {
			t.Fatalf("judged candidate %s follows an unjudged one; the lanes are interleaved "+
				"and the packet is ordered by two incomparable scales", s.Path)
		}
	}
}

// policy.Sensitive paths are refused a layer earlier still: the indexer never
// stores them, so they are not candidates at all. Asserted so that a change
// loosening the indexer is caught here rather than at an egress boundary.
func TestPolicySensitivePathsAreNotEvenIndexed(t *testing.T) {
	r := indexedRepo(t)
	pkt, err := r.Build(context.Background(), retrieval.Request{
		Symbols: []string{"Ordinary", "AccountLimit"}, Query: "account limit hunter2",
		TokenBudget: 20000,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range pkt.Slices {
		if strings.Contains(s.Path, ".env") || strings.Contains(s.Path, "credentials") {
			t.Fatalf("a policy.Sensitive path became a retrieval candidate: %s", s.Path)
		}
	}
}

// Guard rejects slices belonging to another workspace. It must do so before
// any of them is described to an external service.
func TestForeignCandidateNeverReachesTheJudge(t *testing.T) {
	j := &judgment.Fake{Answers: map[string]judgment.Answer{"c0": {Noul: 0.9}}}
	foreign := retrieval.Slice{
		WorkspaceID: workspace.ID("SOMEONE-ELSES-WORKSPACE"), RepositoryID: "r", WorktreeID: "wt",
		Path: "other/project/private-plan.go", PathKnown: true, Symbol: "ForeignSymbol", ContentHash: "h",
		IndexVersion: 1, Origin: retrieval.OriginAnchor, Score: 9, StartLine: 1, EndLine: 5,
	}
	kept, rejected := retrieval.Guard(workspace.ID("ws"), []retrieval.Slice{foreign})
	if len(rejected) != 1 || len(kept) != 0 {
		t.Fatalf("Guard did not reject the foreign slice: kept %d, rejected %d",
			len(kept), len(rejected))
	}
	// The contract Build relies on: a rejected slice is never in the set the
	// reranker is handed. Asserted here against the reranker directly, so the
	// property survives a refactor of Build's internals.
	retrieval.Rerank(context.Background(), j, "objective", kept, retrieval.RerankTuning{}, nil)
	for _, sent := range j.Sent() {
		for _, leak := range []string{"private-plan.go", "ForeignSymbol", "SOMEONE-ELSES-WORKSPACE"} {
			if strings.Contains(sent, leak) {
				t.Fatalf("a guarded-out candidate contributed %q:\n%s", leak, sent)
			}
		}
	}
	if len(j.Calls()) != 0 {
		t.Fatalf("a request was made for an empty candidate set: %v", j.Sent())
	}
}

// Build must record the candidate set as the generator produced it, before
// reranking, and it must never be rendered into a prompt.
func TestBuildRecordsTheGeneratorSetSeparately(t *testing.T) {
	r := indexedRepo(t)
	pkt, err := r.Build(context.Background(), retrieval.Request{
		Symbols: []string{"Ordinary"}, Objective: "obj", TokenBudget: 20000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkt.Generated) == 0 {
		t.Fatal("the pre-rerank candidate set was not recorded, so a generator miss " +
			"cannot be told from a reranker miss")
	}
	// Generated is the set the *generator* produced, so it legitimately holds
	// paths that are fine locally and ineligible for egress. What matters is
	// that it is evaluator-only and never reaches a request; the egress test
	// above covers that.
	for _, p := range pkt.Generated {
		if p == "" {
			t.Fatal("the generator set holds an empty path")
		}
	}
}

// Parity: both arms must rerank exactly the same safe subset.
//
// The partition is above both rerankers rather than inside each, so this is
// structural — but "the two arms saw the same candidates" is the claim the
// whole B-versus-C comparison rests on, and a claim that load-bearing is
// worth a test rather than an argument.
func TestBothArmsRerankTheSameSafeSubset(t *testing.T) {
	answers := map[string]judgment.Answer{}
	for i := range 40 {
		answers["c"+strconv.Itoa(i)] = judgment.Answer{Noul: 0.5, Answered: true}
	}

	judged := indexedRepo(t)
	fake := &judgment.Fake{Answers: answers}
	judged.WithJudge(fake, nil)
	jp, err := judged.Build(context.Background(), retrieval.Request{
		Symbols: []string{"Ordinary", "AccountLimit"}, Objective: "obj", TokenBudget: 20000,
	})
	if err != nil {
		t.Fatal(err)
	}

	local := indexedRepo(t)
	rec := &recordingEmbedder{}
	local.WithLocalReranker(&retrieval.LocalReranker{Provider: rec}, nil)
	lp, err := local.Build(context.Background(), retrieval.Request{
		Symbols: []string{"Ordinary", "AccountLimit"}, Objective: "obj", TokenBudget: 20000,
	})
	if err != nil {
		t.Fatal(err)
	}

	if jp.Rerank.Total != lp.Rerank.Total || jp.Rerank.Protected != lp.Rerank.Protected {
		t.Errorf("the arms partitioned differently: judged total=%d protected=%d, "+
			"local total=%d protected=%d",
			jp.Rerank.Total, jp.Rerank.Protected, lp.Rerank.Total, lp.Rerank.Protected)
	}
	if jp.Rerank.Protected == 0 {
		t.Fatal("the fixture produced no protected candidate, so this proves nothing")
	}
	// Neither arm may have been shown the protected path.
	for _, sent := range append(fake.Sent(), rec.sent...) {
		if strings.Contains(sent, "secrets.go") || strings.Contains(sent, "internal/config") {
			t.Errorf("a protected candidate was described to a reranker:\n%s", sent)
		}
	}
}

// recordingEmbedder is a local embedding provider that keeps what it was asked
// to embed, so the security assertion covers the local arm too.
type recordingEmbedder struct {
	llm.Provider
	sent []string
}

func (e *recordingEmbedder) Capabilities() llm.Capabilities {
	return llm.Capabilities{Embeddings: true, Local: true}
}

func (e *recordingEmbedder) Embed(_ context.Context, req llm.EmbedRequest) (*llm.EmbedResponse, error) {
	e.sent = append(e.sent, req.Input...)
	out := &llm.EmbedResponse{Dims: 3}
	for range req.Input {
		out.Vectors = append(out.Vectors, []float32{1, 0, 0})
	}
	return out, nil
}

// Graph expansion must be reachable from a lexical objective.
//
// A lexical anchor is a chunk with a path and a line range and no node id,
// so nodeIDs skipped every one and expansion ran only when the caller
// already knew a symbol name. For an objective phrased the way a user
// phrases one — "Prefetch objects don't work with slices" — that meant the
// code graph was built, indexed, and never consulted. The file a change
// belongs in is frequently one call away from the file whose text matches,
// and one call away is exactly what the graph is for.
func TestLexicalAnchorsSeedGraphExpansion(t *testing.T) {
	dir := t.TempDir()
	// Two files with a real call between them. The query matches prose in
	// the caller only; the callee is reachable solely through the graph.
	for name, body := range map[string]string{
		"go.mod": "module example.test/seed\n\ngo 1.26\n",
		"caller/caller.go": "package caller\n\nimport \"example.test/seed/callee\"\n\n" +
			"// Ordinary recalculates the quota ceiling for an account.\n" +
			"func Ordinary() int { return callee.Zzqqx() }\n",
		// Deliberately shares no vocabulary with the query, so the only way
		// it can enter the packet is through the call edge.
		"callee/callee.go": "package callee\n\nfunc Zzqqx() int { return 5 }\n",
	} {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := store.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.CloseAll() })
	ctx := context.Background()
	st, err := root.OpenWorkspace(ctx, workspace.DeriveID(dir, "", t.Name()))
	if err != nil {
		t.Fatal(err)
	}
	ix := index.New(st, index.Options{Analyzers: []index.Analyzer{golang.New()}})
	if err := ix.RegisterRepository(ctx, workspace.Repository{
		ID: "r", Name: "r", Path: dir, DefaultBranch: "main",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ix.Repository(ctx, "r", dir); err != nil {
		t.Fatal(err)
	}

	// The query names no symbol; it matches the caller's comment.
	pkt, err := retrieval.New(st).Build(ctx, retrieval.Request{
		Query:     "recalculates the quota ceiling for an account",
		Objective: "raise the quota ceiling", ExpandDepth: 1, TokenBudget: 20000,
	})
	if err != nil {
		t.Fatal(err)
	}
	var sawAnchor, sawCallee bool
	for _, s := range pkt.Slices {
		t.Logf("  %-16s %s", s.Origin, s.Path)
		if s.Origin == retrieval.OriginAnchor && strings.Contains(s.Path, "caller") {
			sawAnchor = true
		}
		// It must arrive *through the graph*. Finding it lexically would
		// prove nothing about expansion.
		if s.Origin == retrieval.OriginGraph && strings.Contains(s.Path, "callee") {
			sawCallee = true
		}
	}
	if !sawAnchor {
		t.Fatal("the query matched nothing in the caller, so this proves nothing")
	}
	if !sawCallee {
		var got []string
		for _, s := range pkt.Slices {
			got = append(got, string(s.Origin)+":"+s.Path)
		}
		t.Errorf("the callee was never reached: a lexical objective seeded no graph "+
			"expansion, so the code graph was not consulted.\npacket held %v", got)
	}
}

// Expansion must be able to answer "who uses this", not only "what does
// this use".
//
// The forensic trace that forced this: an objective matched text in one
// file, and the file the change belonged in *referenced* that file rather
// than being referenced by it. The relationship sat one hop away in the
// graph and forward-only traversal could not see it. A caller is never
// reachable by following a callee's own edges.
func TestReverseHopReachesConsumers(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"go.mod": "module example.test/rev\n\ngo 1.26\n",
		// The query matches this file. It calls nothing; things call it.
		"core/core.go": "package core\n\n// Ceiling caps the quota ledger for an account.\n" +
			"func Ceiling() int { return 5 }\n",
		// Shares no vocabulary with the query. Reachable only by going
		// backwards along its own call into core.
		"zzq/zzq.go": "package zzq\n\nimport \"example.test/rev/core\"\n\n" +
			"func Wmbl() int { return core.Ceiling() + 1 }\n",
	} {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := store.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.CloseAll() })
	ctx := context.Background()
	st, err := root.OpenWorkspace(ctx, workspace.DeriveID(dir, "", t.Name()))
	if err != nil {
		t.Fatal(err)
	}
	ix := index.New(st, index.Options{Analyzers: []index.Analyzer{golang.New()}})
	if err := ix.RegisterRepository(ctx, workspace.Repository{
		ID: "r", Name: "r", Path: dir, DefaultBranch: "main",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ix.Repository(ctx, "r", dir); err != nil {
		t.Fatal(err)
	}

	pkt, err := retrieval.New(st).Build(ctx, retrieval.Request{
		Query: "caps the quota ledger for an account", Objective: "raise the quota cap",
		ExpandDepth: 1, TokenBudget: 20000,
	})
	if err != nil {
		t.Fatal(err)
	}
	var sawSeed, sawConsumer bool
	for _, s := range pkt.Slices {
		if strings.Contains(s.Path, "core/") {
			sawSeed = true
		}
		if s.Origin == retrieval.OriginGraph && strings.Contains(s.Path, "zzq/") {
			sawConsumer = true
		}
	}
	if !sawSeed {
		t.Fatal("the query matched nothing, so this proves nothing")
	}
	if !sawConsumer {
		var got []string
		for _, s := range pkt.Slices {
			got = append(got, string(s.Origin)+":"+s.Path)
		}
		t.Errorf("the consumer was never reached: expansion follows only the seed's own "+
			"edges, so a caller is invisible.\npacket held %v", got)
	}
}
