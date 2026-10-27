package ecosystem

import (
	"path"
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
