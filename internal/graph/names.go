package graph

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
)

// NameMatches reports whether a symbol, as a model or a person writes it,
// names this declaration: its short name, its fully-qualified name, or a tail
// of that name starting at a separator — `BulkDiscount.Discount`, or the
// package-qualified `pricing.BulkDiscount.Discount`, whose package name
// follows the last slash of a Go import path.
//
// The graph stores short names, and every lookup used to be an exact match on
// them. Models write qualified names: a plan named `Reconciler.RunOnce` and
// `internal/service/user.go::UserService.Email`, neither matched, and EDIT
// received a packet with no code in it. A separator boundary keeps a tail from
// matching a longer name that merely ends the same way.
func NameMatches(symbol string, n Node) bool {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return false
	}
	if symbol == n.Name || symbol == n.FQN {
		return true
	}
	if n.FQN == "" {
		return false
	}
	for _, sep := range []string{".", "/", "::", "#"} {
		if strings.HasSuffix(n.FQN, sep+symbol) {
			return true
		}
	}
	return false
}

var (
	// annotatedPath is a repository file inside a longer reference: a path
	// with a directory and an extension, and an optional :line after it.
	annotatedPath = regexp.MustCompile(`([\w.-]+(?:/[\w.-]+)+\.\w+)(?::\d+(?:-\d+)?)?`)
	// goReceiverForm is `func(Recv)Name` or `func (r *Recv) Name`.
	goReceiverForm = regexp.MustCompile(`func\s*\(\s*(?:\w+\s+)?\*?(\w+)\s*\)\s*(\w+)`)
	// leadingName is the dotted identifier a reference starts with.
	leadingName = regexp.MustCompile(`^[A-Za-z_][\w]*(?:(?:\.|::)[A-Za-z_][\w]*)*`)
)

// SplitSymbol separates the parts of a symbol reference a lookup needs: the
// file a `path::symbol` or `path:symbol` reference names, if any, and the
// short name the graph indexes it under.
//
// The left of the separator is a file only when it looks like one — it has a
// directory or an extension — because Rust and C++ spell ordinary qualified
// names with "::". Both forms were recorded in plans, `path::Symbol` from one
// model and `internal/worker/reconcile.go:Reconciler` from another.
func SplitSymbol(ref string) (path, symbol, short string) {
	symbol = strings.TrimSpace(ref)
	looksLikeFile := func(s string) bool {
		return s != "" && !strings.ContainsAny(s, " \t(") &&
			(strings.Contains(s, "/") || filepath.Ext(s) != "")
	}
	if strings.ContainsAny(symbol, " \t(") {
		// An annotated reference. Recorded plans wrote
		// `Email (method) in internal/service/user.go`,
		// `Store.Find (internal/service/user.go:16)` and
		// `internal/worker/reconcile.go:func(Reconciler)RunOnce`: the file,
		// wherever it appears, and the declaration name are what a lookup
		// needs; the commentary around them is not.
		if loc := annotatedPath.FindStringSubmatchIndex(symbol); loc != nil {
			path = symbol[loc[2]:loc[3]]
			symbol = strings.TrimSpace(symbol[:loc[0]] + " " + symbol[loc[1]:])
		}
		symbol = strings.TrimLeft(symbol, ": \t")
		if m := goReceiverForm.FindStringSubmatch(symbol); m != nil {
			symbol = m[1] + "." + m[2]
		} else if m := leadingName.FindString(symbol); m != "" {
			symbol = m
		}
	}
	if left, right, ok := strings.Cut(symbol, "::"); ok && looksLikeFile(left) {
		path, symbol = left, right
	} else if left, right, ok := strings.Cut(symbol, ":"); ok && !strings.HasPrefix(right, ":") &&
		looksLikeFile(left) && right != "" {
		path, symbol = left, right
	}
	short = symbol
	for _, sep := range []string{"::", ".", "#"} {
		if i := strings.LastIndex(short, sep); i >= 0 {
			short = short[i+len(sep):]
		}
	}
	return path, symbol, short
}

// Resolve finds the declarations a symbol reference names, written the way a
// model writes it: a short name, a qualified one such as `Reconciler.RunOnce`
// or `pricing.Calculator`, or one prefixed with its file.
//
// The exact lookup comes first, so a short name behaves as it always has. A
// qualified reference is then looked up by its short name and kept only where
// the full name agrees (NameMatches), which is what stops `Report.Count` from
// matching every Count in the repository. When a reference names a file and
// nothing agrees with the qualifier, a declaration of that short name in that
// file is still the one meant.
//
// Retrieval and plan validation both resolve through this, so they cannot
// disagree about what a name means. They did: validation accepted
// `Reconciler.RunOnce` by its last component while retrieval matched it
// exactly, found nothing, and EDIT was sent a packet with no code in it.
func Resolve(ctx context.Context, g Graph, ref string, limit int) ([]Node, error) {
	path, symbol, short := SplitSymbol(ref)
	if symbol == "" {
		return nil, nil
	}
	inPath := func(nodes []Node) []Node {
		if path == "" {
			return nodes
		}
		var kept []Node
		for _, n := range nodes {
			if n.Path == path {
				kept = append(kept, n)
			}
		}
		return kept
	}
	exact, err := g.NodesByName(ctx, symbol, nil, limit)
	if err != nil {
		return nil, err
	}
	if found := inPath(exact); len(found) > 0 || (short == symbol && path == "") {
		return found, nil
	}
	named, err := g.NodesByName(ctx, short, nil, limit*10)
	if err != nil {
		return nil, err
	}
	named = inPath(named)
	var qualified []Node
	for _, n := range named {
		if NameMatches(symbol, n) {
			qualified = append(qualified, n)
		}
	}
	if len(qualified) == 0 && path != "" {
		qualified = named
	}
	if len(qualified) > limit {
		qualified = qualified[:limit]
	}
	return qualified, nil
}
