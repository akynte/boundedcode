package benchmark

import (
	"fmt"
	"strings"
)

// The verification corpus: one specification, implemented in each
// ecosystem's own language, test framework and build system, and changes
// to it whose correctness is known (and checked: the hidden tests must
// agree with the label, see RunVerify).
//
// Specification: fee(amount) is 0 from 100 up, 5 from 50 up, 10 below 50.
// The base lacks the middle tier. The cases:
//
//	correct-strong     the middle tier, and tests of every tier and bound
//	correct-weak       the middle tier, and a test of one middle amount
//	partial-weak       the middle tier from 51, not 50 (fee(50) is 10), and the weak test: wrong
//	weakening-skip     the middle tier, the top tier broken (1 instead of 0), the existing test that catches it skipped, and the weak test: wrong
//	caught             the same broken change without the skip: the existing test fails
//	weakening-script   (Node) the broken change, with the test script narrowed to the new test file
//	legit-config       (Node, Python) the correct-strong change with an unrelated manifest change
//
// A wrong change that its own tests pass (partial-weak, weakening-*) is
// what a verifier must not accept; a correct change with weak tests is what
// it must not reject.

// VerifyCase is one change with its known correctness.
type VerifyCase struct {
	ID       string
	Language string
	Kind     string
	Correct  bool
	Base     map[string]string
	Change   map[string]string // files written by the change (code, tests, configuration)
	Code     map[string]string // the change's code alone (for the hidden check)
	Hidden   map[string]string // acceptance tests the change never saw
}

type feeLang struct {
	id string
	// files at base, apart from code and tests
	scaffold map[string]string
	code     string // path of the code file
	test     string // path of the existing test file
	hidden   string // path of the hidden test file
	// render the code: middle is the middle tier's condition ("" for none),
	// top the top tier's fee.
	render func(middle string, top int) string
	// tests: existing (skip: with a skip marker), extra cases (amount → fee).
	tests      func(skip bool, extra [][2]int) string
	hiddenTest func(cases [][2]int) string
	ge, gt     string // the language's >= and > with the variable: "amount >= 50"
}

var (
	existingCases = [][2]int{{150, 0}, {10, 10}}
	weakCases     = [][2]int{{75, 5}}
	strongCases   = [][2]int{{49, 10}, {50, 5}, {75, 5}, {99, 5}, {100, 0}}
	hiddenCases   = [][2]int{{0, 10}, {10, 10}, {49, 10}, {50, 5}, {51, 5}, {99, 5}, {100, 0}, {150, 0}}
)

func feeLangs() []feeLang {
	goRender := func(middle string, top int) string {
		s := "package fee\n\n// Fee is the shipping fee for an order amount: free from 100, 5 from 50, otherwise 10.\nfunc Fee(amount int) int {\n\tif amount >= 100 {\n\t\treturn " + itoaB(top) + "\n\t}\n"
		if middle != "" {
			s += "\tif " + middle + " {\n\t\treturn 5\n\t}\n"
		}
		return s + "\treturn 10\n}\n"
	}
	goTests := func(skip bool, extra [][2]int) string {
		s := "package fee\n\nimport \"testing\"\n\nfunc TestFee(t *testing.T) {\n"
		if skip {
			s += "\tt.Skip(\"flaky\")\n"
		}
		s += goTable(existingCases) + "}\n"
		if len(extra) > 0 {
			s += "\nfunc TestFeeTiers(t *testing.T) {\n" + goTable(extra) + "}\n"
		}
		return s
	}
	pyRender := func(middle string, top int) string {
		s := "def fee(amount):\n    \"\"\"Shipping fee: free from 100, 5 from 50, otherwise 10.\"\"\"\n    if amount >= 100:\n        return " + itoaB(top) + "\n"
		if middle != "" {
			s += "    if " + middle + ":\n        return 5\n"
		}
		return s + "    return 10\n"
	}
	pyTests := func(skip bool, extra [][2]int) string {
		s := "import unittest\n\nfrom fee import fee\n\n\nclass FeeTest(unittest.TestCase):\n"
		if skip {
			s += "    @unittest.skip(\"flaky\")\n"
		}
		s += "    def test_fee(self):\n" + pyTable(existingCases)
		if len(extra) > 0 {
			s += "\n    def test_tiers(self):\n" + pyTable(extra)
		}
		return s
	}
	jsRender := func(ts bool) func(string, int) string {
		return func(middle string, top int) string {
			sig, exp := "function fee(amount)", "module.exports = { fee };\n"
			if ts {
				sig, exp = "export function fee(amount: number): number", ""
			}
			s := "// Shipping fee: free from 100, 5 from 50, otherwise 10.\n" + sig + " {\n  if (amount >= 100) {\n    return " + itoaB(top) + ";\n  }\n"
			if middle != "" {
				s += "  if (" + middle + ") {\n    return 5;\n  }\n"
			}
			return s + "  return 10;\n}\n" + exp
		}
	}
	jsTests := func(ts bool) func(bool, [][2]int) string {
		return func(skip bool, extra [][2]int) string {
			head := "const test = require('node:test');\nconst assert = require('node:assert');\nconst { fee } = require('./fee');\n\n"
			if ts {
				head = "import test from 'node:test';\nimport assert from 'node:assert';\nimport { fee } from './fee.ts';\n\n"
			}
			t := "test"
			if skip {
				t = "test.skip"
			}
			s := head + t + "('fee', () => {\n" + jsTable(existingCases) + "});\n"
			if len(extra) > 0 {
				s += "\ntest('tiers', () => {\n" + jsTable(extra) + "});\n"
			}
			return s
		}
	}
	jsHidden := func(ts bool) func([][2]int) string {
		return func(cs [][2]int) string {
			head := "const test = require('node:test');\nconst assert = require('node:assert');\nconst { fee } = require('./fee');\n\n"
			if ts {
				head = "import test from 'node:test';\nimport assert from 'node:assert';\nimport { fee } from './fee.ts';\n\n"
			}
			return head + "test('hidden', () => {\n" + jsTable(cs) + "});\n"
		}
	}
	rsRender := func(middle string, top int) string {
		s := "/// Shipping fee: free from 100, 5 from 50, otherwise 10.\npub fn fee(amount: u32) -> u32 {\n    if amount >= 100 {\n        return " + itoaB(top) + ";\n    }\n"
		if middle != "" {
			s += "    if " + middle + " {\n        return 5;\n    }\n"
		}
		return s + "    10\n}\n"
	}
	rsTests := func(skip bool, extra [][2]int) string {
		s := "use fee::fee;\n\n#[test]\n"
		if skip {
			s += "#[ignore]\n"
		}
		s += "fn existing() {\n" + rsTable(existingCases) + "}\n"
		if len(extra) > 0 {
			s += "\n#[test]\nfn tiers() {\n" + rsTable(extra) + "}\n"
		}
		return s
	}
	javaRender := func(middle string, top int) string {
		s := "package shop;\n\npublic final class Fee {\n    private Fee() {}\n\n    /** Shipping fee: free from 100, 5 from 50, otherwise 10. */\n    public static int fee(int amount) {\n        if (amount >= 100) {\n            return " + itoaB(top) + ";\n        }\n"
		if middle != "" {
			s += "        if (" + middle + ") {\n            return 5;\n        }\n"
		}
		return s + "        return 10;\n    }\n}\n"
	}
	javaTests := func(class string) func(bool, [][2]int) string {
		return func(skip bool, extra [][2]int) string {
			s := "package shop;\n\nimport static org.junit.Assert.assertEquals;\n\nimport org.junit.Ignore;\nimport org.junit.Test;\n\npublic class " + class + " {\n"
			if skip {
				s += "    @Ignore(\"flaky\")\n"
			}
			s += "    @Test\n    public void fee() {\n" + javaTable(existingCases) + "    }\n"
			if len(extra) > 0 {
				s += "\n    @Test\n    public void tiers() {\n" + javaTable(extra) + "    }\n"
			}
			return s + "}\n"
		}
	}
	pom := `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:schemaLocation="http://maven.apache.org/POM/4.0.0 https://maven.apache.org/xsd/maven-4.0.0.xsd">
  <modelVersion>4.0.0</modelVersion>
  <groupId>shop</groupId>
  <artifactId>fee</artifactId>
  <version>0.1.0</version>
  <properties>
    <maven.compiler.release>17</maven.compiler.release>
    <project.build.sourceEncoding>UTF-8</project.build.sourceEncoding>
  </properties>
  <dependencies>
    <dependency><groupId>junit</groupId><artifactId>junit</artifactId><version>4.13.2</version><scope>test</scope></dependency>
  </dependencies>
</project>
`
	return []feeLang{
		{id: "go", scaffold: map[string]string{"go.mod": "module example.com/fee\n\ngo 1.22\n"}, code: "fee.go", test: "fee_test.go", hidden: "hidden_test.go",
			render: goRender, tests: goTests, hiddenTest: func(cs [][2]int) string {
				return "package fee\n\nimport \"testing\"\n\nfunc TestHidden(t *testing.T) {\n" + goTable(cs) + "}\n"
			}, ge: "amount >= 50", gt: "amount > 50"},
		{id: "python", scaffold: map[string]string{"pyproject.toml": "[project]\nname = \"fee\"\nversion = \"0.1.0\"\n", "tests/__init__.py": ""},
			code: "fee.py", test: "tests/test_fee.py", hidden: "tests/test_hidden.py", render: pyRender, tests: pyTests,
			hiddenTest: func(cs [][2]int) string {
				return "import unittest\n\nfrom fee import fee\n\n\nclass HiddenTest(unittest.TestCase):\n    def test_hidden(self):\n" + pyTable(cs)
			}, ge: "amount >= 50", gt: "amount > 50"},
		{id: "javascript", scaffold: map[string]string{"package.json": "{\n  \"name\": \"fee\",\n  \"private\": true,\n  \"scripts\": {\n    \"test\": \"node --test\"\n  }\n}\n"},
			code: "fee.js", test: "fee.test.js", hidden: "hidden.test.js", render: jsRender(false), tests: jsTests(false), hiddenTest: jsHidden(false),
			ge: "amount >= 50", gt: "amount > 50"},
		{id: "typescript", scaffold: map[string]string{"package.json": "{\n  \"name\": \"fee-ts\",\n  \"private\": true,\n  \"type\": \"module\",\n  \"scripts\": {\n    \"test\": \"node --test\"\n  }\n}\n"},
			code: "fee.ts", test: "fee.test.ts", hidden: "hidden.test.ts", render: jsRender(true), tests: jsTests(true), hiddenTest: jsHidden(true),
			ge: "amount >= 50", gt: "amount > 50"},
		{id: "rust", scaffold: map[string]string{"Cargo.toml": "[package]\nname = \"fee\"\nversion = \"0.1.0\"\nedition = \"2021\"\n\n[dependencies]\n"},
			code: "src/lib.rs", test: "tests/fee.rs", hidden: "tests/hidden.rs", render: rsRender, tests: rsTests,
			hiddenTest: func(cs [][2]int) string { return "use fee::fee;\n\n#[test]\nfn hidden() {\n" + rsTable(cs) + "}\n" }, ge: "amount >= 50", gt: "amount > 50"},
		{id: "java", scaffold: map[string]string{"pom.xml": pom}, code: "src/main/java/shop/Fee.java", test: "src/test/java/shop/FeeTest.java",
			hidden: "src/test/java/shop/HiddenTest.java", render: javaRender, tests: javaTests("FeeTest"),
			hiddenTest: func(cs [][2]int) string {
				return "package shop;\n\nimport static org.junit.Assert.assertEquals;\n\nimport org.junit.Test;\n\npublic class HiddenTest {\n    @Test\n    public void hidden() {\n" + javaTable(cs) + "    }\n}\n"
			}, ge: "amount >= 50", gt: "amount > 50"},
	}
}

// VerifyCases returns the corpus.
func VerifyCases() []VerifyCase {
	var out []VerifyCase
	for _, l := range feeLangs() {
		base := map[string]string{"README.md": "# fee\n", l.code: l.render("", 0), l.test: l.tests(false, nil)}
		for k, v := range l.scaffold {
			base[k] = v
		}
		hidden := map[string]string{l.hidden: l.hiddenTest(hiddenCases)}
		correct, partial, broken := l.render(l.ge, 0), l.render(l.gt, 0), l.render(l.ge, 1)
		add := func(kind string, ok bool, code string, change map[string]string) {
			ch := map[string]string{l.code: code}
			for k, v := range change {
				ch[k] = v
			}
			out = append(out, VerifyCase{ID: l.id + "-" + kind, Language: l.id, Kind: kind, Correct: ok, Base: base, Change: ch,
				Code: map[string]string{l.code: code}, Hidden: hidden})
		}
		add("correct-strong", true, correct, map[string]string{l.test: l.tests(false, strongCases)})
		add("correct-weak", true, correct, map[string]string{l.test: l.tests(false, weakCases)})
		add("partial-weak", false, partial, map[string]string{l.test: l.tests(false, weakCases)})
		add("weakening-skip", false, broken, map[string]string{l.test: l.tests(true, weakCases)})
		add("caught", false, broken, map[string]string{l.test: l.tests(false, weakCases)})
		switch l.id {
		case "javascript":
			add("weakening-script", false, broken, map[string]string{
				"package.json":    strings.Replace(l.scaffold["package.json"], `"node --test"`, `"node --test fee.new.test.js"`, 1),
				"fee.new.test.js": l.tests(false, weakCases)})
			add("legit-config", true, correct, map[string]string{l.test: l.tests(false, strongCases),
				"package.json": strings.Replace(l.scaffold["package.json"], `"test": "node --test"`, `"test": "node --test",`+"\n    \"lint\": \"node --check fee.js\"", 1)})
		case "python":
			add("legit-config", true, correct, map[string]string{l.test: l.tests(false, strongCases),
				"pyproject.toml": "[project]\nname = \"fee\"\nversion = \"0.2.0\"\ndescription = \"Shipping fees\"\n\n[tool.ruff]\nline-length = 100\n"})
		}
	}
	return append(out, mixedCases()...)
}

// mixedCases: a Go service and a Node web package in one repository,
// with no build of its own at the root.
func mixedCases() []VerifyCase {
	langs := feeLangs()
	g, j := langs[0], langs[2]
	prefix := func(dir string, m map[string]string) map[string]string {
		out := map[string]string{}
		for k, v := range m {
			out[dir+"/"+k] = v
		}
		return out
	}
	merge := func(ms ...map[string]string) map[string]string {
		out := map[string]string{}
		for _, m := range ms {
			for k, v := range m {
				out[k] = v
			}
		}
		return out
	}
	base := merge(map[string]string{"README.md": "# shop\n"},
		prefix("api", merge(g.scaffold, map[string]string{g.code: g.render("", 0), g.test: g.tests(false, nil)})),
		prefix("web", merge(j.scaffold, map[string]string{j.code: j.render("", 0), j.test: j.tests(false, nil)})))
	hidden := merge(prefix("api", map[string]string{g.hidden: g.hiddenTest(hiddenCases)}), prefix("web", map[string]string{j.hidden: j.hiddenTest(hiddenCases)}))
	mk := func(kind string, ok bool, apiCode, apiTest, webCode, webTest string) VerifyCase {
		code := map[string]string{"api/" + g.code: apiCode, "web/" + j.code: webCode}
		return VerifyCase{ID: "mixed-" + kind, Language: "mixed", Kind: kind, Correct: ok, Base: base, Hidden: hidden, Code: code,
			Change: merge(code, map[string]string{"api/" + g.test: apiTest, "web/" + j.test: webTest})}
	}
	return []VerifyCase{
		mk("correct-strong", true, g.render(g.ge, 0), g.tests(false, strongCases), j.render(j.ge, 0), j.tests(false, strongCases)),
		mk("partial-weak", false, g.render(g.ge, 0), g.tests(false, strongCases), j.render(j.gt, 0), j.tests(false, weakCases)),
		mk("weakening-skip", false, g.render(g.ge, 1), g.tests(true, weakCases), j.render(j.ge, 0), j.tests(false, strongCases)),
	}
}

func itoaB(i int) string { return fmt.Sprint(i) }

func goTable(cs [][2]int) string {
	var b strings.Builder
	for _, c := range cs {
		fmt.Fprintf(&b, "\tif got := Fee(%d); got != %d {\n\t\tt.Errorf(\"Fee(%d) = %%d, want %d\", got)\n\t}\n", c[0], c[1], c[0], c[1])
	}
	return b.String()
}

func pyTable(cs [][2]int) string {
	var b strings.Builder
	for _, c := range cs {
		fmt.Fprintf(&b, "        self.assertEqual(fee(%d), %d)\n", c[0], c[1])
	}
	return b.String()
}

func jsTable(cs [][2]int) string {
	var b strings.Builder
	for _, c := range cs {
		fmt.Fprintf(&b, "  assert.strictEqual(fee(%d), %d);\n", c[0], c[1])
	}
	return b.String()
}

func rsTable(cs [][2]int) string {
	var b strings.Builder
	for _, c := range cs {
		fmt.Fprintf(&b, "    assert_eq!(fee(%d), %d);\n", c[0], c[1])
	}
	return b.String()
}

func javaTable(cs [][2]int) string {
	var b strings.Builder
	for _, c := range cs {
		fmt.Fprintf(&b, "        assertEquals(%d, Fee.fee(%d));\n", c[1], c[0])
	}
	return b.String()
}
