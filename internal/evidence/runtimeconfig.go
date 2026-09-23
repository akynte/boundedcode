package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// RuntimeConfig is evals/evidence-v1/runtime-config.yaml — the frozen,
// secret-free generator identity for Evidence Suite v1 (design instruction
// GAP 1). It is a separate artifact from manifest.json on purpose:
// manifest.json was already frozen (its hash is checked everywhere) before
// a usable generator configuration existed, and this file's own SHA-256 is
// what every persisted Result carries instead.
type RuntimeConfig struct {
	SchemaVersion int    `yaml:"schema_version"`
	Suite         string `yaml:"suite"`
	Generator     struct {
		ProvidersFile string `yaml:"providers_file"`
		Provider      string `yaml:"provider"`
		Model         string `yaml:"model"`
		APIKeyEnv     string `yaml:"api_key_env"`
		Reasoning     struct {
			ThinkingPolicy string `yaml:"thinking_policy"`
		} `yaml:"reasoning"`
		Sampling struct {
			Temperature float64 `yaml:"temperature"`
		} `yaml:"sampling"`
		Context struct {
			ContextTokens        int `yaml:"context_tokens"`
			ReservedOutputTokens int `yaml:"reserved_output_tokens"`
			MaxToolsExposed      int `yaml:"max_tools_exposed"`
			MaxSteps             int `yaml:"max_steps"`
		} `yaml:"context"`
		Routing struct {
			Role       string `yaml:"role"`
			SingleRole bool   `yaml:"single_role"`
		} `yaml:"routing"`
	} `yaml:"generator"`
}

// LoadRuntimeConfig reads and parses path, returning both the parsed
// config and the SHA-256 of its exact bytes — the hash every Result and
// `bcode evidence preflight` records, computed once over what was actually
// read, never recomputed after any in-memory adjustment.
func LoadRuntimeConfig(path string) (RuntimeConfig, string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return RuntimeConfig{}, "", err
	}
	var cfg RuntimeConfig
	if err := yaml.Unmarshal(body, &cfg); err != nil {
		return RuntimeConfig{}, "", fmt.Errorf("evidence: parsing runtime-config.yaml: %w", err)
	}
	if cfg.Generator.Model == "" || cfg.Generator.Provider == "" {
		return RuntimeConfig{}, "", fmt.Errorf("evidence: runtime-config.yaml is missing " +
			"generator.provider or generator.model")
	}
	sum := sha256.Sum256(body)
	return cfg, hex.EncodeToString(sum[:]), nil
}
