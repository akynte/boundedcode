package retrieve

import (
	"path"
	"strings"

	"github.com/akynte/boundedcode/internal/ecosystem"
	"github.com/akynte/boundedcode/internal/lang"
)

// isTestPath: test code by its ecosystem's conventions or its language's
// test patterns.
func isTestPath(rel string) bool {
	if ecosystem.IsTest(rel) {
		return true
	}
	if l := lang.ForPath(rel, ""); l != nil && l.IsTest(rel) {
		return true
	}
	return false
}

// implStems are the names a test file's implementation goes by, from the
// naming conventions of each ecosystem's test runners: cart_test.go ->
// cart.go, test_cart.py -> cart.py, cart.test.ts -> cart.ts, CartTest.java
// -> Cart.java, cart_spec.rb -> cart.rb, CartTests.cs -> Cart.cs.
func implStems(rel string) []string {
	base := path.Base(rel)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	var out []string
	add := func(s string) {
		if s != "" && s != stem {
			out = append(out, s)
		}
	}
	for _, mid := range []string{".test", ".spec", ".tests", ".e2e", ".unit"} {
		if strings.HasSuffix(stem, mid) {
			add(strings.TrimSuffix(stem, mid))
		}
	}
	for _, suf := range []string{"_test", "_spec", "_tests", "Tests", "Test", "Spec", "IT", "Suite"} {
		if strings.HasSuffix(stem, suf) && len(stem) > len(suf) {
			add(strings.TrimSuffix(stem, suf))
		}
	}
	for _, pre := range []string{"test_", "Test"} {
		if strings.HasPrefix(stem, pre) && len(stem) > len(pre) {
			add(strings.TrimPrefix(stem, pre))
		}
	}
	if len(out) == 0 && isTestPath(rel) {
		// tests/cart.rs, __tests__/cart.ts, spec/cart.rb: same stem elsewhere.
		out = append(out, stem)
	}
	return out
}

// implCandidates are the listed non-test files a test file's
// implementation may be: the same stem in a source language, the closest
// by directory first (src/test/java/a/B.java -> src/main/java/a/B.java).
func (r *repo) implCandidates(test string) []string {
	var out []string
	seen := map[string]bool{}
	for _, stem := range implStems(test) {
		for _, f := range r.files {
			b := path.Base(f)
			if strings.TrimSuffix(b, path.Ext(b)) != stem || seen[f] || isTestPath(f) || f == test {
				continue
			}
			if l := r.langOf[f]; l == nil || l.Kind != lang.KindProgramming {
				continue
			}
			seen[f] = true
			out = append(out, f)
		}
	}
	// Closest first: the most leading directories in common with the test,
	// mirrored layouts (src/test -> src/main) counting the rest.
	score := func(f string) int {
		a, b := strings.Split(path.Dir(f), "/"), strings.Split(path.Dir(test), "/")
		n := 0
		for i := 0; i < len(a) && i < len(b) && a[i] == b[i]; i++ {
			n++
		}
		return 2*n + commonSuffix(a, b)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && (score(out[j]) > score(out[j-1]) || score(out[j]) == score(out[j-1]) && out[j] < out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
