package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FrozenTask identifies one task and its evaluator/base material in an official
// run manifest.
type FrozenTask struct {
	ID            string `json:"id"`
	TaskHash      string `json:"task_hash"`
	EvaluatorHash string `json:"evaluator_hash"`
	BaseCommit    string `json:"base_commit"`
	FixtureHash   string `json:"fixture_hash,omitempty"`
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
	official = official || s.Official
	if official && s.Smoke {
		return FrozenManifest{}, fmt.Errorf("bench: refusing to freeze smoke suite %q as official", s.ID)
	}
	if official && (s.Model.Provider == "" || s.Model.Model == "" || s.Model.ContextTokens <= 0) {
		return FrozenManifest{}, fmt.Errorf("bench: refusing to mark suite %q official without provider, model, and context_tokens", s.ID)
	}
	if official {
		for _, task := range s.Tasks {
			if task.WorkerDriver != "" {
				return FrozenManifest{}, fmt.Errorf("bench: refusing to mark task %q official with worker_driver %q", task.ID, task.WorkerDriver)
			}
			if len(task.Evaluator.Files) == 0 && len(task.Evaluator.InlineFiles) == 0 && task.Evaluator.Oracle == "" {
				return FrozenManifest{}, fmt.Errorf("bench: refusing to mark task %q official without hidden evaluator material", task.ID)
			}
		}
	}
	m := FrozenManifest{SchemaVersion: ManifestSchemaVersion, SuiteID: s.ID, SuiteVersion: s.Version,
		SuiteHash: s.Hash(), Model: s.Model, Runner: s.Runner.effective(), Limits: map[string]Limits{},
		NetworkPolicies: map[string]string{}, ExecutionSeed: seed, CreatedAt: time.Now().UTC(), Official: official}
	for _, t := range s.Tasks {
		m.Tasks = append(m.Tasks, FrozenTask{ID: t.ID, TaskHash: taskHash(t), EvaluatorHash: t.hiddenHash, BaseCommit: t.BaseCommit, FixtureHash: t.FixtureHash})
		m.Limits[t.ID] = EffectiveLimits(t.Limits)
		m.NetworkPolicies[t.ID] = t.NetworkPolicy
	}
	sort.Slice(m.Tasks, func(i, j int) bool { return m.Tasks[i].ID < m.Tasks[j].ID })
	// The code revision is best effort for a smoke/non-official manifest and
	// required for an official one. An official run without an exact code
	// identity is not reproducible enough to publish.
	commit, dirty, ok := harnessBuildRevision()
	if !ok {
		if official {
			return FrozenManifest{}, fmt.Errorf("bench: cannot determine the embedded BoundedCode revision for an official manifest")
		}
	} else {
		m.BoundedCodeCommit = commit
	}
	if official && dirty {
		return FrozenManifest{}, fmt.Errorf("bench: official manifests require a clean BoundedCode build")
	}
	return m, nil
}

func ManifestHash(m FrozenManifest) string {
	// Creation time is provenance metadata, not an experimental input. Excluding
	// it keeps a regenerated freeze for the same suite/seed comparable.
	identity := m
	identity.CreatedAt = time.Time{}
	body, _ := json.Marshal(identity)
	return "manifest-" + digestBytes(body)[:32]
}

func WriteManifest(path string, m FrozenManifest) error {
	if path == "" {
		return fmt.Errorf("bench: manifest path is empty")
	}
	return withOutputLock(context.Background(), filepath.Dir(path), func() error {
		return writeManifestUnlocked(path, m)
	})
}

func writeManifestUnlocked(path string, m FrozenManifest) error {
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomicFile(path, append(body, '\n'), 0o600)
}

func LoadManifest(path string) (FrozenManifest, error) {
	body, err := os.ReadFile(path) //nolint:gosec // operator-selected manifest
	if err != nil {
		return FrozenManifest{}, err
	}
	var m FrozenManifest
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return FrozenManifest{}, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return FrozenManifest{}, fmt.Errorf("bench: manifest %s contains multiple JSON values", path)
		}
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
	return VerifyFrozenAt(context.Background(), s, m)
}

func VerifyFrozenAt(ctx context.Context, s Suite, m FrozenManifest) error {
	if m.SchemaVersion != ManifestSchemaVersion {
		return fmt.Errorf("bench: unsupported manifest schema %d", m.SchemaVersion)
	}
	if s.Hash() != m.SuiteHash || m.SuiteID != s.ID || m.SuiteVersion != s.Version {
		return fmt.Errorf("bench: suite identity changed: manifest %s/%s, current %s/%s", m.SuiteID, m.SuiteHash, s.ID, s.Hash())
	}
	if !sameFingerprintValue(s.Model, m.Model) || !sameFingerprintValue(s.Runner.effective(), m.Runner.effective()) {
		return fmt.Errorf("bench: model or runner configuration differs from the frozen manifest")
	}
	if s.Official && !m.Official {
		return fmt.Errorf("bench: official suite %q requires an official manifest", s.ID)
	}
	if m.Official && strings.TrimSpace(m.BoundedCodeCommit) == "" {
		return fmt.Errorf("bench: official manifest has no BoundedCode revision")
	}
	if m.BoundedCodeCommit != "" {
		current, dirty, ok := harnessBuildRevision()
		if !ok {
			return fmt.Errorf("bench: cannot verify the embedded BoundedCode revision")
		}
		if current != m.BoundedCodeCommit {
			return fmt.Errorf("bench: BoundedCode revision changed: manifest %s, current %s", m.BoundedCodeCommit, current)
		}
		if m.Official && dirty {
			return fmt.Errorf("bench: official run requires a clean BoundedCode build")
		}
	}
	byID := make(map[string]FrozenTask, len(m.Tasks))
	for _, t := range m.Tasks {
		if t.ID == "" {
			return fmt.Errorf("bench: frozen manifest contains a task with no id")
		}
		if _, exists := byID[t.ID]; exists {
			return fmt.Errorf("bench: frozen manifest repeats task %q", t.ID)
		}
		byID[t.ID] = t
	}
	if len(byID) != len(s.Tasks) {
		return fmt.Errorf("bench: frozen task set has %d tasks, current suite has %d", len(byID), len(s.Tasks))
	}
	if len(m.Limits) != len(s.Tasks) || len(m.NetworkPolicies) != len(s.Tasks) {
		return fmt.Errorf("bench: frozen manifest task limit/network maps do not match the suite task set")
	}
	for _, t := range s.Tasks {
		frozen, ok := byID[t.ID]
		if !ok {
			return fmt.Errorf("bench: task %q is not in the frozen manifest", t.ID)
		}
		if frozen.TaskHash != taskHash(t) || frozen.EvaluatorHash != t.hiddenHash || frozen.BaseCommit != t.BaseCommit || frozen.FixtureHash != t.FixtureHash {
			return fmt.Errorf("bench: task %q changed after freeze", t.ID)
		}
		limits, hasLimits := m.Limits[t.ID]
		network, hasNetwork := m.NetworkPolicies[t.ID]
		if !hasLimits || !hasNetwork || limits != EffectiveLimits(t.Limits) || network != t.NetworkPolicy {
			return fmt.Errorf("bench: task %q limits or network policy changed after freeze", t.ID)
		}
	}
	return nil
}
