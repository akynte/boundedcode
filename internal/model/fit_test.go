package model

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/akynte/boundedcode/configs"
	"github.com/akynte/boundedcode/internal/hw"
)

func builtinCatalog(t *testing.T) Catalog {
	t.Helper()
	c, err := LoadCatalog(configs.FS, "models", "")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestRecommend checks the model proposed for representative machines.
func TestRecommend(t *testing.T) {
	c := builtinCatalog(t)
	cuda := func(vram, ram int) hw.Snapshot {
		return hw.Snapshot{MemTotalMiB: ram, Accelerator: hw.Accelerator{Kind: hw.AccelCUDA, MemoryMiB: vram}}
	}
	mac := func(ram int) hw.Snapshot {
		mem := ram * 2 / 3
		return hw.Snapshot{MemTotalMiB: ram, Accelerator: hw.Accelerator{Kind: hw.AccelMetal, MemoryMiB: mem, Unified: true}}
	}
	cpu := func(ram int) hw.Snapshot {
		return hw.Snapshot{MemTotalMiB: ram, Accelerator: hw.Accelerator{Kind: hw.AccelNone}}
	}
	for _, tc := range []struct {
		name    string
		machine hw.Snapshot
		want    string
		level   string
	}{
		{"reference: 8 GB GPU, 64 GB RAM", cuda(8188, 64028), "qwen3.6-35b-a3b", FitOffload},
		{"24 GB GPU", cuda(24576, 32768), "qwen3.6-35b-a3b", FitGPU},
		{"12 GB GPU, 32 GB RAM", cuda(12288, 32768), "qwen3.6-35b-a3b", FitOffload},
		{"16 GB Mac", mac(16384), "qwen3.5-9b", FitGPU},
		{"8 GB laptop, no GPU", cpu(8192), "qwen3.5-4b", FitCPU},
		{"4 GB GPU, 16 GB RAM", cuda(4096, 16384), "qwen3.5-4b", FitSplit},
		{"32 GB RAM, no GPU", cpu(32768), "qwen3.5-4b", FitCPU},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Recommend(c, "qwen3.6-35b-a3b", tc.machine)
			if r.Best.Profile != tc.want || r.Best.Level != tc.level {
				t.Fatalf("recommended %s (%s): %s; want %s (%s)\nfits: %+v", r.Best.Profile, r.Best.Level, r.Reason, tc.want, tc.level, r.Fits)
			}
			for _, f := range r.Fits {
				if f.Profile == "laguna-xs-2.1" && r.Best.Profile == f.Profile {
					t.Fatal("a profile under license review was recommended")
				}
			}
		})
	}
	if r := Recommend(c, "qwen3.6-35b-a3b", cpu(3072)); r.Best.Profile != "" {
		t.Fatalf("a 3 GB machine got %s", r.Best.Profile)
	}
}

func TestFitUnknownSize(t *testing.T) {
	f := FitFor(Profile{Name: "x"}, hw.Snapshot{MemTotalMiB: 65536})
	if f.Level != FitUnknown || f.Usable() {
		t.Fatalf("fit = %+v", f)
	}
}

// TestUserOverrideKeepsCatalog: a `bench infra --apply` override keeps the
// built-in profile's revision, checksum and status for the same weights,
// but an override for other weights inherits nothing.
func TestUserOverrideKeepsCatalog(t *testing.T) {
	dir := t.TempDir()
	tuned := "name: qwen3.6-35b-a3b\ndisplay_name: tuned\nfile: Qwen3.6-35B-A3B-UD-Q4_K_M.gguf\nsource:\n  repo: unsloth/Qwen3.6-35B-A3B-GGUF\n  file: Qwen3.6-35B-A3B-UD-Q4_K_M.gguf\n  revision: \"\"\n  license: apache-2.0\nserver:\n  ctx_size: 65536\n"
	other := "name: qwen3.5-4b\ndisplay_name: mine\nfile: other.gguf\nsource:\n  repo: me/other\n  file: other.gguf\n"
	for name, body := range map[string]string{"a.yaml": tuned, "b.yaml": other} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c, err := LoadCatalog(configs.FS, "models", dir)
	if err != nil {
		t.Fatal(err)
	}
	q := c["qwen3.6-35b-a3b"]
	if q.Status != StatusValidated || q.Source.SHA256 == "" || q.Source.Revision == "" || q.Server.CtxSize != 65536 || q.DisplayName != "tuned" {
		t.Fatalf("override = %+v", q)
	}
	if o := c["qwen3.5-4b"]; o.Source.SHA256 != "" || o.Status != "" {
		t.Fatalf("override of other weights inherited: %+v", o)
	}
}
