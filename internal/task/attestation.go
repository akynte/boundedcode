package task

import (
	"time"

	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/worktree"
)

// VerificationPredicate names the record format appended to the evidence
// chain for every verification run. It changes when the fields' meaning does.
const VerificationPredicate = "https://boundedcode.dev/attestation/verification/v1"

// VerificationRecord is what one verification run attests to: which exact
// change was examined, against which base, by which checks and oracle, with
// which outcome. It names the full output of each check by artifact hash
// rather than repeating it, and a hidden check by ID and verdict only.
type VerificationRecord struct {
	Predicate        string           `json:"predicate"`
	TaskID           string           `json:"task_id"`
	Level            string           `json:"level"`
	Base             string           `json:"base"`
	PatchSHA256      string           `json:"patch_sha256"`
	SnapshotManifest string           `json:"snapshot_manifest"`
	Candidate        string           `json:"candidate"`
	OracleDigest     string           `json:"oracle_digest,omitempty"`
	HiddenChecks     []string         `json:"hidden_checks,omitempty"`
	Results          []AttestedResult `json:"results"`
	VerifiedAt       time.Time        `json:"verified_at"`
}

// AttestedResult is one check's outcome as the record carries it.
type AttestedResult struct {
	Recipe       string `json:"recipe"`
	Kind         string `json:"kind"`
	Status       string `json:"status"`
	ArtifactHash string `json:"artifact_hash,omitempty"`
	Headline     string `json:"headline,omitempty"`
}

func verificationRecord(t *Task, snap *worktree.Snapshot, candidate string,
	intent map[string]any, results []recipe.Result) VerificationRecord {

	rec := VerificationRecord{
		Predicate: VerificationPredicate, TaskID: t.ID, Level: string(t.Verification),
		Base: snap.Base, PatchSHA256: snap.PatchSHA256, SnapshotManifest: snap.Manifest,
		Candidate: candidate, VerifiedAt: time.Now().UTC(),
	}
	if digest, ok := intent["oracle_digest"].(string); ok {
		rec.OracleDigest = digest
	}
	if ids, ok := intent["hidden_checks"].([]string); ok {
		rec.HiddenChecks = ids
	}
	for _, res := range results {
		rec.Results = append(rec.Results, AttestedResult{
			Recipe: res.Recipe, Kind: string(res.Kind), Status: string(res.Status),
			ArtifactHash: res.ArtifactHash, Headline: res.Summary.Headline,
		})
	}
	return rec
}
