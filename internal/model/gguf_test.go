package model

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/akynte/boundedcode/configs"
)

// ggufFile writes a minimal GGUF v3 header with the given key/values
// (string, uint32, or []string arrays).
func ggufFile(t *testing.T, kv [][2]any) []byte {
	t.Helper()
	var b bytes.Buffer
	w := func(v any) {
		if err := binary.Write(&b, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	str := func(s string) { w(uint64(len(s))); b.WriteString(s) }
	b.WriteString("GGUF")
	w(uint32(3))
	w(uint64(7)) // tensors
	w(uint64(len(kv)))
	for _, e := range kv {
		str(e[0].(string))
		switch v := e[1].(type) {
		case string:
			w(uint32(8))
			str(v)
		case uint32:
			w(uint32(4))
			w(v)
		case float32:
			w(uint32(6))
			w(v)
		case []string:
			w(uint32(9))
			w(uint32(8))
			w(uint64(len(v)))
			for _, s := range v {
				str(s)
			}
		}
	}
	return b.Bytes()
}

func TestReadGGUFMeta(t *testing.T) {
	data := ggufFile(t, [][2]any{
		{"general.architecture", "qwen35moe"},
		{"general.name", "Test"},
		{"qwen35moe.block_count", uint32(40)},
		{"qwen35moe.context_length", uint32(262144)},
		{"qwen35moe.rope.freq_base", float32(1e7)},
		{"tokenizer.ggml.tokens", []string{"a", "b", "c"}},
		{"tokenizer.ggml.pre", "qwen35"},
		{"qwen35moe.nextn_predict_layers", uint32(1)},
	})
	m, err := readGGUFMeta(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if m.Architecture != "qwen35moe" || m.Tokenizer != "qwen35" || m.ContextLength != 262144 || m.Blocks != 40 || m.MTPLayers != 1 || m.Tensors != 7 {
		t.Fatalf("meta = %+v", m)
	}
	if _, err := readGGUFMeta(bytes.NewReader([]byte("GGML0000000000000000000000"))); err == nil {
		t.Fatal("not GGUF accepted")
	}
	c := Capabilities{Tokenizer: "qwen35", ContextMax: 262144, Speculative: []string{"ngram", "draft"}}
	if d := CheckDeclared(c, m); len(d) != 1 || !slices.ContainsFunc(d, func(s string) bool { return bytes.Contains([]byte(s), []byte("MTP layer")) }) {
		t.Fatalf("undeclared MTP layers not reported: %q", d)
	}
	c.Speculative = append(c.Speculative, "mtp")
	if d := CheckDeclared(c, m); len(d) != 0 {
		t.Fatalf("agreement reported: %q", d)
	}
	m.MTPLayers, c.Tokenizer = 0, "qwen2"
	if d := CheckDeclared(c, m); len(d) != 2 {
		t.Fatalf("two disagreements expected: %q", d)
	}
}

// TestBuiltinProfilesMatchLocalWeights checks the declared capabilities of
// every profile whose weights are on this machine.
func TestBuiltinProfilesMatchLocalWeights(t *testing.T) {
	builtin, err := LoadCatalog(configs.FS, "models", "")
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, name := range builtin.Names() {
		p := builtin[name]
		path := p.ResolveFile(DataModelsDir())
		if _, err := os.Stat(path); err != nil {
			continue
		}
		found++
		m, err := ReadGGUFMeta(path)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if d := CheckDeclared(p.Capabilities, m); len(d) > 0 {
			t.Errorf("%s (%s): %q", name, filepath.Base(path), d)
		}
	}
	if found == 0 {
		t.Skip("no model weights on this machine")
	}
}
