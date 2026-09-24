package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewLayoutUsesTheEnvironmentBeforeThePlatformDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("BC_IN_CONTAINER", "")
	data := filepath.Join(home, "data")
	t.Setenv(EnvDataDir, data)
	layout, err := NewLayout("")
	if err != nil {
		t.Fatal(err)
	}
	if layout.Root() != data {
		t.Fatalf("layout root = %q, want %q", layout.Root(), data)
	}
}

func TestExplicitLayoutWinsOverEnvironment(t *testing.T) {
	t.Setenv(EnvDataDir, filepath.Join(t.TempDir(), "environment"))
	explicit := filepath.Join(t.TempDir(), "explicit")
	layout, err := NewLayout(explicit)
	if err != nil {
		t.Fatal(err)
	}
	if layout.Root() != explicit {
		t.Fatalf("layout root = %q, want %q", layout.Root(), explicit)
	}
}

func TestPlatformDefaultIsUsableWithoutAnEnvironmentOverride(t *testing.T) {
	t.Setenv(EnvDataDir, "")
	t.Setenv("BC_IN_CONTAINER", "")
	root := DefaultDataDirPath()
	if root == "" {
		t.Fatal("platform default data directory is empty")
	}
	if _, err := os.Stat(root); err == nil {
		// It is fine if a developer already has the conventional directory;
		// the important property is that setup and the CLI resolve the same
		// non-empty path.
		t.Logf("default data directory already exists: %s", root)
	}
}
