package retrieve

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/akynte/boundedcode/internal/repointel"
)

// shop is a polyglot repository: a Go service, a Python job, a TypeScript
// client, a Java module, a Rust crate, an OpenAPI definition, configuration,
// documentation, tests in each language, a secret and test data.
var shop = map[string]string{
	"go.mod":                                             "module example.com/shop\n",
	"internal/cart/cart.go":                              "package cart\n\n// Total sums the cart.\nfunc Total(items []Item) int {\n\tsum := 0\n\tfor _, it := range items {\n\t\tsum += it.Price * it.Qty\n\t}\n\treturn sum\n}\n\ntype Item struct{ Price, Qty int }\n",
	"internal/cart/cart_test.go":                         "package cart\n\nimport \"testing\"\n\nfunc TestTotal(t *testing.T) {\n\tif Total([]Item{{2, 3}}) != 6 {\n\t\tt.Fatal(\"wrong\")\n\t}\n}\n",
	"internal/api/server.go":                             "package api\n\nimport \"os\"\n\nfunc Routes() {\n\thandle(\"/api/v1/orders\", listOrders)\n\t_ = os.Getenv(\"SHOP_RATE_LIMIT\")\n}\n\nfunc listOrders() {}\nfunc handle(string, func()) {}\n",
	"api/openapi.yaml":                                   "paths:\n  /api/v1/orders:\n    get:\n      summary: list orders\n",
	"jobs/inventory/stock.py":                            "def reorder(items, per_box):\n    return [i.qty // per_box for i in items]\n\n\ndef restock(n):\n    return n\n",
	"jobs/inventory/test_stock.py":                       "from inventory.stock import reorder\n\ndef test_reorder():\n    assert reorder([], 3) == []\n",
	"web/src/cart/discount.ts":                           "export function applyDiscount(price: number, pct: number): number {\n  return price - price * pct\n}\n",
	"web/src/cart/discount.test.ts":                      "import { applyDiscount } from './discount'\n\nit('applies discount', () => {\n  expect(applyDiscount(10, 0.5)).toBe(5)\n})\n",
	"java/src/main/java/com/acme/orders/Orders.java":     "package com.acme.orders;\n\npublic class Orders {\n    public int paidTotal() {\n        return 0;\n    }\n}\n",
	"java/src/test/java/com/acme/orders/OrdersTest.java": "package com.acme.orders;\n\npublic class OrdersTest {\n    @Test\n    public void paid() {\n        new Orders().paidTotal();\n    }\n}\n",
	"ledger/src/ledger.rs":                               "pub fn transfer(from: i64, amount: i64) -> i64 {\n    from - amount\n}\n",
	"config/shop.yaml":                                   "server:\n  read_timeout: 30s\n",
	"docs/cart.md":                                       "# Cart\n\nThe cart totals items.\n",
	".env":                                               "SHOP_RATE_LIMIT=secret Total\n",
	"internal/cart/testdata/cart.go":                     "package cart\n\nfunc Total() int { return 1 }\n",
	"node_modules/lib/index.js":                          "function Total() {}\n",
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for p, s := range files {
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, p), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func gitInit(t *testing.T, root string) string {
	t.Helper()
	for _, a := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@e", "commit", "-qm", "base"}} {
		c := exec.Command("git", a...)
		c.Dir = root
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	c := exec.Command("git", "rev-parse", "HEAD")
	c.Dir = root
	out, err := c.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func retrieveIn(t *testing.T, root string, in Input, opt Options) Result {
	t.Helper()
	opt.Root = root
	res, err := Retrieve(context.Background(), in, opt)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func top(res Result) string {
	if len(res.Items) == 0 {
		return ""
	}
	return res.Items[0].File
}

func listed(res Result) []string {
	var out []string
	for _, it := range res.Items {
		out = append(out, it.File)
	}
	return out
}

func ran(res Result) []string {
	var out []string
	for _, s := range res.Steps {
		if s.Ran {
			out = append(out, string(s.Strategy))
		}
	}
	return out
}

// TestStrategyPerSignal: each kind of evidence selects its strategy and
// puts the file the task needs first, in each language.
func TestStrategyPerSignal(t *testing.T) {
	root := writeTree(t, shop)
	cases := []struct {
		name     string
		in       Input
		strategy Strategy
		want     string
	}{
		{"go panic", Input{Request: "the service crashed", Evidence: []string{"panic: runtime error: index out of range\n\ngoroutine 1 [running]:\nexample.com/shop/internal/cart.Total()\n\t/build/shop/internal/cart/cart.go:7 +0x1d"}},
			StackTrace, "internal/cart/cart.go"},
		{"python traceback", Input{Request: "reorder crashes:\n```\nTraceback (most recent call last):\n  File \"/srv/jobs/inventory/stock.py\", line 2, in reorder\nZeroDivisionError: integer division or modulo by zero\n```"},
			StackTrace, "jobs/inventory/stock.py"},
		{"java exception", Input{Request: "NPE in orders", Evidence: []string{"java.lang.NullPointerException\n\tat com.acme.orders.Orders.paidTotal(Orders.java:5)"}},
			StackTrace, "java/src/main/java/com/acme/orders/Orders.java"},
		{"rust panic", Input{Request: "overflow", Evidence: []string{"thread 'main' panicked at src/ledger.rs:2:5:\nattempt to subtract with overflow"}},
			StackTrace, "ledger/src/ledger.rs"},
		{"tsc error", Input{Request: "the build fails", Evidence: []string{"web/src/cart/discount.ts(2,10): error TS2322: Type 'string' is not assignable to type 'number'."}},
			BuildError, "web/src/cart/discount.ts"},
		{"go test failure", Input{Request: "make the tests pass", Evidence: []string{"--- FAIL: TestTotal (0.00s)\n    cart_test.go:6: wrong\nFAIL"}},
			TestToImpl, "internal/cart/cart.go"},
		{"pytest id", Input{Request: "fix it", Evidence: []string{"FAILED jobs/inventory/test_stock.py::test_reorder - AssertionError"}},
			TestToImpl, "jobs/inventory/stock.py"},
		{"junit id", Input{Request: "fix it", Evidence: []string{"FAILED com.acme.orders.OrdersTest#paid"}},
			TestToImpl, "java/src/main/java/com/acme/orders/Orders.java"},
		{"jest", Input{Request: "fix it", Evidence: []string{"FAIL web/src/cart/discount.test.ts\n  ● applies discount"}},
			TestToImpl, "web/src/cart/discount.ts"},
		{"named symbol", Input{Request: "`applyDiscount` rounds wrongly"}, Symbols, "web/src/cart/discount.ts"},
		{"qualified symbol", Input{Request: "`cart.Total` double-counts"}, Symbols, "internal/cart/cart.go"},
		{"member in prose", Input{Request: "Add a unit test for Orders.paidTotal in the java/src/main/java/com/acme package"}, Symbols, "java/src/main/java/com/acme/orders/Orders.java"},
		{"named path", Input{Request: "tidy up ledger/src/ledger.rs please"}, Paths, "ledger/src/ledger.rs"},
		{"endpoint", Input{Request: "GET /api/v1/orders should paginate"}, Contracts, "api/openapi.yaml"},
		{"env var", Input{Request: "make SHOP_RATE_LIMIT configurable per tenant"}, DocsConfig, "internal/api/server.go"},
		{"prose only", Input{Request: "the inventory stock reorder job rounds boxes down"}, Discovery, "jobs/inventory/stock.py"},
	}
	for _, c := range cases {
		res := retrieveIn(t, root, c.in, Options{})
		found := false
		for _, s := range ran(res) {
			found = found || s == string(c.strategy)
		}
		if !found {
			t.Errorf("%s: %s did not run (ran %v)", c.name, c.strategy, ran(res))
		}
		if got := top(res); got != c.want {
			t.Errorf("%s: first %q, want %q; listed %v", c.name, got, c.want, listed(res))
		}
	}
}

// TestNeverListsSecretsOrDependencies: a secret file, a dependency and
// test data never outrank (or, for the first two, appear among) the code.
func TestNeverListsSecretsOrDependencies(t *testing.T) {
	root := writeTree(t, shop)
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "internal/cart/passwd.go")); err != nil {
		t.Fatal(err)
	}
	res := retrieveIn(t, root, Input{Request: "`Total` and SHOP_RATE_LIMIT and root:x:0"}, Options{})
	for _, f := range listed(res) {
		if f == ".env" || strings.HasPrefix(f, "node_modules/") || strings.HasSuffix(f, "passwd.go") {
			t.Fatalf("listed %s: %v", f, listed(res))
		}
	}
	if top(res) == "internal/cart/testdata/cart.go" {
		t.Fatalf("test data ranked first: %v", listed(res))
	}
}

// TestDeterministic: the same input over the same tree gives the same
// result.
func TestDeterministic(t *testing.T) {
	root := writeTree(t, shop)
	in := Input{Request: "`Total` is wrong for discounted items in the cart and applyDiscount; see GET /api/v1/orders", Evidence: []string{"--- FAIL: TestTotal"}}
	a := retrieveIn(t, root, in, Options{})
	for i := 0; i < 3; i++ {
		b := retrieveIn(t, root, in, Options{})
		if !reflect.DeepEqual(listed(a), listed(b)) || Render(a, 800) != Render(b, 800) {
			t.Fatalf("run %d differs:\n%v\n%v", i, listed(a), listed(b))
		}
	}
}

// TestBudgets: the file limit and a cancelled context stop retrieval and
// say so.
func TestBudgets(t *testing.T) {
	root := writeTree(t, shop)
	res := retrieveIn(t, root, Input{Request: "`Total`"}, Options{MaxFiles: 3})
	if res.Truncated != "file limit" || res.Files != 3 {
		t.Fatalf("file limit: %+v", res)
	}
	res = retrieveIn(t, root, Input{Request: "`Total`"}, Options{MaxBytes: 10})
	if res.Truncated != "byte budget" {
		t.Fatalf("byte budget: %q", res.Truncated)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Retrieve(ctx, Input{Request: "`Total`"}, Options{Root: root}); err == nil {
		t.Fatal("a cancelled retrieval must fail")
	}
	t0 := time.Now()
	retrieveIn(t, root, Input{Request: "`Total`"}, Options{Budget: time.Nanosecond})
	if time.Since(t0) > 2*time.Second {
		t.Fatal("the time budget was not kept")
	}
}

// TestProvenanceAndStaleness: items carry the revision, the blob and the
// methods; a change to the file makes them stale; files the model already
// read unchanged are named, not repeated.
func TestProvenanceAndStaleness(t *testing.T) {
	root := writeTree(t, shop)
	rev := gitInit(t, root)
	res := retrieveIn(t, root, Input{Request: "`applyDiscount` rounds wrongly"}, Options{})
	it := res.Items[0]
	content, _ := os.ReadFile(filepath.Join(root, it.File))
	if it.Prov.Revision != rev || it.Prov.Blob != BlobID(content) || len(it.Prov.Methods) == 0 || it.Prov.At.IsZero() {
		t.Fatalf("provenance: %+v", it.Prov)
	}
	// git agrees on the blob id.
	c := exec.Command("git", "hash-object", it.File)
	c.Dir = root
	if out, err := c.Output(); err != nil || strings.TrimSpace(string(out)) != it.Prov.Blob {
		t.Fatalf("blob id %s, git says %s (%v)", it.Prov.Blob, out, err)
	}
	if s := Stale(root, res.Items); len(s) != 0 {
		t.Fatalf("nothing changed, stale %v", s)
	}
	if err := os.WriteFile(filepath.Join(root, it.File), append(content, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if s := Stale(root, res.Items); len(s) != 1 || s[0] != it.File {
		t.Fatalf("stale %v", s)
	}
	// Known with the current content: named as already read.
	now, _ := os.ReadFile(filepath.Join(root, it.File))
	res = retrieveIn(t, root, Input{Request: "`applyDiscount` rounds wrongly"}, Options{Known: map[string]string{it.File: BlobID(now)}})
	if !res.Items[0].Known || !strings.Contains(Render(res, 800), "already read in this conversation, unchanged: "+it.File) {
		t.Fatalf("known file: %+v\n%s", res.Items[0], Render(res, 800))
	}
	// Known with older content: listed again.
	res = retrieveIn(t, root, Input{Request: "`applyDiscount` rounds wrongly"}, Options{Known: map[string]string{it.File: BlobID(content)}})
	if res.Items[0].Known {
		t.Fatal("a file that changed since it was read is not known")
	}
}

// TestHistory: a commit the request names gives its files.
func TestHistory(t *testing.T) {
	root := writeTree(t, shop)
	gitInit(t, root)
	if err := os.WriteFile(filepath.Join(root, "ledger/src/ledger.rs"), []byte("pub fn transfer() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sha := gitInit(t, root) // a second commit changing ledger.rs
	res := retrieveIn(t, root, Input{Request: "balances are wrong since commit " + sha[:10]}, Options{})
	if top(res) != "ledger/src/ledger.rs" {
		t.Fatalf("history: %v %v", listed(res), ran(res))
	}
	// Outside git the strategy reports why it did not run.
	plain := writeTree(t, shop)
	res = retrieveIn(t, plain, Input{Request: "wrong since commit 0123456789abcdef"}, Options{})
	for _, s := range res.Steps {
		if s.Strategy == History && (s.Ran || !strings.Contains(s.Why, "not a git checkout")) {
			t.Fatalf("history outside git: %+v", s)
		}
	}
}

// TestRenderBudget: the block stays within its token budget.
func TestRenderBudget(t *testing.T) {
	root := writeTree(t, shop)
	res := retrieveIn(t, root, Input{Request: "`Total` `applyDiscount` `reorder` `paidTotal` `transfer` `Routes`"}, Options{})
	for _, budget := range []int{40, 120, 1200} {
		out := Render(res, budget)
		if n := (len(out)*10 + 31) / 32; n > budget && strings.Count(out, "\n- ") > 0 {
			t.Fatalf("budget %d: %d tokens\n%s", budget, n, out)
		}
	}
}

// fakeServer serves Go only, and finds Total where the patterns would not
// (a language server's answer).
type fakeServer struct{ calls *int }

func (fakeServer) Name() string           { return "fake" }
func (fakeServer) Covers(string) []string { return []string{"go"} }
func (f fakeServer) FindSymbol(context.Context, string, string, repointel.FindOptions) ([]repointel.Symbol, error) {
	*f.calls++
	return []repointel.Symbol{{NamePath: "Total", Kind: "Function", File: "internal/api/server.go", StartLine: 5}}, nil
}
func (fakeServer) References(context.Context, string, repointel.Symbol) ([]repointel.Reference, error) {
	return nil, nil
}
func (fakeServer) Implementations(context.Context, string, repointel.Symbol) ([]repointel.Symbol, error) {
	return nil, nil
}

// TestLanguageServerAnswers: where a server covers the language its
// answer counts (and only the server is asked: the scan has the
// patterns' answers); TypeScript, which it does not cover, still comes
// from the patterns.
func TestLanguageServerAnswers(t *testing.T) {
	root := writeTree(t, shop)
	n := 0
	res := retrieveIn(t, root, Input{Request: "`Total` and `applyDiscount` are wrong"}, Options{Nav: repointel.WithFallback(fakeServer{&n})})
	var server, ts bool
	for _, it := range res.Items {
		for _, w := range it.Why {
			server = server || it.File == "internal/api/server.go" && w.Strategy == Symbols && w.Weight >= relDefLSP*0.99
		}
		ts = ts || it.File == "web/src/cart/discount.ts"
	}
	if !server || !ts || n == 0 {
		t.Fatalf("server %v, ts %v, calls %d: %v", server, ts, n, listed(res))
	}
}

// TestEveryNamedThingKeepsAPlace: a request naming a TypeScript function
// (a definition) and a Go struct field (only uses) gets both; the field's
// uses would otherwise fall under the relative cut.
func TestEveryNamedThingKeepsAPlace(t *testing.T) {
	root := writeTree(t, map[string]string{
		"web/src/summary.ts": "export function parseSummary(json: string) {\n  return JSON.parse(json)\n}\n",
		"api/order.go":       "package api\n\ntype Summary struct {\n\tDiscountCents int64\n}\n",
		"api/handler.go":     "package api\n\nfunc fill(s *Summary) { s.DiscountCents = 0 }\n",
	})
	res := retrieveIn(t, root, Input{Request: "Add `DiscountCents` to the Go summary and read it in `parseSummary`."}, Options{})
	got := strings.Join(listed(res), ",")
	if !strings.HasPrefix(got, "web/src/summary.ts,") || !strings.Contains(got, "api/") {
		t.Errorf("listed %s, want the definition first and a place of DiscountCents", got)
	}
}
