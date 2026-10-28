package benchmark

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGoldOf: gold is the existing code files a fix changes, with the
// definitions around the changed lines; new files and prose are not.
func TestGoldOf(t *testing.T) {
	root := t.TempDir()
	for p, s := range map[string]string{
		"pkg/cart.go":  "package pkg\n\nfunc Total() int {\n\treturn 1\n}\n\nfunc Other() {}\n",
		"CHANGELOG.md": "# Changes\n",
	} {
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, p), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	patch := "diff --git a/pkg/cart.go b/pkg/cart.go\n--- a/pkg/cart.go\n+++ b/pkg/cart.go\n@@ -3,3 +3,3 @@ func Total() int {\n func Total() int {\n-\treturn 1\n+\treturn 2\n }\n" +
		"diff --git a/CHANGELOG.md b/CHANGELOG.md\n--- a/CHANGELOG.md\n+++ b/CHANGELOG.md\n@@ -1 +1,2 @@\n # Changes\n+- fix\n" +
		"diff --git a/pkg/new.go b/pkg/new.go\nnew file mode 100644\n--- /dev/null\n+++ b/pkg/new.go\n@@ -0,0 +1 @@\n+package pkg\n"
	files, syms := goldOf(root, patch)
	if strings.Join(files, ",") != "pkg/cart.go" || strings.Join(syms, ",") != "pkg/cart.go#Total" {
		t.Fatalf("gold %v %v", files, syms)
	}
}

// TestSeedPackFiles reads files and definitions out of the basic block as
// the model sees it.
func TestSeedPackFiles(t *testing.T) {
	block := "Likely relevant places, found by searching the project for the names in the request (verify before relying on them):\n" +
		"- Post: internal/ledger/ledger.go:4 (func (l *Ledger) Post(entries ...Entry) error {), internal/ledger/ledger_test.go:3\n" +
		"- internal/ledger/ledger.go: 120 bytes\n" +
		"- cart.go: internal/cart/cart.go, web/cart.go\n"
	files, syms := seedPackFiles(block)
	if strings.Join(files, ",") != "internal/ledger/ledger.go,internal/ledger/ledger_test.go,internal/cart/cart.go,web/cart.go" {
		t.Fatalf("files %v", files)
	}
	if strings.Join(syms, ",") != "internal/ledger/ledger.go#Post" {
		t.Fatalf("symbols %v", syms)
	}
}

func TestScoreArm(t *testing.T) {
	a := RetrievalArm{Files: []string{"x.go", "a.go", "y.go", "b.go"}, Symbols: []string{"a.go#F", "x.go#G"}}
	scoreArm(&a, []string{"a.go", "b.go", "c.go"}, []string{"a.go#F"})
	if math.Abs(a.Recall-2.0/3) > 1e-9 || a.Precision != 0.5 || a.Hit1 || !a.Hit3 || a.SymbolsRight != 1 {
		t.Fatalf("%+v", a)
	}
}

func TestSignTest(t *testing.T) {
	if p := signTest(0, 0); p != 1 {
		t.Fatal(p)
	}
	if p := signTest(10, 0); math.Abs(p-2.0/1024) > 1e-12 {
		t.Fatal(p)
	}
	if p := signTest(5, 5); p != 1 {
		t.Fatal(p)
	}
}
