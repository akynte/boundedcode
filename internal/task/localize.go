package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/akynte/boundedcode/internal/firewall"
	"github.com/akynte/boundedcode/internal/graph"
	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/retrieval"
	"github.com/akynte/boundedcode/internal/telemetry"
	"github.com/akynte/boundedcode/internal/workflow"
	"github.com/akynte/boundedcode/internal/worktree"
)

var localizationSchema = json.RawMessage(`{"type":"object","properties":{"files":{"type":"array","items":{"type":"string","description":"a repository-relative file path and nothing else"}},"symbols":{"type":"array","items":{"type":"string","description":"one declaration name exactly as the code declares it: FunctionName, TypeName or TypeName.MethodName. No file path, no line number, no parentheses, no explanation"}},"hypothesis":{"type":"string"}},"required":["files","symbols","hypothesis"],"additionalProperties":false}`)

// localize separates structure selection, symbol selection and confirmation.
// Source bodies enter only the last call, after deterministic read checks.
func (r *Runner) localize(ctx context.Context, t *Task, wt *worktree.Worktree, s *workflow.State) error {
	access := firewall.Access{Protected: r.Policies}
	var paths []string
	err := filepath.WalkDir(wt.Path, func(full string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(wt.Path, full)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".bc", "node_modules", "vendor", "dist", "target":
				return filepath.SkipDir
			}
			return nil
		}
		// A refusal from the firewall means the path is not part of the
		// structure the model may see. Skipping it is the outcome, not an
		// error to propagate: one protected file must not end localization.
		//nolint:nilerr // the refusal is the reason to skip, not to fail
		if entry.Type()&os.ModeSymlink != 0 || access.Check(wt.Path, rel, false) != nil {
			return nil
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return err
	}
	type selection struct {
		Files      []string `json:"files"`
		Symbols    []string `json:"symbols"`
		Hypothesis string   `json:"hypothesis"`
	}
	// §6.3's structure pass takes the repository skeleton and the files a
	// lexical search already liked, not the whole tree. Thousands of paths is
	// thousands of tokens spent proving that most of a repository is
	// irrelevant, which the model then has to read past.
	//
	// It used to refuse outright above four thousand. That made repository
	// size a reason a task could not start: Django has 5,225 indexed paths and
	// every arm stopped here, having learned nothing about Django. A skeleton
	// too large for the budget is a presentation problem, so it is presented
	// differently — ranked deterministically, cut to what fits, and the cut
	// disclosed — rather than being a failure.
	hits := r.lexicalHits(ctx, wt.Path, t.Title)
	evidence := map[string]any{}
	if len(hits.Files) > 0 {
		evidence["lexical_hits"] = hits
	}
	hitPaths := make([]string, 0, len(hits.Files))
	for _, f := range hits.Files {
		hitPaths = append(hitPaths, f.Path)
	}
	sk := fitSkeleton(paths, hitPaths, evidence, r.evidenceFits(ctx, t, s))
	evidence["files"] = sk.Files
	evidence["structure"] = sk.Summary
	if r.Telemetry != nil {
		_ = r.Telemetry.Event(ctx, t.ID, "localize_structure", "localize", 0, sk.Summary.Retained,
			telemetry.Attrs{"available": sk.Summary.Available,
				"retained": sk.Summary.Retained, "dropped": sk.Summary.Dropped})
	}
	if sk.Summary.Dropped > 0 {
		r.logf("task %s: LOCALIZE skeleton %d of %d path(s), %d dropped for the phase evidence budget",
			t.ID, sk.Summary.Retained, sk.Summary.Available, sk.Summary.Dropped)
	}

	var chosen selection
	// The ranked repository map is P2 of the frozen prefix and is already in
	// front of the model. Repeating it here would spend the structure pass's
	// whole log budget restating what the prefix says.
	if err := r.decide(ctx, t, s, "Select relevant files from repository structure. Return a preliminary hypothesis and symbol names if known.", evidence, localizationSchema, &chosen); err != nil {
		return err
	}
	packet, err := r.Retriever.Build(ctx, retrieval.Request{Root: wt.Path,
		Symbols: chosen.Symbols, TokenBudget: 6000, Objective: t.Title})
	if err != nil {
		return err
	}
	r.recordPacket(packet)
	for i := range packet.Slices {
		packet.Slices[i].Body = ""
	}
	skeleton, err := r.Retriever.Skeleton(ctx, chosen.Files)
	if err != nil {
		return err
	}
	// The signature pass has the same problem the structure pass had, and it
	// only appeared once Python produced symbols: a file like
	// src/_pytest/python.py has thousands of declarations, so the skeleton of
	// a dozen chosen files is far larger than an evidence block. It is cut
	// the same way — deterministic order, drop from the tail, say how much
	// was dropped — rather than failing the phase.
	signatures, droppedSigs := fitSignatures(skeleton, map[string]any{"selection": chosen, "symbols": packet},
		r.evidenceFits(ctx, t, s))
	sigEvidence := map[string]any{"selection": chosen, "skeleton": signatures, "symbols": packet}
	if droppedSigs > 0 {
		sigEvidence["skeleton_truncated"] = map[string]int{
			"available": len(skeleton), "retained": len(signatures), "dropped_for_budget": droppedSigs,
		}
		r.logf("task %s: LOCALIZE signatures %d of %d, %d dropped for the phase evidence budget",
			t.ID, len(signatures), len(skeleton), droppedSigs)
	}
	if err := r.decide(ctx, t, s, "Refine the file and symbol selection using signatures. Do not claim confirmation yet.", sigEvidence, localizationSchema, &chosen); err != nil {
		return err
	}
	// The selection is the model's guess at paths, made before it has read
	// anything, and a plausible guess is often a file that does not exist —
	// the recorded case named restock_test.go beside restock.go. Stat-ing it
	// failed the whole task. A missing path is dropped and named in the
	// confirmation evidence, so the model hears about it; only a selection
	// with nothing real left in it is a failure.
	var missing []string
	chosen.Files, missing = existingFiles(wt.Path, chosen.Files)
	if len(missing) > 0 {
		r.logf("task %s: LOCALIZE dropped %d selected path(s) that do not exist: %s",
			t.ID, len(missing), strings.Join(missing, ", "))
	}
	if len(chosen.Files) == 0 || len(chosen.Files) > 12 {
		return fmt.Errorf("localization requires 1–12 existing files")
	}
	skeleton, err = r.Retriever.Skeleton(ctx, chosen.Files)
	if err != nil {
		return err
	}
	bodies := map[string]string{}
	total := 0
	for _, file := range chosen.Files {
		if err := access.Check(wt.Path, file, false); err != nil {
			return err
		}
		full, err := worktree.Resolve(wt.Path, file)
		if err != nil {
			return err
		}
		info, err := os.Stat(full)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 2<<20 {
			return fmt.Errorf("localization body budget exceeded for %s", file)
		}
		body, err := os.ReadFile(full)
		if err != nil {
			return err
		}
		selected := localizationBody(file, string(body), chosen.Symbols, skeleton,
			r.rankDeclarations(ctx, t.Title, file, skeleton), r.localizeTuning())
		if total+len(selected) > 36000 {
			return fmt.Errorf("localization evidence exceeds 12K token budget; narrow the selected symbols")
		}
		bodies[file] = selected
		total += len(selected)
	}
	confirmEvidence := map[string]any{"selection": chosen, "bodies": bodies, "failures": s.Feedback}
	if len(missing) > 0 {
		confirmEvidence["missing_files"] = missing
	}
	if err := r.decide(ctx, t, s, "Confirm the root cause against source bodies. Identify the files and symbols the plan must address.", confirmEvidence, localizationSchema, &chosen); err != nil {
		return err
	}
	// The confirmation can reintroduce a guess the refinement dropped.
	chosen.Files, _ = existingFiles(wt.Path, chosen.Files)
	if strings.TrimSpace(chosen.Hypothesis) == "" || len(chosen.Files) == 0 || len(chosen.Symbols) == 0 {
		return fmt.Errorf("localization did not confirm a hypothesis, files and symbols")
	}
	s.Files, s.Symbols, s.Hypothesis = chosen.Files, chosen.Symbols, chosen.Hypothesis
	s.Bodies = bodies
	return nil
}

// existingFiles splits a selection into the paths present in the worktree and
// the ones that are not. Only "does not exist" drops a path: anything else —
// a path outside the worktree, a directory, an unreadable file — stays in for
// the access and body checks, which refuse it with their own reason.
func existingFiles(root string, files []string) (kept, missing []string) {
	for _, file := range files {
		if full, err := worktree.Resolve(root, file); err == nil {
			if _, err := os.Stat(full); errors.Is(err, fs.ErrNotExist) {
				missing = append(missing, file)
				continue
			}
		}
		kept = append(kept, file)
	}
	return kept, missing
}

// Line context around a selected declaration, and the fallback when none was
// selected.
//
// The context lines are for the reader: a signature is rarely comprehensible
// without the comment above it or the closing lines below. The fallback is a
// guess, and it is only right for a file small enough that the top of it is
// most of it.
const (
	bodyContextBefore = 4
	bodyContextAfter  = 3
)

// localizationBody picks the lines of one file worth showing.
//
// judged is the relevance of each declaration in this file, keyed by symbol,
// or nil when no judgment was available. Nil means the first-eighty-lines
// fallback stands — a wrong guess about where to look costs a confirmation
// pass, and a phase that failed because an advisory call did not answer would
// cost the task.
func localizationBody(file, body string, symbols []string, skeleton []retrieval.Slice,
	judged map[string]float64, t LocalizeTuning) string {

	t = t.withDefaults()
	floor, maxDecls, fallbackLines := t.DeclarationFloor, t.DeclarationsKept, t.FallbackBodyLines
	lines := strings.Split(body, "\n")
	selected := make([]bool, len(lines))
	take := func(decl retrieval.Slice) {
		start := max(0, decl.StartLine-bodyContextBefore)
		end := min(len(lines), decl.EndLine+bodyContextAfter)
		for i := start; i < end; i++ {
			selected[i] = true
		}
	}

	matched := false
	for _, decl := range skeleton {
		if decl.Path != file {
			continue
		}
		// A skeleton row carries the short name; the selection is written
		// qualified (`UserService.Email`, `path::Symbol`). Compared whole,
		// they never matched and every body fell back to the top of the
		// file. The file is already fixed by the loop, so the short name is
		// enough to find the declaration meant.
		wanted := false
		for _, name := range symbols {
			_, _, short := graph.SplitSymbol(name)
			wanted = wanted || name == decl.Symbol || short == decl.Symbol
		}
		if !wanted || decl.StartLine < 1 || decl.EndLine < decl.StartLine {
			continue
		}
		matched = true
		take(decl)
	}

	if !matched && len(judged) > 0 {
		// Nothing the selection named is in this file, but something in it was
		// worth retrieving. Show the declarations a judgment thought bore on
		// the objective rather than whatever happens to be at the top.
		var best []retrieval.Slice
		for _, decl := range skeleton {
			if decl.Path != file || decl.StartLine < 1 || decl.EndLine < decl.StartLine {
				continue
			}
			if judged[decl.Symbol] >= floor {
				best = append(best, decl)
			}
		}
		sort.SliceStable(best, func(i, j int) bool {
			return judged[best[i].Symbol] > judged[best[j].Symbol]
		})
		if len(best) > maxDecls {
			best = best[:maxDecls]
		}
		for _, decl := range best {
			matched = true
			take(decl)
		}
	}

	if !matched {
		for i := 0; i < min(fallbackLines, len(lines)); i++ {
			selected[i] = true
		}
	}
	var out strings.Builder
	gap := false
	for i, line := range lines {
		if !selected[i] {
			gap = true
			continue
		}
		if gap {
			out.WriteString("[unselected lines omitted]\n")
			gap = false
		}
		fmt.Fprintf(&out, "%d: %s\n", i+1, line)
	}
	if gap {
		out.WriteString("[unselected lines omitted]\n")
	}
	return out.String()
}

// LocalizeTuning is the knob set for the judged parts of localization.
//
// Defaults are the shipped behaviour; the eval harness overrides them per arm
// so a top-K can be fitted on the dev set without recompiling. Zero fields
// take the default.
type LocalizeTuning struct {
	// Candidates is how many lexical hits are considered at all.
	Candidates int
	// Kept and KeptJudged are how many files the structure pass is shown,
	// unjudged and judged.
	//
	// Twenty is what a match count can honestly support: the count says a
	// file mentions the terms, not that the work is there, so the list has to
	// be long enough that the right file is somewhere in it. A relevance
	// judgment answers the second question directly, so a shorter list may be
	// a better one — but that is the hypothesis, not a finding, which is why
	// the number is tunable rather than fixed.
	Kept, KeptJudged int
	// MaxDeclarations bounds the per-file declaration judgment.
	MaxDeclarations int
	// DeclarationsKept and DeclarationFloor govern which declarations replace
	// the first-eighty-lines fallback.
	DeclarationsKept  int
	DeclarationFloor  float64
	FallbackBodyLines int
}

// Shipped defaults for LocalizeTuning.
const (
	DefaultLexicalCandidates = 30
	DefaultLexicalKept       = 20
	DefaultLexicalKeptJudged = 8
	DefaultMaxDeclarations   = 60
	DefaultDeclarationsKept  = 6
	DefaultDeclarationFloor  = 0.5
	DefaultFallbackBodyLines = 80
)

func (t LocalizeTuning) withDefaults() LocalizeTuning {
	if t.Candidates <= 0 {
		t.Candidates = DefaultLexicalCandidates
	}
	if t.Kept <= 0 {
		t.Kept = DefaultLexicalKept
	}
	if t.KeptJudged <= 0 {
		t.KeptJudged = DefaultLexicalKeptJudged
	}
	if t.MaxDeclarations <= 0 {
		t.MaxDeclarations = DefaultMaxDeclarations
	}
	if t.DeclarationsKept <= 0 {
		t.DeclarationsKept = DefaultDeclarationsKept
	}
	if t.DeclarationFloor <= 0 {
		t.DeclarationFloor = DefaultDeclarationFloor
	}
	if t.FallbackBodyLines <= 0 {
		t.FallbackBodyLines = DefaultFallbackBodyLines
	}
	return t
}

func (r *Runner) localizeTuning() LocalizeTuning { return r.LocalizeTuning.withDefaults() }

// lexicalHits is §6.2's concept route: the task text expanded into the
// spellings source actually uses, searched live against the working tree.
//
// It is best-effort by design. ripgrep is the cheapest layer and the only one
// that sees uncommitted edits, but it is not required: a machine without it, or
// a search that outran its budget, falls back to the index and the repository
// map, which is a worse ranking rather than a failed phase. What is never done
// is reporting a truncated search as a complete one.
func (r *Runner) lexicalHits(ctx context.Context, root, query string) lexicalReport {
	var report lexicalReport
	seen := map[string]int{}
	for _, term := range retrieval.ExpandTerms(query) {
		res, err := retrieval.Grep(ctx, root, term, true)
		if err != nil {
			if errors.Is(err, retrieval.ErrRipgrepMissing) {
				return lexicalReport{}
			}
			r.logf("lexical search for %q: %v", term, err)
			continue
		}
		report.Truncated = report.Truncated || res.Truncated
		for _, hit := range res.Hits {
			seen[hit.Path]++
		}
	}
	for path, count := range seen {
		report.Files = append(report.Files, lexicalFile{Path: path, Matches: count})
	}
	// A file matching several of the expanded spellings is a better candidate
	// than one matching a single term many times, so the count of distinct
	// matching lines orders them and the path breaks ties deterministically.
	//
	// It is a proxy, and it is wrong in a predictable direction: the biggest
	// file wins, because a big file mentions more things. That is what the
	// count is kept for — generating candidates and breaking ties — rather
	// than what it is trusted for.
	sort.Slice(report.Files, func(i, j int) bool {
		if report.Files[i].Matches != report.Files[j].Matches {
			return report.Files[i].Matches > report.Files[j].Matches
		}
		return report.Files[i].Path < report.Files[j].Path
	})
	t := r.localizeTuning()
	if len(report.Files) > t.Candidates {
		report.Files = report.Files[:t.Candidates]
		report.Truncated = true
	}

	kept := t.Kept
	if r.rankFiles(ctx, query, &report) {
		kept = t.KeptJudged
	}
	if len(report.Files) > kept {
		report.Files = report.Files[:kept]
		// Still set, for the same reason as above: a shorter list must never
		// read as a complete one.
		report.Truncated = true
	}
	return report
}

// rankFiles reorders the candidates by judged relevance, reporting whether it
// did. One request, and it decides what the structure pass is shown — the
// cheapest judgment in the system by that measure.
func (r *Runner) rankFiles(ctx context.Context, objective string, report *lexicalReport) bool {
	if r.Judge == nil || !r.Judge.Available() || len(report.Files) == 0 || objective == "" {
		return false
	}
	// Every candidate must be disclosable or none is asked about: a partial
	// question set gives a partial ordering, and a partial ordering here is a
	// mixture of a probability and a match count.
	for _, f := range report.Files {
		if !judgment.Eligible(f.Path) {
			r.logf("rank files: a candidate path is egress-sensitive; keeping the match-count order")
			return false
		}
	}

	const collection = "candidates"
	st := judgment.NewState(judgment.RedactStrict)
	if err := st.Objective(objective); err != nil {
		r.logf("rank files: %v", err)
		return false
	}
	items := make([]*judgment.RepoItem, 0, len(report.Files))
	qs := make(map[string]judgment.Question, len(report.Files))
	for i, f := range report.Files {
		id := "f" + strconv.Itoa(i)
		item, err := st.NewRepoItem(id, f.Path)
		if err != nil {
			r.logf("rank files: %v", err)
			return false
		}
		item.Count("matching_lines", f.Matches)
		items = append(items, item)
		qs[id] = judgment.Noul(fileRelevanceProposition(collection, id))
	}
	if err := st.AddItems(collection, items); err != nil {
		r.logf("rank files: %v", err)
		return false
	}

	answers, note := judgment.AskAll(ctx, r.Judge, st, qs)
	if !note.Used() {
		r.logf("rank files: %s", note.Detail)
		return false
	}
	scored := make([]float64, len(report.Files))
	for i := range report.Files {
		a := answers["f"+strconv.Itoa(i)]
		if !a.Answered {
			// One unanswered candidate makes the ordering a mixture of a
			// probability and a match count, which is not an ordering. Nothing
			// has been written to the report yet, so returning here leaves it
			// exactly as the deterministic pass produced it.
			r.logf("rank files: an answer was missing; keeping the match-count order")
			return false
		}
		scored[i] = a.Noul
	}
	for i := range report.Files {
		report.Files[i].Relevance = scored[i]
	}
	sort.SliceStable(report.Files, func(i, j int) bool {
		if report.Files[i].Relevance != report.Files[j].Relevance {
			return report.Files[i].Relevance > report.Files[j].Relevance
		}
		return report.Files[i].Matches > report.Files[j].Matches
	})
	return true
}

// rankDeclarations scores the declarations of one file against the objective,
// for the case where the selection named no symbol in it. Nil on any failure,
// which restores the first-eighty-lines fallback.
func (r *Runner) rankDeclarations(ctx context.Context, objective, file string,
	skeleton []retrieval.Slice) map[string]float64 {

	if r.Judge == nil || !r.Judge.Available() || objective == "" {
		return nil
	}
	var decls []retrieval.Slice
	for _, d := range skeleton {
		if d.Path == file && d.Symbol != "" {
			decls = append(decls, d)
		}
	}
	t := r.localizeTuning()
	if len(decls) == 0 || len(decls) > t.MaxDeclarations {
		return nil
	}
	if !judgment.Eligible(file) {
		return nil
	}
	const collection = "declarations"
	st := judgment.NewState(judgment.RedactStrict)
	if err := st.Objective(objective); err != nil {
		return nil
	}
	if err := st.TrustedFact("file", file); err != nil {
		return nil
	}
	items := make([]*judgment.RepoItem, 0, len(decls))
	qs := make(map[string]judgment.Question, len(decls))
	for i, d := range decls {
		id := "d" + strconv.Itoa(i)
		item, err := st.NewRepoItem(id, d.Path)
		if err != nil {
			return nil
		}
		item.Meta("symbol", d.Symbol).Meta("kind", string(d.Kind)).Lines(d.StartLine, d.EndLine)
		items = append(items, item)
		qs[id] = judgment.Noul(declRelevanceProposition(collection, id))
	}
	if err := st.AddItems(collection, items); err != nil {
		return nil
	}
	answers, note := judgment.AskAll(ctx, r.Judge, st, qs)
	if !note.Used() {
		return nil
	}
	out := map[string]float64{}
	for i, d := range decls {
		if a := answers["d"+strconv.Itoa(i)]; a.Answered {
			out[d.Symbol] = a.Noul
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// declRelevanceProposition asks about one declaration in a file the selection
// did not name a symbol in.
func declRelevanceProposition(collection, id string) string {
	return "`file` was selected as relevant to `objective`, but nothing in the selection " +
		"said which part of it matters. Considering the declaration with id `" + id +
		"` in `" + collection + "`: would an engineer need to read it to carry out " +
		"`objective` correctly? " + retrieval.RelevanceRubric()
}

// fileRelevanceProposition is the file-level form of the relevance question.
// Like the retriever's, it names the candidate inside the proposition rather
// than in criteria or in the question's map key, and it judges by engineering
// necessity rather than by file category.
func fileRelevanceProposition(collection, id string) string {
	return "Considering the candidate with id `" + id + "` in `" + collection + "`: " +
		"would an engineer need to open that file to carry out `objective` correctly? " +
		"`matching_lines` is how many of its lines matched a keyword search for the " +
		"objective's terms, which says the file mentions them and not that the work is " +
		"there. " + retrieval.RelevanceRubric()
}

// lexicalReport is what the structure pass is told about the live search.
type lexicalReport struct {
	Files []lexicalFile `json:"files"`
	// Truncated says the search was cut short, so an absent file is not
	// evidence that nothing matched there.
	Truncated bool `json:"truncated,omitempty"`
}

type lexicalFile struct {
	Path    string `json:"path"`
	Matches int    `json:"matches"`
	// Relevance is set when a judgment ordered these rather than the match
	// count. It is reported so the structure pass, and anyone reading a trace,
	// can see which ordering they are looking at.
	Relevance float64 `json:"relevance,omitempty"`
}

// The repository skeleton, bounded by the budget that will carry it.
//
// The structure pass needs to know the shape of the repository, and the
// evidence block that carries it has a fixed size. Those two facts used to
// meet as a refusal above four thousand paths, which made "this repository is
// large" indistinguishable from "this task cannot be attempted". They meet
// here instead: the paths are ranked by a rule that does not consult a model,
// the list is cut where the budget ends, and what was cut is stated in the
// evidence so the model is not shown a partial tree it believes is whole.

// repoSkeleton is what the structure pass is shown about the repository.
type repoSkeleton struct {
	Files   []string
	Summary skeletonSummary
}

// skeletonSummary is the disclosure. A truncated list that does not say it is
// truncated is worse than a short one: the model reasons about absence.
type skeletonSummary struct {
	// Directories is the census — every populated directory and how many
	// files it holds. It survives truncation of the file list, so the shape
	// of the repository is preserved even when most of its paths are not.
	Directories []directoryCount `json:"directories,omitempty"`
	Available   int              `json:"paths_available"`
	Retained    int              `json:"paths_retained"`
	Dropped     int              `json:"paths_dropped_for_budget"`
	// DirectoriesTotal reports how many populated directories exist, so a
	// census that is itself abbreviated says so.
	DirectoriesTotal int `json:"directories_total,omitempty"`
}

type directoryCount struct {
	Dir   string `json:"dir"`
	Files int    `json:"files"`
}

// maxDirectoryCensus bounds the summary that survives truncation.
//
// It is a summary rather than a budget: a repository with more populated
// directories than this is described well enough by its largest ones, and the
// total is reported beside them so the abbreviation is visible.
const maxDirectoryCensus = 200

// fitSkeleton ranks the repository's paths and keeps as many as the phase
// budget allows, given whatever else is already in the evidence.
//
// other is the rest of the evidence map; it is marshalled as it stands so the
// budget spent on lexical hits is not spent twice. fits is the phase's own
// admission test — the byte ceiling decide enforces *and* the append-only
// log's token budget, which is the tighter of the two and the one a
// four-thousand-path skeleton actually hits. The loop asks it about the real
// serialization rather than estimating, because that is what will be
// admitted.
//
// A nil fits leaves the byte ceiling as the only bound, which is what the
// unit tests exercise.
func fitSkeleton(paths, hits []string, other map[string]any, fits func([]byte) bool) repoSkeleton {
	ranked := rankSkeleton(paths, hits)
	sum := skeletonSummary{Available: len(paths)}
	sum.Directories, sum.DirectoriesTotal = directoryCensus(paths)

	// A trial map rather than the caller's: nothing is written back until the
	// size is known to fit.
	trial := make(map[string]any, len(other)+2)
	for k, v := range other {
		trial[k] = v
	}
	kept := ranked
	for {
		sum.Retained, sum.Dropped = len(kept), len(paths)-len(kept)
		trial["files"] = kept
		trial["structure"] = sum
		body, err := json.Marshal(trial)
		if err == nil && len(body) <= phaseEvidenceLimit && (fits == nil || fits(body)) {
			break
		}
		if len(kept) == 0 {
			// Nothing left to drop. The remaining evidence is over budget on
			// its own, which is decide's refusal to make, not this function's.
			break
		}
		// An eighth at a time: a path is about forty bytes, so one-at-a-time
		// would marshal a hundred-kilobyte map thousands of times to reach
		// the same answer.
		drop := len(kept) / 8
		if drop < 1 {
			drop = 1
		}
		kept = kept[:len(kept)-drop]
	}
	return repoSkeleton{Files: kept, Summary: sum}
}

// rankSkeleton orders the repository's paths by how likely the structure pass
// is to need them, using only what the supervisor already computed.
//
// Three bands, and nothing in them is a judgment:
//
//  1. the files lexical search already liked, in its order — this is the one
//     signal about *this* objective, so it is never the part that gets cut;
//  2. their siblings, because a fix is usually near the thing that matched,
//     and a directory shown without its contents hides the alternatives;
//  3. everything else, shallowest first, because a repository's entry points,
//     manifests and top-level packages sit near the root and its generated
//     and vendored depths sit far from it.
//
// Ties break lexicographically so two runs over one tree rank identically.
func rankSkeleton(paths, hits []string) []string {
	hitSet := make(map[string]bool, len(hits))
	hitDirs := make(map[string]bool, len(hits))
	for _, h := range hits {
		h = filepath.ToSlash(h)
		hitSet[h] = true
		hitDirs[path.Dir(h)] = true
	}

	var band1, band2, band3 []string
	for _, p := range paths {
		switch {
		case hitSet[p]:
			// Kept in the lexical ranking's own order, below.
		case hitDirs[path.Dir(p)]:
			band2 = append(band2, p)
		default:
			band3 = append(band3, p)
		}
	}
	// Band 1 in lexical-hit order, restricted to paths that survived the walk.
	present := make(map[string]bool, len(paths))
	for _, p := range paths {
		present[p] = true
	}
	for _, h := range hits {
		if h = filepath.ToSlash(h); present[h] {
			band1 = append(band1, h)
		}
	}
	sort.Strings(band2)
	sort.Slice(band3, func(i, j int) bool {
		di, dj := strings.Count(band3[i], "/"), strings.Count(band3[j], "/")
		if di != dj {
			return di < dj
		}
		return band3[i] < band3[j]
	})
	out := make([]string, 0, len(paths))
	out = append(out, band1...)
	out = append(out, band2...)
	return append(out, band3...)
}

// directoryCensus counts files per populated directory and returns the
// largest, with the total so an abbreviated census says it is one.
func directoryCensus(paths []string) ([]directoryCount, int) {
	counts := map[string]int{}
	for _, p := range paths {
		counts[path.Dir(p)]++
	}
	out := make([]directoryCount, 0, len(counts))
	for dir, n := range counts {
		out = append(out, directoryCount{Dir: dir, Files: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Files != out[j].Files {
			return out[i].Files > out[j].Files
		}
		return out[i].Dir < out[j].Dir
	})
	total := len(out)
	if len(out) > maxDirectoryCensus {
		out = out[:maxDirectoryCensus]
	}
	return out, total
}

// fitSignatures cuts a file skeleton down to what the phase will admit.
//
// The order is the one Skeleton produced — file by file, declaration by
// declaration in source order — so two runs over one tree cut at the same
// place. Dropping from the tail keeps whole files at the head intact rather
// than thinning every file into uselessness.
func fitSignatures(sk []retrieval.Slice, other map[string]any, fits func([]byte) bool) ([]retrieval.Slice, int) {
	kept := sk
	for {
		trial := make(map[string]any, len(other)+1)
		for k, v := range other {
			trial[k] = v
		}
		trial["skeleton"] = kept
		body, err := json.Marshal(trial)
		if err == nil && len(body) <= phaseEvidenceLimit && (fits == nil || fits(body)) {
			return kept, len(sk) - len(kept)
		}
		if len(kept) == 0 {
			return kept, len(sk)
		}
		drop := len(kept) / 8
		if drop < 1 {
			drop = 1
		}
		kept = kept[:len(kept)-drop]
	}
}

// maxEditFileDeclarations bounds the declarations a plan's files contribute to
// EDIT's packet when its symbols name none; the packet budget trims further.
const maxEditFileDeclarations = 40

// editSymbols is what EDIT's packet is retrieved from: the plan's symbols, and
// the declarations of the files the plan writes when those name nothing.
//
// The plan's symbols are free text. Recorded plans left them empty or listed
// file paths in them, and EDIT then received a packet with no code — the same
// starvation qualified names caused, by another route. The files the plan
// declares it will write are the other statement of where the change is, so
// they stand in when the symbols resolve to nothing, and a file path listed as
// a symbol is read as that file.
func (r *Runner) editSymbols(ctx context.Context, plan workflow.Plan) []string {
	if r.Retriever == nil || r.Retriever.Graph() == nil {
		return plan.Symbols
	}
	var refs, files []string
	resolved := false
	for _, sym := range plan.Symbols {
		sym = strings.TrimSpace(sym)
		if isFileRef(sym, plan) {
			files = append(files, sym)
			continue
		}
		refs = append(refs, sym)
		if nodes, err := graph.Resolve(ctx, r.Retriever.Graph(), sym, 1); err == nil && len(nodes) > 0 {
			resolved = true
		}
	}
	if !resolved {
		files = append(files, plan.WriteAllowlist...)
	}
	if len(files) == 0 {
		return refs
	}
	seen := map[string]bool{}
	var unique []string
	for _, f := range files {
		if !seen[f] {
			seen[f] = true
			unique = append(unique, f)
		}
	}
	decls, err := r.Retriever.Skeleton(ctx, unique)
	if err != nil {
		return refs
	}
	for i, d := range decls {
		if i == maxEditFileDeclarations {
			break
		}
		if d.Symbol != "" {
			refs = append(refs, d.Path+"::"+d.Symbol)
		}
	}
	return refs
}

// isFileRef reports whether a plan symbol is really a file path.
func isFileRef(sym string, plan workflow.Plan) bool {
	if slices.Contains(plan.Files.Paths(), sym) {
		return true
	}
	return strings.Contains(sym, "/") && filepath.Ext(sym) != "" && !strings.ContainsAny(sym, " :()")
}
