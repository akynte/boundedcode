package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsValid(t *testing.T) {
	if err := Defaults().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMissingIsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Inference.Port != Defaults().Inference.Port {
		t.Fatal("defaults not applied")
	}
}

func TestLoadRejectsUnknownAndInvalid(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"unknown": "version: 1\ninference:\n  prot: 1\n",
		"mode":    "version: 1\ninference:\n  mode: cloud\n",
		"dur":     "version: 1\ninference:\n  startup_timeout: soon\n",
		"version": "version: 9\n",
		// Serena is pinned to the MIT-licensed 1.7.0; no other version,
		// automatic upgrades or network transports are accepted.
		"serena-v2":        "version: 1\nrepointel:\n  serena:\n    version: \"2.0.0\"\n",
		"serena-upgrade":   "version: 1\nrepointel:\n  serena:\n    auto_upgrade: true\n",
		"serena-http":      "version: 1\nrepointel:\n  serena:\n    transport: streamable-http\n",
		"serena-instances": "version: 1\nrepointel:\n  serena:\n    max_instances: 0\n",
		"serena-timeout":   "version: 1\nrepointel:\n  serena:\n    enabled: true\n    call_timeout: 0s\n",
		"engine":           "version: 1\nsandbox:\n  engine: nerdctl\n",
		"iterations":       "version: 1\nagent:\n  max_iterations: 0\n",
		"runtime":          "version: 1\nagent:\n  runtime: scripted\n",
		"wall-clock":       "version: 1\nbudgets:\n  max_wall_clock: -1h\n",
		"local-tokens":     "version: 1\nbudgets:\n  max_local_tokens: -1\n",
		"escalations":      "version: 1\nbudgets:\n  max_escalations: -1\n",
		"packet":           "version: 1\nfrontier:\n  max_packet_tokens: 0\n",
		"idle-sleep":       "version: 1\ninference:\n  idle_sleep: -5m\n",
		"provider":         "version: 1\nrepointel:\n  provider: sourcegraph\n",
	}
	for name, body := range cases {
		p := filepath.Join(dir, name+".yaml")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestSerenaEnabledLoads(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	body := "version: 1\nrepointel:\n  serena:\n    enabled: true\n    idle_timeout: 2m\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	s := c.RepoIntel.Serena
	if !s.Enabled || s.Version != SerenaVersion || s.Transport != "stdio" || s.IdleTimeout.D().Minutes() != 2 {
		t.Fatalf("%+v", s)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c", "config.yaml")
	cfg := Defaults()
	cfg.Inference.Mode = "external"
	cfg.Inference.ExternalURL = "http://127.0.0.1:9999"
	if err := Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Inference.ExternalURL != cfg.Inference.ExternalURL || got.Budgets.MaxWallClock != cfg.Budgets.MaxWallClock {
		t.Fatalf("round trip mismatch: %+v", got.Inference)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v", fi.Mode().Perm())
	}
}

func TestRuntimeDirSharedAcrossHomes(t *testing.T) {
	t.Setenv("BOUNDEDCODE_HOME", t.TempDir())
	a, err := DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("BOUNDEDCODE_HOME", t.TempDir())
	b, _ := DefaultPaths()
	if a.Runtime != b.Runtime || a.Data == b.Data {
		t.Fatalf("runtime must be machine-wide, data per home: %+v %+v", a, b)
	}
}

// TestLegacyKeysAccepted: keys written by earlier releases still load.
func TestLegacyKeysAccepted(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	body := "version: 1\nrepointel:\n  provider: codebase-memory-mcp\nsandbox:\n  engine: podman\ninference:\n  idle_sleep: 0s\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err != nil {
		t.Fatal(err)
	}
}

func TestSerenaTimeoutsIgnoredWhenDisabled(t *testing.T) {
	c := Defaults()
	c.RepoIntel.Serena.CallTimeout = 0
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceOverride(t *testing.T) {
	paths := Paths{Config: t.TempDir()}
	user := "version: 1\nbudgets:\n  max_attempts: 4\n  max_escalations: 1\nfrontier:\n  enabled: true\n"
	if err := os.WriteFile(filepath.Join(paths.Config, "config.yaml"), []byte(user), 0o600); err != nil {
		t.Fatal(err)
	}
	// No override file: the user config.
	c, err := LoadForWorkspace(paths, "payment-platform")
	if err != nil || c.Budgets.MaxAttempts != 4 || !c.Frontier.Enabled {
		t.Fatalf("%+v %v", c.Budgets, err)
	}
	wsFile := WorkspaceFile(paths.Config, "payment-platform")
	_ = os.MkdirAll(filepath.Dir(wsFile), 0o700)
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(wsFile, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("budgets:\n  max_attempts: 9\nescalation:\n  high_risk_review: [ledger]\nrepointel:\n  serena:\n    enabled: true\nfrontier:\n  enabled: false\n")
	c, err = LoadForWorkspace(paths, "payment-platform")
	if err != nil {
		t.Fatal(err)
	}
	d := Defaults()
	switch {
	case c.Budgets.MaxAttempts != 9: // workspace beats user
		t.Fatalf("max_attempts = %d", c.Budgets.MaxAttempts)
	case c.Budgets.MaxEscalations != 1: // user beats default, kept when not overridden
		t.Fatalf("max_escalations = %d", c.Budgets.MaxEscalations)
	case c.Budgets.ContextPackTokens != d.Budgets.ContextPackTokens:
		t.Fatalf("context_pack_tokens = %d", c.Budgets.ContextPackTokens)
	case len(c.Escalation.HighRiskReview) != 1 || len(c.Escalation.ArchitecturalRisk) != len(d.Escalation.ArchitecturalRisk):
		t.Fatalf("escalation = %+v", c.Escalation)
	case !c.RepoIntel.Serena.Enabled || c.Frontier.Enabled || !c.RepoIntel.CrossService:
		t.Fatalf("switches: serena=%v frontier=%v cross=%v", c.RepoIntel.Serena.Enabled, c.Frontier.Enabled, c.RepoIntel.CrossService)
	}
	// Other workspaces are unaffected.
	if c, _ := LoadForWorkspace(paths, "other"); c.Budgets.MaxAttempts != 4 {
		t.Fatal("override applied to another workspace")
	}
	for name, body := range map[string]string{
		"machine setting": "inference:\n  port: 1\n",
		"unknown key":     "budgets:\n  max_atempts: 3\n",
		"invalid value":   "budgets:\n  max_attempts: 0\n",
		"model switch":    "repointel:\n  serena:\n    version: 2.0.0\n",
	} {
		write(body)
		if _, err := LoadForWorkspace(paths, "payment-platform"); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	for _, bad := range []string{"", "../x", "a/b", ".hidden"} {
		if _, err := Defaults().WithWorkspace(paths.Config, bad); err == nil {
			t.Errorf("workspace name %q accepted", bad)
		}
	}
}
