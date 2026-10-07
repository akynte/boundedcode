package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/akynte/boundedcode/internal/telemetry"
)

func TestFileFallback(t *testing.T) {
	dir := t.TempDir()
	s := &Store{File: filepath.Join(dir, "credentials.json"), NoKeyring: true}
	if _, _, err := s.Get("anthropic"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty store: %v", err)
	}
	if _, err := s.Set("anthropic", "sk-ant-test\n0123456789"); err == nil {
		t.Fatal("a multi-line key must be refused")
	}
	// Surrounding whitespace from a paste is trimmed.
	if src, err := s.Set("anthropic", "  sk-ant-test-0123456789 \n"); err != nil || src != SourceFile {
		t.Fatalf("set: %s %v", src, err)
	}
	fi, err := os.Stat(s.File)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("credentials file mode %v err %v", fi.Mode(), err)
	}
	v, src, err := s.Get("anthropic")
	if err != nil || v != "sk-ant-test-0123456789" || src != SourceFile {
		t.Fatalf("get: %q %s %v", v, src, err)
	}
	if got := telemetry.Redact("using sk-ant-test-0123456789"); strings.Contains(got, "0123456789") {
		t.Fatalf("stored key not redacted: %q", got)
	}
	if err := s.Delete("anthropic"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.File); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty credentials file left behind: %v", err)
	}
}

func TestEnvironmentOverride(t *testing.T) {
	s := &Store{File: filepath.Join(t.TempDir(), "c.json"), NoKeyring: true}
	if _, err := s.Set("openai-compatible", "stored-key-0123456789"); err != nil {
		t.Fatal(err)
	}
	// Fake keys, assembled so secret scanners do not flag the source.
	t.Setenv("BOUNDEDCODE_OPENAI_COMPATIBLE_"+"API_KEY", "env-key-"+"0123456789")
	t.Setenv("OPENAI_API_KEY", "vendor-variable-is-ignored")
	v, src, err := s.Get("openai-compatible")
	if err != nil || v != "env-key-0123456789" || src != SourceEnv {
		t.Fatalf("get: %q %s %v", v, src, err)
	}
}

func TestKeyring(t *testing.T) {
	keyring.MockInit()
	s := &Store{File: filepath.Join(t.TempDir(), "c.json")}
	// A key in the file from an earlier fallback moves to the keyring.
	fileOnly := &Store{File: s.File, NoKeyring: true}
	if _, err := fileOnly.Set("gemini", "old-file-key-0123456789"); err != nil {
		t.Fatal(err)
	}
	src, err := s.Set("gemini", "AIza-keyring-0123456789")
	if err != nil || src != SourceKeyring {
		t.Fatalf("set: %s %v", src, err)
	}
	if names := s.Names(); len(names) != 0 {
		t.Fatalf("file still holds %v", names)
	}
	v, src, err := s.Get("gemini")
	if err != nil || v != "AIza-keyring-0123456789" || src != SourceKeyring {
		t.Fatalf("get: %q %s %v", v, src, err)
	}
	if err := s.Delete("gemini"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.Status("gemini"); ok {
		t.Fatal("key still present after delete")
	}
}
