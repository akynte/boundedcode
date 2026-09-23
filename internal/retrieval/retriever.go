package retrieval

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/graph"
	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/memory"
	"github.com/akynte/boundedcode/internal/policy"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/version"
	"github.com/akynte/boundedcode/internal/workspace"
)

// Retriever answers context requests for one workspace.
//
// The order is fixed by §8.2 and is deliberately not a model decision:
//  1. lexical anchors over FTS5,
//  2. graph expansion from those anchors,
//  3. mandatory slots filled by impact analysis so consumers and contracts are
//     never dropped.
type Retriever struct {
	st     *store.Store
	g      graph.Graph
	mem    *memory.Store
	judge  judgment.Judge
	local  *LocalReranker
	tuning RerankTuning
	logf   func(string, ...any)
}

// New binds a retriever to a workspace store.
func New(s *store.Store) *Retriever { return &Retriever{st: s, g: graph.New(s)} }

// WithMemory attaches the repository's durable notes (§8.2, §11).
//
// They live in the repository rather than the data directory so they travel
// with it, which is why the retriever cannot find them on its own: it knows a
// workspace, and the notes belong to a checkout.
func (r *Retriever) WithMemory(m *memory.Store) *Retriever {
	r.mem = m
	return r
}

// WithJudge attaches a judge that puts one comparable relevance on every
// candidate. See rerank.go for what that changes and what it may not.
//
// It is optional in the strongest sense: without it, and with a judge that is
// off or unreachable, every packet is assembled exactly as it was before this
// existed.
func (r *Retriever) WithJudge(j judgment.Judge, logf func(string, ...any)) *Retriever {
	r.judge = j
	r.logf = logf
	return r
}

// WithLocalReranker attaches the fully local control arm.
//
// It is mutually exclusive with WithJudge by construction below: a run uses
// one reranker or none, because an experiment comparing two treatments must
// not silently apply both.
func (r *Retriever) WithLocalReranker(l *LocalReranker, logf func(string, ...any)) *Retriever {
	r.local = l
	r.logf = logf
	return r
}

// HasJudge reports whether a judge is attached. It exists so an assembly test
// can prove the wiring rather than infer it from a skip reason after the fact.
func (r *Retriever) HasJudge() bool { return r != nil && r.judge != nil }

// JudgeAvailable reports whether the attached judge would answer. A retriever
// carrying judgment.Off() has a judge and is not available, which is the
// ordinary unconfigured case and the one a skip reason of no_judge describes.
func (r *Retriever) JudgeAvailable() bool {
	return r != nil && r.judge != nil && r.judge.Available()
}

// WithTuning overrides the rerank knobs. Zero fields keep the shipped
// defaults, so the eval harness can vary one threshold on the dev set without
// restating the rest and without a rebuild.
func (r *Retriever) WithTuning(t RerankTuning) *Retriever {
	r.tuning = t
	return r
}

// MemoryBudgetFraction is the share of a packet that durable notes may take.
//
// §8.2 adopts this memory at "low" cost and §11 flags the pattern as "good,
// easy to abuse": notes accumulate, and a playbook that grows until it crowds
// out the code is the failure mode. A note that does not fit is dropped and
// counted rather than silently trimmed, so the cap is visible when it bites.
const MemoryBudgetFraction = 0.15

// GraphBudgetFraction is the share of a packet that graph expansion may take.
//
// Expansion has the same failure mode the notes cap exists for, and had no
// equivalent bound: it reaches up to MaxNodes neighbours and appends them, so
// it filled whatever budget the anchors left, on every step of every task. The
// first measured run showed what that costs — the arm with the graph spent
// 1.8x the tokens of the arm without it and solved no more, which is what
// paying full price for a packet nobody read looks like.
//
// A third is deliberately generous. The graph's case is real: a consumer in
// another package is exactly what lexical search cannot find. The claim being
// bounded is not that expansion is worthless, only that it must compete for
// room rather than take what is left.
const GraphBudgetFraction = 0.33

// GraphFloorFraction reserves room for expansion in a judged packet.
//
// The 0.33 cap above exists because inverse depth could not be compared to
// bm25, so expansion had to be fenced off rather than ranked. A judged packet
// does not need the fence: a graph slice at 0.9 outranks an anchor at 0.2 on
// the same question, and it should displace it.
//
// What survives the cap's removal is the opposite guarantee. The graph's case
// is that "a consumer in another package is exactly what lexical search cannot
// find", and a run of confident anchors should not be able to crowd that out
// entirely. So the share becomes a floor instead of a ceiling.
const DefaultGraphFloorFraction = 0.10

// RelevanceFloor is the relevance below which a reserved graph slot is
// released rather than filled. A reservation nobody clears is budget spent on
// noise, which is the failure the cap was protecting against in the first
// place.
const DefaultRelevanceFloor = 0.35

// Graph exposes the code graph this retriever reads, for callers that need
// to check a claim against it rather than retrieve with it — plan validation
// asks whether a symbol exists at all, which is a question about the
// repository and not about relevance.
func (r *Retriever) Graph() graph.Graph { return r.g }

// WorkspaceID reports the workspace this retriever serves.
func (r *Retriever) WorkspaceID() workspace.ID { return r.st.ID() }

// Skeleton returns signatures in selected files, without loading source bodies.
// The file list is bound as values and secret paths never reach the query.
func (r *Retriever) Skeleton(ctx context.Context, paths []string) ([]Slice, error) {
	var out []Slice
	for _, path := range paths {
		if policy.Sensitive(path) {
			continue
		}
		slices, err := r.skeletonOf(ctx, path)
		if err != nil {
			return nil, err
		}
		out = append(out, slices...)
	}
	return out, nil
}

// skeletonOf reads one file's signatures. It is its own function so the rows
// can be closed by defer: a loop that closes them by hand has to get every
// early return right, and one missed path leaks a statement per file.
func (r *Retriever) skeletonOf(ctx context.Context, path string) ([]Slice, error) {
	rows, err := r.st.Index().SQL().QueryContext(ctx, `SELECT n.name, n.signature,n.start_line,n.end_line FROM nodes n JOIN files f ON f.file_id=n.file_id WHERE f.path=? ORDER BY n.start_line,n.node_id LIMIT 500`, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Slice
	for rows.Next() {
		s := Slice{Path: path}
		if err := rows.Scan(&s.Symbol, &s.Signature, &s.StartLine, &s.EndLine); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Request describes what the current step needs.
type Request struct {
	Root string
	// Query is the lexical anchor text.
	Query string
	// Symbols are symbol names to look up directly.
	Symbols []string
	// MaxAnchors caps stage 1.
	MaxAnchors int
	// ExpandDepth caps stage 2. Zero disables graph expansion.
	ExpandDepth int
	// ExpandKinds restricts which edges are followed.
	ExpandKinds []graph.EdgeKind
	// ImpactOf, when set, fills the mandatory consumer slots for a planned
	// change to the named symbols.
	ImpactOf []string
	// ImpactChange classifies that planned change.
	ImpactChange graph.ChangeKind
	// TokenBudget caps the packet. Zero means DefaultTokenBudget.
	TokenBudget int
	// Objective is what the step is trying to do, in the words it was asked
	// in. It is used only to rerank candidates, and only when a judge is
	// configured; empty turns reranking off for this request. It is not the
	// same as Query, which is lexical search text: "make the daily account
	// limit configurable" is an objective, and ExpandTerms turns it into the
	// identifier spellings Query wants.
	Objective string
	// Failure, when set, adds a second relevance proposition per candidate
	// — design.md mechanism M9 — keyed on resolving this specific open
	// verification failure rather than only Objective. Empty on the first
	// attempt of a task, when there is no failure yet to key on.
	Failure FailureQuery
}

// DefaultTokenBudget is a conservative packet cap. §9.3 forbids hardcoding
// real limits: the operative value comes from the active hardware profile, and
// this constant is only the fallback when no profile is loaded.
const DefaultTokenBudget = 6000

// Packet is the assembled context for one step, plus the accounting that makes
// §8.3's metrics possible.
type Packet struct {
	ProjectNotes []memory.ProjectNote `json:"project_notes,omitempty"`
	WorkspaceID  workspace.ID         `json:"workspace_id"`
	Slices       []Slice              `json:"slices"`
	Tokens       int                  `json:"tokens"`
	Budget       int                  `json:"budget"`
	// Rejected counts slices dropped by Guard. Any non-zero value here is an
	// isolation incident and is surfaced, never swallowed (§2.3).
	Rejected []string `json:"rejected,omitempty"`
	// Dropped counts slices that did not fit the budget, which is the signal
	// that the packet cap is too small for the task (§8.3).
	Dropped int `json:"dropped"`
	// Impact is attached when the request asked for it, so the planner and the
	// human gate see the same report (§3.3).
	Impact *graph.Impact `json:"impact,omitempty"`
	// Notes are the repository's durable memory (§8.2): intent explaining why
	// work is being done, observations of what was seen, advice meant to steer
	// it. They are carried separately from Slices because they are not code and
	// must not be read as if they were.
	Notes []memory.Note `json:"notes,omitempty"`
	// NotesDropped counts notes that did not fit the memory budget. A playbook
	// quietly losing its tail is how advice stops matching what people think
	// the system was told.
	NotesDropped int `json:"notes_dropped,omitempty"`
	// Injection is what the context-injection screen found in this packet's
	// slices (design.md mechanism M8). It is carried on the packet rather
	// than consumed inside Build so a caller can put the finding in front of
	// a person at the gate: the notice prepended to a body warns the
	// generator, and this warns the human. Content is never dropped on the
	// strength of it — a repository legitimately contains prompt files.
	Injection InjectionResult `json:"injection,omitzero"`
	// Rerank says whether judged reranking was attempted, whether it was
	// actually applied, and why not when it was not.
	//
	// It replaces a bare count of judged slices, which was unsound: a packet
	// where some candidates were answered and the ordering then fell back to
	// the deterministic path reported a non-zero count, so a benchmark could
	// not tell which runs had received the treatment. Applied is the only
	// field that answers that, and it is true only when judged scores
	// determined both the ordering and the budget fill.
	Rerank RerankResult `json:"rerank,omitzero"`
	// Generated lists the distinct files the candidate set covered *before*
	// reranking, sorted.
	//
	// It exists for the evaluation and for nothing else: scoring it against
	// the files a real fix touched separates a generator that never proposed
	// the right file from a reranker that buried it. It is never rendered
	// into a prompt.
	Generated []string `json:"generated,omitempty"`
}

// Build assembles a packet. Every slice that leaves this function has passed
// Guard, so a packet can never carry another workspace's content.
func (r *Retriever) Build(ctx context.Context, req Request) (*Packet, error) {
	if req.MaxAnchors <= 0 {
		req.MaxAnchors = 20
	}
	budget := req.TokenBudget
	if budget <= 0 {
		budget = DefaultTokenBudget
	}

	var candidates []Slice

	// Stage 1: lexical anchors.
	if req.Query != "" {
		anchors, err := r.anchors(ctx, req.Query, req.MaxAnchors)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, anchors...)
	}
	for _, sym := range req.Symbols {
		nodes, err := r.resolveSymbol(ctx, sym, 10)
		if err != nil {
			return nil, err
		}
		for _, n := range nodes {
			candidates = append(candidates, r.sliceFromNode(n, OriginExplicit, 1.0, 0, "", ""))
		}
	}

	// Stage 2: graph expansion from the anchors.
	if req.ExpandDepth > 0 {
		seeds := append(nodeIDs(candidates), r.anchorSeeds(ctx, candidates)...)
		candidates = append(candidates, r.consumerHop(ctx, seeds)...)
		if len(seeds) > 0 {
			reached, err := r.g.Traverse(ctx, graph.Query{
				Start: seeds, Dir: graph.Forward, Kinds: req.ExpandKinds,
				MaxDepth: req.ExpandDepth, MaxNodes: 500,
			})
			if err != nil {
				return nil, err
			}
			for _, rc := range reached {
				if rc.Depth == 0 {
					continue
				}
				candidates = append(candidates,
					r.sliceFromNode(rc.Node, OriginGraph, 0.5/float64(rc.Depth), rc.Depth, rc.Via, rc.Evidence))
			}
		}
	}

	pkt := &Packet{WorkspaceID: r.st.ID(), Budget: budget}
	if req.Root != "" {
		var repoID string
		if err := r.st.Index().SQL().QueryRowContext(ctx, `SELECT repository_id FROM repositories ORDER BY repository_id LIMIT 1`).Scan(&repoID); err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		notes, err := memory.LoadProject(req.Root, repoID, func(name string) (bool, error) {
			nodes, err := r.g.NodesByName(ctx, name, nil, 1)
			return len(nodes) > 0, err
		})
		if err != nil {
			return nil, err
		}
		spent := 0
		for _, note := range notes {
			cost := len(note.Text)/3 + 100
			if note.Stale || spent+cost > int(float64(budget)*MemoryBudgetFraction) {
				pkt.NotesDropped++
				continue
			}
			pkt.ProjectNotes = append(pkt.ProjectNotes, note)
			spent += cost
		}
		pkt.Tokens += spent
	}

	// Stage 3: mandatory impact slots. These are added last but reserved
	// first: §8.2 requires that consumers and contracts are never dropped, so
	// they are placed into the packet before discretionary slices compete for
	// the remaining budget.
	var mandatory []Slice
	if len(req.ImpactOf) > 0 {
		imp, err := r.impact(ctx, req.ImpactOf, req.ImpactChange)
		if err != nil {
			return nil, err
		}
		pkt.Impact = imp
		for _, c := range imp.Consumers {
			mandatory = append(mandatory, r.sliceFromNode(c.Node, OriginImpact, 1.0, c.Depth, c.Via, c.Evidence))
		}
	}

	// Authorisation first, and nothing external before it.
	//
	// Guard rejects slices belonging to another workspace; policy.Sensitive
	// drops paths whose contents must not enter model context at all. Both
	// used to run *after* the judged rerank, which meant a slice about to be
	// rejected had already had its path and symbol sent to a third party. The
	// order below is the fix, and the comments on Rerank say so too, because
	// this is the kind of ordering a later refactor reverses without noticing.
	kept, rejected := Guard(r.st.ID(), append(mandatory, candidates...))
	for _, err := range rejected {
		pkt.Rejected = append(pkt.Rejected, err.Error())
	}

	// Old indexes may predate the secret-read policy. Filtering here rather
	// than during the fill keeps it at the model boundary while putting it
	// ahead of every external call, and counts each drop once.
	eligible := make([]Slice, 0, len(kept))
	for _, s := range kept {
		if policy.Sensitive(s.Path) {
			pkt.Dropped++
			continue
		}
		eligible = append(eligible, s)
	}

	// Mandatory impact slots are never judged and never reordered against the
	// rest: §8.2 requires consumers and contracts survive, and a probability
	// does not get to renegotiate that. Splitting them out here also keeps
	// them out of the request entirely.
	var mandatoryKept, judgeable []Slice
	for _, s := range eligible {
		if s.Origin == OriginImpact {
			mandatoryKept = append(mandatoryKept, s)
			continue
		}
		judgeable = append(judgeable, s)
	}

	// The candidate set as the generator produced it, before any reranking.
	// Recorded so an evaluation can tell a generator miss from a reranker
	// miss; never shown to the model.
	pkt.Generated = distinctPaths(judgeable)

	var rr RerankResult
	rr, judgeable = r.rerank(ctx, req.Objective, req.Failure, judgeable)
	pkt.Rerank = rr

	// Best first. The score was computed for every slice — bm25 for an anchor,
	// inverse depth for an expanded node — and nothing read it: the packet was
	// filled in the order things happened to be appended, so a depth-2
	// neighbour could displace a better one purely by traversal order.
	//
	// Without a judgment this sorts inside an origin rather than across all of
	// them, because bm25 and inverse depth are not the same scale and
	// comparing them directly would rank by units rather than by relevance.
	// With one, there is a single signal to sort by; whether it orders well
	// across origins in this domain is what the pilot measures.
	sortByScore(judgeable)

	// Notes first: they are the stable part of the packet and §8.2 puts the
	// stable prefix first so the provider's prompt cache survives the loop.
	// Their cost comes out of the budget before code competes for it, bounded
	// so they can never be most of the packet.
	r.addNotes(ctx, req.Objective, pkt, int(float64(budget)*MemoryBudgetFraction))

	r.fill(pkt, append(mandatoryKept, judgeable...), budget, rr.Applied)

	// Injection screening (design.md mechanism M8, first sub-site), over the
	// packet's final slices. At TierOrdering a flagged slice's body gains a
	// stronger notice; at TierLogged, the default, this runs and journals
	// and pkt.Slices is untouched.
	jctx, jobs := judgment.Begin(ctx, InjectionSite)
	inj := CheckInjection(jctx, r.judge, pkt.Slices, InjectionTuning{}, r.logf)
	jobs.Subjects(inj.Candidates)
	jobs.Skip(inj.SkipReason)
	jobs.Finish(ctx, len(inj.Findings))
	pkt.Injection = inj
	if inj.Tier.Permits(judgment.TierOrdering) {
		for _, f := range inj.Findings {
			if f.Index >= 0 && f.Index < len(pkt.Slices) &&
				!strings.HasPrefix(pkt.Slices[f.Index].Body, injectionNotice) {
				// The notice is prepended; the body is never removed or
				// rewritten. A repository legitimately contains prompt
				// files, agent instructions and documentation about such
				// systems, and a screen that dropped them would make those
				// files unreadable to the one process that needs to read
				// them. The gate-visible half of this lives in the caller,
				// which carries pkt.Injection into the outcome.
				pkt.Slices[f.Index].Body = injectionNotice + pkt.Slices[f.Index].Body
			}
		}
	}
	return pkt, nil
}

// rerank applies whichever reranker this retriever was given, or none, to the
// candidates that may be described to it — and returns the candidate set with
// the protected lane merged back.
//
// The local and the judged reranker are alternatives rather than a fallback
// chain: an arm that quietly used the local one when the judged one failed
// would report a treatment it did not receive.
//
// The partition is here, above both of them, for two reasons. It is the only
// way the two arms can be given provably the same input — the safe subset is
// computed once, by one rule, and handed to whichever reranker is configured
// — and it is the only place where "which candidates may leave" is decided
// once rather than per implementation.
//
// Before this, one protected candidate refused the whole set. That was sound
// and far too blunt: Django carries files named password_validation.py, so
// every packet in the repository contained one, every rerank skipped, and
// both arms silently degraded to the deterministic order while reporting
// that they had been attempted. A repository does not become unrankable
// because it has a file about passwords in it.
func (r *Retriever) rerank(ctx context.Context, objective string, failure FailureQuery, candidates []Slice) (RerankResult, []Slice) {
	safe, protected := partitionForEgress(candidates)

	var res RerankResult
	switch {
	case r.local.Available():
		// The local reranker is the benchmarked control for the judged arm
		// (see design.md M9 and docs/explanation/judgments.md): it stays on
		// Objective alone, so the comparison between the two arms is never
		// confounded by one of them seeing a failure the other did not.
		res = r.local.Rerank(ctx, objective, safe, r.logf)
	default:
		tn := r.tuning
		tn.FailureQuery = failure
		res = Rerank(ctx, r.judge, objective, safe, tn, r.logf)
	}
	res.Protected = len(protected)
	res.Total = len(candidates)

	if len(protected) == 0 {
		return res, safe
	}
	// The protected lane, merged back. Judged candidates lead, in their new
	// order; the protected ones follow in the deterministic order they
	// already had. They are not interleaved by score because there is no
	// score to interleave on: a probability and a bm25 value are the units
	// error this whole path exists to avoid, and inventing a relevance for a
	// candidate nothing was allowed to look at would be worse than either.
	sortByScore(protected)
	merged := make([]Slice, 0, len(safe)+len(protected))
	merged = append(merged, safe...)
	return res, append(merged, protected...)
}

// partitionForEgress splits candidates into those that may be described to a
// reranker and those that may not.
//
// Order is preserved within each side, so the split is deterministic and two
// runs over one packet produce the same two lanes. The predicate is the same
// one Rerank enforces again on its own input: this decides what is offered,
// that refuses what should never have been.
func partitionForEgress(candidates []Slice) (safe, protected []Slice) {
	for _, s := range candidates {
		if externalizable(s) {
			safe = append(safe, s)
			continue
		}
		protected = append(protected, s)
	}
	return safe, protected
}

// distinctPaths lists the files a candidate set covers, sorted so two runs are
// comparable.
func distinctPaths(slices []Slice) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(slices))
	for _, s := range slices {
		if s.Path == "" || seen[s.Path] {
			continue
		}
		seen[s.Path] = true
		out = append(out, s.Path)
	}
	sort.Strings(out)
	return out
}

// floorFraction and relevanceFloor read the active tuning, falling back to
// the shipped defaults. They are methods rather than constants so the eval
// harness can move them on the dev set without a rebuild.
func (r *Retriever) floorFraction() float64 {
	if r.tuning.GraphFloor > 0 {
		return r.tuning.GraphFloor
	}
	return DefaultGraphFloorFraction
}

func (r *Retriever) relevanceFloor() float64 {
	if r.tuning.RelevanceFloor > 0 {
		return r.tuning.RelevanceFloor
	}
	return DefaultRelevanceFloor
}

// fill places slices into the packet until the budget is spent.
//
// Two modes, matching sortByScore. Unjudged, expansion competes for a bounded
// share: without the cap it takes whatever the anchors left, which was most of
// the packet on most steps. Judged, the cap becomes a reservation — expansion
// is ranked on the same scale as everything else and no longer needs fencing
// off, but a floor keeps a run of confident anchors from crowding it out.
//
// Both modes drop rather than trim, and both count what they dropped: §8.3
// makes retrieval misses a primary metric, and a packet that quietly shrank is
// a miss nobody can see.
func (r *Retriever) fill(pkt *Packet, kept []Slice, budget int, judged bool) {
	seen := map[int64]bool{}
	admit := func(s Slice) (int, bool) {
		// Eligibility was decided in Build, ahead of every external call, and
		// each drop was counted there once. What is left here is deduplication.
		if s.NodeID != 0 {
			if seen[s.NodeID] {
				return 0, false
			}
			seen[s.NodeID] = true
		}
		return s.TokenEstimate(), true
	}
	place := func(s Slice, cost int) {
		pkt.Slices = append(pkt.Slices, s)
		pkt.Tokens += cost
	}

	if !judged {
		graphBudget := int(float64(budget) * GraphBudgetFraction)
		graphTokens := 0
		for _, s := range kept {
			cost, ok := admit(s)
			if !ok {
				continue
			}
			if s.Origin == OriginGraph && graphTokens+cost > graphBudget {
				pkt.Dropped++
				continue
			}
			if s.Origin != OriginImpact && pkt.Tokens+cost > budget {
				pkt.Dropped++
				continue
			}
			if s.Origin == OriginGraph {
				graphTokens += cost
			}
			place(s, cost)
		}
		return
	}

	// Pass 1: the reserved expansion slots, best first. kept is already in
	// relevance order, so the first graph slices it holds are the ones worth
	// reserving for. A slice below the floor does not consume the reservation
	// — it competes for the general budget in pass 2 like anything else.
	reserved := int(float64(budget) * r.floorFraction())
	spent := 0
	placed := map[int]bool{}
	for i, s := range kept {
		if s.Origin != OriginGraph || s.Relevance < r.relevanceFloor() {
			continue
		}
		cost, ok := admit(s)
		if !ok {
			continue
		}
		if spent+cost > reserved || pkt.Tokens+cost > budget {
			// The reservation is full. Stop reserving rather than skipping to
			// a smaller slice that fits: the point of the floor is to protect
			// the *best* expansion from being crowded out, and filling the
			// last of it with whatever happens to be small would spend it on
			// the ones least worth protecting. Undo admit's claim on the node
			// id so pass 2 can weigh this slice against everything else.
			delete(seen, s.NodeID)
			break
		}
		place(s, cost)
		placed[i] = true
		spent += cost
	}

	// Pass 2: everything else, strictly by relevance, no per-origin cap.
	for i, s := range kept {
		if placed[i] {
			continue
		}
		cost, ok := admit(s)
		if !ok {
			continue
		}
		if s.Origin != OriginImpact && pkt.Tokens+cost > budget {
			pkt.Dropped++
			continue
		}
		place(s, cost)
	}
}

// anchors runs the FTS5 stage. bm25() is SQLite's built-in ranking function;
// lower is better, so it is negated into a score.
func (r *Retriever) anchors(ctx context.Context, query string, limit int) ([]Slice, error) {
	match := ftsQuery(query)
	if match == "" {
		return nil, nil
	}
	rows, err := r.st.Index().SQL().QueryContext(ctx, `
		SELECT c.chunk_id, c.repository_id, f.path, c.start_line, c.end_line,
		       c.content_hash, c.index_version, COALESCE(f.worktree_id,''),
		       snippet(chunks_fts, 0, '', '', ' … ', 24) AS body,
		       -bm25(chunks_fts) AS score
		FROM chunks_fts
		JOIN chunks c ON c.chunk_id = chunks_fts.rowid
		JOIN files f ON f.file_id = c.file_id
		WHERE chunks_fts MATCH ?
		ORDER BY score DESC
		LIMIT ?`, match, limit)
	if err != nil {
		return nil, fmt.Errorf("retrieval: lexical anchors: %w", err)
	}
	defer rows.Close()

	var out []Slice
	for rows.Next() {
		var s Slice
		var chunkID int64
		if err := rows.Scan(&chunkID, &s.RepositoryID, &s.Path, &s.StartLine, &s.EndLine,
			&s.ContentHash, &s.IndexVersion, &s.WorktreeID, &s.Body, &s.Score); err != nil {
			return nil, err
		}
		s.WorkspaceID = r.st.ID()
		s.Origin = OriginAnchor
		s.Symbol = s.Path
		// f.path is a real repository-relative path, not the FQN substitute
		// sliceFromNode falls back to, so the egress gate may evaluate it.
		// Leaving this false made every lexically retrieved packet contain a
		// candidate the judged reranker had to refuse, which skipped the
		// whole rerank with ineligible_candidate — silently, because a skip
		// is the designed behaviour when a candidate really is ineligible.
		s.PathKnown = true
		out = append(out, s)
	}
	return out, rows.Err()
}

// ftsQuery turns free text into an FTS5 MATCH expression. Every term is
// quoted, so user input can never be read as FTS5 syntax.
func ftsQuery(q string) string {
	// Split on anything that is not an identifier character. Punctuation is a
	// separator rather than syntax, so no input can reach FTS5 as an operator.
	isTermRune := func(r rune) bool {
		return r == '_' || r == '.' || r == '/' || r == '-' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
	}
	fields := strings.FieldsFunc(q, func(r rune) bool { return !isTermRune(r) })
	terms := make([]string, 0, len(fields))
	for _, f := range fields {
		if len(f) < 2 {
			continue
		}
		terms = append(terms, `"`+strings.ReplaceAll(f, `"`, `""`)+`"`)
	}
	return strings.Join(terms, " OR ")
}

// resolveSymbol resolves a symbol reference the way plan validation does.
func (r *Retriever) resolveSymbol(ctx context.Context, ref string, limit int) ([]graph.Node, error) {
	return graph.Resolve(ctx, r.g, ref, limit)
}

func (r *Retriever) impact(ctx context.Context, symbols []string, kind graph.ChangeKind) (*graph.Impact, error) {
	if kind == "" {
		kind = graph.ChangeBehaviour
	}
	var ids []int64
	for _, sym := range symbols {
		nodes, err := r.resolveSymbol(ctx, sym, 10)
		if err != nil {
			return nil, err
		}
		for _, n := range nodes {
			ids = append(ids, n.ID)
		}
	}
	imp, err := r.g.ImpactOf(ctx, ids, kind)
	if err != nil {
		return nil, err
	}
	imp = graph.PublicImpact(imp)
	return &imp, nil
}

func (r *Retriever) sliceFromNode(n graph.Node, origin Origin, score float64, depth int,
	via graph.EdgeKind, ev graph.Evidence) Slice {
	path, known := n.Path, true
	if path == "" {
		// No file association. The FQN keeps the slice identifiable; see
		// Slice.PathKnown for why that makes it ineligible for egress.
		path, known = n.FQN, false
	}
	hash := n.ContentHash
	if hash == "" {
		// A node without its own content hash still needs provenance; the FQN
		// digest keeps Validate honest rather than inventing a file hash.
		hash = "fqn:" + n.FQN
	}
	return Slice{
		WorkspaceID: n.WorkspaceID, RepositoryID: n.RepositoryID, WorktreeID: n.WorktreeID,
		Path: path, PathKnown: known, Symbol: n.Name, ContentHash: hash,
		IndexVersion: version.IndexerVersion,
		NodeID:       n.ID, Kind: n.Kind, StartLine: n.StartLine, EndLine: n.EndLine,
		Signature: n.Signature, Origin: origin, Score: score, Depth: depth, Via: via, Evidence: ev,
	}
}

func nodeIDs(slices []Slice) []int64 {
	seen := map[int64]bool{}
	var out []int64
	for _, s := range slices {
		if s.NodeID != 0 && !seen[s.NodeID] {
			seen[s.NodeID] = true
			out = append(out, s.NodeID)
		}
	}
	return out
}

// CountRows is a maintenance helper used by the isolation tests to assert that
// no table in a workspace database holds a row stamped with another workspace.
func (r *Retriever) CountForeignRows(ctx context.Context) (int, error) {
	total := 0
	err := r.st.Index().ReadTx(ctx, func(tx *sql.Tx) error {
		for _, table := range []string{"repositories", "files", "nodes", "edges", "chunks"} {
			var n int
			if err := tx.QueryRowContext(ctx,
				`SELECT count(*) FROM `+table+` WHERE workspace_id <> ?`, r.st.ID().String()).Scan(&n); err != nil {
				return fmt.Errorf("retrieval: scan %s: %w", table, err)
			}
			total += n
		}
		return nil
	})
	return total, err
}

// addNotes fills the packet's memory section within its own sub-budget.
//
// Kinds are kept in the order memory.Kinds gives, which is the order a reader
// should see them: intent explains why, observation records what was seen,
// advice tries to steer. Mixing them would let a one-off read as a rule, which
// is the drift the three kinds exist to prevent.
func (r *Retriever) addNotes(ctx context.Context, objective string, pkt *Packet, budget int) {
	if r.mem == nil || budget <= 0 {
		return
	}
	all, err := r.mem.All()
	if err != nil {
		// A repository with no notes is the common case and not a failure. A
		// malformed one should not take the task down either: the packet is
		// still usable without them, and `bcode memory list` is where a broken
		// file gets reported.
		return
	}

	// Note-relevance screening (design.md mechanism M8, second sub-site):
	// which notes are even considered for the budget below. At TierLogged,
	// the default, every note is still considered exactly as before this
	// existed; irrelevant is computed but unused.
	irrelevant := map[string]bool{}
	if rel := r.noteRelevance(ctx, objective, all); rel.Tier.Permits(judgment.TierOrdering) {
		for _, score := range rel.Scores {
			if score.Relevant < DefaultNoteRelevanceFloor {
				irrelevant[score.ID] = true
			}
		}
	}

	spent := 0
	for _, kind := range memory.Kinds() {
		notes := all[kind]
		// Newest first: the caps drop the oldest, so the newest are the ones a
		// reader has not already seen play out.
		for i := len(notes) - 1; i >= 0; i-- {
			if irrelevant[notes[i].ID] {
				continue
			}
			cost := noteTokens(notes[i])
			if spent+cost > budget {
				pkt.NotesDropped++
				continue
			}
			pkt.Notes = append(pkt.Notes, notes[i])
			spent += cost
			pkt.Tokens += cost
		}
	}
}

// noteTokens estimates a note the same way a slice is estimated, counting the
// noteRelevance flattens every kind's notes and judges them against
// objective in one pass. A retriever with no judge (r.judge is judgment.Off()
// when unconfigured) still returns a well-formed, unattempted result — see
// CheckNoteRelevance.
func (r *Retriever) noteRelevance(ctx context.Context, objective string, all map[memory.Kind][]memory.Note) NoteRelevanceResult {
	var flat []memory.Note
	for _, kind := range memory.Kinds() {
		flat = append(flat, all[kind]...)
	}
	jctx, jobs := judgment.Begin(ctx, NoteRelevanceSite)
	res := CheckNoteRelevance(jctx, r.judge, objective, flat, NoteRelevanceTuning{}, r.logf)
	// An ordering site produces no findings: its output is a ranking, so the
	// honest count is zero and what matters is how many notes it judged.
	jobs.Subjects(res.Total)
	jobs.Skip(res.SkipReason)
	jobs.Finish(ctx, 0)
	return res
}

// provenance because that is rendered too — a rule whose source is invisible
// cannot be judged (§11).
func noteTokens(n memory.Note) int {
	return (len(n.Text)+len(n.Provenance.Source)+len(n.Kind))/4 + 8
}

// sortByScore orders slices best-first.
//
// Two modes, and which one applies is decided by the evidence rather than by
// configuration.
//
// Unjudged, every slice carries a score in the units of whatever found it, so
// the sort stays inside each origin and leaves the origins in place. Anchors
// stay ahead of expansion because a lexical hit on the query is a stronger
// signal than being adjacent to one, and the two scores are not on a common
// scale.
//
// Judged, every slice carries a probability from the same question, which is
// the same quantity whatever found it. So the origins are sorted together —
// the comparison the paragraph above says is impossible, made possible by
// having asked one question instead of reading two scores.
//
// A partially judged set is treated as unjudged. There is no third mode that
// mixes a probability with a bm25 value, because that is exactly the units
// error the unjudged branch exists to avoid.
func sortByScore(slices []Slice) {
	if judgedPrefix(slices) {
		judged := 0
		for _, s := range slices {
			if !s.Judged {
				break
			}
			judged++
		}
		sort.SliceStable(slices[:judged], func(i, j int) bool {
			a, b := slices[i], slices[j]
			if a.Relevance != b.Relevance {
				return a.Relevance > b.Relevance
			}
			// A tie goes to the anchor: it was found by matching the query
			// rather than by being near something that did.
			return a.Origin == OriginAnchor && b.Origin != OriginAnchor
		})
		return
	}
	sort.SliceStable(slices, func(i, j int) bool {
		if slices[i].Origin != slices[j].Origin {
			return false // keep the existing grouping
		}
		return slices[i].Score > slices[j].Score
	})
}

// anchorSeeds resolves lexical anchors to the declarations that contain them,
// so graph expansion has somewhere to start.
//
// A lexical anchor is a chunk of a file. It carries a path and a line range
// and no node id, so nodeIDs skipped every one of them and, for an objective
// that names no symbol, the expansion had no seeds at all — the code graph
// was built, indexed and never consulted. That is most objectives: a user
// writes "Prefetch objects don't work with slices", not "QuerySet#_prefetch".
//
// The consequence is not a worse ranking, it is a missing relationship. The
// file the change belongs in is often not the file whose text matches; it is
// one call away, and one call away is exactly what the graph is for.
//
// Deterministic: a declaration seeds expansion when it encloses the chunk's
// lines in the chunk's own file. Bounded: one query per distinct anchor
// file, capped, results sorted so two runs seed identically.
// unknownExtent is the width given to a declaration whose analyzer recorded
// no end line. It sorts after every measured declaration and never counts as
// an enclosing container, because nothing is known about what it encloses.
const unknownExtent = 1 << 30

func (r *Retriever) anchorSeeds(ctx context.Context, candidates []Slice) []int64 {
	const maxAnchorFiles = 12
	// A chunk may span several declarations; seeding all of them turns one
	// wide chunk into a traversal of everything it touched.
	const maxSeedsPerChunk = 4
	byFile := map[string][]Slice{}
	var order []string
	for _, s := range candidates {
		if s.NodeID != 0 || s.Origin != OriginAnchor || !s.PathKnown || s.Path == "" {
			continue
		}
		if _, seen := byFile[s.Path]; !seen {
			if len(order) >= maxAnchorFiles {
				continue
			}
			order = append(order, s.Path)
		}
		byFile[s.Path] = append(byFile[s.Path], s)
	}

	seen := map[int64]bool{}
	var out []int64
	for _, file := range order {
		nodes, err := r.g.NodesInFile(ctx, file, 500)
		if err != nil {
			continue
		}
		for _, chunk := range byFile[file] {
			// Every declaration whose lines overlap the chunk, not the one
			// that contains it: a chunk is a window on a file and is usually
			// wider than a declaration, so containment matched nothing at
			// all on a small file — the whole file is one chunk starting at
			// line 1 and no function starts there.
			type span struct {
				id    int64
				width int
			}
			var hits []span
			for _, n := range nodes {
				if n.ID == 0 || n.StartLine <= 0 {
					continue
				}
				// Not every analyzer records where a declaration ends — the
				// Go one stores start only, so an end-line of zero means
				// "unknown extent", not "ends before it begins". Treating it
				// as the latter skipped every Go declaration and the seeds
				// came out empty.
				end, width := n.EndLine, 0
				if end < n.StartLine {
					end, width = n.StartLine, unknownExtent
				} else {
					width = end - n.StartLine
				}
				if end < chunk.StartLine || n.StartLine > chunk.EndLine {
					continue
				}
				hits = append(hits, span{n.ID, width})
			}
			// Narrowest first, so a chunk covering a whole file seeds its
			// declarations rather than the node that wraps all of them.
			// Declarations of unknown extent sort last, after every
			// declaration whose range is actually recorded.
			sort.Slice(hits, func(i, j int) bool {
				if hits[i].width != hits[j].width {
					return hits[i].width < hits[j].width
				}
				return hits[i].id < hits[j].id
			})
			take := func(id int64) {
				if !seen[id] {
					seen[id] = true
					out = append(out, id)
				}
			}
			for i, h := range hits {
				if i >= maxSeedsPerChunk {
					break
				}
				take(h.id)
			}
			// …and the declaration that encloses them all.
			//
			// Narrowest-first is right for "what did the text match", and
			// wrong for "who uses this code". Another file does not refer
			// to a private method buried in a class; it refers to the
			// class. So the widest enclosing declaration is the one
			// carrying the incoming edges, and cutting the list at four
			// narrow hits discarded it every time — on Django the matched
			// chunks sat inside a class spanning most of the file, and the
			// consumers of that class were unreachable while its methods'
			// consumers (tests) came back instead.
			//
			// Declarations of unknown extent are not containers, they are
			// unmeasured, so they are not eligible.
			for i := len(hits) - 1; i >= 0; i-- {
				if hits[i].width < unknownExtent {
					take(hits[i].id)
					break
				}
			}
		}
	}
	return out
}

// consumerEdgeKinds are the relationships worth following backwards: who
// calls this, who refers to it, who implements it.
var consumerEdgeKinds = []graph.EdgeKind{graph.EdgeCalls, graph.EdgeReferences, graph.EdgeImplements}

// Bounds on the reverse hop. One hop, a handful of seeds, a handful of
// results: enough to name the other side of a relationship, not enough to
// become a second retrieval.
const (
	maxConsumerSeeds   = 16
	maxConsumerResults = 12
	// maxConsumersPerSeed bounds how many consumers of one seed are looked
	// at, not how many are kept. The selection below is what keeps the
	// result small; truncating here instead meant a widely used class
	// offered only the four consumers with the lowest node ids, and the
	// choice of which places a change has to account for was being made by
	// the order the indexer walked the repository in.
	maxConsumersPerSeed = 48
	// maxConsumersGathered caps the total work across all seeds, so a
	// repository with sixteen widely used seeds cannot turn one hop into
	// hundreds of node lookups.
	maxConsumersGathered = 240
)

// consumerHop follows one hop *backwards* from the seeds: the declarations
// that use what the query matched.
//
// Expansion has only ever gone forwards — from a seed to what it depends on.
// That answers "what does this code use" and never "who uses this code", and
// the second question is the one a change usually turns on. The forensic
// trace that forced this: an objective matched text in Django's query.py,
// and the file the change belonged in referenced QuerySet rather than being
// referenced by it, so a depth-1 relationship sat in the graph and no
// traversal could see it. Forward-only expansion cannot reach a caller.
//
// Bounded in four ways — edge kinds, one hop, seed count, result count — and
// ordered deterministically, so two runs over one repository expand the
// same way.
func (r *Retriever) consumerHop(ctx context.Context, seeds []int64) []Slice {
	if len(seeds) == 0 {
		return nil
	}
	if len(seeds) > maxConsumerSeeds {
		// The order seeds arrive in is already deterministic and already
		// meaningful: anchors come in bm25 rank and their declarations
		// follow. Sorting by node id before truncating threw that away and
		// kept whichever declarations happened to be indexed first, which
		// is a property of the indexer's walk and of nothing else.
		seeds = seeds[:maxConsumerSeeds]
	}
	seen := map[int64]bool{}
	for _, id := range seeds {
		seen[id] = true
	}
	// Gather first, select second. Taking the first four consumers of each
	// seed in node-id order spent the whole result budget on two test files:
	// a widely used class has hundreds of consumers, and which four come
	// back that way is decided by the order the indexer happened to walk the
	// repository in, which is a fact about the indexer and not about the
	// code.
	var gathered []Slice
	for _, id := range seeds {
		edges, err := r.g.Neighbors(ctx, id, graph.Reverse, consumerEdgeKinds)
		if err != nil {
			continue
		}
		sort.Slice(edges, func(i, j int) bool { return edges[i].Src < edges[j].Src })
		added := 0
		for _, e := range edges {
			if added >= maxConsumersPerSeed || len(gathered) >= maxConsumersGathered {
				break
			}
			if seen[e.Src] {
				continue
			}
			seen[e.Src] = true
			node, err := r.g.Node(ctx, e.Src)
			if err != nil {
				continue
			}
			// A consumer is worth naming only if it is somewhere a person
			// could look. A directory that happens to reference something is
			// not an edit surface.
			if node.Kind == graph.KindDirectory || node.Path == "" {
				continue
			}
			gathered = append(gathered, r.sliceFromNode(node, OriginGraph, 0.4, 1, e.Kind, e.Evidence))
			added++
		}
		if len(gathered) >= maxConsumersGathered {
			break
		}
	}
	return spreadByFile(gathered, maxConsumerResults)
}

// spreadByFile picks up to limit slices, one file at a time in round-robin,
// so no single file can fill the result set.
//
// The question the reverse hop asks is "who uses this", and the answer that
// helps is a set of distinct places, not the first dozen declarations of
// whichever file has the most of them. Order within a file is preserved, and
// files are visited in the order they were first gathered, so the result is
// deterministic.
func spreadByFile(slices []Slice, limit int) []Slice {
	if len(slices) <= limit {
		return slices
	}
	byFile := map[string][]Slice{}
	var order []string
	for _, s := range slices {
		if _, seen := byFile[s.Path]; !seen {
			order = append(order, s.Path)
		}
		byFile[s.Path] = append(byFile[s.Path], s)
	}
	out := make([]Slice, 0, limit)
	for len(out) < limit {
		progressed := false
		for _, file := range order {
			if len(out) >= limit {
				break
			}
			rest := byFile[file]
			if len(rest) == 0 {
				continue
			}
			out = append(out, rest[0])
			byFile[file] = rest[1:]
			progressed = true
		}
		if !progressed {
			break
		}
	}
	return out
}
