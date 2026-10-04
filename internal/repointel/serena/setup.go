package serena

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/repointel"
)

// Prerequisites lists the external language servers Serena v1.7.0 expects
// per language and how we check them. Serena downloads the TypeScript server
// itself (with npm) into the shared language_servers directory; that happens
// only in InstallLanguageServers, never during tasks.
var Prerequisites = map[string][]string{
	"go":         {"go", "gopls"},
	"typescript": {"node", "npm"},
	"python":     {}, // Serena ships pyright as a Python dependency
	"rust":       {"rust-analyzer"},
}

// probeFiles are minimal sources that make Serena start a language server.
var probeFiles = map[string][2]string{
	"go":         {"probe.go", "package probe\n\n// Probe exists so the language server has a symbol.\nfunc Probe() {}\n"},
	"typescript": {"probe.ts", "export function probe(): number {\n  return 1;\n}\n"},
	"python":     {"probe.py", "def probe():\n    return 1\n"},
	"rust":       {"src/lib.rs", "pub fn probe() -> i32 { 1 }\n"},
}

// MissingPrerequisites returns the executables a language needs but lacks.
func MissingPrerequisites(lang string) []string {
	var out []string
	for _, bin := range Prerequisites[lang] {
		if _, err := exec.LookPath(bin); err != nil {
			out = append(out, bin)
		}
	}
	return out
}

// InstallLanguageServers starts a throwaway instance per language on a tiny
// probe project so Serena installs (TypeScript) or validates (Go, …) the
// language server. Network access is allowed only here.
func InstallLanguageServers(ctx context.Context, m *Manager, langs []string) error {
	var errs []error
	for _, lang := range langs {
		if err := ProbeLanguage(ctx, m, lang, true); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", lang, err))
		}
	}
	return errors.Join(errs...)
}

// ProbeLanguage checks that Serena can start the language server for lang
// and answer a symbol query. allowDownload permits npm installs.
func ProbeLanguage(ctx context.Context, m *Manager, lang string, allowDownload bool) error {
	pf, ok := probeFiles[lang]
	if !ok {
		return fmt.Errorf("no probe for %q", lang)
	}
	if miss := MissingPrerequisites(lang); len(miss) > 0 {
		return fmt.Errorf("missing %s on PATH", strings.Join(miss, ", "))
	}
	if err := m.Verify(ctx); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "bc-serena-probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, pf[0])
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(pf[1]), 0o600); err != nil {
		return err
	}
	switch lang {
	case "go":
		_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module probe\n\ngo 1.22\n"), 0o600)
	case "rust":
		_ = os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"probe\"\nversion = \"0.1.0\"\nedition = \"2021\"\n"), 0o600)
	}
	probe := &Manager{Executable: m.Executable, Root: m.Root, LanguageServers: m.LanguageServers, LogDir: m.LogDir, MaxInstances: 1,
		StartupTimeout: max(m.StartupTimeout, 3*time.Minute), CallTimeout: 3 * time.Minute, Log: m.Log,
		SkipVersionCheck: true, ExtraEnv: m.ExtraEnv}
	if allowDownload {
		probe.ExtraEnv = append(append([]string{}, m.ExtraEnv...), "npm_config_offline=false")
	}
	defer func() { _ = probe.Close(); _ = removeHome(m.Root, canonical(dir)) }()
	nav := &Navigator{M: probe}
	nav.langs = map[string][]string{canonical(dir): {lang}}
	syms, err := nav.FindSymbol(ctx, dir, "probe", repointel.FindOptions{})
	if err != nil {
		return err
	}
	if len(syms) == 0 {
		return fmt.Errorf("language server answered but found no symbol (see %s)", probe.LogDir)
	}
	return nil
}
