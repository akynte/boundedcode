package judgment

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// These tests pin one boundary: where the Jev credential is required.
//
// The defect they exist for is that `enabled: true` with no key exported made
// Config.Validate fail, and Validate is reached by every command that opens a
// data directory — so a run that never consults the judge died during
// configuration loading. Enabled describes the file. Whether a key is exported
// describes the environment, and only the paths that actually build a request
// are entitled to insist on it.

// keylessConfig is the configuration at the centre of the defect: complete,
// enabled, and with nothing exported for api_key_env.
func keylessConfig(t *testing.T) Config {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Endpoint = "https://example.invalid/v1"
	cfg.Model = "jev-1.13.0"
	cfg.APIKeyEnv = "BC_TEST_JUDGMENT_KEY"
	t.Setenv(cfg.APIKeyEnv, "")
	return cfg
}

// Cases 1 and 2. A configuration that is enabled but keyless is a valid
// configuration, so every workflow that does not consult the judge gets past
// startup. Validate is the function those workflows reach.
func TestEnabledWithoutCredentialIsAValidConfiguration(t *testing.T) {
	cfg := keylessConfig(t)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("a keyless but otherwise complete configuration was rejected: %v", err)
	}
	if cfg.HasCredential() {
		t.Fatal("HasCredential reported a key that is not exported")
	}
}

// The same configuration must also build. New is what supervisor.Judge calls
// on behalf of every `bcode eval` invocation, whatever arm was selected, so a
// refusal here is the defect reappearing one layer down.
func TestNewSucceedsWithoutCredentialAndReportsUnavailable(t *testing.T) {
	cfg := keylessConfig(t)
	j, err := New(cfg, Deps{})
	if err != nil {
		t.Fatalf("New refused a keyless configuration: %v", err)
	}
	if j == nil {
		t.Fatal("New returned no judge and no error")
	}
	// Unavailable rather than absent: the arm gate in internal/eval asks
	// Available(), and answering true here would let a rerank arm run without
	// the reranker it is named after.
	if j.Available() {
		t.Fatal("a judge with no credential reported itself available")
	}
}

// Structural faults are still faults. Dropping the key check must not have
// turned Validate into a function that accepts anything.
func TestValidateStillRejectsStructuralFaults(t *testing.T) {
	for name, mutate := range map[string]func(*Config){
		"no endpoint":    func(c *Config) { c.Endpoint = "" },
		"no model":       func(c *Config) { c.Model = "" },
		"no api_key_env": func(c *Config) { c.APIKeyEnv = "" },
		"bad redact":     func(c *Config) { c.Redact = RedactMode("sideways") },
		"bad confidence": func(c *Config) { c.MinConfidence = 1.5 },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := keylessConfig(t)
			mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}
}

// Case 3, at the boundary the arm gate and the smoke test share. A caller that
// has decided it needs Jev is refused, and told which of the two reasons
// applies.
func TestRequireCredentialFailsClosedWithoutKey(t *testing.T) {
	cfg := keylessConfig(t)
	err := cfg.RequireCredential()
	if err == nil {
		t.Fatal("RequireCredential accepted a configuration with no key")
	}
	if !errors.Is(err, ErrNoCredential) {
		t.Fatalf("error is not ErrNoCredential: %v", err)
	}
	// The message has to name the variable, or the operator is told only that
	// something is missing.
	if !strings.Contains(err.Error(), cfg.APIKeyEnv) {
		t.Fatalf("the refusal does not name %s: %v", cfg.APIKeyEnv, err)
	}
}

// Case 4. With the key exported the configuration is accepted and the judge
// reports itself usable.
func TestCredentialPresentIsAccepted(t *testing.T) {
	cfg := keylessConfig(t)
	t.Setenv(cfg.APIKeyEnv, "test-key")

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate rejected a fully configured judgment: %v", err)
	}
	if !cfg.HasCredential() {
		t.Fatal("HasCredential did not see the exported key")
	}
	if err := cfg.RequireCredential(); err != nil {
		t.Fatalf("RequireCredential refused an exported key: %v", err)
	}
	j, err := New(cfg, Deps{})
	if err != nil {
		t.Fatalf("New refused a fully configured judgment: %v", err)
	}
	if !j.Available() {
		t.Fatal("a fully configured judge reported itself unavailable")
	}
}

// A disabled integration has no credential to require. RequireCredential is
// only ever reached by a caller that wants the judge, so "off" is a refusal
// there rather than a quiet success.
func TestRequireCredentialRefusesWhenDisabled(t *testing.T) {
	cfg := keylessConfig(t)
	cfg.Enabled = false
	if err := cfg.RequireCredential(); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("a disabled integration did not refuse with ErrNoCredential: %v", err)
	}
	if cfg.HasCredential() {
		t.Fatal("a disabled integration reported a credential")
	}
}

// Case 5. The smoke test's entire purpose is one live round trip, so it has no
// unauthenticated mode. It must refuse before opening a socket, and say why.
func TestSmokeFailsClosedWithoutCredential(t *testing.T) {
	cfg := keylessConfig(t)
	j, err := New(cfg, Deps{})
	if err != nil {
		t.Fatalf("New refused a keyless configuration: %v", err)
	}
	_, err = Smoke(context.Background(), j)
	if err == nil {
		t.Fatal("Smoke ran without a credential")
	}
	if !errors.Is(err, ErrNoCredential) {
		t.Fatalf("Smoke did not refuse with ErrNoCredential: %v", err)
	}
}
