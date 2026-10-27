package direct

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A benchmark sees each request split by what it is made of, and the
// harness's own time per phase.
func TestRequestSectionsAndPhases(t *testing.T) {
	up := &scripted{replies: []reply{
		{calls: []call{{name: "write", args: map[string]any{"path": "a.json", "content": "{}"}}}},
		{calls: []call{{name: "ls", args: map[string]any{"path": "."}}}},
		{text: "done"},
	}}
	r, root, _ := newRunner(t, up, ApprovalAuto)
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var reqs []map[string]string
	r.OnRequest = func(s map[string]string) { reqs = append(reqs, s) }
	if _, err := r.Send(context.Background(), "write a.json and list the files"); err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 3 {
		t.Fatalf("requests seen: %d", len(reqs))
	}
	first := reqs[0]
	for _, k := range []string{SectionSystem, SectionToolSchemas, SectionEnvironment, SectionProjectMap, SectionUser} {
		if first[k] == "" {
			t.Errorf("first request has no %s section: %v", k, keys(first))
		}
	}
	if !strings.Contains(first[SectionProjectMap], "Go module example.com/m") || strings.Contains(first[SectionUser], "Project map") {
		t.Errorf("project map not separated from the user text")
	}
	last := reqs[2]
	if last["tool:write"] == "" || last["tool:ls"] == "" || !strings.Contains(last[SectionToolCalls], "ls") {
		t.Errorf("tool results and calls by tool: %v", keys(last))
	}
	if last[SectionSystem] != first[SectionSystem] {
		t.Error("the system prompt changed between requests")
	}
	ph := r.Phases()
	for _, k := range []string{PhaseProjectMap, PhaseSeedPack, PhaseWorkingSet, PhaseChecks} {
		if _, ok := ph[k]; !ok {
			t.Errorf("no time recorded for %s: %v", k, ph)
		}
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
