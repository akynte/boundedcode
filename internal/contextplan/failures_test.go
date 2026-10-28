package contextplan

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/verify"
)

// TestFailureSourcesAnyLanguage: a retry pack shows the code the failures
// point at, whatever the language: a Python traceback, a JVM exception, a
// Rust panic, a failing JUnit test's implementation. Paths printed from
// another machine are mapped onto the worktree; a planted symlink to a
// file outside it is never read.
func TestFailureSourcesAnyLanguage(t *testing.T) {
	w := lexicalWorktree(t, map[string]string{
		"jobs/stock.py": "def reorder(items, per_box):\n    return [i // per_box for i in items]\n",
		"orders/src/main/java/com/acme/Orders.java":     "package com.acme;\n\npublic class Orders {\n    public int paidTotal() {\n        return 1 / 0;\n    }\n}\n",
		"orders/src/test/java/com/acme/OrdersTest.java": "package com.acme;\n\npublic class OrdersTest {\n    void paid() { new Orders().paidTotal(); }\n}\n",
		"ledger/src/ledger.rs":                          "pub fn transfer(from: i64, amount: i64) -> i64 {\n    from - amount\n}\n",
	})
	if err := os.Symlink("/etc/passwd", filepath.Join(w.Path, "jobs", "leak.py")); err != nil {
		t.Fatal(err)
	}
	failures := "Traceback (most recent call last):\n  File \"/ci/work/jobs/stock.py\", line 2, in reorder\nZeroDivisionError: integer division or modulo by zero\n" +
		"java.lang.ArithmeticException: / by zero\n\tat com.acme.Orders.paidTotal(Orders.java:5)\n" +
		"thread 'main' panicked at src/ledger.rs:2:5:\nattempt to subtract with overflow\n" +
		"  File \"/ci/work/jobs/leak.py\", line 1, in x\n"
	in := Inputs{Task: &task.Task{ID: "t", OriginalRequest: "make the build green"}, Worktrees: []task.Worktree{w}, Mode: ModeRetry,
		Verification: []verify.Result{{Repository: "billing", Stages: []verify.StageResult{{Name: "test", Status: "fail", Output: failures}}}}}
	var st IntelStats
	out := failureSources(context.Background(), in, &st)
	for _, want := range []string{"billing jobs/stock.py:2", "return [i // per_box for i in items]",
		"billing orders/src/main/java/com/acme/Orders.java:5", "return 1 / 0;", "billing ledger/src/ledger.rs:2", "from - amount"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "root:") || strings.Contains(out, "leak.py") {
		t.Fatalf("followed a planted link:\n%s", out)
	}
	if len(st.Retrieval) == 0 || st.RetrievalMillis <= 0 {
		t.Fatalf("stats: %+v", st)
	}
	// Built into the pack's code section.
	p, err := Build(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Render(), "jobs/stock.py:2") {
		t.Fatalf("pack:\n%s", p.Render())
	}
}
