package task

// A plan is not true because it parses.
//
// The schema says a plan has files, symbols and obligations; it cannot say
// that `src/_pytest/util.py` is a file in this repository, and it is not —
// the planner invented it, twice, along with `testing/util.py` and
// `django/db/models/sql/prefetch.py`. Each time the plan was accepted, the
// phase after it stat'd the path, failed, and the task ended having spent its
// whole budget on a repository it had hallucinated.
//
// So every concrete target is checked against the repository before the plan
// is accepted, and the check is deterministic: the filesystem, the index and
// the graph, never the model's own opinion of whether its target exists. When
// a target is wrong the planner is told exactly which one and what the
// repository actually contains near it — and then it writes the plan again.
// Nothing here rewrites a plan on the model's behalf: a supervisor that
// quietly substituted the nearest real path would be authoring the change.

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/graph"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/workflow"
	"github.com/akynte/boundedcode/internal/worktree"
)

// maxPlanRegenerations bounds how many times a plan may be sent back.
//
// Two corrections is enough for a planner that mistyped a path and not enough
// to be worth a third round of the same answer.
const maxPlanRegenerations = 2

// maxEvidence is how many real alternatives a contradiction carries.
const maxEvidence = 5

// TargetKind says what sort of thing a contradiction is about, so a reader
// can tell a missing file from a symbol nobody declared.
type TargetKind string

const (
	TargetFile   TargetKind = "file"
	TargetSymbol TargetKind = "symbol"
	TargetPreset TargetKind = "verification_preset"
)

// Contradiction is one target the repository does not contain.
type Contradiction struct {
	Kind   TargetKind
	Target string
	Reason string
	// Evidence is what the repository does contain nearby. It is offered to
	// the planner to correct itself with; it is never substituted for the
	// target, because a near match is not the same claim.
	Evidence []string
}

func (c Contradiction) String() string {
	s := fmt.Sprintf("The proposed %s `%s` %s.", c.Kind, c.Target, c.Reason)
	if len(c.Evidence) > 0 {
		s += "\n\nClosest repository evidence:\n- " + strings.Join(c.Evidence, "\n- ")
	}
	return s
}

// PlanTargets is the deterministic evidence plan validation reads.
type PlanTargets struct {
	Root    string
	Graph   graph.Graph
	Presets []recipe.Preset
}

// newMemberOfPlannedFile reports whether sym is Parent.Member where Parent
// resolves to a declaration in a file the plan writes.
func (p PlanTargets) newMemberOfPlannedFile(ctx context.Context, sym string, plan workflow.Plan) bool {
	if p.Graph == nil {
		return false
	}
	_, symbol, _ := graph.SplitSymbol(sym)
	i := strings.LastIndex(symbol, ".")
	if i <= 0 {
		return false
	}
	parents, err := graph.Resolve(ctx, p.Graph, symbol[:i], 5)
	if err != nil {
		return false
	}
	for _, n := range parents {
		if slices.Contains(plan.Files.Paths(), n.Path) {
			return true
		}
	}
	return false
}

// dirExists reports whether a repository-relative directory exists inside the
// worktree. The root itself always does.
func (p PlanTargets) dirExists(dir string) bool {
	if dir == "." || dir == "" {
		return true
	}
	full, err := worktree.Resolve(p.Root, dir)
	if err != nil {
		return false
	}
	info, err := os.Stat(full)
	return err == nil && info.IsDir()
}

// Validate reports every concrete target the repository does not contain.
//
// A file that does not exist is allowed only where the plan is plainly
// creating it: the path must be new *and* its directory must already exist,
// which is the difference between adding a module to a real package and
// inventing a package. A symbol must resolve in the graph. A preset must be
// one INTAKE froze.
func (p PlanTargets) Validate(ctx context.Context, plan workflow.Plan) (found []Contradiction, checked int) {
	seen := map[string]bool{}
	add := func(c Contradiction) {
		key := string(c.Kind) + "\x00" + c.Target
		if seen[key] {
			return
		}
		seen[key] = true
		found = append(found, c)
	}

	for _, f := range append(plan.Files.Paths(), plan.WriteAllowlist...) {
		checked++
		full, err := worktree.Resolve(p.Root, f)
		if err != nil {
			// The recorded case: a correct path with an explanation appended,
			// refused with no evidence at all because the whole string is not
			// a path. It still is not one — nothing here accepts or rewrites
			// it — but the correction can say which real path is inside it.
			evidence := []string(nil)
			if embedded := p.embeddedPath(f); embedded != "" {
				evidence = []string{"Did you mean: " + embedded}
			}
			add(Contradiction{TargetFile, f,
				"does not resolve inside the repository. The path field must contain only " +
					"the repository-relative path, with any rationale in the reason field",
				evidence})
			continue
		}
		if _, err := os.Stat(full); err == nil {
			continue
		}
		// Before the new-file allowance: a string that *contains* a real
		// repository path is a malformed target, not a file being created.
		// Checking the directory first accepted
		// "src/_pytest/compat.py (num_mock_patch_args, lines 62-73: …)"
		// outright, because path.Dir of it is src/_pytest, which exists.
		if embedded := p.embeddedPath(f); embedded != "" {
			add(Contradiction{TargetFile, f,
				"is not a repository path. The path field must contain only the " +
					"repository-relative path, with any rationale in the reason field",
				[]string{"Did you mean: " + embedded}})
			continue
		}
		// A new file is legitimate; a new file in a directory that is also
		// invented is the failure this catches.
		if p.dirExists(path.Dir(f)) {
			continue
		}
		add(Contradiction{TargetFile, f,
			"does not exist, and neither does the directory it would be created in",
			p.filesNear(f)})
	}

	// Symbols are refused only where the graph can be believed about them.
	//
	// A repository with no semantic index — a language this build has no
	// analyzer for, or an index that has not been built — resolves nothing,
	// and refusing every symbol on that basis would be reading an empty
	// table as a statement about the code. So the symbols are resolved
	// first, and refusals are issued only if at least one of them resolved:
	// that is the difference between "the graph does not know this symbol"
	// and "the graph does not know anything".
	var unresolved []string
	resolvedAny := false
	for _, sym := range plan.Symbols {
		checked++
		if sym = strings.TrimSpace(sym); sym == "" {
			continue
		}
		if p.symbolResolves(ctx, sym) {
			resolvedAny = true
			continue
		}
		unresolved = append(unresolved, sym)
	}
	if resolvedAny {
		for _, sym := range unresolved {
			// A declaration the plan adds, named with the file the plan says
			// it writes — `internal/service/user_test.go:TestEmailMissingUser`
			// — does not exist yet and is not an invention. Refusing it spent
			// both corrections of a plan whose only fault was naming the
			// regression test it was about to write. An unqualified name
			// that resolves nowhere is still refused.
			if file, _, _ := graph.SplitSymbol(sym); file != "" && slices.Contains(plan.Files.Paths(), file) {
				continue
			}
			// Likewise a new member of a declaration the plan writes:
			// `domain.Order.ReservationReleasedAt` is a field the plan adds
			// to an Order it edits — which was the recorded fix — not an
			// invention, when Order itself resolves into a planned file.
			if p.newMemberOfPlannedFile(ctx, sym, plan) {
				continue
			}
			add(Contradiction{TargetSymbol, sym, "is not a symbol this repository declares",
				p.symbolAlternatives(ctx, sym, plan)})
		}
	}

	for _, ob := range plan.Obligations {
		checked++
		if ob.Path == "" {
			continue
		}
		full, err := worktree.Resolve(p.Root, ob.Path)
		if err != nil {
			add(Contradiction{TargetFile, ob.Path, "does not resolve inside the repository", nil})
			continue
		}
		if _, err := os.Stat(full); err != nil {
			// A file the plan itself creates is the same legitimate new file
			// the files check above admits. Refusing it here, with "do not
			// keep a target that was refused", talked a planner out of the
			// new regression test the task needed.
			if slices.Contains(plan.Files.Paths(), ob.Path) && p.dirExists(path.Dir(ob.Path)) {
				continue
			}
			add(Contradiction{TargetFile, ob.Path,
				"is named by an obligation but is not a file in this repository", p.filesNear(ob.Path)})
		}
	}

	names := recipe.PresetNames(p.Presets, "")
	for _, want := range plan.Regenerate {
		checked++
		if !contains(names, want) {
			add(Contradiction{TargetPreset, want, "is not a frozen verification preset", names})
		}
	}
	return found, checked
}

// symbolResolves asks the graph, not the model.
func (p PlanTargets) symbolResolves(ctx context.Context, sym string) bool {
	if p.Graph == nil {
		// With no graph there is no deterministic evidence either way, and
		// inventing a refusal would be worse than not checking.
		return true
	}
	// The name as written, and the last component of a qualified name: a
	// planner that writes Class.method is naming something real.
	for _, candidate := range []string{sym, lastComponent(sym)} {
		nodes, err := p.Graph.NodesByName(ctx, candidate, nil, 1)
		if err == nil && len(nodes) > 0 {
			return true
		}
	}
	return false
}

// filesNear lists real files whose base name or directory is shared with the
// invented one. It is evidence, not a substitution.
func (p PlanTargets) filesNear(target string) []string {
	base := strings.TrimSuffix(path.Base(target), path.Ext(target))
	dir := path.Dir(target)
	var same, sibling []string
	_ = filepath.WalkDir(p.Root, func(full string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable entry is not evidence
		}
		if e.IsDir() {
			switch e.Name() {
			case ".git", ".bc", ".agent", "node_modules", "vendor", "__pycache__", "build", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if len(same)+len(sibling) > 200 {
			return filepath.SkipAll
		}
		rel, relErr := filepath.Rel(p.Root, full)
		if relErr != nil {
			return nil //nolint:nilerr // outside the root is not evidence
		}
		rel = filepath.ToSlash(rel)
		relBase := strings.TrimSuffix(path.Base(rel), path.Ext(rel))
		switch {
		case relBase == base:
			same = append(same, rel)
		case path.Dir(rel) == dir:
			sibling = append(sibling, rel)
		}
		return nil
	})
	sort.Strings(same)
	sort.Strings(sibling)
	out := append(same, sibling...)
	if len(out) == 0 {
		// Neither the name nor the directory exists, which is the case a
		// wholly invented path produces. The nearest real directory is still
		// evidence: it tells the planner where the repository actually keeps
		// things, which is what it got wrong.
		out = p.filesUnderNearestAncestor(dir)
	}
	if len(out) > maxEvidence {
		out = out[:maxEvidence]
	}
	return out
}

// filesUnderNearestAncestor walks up from a directory that does not exist to
// one that does, and lists what is really in it.
func (p PlanTargets) filesUnderNearestAncestor(dir string) []string {
	for d := dir; ; d = path.Dir(d) {
		full, err := worktree.Resolve(p.Root, d)
		if err == nil {
			if info, statErr := os.Stat(full); statErr == nil && info.IsDir() {
				return p.listFiles(full, d)
			}
		}
		if d == "." || d == "/" || d == "" {
			return nil
		}
	}
}

// listFiles names real files under a directory, shallowest first, bounded.
func (p PlanTargets) listFiles(full, rel string) []string {
	var out []string
	_ = filepath.WalkDir(full, func(child string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable entry is not evidence
		}
		if e.IsDir() {
			switch e.Name() {
			case ".git", ".bc", ".agent", "node_modules", "vendor", "__pycache__", "build", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if len(out) >= maxEvidence*4 {
			return filepath.SkipAll
		}
		r, relErr := filepath.Rel(p.Root, child)
		if relErr != nil {
			return nil //nolint:nilerr // outside the root is not evidence
		}
		out = append(out, filepath.ToSlash(r))
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		di, dj := strings.Count(out[i], "/"), strings.Count(out[j], "/")
		if di != dj {
			return di < dj
		}
		return out[i] < out[j]
	})
	return out
}

// symbolAlternatives gathers real declarations to offer instead, in the
// order that makes them most likely to be the one meant.
//
// The forensic runs showed this matter: every invalid-symbol correction
// carried an empty list, because the only source was a name lookup on the
// last component — `_prefetch`, `_slice` — which the repository does not
// declare. "X is not a symbol" with nothing to choose instead is feedback
// nobody can act on, and the planner reused the refused symbol twice.
//
// Bounded, deterministic, and evidence only: a near match never proves a
// symbol exists, it only tells the planner where to look.
func (p PlanTargets) symbolAlternatives(ctx context.Context, sym string, plan workflow.Plan) []string {
	var out []string
	seen := map[string]bool{}
	push := func(vals []string) {
		for _, v := range vals {
			if len(out) >= maxEvidence || seen[v] {
				continue
			}
			seen[v] = true
			out = append(out, v)
		}
	}
	// 1. What the file the planner named actually declares. A symbol written
	//    as Class.method or pkg.thing usually comes with the right file.
	for _, f := range plan.Files {
		push(p.symbolsInFile(ctx, f.Path))
	}
	// 2. Short-name matches anywhere in the repository.
	push(p.symbolsNear(ctx, sym))
	// 3. Declarations beside the symbols that did resolve, which is the
	//    neighbourhood the plan is already working in.
	for _, other := range plan.Symbols {
		if len(out) >= maxEvidence || other == sym {
			continue
		}
		if p.Graph == nil {
			break
		}
		nodes, err := p.Graph.NodesByName(ctx, lastComponent(other), nil, 2)
		if err != nil {
			continue
		}
		for _, n := range nodes {
			push(p.symbolsInFile(ctx, n.Path))
		}
	}
	return out
}

// symbolsNear lists real declarations sharing the invented symbol's last
// component, then declarations in the files the plan already named.
func (p PlanTargets) symbolsNear(ctx context.Context, sym string) []string {
	if p.Graph == nil {
		return nil
	}
	var out []string
	nodes, err := p.Graph.NodesByName(ctx, lastComponent(sym), nil, maxEvidence)
	if err == nil {
		for _, n := range nodes {
			out = append(out, fmt.Sprintf("%s in %s", n.Name, n.Path))
		}
	}
	sort.Strings(out)
	if len(out) > maxEvidence {
		out = out[:maxEvidence]
	}
	return out
}

func lastComponent(sym string) string {
	// A file prefix first: split on "." as it stood, the recorded
	// `internal/worker/reconcile.go:Reconciler` became `go:Reconciler`, which
	// names nothing, and a real type was refused.
	_, sym, _ = graph.SplitSymbol(sym)
	for _, sep := range []string{"#", ".", "::", "/"} {
		if i := strings.LastIndex(sym, sep); i >= 0 && i+len(sep) < len(sym) {
			sym = sym[i+len(sep):]
		}
	}
	return strings.TrimSuffix(sym, "()")
}

// CorrectionFor renders contradictions as an instruction the planner can act
// on: what is wrong, what the repository has instead, and what to do.
func CorrectionFor(found []Contradiction) string {
	var b strings.Builder
	b.WriteString("The previous plan named targets this repository does not contain.\n\n")
	for _, c := range found {
		b.WriteString(c.String())
		b.WriteString("\n\n")
	}
	b.WriteString("Generate a corrected plan using only verified repository targets. " +
		"Do not keep a target that was refused; either name one of the real alternatives " +
		"above or drop it.")
	return b.String()
}

// embeddedPath finds a real repository path inside a malformed target.
//
// It exists for exactly one recorded shape: the planner wrote the right file
// and appended its reasoning to the same string. The answer is not to accept
// the string — a field that means two things is the defect — but a
// correction that can say "did you mean src/_pytest/compat.py" is worth far
// more than one that says the target does not resolve.
//
// It is deterministic and it is not fuzzy matching: the candidate must be a
// whitespace-delimited prefix of the string *and* an existing file. A near
// match is never evidence that a target is valid; this is used only to word
// the refusal.
func (p PlanTargets) embeddedPath(target string) string {
	fields := strings.FieldsFunc(target, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '(' || r == ')' || r == ','
	})
	for _, candidate := range fields {
		candidate = strings.Trim(candidate, "`\"'")
		if candidate == "" || !strings.Contains(candidate, "/") {
			continue
		}
		full, err := worktree.Resolve(p.Root, candidate)
		if err != nil {
			continue
		}
		if info, statErr := os.Stat(full); statErr == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

// symbolsInFile lists the declarations the graph holds for one file. It is
// the first and best source of alternatives for a symbol the planner
// invented: it already told us which file it meant.
func (p PlanTargets) symbolsInFile(ctx context.Context, file string) []string {
	if p.Graph == nil || file == "" {
		return nil
	}
	rows, err := p.Graph.NodesInFile(ctx, file, maxEvidence*3)
	if err != nil {
		return nil
	}
	var out []string
	for _, n := range rows {
		if !workflow.PlannerActionable(n) {
			continue
		}
		out = append(out, fmt.Sprintf("%s (%s) in %s", n.Name, n.Kind, n.Path))
	}
	sort.Strings(out)
	if len(out) > maxEvidence {
		out = out[:maxEvidence]
	}
	return out
}
