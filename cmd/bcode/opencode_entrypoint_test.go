package main

import (
	"strings"
	"testing"
)

func TestBareOpenCodeIsTheManagedSessionEntrypoint(t *testing.T) {
	cmd := newOpenCodeCmd()
	if cmd.Short != "Start OpenCode with a managed BoundedCode session" {
		t.Fatalf("bare bcode opencode is not the managed session entrypoint: %q", cmd.Short)
	}
	if !strings.Contains(cmd.Long, "automatically") {
		// The lifecycle wording is part of the user contract, not just help
		// text; keep this test from silently accepting the old setup-only path.
		t.Fatalf("opencode help does not explain automatic lifecycle management:\n%s", cmd.Long)
	}
}
