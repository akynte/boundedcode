package prune

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// Every corpus output keeps the evidence its case names at the budgets
// direct mode uses (benchmarks/prune: real output of real toolchains).
func TestCorpusKeepsEvidence(t *testing.T) {
	for _, set := range []string{"../../benchmarks/prune", "../../benchmarks/prune/heldout"} {
		raw, err := os.ReadFile(filepath.Join(set, "cases.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		var cases []struct {
			ID   string   `yaml:"id"`
			Must []string `yaml:"must"`
		}
		if err := yaml.Unmarshal(raw, &cases); err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			b, err := os.ReadFile(filepath.Join(set, "corpus", c.ID+".txt"))
			if err != nil {
				t.Fatal(err)
			}
			for _, budget := range []int{4096, 8192} {
				r := Prune(string(b), Options{Budget: budget})
				if len(r.Text) > budget+budget/4 {
					t.Errorf("%s at %d: %d bytes", c.ID, budget, len(r.Text))
				}
				for _, m := range c.Must {
					if !regexp.MustCompile("(?m)" + m).MatchString(r.Text) {
						t.Errorf("%s at %d lost %q", c.ID, budget, m)
					}
				}
			}
		}
	}
}

func TestSmallOutputIsKept(t *testing.T) {
	out := "ok  \texample.com/a\t0.01s\nok  \texample.com/b\t0.02s\n"
	r := Prune(out, Options{Budget: 4096})
	if r.Pruned || r.Text != strings.TrimRight(out, "\n") {
		t.Fatalf("%+v", r)
	}
}

func TestEscapesAndOverwritesAreRemoved(t *testing.T) {
	out := "\x1b[32mPASS\x1b[0m\nDownloading 10%\rDownloading 55%\rDownloading 100%\n\x1b]0;title\x07done"
	r := Prune(out, Options{Budget: 4096})
	if strings.Contains(r.Text, "\x1b") || strings.Contains(r.Text, "55%") || !strings.Contains(r.Text, "Downloading 100%") {
		t.Fatalf("%q", r.Text)
	}
}

func TestRepeatedLinesFold(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&b, "12:00:%02d.%03d retrying connection to db (took 12ms)\n", i, i*7)
	}
	b.WriteString("fatal: could not connect\n")
	r := Prune(b.String(), Options{Budget: 1 << 20})
	if r.Repeats != 48 || strings.Count(r.Text, "retrying connection") != 2 || !strings.Contains(r.Text, "48 more lines like these") {
		t.Fatalf("repeats %d:\n%s", r.Repeats, r.Text)
	}
}

// Error words inside qualified names are not errors: a Rust backtrace's
// library frames must not outrank the failure.
func TestErrorWordsInNames(t *testing.T) {
	for line, want := range map[string]bool{
		"  12: std::panicking::catch_unwind::<core::panic::unwind_safe::AssertUnwindSafe>": false,
		"    at errors.New (errors.go:12)":                                                 false,
		"handler := on_error_handler(cfg)":                                                 false,
		"thread 'main' panicked at src/main.rs:3:5:":                                       true,
		"Error: connection refused":                                                        true,
		"FAILED tests/test_a.py::test_b - assert 1 == 2":                                   true,
		"Tests: 0 failed, 12 passed":                                                       false,
	} {
		if got := hasErrorWord(zeroRE.ReplaceAllString(line, "")); got != want {
			t.Errorf("%q: %v, want %v", line, got, want)
		}
	}
}

// Timestamps and dates are not search results.
func TestLogIsNotSearch(t *testing.T) {
	var log, grep []string
	for i := 0; i < 30; i++ {
		log = append(log, fmt.Sprintf("2026-10-28 21:30:%02d,983 INFO worker: job %d", i, i))
		grep = append(grep, fmt.Sprintf("src/app/handler_%d.go:%d:func handle%d()", i, i+3, i))
	}
	if isSearch(log) {
		t.Fatal("a log was taken for search results")
	}
	if !isSearch(grep) {
		t.Fatal("grep output was not recognised")
	}
}

func TestSearchKeepsEveryFile(t *testing.T) {
	var b strings.Builder
	for f := 0; f < 40; f++ {
		for m := 0; m < 30; m++ {
			fmt.Fprintf(&b, "pkg/file%02d.go:%d:\tx := compute(%d) // a matching line with some text\n", f, m+1, m)
		}
	}
	r := Prune(b.String(), Options{Budget: 4096})
	if !strings.Contains(r.Text, "pkg/file00.go:1:") || !strings.Contains(r.Text, "more matches in pkg/file00.go") || !strings.Contains(r.Text, "search results shortened: 1200 matches in 40 files") {
		t.Fatalf("%s", r.Text)
	}
}

// When the errors alone exceed the budget, the header says so.
func TestHeaderSaysWhatWasDropped(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 400; i++ {
		name := strings.Repeat(string(rune('a'+i%26)), 1+i/26) // distinct messages, not numbered ones
		fmt.Fprintf(&b, "src/m%d.c:%d:5: error: unknown type name '%s'\n", i, i+1, name)
	}
	r := Prune(b.String(), Options{Budget: 2048})
	if r.ErrorsDropped == 0 || !strings.Contains(r.Text, "did not fit and were dropped") {
		t.Fatalf("%d dropped:\n%s", r.ErrorsDropped, r.Text[:300])
	}
	if !strings.Contains(r.Text, "src/m0.c:1:5: error") || !strings.Contains(r.Text, "src/m399.c:400:5") {
		t.Fatalf("first or last error lost:\n%s", r.Text)
	}
}

// The same message at many places: the first few, the rest named.
func TestRepeatedMessageNamesThePlaces(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&b, "cart.ts(%d,33): error TS2304: Cannot find name 'logger'.\n", i)
		for j := 0; j < 20; j++ {
			fmt.Fprintf(&b, "  context line %d of a long note\n", j)
		}
	}
	r := Prune(b.String(), Options{Budget: 4096})
	if strings.Count(r.Text, "Cannot find name 'logger'.\n") != sameMessage || !strings.Contains(r.Text, "at 25 more places: cart.ts:6") {
		t.Fatalf("%s", r.Text)
	}
}

func TestSignal(t *testing.T) {
	if Signal("diff --git a/x b/x\n+added line\n-removed line\n") {
		t.Fatal("a diff has no signal")
	}
	if !Signal("compiling...\nmain.go:3:1: syntax error\n") {
		t.Fatal("a compiler error has a signal")
	}
}
