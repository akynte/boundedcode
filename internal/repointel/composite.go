package repointel

import (
	"context"
	"errors"
	"sort"

	"github.com/akynte/boundedcode/internal/lang"
)

// Coverage is implemented by a navigator that serves some languages only
// (a language server per language): Covers lists the internal/lang ids it
// answers for under root.
type Coverage interface {
	Covers(root string) []string
}

// Composite routes symbol questions by the language of each file: the
// first provider that covers a file's language answers for it, and the
// text navigator answers for every language no provider covers. A Go
// service with a TypeScript client and SQL migrations gets gopls's and
// tsserver's answers where they serve and definition patterns for the
// SQL, in one result, instead of whichever answered first. A provider
// that does not report its coverage is treated as covering everything it
// returns, with the text navigator as the fallback when it fails or finds
// nothing (WithFallback's behaviour).
type Composite struct {
	Providers []Navigator
	Text      TextNavigator
	// NoText leaves the text navigator out of FindSymbol, for a caller
	// that has definition patterns' answers of its own (internal/retrieve).
	NoText bool
}

// Name implements Navigator.
func (c Composite) Name() string {
	name := ""
	for _, p := range c.Providers {
		name += p.Name() + "+"
	}
	return name + "text"
}

// covered maps language ids to the provider serving them under root.
func (c Composite) covered(root string) (map[string]int, bool) {
	m := map[string]int{}
	all := true
	for i, p := range c.Providers {
		cv, ok := p.(Coverage)
		if !ok {
			all = false
			continue
		}
		for _, id := range cv.Covers(root) {
			if _, taken := m[id]; !taken {
				m[id] = i
			}
		}
	}
	return m, all
}

func langID(file string) string {
	if l := lang.ForPath(file, ""); l != nil {
		return l.ID
	}
	return ""
}

// FindSymbol implements Navigator.
func (c Composite) FindSymbol(ctx context.Context, root, name string, opt FindOptions) ([]Symbol, error) {
	cov, all := c.covered(root)
	var out []Symbol
	var errs []error
	failed := map[int]bool{}
	anyAnswer := false
	for i, p := range c.Providers {
		s, err := p.FindSymbol(ctx, root, name, opt)
		if err != nil {
			errs = append(errs, err)
			failed[i] = true
			continue
		}
		for _, sym := range s {
			if j, ok := cov[langID(sym.File)]; ok && j != i {
				continue // another provider serves this language
			}
			out = append(out, sym)
			anyAnswer = true
		}
	}
	// The text navigator answers for the languages no working provider
	// covers, and for everything when a provider without coverage found
	// nothing.
	var text []Symbol
	if !c.NoText {
		t, err := c.Text.FindSymbol(ctx, root, name, opt)
		if err != nil {
			errs = append(errs, err)
		}
		text = t
	}
	for _, sym := range text {
		if j, served := cov[langID(sym.File)]; served && !failed[j] || !served && !all && anyAnswer {
			continue
		}
		out = append(out, sym)
	}
	if len(out) == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	sortSymbols(out)
	return out, nil
}

// References implements Navigator: the provider that covers the symbol's
// language, the text navigator otherwise or when it fails.
func (c Composite) References(ctx context.Context, root string, s Symbol) ([]Reference, error) {
	if p := c.providerFor(root, s.File); p != nil {
		if r, err := p.References(ctx, root, s); err == nil && len(r) > 0 {
			return r, nil
		}
	}
	return c.Text.References(ctx, root, s)
}

// Implementations implements Navigator; only a language server can answer.
func (c Composite) Implementations(ctx context.Context, root string, s Symbol) ([]Symbol, error) {
	if p := c.providerFor(root, s.File); p != nil {
		return p.Implementations(ctx, root, s)
	}
	return nil, ErrNeedsLanguageServer
}

func (c Composite) providerFor(root, file string) Navigator {
	cov, _ := c.covered(root)
	if i, ok := cov[langID(file)]; ok {
		return c.Providers[i]
	}
	for _, p := range c.Providers {
		if _, ok := p.(Coverage); !ok {
			return p
		}
	}
	return nil
}

func sortSymbols(s []Symbol) {
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].File != s[j].File {
			return s[i].File < s[j].File
		}
		return s[i].StartLine < s[j].StartLine
	})
}
