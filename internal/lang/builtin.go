package lang

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

func re(s ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(s))
	for i, x := range s {
		out[i] = regexp.MustCompile(x)
	}
	return out
}

// Stack-frame formats (Language.Frames). A frame names the file, the line
// and, where the runtime prints it, the function.
var (
	goFrames = re(`^\s+(?P<file>\S+\.go):(?P<line>\d+)(?: \+0x[0-9a-f]+)?\s*$`)
	jsFrames = re(`^\s*at (?:async )?(?:(?P<symbol>[^\s(]+) \()?(?:file://)?(?P<file>[^\s()]+?\.(?:[cm]?[jt]sx?|vue|svelte)):(?P<line>\d+):(?P<col>\d+)\)?`)
	pyFrames = re(`^\s*File "(?P<file>[^"]+)", line (?P<line>\d+)(?:, in (?P<symbol>[\w<>.]+))?`)
	// rustc prints "panicked at src/x.rs:2:5" (and, before 1.73, the
	// message first); backtraces print "at ./src/x.rs:5:5".
	rustFrames = re(`panicked at (?:'[^']*', )?(?P<file>[^\s:']+\.rs):(?P<line>\d+):(?P<col>\d+)`,
		`^\s+at (?:\./)?(?P<file>[^\s:]+\.rs):(?P<line>\d+):(?P<col>\d+)`)
	// The JVM prints the class's file name only; its package is in the
	// symbol ("at com.acme.Orders.total(Orders.java:42)").
	jvmFrames    = re(`^\s*at (?P<symbol>[\w$.<>/]+)\((?P<file>[\w$-]+\.(?:java|kt|kts|scala|groovy)):(?P<line>\d+)\)`)
	nativeFrames = re(`#\d+\s+0x[0-9a-f]+ in (?P<symbol>\S+) (?:\([^)]*\) )?(?P<file>\S+\.(?:c|cc|cpp|cxx|h|hh|hpp)):(?P<line>\d+)`)
	csFrames     = re("^\\s*at (?P<symbol>[\\w.<>`+\\[\\], ]+)\\(.*\\) in (?P<file>.+?\\.cs):line (?P<line>\\d+)")
	phpFrames    = re(`^#\d+ (?P<file>[^\s(]+\.php)\((?P<line>\d+)\): (?P<symbol>[\w\\:>-]+)`, ` in (?P<file>\S+\.php):(?P<line>\d+)`)
	rubyFrames   = re("^\\s*(?:from )?(?P<file>[^\\s:]+\\.rb):(?P<line>\\d+):in [`'](?:[\\w:]+[#.])?(?P<symbol>[^'`]+)'")
	swiftFrames  = re(`^\d+\s+\S+\s+0x[0-9a-f]+ (?P<symbol>.+?) \+ \d+ at (?P<file>\S+\.swift):(?P<line>\d+)`, `file (?P<file>\S+\.swift), line (?P<line>\d+)`)
	dartFrames   = re(`^#\d+\s+(?P<symbol>\S+) \((?:package:\w+/|file://)?(?P<file>[^\s:]+\.dart):(?P<line>\d+)(?::(?P<col>\d+))?\)`)
	elixirFrames = re(`^\s*(?:\([\w ]+[\d.]*\) )?(?P<file>[\w/.-]+\.exs?):(?P<line>\d+): (?P<symbol>[\w.]+/\d+)`)
)

// pyParse parses a Python file without importing it or writing bytecode.
const pyParse = "import ast,sys\nsrc=open(sys.argv[1],'rb').read()\nast.parse(src, sys.argv[1])\n"

func init() {
	for _, l := range builtins() {
		Register(l)
	}
}

// Per-file checks are listed only where the tool's exit status means "this
// file does not parse" and nothing else; where a check needs the build's
// flags, class path or downloaded packages (C, C++, Java, Kotlin, C#,
// Rust, TypeScript, Swift, Dart), the project's own build is the check and
// Note says so.
func builtins() []Language {
	return []Language{
		{ID: "go", Frames: goFrames, Mutation: "go", Skips: re(`\bt\.Skip(?:f|Now)?\(`, `^//go:build\s`, `^// \+build\s`), Name: "Go", Kind: KindProgramming, Extensions: []string{".go"},
			Definitions: re(`^func\s+(?:\([^)]*\)\s*)?(?P<name>\w+)`, `^type\s+(?P<name>\w+)`, `^(?:var|const)\s+(?P<name>\w+)`,
				`^\t(?P<name>[A-Za-z_]\w*)\s+(?:struct|interface)\b`),
			Tests:   re(`_test\.go$`),
			Outline: goOutline,
			Check: &Check{What: "Go parse", InProcess: goParse,
				Then: &Check{What: "go build", Tools: []string{"go"}, Args: []string{"build", "-o", nullDevice, "{pkg}"}, TestArgs: []string{"vet", "{pkg}"},
					Root: []string{"go.mod"}}},
			Format: []Tool{{Name: "gofmt", Bin: "gofmt", Args: []string{"-w", "{file}"}, Mutates: true}},
			Lint:   []Tool{{Name: "golangci-lint", Bin: "golangci-lint", Args: []string{"run", "{dir}"}}, {Name: "staticcheck", Bin: "staticcheck", Args: []string{"{pkg}"}}},
			LSP:    []Tool{{Name: "gopls", Bin: "gopls"}}},
		{ID: "javascript", Frames: jsFrames, Mutation: "c-family", Skips: jsSkips, Name: "JavaScript", Kind: KindProgramming, Extensions: []string{".js", ".mjs", ".cjs", ".jsx"}, Shebangs: []string{"node", "nodejs", "bun", "deno"},
			Definitions: jsDefinitions,
			Tests:       re(`\.(test|spec)\.[cm]?jsx?$`, `(^|/)__tests__/`),
			Check:       &Check{What: "node parse", Tools: []string{"node"}, Args: []string{"--check", "{file}"}, Only: []string{".js", ".mjs", ".cjs"}},
			Format:      []Tool{{Name: "prettier", Bin: "prettier", Args: []string{"--write", "{file}"}, Mutates: true}},
			Lint:        []Tool{{Name: "eslint", Bin: "eslint", Args: []string{"{file}"}}},
			LSP:         []Tool{{Name: "typescript-language-server", Bin: "typescript-language-server", Args: []string{"--stdio"}}},
			Note:        "JSX is not parsed by node; the project's build or tests check it"},
		{ID: "typescript", Frames: jsFrames, Mutation: "c-family", Skips: jsSkips, Name: "TypeScript", Kind: KindProgramming, Extensions: []string{".ts", ".tsx", ".mts", ".cts", ".d.ts"},
			Definitions: append(re(`^(?:export\s+)?(?:declare\s+)?(?:abstract\s+)?(?:interface|type|enum|namespace|module)\s+(?P<name>[\w$]+)`), jsDefinitions...),
			Tests:       re(`\.(test|spec)\.[cm]?tsx?$`, `(^|/)__tests__/`),
			Format:      []Tool{{Name: "prettier", Bin: "prettier", Args: []string{"--write", "{file}"}, Mutates: true}},
			Lint:        []Tool{{Name: "eslint", Bin: "eslint", Args: []string{"{file}"}}},
			LSP:         []Tool{{Name: "typescript-language-server", Bin: "typescript-language-server", Args: []string{"--stdio"}}},
			Diagnostics: re(`^(?P<file>[^\s(]+\.[cm]?tsx?)\((?P<line>\d+),(?P<col>\d+)\):\s*(?P<msg>.*)`),
			Note:        "no per-file check: types need the project (its typecheck or build script, tsc --noEmit)"},
		{ID: "python", Frames: pyFrames, FramesInnermostLast: true, Mutation: "python", Skips: re(`@pytest\.mark\.(?:skip|skipif|xfail)\b`, `\bpytest\.(?:skip|xfail)\(`, `@unittest\.(?:skip|skipIf|skipUnless|expectedFailure)\b`, `\.skipTest\(`, `__test__\s*=\s*False`), Name: "Python", Kind: KindProgramming, Extensions: []string{".py", ".pyi", ".pyw"}, Shebangs: []string{"python", "python3", "python2", "pypy", "pypy3"},
			Definitions: re(`^\s*(?:async\s+)?def\s+(?P<name>\w+)`, `^\s*class\s+(?P<name>\w+)`, `^(?P<name>[A-Z][A-Z0-9_]+)\s*(?::[^=]+)?=`),
			Tests:       re(`(^|/)test_[^/]*\.py$`, `_tests?\.py$`, `(^|/)tests?/.*\.py$`, `(^|/)conftest\.py$`),
			Check:       &Check{What: "python parse", Tools: []string{"python3", "python"}, Args: []string{"-c", pyParse, "{file}"}},
			Format:      []Tool{{Name: "ruff format", Bin: "ruff", Args: []string{"format", "{file}"}, Mutates: true}, {Name: "black", Bin: "black", Args: []string{"-q", "{file}"}, Mutates: true}},
			Lint:        []Tool{{Name: "ruff", Bin: "ruff", Args: []string{"check", "{file}"}}, {Name: "flake8", Bin: "flake8", Args: []string{"{file}"}}, {Name: "pylint", Bin: "pylint", Args: []string{"{file}"}}},
			LSP:         []Tool{{Name: "pyright", Bin: "pyright-langserver", Args: []string{"--stdio"}}, {Name: "pylsp", Bin: "pylsp"}, {Name: "jedi-language-server", Bin: "jedi-language-server"}},
			Diagnostics: re(`File "(?P<file>[^"]+)", line (?P<line>\d+)`)},
		{ID: "rust", Frames: rustFrames, Mutation: "c-family", Skips: re(`#\[ignore\b`, `#\[cfg\(not\(test\)\)\]`), Name: "Rust", Kind: KindProgramming, Extensions: []string{".rs"},
			Definitions: re(`^\s*(?:pub(?:\([^)]*\))?\s+)?(?:default\s+)?(?:const\s+)?(?:async\s+)?(?:unsafe\s+)?(?:extern\s+"[^"]*"\s+)?fn\s+(?P<name>\w+)`,
				`^\s*(?:pub(?:\([^)]*\))?\s+)?(?:struct|enum|trait|union|type|mod)\s+(?P<name>\w+)`,
				`^\s*(?:unsafe\s+)?impl(?:<[^>]*>)?\s+(?:[\w:<>, ]+\s+for\s+)?(?P<name>[\w:]+)`, `^\s*macro_rules!\s*(?P<name>\w+)`),
			Tests:       re(`(^|/)tests/.*\.rs$`),
			Format:      []Tool{{Name: "rustfmt", Bin: "rustfmt", Args: []string{"{file}"}, Mutates: true}},
			Lint:        []Tool{{Name: "clippy", Bin: "cargo", Args: []string{"clippy"}}},
			LSP:         []Tool{{Name: "rust-analyzer", Bin: "rust-analyzer"}},
			Diagnostics: re(`-->\s*(?P<file>[^:\s]+\.rs):(?P<line>\d+):(?P<col>\d+)`),
			Note:        "no per-file check: cargo check (the project's build) is the check"},
		{ID: "java", Frames: jvmFrames, Mutation: "c-family", Skips: re(`@(?:Ignore|Disabled|DisabledIf\w*|EnabledIf\w*)\b`, `\bAssumptions?\.assume\w*\(`, `\bAssume\.assume\w*\(`), Name: "Java", Kind: KindProgramming, Extensions: []string{".java"},
			Definitions: re(`^\s*(?:(?:public|protected|private|static|final|abstract|sealed|non-sealed|strictfp)\s+)*(?:class|interface|enum|record|@interface)\s+(?P<name>\w+)`,
				`^\s+(?:(?:public|protected|private|static|final|abstract|synchronized|native|default)\s+)+(?:<[^>]+>\s+)?[\w<>\[\],.? ]+\s+(?P<name>\w+)\s*\(`),
			Tests:  re(`(^|/)src/test/.*\.java$`, `Tests?\.java$`, `IT\.java$`),
			Format: []Tool{{Name: "google-java-format", Bin: "google-java-format", Args: []string{"-i", "{file}"}, Mutates: true}},
			LSP:    []Tool{{Name: "jdtls", Bin: "jdtls"}},
			Diagnostics: re(`^(?P<file>[^:\s]+\.java):(?P<line>\d+): (?:error|warning): (?P<msg>.*)`,
				`\[(?:ERROR|WARNING)\] (?P<file>[^:\s\[]+\.java):\[(?P<line>\d+),(?P<col>\d+)\] (?P<msg>.*)`),
			Note: "no per-file check: compiling needs the class path (the project's Maven or Gradle build)"},
		{ID: "kotlin", Frames: jvmFrames, Mutation: "c-family", Skips: re(`@(?:Ignore|Disabled)\b`, `\bassume\w*\(`), Name: "Kotlin", Kind: KindProgramming, Extensions: []string{".kt", ".kts"},
			Definitions: re(`^\s*(?:(?:public|private|internal|protected|open|abstract|sealed|data|inline|value|enum|annotation|override|suspend|operator|infix|tailrec|external|const|lateinit)\s+)*(?:class|interface|object|fun|typealias|val|var)\s+(?:<[^>]*>\s*)?(?:[\w.]+\.)?(?P<name>\w+)`),
			Tests:       re(`(^|/)src/test/.*\.kts?$`, `Tests?\.kt$`),
			Lint:        []Tool{{Name: "ktlint", Bin: "ktlint", Args: []string{"{file}"}}},
			LSP:         []Tool{{Name: "kotlin-language-server", Bin: "kotlin-language-server"}},
			Diagnostics: re(`^(?:e|w|error|warning): (?:file://)?(?P<file>[^:\s]+\.kts?):(?P<line>\d+):(?P<col>\d+):? (?P<msg>.*)`),
			Note:        "no per-file check: compiling needs the project's Gradle or Maven build"},
		{ID: "c", Frames: nativeFrames, Mutation: "c-family", Skips: cSkips, Name: "C", Kind: KindProgramming, Extensions: []string{".c", ".h"},
			Definitions: cDefinitions,
			Tests:       re(`(^|/)tests?/.*\.[ch]$`, `_test\.c$`, `(^|/)test_[^/]*\.c$`),
			Format:      []Tool{{Name: "clang-format", Bin: "clang-format", Args: []string{"-i", "{file}"}, Mutates: true}},
			Lint:        []Tool{{Name: "clang-tidy", Bin: "clang-tidy", Args: []string{"{file}"}}, {Name: "cppcheck", Bin: "cppcheck", Args: []string{"{file}"}}},
			LSP:         []Tool{{Name: "clangd", Bin: "clangd"}},
			Note:        "no per-file check: include paths and flags come from the build (CMake, Meson, Make)"},
		{ID: "cpp", Frames: nativeFrames, Mutation: "c-family", Skips: cSkips, Name: "C++", Kind: KindProgramming, Extensions: []string{".cc", ".cpp", ".cxx", ".c++", ".hpp", ".hh", ".hxx", ".ipp", ".tpp", ".cu"},
			Definitions: append(re(`^\s*(?:template\s*<[^>]*>\s*)?(?:class|struct|union|namespace|enum(?:\s+class)?|concept)\s+(?P<name>\w+)`), cDefinitions...),
			Tests:       re(`(^|/)tests?/.*\.(cc|cpp|cxx)$`, `_test\.(cc|cpp|cxx)$`, `(^|/)test_[^/]*\.(cc|cpp)$`),
			Format:      []Tool{{Name: "clang-format", Bin: "clang-format", Args: []string{"-i", "{file}"}, Mutates: true}},
			Lint:        []Tool{{Name: "clang-tidy", Bin: "clang-tidy", Args: []string{"{file}"}}, {Name: "cppcheck", Bin: "cppcheck", Args: []string{"{file}"}}},
			LSP:         []Tool{{Name: "clangd", Bin: "clangd"}},
			Note:        "no per-file check: include paths and flags come from the build (CMake, Meson, Make)"},
		{ID: "csharp", Frames: csFrames, Mutation: "c-family", Skips: re(`\[(?:Ignore|Explicit)\b`, `\bSkip\s*=`, `Assert\.Inconclusive\(`), Name: "C#", Kind: KindProgramming, Extensions: []string{".cs", ".csx"},
			Definitions: re(`^\s*(?:(?:public|private|protected|internal|static|sealed|abstract|partial|readonly|ref|unsafe|file|new)\s+)*(?:class|struct|interface|enum|record(?:\s+struct|\s+class)?|namespace)\s+(?P<name>[\w.]+)`,
				`^\s+(?:(?:public|private|protected|internal|static|virtual|override|abstract|async|sealed|extern|partial|new|unsafe)\s+)+[\w<>\[\],.? ]+\s+(?P<name>\w+)\s*[(<]`),
			Tests:       re(`(^|/)[^/]*\.Tests?/.*\.cs$`, `Tests?\.cs$`),
			Format:      []Tool{{Name: "csharpier", Bin: "csharpier", Args: []string{"format", "{file}"}, Mutates: true}},
			LSP:         []Tool{{Name: "csharp-ls", Bin: "csharp-ls"}, {Name: "OmniSharp", Bin: "OmniSharp"}},
			Diagnostics: re(`^(?P<file>[^(\s]+\.cs)\((?P<line>\d+),(?P<col>\d+)\): (?:error|warning) \w+: (?P<msg>.*)`),
			Note:        "no per-file check: dotnet build is the check"},
		{ID: "fsharp", Name: "F#", Kind: KindProgramming, Extensions: []string{".fs", ".fsi", ".fsx"},
			Definitions: re(`^\s*(?:let|member|type|module)\s+(?:rec\s+|inline\s+|private\s+)*(?P<name>[\w.]+)`)},
		{ID: "php", Frames: phpFrames, Mutation: "c-family-hash", Skips: re(`markTest(?:Skipped|Incomplete)\(`, `@group\s+(?:skip|disabled)`, `#\[Group\(`), Name: "PHP", Kind: KindProgramming, Extensions: []string{".php", ".phtml"}, Shebangs: []string{"php"},
			Definitions: re(`^\s*(?:(?:abstract|final|readonly)\s+)*(?:class|interface|trait|enum)\s+(?P<name>\w+)`,
				`^\s*(?:(?:public|private|protected|static|abstract|final)\s+)*function\s+&?(?P<name>\w+)`),
			Tests:       re(`Test\.php$`, `(^|/)tests?/.*\.php$`),
			Check:       &Check{What: "php lint", Tools: []string{"php"}, Args: []string{"-l", "{file}"}},
			Lint:        []Tool{{Name: "phpstan", Bin: "phpstan", Args: []string{"analyse", "{file}"}}, {Name: "psalm", Bin: "psalm", Args: []string{"{file}"}}},
			Format:      []Tool{{Name: "php-cs-fixer", Bin: "php-cs-fixer", Args: []string{"fix", "{file}"}, Mutates: true}},
			LSP:         []Tool{{Name: "intelephense", Bin: "intelephense", Args: []string{"--stdio"}}, {Name: "phpactor", Bin: "phpactor", Args: []string{"language-server"}}},
			Diagnostics: re(`in (?P<file>\S+\.php) on line (?P<line>\d+)`)},
		{ID: "ruby", Frames: rubyFrames, Skips: re(`^\s*skip\b`, `^\s*x(?:it|describe|context|specify)\b`, `^\s*pending\b`), Name: "Ruby", Kind: KindProgramming, Extensions: []string{".rb", ".rake", ".gemspec", ".ru"}, Filenames: []string{"Rakefile", "Gemfile", "Guardfile", "Podfile"},
			Shebangs:    []string{"ruby"},
			Definitions: re(`^\s*def\s+(?:self\.)?(?P<name>[\w?!=]+)`, `^\s*(?:class|module)\s+(?P<name>[\w:]+)`),
			Tests:       re(`_test\.rb$`, `_spec\.rb$`, `(^|/)(test|spec)/.*\.rb$`),
			Check:       &Check{What: "ruby parse", Tools: []string{"ruby"}, Args: []string{"-c", "{file}"}},
			Lint:        []Tool{{Name: "rubocop", Bin: "rubocop", Args: []string{"{file}"}}},
			LSP:         []Tool{{Name: "ruby-lsp", Bin: "ruby-lsp"}, {Name: "solargraph", Bin: "solargraph", Args: []string{"stdio"}}}},
		{ID: "swift", Frames: swiftFrames, Mutation: "c-family", Skips: re(`\bXCTSkip\w*\(`, `\.disabled\(`), Name: "Swift", Kind: KindProgramming, Extensions: []string{".swift"},
			Definitions: re(`^\s*(?:(?:public|private|fileprivate|internal|open|final|static|class|override|mutating|nonisolated|@\w+(?:\([^)]*\))?)\s+)*(?:func|class|struct|enum|protocol|extension|actor|typealias)\s+(?P<name>[\w.]+)`),
			Tests:       re(`(^|/)Tests/.*\.swift$`, `Tests?\.swift$`),
			Format:      []Tool{{Name: "swift-format", Bin: "swift-format", Args: []string{"-i", "{file}"}, Mutates: true}},
			LSP:         []Tool{{Name: "sourcekit-lsp", Bin: "sourcekit-lsp"}},
			Note:        "no per-file check: swift build is the check"},
		{ID: "dart", Frames: dartFrames, Mutation: "c-family", Skips: re(`\bskip:\s*(?:true|\x27|")`, `@Skip\(`), Name: "Dart", Kind: KindProgramming, Extensions: []string{".dart"},
			Definitions: re(`^\s*(?:(?:abstract|sealed|base|final|interface|mixin)\s+)*(?:class|mixin|enum|extension|typedef)\s+(?P<name>\w+)`,
				`^(?:[\w<>?,]+\s+)?(?P<name>[a-z_]\w*)\s*\([^;]*\)\s*(?:async\*?\s*|sync\*\s*)?(?:\{|=>)`),
			Tests:  re(`_test\.dart$`),
			Format: []Tool{{Name: "dart format", Bin: "dart", Args: []string{"format", "{file}"}, Mutates: true}},
			LSP:    []Tool{{Name: "dart language-server", Bin: "dart", Args: []string{"language-server"}}},
			Note:   "no per-file check: dart analyze (needs the project's packages) is the check"},
		{ID: "shell", Skips: re(`^\s*skip\b`), Name: "shell", Kind: KindProgramming, Extensions: []string{".sh", ".bash", ".ksh", ".bats"}, Shebangs: []string{"sh", "bash", "dash", "ksh", "ash"},
			Definitions: re(`^\s*(?:function\s+)?(?P<name>[\w.:-]+)\s*\(\)\s*\{?`, `^\s*function\s+(?P<name>[\w.:-]+)`),
			Tests:       re(`\.bats$`, `(^|/)tests?/.*\.sh$`),
			Check:       &Check{What: "bash parse", Tools: []string{"bash", "sh"}, Args: []string{"-n", "{file}"}},
			Lint:        []Tool{{Name: "shellcheck", Bin: "shellcheck", Args: []string{"{file}"}}},
			Format:      []Tool{{Name: "shfmt", Bin: "shfmt", Args: []string{"-w", "{file}"}, Mutates: true}},
			LSP:         []Tool{{Name: "bash-language-server", Bin: "bash-language-server", Args: []string{"start"}}},
			Diagnostics: re(`^(?P<file>[^:\s]+): line (?P<line>\d+): (?P<msg>.*)`, `^In (?P<file>\S+) line (?P<line>\d+):`)},
		{ID: "zsh", Name: "zsh", Kind: KindProgramming, Extensions: []string{".zsh"}, Shebangs: []string{"zsh"},
			Definitions: re(`^\s*(?:function\s+)?(?P<name>[\w.:-]+)\s*\(\)\s*\{?`, `^\s*function\s+(?P<name>[\w.:-]+)`),
			Check:       &Check{What: "zsh parse", Tools: []string{"zsh"}, Args: []string{"-n", "{file}"}}},
		{ID: "powershell", Name: "PowerShell", Kind: KindProgramming, Extensions: []string{".ps1", ".psm1", ".psd1"}, Shebangs: []string{"pwsh"},
			Definitions: re(`(?i)^\s*function\s+(?P<name>[\w-]+)`, `(?i)^\s*class\s+(?P<name>\w+)`)},
		{ID: "sql", Name: "SQL", Kind: KindData, Extensions: []string{".sql", ".psql", ".mysql", ".pgsql"},
			Definitions: re(`(?i)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:TEMP(?:ORARY)?\s+|UNLOGGED\s+)?(?:TABLE|VIEW|MATERIALIZED\s+VIEW|FUNCTION|PROCEDURE|(?:UNIQUE\s+)?INDEX|TRIGGER|TYPE|SCHEMA|SEQUENCE|DOMAIN|EXTENSION)\s+(?:IF\s+NOT\s+EXISTS\s+)?(?P<name>[\w."`+"`"+`\[\]]+)`,
				`(?i)^\s*ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?(?P<name>[\w."`+"`"+`\[\]]+)`),
			Lint: []Tool{{Name: "sqlfluff", Bin: "sqlfluff", Args: []string{"lint", "{file}"}}},
			LSP:  []Tool{{Name: "sqls", Bin: "sqls"}},
			Note: "no per-file check: SQL dialects differ; the project's migrations or tests are the check"},
		{ID: "terraform", Name: "Terraform/HCL", Kind: KindInfra, Extensions: []string{".tf", ".tfvars", ".hcl", ".tftest.hcl"},
			Definitions: re(`^(?:resource|data|ephemeral)\s+"[^"]+"\s+"(?P<name>[^"]+)"`, `^(?:module|variable|output|provider|check)\s+"(?P<name>[^"]+)"`),
			Format:      []Tool{{Name: "terraform fmt", Bin: "terraform", Args: []string{"fmt", "{file}"}, Mutates: true}},
			Lint:        []Tool{{Name: "tflint", Bin: "tflint"}},
			LSP:         []Tool{{Name: "terraform-ls", Bin: "terraform-ls", Args: []string{"serve"}}},
			Note:        "no per-file check: terraform validate needs terraform init"},
		{ID: "dockerfile", Name: "Dockerfile", Kind: KindInfra, Filenames: []string{"Dockerfile", "Containerfile"}, Extensions: []string{".dockerfile"},
			Definitions: re(`(?i)^FROM\s+\S+(?:\s+AS\s+(?P<name>\S+))?`),
			Lint:        []Tool{{Name: "hadolint", Bin: "hadolint", Args: []string{"{file}"}}}},
		{ID: "yaml", Name: "YAML", Kind: KindData, Extensions: []string{".yaml", ".yml"},
			Definitions: re(`^(?P<name>[A-Za-z_][\w.-]*):(?:\s|$)`),
			Check:       &Check{What: "yaml", InProcess: yamlParse},
			Lint:        []Tool{{Name: "yamllint", Bin: "yamllint", Args: []string{"{file}"}}},
			LSP:         []Tool{{Name: "yaml-language-server", Bin: "yaml-language-server", Args: []string{"--stdio"}}}},
		{ID: "json", Name: "JSON", Kind: KindData, Extensions: []string{".json", ".jsonl", ".geojson", ".webmanifest"},
			Check: &Check{What: "json", InProcess: jsonParse}},
		{ID: "jsonc", Name: "JSON with comments", Kind: KindData, Extensions: []string{".jsonc", ".json5"},
			Filenames: []string{"tsconfig.json", "jsconfig.json", "devcontainer.json", ".eslintrc.json", "settings.json", "launch.json",
				"tasks.json", "extensions.json", "tsconfig.base.json", "tsconfig.build.json", "tslint.json", ".babelrc", "deno.json"}},
		{ID: "toml", Name: "TOML", Kind: KindData, Extensions: []string{".toml"}, Filenames: []string{"Cargo.lock", "poetry.lock", "uv.lock"},
			Definitions: re(`^\[\[?(?P<name>[^\]]+)\]\]?\s*$`)},
		{ID: "xml", Name: "XML", Kind: KindData, Extensions: []string{".xml", ".csproj", ".fsproj", ".vbproj", ".props", ".targets", ".xsd", ".xsl", ".xslt", ".plist", ".resx", ".nuspec"},
			Check: &Check{What: "xml", InProcess: xmlParse}},
		{ID: "markdown", Name: "Markdown", Kind: KindMarkup, Extensions: []string{".md", ".markdown", ".mdx"},
			Definitions: re(`^(?P<name>#{1,3} \S.*)`)},
		{ID: "html", Name: "HTML", Kind: KindMarkup, Extensions: []string{".html", ".htm", ".xhtml"}},
		{ID: "css", Name: "CSS", Kind: KindMarkup, Extensions: []string{".css", ".scss", ".sass", ".less"}},
		{ID: "vue", Name: "Vue", Kind: KindProgramming, Extensions: []string{".vue"}},
		{ID: "svelte", Name: "Svelte", Kind: KindProgramming, Extensions: []string{".svelte"}},
		{ID: "protobuf", Name: "Protocol Buffers", Kind: KindData, Extensions: []string{".proto"},
			Definitions: re(`^\s*(?:message|service|enum|rpc|extend)\s+(?P<name>\w+)`)},
		{ID: "graphql", Name: "GraphQL", Kind: KindData, Extensions: []string{".graphql", ".gql", ".graphqls"},
			Definitions: re(`^\s*(?:extend\s+)?(?:type|interface|enum|input|union|scalar|schema|directive)\s+@?(?P<name>\w+)`)},
		{ID: "make", Name: "Make", Kind: KindInfra, Filenames: []string{"Makefile", "GNUmakefile", "makefile"}, Extensions: []string{".mk", ".mak"},
			Definitions: re(`^(?P<name>[A-Za-z0-9_./%-]+)\s*:(?:[^=]|$)`)},
		{ID: "cmake", Name: "CMake", Kind: KindInfra, Filenames: []string{"CMakeLists.txt"}, Extensions: []string{".cmake"},
			Definitions: re(`(?i)^\s*(?:function|macro)\s*\(\s*(?P<name>\w+)`, `(?i)^\s*add_(?:library|executable)\s*\(\s*(?P<name>[\w.-]+)`)},
		{ID: "groovy", Frames: jvmFrames, Mutation: "c-family", Name: "Groovy", Kind: KindProgramming, Extensions: []string{".groovy", ".gradle", ".gvy"},
			Definitions: re(`^\s*(?:(?:public|private|protected|static|final|abstract|def)\s+)*(?:class|interface|trait|enum)\s+(?P<name>\w+)`, `^\s*def\s+(?P<name>\w+)\s*\(`)},
		{ID: "scala", Frames: jvmFrames, Mutation: "c-family", Name: "Scala", Kind: KindProgramming, Extensions: []string{".scala", ".sc", ".sbt"},
			Definitions: re(`^\s*(?:(?:private|protected|final|sealed|abstract|implicit|lazy|override|case)\s+)*(?:class|object|trait|enum|def|val|type|given)\s+(?P<name>\w+)`),
			Tests:       re(`(^|/)src/test/`, `(Spec|Test|Suite)\.scala$`)},
		{ID: "elixir", Frames: elixirFrames, Skips: re(`@tag\s+:skip`, `@moduletag\s+:skip`), Name: "Elixir", Kind: KindProgramming, Extensions: []string{".ex", ".exs"}, Shebangs: []string{"elixir"},
			Definitions: re(`^\s*(?:defmodule|def|defp|defmacro|defmacrop|defprotocol|defimpl|defstruct|defguard)\s+(?P<name>[\w.?!]+)`),
			Tests:       re(`_test\.exs$`)},
		{ID: "erlang", Name: "Erlang", Kind: KindProgramming, Extensions: []string{".erl", ".hrl"}, Shebangs: []string{"escript"},
			Definitions: re(`^(?P<name>[a-z]\w*)\s*\(`), Tests: re(`_SUITE\.erl$`, `_tests\.erl$`)},
		{ID: "haskell", Name: "Haskell", Kind: KindProgramming, Extensions: []string{".hs", ".lhs"},
			Definitions: re(`^(?:data|newtype|type|class|instance)\s+(?:\([^)]*\)\s*=>\s*)?(?P<name>[A-Z]\w*)`, `^(?P<name>[a-z_]\w*)\s*::`)},
		{ID: "ocaml", Name: "OCaml", Kind: KindProgramming, Extensions: []string{".ml", ".mli"},
			Definitions: re(`^\s*(?:let(?:\s+rec)?|type|module|class|exception)\s+(?P<name>\w+)`)},
		{ID: "clojure", Name: "Clojure", Kind: KindProgramming, Extensions: []string{".clj", ".cljs", ".cljc", ".edn"},
			Definitions: re(`^\s*\((?:defn-?|def|defmacro|defprotocol|defrecord|deftype|defmulti|ns)\s+(?P<name>[\w.*+!?<>=/-]+)`)},
		{ID: "lua", Name: "Lua", Kind: KindProgramming, Extensions: []string{".lua"}, Shebangs: []string{"lua", "luajit"},
			Definitions: re(`^\s*(?:local\s+)?function\s+(?P<name>[\w.:]+)`, `^\s*(?:local\s+)?(?P<name>[\w.]+)\s*=\s*function\b`)},
		{ID: "perl", Name: "Perl", Kind: KindProgramming, Extensions: []string{".pl", ".pm", ".t"}, Shebangs: []string{"perl"},
			Definitions: re(`^\s*sub\s+(?P<name>\w+)`, `^\s*package\s+(?P<name>[\w:]+)`)},
		{ID: "r", Name: "R", Kind: KindProgramming, Extensions: []string{".r", ".R", ".rmd"}, Shebangs: []string{"Rscript"},
			Definitions: re(`^(?P<name>[\w.]+)\s*(?:<-|=)\s*function\b`)},
		{ID: "julia", Name: "Julia", Kind: KindProgramming, Extensions: []string{".jl"}, Shebangs: []string{"julia"},
			Definitions: re(`^\s*(?:function|macro|struct|mutable\s+struct|abstract\s+type|module)\s+(?P<name>[\w.!]+)`)},
		{ID: "zig", Name: "Zig", Kind: KindProgramming, Extensions: []string{".zig"},
			Definitions: re(`^\s*(?:pub\s+)?(?:export\s+)?(?:inline\s+)?fn\s+(?P<name>\w+)`, `^\s*(?:pub\s+)?const\s+(?P<name>\w+)\s*=\s*(?:struct|enum|union)`)},
		{ID: "nix", Name: "Nix", Kind: KindInfra, Extensions: []string{".nix"}},
		{ID: "objc", Name: "Objective-C", Kind: KindProgramming, Extensions: []string{".mm"},
			Definitions: re(`^@(?:interface|implementation|protocol)\s+(?P<name>\w+)`, `^[-+]\s*\([^)]*\)\s*(?P<name>\w+)`)},
		{ID: "solidity", Name: "Solidity", Kind: KindProgramming, Extensions: []string{".sol"},
			Definitions: re(`^\s*(?:abstract\s+)?(?:contract|library|interface|function|modifier|event|struct|enum)\s+(?P<name>\w+)`)},
	}
}

var jsDefinitions = re(
	`^(?:export\s+)?(?:default\s+)?(?:async\s+)?function\*?\s+(?P<name>[\w$]+)`,
	`^(?:export\s+)?(?:default\s+)?(?:abstract\s+)?class\s+(?P<name>[\w$]+)`,
	`^(?:export\s+)?(?:const|let|var)\s+(?P<name>[\w$]+)\s*(?::[^=]+)?=\s*(?:async\s+)?(?:function\b|\([^)]*\)\s*(?::[^=]+)?=>|[\w$]+\s*=>)`,
	`^\s{2,4}(?:(?:static|async|get|set|public|private|protected|readonly|override)\s+)*\*?(?P<name>#?[\w$]+)\s*\([^)]*\)\s*(?::\s*[^{]+)?\{\s*$`,
)

var jsSkips = re(`\b(?:it|test|describe|context|suite)\.(?:skip|todo|only)\(`, `\bx(?:it|test|describe)\(`, `\bf(?:it|describe)\(`, `\btest\.(?:skipIf|fails)\(`)

var cSkips = re(`\bGTEST_SKIP\(`, `\bDISABLED_\w+`, `\bSKIP\(`)

var cDefinitions = re(
	`^(?:typedef\s+)?(?:struct|union|enum)\s+(?P<name>\w+)`,
	`^#\s*define\s+(?P<name>\w+)`,
	`^(?:[A-Za-z_][\w:<>,]*[\s*&]+)+(?P<name>[A-Za-z_~][\w:~]*)\s*\([^;]*$`,
	// A one-line function: the body follows the signature.
	`^(?:[A-Za-z_][\w:<>,]*[\s*&]+)+(?P<name>[A-Za-z_~][\w:~]*)\s*\([^;{]*\)\s*(?:const\s*)?\{`,
	// A constructor or destructor defined outside its class: no return type.
	`^(?P<name>[A-Za-z_]\w*::~?[A-Za-z_]\w*)\s*\(`,
)

// keywords are never definitions (a method pattern also matches "if (x) {").
var keywords = map[string]bool{"if": true, "for": true, "while": true, "switch": true, "catch": true, "return": true, "else": true,
	"do": true, "try": true, "function": true, "new": true, "delete": true, "sizeof": true, "case": true, "throw": true, "with": true}

func goParse(name string, src []byte) error {
	_, err := parser.ParseFile(token.NewFileSet(), name, src, parser.AllErrors|parser.SkipObjectResolution)
	return err
}

func yamlParse(_ string, src []byte) error {
	dec := yaml.NewDecoder(bytes.NewReader(src))
	for {
		var v any
		err := dec.Decode(&v)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func jsonParse(name string, src []byte) error {
	if len(bytes.TrimSpace(src)) == 0 {
		return errors.New("empty, not JSON")
	}
	if strings.HasSuffix(name, ".jsonl") {
		for i, line := range bytes.Split(src, []byte("\n")) {
			if len(bytes.TrimSpace(line)) > 0 && !json.Valid(line) {
				return fmt.Errorf("line %d is not valid JSON", i+1)
			}
		}
		return nil
	}
	var v any
	return json.Unmarshal(src, &v)
}

func xmlParse(_ string, src []byte) error {
	d := xml.NewDecoder(bytes.NewReader(src))
	d.Strict = true
	for {
		_, err := d.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// goOutline lists the lines of top-level declarations (each spec of a
// grouped declaration) in Go source; nil when it does not parse.
func goOutline(src string) []int {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if err != nil || f == nil {
		return nil
	}
	var out []int
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			out = append(out, fset.Position(d.Pos()).Line)
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				continue
			}
			if !d.Lparen.IsValid() {
				out = append(out, fset.Position(d.Pos()).Line)
				continue
			}
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					out = append(out, fset.Position(s.Pos()).Line)
				case *ast.ValueSpec:
					out = append(out, fset.Position(s.Pos()).Line)
				}
			}
		}
	}
	return out
}
