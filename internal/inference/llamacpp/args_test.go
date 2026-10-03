package llamacpp

import (
	"slices"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/model"
)

func TestBuildArgs(t *testing.T) {
	p := model.Profile{Name: "m", File: "m.gguf"}
	p.Server = model.ServerFlags{CtxSize: 65536, GPULayers: 999, NCPUMoE: 30, FlashAttn: "on", CacheTypeK: "q8_0", Jinja: true}
	p.Sampling.Temperature = 0.6
	args := BuildArgs(p, "/models/m.gguf", "127.0.0.1", 8765)
	joined := strings.Join(args, " ")
	for _, want := range []string{"--model /models/m.gguf", "--alias m", "--port 8765", "--ctx-size 65536", "--n-cpu-moe 30", "--flash-attn on", "--cache-type-k q8_0", "--jinja", "--temp 0.6", "--metrics"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %q", want, joined)
		}
	}
	if slices.Contains(args, "--cache-type-v") || slices.Contains(args, "--mlock") {
		t.Errorf("unset flags emitted: %q", joined)
	}
	if ArgsHash("b", args) == ArgsHash("b", append(args, "--x")) {
		t.Error("hash must change with args")
	}
}

func TestClassifyExit(t *testing.T) {
	m := &Manager{StateDir: t.TempDir()}
	if err := writeFile(m.logPath(), "load...\nggml_backend_cuda_buffer_type_alloc_buffer: allocating 9000 MiB on device 0: cudaMalloc failed: out of memory\n"); err != nil {
		t.Fatal(err)
	}
	err := m.classifyExit(nil)
	if err == nil || !strings.Contains(err.Error(), "out of memory") {
		t.Fatalf("got %v", err)
	}
}
