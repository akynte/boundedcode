package telemetry

import "testing"

func TestAddSecretRedactsExactValue(t *testing.T) {
	odd := "provider-key-without-a-known-shape-0123"
	if got := Redact("key " + odd); got != "key "+odd {
		t.Fatalf("unregistered value changed: %q", got)
	}
	AddSecret(odd)
	AddSecret("short")
	if got := Redact("key " + odd + " short"); got != "key [REDACTED] short" {
		t.Fatalf("Redact = %q", got)
	}
}
