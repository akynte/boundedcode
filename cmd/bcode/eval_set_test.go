package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/eval"
)

// The commands in the documentation must execute exactly as written.
//
// The previous pass documented `bcode eval run --arm … --set dev`. Neither flag
// existed: the real one is --arms, and --set had not been implemented at all.
// Nobody noticed because nothing checked, and a benchmark procedure nobody can
// run is worse than none — it reads as though the measurement is available.
func TestDocumentedEvalCommandsParse(t *testing.T) {
	root := repoRoot(t)
	docs := []string{
		filepath.Join(root, "docs", "how-to", "use-judgments.md"),
		filepath.Join(root, "docs", "explanation", "judgments.md"),
		filepath.Join(root, "README.md"),
	}
	// Only the `bcode …` lines inside fenced console blocks.
	line := regexp.MustCompile(`(?m)^\$ (bcode [a-z].*)$`)

	var checked int
	for _, doc := range docs {
		body, err := os.ReadFile(doc) //nolint:gosec // a path this test constructed
		if err != nil {
			continue
		}
		for _, m := range line.FindAllStringSubmatch(string(body), -1) {
			cmdline := strings.TrimSpace(m[1])
			if i := strings.Index(cmdline, " #"); i >= 0 {
				cmdline = strings.TrimSpace(cmdline[:i])
			}
			if strings.ContainsAny(cmdline, "…") {
				continue // an elided example, not a runnable command
			}
			checked++
			t.Run(cmdline, func(t *testing.T) {
				args := append(strings.Fields(cmdline)[1:], "--help")
				out, err := exec.Command("go", append([]string{"run",
					"github.com/akynte/boundedcode/cmd/bcode"}, args...)...).CombinedOutput()
				if err != nil {
					t.Fatalf("%s does not parse:\n%s", cmdline, out)
				}
				if strings.Contains(string(out), "unknown flag") {
					t.Fatalf("%s uses a flag that does not exist:\n%s", cmdline, out)
				}
			})
		}
	}
	if checked == 0 {
		t.Fatal("no documented command was checked; the regexp no longer matches the docs")
	}
}

// --set must actually select, not merely parse.
func TestEvalSetFilterSelects(t *testing.T) {
	tasks := []eval.Task{
		{ID: "a"}, // unlabelled: defaults to dev
		{ID: "b", Set: eval.SetDev},
		{ID: "c", Set: eval.SetHeldout},
	}
	dev := filterSet(tasks, eval.SetDev)
	if len(dev) != 2 {
		t.Fatalf("dev selected %d task(s), want 2 (an unlabelled task defaults to dev)", len(dev))
	}
	held := filterSet(tasks, eval.SetHeldout)
	if len(held) != 1 || held[0].ID != "c" {
		t.Fatalf("heldout selected %v", held)
	}
	for _, d := range dev {
		if d.ID == "c" {
			t.Fatal("a held-out task was selected into dev; tuning would contaminate it")
		}
	}
}

// An unknown set must be refused rather than silently running everything: a
// typo in --set would otherwise produce a full-set run labelled as held-out.
func TestEvalRejectsAnUnknownSet(t *testing.T) {
	out, err := exec.Command("go", "run",
		"github.com/akynte/boundedcode/cmd/bcode", "eval", "run", "--set", "helout").CombinedOutput()
	if err == nil {
		t.Fatal("an unknown set was accepted")
	}
	if !strings.Contains(string(out), "unknown set") {
		t.Fatalf("the error does not name the problem:\n%s", out)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for range 6 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Skip("module root not found")
	return ""
}
