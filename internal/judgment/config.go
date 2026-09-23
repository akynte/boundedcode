package judgment

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Defaults. They are chosen so that the shipped state of this feature is
// "present, off, and sending nothing".
const (
	// DefaultEndpoint is TypeSafe's System One endpoint.
	DefaultEndpoint = "https://api.typesafe.ai/v1/systemone"
	// DefaultMinConfidence is where a choice or score stops being worth acting
	// on. It is deliberately high: the fallback is the deterministic answer
	// the system already had, so declining a judgment costs nothing but the
	// tokens already spent.
	DefaultMinConfidence = 0.75
	// DefaultTimeout bounds one request. A judgment is on the hot path of
	// retrieval, so a slow answer is a worse outcome than no answer.
	DefaultTimeout = 20 * time.Second
	// ConfigFile is the file this is configured in, beside bcode.yaml and
	// providers.yaml.
	ConfigFile = "judgment.yaml"
	// RecommendedModel is the pinned Jev snapshot this project documents.
	//
	// It is a concrete version rather than jev-latest because the model id is
	// part of the judgment cache key and of every benchmark's provenance: an
	// alias that moves underneath makes two runs incomparable while the
	// configuration looks unchanged. `bcode judgment smoke` is how an operator
	// confirms the service accepts it before a benchmark depends on it.
	RecommendedModel = "jev-1.13.0"
)

// AliasModels are identifiers that name a moving target rather than a build.
// They work, and `bcode doctor` warns about them, because a benchmark run on one
// cannot say which model produced its numbers.
var AliasModels = map[string]bool{"jev-latest": true, "latest": true}

// Config is judgment.yaml.
//
// It is a separate file from providers.yaml on purpose. providers.yaml maps
// roles to chat models that are interchangeable with one another; a judge is
// not interchangeable with a chat model, and a configuration format that let
// someone write one where the other was expected would make it look like it
// was. See the package comment and the ADR.
type Config struct {
	// Enabled is false in the shipped state and in the absence of the file.
	Enabled bool `yaml:"enabled"`
	// Endpoint is the System One URL.
	Endpoint string `yaml:"endpoint,omitempty"`
	// Model is the judgment model.
	//
	// Pin it to a dated snapshot rather than an alias. The model id is part of
	// the cache key, which is what makes a cached judgment reproducible; an
	// alias that moves underneath makes yesterday's eval run and today's
	// incomparable while appearing to be the same configuration. `bcode doctor`
	// warns when this looks like an alias.
	Model string `yaml:"model,omitempty"`
	// APIKeyEnv names an environment variable rather than holding a secret, so
	// this file stays safe to commit and to attach to a bug report. The same
	// convention as providers.yaml.
	APIKeyEnv string `yaml:"api_key_env,omitempty"`
	// Redact controls whether repository content may leave at all. Strict is
	// the default and permits none.
	Redact RedactMode `yaml:"redact,omitempty"`
	// MinConfidence is the floor for choice and score answers, where a call
	// site sets none of its own.
	MinConfidence float64 `yaml:"min_confidence,omitempty"`
	// TimeoutSeconds bounds one request. Zero means DefaultTimeout.
	TimeoutSeconds int `yaml:"timeout_seconds,omitempty"`
	// Cache stores answers content-addressed by state and question set.
	Cache bool `yaml:"cache,omitempty"`
	// Sites maps a judgment site's name to the authority tier it has earned.
	// A site with no entry runs at TierLogged: it is asked, its answer is
	// journalled, and nothing consumes it. Promoting a site is a line an
	// operator adds here after reading its calibration record (see
	// TierLogged's doc comment and design.md §7), never something this
	// package or a call site decides for itself.
	Sites map[string]Tier `yaml:"sites,omitempty"`
}

// DefaultConfig is the shipped state: the feature exists and does nothing.
func DefaultConfig() Config {
	//nolint:gosec // APIKeyEnv below is the name of an environment variable,
	// not a key. Holding the name rather than the value is the whole point of
	// the convention: it is what keeps this file safe to commit and to attach
	// to a bug report.
	return Config{
		Enabled:       false,
		Endpoint:      DefaultEndpoint,
		APIKeyEnv:     "TYPESAFE_API_KEY",
		Redact:        RedactStrict,
		MinConfidence: DefaultMinConfidence,
		Cache:         true,
	}
}

// Load reads judgment.yaml from a configuration directory.
//
// An absent file is not an error and not a warning: it is the ordinary state
// of a system that does not use this. It yields DefaultConfig, which is
// disabled.
func Load(dir string) (Config, error) {
	cfg := DefaultConfig()
	body, err := os.ReadFile(filepath.Join(dir, ConfigFile))
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("judgment: reading %s: %w", ConfigFile, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(body))
	// KnownFields, as providers.yaml does: a misspelled key that silently did
	// nothing would be a security setting that silently did nothing.
	dec.KnownFields(true)
	var file Config
	if err := dec.Decode(&file); err != nil {
		return cfg, fmt.Errorf("judgment: parsing %s: %w", ConfigFile, err)
	}
	cfg.Enabled = file.Enabled
	if file.Endpoint != "" {
		cfg.Endpoint = file.Endpoint
	}
	if file.Model != "" {
		cfg.Model = file.Model
	}
	if file.APIKeyEnv != "" {
		cfg.APIKeyEnv = file.APIKeyEnv
	}
	if file.Redact != "" {
		cfg.Redact = file.Redact
	}
	if file.MinConfidence != 0 {
		cfg.MinConfidence = file.MinConfidence
	}
	cfg.TimeoutSeconds = file.TimeoutSeconds
	cfg.Cache = file.Cache
	cfg.Sites = file.Sites
	return cfg, nil
}

// Validate reports a configuration that cannot be used. It is called by New,
// and by `bcode doctor` on the file as it sits on disk, so that a misconfiguration
// that would prevent startup is reportable without starting.
//
// It answers a question about the file, not about the environment. Whether the
// key named by api_key_env is exported is a runtime capability, not a
// misconfiguration: enabled means the integration is configured, and a
// workflow that never consults the judge has no reason to care. The credential
// is required at the boundary that builds a live request instead — see
// RequireCredential — so a missing key fails the Jev-backed paths closed
// without failing every other command that happens to load this file.
func (c Config) Validate() error {
	// Site tier validation runs unconditionally, even when the integration
	// is disabled or absent. A malformed tier ("routng") or an over-grant is
	// a configuration error about what judgment.yaml *says*, not about
	// whether a live judge currently acts on it — a file can be authored
	// with enabled: false while the operator is staging a promotion, and a
	// typo there must not silently collapse to logged (Tier.Valid() already
	// makes an unrecognised tier behave as logged at read time; Validate
	// exists so that collapse is reported instead of hidden). Checking it
	// only under c.Enabled was the gap: `bcode doctor` short-circuits to OK
	// before calling Validate at all when disabled, so a broken sites map
	// was invisible until the integration was turned on with the typo still
	// in it.
	for site, tier := range c.Sites {
		if !tier.Valid() {
			return fmt.Errorf("judgment: sites.%s names unknown tier %q; use logged, "+
				"ordering or routing", site, tier)
		}
		// A site may not be granted an authority its own code never
		// implements. Without this, `sites: {fact_relevance: routing}`
		// would parse, validate, and read as though a person had turned
		// something on — while the call site only ever checks for ordering,
		// so nothing changes. A configuration that appears to grant an
		// effect and does not is worse than one that refuses.
		//
		// An unregistered site is left alone rather than refused: a
		// judgment.yaml written for a newer build may legitimately name a
		// site this binary does not have, and a test binary that does not
		// link internal/workflow sees none of its sites at all.
		if info, known := Site(site); known && tier.rank() > info.MaxEffect.rank() {
			return fmt.Errorf("judgment: sites.%s is set to %q but that site implements no "+
				"effect above %q; grant it %q or less, or the configuration would be "+
				"claiming an authority nothing acts on",
				site, tier, info.MaxEffect, info.MaxEffect)
		}
	}
	// Everything below concerns actually reaching the service, so it only
	// applies once the integration is turned on: a disabled or absent file
	// has no endpoint, model or credential to be wrong about.
	if !c.Enabled {
		return nil
	}
	if c.Endpoint == "" {
		return errors.New("judgment: enabled with no endpoint")
	}
	if c.Model == "" {
		return errors.New("judgment: enabled with no model. Pin a dated snapshot, " +
			"not a moving alias: the model id is part of the cache key")
	}
	if !c.Redact.Valid() {
		return fmt.Errorf("judgment: unknown redact mode %q; use strict or repo_text", c.Redact)
	}
	if c.APIKeyEnv == "" {
		return errors.New("judgment: enabled with no api_key_env")
	}
	if c.MinConfidence < 0 || c.MinConfidence > 1 {
		return fmt.Errorf("judgment: min_confidence %v is not a probability", c.MinConfidence)
	}
	return nil
}

// HasCredential reports whether the environment currently holds the key named
// by api_key_env. A disabled integration has nothing to hold.
func (c Config) HasCredential() bool {
	return c.Enabled && c.APIKeyEnv != "" && os.Getenv(c.APIKeyEnv) != ""
}

// RequireCredential fails closed for a caller that is about to depend on the
// external judge.
//
// This is the narrow boundary the key check moved to. Callers that reach it
// have already decided they need Jev — a rerank arm that names it, a smoke
// test whose whole purpose is the round trip — so refusing here is refusing
// the thing that was asked for, and never a silent downgrade to an arm that
// was not.
func (c Config) RequireCredential() error {
	if !c.Enabled {
		return fmt.Errorf("%w: enabled is not set in %s", ErrNoCredential, ConfigFile)
	}
	if !c.HasCredential() {
		return fmt.Errorf("%w: %s is enabled but %s is unset in the environment. "+
			"Export it to use the judge, or select a configuration that does not need it",
			ErrNoCredential, ConfigFile, c.APIKeyEnv)
	}
	return nil
}

// Timeout is the configured bound on one request.
func (c Config) Timeout() time.Duration {
	if c.TimeoutSeconds > 0 {
		return time.Duration(c.TimeoutSeconds) * time.Second
	}
	return DefaultTimeout
}

// PinnedModel reports whether the model names a build rather than a moving
// target. It is a naming convention, so this is advice rather than a rule, and
// only `bcode doctor` and the eval harness act on it.
func (c Config) PinnedModel() bool {
	if c.Model == "" {
		return false
	}
	return !AliasModels[c.Model] && !strings.HasSuffix(c.Model, "-latest")
}
