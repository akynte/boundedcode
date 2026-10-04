// Package repointel defines the repository-intelligence boundary: code graph,
// search, call tracing and change impact. The initial implementation wraps
// codebase-memory-mcp (ADR-0005). Results are compact text intended for
// context packs, plus typed fields where the control plane needs them.
package repointel

import "context"

// Intelligence answers structural questions about indexed repositories.
type Intelligence interface {
	Name() string
	// Index (re)indexes a repository and returns its project handle.
	Index(ctx context.Context, repoPath, name string, mode string) (IndexResult, error)
	// Search finds symbols by name pattern / BM25 query.
	Search(ctx context.Context, project, query string, limit int) (string, error)
	// Trace returns callers/callees of a function.
	Trace(ctx context.Context, project, function, direction string, depth int) (string, error)
	// Snippet returns source for a qualified or short name.
	Snippet(ctx context.Context, project, name string) (string, error)
	// Impact maps the working-tree diff (vs. baseBranch) to affected symbols.
	Impact(ctx context.Context, project, baseBranch string, depth int) (string, error)
	// Architecture returns an overview of the project.
	Architecture(ctx context.Context, project string) (string, error)
}

// IndexResult summarizes an indexing run.
type IndexResult struct {
	Project  string  `json:"project"`
	Nodes    int     `json:"nodes"`
	Edges    int     `json:"edges"`
	Status   string  `json:"status"`
	Seconds  float64 `json:"seconds"`
	Skipped  int     `json:"skipped_count"`
	Partial  int     `json:"parse_partial_count"`
	Unusable int     `json:"parse_unusable_count"`
}
