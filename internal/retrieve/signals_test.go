package retrieve

import (
	"strings"
	"testing"
	"time"
)

func TestIntent(t *testing.T) {
	for req, want := range map[string]Intent{
		"The parser panics on empty input":               IntentFix,
		"Rename Ledger.Post to Ledger.Record everywhere": IntentRefactor,
		"How does the retry loop decide to back off?":    IntentExplain,
		"Add tests for the currency converter":           IntentTest,
		"Make the cache size a configuration option":     IntentConfig,
		"Support exporting invoices as CSV":              IntentChange,
	} {
		if got := Extract(req).Intent; got != want {
			t.Errorf("%q: %s, want %s", req, got, want)
		}
	}
}

// TestTestIDs: failed tests as each runner reports them.
func TestTestIDs(t *testing.T) {
	cases := []struct{ out, name, file string }{
		{"FAILED tests/test_cart.py::TestCart::test_total[eur] - assert 1 == 2", "test_total", "tests/test_cart.py"},
		{"FAILED com.acme.orders.OrdersTest#paidTotal", "paidTotal", "OrdersTest"},
		{"FAIL: test_reorder (inventory.tests.StockTests)", "test_reorder", ""},
		{"test routing::tests::nest::nesting_router_at_root ... FAILED", "routing::tests::nest::nesting_router_at_root", ""},
		{"FAILED routing::tests::nest::nesting_router_at_root", "nesting_router_at_root", ""},
		{"--- FAIL: TestBalance (0.00s)", "TestBalance", ""},
		{"  ● Cart › applies discount", "applies discount", ""},
		{"1) Tests\\Unit\\CartTest::testTotal", "testTotal", ""},
	}
	for _, c := range cases {
		s := Extract("fix it", c.out)
		found := false
		for _, n := range s.Tests {
			found = found || n == c.name
		}
		if !found {
			t.Errorf("%q: tests %v, want %q", c.out, s.Tests, c.name)
		}
		if c.file != "" && (len(s.TestFiles) == 0 || s.TestFiles[0] != c.file) {
			t.Errorf("%q: test files %v, want %q", c.out, s.TestFiles, c.file)
		}
	}
}

func TestSignals(t *testing.T) {
	req := "After commit 3f2a9c1d the `GET /api/v1/orders` route ignores SHOP_PAGE_SIZE; see ledger/src/ledger.rs and `tokio::sync::broadcast`.\n```\nfrom django.db.models import Q\nTIME_IN_INTERVAL(x)\n```"
	s := Extract(req)
	if strings.Join(s.Endpoints, ",") != "/api/v1/orders" {
		t.Errorf("endpoints %v", s.Endpoints)
	}
	if strings.Join(s.EnvVars, ",") != "SHOP_PAGE_SIZE" {
		t.Errorf("env vars %v (a code sample's constants are not)", s.EnvVars)
	}
	if strings.Join(s.Commits, ",") != "3f2a9c1d" {
		t.Errorf("commits %v", s.Commits)
	}
	var mods []string
	for _, m := range s.Modules {
		mods = append(mods, strings.Join(m, "."))
	}
	if j := strings.Join(mods, " "); !strings.Contains(j, "tokio.sync.broadcast") || !strings.Contains(j, "django.db.models") {
		t.Errorf("modules %v", mods)
	}
	for _, m := range mods {
		if strings.HasSuffix(m, ".rs") {
			t.Errorf("a file path is not a module: %v", mods)
		}
	}
	if !s.Strong() {
		t.Error("strong")
	}
	if Extract("the layout jumps when the window is resized").Strong() {
		t.Error("prose is not strong")
	}
}

// TestHostileEvidence: evidence is data. Paths that leave the checkout,
// absolute system paths and enormous output neither escape the root nor
// stall retrieval.
func TestHostileEvidence(t *testing.T) {
	root := writeTree(t, shop)
	ev := "  File \"../../../../etc/passwd\", line 1, in x\n" +
		"\t/etc/shadow.go:1 +0x0\n" +
		"panic: x\n\t" + root + "/../outside.go:3 +0x1\n" +
		strings.Repeat("at junk (/nowhere/x.js:1:1)\n", 50000)
	t0 := time.Now()
	res := retrieveIn(t, root, Input{Request: "it crashed", Evidence: []string{ev}}, Options{})
	if time.Since(t0) > 5*time.Second && !raceEnabled {
		t.Fatalf("took %s", time.Since(t0))
	}
	for _, it := range res.Items {
		if strings.Contains(it.File, "..") || strings.HasPrefix(it.File, "/") || strings.Contains(it.File, "passwd") || strings.Contains(it.File, "shadow") {
			t.Fatalf("listed %q", it.File)
		}
	}
}

// TestMembersAndRoutes: a member named in prose is a name to look up, in
// any language's notation; a path inside a directory name is no route.
func TestMembersAndRoutes(t *testing.T) {
	s := Extract("Add a test for Ledger.Balance in ledger-service's internal/ledger package; Order::total and ledger.Post too, not os.path or e.g. this.")
	if len(s.Endpoints) != 0 {
		t.Errorf("endpoints %v: internal/ledger holds no route", s.Endpoints)
	}
	var names []string
	for _, sd := range s.Seeds {
		if isNameSeed(sd) {
			names = append(names, sd.Text)
		}
	}
	if j := strings.Join(names, ","); j != "Ledger.Balance,Order.total,ledger.Post" {
		t.Errorf("names %q", j)
	}
	for _, r := range []string{"GET /api/v1/orders pages", "call `/health` first", "(see /api/v1/orders)"} {
		if e := Extract(r).Endpoints; len(e) != 1 {
			t.Errorf("%q: endpoints %v", r, e)
		}
	}
}
