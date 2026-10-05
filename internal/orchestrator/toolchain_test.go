package orchestrator

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/verify"
)

// TestAgentToolchainIsolatedFromVerification: the agent gets verification's
// module cache but never verification's build cache (Go caches test results).
func TestAgentToolchainIsolatedFromVerification(t *testing.T) {
	root := t.TempDir()
	r := &Runner{Paths: config.Paths{Data: filepath.Join(root, "data")},
		Verify: &verify.Engine{CacheDir: filepath.Join(root, "cache"), GoModCache: "/m"}}
	tc := r.agentToolchain("t1")
	if tc.GoModCache != "/m" {
		t.Fatalf("module cache = %q", tc.GoModCache)
	}
	if strings.HasPrefix(tc.GoCache, r.Verify.CacheDir) || !strings.HasPrefix(tc.GoCache, r.Paths.TaskDir("t1")) {
		t.Fatalf("agent build cache %q must be the task's own, not verification's %q", tc.GoCache, r.Verify.CacheDir)
	}
}
