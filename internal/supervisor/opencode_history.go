package supervisor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/store"
)

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
