package judgeval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Manifest is the reproducibility record design instruction §21 requires:
// everything needed to regenerate a result from (repository revision +
// dataset + manifest) alone. A result artifact (results.go, once a runner
// writes one) always carries one of these.
type Manifest struct {
	SchemaVersion int `json:"schema_version"`

	// RepoRevision is `git rev-parse HEAD`, or "" outside a git checkout —
	// never fabricated.
	RepoRevision string `json:"repo_revision,omitempty"`
	// RepoDirty is true if the working tree had uncommitted changes when
	// this manifest was built, since a revision alone does not reproduce a
	// dirty tree's exact input.
	RepoDirty bool `json:"repo_dirty,omitempty"`

	Site        string `json:"site"`
	SiteVersion string `json:"site_version"`
	// DatasetPath and DatasetHash together pin the exact case set: the path
	// for a human, the hash (sha256 of the file's bytes) for verification
	// that a copy is the same file, since the file itself is expected to be
	// checked into the repository at DatasetPath alongside this manifest.
	DatasetPath string `json:"dataset_path"`
	DatasetHash string `json:"dataset_hash"`
	Split       Split  `json:"split"`

	// Model is the exact judge model id (e.g. "jev-1.13.0"), never an
	// alias — judgment.Config.PinnedModel already enforces this convention
	// for live use; a manifest restates it because a result is meaningless
	// once the model behind "jev-latest" has moved.
	Model string `json:"model,omitempty"`
	// PolicyVersion is PolicySchemaVersion at the time of the run, plus a
	// hash of the resolved SitePolicy actually used, so a later policy edit
	// cannot be mistaken for having applied retroactively to an old result.
	PolicyVersion int    `json:"policy_version"`
	PolicyHash    string `json:"policy_hash"`

	Arm  string `json:"arm"`
	Seed int64  `json:"seed"`

	// Live is false for every result an operator can regenerate offline
	// (judgment.Fake, a recorded dataset) and true only for a run that made
	// real network calls to the judge — design instruction §20: "live Jev
	// evaluation must be an explicit opt-in operation," and a manifest is
	// where that fact is recorded so a reader never has to guess whether a
	// number cost money.
	Live bool `json:"live"`

	// ConfigDigest is a hash of whatever configuration inputs materially
	// affect the result beyond what is already listed above (tuning
	// parameters, redaction mode) — a caller-supplied string this package
	// does not interpret.
	ConfigDigest string `json:"config_digest,omitempty"`

	// Timestamp is caller-supplied. This package's own functions never
	// stamp wall-clock time themselves; a manifest a caller builds without
	// setting Timestamp is still fully reproducible, which is the property
	// that matters more than knowing when a run happened.
	Timestamp string `json:"timestamp,omitempty"`
}

// HashFile returns the sha256 of a file's bytes, hex-encoded, for use as a
// Manifest.DatasetHash.
func HashFile(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

// HashPolicy hashes a SitePolicy's canonical JSON encoding, so
// Manifest.PolicyHash changes if and only if a threshold or class actually
// changed.
func HashPolicy(p SitePolicy) (string, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

// GitRevision reports the current commit and whether the tree is dirty.
// Best-effort: outside a git checkout, or if git is unavailable, it returns
// zero values rather than an error — a manifest with an empty RepoRevision
// is still useful; a run that cannot proceed at all because git is missing
// would not be.
func GitRevision(ctx context.Context, repoDir string) (rev string, dirty bool) {
	out, err := runGit(ctx, repoDir, "rev-parse", "HEAD")
	if err != nil {
		return "", false
	}
	rev = strings.TrimSpace(out)
	status, err := runGit(ctx, repoDir, "status", "--porcelain")
	if err == nil && strings.TrimSpace(status) != "" {
		dirty = true
	}
	return rev, dirty
}

// runGit runs one git subcommand under the caller's context, so cancelling
// the campaign stops git rather than leaving it to finish on its own. The
// executable is the fixed name "git", resolved through PATH, and no argument
// reaches a shell: exec.CommandContext passes argv directly.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}

// SaveResult writes a campaign result as indented JSON, creating its
// directory.
//
// It lives here rather than in cmd/bcode so the storescope exemption stays on
// this package with one justification rather than on the whole CLI, which is
// the same reasoning internal/eval's exemption gives.
func SaveResult(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

// SaveManifest writes m as indented JSON.
func SaveManifest(path string, m Manifest) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

// LoadManifest reads a manifest previously written by SaveManifest.
func LoadManifest(path string) (Manifest, error) {
	var m Manifest
	body, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	err = json.Unmarshal(body, &m)
	return m, err
}
