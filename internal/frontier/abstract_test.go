package frontier

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/contextplan"
)

func TestAbstractPacket(t *testing.T) {
	root := t.TempDir()
	src := "package ledger\n\nfunc Balance(entries []Entry) (total int64) {\n\tfor _, e := range entries {\n\t\ttotal += e.AmountCents\n\t}\n\treturn total\n}\n"
	if err := os.WriteFile(filepath.Join(root, "ledger.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	lines, err := CollectSourceLines(context.Background(), []string{root}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if !lines["func Balance(entries []Entry) (total int64) {"] || lines["}"] || lines["return total"] {
		t.Fatalf("collected %v", lines)
	}
	pack := contextplan.Pack{Sections: []contextplan.Section{
		{Key: "task", Title: "TASK", Body: "Fix the balance: it double-counts refunds."},
		{Key: "verification", Title: "LATEST VERIFICATION FAILURES", Body: "--- FAIL: TestBalanceRefund\n    ledger_test.go:12: got 300, want 100\n\t\ttotal += e.AmountCents"},
		{Key: "diff", Title: "CURRENT CHANGES", Body: "diff --git a/ledger.go b/ledger.go\n--- a/ledger.go\n+++ b/ledger.go\n@@ -3,3 +3,4 @@\n func Balance(entries []Entry) (total int64) {\n+\tif e.Refund { continue }\n-\t\ttotal += e.AmountCents\n"},
		{Key: "code", Title: "RELEVANT CODE", Body: src},
		{Key: "contracts", Title: "CROSS-SERVICE CONTRACTS", Body: "topic payments.charged"},
	}}
	p := BuildAbstractPacket(Trigger{Code: Z2, Reason: "repeated failure"}, pack, "What is wrong?", nil, "", lines)
	if err := CheckAbstract(p, lines); err != nil {
		t.Fatalf("abstract packet refused: %v\n%s", err, p)
	}
	for _, leak := range []string{"total += e.AmountCents", "func Balance(entries", "payments.charged", "RELEVANT CODE"} {
		if strings.Contains(p, leak) {
			t.Errorf("packet carries %q:\n%s", leak, p)
		}
	}
	for _, kept := range []string{"double-counts refunds", "TestBalanceRefund", "got 300, want 100", "- ledger.go: +1 -1", "withheld"} {
		if !strings.Contains(p, kept) {
			t.Errorf("packet lost %q:\n%s", kept, p)
		}
	}
	// The gate refuses a packet with a source line, whatever built it.
	full := BuildPacket(Trigger{Code: Z2, Reason: "x"}, pack, "q", nil, "")
	if err := CheckAbstract(full, lines); err == nil {
		t.Fatal("a full packet passed the abstract gate")
	}
}
