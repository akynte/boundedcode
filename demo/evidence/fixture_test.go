// Package evidence holds the controlled fixture of the evidence demo
// (demo/evidence/README.md). These tests prove the fixture's premises
// without BoundedCode or a model:
//
//   - the shop's own tests pass on the buggy code;
//   - the bug is real: 10 items are not discounted;
//   - a regression test for it fails on the original code and passes with
//     the one-line reference fix, which keeps the existing tests passing.
package evidence

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// shop copies the fixture into a fresh directory and lays over the named
// reference files.
func shop(t *testing.T, reference ...string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("testdata/shop")); err != nil {
		t.Fatal(err)
	}
	for _, f := range reference {
		b, err := os.ReadFile(filepath.Join("testdata/reference", f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func goCmd(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off", "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestFixturePremises(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil || testing.Short() {
		t.Skip("go toolchain required")
	}
	t.Run("existing tests pass on the buggy code", func(t *testing.T) {
		if out, err := goCmd(t, shop(t), "test", "-count=1", "./..."); err != nil {
			t.Fatalf("go test: %v\n%s", err, out)
		}
	})
	t.Run("the bug is observable", func(t *testing.T) {
		out, err := goCmd(t, shop(t), "run", "./cmd/quote", "10")
		if err != nil || !strings.Contains(out, "total 1000 cents") {
			t.Fatalf("quote 10 = %q, %v (want the undiscounted 1000)", out, err)
		}
	})
	t.Run("regression test fails on the original code", func(t *testing.T) {
		out, err := goCmd(t, shop(t, "bulk_test.go"), "test", "-count=1", "./...")
		if err == nil || !strings.Contains(out, "--- FAIL: TestTotalBulkDiscountAtExactlyTen") || !strings.Contains(out, "Total = 1000, want 900") {
			t.Fatalf("want exactly the regression test to fail:\n%s", out)
		}
		if strings.Contains(out, "--- FAIL: TestTotalSmallCart") || strings.Contains(out, "--- FAIL: TestTotalLargeCart") {
			t.Fatalf("an existing test failed:\n%s", out)
		}
	})
	t.Run("regression test and existing tests pass with the reference fix", func(t *testing.T) {
		dir := shop(t, "bulk_test.go", "cart.go")
		if out, err := goCmd(t, dir, "test", "-count=1", "-v", "./..."); err != nil || !strings.Contains(out, "--- PASS: TestTotalBulkDiscountAtExactlyTen") {
			t.Fatalf("go test: %v\n%s", err, out)
		}
		if out, _ := goCmd(t, dir, "run", "./cmd/quote", "10"); !strings.Contains(out, "total 900 cents") {
			t.Fatalf("quote 10 with the fix = %q", out)
		}
	})
}
