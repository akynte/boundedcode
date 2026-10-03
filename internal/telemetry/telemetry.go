// Package telemetry records structured, redacted audit events into the state
// database and mirrors them to a slog logger.
package telemetry

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/akynte/boundedcode/internal/store"
)

// Recorder writes audit events. A nil *Recorder is valid and drops events,
// which keeps call sites simple in tests.
type Recorder struct {
	db  *sql.DB
	log *slog.Logger
}

// New returns a Recorder. log may be nil.
func New(db *sql.DB, log *slog.Logger) *Recorder {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Recorder{db: db, log: log}
}

// Event is one audit record.
type Event struct {
	ID     int64           `json:"id"`
	TS     string          `json:"ts"`
	TaskID string          `json:"task_id,omitempty"`
	Kind   string          `json:"kind"`
	Data   json.RawMessage `json:"data"`
}

// Emit records an event. data is JSON-encoded and redacted. Emit never fails
// the caller's operation: errors are logged, not returned, because losing an
// audit line must not lose a task. Use EmitStrict where the record is part of
// the task's correctness.
func (r *Recorder) Emit(ctx context.Context, taskID, kind string, data any) {
	if err := r.EmitStrict(ctx, taskID, kind, data); err != nil && r != nil {
		r.log.Error("telemetry emit failed", "kind", kind, "err", err)
	}
}

// EmitStrict is Emit but returns the storage error.
func (r *Recorder) EmitStrict(ctx context.Context, taskID, kind string, data any) error {
	if r == nil {
		return nil
	}
	raw := []byte("{}")
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("encode event %s: %w", kind, err)
		}
		raw = []byte(Redact(string(b)))
	}
	r.log.LogAttrs(ctx, slog.LevelInfo, kind, slog.String("task", taskID), slog.String("data", string(raw)))
	if r.db == nil {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO events(ts, task_id, kind, data) VALUES(?,?,?,?)`,
		store.Now(), taskID, kind, string(raw))
	return err
}

// Events returns events for a task (or all events when taskID is empty) with
// id greater than after, oldest first.
func Events(ctx context.Context, db *sql.DB, taskID string, after int64, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 1000
	}
	q := `SELECT id, ts, task_id, kind, data FROM events WHERE id > ?`
	args := []any{after}
	if taskID != "" {
		q += ` AND task_id = ?`
		args = append(args, taskID)
	}
	q += ` ORDER BY id LIMIT ?`
	args = append(args, limit)
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var data string
		if err := rows.Scan(&e.ID, &e.TS, &e.TaskID, &e.Kind, &data); err != nil {
			return nil, err
		}
		e.Data = json.RawMessage(data)
		out = append(out, e)
	}
	return out, rows.Err()
}
