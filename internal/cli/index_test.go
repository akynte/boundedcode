package cli

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/config"
)

// TestIndexRequiresRepositoryIntelligence: index checks codebase-memory-mcp
// once and names the setup step, instead of failing per repository.
func TestIndexRequiresRepositoryIntelligence(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	be := newTestBackend(t)
	mustExec(t, be, "init")
	cfg, err := config.Load(be.base.configFile())
	if err != nil {
		t.Fatal(err)
	}
	cfg.RepoIntel.Binary = "/nonexistent/codebase-memory-mcp"
	if err := config.Save(be.base.configFile(), cfg); err != nil {
		t.Fatal(err)
	}
	mustExec(t, be, "workspace", "create", "demo")
	mustExec(t, be, "workspace", "add", gitRepo(t))
	var out bytes.Buffer
	err = be.Exec(context.Background(), []string{"index"}, &out)
	if err == nil || !strings.Contains(err.Error(), "codebase-memory-mcp is not installed") || !strings.Contains(err.Error(), "setup --only tools") {
		t.Fatalf("err = %v\n%s", err, out.String())
	}
}
