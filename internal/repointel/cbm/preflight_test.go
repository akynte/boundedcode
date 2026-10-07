package cbm

import (
	"context"
	"strings"
	"testing"
)

func TestPreflightNamesTheFix(t *testing.T) {
	err := Preflight(context.Background(), "/nonexistent/codebase-memory-mcp")
	if err == nil || !strings.HasPrefix(err.Error(), "codebase-memory-mcp is not installed") || !strings.HasSuffix(err.Error(), "setup --only tools`") {
		t.Fatalf("missing: %v", err)
	}
	c := fakeClient(t, "FAKE_CBM_VERSION", "0.12.1")
	err = Preflight(context.Background(), c.Binary)
	if err == nil || !strings.Contains(err.Error(), "0.12.1") || !strings.Contains(err.Error(), "is not the supported "+RequiredVersion) ||
		!strings.HasSuffix(err.Error(), "setup --only tools`") || strings.Contains(err.Error(), "install-deps.sh") {
		t.Fatalf("wrong version: %v", err)
	}
	t.Setenv("FAKE_CBM_VERSION", RequiredVersion)
	if err := Preflight(context.Background(), c.Binary); err != nil {
		t.Fatal(err)
	}
}
