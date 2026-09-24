package bench

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RunSpec is one scheduled execution. PairID is stable for a task/repetition
// and is independent of which arm runs first.
type RunSpec struct {
	RunIndex   int    `json:"run_index"`
	RunID      string `json:"run_id"`
	PairID     string `json:"pair_id"`
	TaskID     string `json:"task_id"`
	Repetition int    `json:"repetition"`
	Mode       Mode   `json:"mode"`
	Seed       int64  `json:"seed"`
	SuiteHash  string `json:"suite_hash"`
}

func (s RunSpec) Validate() error {
	var problems []string
	if s.RunIndex < 0 {
		problems = append(problems, "run_index cannot be negative")
	}
	if strings.TrimSpace(s.RunID) == "" {
		problems = append(problems, "run_id is required")
	}
	if strings.TrimSpace(s.PairID) == "" {
		problems = append(problems, "pair_id is required")
	}
	if strings.TrimSpace(s.TaskID) == "" {
		problems = append(problems, "task_id is required")
	}
	if s.Repetition <= 0 {
		problems = append(problems, "repetition must be positive")
	}
	if !s.Mode.Valid() {
		problems = append(problems, fmt.Sprintf("invalid mode %q", s.Mode))
	}
	if strings.TrimSpace(s.SuiteHash) == "" {
		problems = append(problems, "suite_hash is required")
	}
	if len(problems) > 0 {
		return fmt.Errorf("bench run specification: %s", strings.Join(problems, "; "))
	}
	return nil
}

type Schedule struct {
	SchemaVersion int       `json:"schema_version"`
	SuiteID       string    `json:"suite_id"`
	SuiteVersion  string    `json:"suite_version"`
	SuiteHash     string    `json:"suite_hash"`
	Seed          int64     `json:"seed"`
	Runs          int       `json:"runs_per_task"`
	Modes         []Mode    `json:"modes"`
	Entries       []RunSpec `json:"entries"`
	Concurrency   int       `json:"concurrency"`
	Warnings      []string  `json:"warnings,omitempty"`
	GeneratedAt   string    `json:"generated_at"`
}

func (s Schedule) Validate() error {
	if s.SchemaVersion != 1 {
		return fmt.Errorf("bench: unsupported schedule schema %d", s.SchemaVersion)
	}
	if strings.TrimSpace(s.SuiteID) == "" || strings.TrimSpace(s.SuiteVersion) == "" || strings.TrimSpace(s.SuiteHash) == "" {
		return errors.New("bench: schedule suite identity is incomplete")
	}
	if s.Runs <= 0 || len(s.Modes) == 0 || len(s.Entries) == 0 || s.Concurrency <= 0 {
		return errors.New("bench: schedule has no runs/modes or an invalid concurrency")
	}
	seenMode := map[Mode]bool{}
	for _, mode := range s.Modes {
		if !mode.Valid() || seenMode[mode] {
			return fmt.Errorf("bench: schedule has invalid or duplicate mode %q", mode)
		}
		seenMode[mode] = true
	}
	seenRun := map[string]bool{}
	seenCell := map[string]bool{}
	pairTask := map[string]string{}
	pairRepetition := map[string]int{}
	pairModes := map[string]map[Mode]bool{}
	for i, spec := range s.Entries {
		if err := spec.Validate(); err != nil {
			return fmt.Errorf("bench: schedule entry %d: %w", i, err)
		}
		if spec.SuiteHash != s.SuiteHash {
			return fmt.Errorf("bench: schedule entry %d belongs to suite %s, not %s", i, spec.SuiteHash, s.SuiteHash)
		}
		if spec.Seed != s.Seed {
			return fmt.Errorf("bench: schedule entry %d has seed %d, schedule seed is %d", i, spec.Seed, s.Seed)
		}
		if !seenMode[spec.Mode] {
			return fmt.Errorf("bench: schedule entry %d uses unlisted mode %q", i, spec.Mode)
		}
		if spec.RunIndex != i {
			return fmt.Errorf("bench: schedule entry %d has run_index %d", i, spec.RunIndex)
		}
		if previous, ok := pairTask[spec.PairID]; ok && previous != spec.TaskID {
			return fmt.Errorf("bench: pair %q spans tasks %q and %q", spec.PairID, previous, spec.TaskID)
		}
		if previous, ok := pairRepetition[spec.PairID]; ok && previous != spec.Repetition {
			return fmt.Errorf("bench: pair %q spans repetitions %d and %d", spec.PairID, previous, spec.Repetition)
		}
		pairTask[spec.PairID] = spec.TaskID
		pairRepetition[spec.PairID] = spec.Repetition
		if pairModes[spec.PairID] == nil {
			pairModes[spec.PairID] = map[Mode]bool{}
		}
		pairModes[spec.PairID][spec.Mode] = true
		if seenRun[spec.RunID] {
			return fmt.Errorf("bench: schedule repeats run id %q", spec.RunID)
		}
		cell := spec.PairID + "\x00" + string(spec.Mode)
		if seenCell[cell] {
			return fmt.Errorf("bench: schedule repeats pair/mode %q/%q", spec.PairID, spec.Mode)
		}
		seenRun[spec.RunID] = true
		seenCell[cell] = true
	}
	for pair, modes := range pairModes {
		for _, mode := range s.Modes {
			if !modes[mode] {
				return fmt.Errorf("bench: pair %q is missing mode %q", pair, mode)
			}
		}
		if len(modes) != len(s.Modes) {
			return fmt.Errorf("bench: pair %q has an unexpected number of modes", pair)
		}
	}
	for pair, repetition := range pairRepetition {
		if repetition < 1 || repetition > s.Runs {
			return fmt.Errorf("bench: pair %q has repetition %d outside 1..%d", pair, repetition, s.Runs)
		}
	}
	if len(s.Entries) != len(pairModes)*len(s.Modes) {
		return fmt.Errorf("bench: schedule has %d entries for %d pairs and %d modes", len(s.Entries), len(pairModes), len(s.Modes))
	}
	return nil
}

// BuildSchedule creates a deterministic interleaved order. Each task/repeat
// cell is rotated from the seed, so RAW and BOUNDED are paired without
// putting every RAW trial before every BOUNDED trial.
func BuildSchedule(s Suite, modes []Mode, runs int, seed int64, selected []string) (Schedule, error) {
	if err := s.Validate(); err != nil {
		return Schedule{}, err
	}
	if runs <= 0 {
		return Schedule{}, fmt.Errorf("bench: runs must be positive")
	}
	if len(modes) == 0 {
		return Schedule{}, fmt.Errorf("bench: at least one mode is required")
	}
	seenMode := map[Mode]bool{}
	for _, m := range modes {
		if !m.Valid() {
			return Schedule{}, fmt.Errorf("bench: invalid mode %q", m)
		}
		if seenMode[m] {
			return Schedule{}, fmt.Errorf("bench: duplicate mode %q", m)
		}
		seenMode[m] = true
	}
	selectedSet := map[string]bool{}
	knownTasks := make(map[string]bool, len(s.Tasks))
	for _, task := range s.Tasks {
		knownTasks[task.ID] = true
	}
	for _, id := range selected {
		if !knownTasks[id] {
			return Schedule{}, fmt.Errorf("bench: selected task %q is not in suite %q", id, s.ID)
		}
		selectedSet[id] = true
	}
	tasks := append([]Task(nil), s.Tasks...)
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	var entries []RunSpec
	index := 0
	for _, t := range tasks {
		if len(selectedSet) > 0 && !selectedSet[t.ID] {
			continue
		}
		for repetition := 1; repetition <= runs; repetition++ {
			order := rotateModes(seed, t.ID, repetition, modes)
			pair := pairID(s.Hash(), t.ID, repetition, seed)
			for _, mode := range order {
				entries = append(entries, RunSpec{
					RunIndex: index, RunID: runID(s.Hash(), t.ID, repetition, mode, seed),
					PairID: pair, TaskID: t.ID, Repetition: repetition, Mode: mode,
					Seed: seed, SuiteHash: s.Hash(),
				})
				index++
			}
		}
	}
	if len(entries) == 0 {
		return Schedule{}, fmt.Errorf("bench: selected task set is empty")
	}
	concurrency := s.Runner.Concurrency
	if concurrency == 0 {
		concurrency = 1
	}
	warnings := []string{}
	if concurrency != 1 {
		warnings = append(warnings, "concurrency is opt-in and timing comparisons may not be comparable; this runner executes sequentially")
	}
	schedule := Schedule{SchemaVersion: 1, SuiteID: s.ID, SuiteVersion: s.Version,
		SuiteHash: s.Hash(), Seed: seed, Runs: runs, Modes: append([]Mode(nil), modes...),
		Entries: entries, Concurrency: 1, Warnings: warnings, GeneratedAt: nowString()}
	if err := schedule.Validate(); err != nil {
		return Schedule{}, err
	}
	return schedule, nil
}

func rotateModes(seed int64, task string, repetition int, modes []Mode) []Mode {
	if len(modes) < 2 {
		return append([]Mode(nil), modes...)
	}
	h := sha256.New()
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(seed)) //nolint:gosec // seed is not a size
	h.Write(buf[:])
	fmt.Fprintf(h, "%d:%s|%d", len(task), task, repetition)
	offset := int(binary.BigEndian.Uint32(h.Sum(nil)[:4])) % len(modes)
	out := make([]Mode, 0, len(modes))
	for i := range modes {
		out = append(out, modes[(i+offset)%len(modes)])
	}
	return out
}

func pairID(suiteHash, task string, repetition int, seed int64) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("pair-v2:%s:%s:%d:%d", suiteHash, task, repetition, seed)))
	return "pair-" + hexShort(h[:])
}

func runID(suiteHash, task string, repetition int, mode Mode, seed int64) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("run-v2:%s:%s:%d:%s:%d", suiteHash, task, repetition, mode, seed)))
	return "run-" + hexShort(h[:])
}

func hexShort(body []byte) string {
	const hexdigits = "0123456789abcdef"
	var b strings.Builder
	for _, v := range body {
		b.WriteByte(hexdigits[v>>4])
		b.WriteByte(hexdigits[v&15])
		if b.Len() >= 16 {
			return b.String()[:16]
		}
	}
	return b.String()
}

func nowString() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func WriteSchedule(path string, schedule Schedule) error {
	if path == "" {
		return fmt.Errorf("bench: schedule path is empty")
	}
	if err := schedule.Validate(); err != nil {
		return err
	}
	return withOutputLock(context.Background(), filepath.Dir(path), func() error {
		return writeScheduleUnlocked(path, schedule)
	})
}

func writeScheduleUnlocked(path string, schedule Schedule) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	body, err := json.MarshalIndent(schedule, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomicFile(path, append(body, '\n'), 0o600)
}

func LoadSchedule(path string) (Schedule, error) {
	body, err := os.ReadFile(path) //nolint:gosec // operator-selected schedule
	if err != nil {
		return Schedule{}, err
	}
	var s Schedule
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return Schedule{}, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return Schedule{}, fmt.Errorf("bench: schedule %s contains multiple JSON values", path)
		}
		return Schedule{}, err
	}
	if err := s.Validate(); err != nil {
		return Schedule{}, err
	}
	return s, nil
}

// RunDirectory is stable and human-readable while retaining the full run id to
// prevent collisions when a task is rerun.
func RunDirectory(outputRoot string, spec RunSpec) string {
	return filepath.Join(outputRoot, sanitizePath(spec.TaskID), sanitizePath(string(spec.Mode)),
		fmt.Sprintf("%04d-%s", spec.RunIndex, sanitizePath(spec.RunID)))
}

func ResultPath(outputRoot string, spec RunSpec) string {
	return filepath.Join(RunDirectory(outputRoot, spec), "result.json")
}

// PendingRuns implements resume: a run with a valid result is skipped unless
// explicitly selected by rerun. A malformed result is not silently treated as
// complete.
func PendingRuns(schedule Schedule, outputRoot string, rerun map[string]bool) ([]RunSpec, error) {
	if err := schedule.Validate(); err != nil {
		return nil, err
	}
	var pending []RunSpec
	for _, spec := range schedule.Entries {
		if rerun[spec.RunID] {
			pending = append(pending, spec)
			continue
		}
		path := ResultPath(outputRoot, spec)
		if result, err := LoadResult(path); err == nil {
			if !resultMatchesSpec(result, spec) {
				return nil, fmt.Errorf("bench: result %s does not belong to scheduled run %s", path, spec.RunID)
			}
			if result.Status.Evidence() || !result.Status.Retryable() {
				continue
			}
			// A transient infrastructure/provider/evaluator fault is durable
			// evidence, but it is not a completed cell. Resume retries it with
			// a fresh physical execution rather than silently skipping it.
		} else if _, statErr := os.Stat(path); statErr == nil {
			return nil, fmt.Errorf("bench: result %s exists but is unreadable; move it aside or rerun explicitly", path)
		} else if !os.IsNotExist(statErr) {
			return nil, statErr
		}
		pending = append(pending, spec)
	}
	return pending, nil
}

func resultMatchesSpec(result Result, spec RunSpec) bool {
	return result.SchemaVersion == ResultSchemaVersion && result.RunID == spec.RunID &&
		result.PairID == spec.PairID && result.TaskID == spec.TaskID && result.Mode == spec.Mode &&
		result.RunIndex == spec.RunIndex && result.Repetition == spec.Repetition && result.Seed == spec.Seed &&
		result.SuiteHash != "" && result.Configuration.SuiteHash == result.SuiteHash &&
		result.Task.ID == result.TaskID && result.Task.TaskHash != "" && result.Configuration.TaskHash == result.Task.TaskHash &&
		(spec.SuiteHash == "" || result.SuiteHash == spec.SuiteHash)
}

func sanitizePath(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "task"
	}
	return b.String()
}
