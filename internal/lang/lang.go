// Package lang describes programming languages and file formats as data:
// how to recognise a file, where its definitions are, which files are
// tests, how to check a file after a change, which formatters, linters and
// language servers exist for it, and how its tools report diagnostics.
//
// Nothing in BoundedCode needs a language to be described here to work on
// it: reading, editing, searching, the shell and git are language-blind.
// A description adds capabilities (outlines, symbol search, a check after
// each edit, a census of the repository). A language is added by adding a
// Language value (or, for a user, an entry in the configuration), never by
// changing the agent loop.
package lang

import (
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Kinds of language.
const (
	KindProgramming = "programming"
	KindData        = "data"   // JSON, YAML, TOML, XML, SQL
	KindMarkup      = "markup" // Markdown, HTML
	KindInfra       = "infra"  // Terraform, Dockerfile, Kubernetes, Helm
)

// Language describes one language or format.
type Language struct {
	ID   string // "go", "typescript", "csharp"
	Name string // "Go", "TypeScript", "C#"
	Kind string
	// Extensions (".go"), file names ("Dockerfile", "Makefile") and
	// interpreters named on a #! line ("python3", "bash").
	Extensions, Filenames, Shebangs []string
	// Definitions match a definition at the start of a line; the group
	// named "name" is the symbol. Used for outlines and symbol search.
	Definitions []*regexp.Regexp
	// Tests match repository-relative test file paths (slash-separated).
	Tests []*regexp.Regexp
	// Check is the cheapest deterministic check of one file after a change
	// (nil: none exists).
	Check *Check
	// Format and Lint are tools that change or judge files; LSP are
	// language servers. The first one installed is used.
	Format, Lint, LSP []Tool
	// Diagnostics match the tool output lines that locate a problem, with
	// groups "file", "line", optionally "col" and "msg".
	Diagnostics []*regexp.Regexp
	// Frames match the lines of a stack trace (a panic, an exception, a
	// failed test's trace), with groups "file", "line", optionally "col"
	// and "symbol". FramesInnermostLast is set where the runtime prints the
	// failing call last (Python).
	Frames              []*regexp.Regexp
	FramesInnermostLast bool
	// Note says what a check cannot cover for this language.
	Note string
	// Outline lists the lines of a file's top-level definitions more
	// precisely than Definitions (a parser); nil uses Definitions.
	Outline func(src string) []int
	// Skips match a line that skips, disables or focuses tests (t.Skip,
	// @pytest.mark.skip, it.only, @Disabled, #[ignore]): added to a test,
	// it can make verification pass without testing anything.
	Skips []*regexp.Regexp
	// Mutation names the mutation provider that applies to the language's
	// files (internal/verify): "go" (its AST), "python" (its tokenizer and
	// parser), "c-family" (a lexer for C-like syntax: strings and comments
	// are never mutated), "c-family-hash" (the same, with # comments);
	// "" when mutation testing is unavailable.
	Mutation string
}

// Check is a per-file check.
type Check struct {
	// What names the check for the model ("python parse", "go build").
	What string
	// Tools are alternatives; the first installed runs with Args, where
	// {file} is the file's absolute path, {dir} its directory, {pkg} its
	// directory relative to Root as "./x" and {root} the Root directory.
	Tools []string
	Args  []string
	// Root names files marking the directory to run in (the nearest
	// ancestor that has one: go.mod, Cargo.toml); "" runs in {dir}.
	Root []string
	// InProcess checks the content without a tool (JSON, YAML, Go parse);
	// a non-nil error is the diagnostic.
	InProcess func(name string, src []byte) error
	// Then runs after InProcess passes (the Go build after its parse).
	Then *Check
	// Only limits the check to these extensions ("" = every extension of
	// the language): node parses .js, not .jsx.
	Only []string
	// TestArgs replace Args for the language's test files (go vet compiles
	// a package's tests, go build does not).
	TestArgs []string
}

// Tool is an executable with the arguments that apply it.
type Tool struct {
	Name string   // what the user knows it as ("gofmt", "ruff", "gopls")
	Bin  string   // the executable
	Args []string // with {file}, {dir}
	// Mutates is set for tools that change files (formatters).
	Mutates bool
}

var (
	mu       sync.RWMutex
	registry []*Language
	byExt    map[string]*Language
	byName   map[string]*Language
	byShe    map[string]*Language
	byID     map[string]*Language
)

// Register adds or replaces a language (by ID). Built-in languages are
// registered at start; a user's own come from the configuration.
func Register(l Language) {
	mu.Lock()
	defer mu.Unlock()
	for i, x := range registry {
		if x.ID == l.ID {
			registry[i] = &l
			reindex()
			return
		}
	}
	registry = append(registry, &l)
	reindex()
}

func reindex() {
	byExt, byName, byShe, byID = map[string]*Language{}, map[string]*Language{}, map[string]*Language{}, map[string]*Language{}
	for _, l := range registry {
		byID[l.ID] = l
		for _, e := range l.Extensions {
			byExt[strings.ToLower(e)] = l
		}
		for _, n := range l.Filenames {
			byName[strings.ToLower(n)] = l
		}
		for _, s := range l.Shebangs {
			byShe[s] = l
		}
	}
}

// All returns every registered language, by ID.
func All() []*Language {
	mu.RLock()
	defer mu.RUnlock()
	out := append([]*Language(nil), registry...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ByID returns a language, or nil.
func ByID(id string) *Language {
	mu.RLock()
	defer mu.RUnlock()
	return byID[id]
}

// ForPath returns the language of a file from its name and, when given,
// its first line (a #! line names the interpreter); nil when unknown.
func ForPath(name, firstLine string) *Language {
	mu.RLock()
	defer mu.RUnlock()
	base := strings.ToLower(path.Base(strings.ReplaceAll(name, "\\", "/")))
	if l := byName[base]; l != nil {
		return l
	}
	// Compound extensions first: .d.ts, .tftest.hcl, .blade.php.
	for i := 0; i < len(base); i++ {
		if base[i] == '.' && i > 0 {
			if l := byExt[base[i:]]; l != nil {
				return l
			}
		}
	}
	if strings.HasPrefix(base, "dockerfile.") || strings.HasSuffix(base, ".dockerfile") {
		return byID["dockerfile"]
	}
	if strings.HasPrefix(firstLine, "#!") {
		f := strings.Fields(strings.TrimPrefix(firstLine, "#!"))
		if len(f) > 0 {
			interp := path.Base(f[0])
			if interp == "env" && len(f) > 1 {
				interp = f[1]
				if interp == "-S" && len(f) > 2 {
					interp = f[2]
				}
			}
			interp = strings.TrimRight(interp, "0123456789.")
			if l := byShe[interp]; l != nil {
				return l
			}
		}
	}
	return nil
}

// IsTest reports whether rel is a test file of its language.
func (l *Language) IsTest(rel string) bool {
	for _, re := range l.Tests {
		if re.MatchString(rel) {
			return true
		}
	}
	return false
}

// Definition returns the symbol a line defines ("" when none).
func (l *Language) Definition(line string) (string, bool) {
	for _, re := range l.Definitions {
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if i := re.SubexpIndex("name"); i > 0 && m[i] != "" {
			if keywords[m[i]] {
				continue
			}
			return m[i], true
		}
		return "", true
	}
	return "", false
}
