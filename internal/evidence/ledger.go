package evidence

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Status is one run's durable state — design instruction §12. A simple
// structured ledger, not a workflow engine: every record is one JSON line
// appended to a per-suite file, and the current status of a (task, arm)
// pair is the last record for that key.
type Status string

const (
	StatusPending  Status = "PENDING"
	StatusRunning  Status = "RUNNING"
	StatusSolved   Status = "SOLVED"
	StatusFailed   Status = "FAILED"
	StatusInfraErr Status = "INFRA_ERROR"
	// StatusInvalid covers a run whose evidence cannot be trusted for a
	// reason unrelated to whether the generated patch was correct —
	// design instruction §12's ANTI_LEAKAGE_INVALID/ORACLE_INVALID/
	// GRADER_INVALID are all represented as StatusInvalid with the
	// specific reason in InvalidReason, rather than three more top-level
	// statuses every switch in this package would need to handle
	// identically.
	StatusInvalid Status = "INVALID"
)

// InvalidReason names why a run is StatusInvalid, when it is.
type InvalidReason string

const (
	InvalidReasonAntiLeakage  InvalidReason = "ANTI_LEAKAGE_INVALID"
	InvalidReasonOracle       InvalidReason = "ORACLE_INVALID"
	InvalidReasonGrader       InvalidReason = "GRADER_INVALID"
	InvalidReasonArmConfig    InvalidReason = "ARM_CONFIG_INVALID"
	InvalidReasonHashMismatch InvalidReason = "FROZEN_HASH_MISMATCH"
)

// terminal reports whether a status is a final state a resume must never
// silently overwrite by rerunning — design instruction §14/§15: SOLVED and
// FAILED are both terminal and both are real results; only INFRA_ERROR is
// retryable, and INVALID requires explicit operator action.
func (s Status) terminal() bool {
	switch s {
	case StatusSolved, StatusFailed, StatusInvalid:
		return true
	default:
		return false
	}
}

// Retryable reports whether resume may automatically start a new run for
// this status without any operator action — true only for INFRA_ERROR and
// for an interrupted RUNNING record (handled specially in Resume, not via
// this method, since a live RUNNING record found at startup means the
// previous process died mid-run, not that this run naturally reached that
// state).
func (s Status) Retryable() bool { return s == StatusInfraErr }

// Record is one immutable ledger entry. Every write appends a new Record;
// none is ever edited or deleted — design instruction §13/§16's
// append-only rule, enforced here by Ledger only ever calling os.O_APPEND.
type Record struct {
	SuiteHash     string        `json:"suite_hash"`
	TaskID        string        `json:"task_id"`
	Arm           Arm           `json:"arm"`
	RunID         string        `json:"run_id"`
	Status        Status        `json:"status"`
	InvalidReason InvalidReason `json:"invalid_reason,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
	// PreviousRunID links a retry to the attempt it replaces — design
	// instruction §13/§14. Empty for a run's first attempt.
	PreviousRunID string `json:"previous_run_id,omitempty"`
	RetryReason   string `json:"retry_reason,omitempty"`
	// Detail is a short human-readable note — the reason a status was
	// reached, never a place for secret values.
	Detail string `json:"detail,omitempty"`
}

// Ledger is the durable, append-only run-state store for one suite. It is
// a single JSONL file under the guarded run root — design instruction §12's
// "do not overbuild a workflow engine; a simple structured run ledger is
// sufficient."
type Ledger struct {
	path string
}

// OpenLedger opens (creating if absent) the ledger file at path.
func OpenLedger(path string) (*Ledger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0o644)
	if err != nil {
		return nil, err
	}
	f.Close()
	return &Ledger{path: path}, nil
}

// Append writes one record. It never rewrites or truncates the file.
func (l *Ledger) Append(r Record) error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(body, '\n')); err != nil {
		return err
	}
	return nil
}

// All reads every record in the ledger, in file order (which is
// chronological, since Append only ever appends).
func (l *Ledger) All() ([]Record, error) {
	f, err := os.Open(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r Record
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, fmt.Errorf("evidence: corrupt ledger line: %w", err)
		}
		out = append(out, r)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Current returns the latest record for every (task, arm) key — the
// current status of each planned run. A key with no record at all is not
// included; the caller (Resume) treats an absent key as PENDING.
func (l *Ledger) Current() (map[string]Record, error) {
	all, err := l.All()
	if err != nil {
		return nil, err
	}
	out := map[string]Record{}
	for _, r := range all {
		key := r.TaskID + "|" + string(r.Arm)
		out[key] = r // later records overwrite earlier ones; All() is in file order
	}
	return out, nil
}

// ResumeAction is what Resume decides to do for one planned run.
type ResumeAction string

const (
	// ActionSkip means the run already reached a terminal, non-retryable
	// state; resume must never touch it again — design instruction §14/§15.
	ActionSkip ResumeAction = "SKIP_TERMINAL"
	// ActionStart means no record exists yet; a first attempt should run.
	ActionStart ResumeAction = "START_FIRST_ATTEMPT"
	// ActionRetryInfra means the last record was INFRA_ERROR, which design
	// instruction §14 explicitly permits retrying.
	ActionRetryInfra ResumeAction = "RETRY_INFRA_ERROR"
	// ActionRecoverInterrupted means the last record was RUNNING with no
	// terminal record after it — the previous process died mid-run.
	// Resume must mark that attempt infrastructure-interrupted (never
	// silently overwrite it) and start a new, linked run ID — design
	// instruction §14's "on resume: mark previous attempt as
	// infrastructure-interrupted and create a new linked run ID."
	ActionRecoverInterrupted ResumeAction = "RECOVER_INTERRUPTED_RUNNING"
)

// Decide computes the resume action for one planned run from its latest
// ledger record (or its absence). This is the enforcement point for design
// instruction §15's "this rule must be enforced in code, not merely
// documented" — no caller can accidentally rerun a SOLVED or FAILED run
// through this function, because it never returns an action that would
// start one for those statuses.
func Decide(latest *Record) ResumeAction {
	if latest == nil {
		return ActionStart
	}
	switch latest.Status {
	case StatusSolved, StatusFailed, StatusInvalid:
		return ActionSkip
	case StatusInfraErr:
		return ActionRetryInfra
	case StatusRunning:
		return ActionRecoverInterrupted
	default:
		// An unrecognized status is treated the same as a terminal one:
		// failing toward "do nothing automatically" is the safe direction
		// for a status this code does not understand, matching §41's "no
		// solve result overrides these" safety-first posture.
		return ActionSkip
	}
}

// NewRunID is a suite-scoped, sortable, unique run identifier.
func NewRunID(taskID string, arm Arm) string {
	return fmt.Sprintf("%s-%s-%d", shortTaskID(taskID), armSlug(arm), time.Now().UnixNano())
}

func shortTaskID(id string) string {
	if len(id) > 24 {
		return id[:24]
	}
	return id
}

func armSlug(a Arm) string {
	if a == ArmControl {
		return "control"
	}
	return "treatment"
}

// Summary aggregates ledger state by status, for `bcode evidence status`.
type Summary struct {
	Planned  int            `json:"planned"`
	ByStatus map[Status]int `json:"by_status"`
}

// Summarize computes a Summary over plans and the ledger's current state.
func Summarize(plans []RunPlan, current map[string]Record) Summary {
	s := Summary{Planned: len(plans), ByStatus: map[Status]int{}}
	for _, p := range plans {
		rec, ok := current[p.Key()]
		if !ok {
			s.ByStatus[StatusPending]++
			continue
		}
		s.ByStatus[rec.Status]++
	}
	return s
}

// SortedKeys returns plan keys in RunOrder — the frozen sequence, never
// map iteration order — for a deterministic `bcode evidence status` listing.
func SortedKeys(plans []RunPlan) []string {
	ordered := Ordered(plans)
	out := make([]string, 0, len(ordered))
	for _, p := range ordered {
		out = append(out, p.Key())
	}
	return out
}
