// Package workspace manages workspaces: named groups of repositories that
// the control plane reasons about together.
package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/ids"
	"github.com/akynte/boundedcode/internal/store"
)

// Workspace is a named set of repositories.
type Workspace struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

// Repository is a member repository.
type Repository struct {
	ID            string   `json:"id"`
	WorkspaceID   string   `json:"workspace_id"`
	Name          string   `json:"name"`
	Path          string   `json:"path"`
	Origin        string   `json:"origin"`
	DefaultBranch string   `json:"default_branch"`
	Languages     []string `json:"languages"`
	IndexProject  string   `json:"index_project"`
	IndexedAt     string   `json:"indexed_at"`
	// Enabled repositories take part in new tasks, indexing and queries.
	// A disabled one keeps its id and metadata, and tasks that already have
	// a worktree in it keep working.
	Enabled bool `json:"enabled"`
}

// ErrInUse means a repository cannot be removed because task worktrees
// reference it; disable it instead.
var ErrInUse = errors.New("repository is referenced by tasks")

// Store persists workspaces.
type Store struct{ DB *sql.DB }

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// Create makes a workspace.
func (s Store) Create(ctx context.Context, name string) (Workspace, error) {
	if !nameRE.MatchString(name) {
		return Workspace{}, fmt.Errorf("invalid workspace name %q (lowercase letters, digits, . _ -)", name)
	}
	w := Workspace{ID: ids.New("w"), Name: name, CreatedAt: store.Now()}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO workspaces(id, name, created_at, updated_at) VALUES(?,?,?,?)`, w.ID, w.Name, w.CreatedAt, w.CreatedAt)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return w, fmt.Errorf("workspace %q already exists", name)
	}
	return w, err
}

// Get finds a workspace by name or id.
func (s Store) Get(ctx context.Context, nameOrID string) (Workspace, error) {
	var w Workspace
	err := s.DB.QueryRowContext(ctx, `SELECT id, name, created_at FROM workspaces WHERE name = ? OR id = ?`, nameOrID, nameOrID).
		Scan(&w.ID, &w.Name, &w.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return w, fmt.Errorf("workspace %q: %w", nameOrID, store.ErrNotFound)
	}
	return w, err
}

// List returns all workspaces.
func (s Store) List(ctx context.Context) ([]Workspace, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, name, created_at FROM workspaces ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Workspace
	for rows.Next() {
		var w Workspace
		if err := rows.Scan(&w.ID, &w.Name, &w.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// AddRepo registers the git repository containing path.
func (s Store) AddRepo(ctx context.Context, w Workspace, path, name string) (Repository, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Repository{}, err
	}
	info, err := gitops.Inspect(ctx, abs)
	if err != nil {
		return Repository{}, err
	}
	if name == "" {
		name = filepath.Base(info.Root)
	}
	r := Repository{ID: ids.New("r"), WorkspaceID: w.ID, Name: name, Path: info.Root, Origin: info.Origin,
		DefaultBranch: info.DefaultBranch, Languages: DetectLanguages(info.Root), Enabled: true}
	langs, _ := json.Marshal(r.Languages)
	_, err = s.DB.ExecContext(ctx, `INSERT INTO repositories(id, workspace_id, name, path, origin, default_branch, languages, created_at)
		VALUES(?,?,?,?,?,?,?,?)`, r.ID, r.WorkspaceID, r.Name, r.Path, r.Origin, r.DefaultBranch, string(langs), store.Now())
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return r, fmt.Errorf("repository %s (%s) is already in workspace %s", name, info.Root, w.Name)
	}
	return r, err
}

// Repos lists the enabled repositories of a workspace: the ones new tasks,
// indexing and queries use.
func (s Store) Repos(ctx context.Context, workspaceID string) ([]Repository, error) {
	return s.repos(ctx, workspaceID, true)
}

// AllRepos lists every repository of a workspace, disabled ones included.
func (s Store) AllRepos(ctx context.Context, workspaceID string) ([]Repository, error) {
	return s.repos(ctx, workspaceID, false)
}

func (s Store) repos(ctx context.Context, workspaceID string, enabledOnly bool) ([]Repository, error) {
	q := `SELECT id, workspace_id, name, path, origin, default_branch, languages, index_project, indexed_at, enabled
		FROM repositories WHERE workspace_id = ?`
	if enabledOnly {
		q += ` AND enabled = 1`
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY name`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Repository
	for rows.Next() {
		var r Repository
		var langs string
		if err := rows.Scan(&r.ID, &r.WorkspaceID, &r.Name, &r.Path, &r.Origin, &r.DefaultBranch, &langs, &r.IndexProject, &r.IndexedAt, &r.Enabled); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(langs), &r.Languages)
		out = append(out, r)
	}
	return out, rows.Err()
}

// Repo finds one repository of a workspace by name or id, enabled or not.
func (s Store) Repo(ctx context.Context, workspaceID, nameOrID string) (Repository, error) {
	repos, err := s.AllRepos(ctx, workspaceID)
	if err != nil {
		return Repository{}, err
	}
	for _, r := range repos {
		if r.Name == nameOrID || r.ID == nameOrID {
			return r, nil
		}
	}
	return Repository{}, fmt.Errorf("repository %q: %w", nameOrID, store.ErrNotFound)
}

// SetEnabled enables or disables a repository. Disabling is the reversible
// way to take a repository out of a workspace: its id, metadata, index and
// task history stay.
func (s Store) SetEnabled(ctx context.Context, repoID string, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE repositories SET enabled = ? WHERE id = ?`, v, repoID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("repository %q: %w", repoID, store.ErrNotFound)
	}
	return nil
}

// RemoveRepo deletes a repository and its cross-service endpoints. It
// refuses while any task, finished or not, has a worktree in it: task
// history references the repository row, so such a repository can only be
// disabled.
func (s Store) RemoveRepo(ctx context.Context, repoID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var total, live int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(t.status IN ('active','blocked')), 0)
		FROM task_worktrees w JOIN tasks t ON t.id = w.task_id WHERE w.repository_id = ?`, repoID).Scan(&total, &live); err != nil {
		return err
	}
	if total > 0 {
		return fmt.Errorf("%w: %d task(s), %d still active or blocked; disable the repository instead", ErrInUse, total, live)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM xservice_endpoints WHERE repository_id = ?`, repoID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM repositories WHERE id = ?`, repoID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("repository %q: %w", repoID, store.ErrNotFound)
	}
	return tx.Commit()
}

// MarkIndexed records the repository-intelligence project handle.
func (s Store) MarkIndexed(ctx context.Context, repoID, project string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE repositories SET index_project = ?, indexed_at = ? WHERE id = ?`, project, store.Now(), repoID)
	return err
}
