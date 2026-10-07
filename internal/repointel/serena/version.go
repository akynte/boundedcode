// Package serena runs Serena (github.com/oraios/serena) as an external MCP
// process and implements repointel.Navigator with it. Only Serena v1.7.0 is
// supported: it is the last MIT-licensed release; Serena's main branch
// relicensed the application to GPL-3.0-or-later for v2 (ADR-0008).
//
// Serena is used read-only. The control plane, not the agent, owns the
// processes: one instance per checkout root, started on demand, stopped when
// idle, and never exposed on a network interface (stdio transport only).
package serena

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/buildinfo"
)

// Pinned release. Changing any of these requires the license review in
// docs/licensing/policy.md; scripts/serenaguard fails CI if they change
// without the license matrix and notices changing too.
const (
	RequiredVersion = "1.7.0"
	PinnedCommit    = "949a27ef1e5fda1a6e7b561e777bcece345c6ffd"
	ExpectedLicense = "MIT"
	// LicenseSHA256 is the sha256 of LICENSE at tag v1.7.0, which is also
	// the license file shipped in the serena-agent 1.7.0 wheel.
	LicenseSHA256 = "16017e50562bb112006f6f32d17ca989e0e573b011bd6790b642018b1c195b0c"
	// PackageName is the PyPI distribution.
	PackageName = "serena-agent"
	// WheelSHA256 is the PyPI wheel serena_agent-1.7.0-py3-none-any.whl,
	// verified byte-identical to src/ at PinnedCommit. configs/serena/uv.lock
	// must pin this hash.
	WheelSHA256 = "6dbf1459670d96fb0595f84932adef34260a6fe14ba5135b901fdb3c8c76e891"
)

// Installation describes a detected Serena executable.
type Installation struct {
	Executable string `json:"executable"`
	// CLIVersion is parsed from `serena --version`, without the git suffix
	// Serena appends when its install directory is inside a git checkout.
	CLIVersion string `json:"cli_version"`
	// PackageVersion comes from the Python package metadata of the
	// environment the executable belongs to.
	PackageVersion string `json:"package_version"`
	// LicenseSHA256 is the hash of the LICENSE file installed with the package.
	LicenseSHA256 string `json:"license_sha256"`
	LicenseField  string `json:"license_field"`
}

// UnsupportedError explains why an installation cannot be used.
type UnsupportedError struct {
	Detected string
	Reason   string
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("Serena %s is not supported: %s", e.Detected, e.Reason)
}

// ErrNotInstalled means no Serena executable was found.
var ErrNotInstalled = errors.New("serena is not installed")

var cliVersionRE = regexp.MustCompile(`(?m)^Serena (\d+\.\d+\.\d+[0-9A-Za-z.+]*)`)

// ParseCLIVersion extracts the release from `serena --version` output, e.g.
// "Serena 1.7.0-dec97a4a" -> "1.7.0". Serena v1.7.0 appends "-<commit>" (and
// "-dirty") taken from whatever git repository encloses its install
// directory, so the suffix is not evidence of the Serena source revision.
func ParseCLIVersion(out string) (string, error) {
	m := cliVersionRE.FindStringSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("unrecognised `serena --version` output: %q", strings.TrimSpace(firstLine(out)))
	}
	v := m[1]
	if i := strings.IndexByte(v, '-'); i > 0 {
		v = v[:i]
	}
	return v, nil
}

// metadataProbe prints the package version, License field and the absolute
// paths of installed license files, reading only importlib metadata.
const metadataProbe = `import importlib.metadata as m, json
d = m.distribution("serena-agent")
lic = [str(f.locate()) for f in (d.files or []) if f.name in ("LICENSE", "LICENSE.txt", "LICENSE.md")]
print(json.dumps({"version": d.version, "license": d.metadata.get("License") or "", "license_files": lic}))`

// Detect inspects the executable. It does not decide support; see Check.
//
// The MCP initialize response is not used: Serena v1.7.0 creates its FastMCP
// server without a version, so serverInfo.version reports the MCP SDK
// version (1.28.1), not Serena's. We use the CLI version plus the package
// metadata of the executable's own environment, and hash the installed
// LICENSE so a v2 (GPL) install cannot pass by reporting a version string.
func Detect(ctx context.Context, executable string) (Installation, error) {
	inst := Installation{Executable: executable}
	path, err := exec.LookPath(executable)
	if err != nil {
		return inst, fmt.Errorf("%w: %s not found", ErrNotInstalled, executable)
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	inst.Executable = path
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = probeEnv()
	cmd.Dir = os.TempDir()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return inst, fmt.Errorf("%s --version: %w: %s", path, err, strings.TrimSpace(lastN(string(out), 300)))
	}
	if inst.CLIVersion, err = ParseCLIVersion(string(out)); err != nil {
		return inst, err
	}
	py := pythonFor(path)
	if py == "" {
		return inst, nil
	}
	cmd = exec.CommandContext(ctx, py, "-c", metadataProbe)
	cmd.Env = probeEnv()
	cmd.Dir = os.TempDir()
	if out, err = cmd.Output(); err != nil {
		return inst, fmt.Errorf("read %s package metadata with %s: %w", PackageName, py, err)
	}
	var md struct {
		Version      string   `json:"version"`
		License      string   `json:"license"`
		LicenseFiles []string `json:"license_files"`
	}
	if err := json.Unmarshal(out, &md); err != nil {
		return inst, fmt.Errorf("decode package metadata: %w", err)
	}
	inst.PackageVersion, inst.LicenseField = md.Version, md.License
	for _, f := range md.LicenseFiles {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		inst.LicenseSHA256 = sha256hex(b)
		break
	}
	return inst, nil
}

// Check returns nil when the installation is the pinned, MIT-licensed
// release, and an *UnsupportedError otherwise. It never suggests or performs
// an upgrade or downgrade.
func Check(inst Installation) error {
	const why = "this integration is pinned to the MIT-licensed Serena v" + RequiredVersion +
		" (Serena v2/main relicensed the application to GPL-3.0-or-later)"
	switch {
	case inst.CLIVersion != RequiredVersion:
		return &UnsupportedError{Detected: inst.CLIVersion, Reason: why}
	case inst.PackageVersion == "":
		return &UnsupportedError{Detected: inst.CLIVersion, Reason: "cannot read the " + PackageName +
			" package metadata next to " + inst.Executable + " (install it with `" + buildinfo.Command() + " serena setup`)"}
	case inst.PackageVersion != RequiredVersion:
		return &UnsupportedError{Detected: inst.PackageVersion, Reason: "package metadata disagrees with `serena --version` (" +
			inst.CLIVersion + "); " + why}
	case inst.LicenseSHA256 != LicenseSHA256:
		got := inst.LicenseSHA256
		if got == "" {
			got = "missing"
		}
		return &UnsupportedError{Detected: inst.PackageVersion, Reason: "installed LICENSE does not match the MIT license of v" +
			RequiredVersion + " (sha256 " + got + ")"}
	}
	return nil
}

// pythonFor returns the interpreter of the virtual environment that owns a
// console script (…/bin/serena -> …/bin/python), or "" if there is none. A
// symlinked script (`uv tool install` links ~/.local/bin/serena into the
// tool's environment) is followed if its own directory has no interpreter.
func pythonFor(exe string) string {
	dirs := []string{filepath.Dir(exe)}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		dirs = append(dirs, filepath.Dir(real))
	}
	for _, d := range dirs {
		for _, name := range []string{"python", "python3"} {
			p := filepath.Join(d, name)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p
			}
		}
	}
	return ""
}

func sha256hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func probeEnv() []string {
	return append(os.Environ(), "SERENA_USAGE_REPORTING=false", "PYTHONDONTWRITEBYTECODE=1")
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}

func lastN(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
