package bench

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
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
	for _, id := range selected {
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
			pair := pairID(s.Hash(), t.ID, repetition)
			for _, mode := range order {
				entries = append(entries, RunSpec{
					RunIndex: index, RunID: runID(s.Hash(), t.ID, repetition, mode),
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
	return Schedule{SchemaVersion: 1, SuiteID: s.ID, SuiteVersion: s.Version,
		SuiteHash: s.Hash(), Seed: seed, Runs: runs, Modes: append([]Mode(nil), modes...),
		Entries: entries, Concurrency: 1, Warnings: warnings, GeneratedAt: nowString()}, nil
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

func pairID(suiteHash, task string, repetition int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("pair-v1:%s:%s:%d", suiteHash, task, repetition)))
	return "pair-" + hexShort(h[:])
}

func runID(suiteHash, task string, repetition int, mode Mode) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("run-v1:%s:%s:%d:%s", suiteHash, task, repetition, mode)))
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
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	body, err := json.MarshalIndent(schedule, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(body, '\n'), 0o600) //nolint:gosec // published schedule artifact
}

func LoadSchedule(path string) (Schedule, error) {
	body, err := os.ReadFile(path) //nolint:gosec // operator-selected schedule
	if err != nil {
		return Schedule{}, err
	}
	var s Schedule
	if err := json.Unmarshal(body, &s); err != nil {
		return Schedule{}, err
	}
	if s.SchemaVersion != 1 {
		return Schedule{}, fmt.Errorf("bench: unsupported schedule schema %d", s.SchemaVersion)
	}
	return s, nil
}

// RunDirectory is stable and human-readable while retaining the full run id to
// prevent collisions when a task is rerun.
func RunDirectory(outputRoot string, spec RunSpec) string {
	return filepath.Join(outputRoot, sanitizePath(spec.TaskID), string(spec.Mode),
		fmt.Sprintf("%04d-%s", spec.RunIndex, spec.RunID))
}

func ResultPath(outputRoot string, spec RunSpec) string {
	return filepath.Join(RunDirectory(outputRoot, spec), "result.json")
}

// PendingRuns implements resume: a run with a valid result is skipped unless
// explicitly selected by rerun. A malformed result is not silently treated as
// complete.
func PendingRuns(schedule Schedule, outputRoot string, rerun map[string]bool) ([]RunSpec, error) {
	var pending []RunSpec
	for _, spec := range schedule.Entries {
		if rerun[spec.RunID] {
			pending = append(pending, spec)
			continue
		}
		path := ResultPath(outputRoot, spec)
		if ResultExists(path) {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return nil, fmt.Errorf("bench: result %s exists but is unreadable; move it aside or rerun explicitly", path)
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		pending = append(pending, spec)
	}
	return pending, nil
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
