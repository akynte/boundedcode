package telemetry

import (
	"context"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/store"
)

func TestRedact(t *testing.T) {
	cases := []struct{ in, mustNotContain string }{
		{"key sk-proj-abcdefghijklmnopqrstuvwxyz0123", "abcdefghijklmnop"},
		{"Authorization: Bearer abcdefghijklmnopqrstuvwxyz.123", "abcdefghijklmnopqrstuvwxyz"},
		{"token ghp_abcdefghijklmnopqrstuvwxyz0123456789", "ghp_abc"},
		// Built at runtime so secret scanners don't flag this fake fixture.
		{"AWS " + "AKIA" + "ABCDEFGHIJKLMNOP here", "ABCDEFGHIJKLMNOP"},
		{`password = "hunter2hunter2"`, "hunter2hunter2"},
		{"-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\n-----END OPENSSH PRIVATE KEY-----", "AAAA"},
	}
	for _, c := range cases {
		got := Redact(c.in)
		if strings.Contains(got, c.mustNotContain) {
			t.Errorf("Redact(%q) = %q, still contains secret", c.in, got)
		}
		if !strings.Contains(got, "[REDACTED]") {
			t.Errorf("Redact(%q) = %q, missing placeholder", c.in, got)
		}
	}
	if got := Redact("ordinary text with token_count=5"); got != "ordinary text with token_count=5" {
		t.Errorf("false positive: %q", got)
	}
}

func TestEmitAndQuery(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := New(s.DB, nil)
	r.Emit(ctx, "t1", "task.created", map[string]string{"note": "api_key=supersecretvalue"})
	r.Emit(ctx, "t2", "task.created", nil)
	evs, err := Events(ctx, s.DB, "t1", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Kind != "task.created" {
		t.Fatalf("events = %+v", evs)
	}
	if strings.Contains(string(evs[0].Data), "supersecretvalue") {
		t.Fatalf("event not redacted: %s", evs[0].Data)
	}
	var nilRec *Recorder
	nilRec.Emit(ctx, "", "noop", nil) // must not panic
}
