package repointel

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/lang"
	"github.com/akynte/boundedcode/internal/policy"
)

// TextNavigator finds symbols in any language from the definition patterns
// of internal/lang: no language server, no index, no build. It is what
// navigation falls back to where no language server serves a language. A
// definition is a line matching a pattern; a reference is the name as a
// whole word. It cannot find implementations.
type TextNavigator struct {
	// MaxFiles bounds a search (default 30000); MaxBytes skips larger
	// files (default 2 MiB).
	MaxFiles int
	MaxBytes int64
}

// Name implements Navigator.
func (TextNavigator) Name() string { return "text" }

// ErrNeedsLanguageServer is returned for what only a language server can
// answer.
var ErrNeedsLanguageServer = errors.New("this needs a language server, and none serves this language here")

type textFile struct {
	rel  string
	lang *lang.Language
	data []byte
}

// walk calls fn for each text file under root (or within) that contains
// needle, in path order.
func (n TextNavigator) walk(ctx context.Context, root, within string, needle []byte, fn func(textFile) bool) error {
	maxFiles, maxBytes := n.MaxFiles, n.MaxBytes
	if maxFiles <= 0 {
		maxFiles = 30000
	}
	if maxBytes <= 0 {
		maxBytes = 2 << 20
	}
	start := root
	if within != "" {
		start = filepath.Join(root, filepath.FromSlash(within))
		if rel, err := filepath.Rel(root, start); err != nil || strings.HasPrefix(rel, "..") {
			return errors.New("within is outside the project")
		}
	}
	count := 0
	stop := errors.New("stop")
	err := filepath.WalkDir(start, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // unreadable entries are skipped
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if p != start && (lang.SkipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if count++; count > maxFiles {
			return stop
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxBytes {
			return nil //nolint:nilerr // skipped
		}
		b, err := os.ReadFile(p)
		if err != nil || !bytes.Contains(b, needle) || bytes.IndexByte(b[:min(len(b), 8000)], 0) >= 0 {
			return nil //nolint:nilerr // unreadable, irrelevant or binary
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if policy.IsSecretPath(rel) {
			return nil // the same mask as grep and read
		}
		first, _, _ := bytes.Cut(b, []byte("\n"))
		if !fn(textFile{rel: rel, lang: lang.ForPath(rel, string(first)), data: b}) {
			return stop
		}
		return nil
	})
	if errors.Is(err, stop) {
		return nil
	}
	return err
}

type def struct {
	name   string
	line   int // 1-based
	indent int
}

func definitions(l *lang.Language, lines []string) []def {
	if l == nil {
		return nil
	}
	var out []def
	for i, s := range lines {
		if name, ok := l.Definition(s); ok {
			out = append(out, def{name: name, line: i + 1, indent: len(s) - len(strings.TrimLeft(s, " \t"))})
		}
	}
	return out
}

// end estimates where the definition at index i ends: before the next
// definition indented no deeper, or after maxLines.
func end(defs []def, i, total int) int {
	const maxLines = 300
	e := total
	for j := i + 1; j < len(defs); j++ {
		if defs[j].indent <= defs[i].indent {
			e = defs[j].line - 1
			break
		}
	}
	if e-defs[i].line+1 > maxLines {
		e = defs[i].line + maxLines - 1
	}
	return e
}

func splitLines(b []byte) []string {
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}

// FindSymbol implements Navigator: definitions named name (the last part
// of a name path "Type/Method").
func (n TextNavigator) FindSymbol(ctx context.Context, root, name string, opt FindOptions) ([]Symbol, error) {
	target := name
	if i := strings.LastIndexAny(target, "/."); i >= 0 && i < len(target)-1 {
		target = target[i+1:]
	}
	if target == "" {
		return nil, errors.New("no name")
	}
	var out []Symbol
	err := n.walk(ctx, root, opt.Within, []byte(target), func(f textFile) bool {
		if f.lang == nil {
			return true
		}
		lines := splitLines(f.data)
		defs := definitions(f.lang, lines)
		for i, d := range defs {
			if d.name != target && !strings.HasSuffix(d.name, "."+target) && !strings.HasSuffix(d.name, "::"+target) {
				continue
			}
			s := Symbol{NamePath: d.name, Kind: f.lang.Name + " definition", File: f.rel, StartLine: d.line, EndLine: end(defs, i, len(lines))}
			if opt.IncludeBody {
				s.Body = strings.Join(lines[s.StartLine-1:s.EndLine], "\n")
			}
			out = append(out, s)
		}
		return len(out) < 50
	})
	return out, err
}

// References implements Navigator: the name as a whole word in any text
// file, with the definition that encloses each use; the definitions
// themselves are not listed.
func (n TextNavigator) References(ctx context.Context, root string, s Symbol) ([]Reference, error) {
	target := s.NamePath
	if i := strings.LastIndexAny(target, "/."); i >= 0 && i < len(target)-1 {
		target = target[i+1:]
	}
	word := regexp.MustCompile(`(^|[^\w$])` + regexp.QuoteMeta(target) + `($|[^\w$])`)
	var out []Reference
	err := n.walk(ctx, root, "", []byte(target), func(f textFile) bool {
		lines := splitLines(f.data)
		defs := definitions(f.lang, lines)
		enclosing := func(line int) (string, string) {
			name, kind := "", "file"
			for _, d := range defs {
				if d.line > line {
					break
				}
				name, kind = d.name, "definition"
			}
			return name, kind
		}
		for i, l := range lines {
			if !word.MatchString(l) {
				continue
			}
			if f.lang != nil {
				if name, ok := f.lang.Definition(l); ok && name == target {
					continue
				}
			}
			sym, kind := enclosing(i + 1)
			out = append(out, Reference{File: f.rel, Symbol: sym, Kind: kind, Line: i + 1, Snippet: strings.TrimSpace(l)})
			if len(out) >= 200 {
				return false
			}
		}
		return true
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out, err
}

// Implementations implements Navigator.
func (TextNavigator) Implementations(context.Context, string, Symbol) ([]Symbol, error) {
	return nil, ErrNeedsLanguageServer
}

// WithFallback uses primary (a language server) and, where it is
// unavailable or finds nothing, the text navigator.
func WithFallback(primary Navigator) Navigator {
	if primary == nil {
		return TextNavigator{}
	}
	return fallback{primary: primary}
}

type fallback struct{ primary Navigator }

func (f fallback) Name() string { return f.primary.Name() + "+text" }

func (f fallback) FindSymbol(ctx context.Context, root, name string, opt FindOptions) ([]Symbol, error) {
	s, err := f.primary.FindSymbol(ctx, root, name, opt)
	if err == nil && len(s) > 0 {
		return s, nil
	}
	return TextNavigator{}.FindSymbol(ctx, root, name, opt)
}

func (f fallback) References(ctx context.Context, root string, s Symbol) ([]Reference, error) {
	r, err := f.primary.References(ctx, root, s)
	if err == nil && len(r) > 0 {
		return r, nil
	}
	return TextNavigator{}.References(ctx, root, s)
}

func (f fallback) Implementations(ctx context.Context, root string, s Symbol) ([]Symbol, error) {
	return f.primary.Implementations(ctx, root, s)
}
