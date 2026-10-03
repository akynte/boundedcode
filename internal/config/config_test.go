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
