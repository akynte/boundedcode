package serena

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/akynte/boundedcode/internal/repointel"
	"github.com/akynte/boundedcode/internal/workspace"
)

// Navigator implements repointel.Navigator over a Manager.
type Navigator struct {
	M *Manager

	mu      sync.Mutex
	langs   map[string][]string
	ignored map[string][]string
}

var _ repointel.Navigator = (*Navigator)(nil)

// Name implements repointel.Navigator.
func (n *Navigator) Name() string { return "serena" }

// project returns the instance definition for root. Languages are detected
// once per root unless the caller registered the project explicitly.
func (n *Navigator) project(root string) Project {
	root = canonical(root)
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.langs == nil {
		n.langs = map[string][]string{}
	}
	l, ok := n.langs[root]
	if !ok {
		l = SupportedLanguages(workspace.DetectLanguages(root))
		n.langs[root] = l
	}
	return Project{Root: root, Languages: l, Ignored: n.ignored[root]}
}

// Ignore hides repo-relative paths under root from Serena (the sandbox's
// secret masks), so symbol context never contains what the agent may not
// read. It applies to instances started afterwards; a running instance with
// different ignores is restarted.
func (n *Navigator) Ignore(root string, paths []string) {
	root = canonical(root)
	n.mu.Lock()
	if n.ignored == nil {
		n.ignored = map[string][]string{}
	}
	changed := strings.Join(n.ignored[root], "\x00") != strings.Join(paths, "\x00")
	n.ignored[root] = append([]string(nil), paths...)
	n.mu.Unlock()
	if changed {
		n.M.Stop(root)
	}
}

// Release stops the instances serving roots (end of a task run). Their
// caches stay, so a resumed task starts warm.
func (n *Navigator) Release(roots ...string) {
	for _, r := range roots {
		n.M.Stop(r)
	}
}

// wire formats of Serena v1.7.0 (lines are 0-based).
type wireLocation struct {
	StartLine int `json:"start_line"`
	EndLine   int `json:"end_line"`
}

type wireSymbol struct {
	NamePath     string       `json:"name_path"`
	Kind         string       `json:"kind"`
	RelativePath string       `json:"relative_path"`
	BodyLocation wireLocation `json:"body_location"`
	Body         string       `json:"body"`
}

func (w wireSymbol) symbol() repointel.Symbol {
	return repointel.Symbol{NamePath: w.NamePath, Kind: w.Kind, File: w.RelativePath,
		StartLine: w.BodyLocation.StartLine + 1, EndLine: w.BodyLocation.EndLine + 1, Body: w.Body}
}

// FindSymbol implements repointel.Navigator.
func (n *Navigator) FindSymbol(ctx context.Context, root, name string, opt repointel.FindOptions) ([]repointel.Symbol, error) {
	args := map[string]any{"name_path_pattern": name, "include_body": opt.IncludeBody, "max_answer_chars": 60000}
	if opt.Within != "" {
		args["relative_path"] = opt.Within
	}
	out, err := n.M.Call(ctx, n.project(root), "find_symbol", args)
	if err != nil {
		return nil, err
	}
	return decodeSymbols(out)
}

// Implementations implements repointel.Navigator.
func (n *Navigator) Implementations(ctx context.Context, root string, s repointel.Symbol) ([]repointel.Symbol, error) {
	out, err := n.M.Call(ctx, n.project(root), "find_implementations",
		map[string]any{"name_path": s.NamePath, "relative_path": s.File, "max_answer_chars": 60000})
	if err != nil {
		return nil, err
	}
	return decodeSymbols(out)
}

// References implements repointel.Navigator.
func (n *Navigator) References(ctx context.Context, root string, s repointel.Symbol) ([]repointel.Reference, error) {
	out, err := n.M.Call(ctx, n.project(root), "find_referencing_symbols",
		map[string]any{"name_path": s.NamePath, "relative_path": s.File, "max_answer_chars": 60000})
	if err != nil {
		return nil, err
	}
	return decodeReferences(out)
}

func decodeSymbols(out string) ([]repointel.Symbol, error) {
	var ws []wireSymbol
	if err := json.Unmarshal([]byte(out), &ws); err != nil {
		return nil, fmt.Errorf("serena: unexpected symbol result: %s", lastN(strings.TrimSpace(out), 200))
	}
	syms := make([]repointel.Symbol, 0, len(ws))
	for _, w := range ws {
		syms = append(syms, w.symbol())
	}
	return syms, nil
}

// markedLineRE finds the referencing line in content_around_reference:
// "  >  35:\tif err := ..." (0-based line number).
var markedLineRE = regexp.MustCompile(`(?m)^\s*>\s*(\d+):(.*)$`)

func decodeReferences(out string) ([]repointel.Reference, error) {
	// {"<file>": {"<Kind>": [{name_path, body_location, content_around_reference}]}}
	var byFile map[string]map[string][]struct {
		NamePath string `json:"name_path"`
		Content  string `json:"content_around_reference"`
	}
	if err := json.Unmarshal([]byte(out), &byFile); err != nil {
		return nil, fmt.Errorf("serena: unexpected reference result: %s", lastN(strings.TrimSpace(out), 200))
	}
	var refs []repointel.Reference
	for file, kinds := range byFile {
		for kind, items := range kinds {
			for _, it := range items {
				r := repointel.Reference{File: file, Symbol: it.NamePath, Kind: kind}
				if m := markedLineRE.FindStringSubmatch(it.Content); m != nil {
					r.Line, _ = strconv.Atoi(m[1])
					r.Line++
					r.Snippet = strings.TrimSpace(m[2])
				}
				refs = append(refs, r)
			}
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].File != refs[j].File {
			return refs[i].File < refs[j].File
		}
		return refs[i].Line < refs[j].Line
	})
	return refs, nil
}
