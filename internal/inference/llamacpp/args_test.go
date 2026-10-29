package llamacpp

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/akynte/boundedcode/configs"
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

func TestReasoningBudgetArgs(t *testing.T) {
	p := model.Profile{Name: "m", File: "m.gguf"}
	if j := strings.Join(BuildArgs(p, "/m", "127.0.0.1", 1), " "); strings.Contains(j, "--reasoning-budget") {
		t.Fatalf("unset budget emitted: %q", j)
	}
	p.Server.ReasoningBudget, p.Server.ReasoningBudgetMessage = 4096, "Acting now."
	args := BuildArgs(p, "/m", "127.0.0.1", 1)
	i := slices.Index(args, "--reasoning-budget")
	if i < 0 || args[i+1] != "4096" || !slices.Contains(args, "--reasoning-budget-message") || !slices.Contains(args, "Acting now.") {
		t.Fatalf("budget args: %q", args)
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

func TestIdleSleepArgs(t *testing.T) {
	base := []string{"--model", "m"}
	if got := WithIdleSleep(base, 0); !slices.Equal(got, base) {
		t.Errorf("zero must not add flags: %q", got)
	}
	if got := WithIdleSleep(base, 500*time.Millisecond); !slices.Equal(got, base) {
		t.Errorf("sub-second must not add flags: %q", got)
	}
	got := WithIdleSleep(slices.Clone(base), 30*time.Minute)
	if !slices.Equal(got, []string{"--model", "m", "--sleep-idle-seconds", "1800"}) {
		t.Errorf("got %q", got)
	}
	p := model.Profile{Name: "m", File: "m.gguf"}
	m := &Manager{Host: "127.0.0.1", Port: 1, IdleSleep: time.Minute}
	if a := strings.Join(m.ServerArgs(p, "/m.gguf"), " "); !strings.HasSuffix(a, "--sleep-idle-seconds 60") {
		t.Errorf("ServerArgs = %q", a)
	}
	m.IdleSleep = 0
	if slices.Contains(m.ServerArgs(p, "/m.gguf"), "--sleep-idle-seconds") {
		t.Error("disabled idle sleep emitted a flag")
	}
}

// Prompt-cache settings map to the pinned release's flags only when set
// (0 is a value for checkpoint spacing and the RAM cache), and the offload
// threshold goes to the environment.
func TestCacheSettingsArgs(t *testing.T) {
	p := model.Profile{Name: "m", File: "m.gguf"}
	plain := strings.Join(BuildArgs(p, "/m", "127.0.0.1", 1), " ")
	for _, f := range []string{"--ctx-checkpoints", "--checkpoint-min-step", "--cache-ram", "reasoning-preserve"} {
		if strings.Contains(plain, f) {
			t.Fatalf("unset %s emitted: %q", f, plain)
		}
	}
	if env := ServerEnv(p); len(env) != 0 {
		t.Fatalf("env %q", env)
	}
	zero, off := 0, false
	p.Server.CtxCheckpoints, p.Server.CheckpointMinStep, p.Server.CacheRAMMiB, p.Server.ReasoningPreserve = 16, &zero, &zero, &off
	p.Server.OpOffloadMinBatch = 256
	j := strings.Join(BuildArgs(p, "/m", "127.0.0.1", 1), " ")
	for _, want := range []string{"--ctx-checkpoints 16", "--checkpoint-min-step 0", "--cache-ram 0", "--no-reasoning-preserve"} {
		if !strings.Contains(j, want) {
			t.Errorf("missing %q in %q", want, j)
		}
	}
	if env := ServerEnv(p); len(env) != 1 || env[0] != "GGML_OP_OFFLOAD_MIN_BATCH=256" {
		t.Fatalf("env %q", env)
	}
}

// The measured offload threshold reaches the built-in profile's server.
func TestBuiltinOffloadThreshold(t *testing.T) {
	c, err := model.LoadCatalog(configs.FS, "models", "")
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.Get("qwen3.6-35b-a3b")
	if err != nil {
		t.Fatal(err)
	}
	if env := ServerEnv(p); !slices.Contains(env, "GGML_OP_OFFLOAD_MIN_BATCH=256") {
		t.Fatalf("env %q", env)
	}
}

func TestSpeculativeArgs(t *testing.T) {
	p := model.Profile{Name: "m", File: "m.gguf"}
	if j := strings.Join(BuildArgs(p, "/m", "127.0.0.1", 1), " "); strings.Contains(j, "--spec-") {
		t.Fatalf("speculative flags without a setting: %q", j)
	}
	p.Server.Speculative = &model.Speculative{Type: "draft-simple", DraftFile: "draft.gguf", DraftGPULayers: 99, NMax: 8}
	m := &Manager{ModelsDir: "/models", Host: "127.0.0.1", Port: 1}
	j := strings.Join(m.ServerArgs(p, "/models/m.gguf"), " ")
	for _, want := range []string{"--spec-type draft-simple", "--spec-draft-model /models/draft.gguf", "--spec-draft-ngl 99", "--spec-draft-n-max 8"} {
		if !strings.Contains(j, want) {
			t.Errorf("missing %q in %q", want, j)
		}
	}
	if p.Server.Speculative.DraftFile != "draft.gguf" {
		t.Error("ServerArgs changed the caller's profile")
	}
	p.Server.Speculative = &model.Speculative{Type: "ngram-mod"}
	if j := strings.Join(BuildArgs(p, "/m", "127.0.0.1", 1), " "); !strings.Contains(j, "--spec-type ngram-mod") || strings.Contains(j, "--spec-draft-model") {
		t.Fatalf("ngram args: %q", j)
	}
}
