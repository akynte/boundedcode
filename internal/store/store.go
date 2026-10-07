// Package store owns the embedded SQLite database that holds the control
// plane's canonical state. Domain packages (task, workspace, ...) issue their
// own queries against *sql.DB; this package only opens, configures and
// migrates the database.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no cgo)
)

// Store wraps the database handle.
type Store struct {
	DB   *sql.DB
	path string
}

// Open opens (creating if needed) the database at path and applies pending
// migrations. Use ":memory:" for tests.
func Open(ctx context.Context, path string) (*Store, error) {
	dsn := path
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
		dsn = "file:" + uriPath(path)
	}
	// WAL + busy timeout let a CLI and a long-running task process share the
	// DB. Immediate transactions take the write lock up front, so concurrent
	// writers queue on busy_timeout instead of failing on lock upgrade.
	dsn += "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if path == ":memory:" {
		// Every new connection to :memory: is a fresh database.
		db.SetMaxOpenConns(1)
	}
	s := &Store{DB: db, path: path}
	if err := s.migrateRetry(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.DB.Close() }

// Path returns the database file path.
func (s *Store) Path() string { return s.path }

// migrateRetry retries migrate while the database is busy. SQLite does not
// call the busy handler while another connection converts a fresh file to
// WAL or recovers its WAL, so concurrent first opens can see SQLITE_BUSY
// even with busy_timeout; this only happens around the first open.
func (s *Store) migrateRetry(ctx context.Context) error {
	deadline := time.Now().Add(15 * time.Second)
	for wait := 5 * time.Millisecond; ; wait = min(wait*2, 250*time.Millisecond) {
		err := s.migrate(ctx)
		if err == nil || !strings.Contains(err.Error(), "SQLITE_BUSY") || time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

func (s *Store) migrate(ctx context.Context) error {
	// One immediate transaction takes the write lock before reading the
	// schema version. A read that later upgrades to a write gets SQLITE_BUSY
	// without waiting when another process commits in between (two CLIs
	// opening a fresh database), so the whole migration holds the lock.
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	var current int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if current > len(migrations) {
		return fmt.Errorf("database schema version %d is newer than this binary supports (%d)", current, len(migrations))
	}
	for i := current; i < len(migrations); i++ {
		version := i + 1
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			return fmt.Errorf("apply migration %d: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`,
			version, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("record migration %d: %w", version, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}

// ErrNotFound is returned by domain lookups that find no row.
var ErrNotFound = errors.New("not found")

// Now returns the canonical timestamp format used in the database.
func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// uriPath is a file path as the path of an SQLite URI filename: forward
// slashes, a leading slash before a Windows drive (file:/C:/...), and
// %, ? and # escaped so they are not read as URI syntax.
func uriPath(p string) string {
	p = filepath.ToSlash(p)
	p = strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(p)
	if len(p) >= 2 && p[1] == ':' {
		p = "/" + p
	}
	return p
}
