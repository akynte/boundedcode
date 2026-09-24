package supervisor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/store"
)

// TaskBoundToSession reports whether the current OpenCode session has an
// explicit binding for the task. A task ID alone is not authority: otherwise a
// model that knows another task's ID could edit or finish it.
func TaskBoundToSession(ctx context.Context, st *store.Store, taskID, sessionID string) (bool, error) {
	if taskID == "" || !strings.HasPrefix(sessionID, "ses_") || len(sessionID) > 100 {
		return false, nil
	}
	var bound string
	err := st.Ledger().SQL().QueryRowContext(ctx, `
		SELECT COALESCE(json_extract(intent, '$.session_id'), '')
		FROM operations
		WHERE task_id=? AND kind='session_start' AND json_valid(intent)
		ORDER BY id DESC LIMIT 1`, taskID).Scan(&bound)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return bound == sessionID, nil
}

// DecisionPage retrieves a bounded slice without materializing a long task's
// entire journal on every OpenCode request. offset is oldest first.
func DecisionPage(ctx context.Context, st *store.Store, taskID string, offset, limit int) ([]Decision, int, error) {
	if offset < 0 || limit < 0 || limit > 100 {
		return nil, 0, errors.New("invalid decision page")
	}
	db := st.Ledger().SQL()
	const filter = `task_id = ? AND kind = 'decision' AND json_valid(intent) AND json_extract(intent, '$.source') = 'user'`
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM operations WHERE `+filter, taskID).Scan(&count); err != nil {
		return nil, 0, err
	}
	rows, err := db.QueryContext(ctx, `SELECT intent, started_at FROM operations WHERE `+filter+` ORDER BY seq LIMIT ? OFFSET ?`, taskID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Decision
	for rows.Next() {
		var raw string
		var at int64
		if err := rows.Scan(&raw, &at); err != nil {
			return nil, 0, err
		}
		var d Decision
		if err := json.Unmarshal([]byte(raw), &d); err != nil {
			return nil, 0, err
		}
		d.At = time.UnixMilli(at).UTC()
		out = append(out, d)
	}
	return out, count, rows.Err()
}

// CurrentPhase returns the phase that is authoritative for reconstruction. The
// native workflow stores it in task_workflow; OpenCode editor tasks instead
// record EDITOR on their session-start operation, because the editor owns the
// middle of the lifecycle and is not allowed to invent a native phase name.
// LatestRecordedCandidate returns the most recent candidate identity written
// by a controlled edit or verification. It is a ledger value, not a fresh
// repository hash computed on every model request; a checkout may have changed
// since that operation, which is why the context labels it as recorded history.
func LatestRecordedCandidate(ctx context.Context, st *store.Store, taskID string) (string, error) {
	var candidate string
	err := st.Ledger().SQL().QueryRowContext(ctx, `
		SELECT COALESCE(json_extract(outcome, '$.candidate'), '')
		FROM operations
		WHERE task_id = ? AND kind IN ('edit', 'recipe_run') AND outcome IS NOT NULL
		ORDER BY id DESC LIMIT 1`, taskID).Scan(&candidate)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return candidate, err
}

func CurrentPhase(ctx context.Context, st *store.Store, taskID string) (string, error) {
	var phase string
	err := st.Ledger().SQL().QueryRowContext(ctx,
		`SELECT phase FROM task_workflow WHERE task_id = ?`, taskID).Scan(&phase)
	if err == nil && phase != "" {
		return phase, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	err = st.Ledger().SQL().QueryRowContext(ctx, `
		SELECT COALESCE(json_extract(intent, '$.phase'), 'EDITOR')
		FROM operations
		WHERE task_id = ? AND kind = 'session_start' AND json_valid(intent)
		ORDER BY seq DESC LIMIT 1`, taskID).Scan(&phase)
	if err == sql.ErrNoRows || phase == "" {
		return "EDITOR", nil
	}
	return phase, err
}

// VerificationPage retrieves historical verification records without making
// the active context card replay every run. The latest record is still shown
// directly; older records remain available through this bounded page.
func VerificationPage(ctx context.Context, st *store.Store, taskID string, before int64, limit int) ([]VerificationRecord, int, error) {
	if before < 0 || limit < 1 || limit > 100 {
		return nil, 0, errors.New("invalid verification page")
	}
	if before == 0 {
		before = 1<<63 - 1
	}
	var count int
	if err := st.Ledger().SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM operations WHERE task_id=? AND kind='recipe_run' AND outcome IS NOT NULL`, taskID).Scan(&count); err != nil {
		return nil, 0, err
	}
	rows, err := st.Ledger().SQL().QueryContext(ctx, `
		SELECT id,intent FROM operations
		WHERE task_id=? AND kind='recipe_run' AND outcome IS NOT NULL AND id<?
		ORDER BY id DESC LIMIT ?`, taskID, before, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]VerificationRecord, 0, limit)
	for rows.Next() {
		var id int64
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, 0, err
		}
		var rec VerificationRecord
		if err := json.Unmarshal([]byte(raw), &rec); err != nil {
			return nil, 0, err
		}
		rec.ID = id
		out = append(out, rec)
	}
	return out, count, rows.Err()
}

func latestVerification(ctx context.Context, st *store.Store, taskID string) (VerificationRecord, bool, error) {
	var raw string
	err := st.Ledger().SQL().QueryRowContext(ctx, `SELECT intent FROM operations
		WHERE task_id = ? AND kind = ? ORDER BY seq DESC LIMIT 1`, taskID, ledger.KindRecipeRun).Scan(&raw)
	if err == sql.ErrNoRows {
		return VerificationRecord{}, false, nil
	}
	if err != nil {
		return VerificationRecord{}, false, err
	}
	var rec VerificationRecord
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		return VerificationRecord{}, false, err
	}
	return rec, true, nil
}
