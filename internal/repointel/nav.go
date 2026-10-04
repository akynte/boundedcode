package repointel

import (
	"context"
	"errors"
)

// Navigator answers precise, symbol-level questions about one checkout using
// a language server (definitions, references, implementations). It
// complements Intelligence: the code graph covers breadth (search across
// indexed repositories, call tracing, change impact, cross-service links);
// a Navigator covers depth inside one repository (ADR-0008).
//
// Root is the absolute path of the checkout to analyze. For a task this is
// always the task worktree, never the primary checkout, so answers reflect
// the task's uncommitted edits.
type Navigator interface {
	Name() string
	// FindSymbol finds symbols whose name (or name-path suffix) matches name.
	FindSymbol(ctx context.Context, root, name string, opt FindOptions) ([]Symbol, error)
	// References lists the symbols that reference s.
	References(ctx context.Context, root string, s Symbol) ([]Reference, error)
	// Implementations lists the symbols implementing s (an interface or one
	// of its methods).
	Implementations(ctx context.Context, root string, s Symbol) ([]Symbol, error)
}

// FindOptions tune FindSymbol.
type FindOptions struct {
	IncludeBody bool
	// Within restricts the search to a file or directory (repo-relative).
	Within string
}

// Symbol is a code symbol located by a language server.
type Symbol struct {
	// NamePath is the symbol's path inside its file, e.g. "Store/Get" or, for
	// Go methods, just "Get". It identifies the symbol together with File.
	NamePath  string `json:"name_path"`
	Kind      string `json:"kind"`
	File      string `json:"file"`       // repo-relative, slash-separated
	StartLine int    `json:"start_line"` // 1-based, inclusive
	EndLine   int    `json:"end_line"`
	Body      string `json:"body,omitempty"`
}

// Reference is one place that refers to a symbol.
type Reference struct {
	File    string `json:"file"`
	Symbol  string `json:"symbol"` // name path of the enclosing symbol
	Kind    string `json:"kind"`   // kind of the enclosing symbol
	Line    int    `json:"line"`   // 1-based line of the reference
	Snippet string `json:"snippet"`
}

// ErrUnavailable reports that a backend cannot serve requests right now (not
// installed, unsupported version, failed to start, or temporarily disabled
// after repeated failures). Callers fall back to other sources.
var ErrUnavailable = errors.New("repository navigation backend unavailable")

// IsInterfaceKind reports whether a symbol kind can have implementations.
func IsInterfaceKind(kind string) bool { return kind == "Interface" }
