// Package cli implements the command-line interface. Commands are thin: they
// parse flags, call domain packages, and render output.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/akynte/boundedcode/configs"
	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/model"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/telemetry"
)

// App carries process-wide dependencies for commands. It is constructed once
// per invocation; nothing here is global.
type App struct {
	Paths  config.Paths
	Config config.Config
	Models model.Catalog
	Out    io.Writer
	Err    io.Writer
	Log    *slog.Logger

	jsonOut bool
	st      *store.Store
}

func (a *App) configFile() string { return filepath.Join(a.Paths.Config, "config.yaml") }

func (a *App) load() error {
	p, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	a.Paths = p
	cfg, err := config.Load(a.configFile())
	if err != nil {
		return err
	}
	a.Config = cfg
	cat, err := model.LoadCatalog(configs.FS, "models", filepath.Join(p.Config, "models"))
	if err != nil {
		return err
	}
	a.Models = cat
	return nil
}

// Store opens the state database on first use.
func (a *App) Store(ctx context.Context) (*store.Store, error) {
	if a.st != nil {
		return a.st, nil
	}
	if err := a.Paths.Ensure(); err != nil {
		return nil, err
	}
	s, err := store.Open(ctx, a.Paths.StateDB())
	if err != nil {
		return nil, err
	}
	a.st = s
	return s, nil
}

// Recorder returns an audit recorder bound to the state database.
func (a *App) Recorder(ctx context.Context) (*telemetry.Recorder, error) {
	s, err := a.Store(ctx)
	if err != nil {
		return nil, err
	}
	return telemetry.New(s.DB, a.Log), nil
}

func (a *App) close() {
	if a.st != nil {
		_ = a.st.Close()
	}
}

// printJSON writes v as indented JSON.
func (a *App) printJSON(v any) error {
	enc := json.NewEncoder(a.Out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func (a *App) printf(format string, args ...any) { fmt.Fprintf(a.Out, format, args...) }

func newLogger(w io.Writer, verbose bool) *slog.Logger {
	level := slog.LevelWarn
	if verbose {
		level = slog.LevelDebug
	}
	if os.Getenv("BOUNDEDCODE_LOG") == "debug" {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}
