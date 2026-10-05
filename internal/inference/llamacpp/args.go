// Package llamacpp supervises llama.cpp's llama-server as an external process.
package llamacpp

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/model"
)

// BuildArgs maps a model profile to llama-server flags. Flag names were
// verified against llama-server --help of the pinned release; see
// docs/architecture/upstream-components.md.
func BuildArgs(p model.Profile, modelPath, host string, port int) []string {
	s := p.Server
	args := []string{
		"--model", modelPath,
		"--alias", p.Name,
		"--host", host,
		"--port", strconv.Itoa(port),
		"--metrics",
		"--no-webui",
	}
	addInt := func(flag string, v int) {
		if v != 0 {
			args = append(args, flag, strconv.Itoa(v))
		}
	}
	addInt("--ctx-size", s.CtxSize)
	addInt("--n-gpu-layers", s.GPULayers)
	addInt("--n-cpu-moe", s.NCPUMoE)
	addInt("--batch-size", s.BatchSize)
	addInt("--ubatch-size", s.UBatchSize)
	addInt("--threads", s.Threads)
	addInt("--parallel", s.Parallel)
	addInt("--cache-reuse", s.CacheReuse)
	if s.FlashAttn != "" {
		args = append(args, "--flash-attn", s.FlashAttn)
	}
	if s.CacheTypeK != "" {
		args = append(args, "--cache-type-k", s.CacheTypeK)
	}
	if s.CacheTypeV != "" {
		args = append(args, "--cache-type-v", s.CacheTypeV)
	}
	if s.Jinja {
		args = append(args, "--jinja")
	}
	if s.Mlock {
		args = append(args, "--mlock")
	}
	if s.NoMmap {
		args = append(args, "--no-mmap")
	}
	if s.Reasoning != "" {
		args = append(args, "--reasoning", s.Reasoning)
	}
	if s.ReasoningBudget != 0 {
		args = append(args, "--reasoning-budget", strconv.Itoa(s.ReasoningBudget))
		if s.ReasoningBudgetMessage != "" {
			args = append(args, "--reasoning-budget-message", s.ReasoningBudgetMessage)
		}
	}
	// Sampling defaults are set server-side so every client (including the
	// agent runtime) gets the profile's recommended values.
	smp := p.Sampling
	if smp.Temperature > 0 {
		args = append(args, "--temp", ftoa(smp.Temperature))
	}
	if smp.TopP > 0 {
		args = append(args, "--top-p", ftoa(smp.TopP))
	}
	if smp.TopK > 0 {
		args = append(args, "--top-k", strconv.Itoa(smp.TopK))
	}
	if smp.MinP > 0 {
		args = append(args, "--min-p", ftoa(smp.MinP))
	}
	if smp.PresencePenalty != 0 {
		args = append(args, "--presence-penalty", ftoa(smp.PresencePenalty))
	}
	if smp.RepeatPenalty != 0 {
		args = append(args, "--repeat-penalty", ftoa(smp.RepeatPenalty))
	}
	return append(args, s.ExtraArgs...)
}

// WithIdleSleep appends --sleep-idle-seconds when d is at least one second.
// It is a server runtime setting, not part of a model profile.
func WithIdleSleep(args []string, d time.Duration) []string {
	if secs := int(d / time.Second); secs > 0 {
		return append(args, "--sleep-idle-seconds", strconv.Itoa(secs))
	}
	return args
}

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// ArgsHash identifies a server configuration so a running server can be
// reused only when it was started with identical arguments.
func ArgsHash(binary string, args []string) string {
	h := sha256.Sum256([]byte(binary + "\x00" + strings.Join(args, "\x00")))
	return hex.EncodeToString(h[:8])
}
