package bench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"time"
)

// EnvironmentSnapshot is the reproducibility context captured beside every
// result. Empty strings mean "not measured"; they are not filled with guesses.
type EnvironmentSnapshot struct {
	OS                     string `json:"os"`
	Architecture           string `json:"architecture"`
	CPU                    string `json:"cpu,omitempty"`
	CPUCount               int    `json:"cpu_count,omitempty"`
	GPU                    string `json:"gpu,omitempty"`
	MemoryMiB              int    `json:"memory_mib,omitempty"`
	Kernel                 string `json:"kernel,omitempty"`
	GoVersion              string `json:"go_version,omitempty"`
	BoundedCodeCommit      string `json:"boundedcode_commit,omitempty"`
	TargetRepositoryCommit string `json:"target_repository_commit,omitempty"`
	TargetRepositoryDirty  bool   `json:"target_repository_dirty"`
	OpenCodeVersion        string `json:"opencode_version,omitempty"`
	RuntimeVersion         string `json:"runtime_version,omitempty"`
	DirtyRepository        bool   `json:"dirty_repository"`
}

// CaptureEnvironment reads what is available without making a benchmark fail
// because nvidia-smi, git, or a version command is absent.
func CaptureEnvironment(ctx context.Context, repo string) EnvironmentSnapshot {
	e := EnvironmentSnapshot{OS: runtime.GOOS, Architecture: runtime.GOARCH, CPUCount: runtime.NumCPU(), GoVersion: runtime.Version()}
	if commit, dirty, ok := harnessBuildRevision(); ok {
		e.BoundedCodeCommit = commit
		e.DirtyRepository = dirty
	}
	if out, err := probe(ctx, "uname", "-r"); err == nil {
		e.Kernel = strings.TrimSpace(out)
	}
	if out, err := probe(ctx, "git", "-C", repo, "rev-parse", "HEAD"); err == nil {
		e.TargetRepositoryCommit = strings.TrimSpace(out)
	}
	if out, err := probe(ctx, "git", "-C", repo, "status", "--porcelain"); err == nil {
		e.TargetRepositoryDirty = strings.TrimSpace(out) != ""
	}
	if out, err := probe(ctx, "opencode", "--version"); err == nil {
		e.OpenCodeVersion = strings.TrimSpace(out)
	}
	if out, err := probe(ctx, "nvidia-smi", "--query-gpu=name", "--format=csv,noheader"); err == nil {
		e.GPU = strings.TrimSpace(out)
	}
	if body, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(body), "\n") {
			key, value, ok := strings.Cut(line, ":")
			if ok && strings.TrimSpace(key) == "model name" {
				e.CPU = strings.TrimSpace(value)
				break
			}
		}
	}
	if body, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(body), "\n") {
			if strings.HasPrefix(line, "MemTotal:") {
				var kb int
				_, _ = fmt.Sscanf(line, "MemTotal: %d kB", &kb)
				e.MemoryMiB = kb / 1024
				break
			}
		}
	}
	return e
}

func harnessBuildRevision() (commit string, dirty bool, ok bool) {
	info, available := debug.ReadBuildInfo()
	if !available || info == nil {
		return "", false, false
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			commit = strings.TrimSpace(setting.Value)
		case "vcs.modified":
			dirty = strings.EqualFold(strings.TrimSpace(setting.Value), "true")
		}
	}
	return commit, dirty, commit != ""
}

func probe(ctx context.Context, name string, args ...string) (string, error) {
	if _, err := exec.LookPath(name); err != nil {
		return "", err
	}
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	//nolint:gosec // fixed probe names and arguments
	cmd := exec.CommandContext(c, name, args...) //nolint:gosec // fixed probe names and arguments
	output := newCappedBuffer(1 << 20)
	cmd.Stdout, cmd.Stderr = output, output
	err := runProcess(c, cmd)
	return output.String(), err
}

// FingerprintInput is the common, mode-independent description of a cell. Mode
// is deliberately absent: raw and bounded results with the same input can be
// compared, while ModeFingerprint records their arm-specific additions.
type FingerprintInput struct {
	SuiteHash          string              `json:"suite_hash"`
	TaskHash           string              `json:"task_hash"`
	BaseCommit         string              `json:"base_commit"`
	Provider           string              `json:"provider"`
	Model              string              `json:"model"`
	ModelFileSHA256    string              `json:"model_file_sha256,omitempty"`
	Quantization       string              `json:"quantization,omitempty"`
	Runtime            string              `json:"runtime,omitempty"`
	ContextTokens      int                 `json:"context_tokens"`
	Temperature        *float64            `json:"temperature"`
	TopP               *float64            `json:"top_p"`
	TopK               *int                `json:"top_k"`
	Seed               *int64              `json:"seed"`
	WallClockSeconds   int                 `json:"wall_clock_seconds"`
	GenerationBudget   int                 `json:"generation_budget"`
	VerificationBudget int                 `json:"verification_budget"`
	TokenBudget        int                 `json:"token_budget"`
	Concurrency        int                 `json:"concurrency"`
	NetworkPolicy      string              `json:"network_policy"`
	Environment        EnvironmentSnapshot `json:"environment"`
	Runner             RunnerConfig        `json:"runner"`
}

// Fingerprints is persisted in a result and used by the reporter.
type Fingerprints struct {
	Common          string   `json:"common_comparability_fingerprint"`
	ModeSpecific    string   `json:"mode_fingerprint"`
	SuiteHash       string   `json:"suite_hash"`
	TaskHash        string   `json:"task_hash"`
	BaseCommit      string   `json:"base_commit"`
	ComparabilityOK bool     `json:"comparability_ok"`
	Warnings        []string `json:"warnings,omitempty"`
}

func makeFingerprintInput(s Suite, t Task, env EnvironmentSnapshot) FingerprintInput {
	limits := EffectiveLimits(t.Limits)
	return FingerprintInput{
		SuiteHash: s.Hash(), TaskHash: taskHash(t), BaseCommit: t.BaseCommit,
		Provider: s.Model.Provider, Model: s.Model.Model, ModelFileSHA256: s.Model.ModelFileSHA256,
		Quantization: s.Model.Quantization, Runtime: s.Model.Runtime,
		ContextTokens: s.Model.ContextTokens, Temperature: s.Model.Temperature,
		TopP: s.Model.TopP, TopK: s.Model.TopK, Seed: s.Model.Seed,
		WallClockSeconds: limits.WallClockSeconds, GenerationBudget: limits.MaxGenerationRequests,
		VerificationBudget: limits.MaxVerificationAttempts, TokenBudget: limits.MaxTokens,
		Concurrency: limits.Concurrency, NetworkPolicy: t.NetworkPolicy, Environment: env, Runner: s.Runner.effective(),
	}
}

// ComputeFingerprints produces the common identity and an arm-specific hash.
func ComputeFingerprints(s Suite, t Task, mode Mode, env EnvironmentSnapshot) Fingerprints {
	input := makeFingerprintInput(s, t, env)
	commonBody, _ := CanonicalJSON(input)
	type modeInput struct {
		Common string `json:"common"`
		Mode   Mode   `json:"mode"`
	}
	modeBody, _ := CanonicalJSON(modeInput{Common: string(commonSHA(commonBody)), Mode: mode})
	return Fingerprints{
		Common: string(commonSHA(commonBody)), ModeSpecific: digestBytes(modeBody),
		SuiteHash: s.Hash(), TaskHash: taskHash(t), BaseCommit: t.BaseCommit,
		ComparabilityOK: true,
	}
}

func commonSHA(body []byte) []byte { h := sha256.Sum256(body); return []byte(hex.EncodeToString(h[:])) }

// PairComparison checks the fields a paired RAW/BOUNDED result must share.
type PairComparison struct {
	OK       bool     `json:"ok"`
	Reasons  []string `json:"reasons,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// ComparePair fails closed for a known mismatch and reports unknown objective
// metadata as a warning rather than pretending it was verified.
func ComparePair(raw, bounded FingerprintInput) PairComparison {
	p := PairComparison{OK: true}
	check := func(name string, a, b any, required bool) {
		if sameFingerprintValue(a, b) {
			return
		}
		if required {
			p.OK = false
			p.Reasons = append(p.Reasons, fmt.Sprintf("%s differs: raw=%v bounded=%v", name, a, b))
		} else {
			p.Warnings = append(p.Warnings, fmt.Sprintf("%s could not be verified: raw=%v bounded=%v", name, a, b))
		}
	}
	check("suite_hash", raw.SuiteHash, bounded.SuiteHash, true)
	check("task_hash", raw.TaskHash, bounded.TaskHash, true)
	check("base_commit", raw.BaseCommit, bounded.BaseCommit, true)
	check("provider", raw.Provider, bounded.Provider, true)
	check("model", raw.Model, bounded.Model, true)
	check("quantization", raw.Quantization, bounded.Quantization, true)
	check("context_tokens", raw.ContextTokens, bounded.ContextTokens, true)
	check("temperature", raw.Temperature, bounded.Temperature, true)
	check("top_p", raw.TopP, bounded.TopP, false)
	check("top_k", raw.TopK, bounded.TopK, false)
	check("wall_clock_seconds", raw.WallClockSeconds, bounded.WallClockSeconds, true)
	check("generation_budget", raw.GenerationBudget, bounded.GenerationBudget, true)
	check("verification_budget", raw.VerificationBudget, bounded.VerificationBudget, true)
	check("token_budget", raw.TokenBudget, bounded.TokenBudget, true)
	check("concurrency", raw.Concurrency, bounded.Concurrency, true)
	check("network_policy", raw.NetworkPolicy, bounded.NetworkPolicy, true)
	check("environment", raw.Environment, bounded.Environment, true)
	check("runner", raw.Runner, bounded.Runner, true)
	if raw.ModelFileSHA256 != "" && bounded.ModelFileSHA256 != "" {
		check("model_file_sha256", raw.ModelFileSHA256, bounded.ModelFileSHA256, true)
	} else {
		p.Warnings = append(p.Warnings, "model file hash is unavailable for at least one arm")
	}
	if raw.Runtime != "" && bounded.Runtime != "" {
		check("runtime", raw.Runtime, bounded.Runtime, true)
	} else {
		p.Warnings = append(p.Warnings, "inference runtime identity is unavailable for at least one arm")
	}
	return p
}

func sameFingerprintValue(a, b any) bool {
	left, leftErr := json.Marshal(a)
	right, rightErr := json.Marshal(b)
	return leftErr == nil && rightErr == nil && string(left) == string(right)
}

// CompareResultPair checks the durable result-level common fingerprint and
// records any mismatch as non-comparable. It is used by reports and by callers
// that want to fail closed before reading a paired delta.
func CompareResultPair(raw, bounded Result) PairComparison {
	if !raw.Fingerprints.ComparabilityOK || !bounded.Fingerprints.ComparabilityOK {
		return PairComparison{OK: false, Reasons: []string{"one result has a failed comparability fingerprint"}}
	}
	if raw.Fingerprints.Common == "" || bounded.Fingerprints.Common == "" {
		return PairComparison{OK: false, Reasons: []string{"one result has no common comparability fingerprint"}}
	}
	if raw.Fingerprints.Common != bounded.Fingerprints.Common {
		return PairComparison{OK: false, Reasons: []string{fmt.Sprintf("common comparability fingerprints differ: raw=%s bounded=%s", raw.Fingerprints.Common, bounded.Fingerprints.Common)}}
	}
	p := PairComparison{OK: true}
	if raw.Official != bounded.Official || raw.Smoke != bounded.Smoke {
		p.OK = false
		p.Reasons = append(p.Reasons, "result provenance differs between paired arms")
	}
	if !raw.Evaluator.Independent || !bounded.Evaluator.Independent {
		p.OK = false
		p.Reasons = append(p.Reasons, "one or both evaluators were not independent/confined")
	}
	if raw.PairID != bounded.PairID || raw.TaskID != bounded.TaskID || raw.Repetition != bounded.Repetition || raw.Seed != bounded.Seed {
		p.OK = false
		p.Reasons = append(p.Reasons, "paired schedule identities differ")
	}
	if raw.SuiteHash != bounded.SuiteHash || raw.Task.TaskHash != bounded.Task.TaskHash {
		p.OK = false
		p.Reasons = append(p.Reasons, "suite or task identities differ between paired arms")
	}
	if raw.ManifestHash != bounded.ManifestHash {
		p.OK = false
		p.Reasons = append(p.Reasons, "result manifest identities differ between paired arms")
	}
	check := func(name string, a, b any) {
		if !sameFingerprintValue(a, b) {
			p.OK = false
			p.Reasons = append(p.Reasons, fmt.Sprintf("%s differs: raw=%v bounded=%v", name, a, b))
		}
	}
	check("model provider", raw.Model.Provider, bounded.Model.Provider)
	check("model", raw.Model.Model, bounded.Model.Model)
	check("model file", raw.Model.ModelFile, bounded.Model.ModelFile)
	check("model sampling temperature", raw.Model.Temperature, bounded.Model.Temperature)
	check("model sampling top_p", raw.Model.TopP, bounded.Model.TopP)
	check("model sampling top_k", raw.Model.TopK, bounded.Model.TopK)
	check("model seed", raw.Model.Seed, bounded.Model.Seed)
	check("context tokens", raw.Model.ContextTokens, bounded.Model.ContextTokens)
	if raw.Model.Runtime == "" || bounded.Model.Runtime == "" {
		p.Warnings = append(p.Warnings, "served runtime identity is unavailable for at least one result")
	} else {
		check("served runtime", raw.Model.Runtime, bounded.Model.Runtime)
	}
	check("wall-clock timeout", raw.Execution.TimeoutSeconds, bounded.Execution.TimeoutSeconds)
	check("task hash", raw.Task.TaskHash, bounded.Task.TaskHash)
	check("task limits", raw.Task.Limits, bounded.Task.Limits)
	check("base commit", raw.Repository.BaseCommit, bounded.Repository.BaseCommit)
	check("fixture hash", raw.Repository.FixtureHash, bounded.Repository.FixtureHash)
	check("network policy", raw.Configuration.Network, bounded.Configuration.Network)
	check("benchmark seed", raw.Seed, bounded.Seed)
	if !raw.Model.IdentityVerified || !bounded.Model.IdentityVerified {
		p.OK = false
		p.Reasons = append(p.Reasons, "served model identity was not independently observed for at least one result")
	}
	if raw.Model.ModelFileSHA256 == "" || bounded.Model.ModelFileSHA256 == "" {
		p.Warnings = append(p.Warnings, "model file hash unavailable for at least one result")
	} else {
		check("model file hash", raw.Model.ModelFileSHA256, bounded.Model.ModelFileSHA256)
	}
	return p
}

// RedactSecrets returns a copy safe to persist. It is deliberately conservative:
// values whose names look like credentials are replaced, not merely omitted.
func RedactSecrets(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "key") || strings.Contains(lk, "token") || strings.Contains(lk, "secret") ||
			strings.Contains(lk, "password") || strings.Contains(lk, "credential") || strings.Contains(lk, "authorization") {
			out[k] = "[REDACTED]"
			continue
		}
		out[k] = v
	}
	return out
}

// sortedMapKeys is retained for callers that need deterministic map ordering.
//
//nolint:unused
func sortedMapKeys(in map[string]string) []string {
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
