// Command gotestsummary makes skipped and failed tests visible in CI.
//
// It reads `go test -json` on stdin and prints the tests' ordinary text
// output, so the job log reads as usual. Then it appends a Markdown summary
// (counts, failed tests, skipped tests with their reasons) to the file named
// by $GITHUB_STEP_SUMMARY, or to stdout when that is unset.
//
//	go test -json ./... | go run ./scripts/gotestsummary [-report-only LABEL]
//
// It exits 1 if any test or package failed. With -report-only, a failure
// also raises a GitHub warning annotation naming LABEL, so a non-blocking
// (continue-on-error) step cannot pass unnoticed.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

type event struct {
	Action  string
	Package string
	Test    string
	Output  string
}

type key struct{ pkg, test string }

type summary struct {
	passed      int
	failed      []key
	failedPkgs  map[string]bool
	skipped     map[key]string // skip reason: the last output line before "--- SKIP"
	testOutputs map[key][]string
}

func summarize(in io.Reader, out io.Writer) (*summary, error) {
	s := &summary{failedPkgs: map[string]bool{}, skipped: map[key]string{}, testOutputs: map[key][]string{}}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var ev event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			fmt.Fprintln(out, sc.Text()) // build errors arrive as plain text
			continue
		}
		k := key{ev.Package, ev.Test}
		switch ev.Action {
		case "output":
			fmt.Fprint(out, ev.Output)
			if ev.Test != "" {
				s.testOutputs[k] = append(s.testOutputs[k], ev.Output)
			}
		case "fail":
			if ev.Test != "" {
				s.failed = append(s.failed, k)
			} else {
				s.failedPkgs[ev.Package] = true
			}
		case "pass":
			if ev.Test != "" {
				s.passed++
			}
		case "skip":
			if ev.Test != "" {
				reason := ""
				for _, o := range s.testOutputs[k] {
					o = strings.TrimSpace(o)
					if o != "" && !strings.HasPrefix(o, "=== ") && !strings.HasPrefix(o, "--- SKIP") {
						reason = o
					}
				}
				s.skipped[k] = reason
			}
		}
	}
	return s, sc.Err()
}

func short(pkg string) string { return strings.TrimPrefix(pkg, "github.com/akynte/boundedcode/") }

func (s *summary) markdown(label string) string {
	var b strings.Builder
	title := "Go tests"
	if label != "" {
		title += " (" + label + ")"
	}
	fmt.Fprintf(&b, "### %s\n\n| Passed | Failed | Skipped | Failed packages |\n|---|---|---|---|\n| %d | %d | %d | %d |\n\n",
		title, s.passed, len(s.failed), len(s.skipped), len(s.failedPkgs))
	if len(s.failed) > 0 || len(s.failedPkgs) > 0 {
		b.WriteString("<details open><summary>Failed</summary>\n\n")
		withTests := map[string]bool{}
		for _, k := range s.failed {
			withTests[k.pkg] = true
			fmt.Fprintf(&b, "- `%s` %s\n", short(k.pkg), k.test)
		}
		for _, p := range sortedKeys(s.failedPkgs) {
			if !withTests[p] {
				fmt.Fprintf(&b, "- `%s` (package: build failure or failure outside a test)\n", short(p))
			}
		}
		b.WriteString("\n</details>\n\n")
	}
	if len(s.skipped) > 0 {
		keys := make([]key, 0, len(s.skipped))
		for k := range s.skipped {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].pkg != keys[j].pkg {
				return keys[i].pkg < keys[j].pkg
			}
			return keys[i].test < keys[j].test
		})
		b.WriteString("<details><summary>Skipped</summary>\n\n")
		for _, k := range keys {
			r := s.skipped[k]
			if r == "" {
				r = "(no reason given)"
			}
			fmt.Fprintf(&b, "- `%s` %s: %s\n", short(k.pkg), k.test, strings.ReplaceAll(r, "|", `\|`))
		}
		b.WriteString("\n</details>\n\n")
	}
	return b.String()
}

func (s *summary) failedPackageCount() int {
	pkgs := map[string]bool{}
	for p := range s.failedPkgs {
		pkgs[p] = true
	}
	for _, k := range s.failed {
		pkgs[k.pkg] = true
	}
	return len(pkgs)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func main() { os.Exit(run()) }

func run() int {
	reportOnly := flag.String("report-only", "", "label for a warning annotation when tests fail in a non-blocking step")
	flag.Parse()
	s, err := summarize(os.Stdin, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gotestsummary:", err)
		return 2
	}
	if err := writeSummary(s.markdown(*reportOnly)); err != nil {
		fmt.Fprintln(os.Stderr, "gotestsummary:", err)
		return 2
	}
	if len(s.failed) == 0 && len(s.failedPkgs) == 0 {
		return 0
	}
	if *reportOnly != "" {
		fmt.Printf("::warning title=%s::%d tests in %d packages fail (report only; see the job summary)\n",
			*reportOnly, len(s.failed), s.failedPackageCount())
	}
	return 1
}

// writeSummary appends to $GITHUB_STEP_SUMMARY, or prints when it is unset.
func writeSummary(md string) error {
	p := os.Getenv("GITHUB_STEP_SUMMARY")
	if p == "" {
		_, err := fmt.Print(md)
		return err
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(md); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
