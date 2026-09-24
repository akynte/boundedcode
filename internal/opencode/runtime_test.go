package opencode_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/opencode"
)

func TestValidateRuntimeRejectsMismatchedOpenCodeLimit(t *testing.T) {
	dir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"default_generation_settings":{"n_ctx":32768},"total_slots":1,"model_alias":"boundedcode-bonsai"}`)
	}))
	defer server.Close()
	config := `{"model":"boundedcode-local/boundedcode-bonsai","providers":{"boundedcode-local":{"models":{"boundedcode-bonsai":{"limit":{"context":%d,"output":8192}}}}},"compaction":{"buffer":12000,"keep":{"tokens":4000}}}`
	for _, test := range []struct {
		context int
		ok      bool
	}{{32768, true}, {65536, false}} {
		if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(fmt.Sprintf(config, test.context)), 0600); err != nil {
			t.Fatal(err)
		}
		budget, err := opencode.ValidateRuntime(context.Background(), dir, server.URL)
		if test.ok && (err != nil || budget.PhysicalContext != 32768 || budget.OutputReserve != 8192) {
			t.Fatalf("matching runtime: %+v %v", budget, err)
		}
		if !test.ok && (err == nil || !strings.Contains(err.Error(), "advertises 65536")) {
			t.Fatalf("mismatch accepted: %+v %v", budget, err)
		}
	}
}

// TestValidateRuntimeCatchesTheOriginalCompactionLoopDefaults is the
// regression guard for the loop documented in
// docs/explanation/opencode-context.md: OpenCode 2.0.15's own defaults
// (buffer=20000, keep=15000) pass the simpler "buffer exceeds retained tail"
// check — 20000 > 15000 — while the resulting margin is negative, which is
// exactly how five automatic compactions happened in one short session. This
// must be rejected before a supervised run starts, not discovered from a live
// session transcript.
func TestValidateRuntimeCatchesTheOriginalCompactionLoopDefaults(t *testing.T) {
	dir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"default_generation_settings":{"n_ctx":32768},"total_slots":1,"model_alias":"boundedcode-bonsai"}`)
	}))
	defer server.Close()
	config := `{"model":"boundedcode-local/boundedcode-bonsai","providers":{"boundedcode-local":{"models":{"boundedcode-bonsai":{"limit":{"context":32768,"output":8192}}}}},"compaction":{"buffer":%d,"keep":{"tokens":%d}}}`
	for _, test := range []struct {
		name         string
		buffer, keep int
		ok           bool
	}{
		{"original OpenCode 2.0.15 defaults", 20000, 15000, false},
		{"current BoundedCode policy", 12000, 4000, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(fmt.Sprintf(config, test.buffer, test.keep)), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := opencode.ValidateRuntime(context.Background(), dir, server.URL)
			if test.ok && err != nil {
				t.Fatalf("policy that should pass was rejected: %v", err)
			}
			if !test.ok && (err == nil || !strings.Contains(err.Error(), "compaction margin")) {
				t.Fatalf("the original compaction-loop configuration was not rejected: %v", err)
			}
		})
	}
}
