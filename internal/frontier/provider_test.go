package frontier

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCodexMissingVsLoggedOut: a missing codex CLI is reported as not
// installed, not as a sign-in problem.
func TestCodexMissingVsLoggedOut(t *testing.T) {
	_, err := (&Codex{Binary: "bc-no-such-codex"}).Ask(t.Context(), "packet", t.TempDir())
	if !errors.Is(err, exec.ErrNotFound) || !strings.Contains(err.Error(), "codex CLI is not installed") || strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("missing: %v", err)
	}

	bin := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'Not logged in'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = (&Codex{Binary: bin}).Ask(t.Context(), "packet", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "codex not logged in (Not logged in): run `codex login`") {
		t.Fatalf("logged out: %v", err)
	}
}
