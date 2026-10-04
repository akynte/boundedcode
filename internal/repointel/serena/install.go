package serena

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	"github.com/akynte/boundedcode/internal/config"
)

// InstallDir is where `serena setup` installs the pinned environment. The
// version is part of the path, so a different release can never be picked up
// from it by accident.
func InstallDir(dataDir string) string {
	return filepath.Join(dataDir, "tools", "serena-"+RequiredVersion)
}

// DefaultExecutable is the serena console script of InstallDir.
func DefaultExecutable(dataDir string) string {
	return filepath.Join(InstallDir(dataDir), ".venv", "bin", "serena")
}

// LanguageServersDir holds the language servers Serena downloads itself.
func LanguageServersDir(dataDir string) string {
	return filepath.Join(InstallDir(dataDir), "language_servers")
}

// Install writes the locked project (pyproject.toml, uv.lock from env, under
// the "serena/" prefix) into dir and installs it with `uv sync --frozen`:
// exact versions and hashes from the lock, no resolution, no upgrades.
func Install(ctx context.Context, uv, dir string, env fs.FS, out io.Writer) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, name := range []string{"pyproject.toml", "uv.lock"} {
		b, err := fs.ReadFile(env, path.Join("serena", name))
		if err != nil {
			return err
		}
		if err := config.WriteFileAtomic(filepath.Join(dir, name), b, 0o600); err != nil {
			return err
		}
	}
	cmd := exec.CommandContext(ctx, uv, "sync", "--frozen", "--no-dev", "--project", dir)
	cmd.Env = append(os.Environ(), "UV_PROJECT_ENVIRONMENT="+filepath.Join(dir, ".venv"))
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("uv sync --frozen: %w", err)
	}
	return nil
}
