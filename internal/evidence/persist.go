package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ResultDir is the immutable, keyed location a completed run's artifacts
// live at — design instruction (GAP C) §12: keyed by suite/manifest hash,
// task, arm, run ID, under the existing guarded evidence run root, never a
// second artifact convention.
func ResultDir(runRoot, suiteHash, taskID string, arm Arm, runID string) string {
	return filepath.Join(runRoot, "results", suiteHash, taskID, armDir(arm), runID)
}

// ResultFiles are the exact filenames PersistResult writes, in one place
// so callers (report/status) name them the same way.
const (
	ResultJSONFile     = "result.json"
	PatchFile          = "patch.diff"
	EvaluatorRawFile   = "evaluator.raw"
	TelemetryFile      = "telemetry.json"
	JudgmentTraceFile  = "judgments.jsonl"
	ArtifactHashesFile = "artifact-hashes.json"
)

// ArtifactHashes records the SHA-256 of every artifact PersistResult wrote
// — design instruction §15: computed once, over the exact bytes written,
// never recomputed after a mutation.
type ArtifactHashes struct {
	ResultJSONSHA256    string `json:"result_json_sha256"`
	PatchSHA256         string `json:"patch_sha256,omitempty"`
	EvaluatorRawSHA256  string `json:"evaluator_raw_sha256,omitempty"`
	TelemetrySHA256     string `json:"telemetry_sha256"`
	JudgmentTraceSHA256 string `json:"judgment_trace_sha256,omitempty"`
}

// Telemetry is what §17 asks be extracted from the real BoundedCode
// outcome/ledger data — every field here comes directly from
// internal/task.Outcome; nothing is estimated. A zero value for an
// int field that outcome does not populate is indistinguishable from "0
// events", which is the honest state of internal/task.Outcome today — see
// docs/evidence/evidence-v1.md's "Known limitations" for the fields this
// package cannot yet break out per-attempt (a future Outcome field, not
// invented here).
type Telemetry struct {
	Attempts   int   `json:"attempts"`
	TokensUsed int   `json:"generator_tokens_used"`
	WallTimeMS int64 `json:"wall_time_ms"`
	PatchBytes int   `json:"patch_bytes"`
	// Jev is the treatment arm's aggregate telemetry (§15/§16), computed
	// from the real, already-persisted judgments.jsonl rows — nil for
	// CONTROL runs, which never attach a Judge (§17).
	Jev *JevSummary `json:"jev,omitempty"`
}

// JudgmentTelemetry is one exported line of §18's Jev telemetry — a
// structured view over judgment.Recorder's own journalled data, never a
// second logging path and never a fresh Jev call made for telemetry's
// sake. Population of this from the real judgment ledger is a follow-on
// step gated on a treatment run actually executing (see execute.go's
// TreatmentJudge wiring); this type exists now so PersistResult's schema
// is final and the file is written (possibly empty) for every treatment
// run.
type JudgmentTelemetry struct {
	Site           string  `json:"site"`
	SiteVersion    string  `json:"site_version"`
	QuestionID     string  `json:"question_id,omitempty"`
	Model          string  `json:"model,omitempty"`
	Confidence     float64 `json:"confidence,omitempty"`
	ProposedEffect string  `json:"proposed_effect,omitempty"`
	AppliedEffect  string  `json:"applied_effect,omitempty"`
	Tier           string  `json:"tier,omitempty"`
	Intervened     bool    `json:"intervened"`
	LatencyMS      int64   `json:"latency_ms,omitempty"`
	InputTokens    int     `json:"input_tokens,omitempty"`
}

// PersistResult writes one completed run's artifacts under ResultDir,
// atomically per file (write to a sibling temp file in the same
// directory, then os.Rename — same-filesystem rename is atomic on every
// platform this project targets) and refuses outright if the run's
// result.json already exists — design instruction §13/§14: a completed
// result is immutable; a retry gets a new run ID instead of overwriting
// this one.
func PersistResult(runRoot string, res Result, patch, evaluatorRaw []byte, judgments []JudgmentTelemetry) (ArtifactHashes, error) {
	dir := ResultDir(runRoot, res.SuiteHash, res.TaskID, res.Arm, res.RunID)
	final := filepath.Join(dir, ResultJSONFile)
	if _, err := os.Stat(final); err == nil {
		return ArtifactHashes{}, fmt.Errorf("evidence: %s already has a persisted result at %s — "+
			"a completed run is immutable; retry with a new run ID (NewRunID) instead", res.RunID, final)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ArtifactHashes{}, err
	}

	var hashes ArtifactHashes

	if len(patch) > 0 {
		if err := atomicWrite(filepath.Join(dir, PatchFile), patch); err != nil {
			return ArtifactHashes{}, err
		}
		hashes.PatchSHA256 = sha256Hex(patch)
	}
	if len(evaluatorRaw) > 0 {
		if err := atomicWrite(filepath.Join(dir, EvaluatorRawFile), evaluatorRaw); err != nil {
			return ArtifactHashes{}, err
		}
		hashes.EvaluatorRawSHA256 = sha256Hex(evaluatorRaw)
	}

	telemetry := Telemetry{
		Attempts: res.Attempts, TokensUsed: res.TokensUsed,
		WallTimeMS: res.WallTimeMS, PatchBytes: res.PatchBytes,
	}
	if res.Arm == ArmExperimentalJevFull {
		summary := SummarizeJudgmentTelemetry(judgments)
		telemetry.Jev = &summary
	}
	telemetryBody, err := json.MarshalIndent(telemetry, "", "  ")
	if err != nil {
		return ArtifactHashes{}, err
	}
	if err := atomicWrite(filepath.Join(dir, TelemetryFile), telemetryBody); err != nil {
		return ArtifactHashes{}, err
	}
	hashes.TelemetrySHA256 = sha256Hex(telemetryBody)

	if res.Arm == ArmExperimentalJevFull {
		var jbuf []byte
		for _, j := range judgments {
			line, err := json.Marshal(j)
			if err != nil {
				return ArtifactHashes{}, err
			}
			jbuf = append(jbuf, line...)
			jbuf = append(jbuf, '\n')
		}
		if err := atomicWrite(filepath.Join(dir, JudgmentTraceFile), jbuf); err != nil {
			return ArtifactHashes{}, err
		}
		if len(jbuf) > 0 {
			hashes.JudgmentTraceSHA256 = sha256Hex(jbuf)
		}
	}

	res.PatchSHA256 = hashes.PatchSHA256
	if hashes.EvaluatorRawSHA256 != "" {
		res.EvaluatorOutputSHA256 = hashes.EvaluatorRawSHA256
	}
	resultBody, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return ArtifactHashes{}, err
	}
	hashes.ResultJSONSHA256 = sha256Hex(resultBody)

	// artifact-hashes.json is what a later loader (GAP 4's report
	// builder) re-verifies against — design instruction §15/§28. Written
	// before result.json for the same reason every other artifact is:
	// result.json's presence is "this run is completed," so the record a
	// later integrity check needs must already be durable first.
	hashesBody, err := json.MarshalIndent(hashes, "", "  ")
	if err != nil {
		return ArtifactHashes{}, err
	}
	if err := atomicWrite(filepath.Join(dir, ArtifactHashesFile), hashesBody); err != nil {
		return ArtifactHashes{}, err
	}

	// result.json is written last and atomically: its presence is what
	// PersistResult itself treats as "this run is completed" (the
	// no-overwrite check above), so every other artifact must already be
	// durable on disk before this file exists.
	if err := atomicWrite(final, resultBody); err != nil {
		return ArtifactHashes{}, err
	}

	return hashes, nil
}

// LoadResult reads back a previously persisted result.json — used by
// status/report so they consume the real durable record rather than
// in-memory execution state (design instruction §19/§21).
func LoadResult(runRoot, suiteHash, taskID string, arm Arm, runID string) (Result, error) {
	body, err := os.ReadFile(filepath.Join(ResultDir(runRoot, suiteHash, taskID, arm, runID), ResultJSONFile))
	if err != nil {
		return Result{}, err
	}
	var res Result
	if err := json.Unmarshal(body, &res); err != nil {
		return Result{}, err
	}
	return res, nil
}

// LoadAndVerifyResult reads back a persisted run and re-hashes every
// artifact on disk against the ArtifactHashes PersistResult recorded at
// write time — design instruction GAP 4 §28: a result whose recorded hash
// no longer matches its bytes is never included in aggregation. It also
// cross-checks the loaded Result's own identity fields against what the
// caller expects (suite hash, task, arm, run ID) — §20's "reject
// corrupted/mismatched result records... do not silently include stale
// results from another suite hash."
func LoadAndVerifyResult(runRoot, suiteHash, taskID string, arm Arm, runID string) (Result, error) {
	dir := ResultDir(runRoot, suiteHash, taskID, arm, runID)

	res, err := LoadResult(runRoot, suiteHash, taskID, arm, runID)
	if err != nil {
		return Result{}, err
	}
	if res.SuiteHash != suiteHash || res.TaskID != taskID || res.Arm != arm || res.RunID != runID {
		return Result{}, fmt.Errorf("evidence: %s: result.json identity does not match its own "+
			"path (suite=%s task=%s arm=%s run=%s vs. path-implied suite=%s task=%s arm=%s run=%s)",
			dir, res.SuiteHash, res.TaskID, res.Arm, res.RunID, suiteHash, taskID, arm, runID)
	}

	hashesBody, err := os.ReadFile(filepath.Join(dir, ArtifactHashesFile))
	if err != nil {
		return Result{}, fmt.Errorf("evidence: %s: missing %s: %w", dir, ArtifactHashesFile, err)
	}
	var want ArtifactHashes
	if err := json.Unmarshal(hashesBody, &want); err != nil {
		return Result{}, fmt.Errorf("evidence: %s: corrupt %s: %w", dir, ArtifactHashesFile, err)
	}

	check := func(file, wantHash string) error {
		if wantHash == "" {
			return nil
		}
		body, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		if got := sha256Hex(body); got != wantHash {
			return fmt.Errorf("%s: hash mismatch (recorded %s, actual %s)", file, wantHash, got)
		}
		return nil
	}
	if err := check(PatchFile, want.PatchSHA256); err != nil {
		return Result{}, fmt.Errorf("evidence: %s: %w", dir, err)
	}
	if err := check(EvaluatorRawFile, want.EvaluatorRawSHA256); err != nil {
		return Result{}, fmt.Errorf("evidence: %s: %w", dir, err)
	}
	if err := check(TelemetryFile, want.TelemetrySHA256); err != nil {
		return Result{}, fmt.Errorf("evidence: %s: %w", dir, err)
	}
	if err := check(JudgmentTraceFile, want.JudgmentTraceSHA256); err != nil {
		return Result{}, fmt.Errorf("evidence: %s: %w", dir, err)
	}
	resultBody, err := os.ReadFile(filepath.Join(dir, ResultJSONFile))
	if err != nil {
		return Result{}, err
	}
	if got := sha256Hex(resultBody); got != want.ResultJSONSHA256 {
		return Result{}, fmt.Errorf("evidence: %s: %s: hash mismatch (recorded %s, actual %s)",
			dir, ResultJSONFile, want.ResultJSONSHA256, got)
	}

	return res, nil
}

func atomicWrite(path string, body []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
