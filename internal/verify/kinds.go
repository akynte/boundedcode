package verify

import "strings"

// Kinds of stage, by the verification layer they serve.
const (
	KindSyntax = "syntax" // Layer 1: formatting and structure (gofmt)
	KindBuild  = "build"  // Layer 2: compile and type-check (go build, go vet, tsc, cargo build, mvn compile)
	KindTest   = "test"   // Layer 3: the repository's tests
	KindLint   = "lint"   // static analysis beyond types (golangci-lint, eslint)
	KindOther  = "other"
)

// StageKind returns the layer a stage serves: its kind when set, the test
// flag, else what its name says. A repository's own stages are classified
// like the built-in ones.
func StageKind(st Stage) string {
	if st.Kind != "" {
		return st.Kind
	}
	if isTestStage(st) {
		return KindTest
	}
	n := strings.ToLower(st.Name)
	if i := strings.LastIndex(n, "/"); i >= 0 {
		n = n[i+1:] // a subproject's stage: api/go-build
	}
	switch {
	case strings.Contains(n, "fmt") || strings.Contains(n, "format"):
		return KindSyntax
	case strings.Contains(n, "lint") || strings.Contains(n, "clippy"):
		return KindLint
	case strings.Contains(n, "build") || strings.Contains(n, "compile") || strings.Contains(n, "vet") || n == "tsc" ||
		strings.Contains(n, "typecheck") || strings.Contains(n, "check"):
		return KindBuild
	}
	return KindOther
}
