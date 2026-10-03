package store

import (
	"context"
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
