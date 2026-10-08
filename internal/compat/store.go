package compat

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/verify"
)

func beginEvaluation(ctx context.Context, db *sql.DB, taskID string) (int64, error) {
	r, err := db.ExecContext(ctx, `INSERT INTO compat_evaluations(task_id, started_at) VALUES(?, ?)`, taskID, store.Now())
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

func finishEvaluation(ctx context.Context, db *sql.DB, id int64, rep Report) error {
	un, _ := json.Marshal(rep.Unaffected)
	_, err := db.ExecContext(ctx, `UPDATE compat_evaluations SET state = ?, error = ?, unaffected = ?, finished_at = ? WHERE id = ?`,
		rep.State(), rep.Error, string(un), store.Now(), id)
	return err
}

func saveResult(ctx context.Context, db *sql.DB, taskID string, evalID int64, l LinkResult) error {
	l.Stale = false
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO compat_results(evaluation_id, task_id, link_id, commits, result, record, created_at) VALUES(?,?,?,?,?,?,?)`,
		evalID, taskID, l.ID, commitsKey(l.Commits), string(l.Result), string(b), store.Now())
	return err
}

// lookup returns the latest result recorded for a link at exactly these
// commits (from any evaluation, including an interrupted one).
func lookup(ctx context.Context, db *sql.DB, taskID, linkID, commits string) (LinkResult, bool) {
	var rec string
	err := db.QueryRowContext(ctx, `SELECT record FROM compat_results WHERE task_id = ? AND link_id = ? AND commits = ? ORDER BY id DESC LIMIT 1`,
		taskID, linkID, commits).Scan(&rec)
	if err != nil {
		return LinkResult{}, false
	}
	var l LinkResult
	if json.Unmarshal([]byte(rec), &l) != nil || l.Result == "" {
		return LinkResult{}, false
	}
	return l, true
}

// runRecord is a recorded composed run (see evaluation.run).
type runRecord struct {
	Result verify.StageResult  `json:"result"`
	Output string              `json:"output"`
	Blocks []verify.CoverBlock `json:"blocks,omitempty"`
}

// Runs are stored beside link results, under a "run:" key (the key holds
// the composition's commits, so a moved commit never matches).
func saveRun(ctx context.Context, db *sql.DB, taskID, key string, rec runRecord) error {
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO compat_results(evaluation_id, task_id, link_id, commits, result, record, created_at) VALUES(NULL,?,?,?,?,?,?)`,
		taskID, runID(key), key, rec.Result.Status, string(b), store.Now())
	return err
}

func lookupRun(ctx context.Context, db *sql.DB, taskID, key string) (runRecord, bool) {
	var rec string
	if err := db.QueryRowContext(ctx, `SELECT record FROM compat_results WHERE task_id = ? AND link_id = ? AND commits = ? ORDER BY id DESC LIMIT 1`,
		taskID, runID(key), key).Scan(&rec); err != nil {
		return runRecord{}, false
	}
	var r runRecord
	return r, json.Unmarshal([]byte(rec), &r) == nil
}

func runID(key string) string {
	h := sha256.Sum256([]byte(key))
	return "run:" + hex.EncodeToString(h[:12])
}

// Load returns the task's latest evaluation. current maps each task
// repository to its current "base..head" commits; a result recorded for
// other commits is marked stale, and so is the report (Incomplete reports an
// evaluation that did not finish).
func Load(ctx context.Context, db *sql.DB, taskID string, current map[string]string) (Report, bool, error) {
	var (
		id                    int64
		state, errS, un, done string
	)
	err := db.QueryRowContext(ctx, `SELECT id, state, error, unaffected, finished_at FROM compat_evaluations WHERE task_id = ? ORDER BY id DESC LIMIT 1`,
		taskID).Scan(&id, &state, &errS, &un, &done)
	if errors.Is(err, sql.ErrNoRows) {
		return Report{}, false, nil
	}
	if err != nil {
		return Report{}, false, err
	}
	rep := Report{Error: errS, Incomplete: done == ""}
	_ = json.Unmarshal([]byte(un), &rep.Unaffected)
	rows, err := db.QueryContext(ctx, `SELECT record FROM compat_results WHERE evaluation_id = ? ORDER BY id`, id)
	if err != nil {
		return rep, true, err
	}
	defer rows.Close()
	for rows.Next() {
		var rec string
		if err := rows.Scan(&rec); err != nil {
			return rep, true, err
		}
		var l LinkResult
		if json.Unmarshal([]byte(rec), &l) != nil {
			continue
		}
		for repo, c := range l.Commits {
			if cur, ok := current[repo]; ok && cur != c {
				l.Stale = true
			}
		}
		rep.Links = append(rep.Links, l)
	}
	sortLinks(rep.Links)
	return rep, true, rows.Err()
}

// Stale reports whether any result was recorded for commits that have moved
// since, or the evaluation did not finish.
func (r Report) Stale() bool {
	if r.Incomplete {
		return true
	}
	for _, l := range r.Links {
		if l.Stale {
			return true
		}
	}
	return false
}

// CommitRange formats a repository's commits as results record them.
func CommitRange(base, head string) string {
	return strings.TrimSpace(base) + ".." + strings.TrimSpace(head)
}
