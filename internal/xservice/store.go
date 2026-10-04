package xservice

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SaveRepo atomically replaces a repository's endpoints.
func SaveRepo(ctx context.Context, db *sql.DB, workspaceID, repositoryID string, eps []Endpoint) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM xservice_endpoints WHERE repository_id = ?`, repositoryID); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO xservice_endpoints(workspace_id, repository_id, repo, kind, file, line, symbol,
		method, path, topic, env, confidence, detail, scanned_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, e := range eps {
		if _, err := stmt.ExecContext(ctx, workspaceID, repositoryID, e.Repo, string(e.Kind), e.File, e.Line, e.Symbol,
			e.Method, e.Path, e.Topic, e.Env, string(e.Confidence), e.Detail, now); err != nil {
			return fmt.Errorf("save endpoint %s: %w", e.Where(), err)
		}
	}
	return tx.Commit()
}

// LoadWorkspace returns all endpoints of a workspace, sorted.
func LoadWorkspace(ctx context.Context, db *sql.DB, workspaceID string) ([]Endpoint, error) {
	rows, err := db.QueryContext(ctx, `SELECT repo, kind, file, line, symbol, method, path, topic, env, confidence, detail
		FROM xservice_endpoints WHERE workspace_id = ?`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Endpoint
	for rows.Next() {
		var e Endpoint
		var kind, conf string
		if err := rows.Scan(&e.Repo, &kind, &e.File, &e.Line, &e.Symbol, &e.Method, &e.Path, &e.Topic, &e.Env, &conf, &e.Detail); err != nil {
			return nil, err
		}
		e.Kind, e.Confidence = Kind(kind), Confidence(conf)
		out = append(out, e)
	}
	SortEndpoints(out)
	return out, rows.Err()
}

// Touching returns links where either endpoint lies in one of the given
// repo-qualified files ("repo/path/to/file.go").
func Touching(links []Link, repoFiles []string) []Link {
	set := map[string]bool{}
	for _, f := range repoFiles {
		set[f] = true
	}
	var out []Link
	for _, l := range links {
		if set[l.From.Repo+"/"+l.From.File] || set[l.To.Repo+"/"+l.To.File] {
			out = append(out, l)
		}
	}
	return out
}
