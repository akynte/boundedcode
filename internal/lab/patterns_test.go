package lab

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/benchmark"
)

// call is a tool call and its result in a synthetic transcript.
type call struct {
	tool   string
	args   map[string]any
	result string
}

// transcript writes a session file under root/<task>-1/state/sessions/r/
// as bench direct leaves it; compactAt inserts a compaction before those
// call indexes.
func transcript(t *testing.T, root, task, session string, calls []call, compactAt ...int) {
	t.Helper()
	dir := filepath.Join(root, task+"-1", "state", "sessions", "r")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var lines []string
	add := func(v any) {
		b, _ := json.Marshal(v)
		lines = append(lines, string(b))
	}
	add(map[string]any{"type": "session", "session": map[string]any{"id": session}})
	add(map[string]any{"type": "message", "message": map[string]any{"role": "user", "content": "do it"}})
	for i, c := range calls {
		for _, at := range compactAt {
			if at == i {
				add(map[string]any{"type": "compact", "keep": 2})
				add(map[string]any{"type": "message", "message": map[string]any{"role": "user", "content": "summary"}})
			}
		}
		a, _ := json.Marshal(c.args)
		id := "c" + string(rune('a'+i%26)) + strings.Repeat("x", i/26)
		add(map[string]any{"type": "message", "message": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": id, "type": "function",
			"function": map[string]any{"name": c.tool, "arguments": string(a)}}}}})
		add(map[string]any{"type": "message", "message": map[string]any{"role": "tool", "tool_call_id": id, "name": c.tool, "content": c.result}})
	}
	if err := os.WriteFile(filepath.Join(dir, session+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func report(t *testing.T, path string, settings map[string]any, results ...benchmark.DirectResult) {
	t.Helper()
	b, _ := json.Marshal(benchmark.DirectReport{ID: "r1", Settings: settings, Results: results})
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func kinds(fs []Finding, task string) map[string]bool {
	out := map[string]bool{}
	for _, f := range fs {
		if f.Run.Task == task {
			out[f.Kind] = true
		}
	}
	return out
}

func bash(cmd, result string) call {
	return call{"bash", map[string]any{"command": cmd}, result}
}

func read(p, result string) call { return call{"read", map[string]any{"path": p}, result} }

func edit(p string) call {
	return call{"edit", map[string]any{"path": p, "old_string": "a", "new_string": "b"}, "edited"}
}

func TestDetectors(t *testing.T) {
	root, rep := t.TempDir(), filepath.Join(t.TempDir(), "r.json")
	// A loop: the same read again and again, then failing recalls.
	loop := []call{read("a.go", "x"), read("a.go", "x"), read("a.go", "x"), read("a.go", "x")}
	for range 5 {
		loop = append(loop, call{"recall", map[string]any{"id": "r9"}, "error: nothing was set aside as r9"})
	}
	transcript(t, root, "loop", "s-loop", loop)
	// A change that is never tested, with shell reads instead of tools and
	// rejected calls.
	transcript(t, root, "untested", "s-untested", []call{bash("cat a.go", "x"), bash("grep -n foo a.go", "x"), bash("ls src", "x"),
		read("src", "error: src is a directory; use ls"), read("b.go", "error: open b.go: no such file or directory"), edit("a.go")})
	// Its own tests pass after the change, the hidden ones fail; the test
	// is re-run with nothing changed.
	transcript(t, root, "confident", "s-confident", []call{read("a.go", "x"), edit("a.go"), bash("go test ./...", "ok"), bash("go test ./...", "ok")})
	// A compaction, then the same unchanged files read again.
	transcript(t, root, "compacted", "s-compacted", []call{read("a.go", "x"), read("b.go", "y"), read("c.go", "z"), bash("go test ./...", "ok"),
		read("a.go", "x"), read("b.go", "y")}, 4)
	// The target file opened only near the end; a big result.
	late := []call{}
	for i := range 12 {
		late = append(late, read("other"+string(rune('a'+i))+".go", "x"))
	}
	late = append(late, read("pkg/ledger.go", strings.Repeat("y", 40<<10)))
	transcript(t, root, "late", "s-late", late)
	report(t, rep, map[string]any{"max_turns": 80.0},
		benchmark.DirectResult{ID: "loop", Category: "session", SessionID: "s-loop", Success: false, Calls: 80, Turns: 1, Languages: []string{"go"}, Split: "dev"},
		benchmark.DirectResult{ID: "untested", Category: "fix", SessionID: "s-untested", Success: false, Calls: 7, Turns: 1, FilesChanged: []string{"a.go"}, Languages: []string{"python"}, Split: "dev"},
		benchmark.DirectResult{ID: "confident", Category: "fix", SessionID: "s-confident", Success: false, Calls: 5, Turns: 1, FilesChanged: []string{"a.go"},
			FailedChecks: []string{"go test ./...: --- FAIL: TestHidden"}, Languages: []string{"go"}, Split: "dev"},
		benchmark.DirectResult{ID: "compacted", Category: "fix", SessionID: "s-compacted", Success: true, Calls: 7, Turns: 1, Compactions: 1, Languages: []string{"go"}, Split: "dev"},
		benchmark.DirectResult{ID: "late", Category: "fix", SessionID: "s-late", Success: true, Calls: 14, Turns: 1, Languages: []string{"rust"}, Split: "dev"},
		benchmark.DirectResult{ID: "held", Category: "fix", Success: true, Calls: 3, Turns: 1, Split: "heldout"},
	)
	tasks := map[string]benchmark.TaskSpec{"late": {ID: "late", Reference: []benchmark.Edit{{Repo: "r", File: "pkg/ledger.go"}}}}
	runs, err := Load([]string{rep}, []string{root}, tasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 6 || len(runs[0].Steps) != 9 || runs[0].Steps[4].Error == "" {
		t.Fatalf("loaded %d runs, %+v", len(runs), runs[0].Steps)
	}
	fs := Detect(runs, Calibrate(runs))
	for task, want := range map[string][]string{
		"loop":      {"identical-call", "failing-streak", "recall-failed", "turn-limit"},
		"untested":  {"unverified-change", "shell-for-tool", "tool-misuse"},
		"confident": {"false-confidence", "rerun-unchanged"},
		"compacted": {"re-read-after"},
		"late":      {"found-late", "large-result"},
	} {
		got := kinds(fs, task)
		for _, k := range want {
			if !got[k] {
				t.Errorf("%s: no %s finding (got %v)", task, k, got)
			}
		}
	}
	if k := kinds(fs, "compacted"); k["unverified-change"] || k["false-confidence"] {
		t.Errorf("a passing run without changes flagged for verification: %v", k)
	}

	// Held-out runs stay out of the analysis unless asked for; failure
	// classifications are not counted as association.
	a, err := Analyze(runs, []string{rep}, false)
	if err != nil {
		t.Fatal(err)
	}
	if a.Runs != 5 || a.LeftOut["heldout"] != 1 || len(a.Reports) != 1 || a.Reports[0].SHA256 == "" {
		t.Fatalf("analysis: %d runs, left out %v", a.Runs, a.LeftOut)
	}
	wv, _ := a.Summary(WeakVerification)
	if wv.Classified != 1 || wv.Runs != 2 {
		t.Fatalf("weak verification: %+v", wv)
	}
	all, _ := Analyze(runs, []string{rep}, true)
	if all.Runs != 6 || all.Scope != "all" {
		t.Fatalf("with held-out: %d %s", all.Runs, all.Scope)
	}
}

func TestHypothesesAndDrafts(t *testing.T) {
	a := &Analysis{Patterns: []PatternSummary{
		{Pattern: RepeatedCalls, Runs: 5, Tasks: 3, Eligible: 50, Recurring: true, Kinds: map[string]int{"identical-call": 5}, Waste: 40},
		{Pattern: ContextExcess, Runs: 1, Tasks: 1, Eligible: 50, Kinds: map[string]int{"large-prompt": 1}},
		{Pattern: Retries, Runs: 4, Tasks: 2, Recurring: true, Associated: true, Kinds: map[string]int{"rerun-unchanged": 4}},
	}, Findings: []FindingRef{{Pattern: RepeatedCalls, Kind: "identical-call", Task: "t1", Languages: []string{"go"}}, {Pattern: Retries, Kind: "rerun-unchanged", Task: "t2", Languages: []string{"python"}}}}
	hs := Hypothesize(a)
	ids := map[string]bool{}
	for _, h := range hs {
		ids[h.ID] = true
	}
	if !ids["loop-guard"] || !ids["rerun-note"] || ids["observation-budget"] || ids["fewer-attempts"] {
		t.Fatalf("hypotheses: %v", ids)
	}
	if hs[0].ID != "rerun-note" {
		t.Fatalf("an associated pattern ranks first: %s", hs[0].ID)
	}
	// Every draft is a valid experiment once its code arm has a patch.
	dir := t.TempDir()
	for _, h := range Hypothesize(&Analysis{Patterns: allRecurring()}) {
		p := filepath.Join(dir, h.ID+".yaml")
		if err := os.WriteFile(p, DraftExperiment(h, 2), 0o644); err != nil {
			t.Fatal(err)
		}
		e, err := LoadExperiment(p)
		if err != nil {
			t.Fatalf("draft %s: %v", h.ID, err)
		}
		if len(e.Arms) < 2 || e.Baseline().Name != "baseline" {
			t.Fatalf("draft %s: %+v", h.ID, e.Arms)
		}
	}
}

// allRecurring marks every pattern recurring with every kind, so every
// intervention yields a hypothesis.
func allRecurring() []PatternSummary {
	var out []PatternSummary
	for _, p := range Patterns {
		k := map[string]int{}
		for _, iv := range Catalog() {
			for _, kind := range iv.Kinds {
				k[kind] = 1
			}
		}
		out = append(out, PatternSummary{Pattern: p, Runs: 3, Tasks: 2, Recurring: true, Kinds: k})
	}
	return out
}
