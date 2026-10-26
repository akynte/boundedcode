package benchmark

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLeaseStudyScoresCommits(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	write := func(p, s string) {
		t.Helper()
		_ = os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755)
		if err := os.WriteFile(filepath.Join(dir, p), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	write("shop/cart/cart.go", "package cart\n\nfunc AlphaThing() int { return 1 }\n\nfunc Other() int { return 2 }\n")
	write("shop/api/api.go", "package api\n\nfunc Serve() {}\n")
	write("shop/cart/data.txtar", "-- a.txt --\nA\n-- b.txt --\nB\n")
	git("add", "-A")
	git("commit", "-qm", "initial")
	// A change the message names, a deleted txtar marker (a removed line
	// that starts with "--- "), and a test file (not scored).
	write("shop/cart/cart.go", "package cart\n\nfunc AlphaThing() int { return 2 }\n\nfunc Other() int { return 2 }\n")
	write("shop/cart/data.txtar", "-- a.txt --\nA\n")
	write("shop/cart/cart_test.go", "package cart\n")
	git("add", "-A")
	git("commit", "-qm", "shop/cart: make AlphaThing return 2\n\nChange-Id: I0123\nSigned-off-by: t <t@t>")
	// A change the message does not point at.
	write("shop/api/api.go", "package api\n\nfunc Serve() { _ = 1 }\n")
	git("add", "-A")
	git("commit", "-qm", "api: tidy the handler")
	// No Go change: skipped.
	write("README.md", "hi\n")
	git("add", "-A")
	git("commit", "-qm", "docs")

	rep, err := (&LeaseStudy{Repo: dir, Commits: 5}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Commits) != 2 || rep.Skipped != 1 {
		t.Fatalf("commits %d skipped %d", len(rep.Commits), rep.Skipped)
	}
	tidy, alpha := rep.Commits[0], rep.Commits[1]
	if alpha.Subject != "shop/cart: make AlphaThing return 2" || alpha.Tests != 1 {
		t.Fatalf("alpha: %+v", alpha)
	}
	f := alpha.Full
	if f.Symbols != (Overlap{Changed: 1, Leased: 1, Both: 1}) || f.Files != (Overlap{Changed: 2, Leased: 1, Both: 1}) || !f.Contained {
		t.Fatalf("alpha scores: %+v", f)
	}
	if b := alpha.Bare; b.Symbols.Both != 1 || !b.Contained {
		t.Fatalf("without the prefix: %+v", b)
	}
	if !tidy.Bare.Empty || tidy.Bare.Contained || tidy.Full.Dirs.Both != 0 {
		t.Fatalf("tidy: %+v", tidy)
	}
	if s := rep.Full; s.Commits != 2 || s.NonEmpty != 1 || s.SymbolP != 1 || s.SymbolR != 0.5 || s.Contained != 1 {
		t.Fatalf("summary: %+v", s)
	}
	var md bytes.Buffer
	WriteLeaseMarkdown(&md, []*LeaseReport{rep})
	if !strings.Contains(md.String(), "| as written | 2 | 1 (50%) | 100% | 50% |") {
		t.Fatalf("markdown:\n%s", md.String())
	}
	if message("fix: x\n\nbody line\n\nReviewed-on: https://r\nFixes golang/go#1") != "fix: x\n\nbody line\n\nFixes golang/go#1" {
		t.Fatal("trailers")
	}
}
