package lang

import (
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Tool kinds a capability report distinguishes.
const (
	CapCheck  = "check"
	CapFormat = "format"
	CapLint   = "lint"
	CapLSP    = "lsp"
)

// Status of a capability.
const (
	Available   = "available"    // a tool is installed and will be used
	MissingTool = "missing tool" // the language has one, none is installed
	Unsupported = "unsupported"  // BoundedCode has none for the language
)

// Capability is one capability of a language here.
type Capability struct {
	Kind   string // CapCheck, CapFormat, CapLint, CapLSP
	Status string
	Tool   string // the tool used, or the ones that would be (missing)
	Path   string // where it is installed
}

// Finder looks tools up: on PATH, never in the project (a tool in the
// repository is the repository's code, not a host tool). Results are
// cached.
type Finder struct {
	// Exclude is a directory whose executables are not host tools (the
	// project).
	Exclude  string
	LookPath func(string) (string, error)
	mu       sync.Mutex
	cache    map[string]string
}

// Find returns the absolute path of an installed host tool, or "".
func (f *Finder) Find(bin string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.cache[bin]; ok {
		return p
	}
	if f.cache == nil {
		f.cache = map[string]string{}
	}
	look := f.LookPath
	if look == nil {
		look = exec.LookPath
	}
	p, err := look(bin)
	if err != nil || !filepath.IsAbs(p) {
		p = ""
	}
	if p != "" && f.Exclude != "" {
		if rel, err := filepath.Rel(f.Exclude, p); err == nil && !strings.HasPrefix(rel, "..") {
			p = "" // inside the project
		}
	}
	f.cache[bin] = p
	return p
}

// FindCheck returns the check for a file of this language with the
// installed tool it runs (path "" for an in-process check), and the status.
func (f *Finder) FindCheck(l *Language, name string) (*Check, string, string) {
	c := l.Check
	if c == nil {
		return nil, "", Unsupported
	}
	if len(c.Only) > 0 && name != "" {
		ok := false
		for _, e := range c.Only {
			ok = ok || strings.HasSuffix(strings.ToLower(name), e)
		}
		if !ok {
			return nil, "", Unsupported
		}
	}
	if c.InProcess != nil {
		return c, "", Available
	}
	for _, t := range c.Tools {
		if p := f.Find(t); p != "" {
			return c, p, Available
		}
	}
	return c, "", MissingTool
}

// Capabilities reports a language's capabilities here.
func (f *Finder) Capabilities(l *Language) []Capability {
	var out []Capability
	switch c, p, st := f.FindCheck(l, ""); {
	case c == nil && st == Unsupported:
		out = append(out, Capability{Kind: CapCheck, Status: Unsupported})
	case st == Available:
		tool := "built in"
		if p != "" {
			tool = filepath.Base(p)
		}
		out = append(out, Capability{Kind: CapCheck, Status: Available, Tool: tool, Path: p})
	default:
		if c.Then != nil || len(c.Tools) > 0 {
			out = append(out, Capability{Kind: CapCheck, Status: MissingTool, Tool: strings.Join(c.Tools, " or ")})
		}
	}
	for _, k := range []struct {
		kind  string
		tools []Tool
	}{{CapFormat, l.Format}, {CapLint, l.Lint}, {CapLSP, l.LSP}} {
		if len(k.tools) == 0 {
			out = append(out, Capability{Kind: k.kind, Status: Unsupported})
			continue
		}
		var names []string
		found := false
		for _, t := range k.tools {
			names = append(names, t.Name)
			if p := f.Find(t.Bin); p != "" {
				out = append(out, Capability{Kind: k.kind, Status: Available, Tool: t.Name, Path: p})
				found = true
				break
			}
		}
		if !found {
			out = append(out, Capability{Kind: k.kind, Status: MissingTool, Tool: strings.Join(names, " or ")})
		}
	}
	return out
}

// Expand substitutes {file}, {dir}, {pkg} and {root} in args.
func Expand(args []string, vars map[string]string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		for k, v := range vars {
			a = strings.ReplaceAll(a, "{"+k+"}", v)
		}
		out[i] = a
	}
	return out
}
