package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenMigratesIdempotently(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	for range 2 {
		s, err := Open(ctx, path)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		var v int
		if err := s.DB.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
			t.Fatal(err)
		}
		if v != len(migrations) {
			t.Fatalf("version = %d, want %d", v, len(migrations))
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.DB.ExecContext(ctx, `INSERT INTO repositories(id, workspace_id, name, path, created_at) VALUES('r','missing','n','/p','t')`)
	if err == nil {
		t.Fatal("expected foreign key violation")
	}
}

func TestConcurrentFirstOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	errs := make(chan error, 8)
	for range 8 {
		go func() {
			s, err := Open(context.Background(), path)
			if err == nil {
				err = s.Close()
			}
			errs <- err
		}()
	}
	for range 8 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent open: %v", err)
		}
	}
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil || n != len(migrations) {
		t.Fatalf("migrations recorded = %d (%v), want %d", n, err, len(migrations))
	}
}

func TestURIPath(t *testing.T) {
	for in, want := range map[string]string{
		"/home/me/state.db":     "/home/me/state.db",
		"/tmp/a?b#c%d/state.db": "/tmp/a%3fb%23c%25d/state.db",
	} {
		if got := uriPath(in); got != want {
			t.Errorf("uriPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestOpenOddPath: a database under a directory whose name has URI
// characters opens and persists.
func TestOpenOddPath(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a?b#c%d", "state.db")
	s, err := Open(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("database not at %s: %v", p, err)
	}
}
