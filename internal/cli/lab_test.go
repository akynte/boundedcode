package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/config"
)

// A workload override applies to projects in its languages only, and an
// empty direct.workloads (the default) changes nothing.
func TestDirectOptionsWorkloads(t *testing.T) {
	app := testApp(t)
	goProj, pyProj := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(goProj, "go.mod"), []byte("module example.com/x\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pyProj, "pyproject.toml"), []byte("[project]\nname = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ep := &directEndpoint{provider: "local", model: "m", ctxSize: 32768}
	before := app.directOptions(goProj, goProj, "auto", ep)
	if before.Workload != "" || before.Retrieval != "" {
		t.Fatalf("no workloads configured, yet: %q %q", before.Workload, before.Retrieval)
	}
	app.Config.Direct.Workloads = []config.WorkloadOverride{{Languages: []string{"go"}, Set: config.Tunables{Retrieval: "basic", MaxTurns: 30}, Source: "lab proposal x"}}
	if err := app.Config.Validate(); err != nil {
		t.Fatal(err)
	}
	g := app.directOptions(goProj, goProj, "auto", ep)
	if g.Retrieval != "basic" || g.MaxTurns != 30 || !strings.Contains(g.Workload, "languages go") || !strings.Contains(g.Workload, "lab proposal x") {
		t.Fatalf("go project: retrieval %q, max turns %d, workload %q", g.Retrieval, g.MaxTurns, g.Workload)
	}
	// Everything else is as configured.
	if g.Approval != before.Approval || g.Sandbox == nil {
		t.Fatal("the override touched something else")
	}
	p := app.directOptions(pyProj, pyProj, "auto", ep)
	if p.Workload != "" || p.Retrieval != "" || p.MaxTurns != before.MaxTurns {
		t.Fatalf("python project got the go override: %+v", p.Workload)
	}
}
