package bench

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadSuite reads a suite manifest and all referenced task definitions.  A
// suite path may be the directory containing suite.yaml or the manifest
// itself.  Nothing is executed and no task is materialized here.
func LoadSuite(path string) (Suite, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Suite{}, err
	}
	manifest := abs
	if info, statErr := os.Stat(abs); statErr == nil && info.IsDir() {
		manifest = filepath.Join(abs, "suite.yaml")
	}
	body, err := os.ReadFile(manifest) //nolint:gosec // operator-selected suite manifest
	if err != nil {
		return Suite{}, fmt.Errorf("bench: read suite: %w", err)
	}
	var s Suite
	dec := yaml.NewDecoder(strings.NewReader(string(body)))
	dec.KnownFields(true)
	if err := dec.Decode(&s); err != nil {
		return Suite{}, fmt.Errorf("bench: parse %s: %w", manifest, err)
	}
	if s.SchemaVersion == 0 {
		s.SchemaVersion = SuiteSchemaVersion
	}
	s.path = manifest
	if len(s.TaskRefs) == 0 {
		// The conventional layout is suite.yaml beside tasks/<id>/task.yaml.
		taskRoot := filepath.Join(filepath.Dir(manifest), "tasks")
		entries, readErr := os.ReadDir(taskRoot)
		if readErr == nil {
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				candidate := filepath.Join(taskRoot, e.Name(), "task.yaml")
				if _, statErr := os.Stat(candidate); statErr == nil {
					s.TaskRefs = append(s.TaskRefs, filepath.Join("tasks", e.Name(), "task.yaml"))
				}
			}
		}
	}
	sort.Strings(s.TaskRefs)
	if len(s.TaskRefs) == 0 {
		return Suite{}, fmt.Errorf("bench: suite %s has no task references", manifest)
	}
	seen := map[string]bool{}
	for _, ref := range s.TaskRefs {
		taskPath := ref
		if !filepath.IsAbs(taskPath) {
			taskPath = filepath.Join(filepath.Dir(manifest), filepath.FromSlash(ref))
		}
		t, loadErr := loadTask(taskPath)
		if loadErr != nil {
			return Suite{}, loadErr
		}
		if seen[t.ID] {
			return Suite{}, fmt.Errorf("bench: duplicate task id %q", t.ID)
		}
		seen[t.ID] = true
		s.Tasks = append(s.Tasks, t)
	}
	if err := s.Validate(); err != nil {
		return Suite{}, err
	}
	s.hash = suiteHash(s)
	return s, nil
}

func loadTask(path string) (Task, error) {
	body, err := os.ReadFile(path) //nolint:gosec // operator-selected task manifest
	if err != nil {
		return Task{}, fmt.Errorf("bench: read task: %w", err)
	}
	var t Task
	dec := yaml.NewDecoder(strings.NewReader(string(body)))
	dec.KnownFields(true)
	if err := dec.Decode(&t); err != nil {
		return Task{}, fmt.Errorf("bench: parse %s: %w", path, err)
	}
	t.path, _ = filepath.Abs(path)
	if t.SchemaVersion == 0 {
		t.SchemaVersion = TaskSchemaVersion
	}
	if err := t.Validate(); err != nil {
		return Task{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := loadHiddenMaterial(&t); err != nil {
		return Task{}, fmt.Errorf("%s: %w", path, err)
	}
	// Re-validate after loading because a command-only evaluator can be
	// rejected when its oracle is outside the fixture.
	if err := t.Validate(); err != nil {
		return Task{}, fmt.Errorf("%s: %w", path, err)
	}
	return t, nil
}

func loadHiddenMaterial(t *Task) error {
	t.hiddenFiles = map[string][]byte{}
	if t.Evaluator.Oracle != "" {
		root := t.Evaluator.Oracle
		if !filepath.IsAbs(root) {
			root = filepath.Join(filepath.Dir(t.path), filepath.FromSlash(root))
		}
		root, err := filepath.Abs(root)
		if err != nil {
			return err
		}
		info, err := os.Stat(root)
		if err != nil {
			return fmt.Errorf("evaluator oracle: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("evaluator oracle %s is not a directory", root)
		}
		t.oracleRoot = root
		// A fixture is the worker's source.  An oracle below it would be
		// visible even if the evaluator did not explicitly copy it.
		if fixture := t.Fixture; fixture != "" {
			fixturePath := fixture
			if !filepath.IsAbs(fixturePath) {
				fixturePath = filepath.Join(filepath.Dir(t.path), filepath.FromSlash(fixture))
			}
			if inside, err := pathWithin(fixturePath, root); err != nil {
				return err
			} else if inside {
				return fmt.Errorf("oracle %s is inside fixture %s", root, fixturePath)
			}
		}
		for _, name := range t.Evaluator.Files {
			clean, err := safeRelative(name)
			if err != nil {
				return fmt.Errorf("evaluator file %q: %w", name, err)
			}
			full := filepath.Join(root, filepath.FromSlash(clean))
			// WalkDir is not needed for a declared file; reject a directory and
			// all symlink escapes explicitly.
			resolved, err := filepath.EvalSymlinks(full)
			if err != nil {
				return fmt.Errorf("evaluator file %s: %w", clean, err)
			}
			if !withinRoot(root, resolved) {
				return fmt.Errorf("evaluator file %s escapes oracle directory", clean)
			}
			data, err := os.ReadFile(resolved) //nolint:gosec // path checked below
			if err != nil {
				return fmt.Errorf("evaluator file %s: %w", clean, err)
			}
			t.hiddenFiles[clean] = append([]byte(nil), data...)
		}
	}
	for name, body := range t.Evaluator.InlineFiles {
		clean, err := safeRelative(name)
		if err != nil {
			return fmt.Errorf("inline evaluator file %q: %w", name, err)
		}
		t.hiddenFiles[clean] = []byte(body)
	}
	t.hiddenHash = hiddenDigest(t.hiddenFiles)
	return nil
}

func hiddenDigest(files map[string][]byte) string {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		fmt.Fprintf(h, "%d:%s=%d:", len(k), k, len(files[k]))
		h.Write(files[k])
	}
	return hex.EncodeToString(h.Sum(nil))
}

func suiteHash(s Suite) string {
	type identity struct {
		Schema      int               `json:"schema"`
		ID          string            `json:"id"`
		Version     string            `json:"version"`
		Official    bool              `json:"official"`
		Smoke       bool              `json:"smoke"`
		Model       ModelConfig       `json:"model"`
		Runner      RunnerConfig      `json:"runner"`
		Environment map[string]string `json:"environment"`
		Tasks       []taskIdentity    `json:"tasks"`
	}
	ids := make([]taskIdentity, 0, len(s.Tasks))
	for _, t := range s.Tasks {
		ids = append(ids, taskIdentityOf(t))
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].ID < ids[j].ID })
	body, _ := CanonicalJSON(identity{
		Schema: s.SchemaVersion, ID: s.ID, Version: s.Version, Official: s.Official,
		Smoke: s.Smoke, Model: s.Model, Runner: s.Runner, Environment: s.Environment,
		Tasks: ids,
	})
	return "suite-" + digestBytes(body)[:32]
}

type taskIdentity struct {
	ID            string `json:"id"`
	TaskHash      string `json:"task_hash"`
	EvaluatorHash string `json:"evaluator_hash"`
	BaseCommit    string `json:"base_commit"`
}

func taskIdentityOf(t Task) taskIdentity {
	return taskIdentity{ID: t.ID, TaskHash: taskHash(t), EvaluatorHash: t.hiddenHash, BaseCommit: t.BaseCommit}
}

func taskHash(t Task) string {
	// Do not include absolute paths or unexported loader state.  The evaluator
	// hash includes the hidden bytes, so a changed oracle changes the identity.
	type identity struct {
		Schema        int               `json:"schema"`
		ID            string            `json:"id"`
		Title         string            `json:"title"`
		Description   string            `json:"description"`
		Repository    string            `json:"repository"`
		Base          string            `json:"base_commit"`
		Fixture       string            `json:"fixture"`
		Languages     []string          `json:"languages"`
		Setup         Command           `json:"setup"`
		Visible       []Command         `json:"visible_validation"`
		Evaluator     Evaluator         `json:"evaluator"`
		Verification  string            `json:"verification"`
		EvaluatorHash string            `json:"evaluator_hash"`
		Limits        Limits            `json:"limits"`
		Network       string            `json:"network"`
		Environment   map[string]string `json:"environment"`
		Scope         []string          `json:"mutable_scope"`
		Tags          []string          `json:"tags"`
		Category      string            `json:"category"`
		Difficulty    map[string]string `json:"difficulty"`
		Provenance    map[string]string `json:"provenance"`
		Driver        string            `json:"worker_driver"`
	}
	body, _ := CanonicalJSON(identity{
		Schema: t.SchemaVersion, ID: t.ID, Title: t.Title, Description: t.Description,
		Repository: t.Repository, Base: t.BaseCommit, Fixture: t.Fixture,
		Languages: t.Languages, Setup: t.Setup, Visible: t.VisibleValidation,
		Evaluator: t.Evaluator, Verification: t.Verification, EvaluatorHash: t.hiddenHash, Limits: t.Limits,
		Network: t.NetworkPolicy, Environment: t.Environment, Scope: t.MutableScope,
		Tags: t.Tags, Category: t.Category, Difficulty: t.Difficulty,
		Provenance: t.Provenance, Driver: t.WorkerDriver,
	})
	return digestBytes(body)
}

func safeRelative(rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", errors.New("must be a non-empty relative path")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	if clean == ".." || strings.HasPrefix(clean, "../") || clean == "." {
		return "", errors.New("path escapes its root")
	}
	return clean, nil
}

func pathWithin(root, candidate string) (bool, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(absRoot, absCandidate)
	if err != nil {
		return false, err
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))), nil
}

func withinRoot(root, candidate string) bool {
	ok, err := pathWithin(root, candidate)
	return err == nil && ok
}

// TaskConfig is the sanitized task view crossing the worker boundary.
type TaskConfig struct {
	ID                string            `json:"id"`
	Title             string            `json:"title"`
	Description       string            `json:"description"`
	Repository        string            `json:"repository"`
	BaseCommit        string            `json:"base_commit"`
	Languages         []string          `json:"languages,omitempty"`
	Limits            Limits            `json:"limits"`
	NetworkPolicy     string            `json:"network_policy"`
	Environment       map[string]string `json:"environment,omitempty"`
	Verification      string            `json:"verification,omitempty"`
	MutableScope      []string          `json:"mutable_scope,omitempty"`
	Tags              []string          `json:"tags,omitempty"`
	Category          string            `json:"category,omitempty"`
	VisibleValidation []Command         `json:"visible_validation,omitempty"`
}

// WorkerTask strips evaluator and oracle state. Keep this as a value type and a
// single conversion so a future field cannot accidentally leak by default.
func WorkerTask(t Task) TaskConfig {
	return TaskConfig{
		ID: t.ID, Title: t.Title, Description: t.Description, Repository: t.Repository,
		BaseCommit: t.BaseCommit, Languages: append([]string(nil), t.Languages...),
		Limits: t.Limits, NetworkPolicy: t.NetworkPolicy,
		Environment: cloneStrings(t.Environment), Verification: t.Verification, MutableScope: append([]string(nil), t.MutableScope...),
		Tags: append([]string(nil), t.Tags...), Category: t.Category,
		VisibleValidation: append([]Command(nil), t.VisibleValidation...),
	}
}

func cloneStrings(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// ResolvedTask returns the input path selected for a task.  A fixture is a
// repository snapshot; otherwise Repository is resolved relative to the task
// manifest. URL repositories are returned unchanged for the materializer.
func (t Task) ResolvedRepository() string {
	if t.Fixture != "" {
		if filepath.IsAbs(t.Fixture) {
			return t.Fixture
		}
		return filepath.Join(filepath.Dir(t.path), filepath.FromSlash(t.Fixture))
	}
	if filepath.IsAbs(t.Repository) {
		return t.Repository
	}
	return filepath.Join(filepath.Dir(t.path), filepath.FromSlash(t.Repository))
}
