package model

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/akynte/boundedcode/configs"
)

func TestBuiltinProfilesValid(t *testing.T) {
	c, err := LoadCatalog(configs.FS, "models", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get("qwen3.6-35b-a3b"); err != nil {
		t.Fatal(err)
	}
}

func TestUserOverrideAndStrictness(t *testing.T) {
	builtin := fstest.MapFS{"m/a.yaml": {Data: []byte("name: a\nfile: a.gguf\n")}}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte("name: a\nfile: /abs/b.gguf\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadCatalog(builtin, "m", dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := c["a"].ResolveFile("/models"); got != "/abs/b.gguf" {
		t.Fatalf("override not applied: %s", got)
	}
	bad := fstest.MapFS{"m/x.yaml": {Data: []byte("name: x\nfile: x.gguf\nunknown_key: 1\n")}}
	if _, err := LoadCatalog(bad, "m", ""); err == nil {
		t.Fatal("expected unknown key to be rejected")
	}
	nongguf := fstest.MapFS{"m/x.yaml": {Data: []byte("name: x\nfile: x.safetensors\n")}}
	if _, err := LoadCatalog(nongguf, "m", ""); err == nil {
		t.Fatal("expected non-gguf to be rejected")
	}
}
