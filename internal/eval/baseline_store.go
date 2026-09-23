package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/akynte/boundedcode/internal/recipe"
)

// BaselineDir is where recorded baselines live.
//
// Beside the provisioning caches rather than inside a workspace: a baseline
// describes a task's fixture in a pinned runtime, which is the same fact for
// every workspace that runs it, and recomputing it per run would spend
// minutes of a measured batch re-establishing something that cannot have
// changed.
func BaselineDir() string {
	if d := os.Getenv("BC_BASELINE_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("BC_DATA"); d != "" {
		return filepath.Join(d, "provisioning", "baselines")
	}
	return filepath.Join(RuntimeWorkDir(), "baselines")
}

// baselinePath names the record for one task's fixture. The fixture digest is
// in the name, so editing the fixture cannot silently reuse a baseline
// measured against different code.
func baselinePath(taskID, fixtureDigest string) string {
	sum := sha256.Sum256([]byte(taskID + "\x00" + fixtureDigest))
	return filepath.Join(BaselineDir(), taskID+"-"+hex.EncodeToString(sum[:4])+".json")
}

// SaveBaseline records a task's baseline.
func SaveBaseline(b *recipe.Baseline) error {
	if b == nil {
		return nil
	}
	if err := os.MkdirAll(BaselineDir(), 0o755); err != nil {
		return fmt.Errorf("baseline directory: %w", err)
	}
	body, err := json.MarshalIndent(b, "", " ")
	if err != nil {
		return fmt.Errorf("encoding the baseline: %w", err)
	}
	path := baselinePath(b.TaskID, b.FixtureDigest)
	if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing the baseline: %w", err)
	}
	return nil
}

// LoadBaseline reads a task's baseline, or nil when none was recorded for
// this fixture. A missing baseline is not an error: without one the absolute
// rule applies, which is stricter, not looser.
func LoadBaseline(taskID, fixtureDigest string) *recipe.Baseline {
	body, err := os.ReadFile(baselinePath(taskID, fixtureDigest))
	if err != nil {
		return nil
	}
	var b recipe.Baseline
	if err := json.Unmarshal(body, &b); err != nil {
		return nil
	}
	if b.TaskID != taskID || b.FixtureDigest != fixtureDigest {
		return nil
	}
	return &b
}

// BaselineFor loads the baseline recorded for this task's current fixture.
func BaselineFor(task Task) *recipe.Baseline {
	return LoadBaseline(task.ID, FixtureDigest(task.FixturePath()))
}
