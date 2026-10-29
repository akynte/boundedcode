// Package model defines model profiles: the declarative description of a
// local model and how to serve it. Models are configuration, not code; adding
// a candidate means adding a YAML file.
package model

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/config"
)

// Profile describes one local model.
type Profile struct {
	Name         string       `yaml:"name"`
	DisplayName  string       `yaml:"display_name"`
	File         string       `yaml:"file"` // GGUF path; relative paths resolve against models_dir
	Source       Source       `yaml:"source"`
	Architecture Architecture `yaml:"architecture"`
	Server       ServerFlags  `yaml:"server"`
	Sampling     Sampling     `yaml:"sampling"`
	// Status is "validated" (measured by BoundedCode's validation),
	// "experimental" (not yet benchmarked here) or "review" (its license is
	// under review). Empty is treated as experimental.
	Status string `yaml:"status"`
	// Description is one line for model choices in set-up.
	Description string `yaml:"description"`
	// Capabilities is what the model is declared to do with BoundedCode's
	// requests. `bench models` measures them on this machine; routing
	// trusts the measurement, never a leaderboard score.
	Capabilities Capabilities `yaml:"capabilities"`
	// Origin is where the profile was loaded from (not serialized).
	Origin string `yaml:"-"`
}

// Source records where the weights come from. Informational: the tool never
// redistributes weights.
type Source struct {
	Repo      string `yaml:"repo"`
	File      string `yaml:"file"`
	Revision  string `yaml:"revision"`
	BaseModel string `yaml:"base_model"`
	License   string `yaml:"license"`
	// SizeBytes and SHA256 identify the file at Revision (from the Hugging
	// Face API); downloads are verified against SHA256. 0/"" = unknown (the
	// download then reads the hash from the API).
	SizeBytes int64  `yaml:"size_bytes"`
	SHA256    string `yaml:"sha256"`
	// LicenseURL is where the license text is published.
	LicenseURL string `yaml:"license_url"`
	// LicenseNotice, when set, is shown before download and must be
	// accepted (custom open-weight licenses: use restrictions, thresholds).
	LicenseNotice string `yaml:"license_notice"`
	// Gated repositories need a Hugging Face token whose account accepted
	// the model's terms on its page.
	Gated bool `yaml:"gated"`
}

// Architecture is coarse model metadata used for planning and reports.
type Architecture struct {
	MoE           bool    `yaml:"moe"`
	TotalParamsB  float64 `yaml:"total_params_b"`
	ActiveParamsB float64 `yaml:"active_params_b"`
	// KVMiB estimates the KV cache at the profile's ctx_size, for the
	// memory fit (0 = 10% of the file size plus 512 MiB). Computed from the
	// model's config (full-attention layers × KV heads × head size × 2 ×
	// bytes per element × tokens); an estimate, not a measurement.
	KVMiB int `yaml:"kv_mib"`
}

// Capabilities are a profile's declared capabilities, from the model card,
// the weights' metadata and the runtime's support. Empty means unknown.
type Capabilities struct {
	// ToolCalling is "native" (the chat template formats tool calls and
	// the server parses them) or "none".
	ToolCalling string `yaml:"tool_calling"`
	// StructuredOutput is "json_schema" (answers can be constrained to a
	// schema) or "none".
	StructuredOutput string `yaml:"structured_output"`
	// Thinking is "toggle" (the template honours enable_thinking per
	// request), "always" (it reasons and cannot be told not to) or "none".
	Thinking string `yaml:"thinking"`
	// ContextMax is the trained context length in tokens.
	ContextMax int `yaml:"context_max"`
	// Tokenizer is the weights' tokenizer family (GGUF
	// tokenizer.ggml.pre). A draft model must share the target's.
	Tokenizer string `yaml:"tokenizer"`
	// Speculative lists the speculative decoding methods these weights
	// allow: "ngram" (any model), "draft" (a smaller model with the same
	// tokenizer) and "mtp" (the weights carry multi-token prediction
	// layers). Allowed is not faster: `bench spec` measures.
	Speculative []string `yaml:"speculative"`
	// Limitations are known compatibility problems, one sentence each.
	Limitations []string `yaml:"limitations"`
}

// Speculative configures speculative decoding (llama-server --spec-*).
// Off unless Type is set.
type Speculative struct {
	// Type is a llama-server --spec-type: ngram-mod, ngram-simple,
	// ngram-map-k, ngram-cache, draft-simple or draft-mtp.
	Type string `yaml:"type"`
	// DraftFile is the draft model's GGUF for draft-simple; relative paths
	// resolve like File.
	DraftFile string `yaml:"draft_file,omitempty"`
	// DraftGPULayers offloads the draft model's layers (-ngld; 0 = the
	// runtime default).
	DraftGPULayers int `yaml:"draft_gpu_layers,omitempty"`
	// NMax and NMin bound the tokens drafted per step (0 = the runtime
	// default: 3 and 0).
	NMax int `yaml:"n_max,omitempty"`
	NMin int `yaml:"n_min,omitempty"`
	// PMin is the least draft probability to keep drafting (greedy).
	PMin float64 `yaml:"p_min,omitempty"`
}

// SpecTypes are the llama-server --spec-type values a profile may use.
var SpecTypes = []string{"ngram-mod", "ngram-simple", "ngram-map-k", "ngram-map-k4v", "ngram-cache", "draft-simple", "draft-mtp"}

// SpecMethod is the capability a --spec-type needs: "ngram", "draft" or
// "mtp".
func SpecMethod(specType string) string {
	switch {
	case strings.HasPrefix(specType, "ngram"):
		return "ngram"
	case specType == "draft-mtp":
		return "mtp"
	case strings.HasPrefix(specType, "draft"):
		return "draft"
	}
	return ""
}

// ServerFlags are inference-server settings. Field names are runtime-neutral;
// the llama.cpp runtime maps them to flags. Zero values mean "runtime default".
type ServerFlags struct {
	CtxSize    int    `yaml:"ctx_size"`
	GPULayers  int    `yaml:"gpu_layers"`
	NCPUMoE    int    `yaml:"n_cpu_moe"`
	FlashAttn  string `yaml:"flash_attn"` // on | off | auto
	CacheTypeK string `yaml:"cache_type_k"`
	CacheTypeV string `yaml:"cache_type_v"`
	BatchSize  int    `yaml:"batch_size"`
	UBatchSize int    `yaml:"ubatch_size"`
	Threads    int    `yaml:"threads"`
	Parallel   int    `yaml:"parallel"`
	Jinja      bool   `yaml:"jinja"`
	Mlock      bool   `yaml:"mlock"`
	NoMmap     bool   `yaml:"no_mmap"`
	CacheReuse int    `yaml:"cache_reuse"`
	// Reasoning maps to llama-server --reasoning (on|off|auto).
	Reasoning string `yaml:"reasoning"`
	// ReasoningBudget caps thinking tokens per response (llama-server
	// --reasoning-budget): 0 leaves the server default, -1 is unrestricted.
	// Unbounded thinking was measured to end in 8K-token runaways (repetition,
	// tool calls trapped inside the thinking block) costing 31-51% of model
	// time on several real tasks.
	ReasoningBudget int `yaml:"reasoning_budget"`
	// ReasoningBudgetMessage is injected before the end-of-thinking tag when
	// the budget is spent, so the model moves on to act.
	ReasoningBudgetMessage string `yaml:"reasoning_budget_message"`
	// Prompt cache (llama-server, verified against the pinned release's
	// --help; nil or 0 leaves the server default).
	// CtxCheckpoints is how many context checkpoints a slot keeps
	// (--ctx-checkpoints, default 32). A model with recurrent layers, such
	// as Qwen3.6's, can only reuse a cached prefix up to a checkpoint.
	CtxCheckpoints int `yaml:"ctx_checkpoints,omitempty"`
	// CheckpointMinStep is the least spacing of checkpoints in tokens
	// (--checkpoint-min-step, default 8192; 0 = no minimum).
	CheckpointMinStep *int `yaml:"checkpoint_min_step,omitempty"`
	// CacheRAMMiB caps the host-memory prompt cache that keeps other
	// conversations' states (--cache-ram, default 8192; 0 disables, -1 no
	// limit).
	CacheRAMMiB *int `yaml:"cache_ram_mib,omitempty"`
	// ReasoningPreserve keeps the reasoning of every earlier turn in the
	// prompt where the chat template supports it (--reasoning-preserve,
	// the server default; false = --no-reasoning-preserve, only the
	// current turn's).
	ReasoningPreserve *bool `yaml:"reasoning_preserve,omitempty"`
	// OpOffloadMinBatch is the smallest batch whose host-resident weight
	// operations (the experts kept in system RAM) are run on the GPU
	// (GGML_OP_OFFLOAD_MIN_BATCH of the CUDA backend; 0 = its default).
	OpOffloadMinBatch int `yaml:"op_offload_min_batch,omitempty"`
	// Speculative enables speculative decoding; nil leaves it off.
	Speculative *Speculative `yaml:"speculative,omitempty"`
	// ExtraArgs are appended verbatim; prefer typed fields.
	ExtraArgs []string `yaml:"extra_args"`
}

// Sampling holds default sampling parameters sent with each request.
type Sampling struct {
	Temperature     float64 `yaml:"temperature"`
	TopP            float64 `yaml:"top_p"`
	TopK            int     `yaml:"top_k"`
	MinP            float64 `yaml:"min_p"`
	PresencePenalty float64 `yaml:"presence_penalty"`
	RepeatPenalty   float64 `yaml:"repeat_penalty"`
}

// Profile statuses.
const (
	StatusValidated    = "validated"
	StatusExperimental = "experimental"
	StatusReview       = "review"
)

var (
	sha256RE = regexp.MustCompile(`^[0-9a-f]{64}$`)
	commitRE = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// Validate checks a profile.
func (p Profile) Validate() error {
	var errs []error
	if p.Name == "" || strings.ContainsAny(p.Name, " /\\") {
		errs = append(errs, fmt.Errorf("name %q must be non-empty without spaces or slashes", p.Name))
	}
	if p.File == "" {
		errs = append(errs, errors.New("file: required"))
	}
	if !strings.HasSuffix(strings.ToLower(p.File), ".gguf") {
		errs = append(errs, fmt.Errorf("file %q: only GGUF is supported by the llama.cpp runtime", p.File))
	}
	switch p.Server.FlashAttn {
	case "", "on", "off", "auto":
	default:
		errs = append(errs, fmt.Errorf("server.flash_attn: %q is not on|off|auto", p.Server.FlashAttn))
	}
	if p.Server.ReasoningBudget < -1 {
		errs = append(errs, fmt.Errorf("server.reasoning_budget: %d (want -1, 0 or a positive token count)", p.Server.ReasoningBudget))
	}
	switch p.Server.Reasoning {
	case "", "on", "off", "auto":
	default:
		errs = append(errs, fmt.Errorf("server.reasoning: %q is not on|off|auto", p.Server.Reasoning))
	}
	if p.Server.CtxSize < 0 || p.Server.NCPUMoE < 0 || p.Server.Parallel < 0 {
		errs = append(errs, errors.New("server: negative sizes are invalid"))
	}
	switch p.Status {
	case "", StatusValidated, StatusExperimental, StatusReview:
	default:
		errs = append(errs, fmt.Errorf("status: %q is not validated|experimental|review", p.Status))
	}
	if p.Source.SHA256 != "" && !sha256RE.MatchString(p.Source.SHA256) {
		errs = append(errs, fmt.Errorf("source.sha256: %q is not a sha256 hex digest", p.Source.SHA256))
	}
	if p.Source.SizeBytes < 0 {
		errs = append(errs, errors.New("source.size_bytes: negative"))
	}
	errs = append(errs, p.Capabilities.validate()...)
	if sp := p.Server.Speculative; sp != nil {
		switch {
		case !slices.Contains(SpecTypes, sp.Type):
			errs = append(errs, fmt.Errorf("server.speculative.type: %q is not one of %s", sp.Type, strings.Join(SpecTypes, ", ")))
		case sp.Type == "draft-simple" && sp.DraftFile == "":
			errs = append(errs, errors.New("server.speculative.draft_file: required for draft-simple"))
		case sp.Type != "draft-simple" && sp.DraftFile != "":
			errs = append(errs, fmt.Errorf("server.speculative.draft_file: only draft-simple uses a draft model (type %s)", sp.Type))
		case len(p.Capabilities.Speculative) > 0 && !slices.Contains(p.Capabilities.Speculative, SpecMethod(sp.Type)):
			errs = append(errs, fmt.Errorf("server.speculative.type %s needs %q, which capabilities.speculative does not list", sp.Type, SpecMethod(sp.Type)))
		}
		if sp.NMax < 0 || sp.NMin < 0 || sp.NMax > 0 && sp.NMin > sp.NMax || sp.PMin < 0 || sp.PMin > 1 {
			errs = append(errs, errors.New("server.speculative: n_max and n_min must be >= 0 with n_min <= n_max, p_min in [0,1]"))
		}
		if sp.DraftFile != "" && !strings.HasSuffix(strings.ToLower(sp.DraftFile), ".gguf") {
			errs = append(errs, fmt.Errorf("server.speculative.draft_file %q: only GGUF is supported", sp.DraftFile))
		}
	}
	if p.Sampling.Temperature < 0 || p.Sampling.TopP < 0 || p.Sampling.TopP > 1 {
		errs = append(errs, errors.New("sampling: temperature must be >= 0 and top_p in [0,1]"))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("model profile %q: %w", p.Name, err)
	}
	return nil
}

func (c Capabilities) validate() []error {
	var errs []error
	check := func(field, v string, ok ...string) {
		if v != "" && !slices.Contains(ok, v) {
			errs = append(errs, fmt.Errorf("capabilities.%s: %q is not %s", field, v, strings.Join(ok, "|")))
		}
	}
	check("tool_calling", c.ToolCalling, "native", "none")
	check("structured_output", c.StructuredOutput, "json_schema", "none")
	check("thinking", c.Thinking, "toggle", "always", "none")
	for _, m := range c.Speculative {
		check("speculative", m, "ngram", "draft", "mtp")
	}
	if c.ContextMax < 0 {
		errs = append(errs, errors.New("capabilities.context_max: negative"))
	}
	return errs
}

// ResolveDraft returns the speculative draft model's GGUF path, resolved
// like ResolveFile ("" without one).
func (p Profile) ResolveDraft(modelsDir string) string {
	sp := p.Server.Speculative
	if sp == nil || sp.DraftFile == "" {
		return ""
	}
	return Profile{File: sp.DraftFile}.ResolveFile(modelsDir)
}

// ResolveFile returns the absolute GGUF path: absolute files as-is, else the
// first existing match in modelsDir, then the project's own models dir
// ($XDG_DATA_HOME/boundedcode/models, where scripts/fetch-model.sh writes).
// If none exists, the modelsDir path is returned (for error messages).
func (p Profile) ResolveFile(modelsDir string) string {
	if filepath.IsAbs(p.File) {
		return p.File
	}
	primary := filepath.Join(modelsDir, p.File)
	if _, err := os.Stat(primary); err == nil || modelsDir == "" {
		return primary
	}
	if fallback := filepath.Join(DataModelsDir(), p.File); fileExists(fallback) {
		return fallback
	}
	return primary
}

// DataModelsDir is where scripts/fetch-model.sh stores weights.
func DataModelsDir() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" || !filepath.IsAbs(base) {
		h, _ := os.UserHomeDir()
		base = filepath.Join(h, ".local", "share")
	}
	return filepath.Join(base, "boundedcode", "models")
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Catalog is the set of known profiles, keyed by name.
type Catalog map[string]Profile

// LoadCatalog loads embedded profiles from builtin (a directory of *.yaml in
// an fs.FS) and then user profiles from userDir, which override by name.
func LoadCatalog(builtin fs.FS, builtinDir, userDir string) (Catalog, error) {
	c := Catalog{}
	if builtin != nil {
		entries, err := fs.ReadDir(builtin, builtinDir)
		if err != nil {
			return nil, fmt.Errorf("read builtin profiles: %w", err)
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			b, err := fs.ReadFile(builtin, builtinDir+"/"+e.Name())
			if err != nil {
				return nil, err
			}
			if err := c.add(b, "builtin:"+e.Name()); err != nil {
				return nil, err
			}
		}
	}
	if userDir != "" {
		entries, err := os.ReadDir(userDir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			path := filepath.Join(userDir, e.Name())
			b, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			if err := c.add(b, path); err != nil {
				return nil, err
			}
		}
	}
	return c, nil
}

func (c Catalog) add(b []byte, origin string) error {
	var p Profile
	if err := config.DecodeStrict(b, &p); err != nil {
		return fmt.Errorf("%s: %w", origin, err)
	}
	if err := p.Validate(); err != nil {
		return fmt.Errorf("%s: %w", origin, err)
	}
	p.Origin = origin
	if prev, ok := c[p.Name]; ok {
		inheritCatalog(&p, prev)
	}
	c[p.Name] = p
	return nil
}

// inheritCatalog fills a user override's missing catalog metadata from the
// built-in profile it replaces, when both describe the same weights (same
// repository and file). Overrides written by `bench infra --apply` carry
// only tuned server settings; they must not lose the model's revision,
// checksum, license notice or validation status. The override's own values
// always win.
func inheritCatalog(p *Profile, prev Profile) {
	if p.Source.Repo != prev.Source.Repo || p.Source.File != prev.Source.File {
		return
	}
	if p.Source.Revision != "" && prev.Source.Revision != "" && p.Source.Revision != prev.Source.Revision {
		return // a different revision is different weights
	}
	src := &p.Source
	if src.Revision == "" {
		src.Revision = prev.Source.Revision
	}
	if src.SizeBytes == 0 {
		src.SizeBytes = prev.Source.SizeBytes
	}
	if src.SHA256 == "" {
		src.SHA256 = prev.Source.SHA256
	}
	if src.LicenseURL == "" {
		src.LicenseURL = prev.Source.LicenseURL
	}
	if src.LicenseNotice == "" {
		src.LicenseNotice = prev.Source.LicenseNotice
	}
	src.Gated = src.Gated || prev.Source.Gated
	if p.Status == "" {
		p.Status = prev.Status
	}
	if p.Description == "" {
		p.Description = prev.Description
	}
	if p.Architecture.KVMiB == 0 {
		p.Architecture.KVMiB = prev.Architecture.KVMiB
	}
	if p.Capabilities.zero() {
		p.Capabilities = prev.Capabilities
	}
}

func (c Capabilities) zero() bool {
	return c.ToolCalling == "" && c.StructuredOutput == "" && c.Thinking == "" && c.ContextMax == 0 && c.Tokenizer == "" &&
		len(c.Speculative) == 0 && len(c.Limitations) == 0
}

// Get returns the named profile.
func (c Catalog) Get(name string) (Profile, error) {
	p, ok := c[name]
	if !ok {
		return Profile{}, fmt.Errorf("unknown model profile %q (known: %s)", name, strings.Join(c.Names(), ", "))
	}
	return p, nil
}

// Names returns sorted profile names.
func (c Catalog) Names() []string {
	names := make([]string, 0, len(c))
	for n := range c {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
