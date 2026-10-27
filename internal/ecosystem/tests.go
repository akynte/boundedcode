package ecosystem

import (
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// IsTest reports whether rel (slash-separated, relative to a project or
// repository root) is test code by its language's conventions. Go's rule
// is exactly the toolchain's (a _test.go file); other languages go by the
// file names and test directories their usual runners collect. Files in
// languages without a convention here are not tests.
func IsTest(rel string) bool {
	base := path.Base(rel)
	ext := strings.ToLower(path.Ext(base))
	stem := strings.TrimSuffix(base, path.Ext(base))
	dirs := strings.Split(path.Dir(rel), "/")
	inDir := func(names ...string) bool {
		for _, d := range dirs {
			for _, n := range names {
				if d == n {
					return true
				}
			}
		}
		return false
	}
	switch ext {
	case ".go":
		return strings.HasSuffix(base, "_test.go")
	case ".py":
		return strings.HasPrefix(base, "test_") || strings.HasSuffix(stem, "_test") || base == "conftest.py" || inDir("tests", "test")
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts":
		return strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") || inDir("__tests__", "test", "tests")
	case ".java", ".kt", ".kts", ".scala", ".groovy":
		return inSequence(dirs, "src", "test") || hasAnySuffix(stem, "Test", "Tests", "IT", "Spec", "Suite")
	case ".rs":
		return inDir("tests") || base == "tests.rs"
	case ".rb":
		return strings.HasSuffix(stem, "_spec") || strings.HasSuffix(stem, "_test") || strings.HasPrefix(base, "test_") || inDir("spec", "test")
	case ".php":
		return strings.HasSuffix(stem, "Test") || inDir("tests")
	case ".cs", ".fs", ".vb":
		if hasAnySuffix(stem, "Test", "Tests") {
			return true
		}
		for _, d := range dirs {
			if hasAnySuffix(d, ".Tests", ".Test", ".UnitTests", ".IntegrationTests") || d == "tests" {
				return true
			}
		}
		return false
	case ".swift":
		return hasAnySuffix(stem, "Tests", "Test") || inDir("Tests")
	case ".ex", ".exs":
		return strings.HasSuffix(stem, "_test") || inDir("test")
	case ".dart":
		return strings.HasSuffix(stem, "_test") || inDir("test")
	case ".c", ".cc", ".cpp", ".cxx", ".h", ".hpp":
		return strings.HasPrefix(base, "test_") || hasAnySuffix(stem, "_test", "_unittest", "_tests") || inDir("test", "tests")
	case ".bats":
		return true
	}
	return false
}

func hasAnySuffix(s string, suffixes ...string) bool {
	for _, x := range suffixes {
		if strings.HasSuffix(s, x) {
			return true
		}
	}
	return false
}

// inSequence reports a directory a directly followed by b.
func inSequence(dirs []string, a, b string) bool {
	for i := 0; i+1 < len(dirs); i++ {
		if dirs[i] == a && dirs[i+1] == b {
			return true
		}
	}
	return false
}

// testFileRE matches the test file naming conventions of the languages
// verification has presets for (and a few more).
var testFileRE = regexp.MustCompile(`(^|/)(` +
	`[^/]*(_test\.go|\.(test|spec)\.[cm]?[jt]sx?)` + // Go, JavaScript/TypeScript
	`|test_[^/]*\.py|[^/]*_test\.py|tests\.py|conftest\.py` + // Python
	`|[^/]*(Test|Tests|IT|TestCase|Spec|Suite)\.(java|kt|kts|scala|groovy)` + // JVM
	`|[^/]*_(test|spec)\.rb|test_[^/]*\.rb` + // Ruby
	`|[^/]*Test\.php` + // PHP
	`|[^/]*Tests?\.(cs|fs|vb|swift)` + // .NET, Swift
	`|[^/]*_(test|unittest)\.(c|cc|cpp|cxx)|test_[^/]*\.(c|cc|cpp|cxx)` + // C, C++
	`|[^/]*_test\.(exs|dart)|[^/]*Spec\.hs` + // Elixir, Dart, Haskell
	`)$`)

// HasTestName reports a conventional test file name in any language (the
// name alone, whatever its directory).
func HasTestName(p string) bool { return testFileRE.MatchString(p) }

// IsTestOrData reports whether a path is test code or test data: test code
// by IsTest, a conventional test file name in any language, or anything
// under a directory that marks tests (test, tests, testdata, spec,
// __tests__, fixtures, or a name such as caddytest, test_utils or
// e2e-tests: test data and helpers). A directory name is split on "-", "_"
// and "."; a part marks tests when it starts or ends with "test" and is not
// an ordinary word that happens to (latest, contest, ...). Verification uses
// it: a change to any of these is a change to the tests.
func IsTestOrData(p string) bool {
	if IsTest(filepath.ToSlash(p)) || testFileRE.MatchString(p) {
		return true
	}
	parts := strings.Split(filepath.ToSlash(p), "/")
	for _, d := range parts[:len(parts)-1] {
		if testDirName(d) {
			return true
		}
	}
	return false
}

// IsDocumentation reports prose: Markdown, reStructuredText, AsciiDoc and
// plain text files, and the conventional licence and changelog files.
func IsDocumentation(p string) bool {
	base := strings.ToLower(path.Base(filepath.ToSlash(p)))
	switch path.Ext(base) {
	case ".md", ".markdown", ".rst", ".adoc", ".txt":
		return true
	}
	for _, n := range []string{"license", "licence", "notice", "changelog", "authors", "contributors"} {
		if base == n || strings.HasPrefix(base, n+".") {
			return true
		}
	}
	return false
}

// notTestWords end (or start) with "test" but name no tests.
var notTestWords = map[string]bool{
	"latest": true, "greatest": true, "contest": true, "contests": true, "protest": true,
	"attest": true, "attests": true, "detest": true, "testament": true, "testimony": true,
	"fastest": true, "shortest": true, "smartest": true, "strictest": true, "lightest": true,
	"tightest": true, "brightest": true, "softest": true, "hottest": true, "slightest": true,
}

func testDirName(d string) bool {
	l := strings.ToLower(d)
	switch l {
	case "spec", "specs", "__tests__", "__mocks__", "e2e", "fixtures":
		return true
	}
	for _, part := range strings.FieldsFunc(l, func(r rune) bool { return r == '-' || r == '_' || r == '.' }) {
		if notTestWords[part] {
			continue
		}
		if strings.HasPrefix(part, "test") || strings.HasSuffix(part, "test") || strings.HasSuffix(part, "tests") {
			return true
		}
	}
	return false
}
