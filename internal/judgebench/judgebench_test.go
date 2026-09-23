package judgebench_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/eval"
	"github.com/akynte/boundedcode/internal/judgebench"
)

// The shipped corpus, judged end to end. It pins two things: the corpus is
// labelled the way its authors meant (a candidate whose hidden label drifts
// from its intent is a broken candidate, not a finding), and the supervisor
// still rejects every attack on the visible tests that CI accepts.
func TestShippedCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("judges every candidate with real toolchain runs")
	}
	for _, tool := range []string{"go", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	tasks, err := eval.LoadSet("../../evals/tasks")
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := judgebench.LoadCorpus("../../evals/judges")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	rows, err := judgebench.Run(context.Background(), tasks, candidates,
		[]judgebench.Judge{judgebench.CI{WorkDir: work}, judgebench.Bcode{WorkDir: work}},
		judgebench.Options{WorkDir: work, Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		name := r.Candidate.TaskID + "/" + r.Candidate.Name
		if r.TruthErr != "" {
			t.Errorf("%s: no label: %s", name, r.TruthErr)
			continue
		}
		for judge, v := range r.Verdicts {
			if v.Err != "" {
				t.Errorf("%s: %s could not judge: %s", name, judge, v.Err)
			}
		}
		// Agent patches have no stated intent; only the hand-written
		// candidates can drift from theirs.
		if want := strings.HasPrefix(r.Candidate.Intent, "correct"); r.Candidate.Patch == "" && r.Solved != want {
			t.Errorf("%s: solved=%v, but its intent is %q", name, r.Solved, r.Candidate.Intent)
		}
		attack := strings.Contains(r.Candidate.Name, "-skip") || strings.Contains(r.Candidate.Name, "-delete")
		if attack && !r.Solved && r.Verdicts["bcode"].Accepted {
			t.Errorf("%s: bcode accepted an attack on the tests that reach the change", name)
		}
		if strings.HasPrefix(r.Candidate.Name, "reference") && !strings.Contains(r.Candidate.Name, "skips") &&
			!r.Verdicts["bcode"].Accepted {
			t.Errorf("%s: bcode rejected a correct fix: %v", name, r.Verdicts["bcode"].Reasons)
		}
	}
}

func TestLoadCorpusReadsEditsAndPatches(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte(
		"task: t1\ncandidates:\n  - name: one\n    intent: correct fix\n    edits:\n      - path: x.go\n        old: a\n        new: b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "t1"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "t1", "agent-x.patch"), []byte("diff --git a/x b/x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := judgebench.LoadCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "agent-x" || got[0].TaskID != "t1" || got[0].Patch == "" ||
		got[1].Name != "one" || len(got[1].Edits) != 1 {
		t.Fatalf("corpus = %+v", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.yaml"), []byte("task: t1\ncandidates:\n  - nme: typo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := judgebench.LoadCorpus(dir); err == nil {
		t.Error("an unknown field must be refused")
	}
}

func TestTalliesExcludeUnlabelled(t *testing.T) {
	rows := []judgebench.Row{
		{Solved: false, Verdicts: map[string]judgebench.Verdict{"j": {Accepted: true}}},
		{Solved: true, Verdicts: map[string]judgebench.Verdict{"j": {Accepted: false}}},
		{TruthErr: "no label", Verdicts: map[string]judgebench.Verdict{"j": {Accepted: true}}},
	}
	got := judgebench.Tallies(rows, []string{"j"})[0]
	if got.FalseAccepts != 1 || got.FalseRejects != 1 || got.CandidatesSeen != 2 {
		t.Fatalf("tally = %+v", got)
	}
}

// Every shipped oracle loads as an operator's suite would, and fails on the
// unfixed code: an oracle that passes there cannot tell done from untouched.
func TestShippedOraclesCatchTheUnfixedCode(t *testing.T) {
	if testing.Short() {
		t.Skip("runs every oracle against its fixture")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	tasks, err := eval.LoadSet("../../evals/tasks")
	if err != nil {
		t.Fatal(err)
	}
	suites, err := judgebench.LoadOracles("../../evals/oracles")
	if err != nil {
		t.Fatal(err)
	}
	if len(suites) == 0 {
		t.Fatal("no oracles loaded")
	}
	for _, task := range tasks {
		suite, ok := suites[task.ID]
		if !ok {
			continue
		}
		caught, err := judgebench.OracleCatchesBase(context.Background(), t.TempDir(), task, suite)
		if err != nil {
			t.Errorf("%s: %v", task.ID, err)
			continue
		}
		if !caught {
			t.Errorf("%s: the oracle passes on the unfixed code", task.ID)
		}
	}
}
