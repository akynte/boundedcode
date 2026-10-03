package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/akynte/boundedcode/internal/buildinfo"
)

// Paths are the filesystem locations the tool uses. They follow the XDG base
// directory specification and can be overridden with BOUNDEDCODE_HOME, which
// places everything under one directory (useful for tests and portable use).
type Paths struct {
	Config string // user configuration (models.yaml, policy.yaml)
	Data   string // state.db, task directories
	Cache  string // derived, rebuildable data
	State  string // logs, pid files
	// Runtime holds machine-wide supervisor state (the inference server owns
	// the GPU, which is shared by every data home). It ignores
	// BOUNDEDCODE_HOME so isolated homes see the same running server.
	Runtime string
}

// DefaultPaths resolves Paths from the environment.
func DefaultPaths() (Paths, error) {
	userHome, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve home directory: %w", err)
	}
	runtime := filepath.Join(userHome, ".local", "state", buildinfo.DataDirName, "runtime")
	if v := os.Getenv("XDG_STATE_HOME"); v != "" && filepath.IsAbs(v) {
		runtime = filepath.Join(v, buildinfo.DataDirName, "runtime")
	}
	if home := os.Getenv(buildinfo.EnvPrefix + "HOME"); home != "" {
		abs, err := filepath.Abs(home)
		if err != nil {
			return Paths{}, fmt.Errorf("resolve %sHOME: %w", buildinfo.EnvPrefix, err)
		}
		return Paths{
			Runtime: runtime,
			Config:  filepath.Join(abs, "config"),
			Data:    filepath.Join(abs, "data"),
			Cache:   filepath.Join(abs, "cache"),
			State:   filepath.Join(abs, "state"),
		}, nil
	}
	xdg := func(env, fallback string) string {
		if v := os.Getenv(env); v != "" && filepath.IsAbs(v) {
			return filepath.Join(v, buildinfo.DataDirName)
		}
		return filepath.Join(userHome, fallback, buildinfo.DataDirName)
	}
	return Paths{
		Runtime: runtime,
		Config:  xdg("XDG_CONFIG_HOME", ".config"),
		Data:    xdg("XDG_DATA_HOME", ".local/share"),
		Cache:   xdg("XDG_CACHE_HOME", ".cache"),
		State:   xdg("XDG_STATE_HOME", ".local/state"),
	}, nil
}

// Ensure creates all directories with owner-only permissions.
func (p Paths) Ensure() error {
	for _, d := range []string{p.Config, p.Data, p.Cache, p.State, p.Runtime} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}
	return nil
}

// StateDB is the path of the canonical SQLite database.
func (p Paths) StateDB() string { return filepath.Join(p.Data, "state.db") }

// TaskDir is the per-task directory for runtime persistence and artifacts.
func (p Paths) TaskDir(taskID string) string { return filepath.Join(p.Data, "tasks", taskID) }
