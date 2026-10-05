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
}

// Architecture is coarse model metadata used for planning and reports.
type Architecture struct {
	MoE           bool    `yaml:"moe"`
	TotalParamsB  float64 `yaml:"total_params_b"`
	ActiveParamsB float64 `yaml:"active_params_b"`
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
	if p.Sampling.Temperature < 0 || p.Sampling.TopP < 0 || p.Sampling.TopP > 1 {
		errs = append(errs, errors.New("sampling: temperature must be >= 0 and top_p in [0,1]"))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("model profile %q: %w", p.Name, err)
	}
	return nil
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
	c[p.Name] = p
	return nil
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
