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

func TestCapabilitiesAndSpeculative(t *testing.T) {
	c, err := LoadCatalog(configs.FS, "models", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range c.Names() {
		if c[name].Capabilities.zero() {
			t.Errorf("%s declares no capabilities", name)
		}
	}
	bad := map[string]string{
		"unknown type":       "speculative: {type: medusa}",
		"draft without file": "speculative: {type: draft-simple}",
		"file without draft": "speculative: {type: ngram-mod, draft_file: d.gguf}",
		"n_min above n_max":  "speculative: {type: ngram-mod, n_max: 2, n_min: 4}",
		"not allowed":        "speculative: {type: draft-mtp}",
	}
	for why, server := range bad {
		y := "name: x\nfile: x.gguf\ncapabilities: {speculative: [ngram, draft]}\nserver:\n  " + server + "\n"
		if _, err := LoadCatalog(fstest.MapFS{"m/x.yaml": {Data: []byte(y)}}, "m", ""); err == nil {
			t.Errorf("%s: accepted", why)
		}
	}
	ok := "name: x\nfile: x.gguf\ncapabilities: {speculative: [ngram, draft]}\nserver:\n  speculative: {type: draft-simple, draft_file: d.gguf, n_max: 8}\n"
	got, err := LoadCatalog(fstest.MapFS{"m/x.yaml": {Data: []byte(ok)}}, "m", "")
	if err != nil {
		t.Fatal(err)
	}
	if d := got["x"].ResolveDraft("/models"); d != filepath.Join("/models", "d.gguf") {
		t.Fatalf("draft path %q", d)
	}
}

func TestOverrideKeepsCapabilities(t *testing.T) {
	builtin := fstest.MapFS{"m/a.yaml": {Data: []byte("name: a\nfile: a.gguf\nsource: {repo: r, file: a.gguf}\ncapabilities: {tool_calling: native, tokenizer: qwen35}\n")}}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte("name: a\nfile: a.gguf\nsource: {repo: r, file: a.gguf}\nserver: {ctx_size: 4096}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadCatalog(builtin, "m", dir)
	if err != nil {
		t.Fatal(err)
	}
	if c["a"].Capabilities.Tokenizer != "qwen35" || c["a"].Server.CtxSize != 4096 {
		t.Fatalf("override = %+v", c["a"])
	}
}
