// Package compat is the cross-repository compatibility gate. When a task
// changes a cross-service contract (gRPC, protobuf or OpenAPI), it finds the
// contract links the change affects, runs the affected repositories' existing
// checks in the sandbox against the other repositories' candidate commits,
// and records for every affected link a result tied to exact commits:
//
//   - compatible: every side the link needs was exercised by a check, built
//     against the candidate commits of the repositories it depends on, and
//     passed;
//   - broken: the candidate definition no longer provides what a dependent
//     still uses, or a dependent's checks fail with the candidate commits and
//     pass with the base commits;
//   - untested: anything else, with what evidence is missing (a repository
//     outside the task, an unsupported language or contract shape, an
//     ambiguous link, a missing dependency, or checks that never exercise the
//     link).
//
// Compatibility is never concluded from static analysis or from passing
// tests that do not exercise the link. See
// docs/design/cross-repo-compatibility.md.
package compat

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Result is a link's compatibility verdict.
type Result string

// Results.
const (
	Compatible Result = "compatible"
	Broken     Result = "broken"
	Untested   Result = "untested"
)

// Change says how the change relates to a link.
const (
	ChangeModified = "changed" // the link exists before and after; a side or its definition changed
	ChangeAdded    = "added"   // the change creates the link
	ChangeRemoved  = "removed" // the link existed at the base commits and no longer does
)

// Gaps of untested results.
const (
	// GapNotExercised: checks ran and passed but none executed this side
	// (or none reads the operation); a test that does would decide it.
	GapNotExercised = "not_exercised"
	// GapBreaking: the task's repositories agree, but the definition change
	// breaks code built from the base definition.
	GapBreaking = "breaking_definition"
)

// GateKinds are the link kinds the gate evaluates (xservice.Link.Kind).
var GateKinds = map[string]bool{"grpc": true, "grpc_def": true, "proto": true, "openapi": true, "openapi_impl": true}

// Side is one endpoint of a link, at the commit it was read from.
type Side struct {
	Repo   string `json:"repo"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Kind   string `json:"kind"`
	Commit string `json:"commit,omitempty"` // empty: from the workspace index (not a task repository)
}

func (s Side) where() string {
	return fmt.Sprintf("%s@%s %s:%d", s.Repo, short(s.Commit), s.File, s.Line)
}

// Check is one command the gate ran for a link.
type Check struct {
	// Purpose: candidate (the candidate commits), control (the base
	// commits, run when the candidate fails), knockout (the candidate with
	// the operation removed from the specification), resolve (where Go
	// resolves the provider's module).
	Purpose string `json:"purpose"`
	Repo    string `json:"repo"`
	Command string `json:"command"`
	// Composition is the commit of each repository in the tree the command
	// ran in.
	Composition map[string]string `json:"composition"`
	Status      string            `json:"status"` // pass | fail | error | skipped
	ExitCode    int               `json:"exit_code,omitempty"`
	DurationMS  int64             `json:"duration_ms"`
	// Exercised says whether and how the command executed this link's side.
	Exercised string `json:"exercised,omitempty"`
	Output    string `json:"output,omitempty"` // failure digest, redacted
}

// LinkResult is the gate's record for one affected link.
type LinkResult struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Contract string `json:"contract"`
	From     Side   `json:"from"` // the dependent
	To       Side   `json:"to"`   // what it depends on
	Change   string `json:"change"`
	Result   Result `json:"result"`
	Reason   string `json:"reason"`
	// Gap categorizes an untested result the task can act on (GapNotExercised,
	// GapBreaking); empty for the others (unsupported shapes, repositories
	// outside the task, missing dependencies).
	Gap      string   `json:"gap,omitempty"`
	Breaking []string `json:"breaking,omitempty"` // breaking definition changes found statically
	Notes    []string `json:"notes,omitempty"`
	Checks   []Check  `json:"checks,omitempty"`
	// Commits are the base..head commits of the task repositories the
	// result depends on; it is stale once any of them moves.
	Commits     map[string]string `json:"commits"`
	EvaluatedAt string            `json:"evaluated_at"`
	// Stale is set when loading: a repository's commit moved since.
	Stale bool `json:"stale,omitempty"`
}

// Report is the gate's result for a task at its current commits.
type Report struct {
	Links []LinkResult `json:"links"`
	// Unaffected are links whose files changed without changing the link
	// (another function in the file, another RPC of the .proto, a comment).
	Unaffected []LinkResult `json:"unaffected,omitempty"`
	// Error is set when the gate could not evaluate the task; the task is
	// then not cross-repository compatible.
	Error string `json:"error,omitempty"`
	// Reused counts results taken from the ledger (same commits).
	Reused int `json:"reused,omitempty"`
	// Incomplete is set when loading an evaluation that did not finish.
	Incomplete bool `json:"incomplete,omitempty"`
}

// Count returns the number of links with a result.
func (r Report) Count(res Result) int {
	n := 0
	for _, l := range r.Links {
		if l.Result == res {
			n++
		}
	}
	return n
}

// State summarizes the report: none (no affected link), compatible, broken,
// untested or error.
func (r Report) State() string {
	switch {
	case r.Error != "":
		return "error"
	case r.Count(Broken) > 0:
		return string(Broken)
	case r.Count(Untested) > 0:
		return string(Untested)
	case len(r.Links) > 0:
		return string(Compatible)
	}
	return "none"
}

// Clear reports whether the gate allows TASK_VERIFIED: every affected link
// is compatible (or none is affected).
func (r Report) Clear() bool { s := r.State(); return s == "none" || s == string(Compatible) }

// Summary is one line: the state and the counts.
func (r Report) Summary() string {
	switch r.State() {
	case "error":
		return "cross-repository compatibility: ERROR (" + r.Error + ")"
	case "none":
		return "cross-repository compatibility: no affected gRPC, protobuf or OpenAPI link"
	}
	return fmt.Sprintf("cross-repository compatibility: %s (%d broken, %d untested, %d compatible)",
		strings.ToUpper(r.State()), r.Count(Broken), r.Count(Untested), r.Count(Compatible))
}

// Lines renders the concise per-link report: one block per link, broken
// first.
func (r Report) Lines() []string {
	out := []string{r.Summary()}
	links := append([]LinkResult(nil), r.Links...)
	order := map[Result]int{Broken: 0, Untested: 1, Compatible: 2}
	sort.SliceStable(links, func(i, j int) bool { return order[links[i].Result] < order[links[j].Result] })
	for _, l := range links {
		out = append(out, l.Lines()...)
	}
	if n := len(r.Unaffected); n > 0 {
		out = append(out, fmt.Sprintf("  (%d link(s) in changed files not affected by the change)", n))
	}
	return out
}

// Lines renders one link.
func (l LinkResult) Lines() []string {
	head := fmt.Sprintf("  %-10s %-12s %s [%s]", strings.ToUpper(string(l.Result)), l.Kind, l.Contract, l.Change)
	if l.Stale {
		head += " (STALE: commits moved)"
	}
	out := []string{head,
		"             " + l.From.where() + " -> " + l.To.where(),
		"             " + l.Reason}
	for _, b := range l.Breaking {
		out = append(out, "             breaking: "+b)
	}
	for _, c := range l.Checks {
		line := fmt.Sprintf("             %s %s: `%s` in %s => %s", c.Purpose, c.Repo, displayCommand(c.Command), composition(c.Composition), c.Status)
		if c.Exercised != "" {
			line += "; " + c.Exercised
		}
		out = append(out, line)
	}
	return out
}

// displayCommand shortens a check's command for the report: the coverage
// profile's scratch path is dropped (the full command is in the record).
func displayCommand(c string) string {
	var out []string
	for _, a := range strings.Fields(c) {
		if strings.HasPrefix(a, "-coverprofile=") {
			continue
		}
		out = append(out, a)
	}
	return trunc(strings.Join(out, " "), 160)
}

func composition(m map[string]string) string {
	var parts []string
	for _, k := range sortedKeys(m) {
		parts = append(parts, k+"@"+short(m[k]))
	}
	return strings.Join(parts, " + ")
}

// EvidenceRequest is the retry instruction for untested links a test in a
// task repository could decide; empty when there are none.
func (r Report) EvidenceRequest() string {
	var items []string
	for _, l := range r.Links {
		if l.Result == Untested && l.Gap == GapNotExercised {
			items = append(items, fmt.Sprintf("- %s %s (%s:%d in %s depends on %s): %s", l.Kind, l.Contract, l.From.File, l.From.Line, l.From.Repo, l.To.Repo, l.Reason))
		}
	}
	if len(items) == 0 {
		return ""
	}
	return "Cross-repository compatibility (deterministic gate): checks pass, but none executes these contract uses, so their compatibility with the other repositories' changes is untested:\n" +
		strings.Join(items, "\n") + "\nIf the code at those lines matters to the task, add or extend a test in the same repository that executes it " +
		"(for a gRPC call: call the function containing it with a fake connection; for an OpenAPI operation: a contract test that reads the specification). " +
		"Do not change the other repositories' definitions to make this pass."
}

// BrokenRequest is the retry instruction for broken links.
func (r Report) BrokenRequest() string {
	var items []string
	for _, l := range r.Links {
		if l.Result == Broken {
			items = append(items, fmt.Sprintf("- %s %s: %s depends on %s. %s", l.Kind, l.Contract, l.From.where(), l.To.where(), l.Reason))
		}
	}
	return "Cross-repository compatibility check failed (deterministic gate: each repository's own checks run against the other task repositories' candidate commits):\n" +
		strings.Join(items, "\n") + "\nMake the dependents work with the changed contract (update their code and tests), or make the change backward compatible " +
		"(add fields or operations instead of renaming or removing them)."
}

// FailureSummary describes the broken links for a retry.
func (r Report) FailureSummary() string {
	var parts []string
	for _, l := range r.Links {
		if l.Result == Broken {
			parts = append(parts, fmt.Sprintf("%s %s (%s -> %s): %s", l.Kind, l.Contract, l.From.Repo, l.To.Repo, l.Reason))
		}
	}
	return "cross-repository: " + strings.Join(parts, "; ")
}

// Signature fingerprints the broken links (loop detection across attempts).
func (r Report) Signature() string {
	var ids []string
	for _, l := range r.Links {
		if l.Result == Broken {
			ids = append(ids, l.ID)
		}
	}
	sort.Strings(ids)
	return "compat:" + strings.Join(ids, ",")
}

// linkID is a link's identity across versions: its kind, contract and the
// files and kinds of its sides (lines move).
func linkID(kind, contract string, from, to Side) string {
	h := sha256.Sum256([]byte(strings.Join([]string{kind, contract, from.Repo, from.File, from.Kind, to.Repo, to.File, to.Kind}, "\x00")))
	return hex.EncodeToString(h[:8])
}

func short(sha string) string {
	if sha == "" {
		return "index"
	}
	return sha[:min(len(sha), 10)]
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
