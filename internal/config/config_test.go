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
