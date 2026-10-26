package lease

import (
	"sort"
	"strings"
	"testing"
)

const cartGo = `package cart

// Total sums the cart.
func Total(items []int) int {
	t := 0
	for _, v := range items {
		t += v
	}
	return t
}

type Ledger struct{ n int }

// Balance returns the balance.
func (l *Ledger) Balance() int { return l.n }

func (l *List[T]) Usage() {}

const (
	maxItems = 10
	minItems = 1
)

var setHelpFunc = func() {}
`

func index(files map[string]string) *Index {
	var paths []string
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return NewIndex(paths, func(p string) []Decl { return ParseGo(p, []byte(files[p])) })
}

func keys(m map[string]bool) string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

func TestParseGo(t *testing.T) {
	ds := ParseGo("shop/cart/cart.go", []byte(cartGo))
	var got []string
	for _, d := range ds {
		got = append(got, d.Key())
	}
	want := "shop/cart:Total shop/cart:Ledger shop/cart:Ledger.Balance shop/cart:List.Usage shop/cart:maxItems shop/cart:minItems shop/cart:setHelpFunc"
	if strings.Join(got, " ") != want {
		t.Fatalf("decls:\n%s\nwant\n%s", strings.Join(got, " "), want)
	}
	// Doc comments belong to the declaration; grouped constants have
	// their own lines.
	if ds[0].Lo != 3 || ds[0].Hi != 10 || ds[4].Lo != 20 || ds[4].Hi != 20 {
		t.Fatalf("lines: %+v %+v", ds[0], ds[4])
	}
	if ParseGo("x.go", []byte("package")) != nil {
		t.Fatal("a file that does not parse has no declarations")
	}
}

func TestDerive(t *testing.T) {
	ix := index(map[string]string{
		"shop/cart/cart.go": cartGo,
		"shop/api/api.go":   "package api\n\nfunc Total() {}\nfunc CreatePayment() {}\n",
		"shop/README.md":    "# shop\n",
		"vendor/x/x.go":     "package x\n\nfunc CreatePayment() {}\n",
	})
	if ix.Files["vendor/x/x.go"] {
		t.Fatal("vendored code is not indexed")
	}
	cases := []struct{ req, syms, files, dirs string }{
		// A qualified method, a camelCase name, a file by suffix.
		{"Ledger.Balance ignores debits; also setHelpFunc and cart/cart.go", "shop/cart:Ledger.Balance shop/cart:setHelpFunc", "shop/cart/cart.go", "shop/cart"},
		// A plain capitalised word is English unless marked as code.
		{"Total is wrong when Usage is printed", "", "", ""},
		{"fix `Total()` please", "shop/api:Total shop/cart:Total", "shop/api/api.go shop/cart/cart.go", "shop/api shop/cart"},
		// pkg.Name picks the package; a directory by suffix.
		{"cart.Total overflows; see shop/api", "shop/cart:Total", "shop/cart/cart.go", "shop/api shop/cart"},
		{"CreatePayment returns 500", "shop/api:CreatePayment", "shop/api/api.go", "shop/api"},
		{"Fix the README.md typo", "", "shop/README.md", "shop"},
	}
	for _, c := range cases {
		l := Derive(ix, c.req)
		if keys(l.Symbols) != c.syms || keys(l.Files) != c.files || keys(l.Dirs) != c.dirs {
			t.Errorf("Derive(%q):\n syms %q\n files %q\n dirs %q", c.req, keys(l.Symbols), keys(l.Files), keys(l.Dirs))
		}
	}
	l := Derive(ix, "cart.Total overflows")
	if !l.Covers("shop/cart/other.go") || l.Covers("shop/api/api.go") || Derive(ix, "nothing here").Covers("shop/cart/cart.go") {
		t.Fatal("Covers")
	}
}

// A name defined in many places says nothing unless the request also
// names the directory.
func TestDeriveAmbiguous(t *testing.T) {
	files := map[string]string{}
	for _, d := range []string{"a", "b", "c", "d", "e"} {
		files["pkg/"+d+"/x.go"] = "package " + d + "\n\nfunc NewClient() {}\n"
	}
	ix := index(files)
	if l := Derive(ix, "NewClient leaks"); !l.Empty() {
		t.Fatalf("ambiguous: %v", keys(l.Symbols))
	}
	if l := Derive(ix, "NewClient in pkg/c leaks"); keys(l.Symbols) != "pkg/c:NewClient" {
		t.Fatalf("with its directory: %v", keys(l.Symbols))
	}
}

func TestTouched(t *testing.T) {
	ds := ParseGo("cart.go", []byte(cartGo))
	var got []string
	for _, d := range Touched(ds, [][2]int{{1, 2}, {6, 6}, {15, 15}}) {
		got = append(got, d.Name)
	}
	if strings.Join(got, ",") != "Total,Balance" {
		t.Fatalf("touched: %v", got)
	}
}

func TestSkipped(t *testing.T) {
	for p, want := range map[string]bool{"vendor/a.go": true, "a/testdata/b.go": true, ".github/x.yml": true, "internal/a.go": false, "node_modules/x/y.js": true} {
		if Skipped(p) != want {
			t.Errorf("Skipped(%q) = %v", p, !want)
		}
	}
}
