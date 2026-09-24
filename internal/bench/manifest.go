package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// FrozenTask identifies one task and its evaluator/base material in an official
// run manifest.
type FrozenTask struct {
	ID            string `json:"id"`
	TaskHash      string `json:"task_hash"`
	EvaluatorHash string `json:"evaluator_hash"`
	BaseCommit    string `json:"base_commit"`
}

// FrozenManifest is emitted before an official run. A result carries its
// manifest identity, and a report warns when identities differ.
type FrozenManifest struct {
	SchemaVersion     int               `json:"schema_version"`
	SuiteID           string            `json:"suite_id"`
	SuiteVersion      string            `json:"suite_version"`
	SuiteHash         string            `json:"suite_hash"`
	Tasks             []FrozenTask      `json:"tasks"`
	Model             ModelConfig       `json:"model"`
	Runner            RunnerConfig      `json:"runner"`
	Limits            map[string]Limits `json:"limits"`
	NetworkPolicies   map[string]string `json:"network_policies"`
	BoundedCodeCommit string            `json:"boundedcode_commit,omitempty"`
	ExecutionSeed     int64             `json:"execution_seed"`
	CreatedAt         time.Time         `json:"created_at"`
	Official          bool              `json:"official"`
}

func Freeze(ctx context.Context, s Suite, seed int64, official bool) (FrozenManifest, error) {
	if err := s.Validate(); err != nil {
		return FrozenManifest{}, err
	}
	if official && s.Smoke {
		return FrozenManifest{}, fmt.Errorf("bench: refusing to freeze smoke suite %q as official", s.ID)
	}
	m := FrozenManifest{SchemaVersion: ManifestSchemaVersion, SuiteID: s.ID, SuiteVersion: s.Version,
		SuiteHash: s.Hash(), Model: s.Model, Runner: s.Runner, Limits: map[string]Limits{},
		NetworkPolicies: map[string]string{}, ExecutionSeed: seed, CreatedAt: time.Now().UTC(), Official: official}
	for _, t := range s.Tasks {
		m.Tasks = append(m.Tasks, FrozenTask{ID: t.ID, TaskHash: taskHash(t), EvaluatorHash: t.hiddenHash, BaseCommit: t.BaseCommit})
		m.Limits[t.ID] = t.Limits
		m.NetworkPolicies[t.ID] = t.NetworkPolicy
	}
	sort.Slice(m.Tasks, func(i, j int) bool { return m.Tasks[i].ID < m.Tasks[j].ID })
	// The code revision is best effort and explicitly nullable in the schema.
	if commit, err := probe(ctx, "git", "rev-parse", "HEAD"); err == nil {
		m.BoundedCodeCommit = commit
	}
	return m, nil
}

func WriteManifest(path string, m FrozenManifest) error {
	if path == "" {
		return fmt.Errorf("bench: manifest path is empty")
	}
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, append(body, '\n'), 0o600) //nolint:gosec // frozen manifest artifact
}

func LoadManifest(path string) (FrozenManifest, error) {
	body, err := os.ReadFile(path) //nolint:gosec // operator-selected manifest
	if err != nil {
		return FrozenManifest{}, err
	}
	var m FrozenManifest
	if err := json.Unmarshal(body, &m); err != nil {
		return FrozenManifest{}, err
	}
	if m.SchemaVersion != ManifestSchemaVersion {
		return FrozenManifest{}, fmt.Errorf("bench: unsupported manifest schema %d", m.SchemaVersion)
	}
	return m, nil
}

// VerifyFrozen fails closed when the suite or any task/evaluator changed after
// the manifest was emitted.
func VerifyFrozen(s Suite, m FrozenManifest) error {
	if s.Hash() != m.SuiteHash {
		return fmt.Errorf("bench: suite hash changed: manifest %s, current %s", m.SuiteHash, s.Hash())
	}
	byID := make(map[string]FrozenTask, len(m.Tasks))
	for _, t := range m.Tasks {
		byID[t.ID] = t
	}
	if len(byID) != len(s.Tasks) {
		return fmt.Errorf("bench: frozen task set has %d tasks, current suite has %d", len(byID), len(s.Tasks))
	}
	for _, t := range s.Tasks {
		frozen, ok := byID[t.ID]
		if !ok {
			return fmt.Errorf("bench: task %q is not in the frozen manifest", t.ID)
		}
		if frozen.TaskHash != taskHash(t) || frozen.EvaluatorHash != t.hiddenHash || frozen.BaseCommit != t.BaseCommit {
			return fmt.Errorf("bench: task %q changed after freeze", t.ID)
		}
	}
	return nil
}
