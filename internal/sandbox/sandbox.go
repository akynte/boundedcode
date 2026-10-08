// Package sandbox runs commands in an isolated environment. The container
// implementation is the security boundary for agent tool execution
// (ADR-0003); agent-level permission prompts are not.
package sandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/akynte/boundedcode/internal/pathutil"
)

// scratchSize bounds each scratch tmpfs; it counts against the container's
// memory limit.
const scratchSize = 512 << 20

// Mount binds a host path into the sandbox.
type Mount struct {
	Host     string
	Target   string
	ReadOnly bool
}

// Spec describes one command execution.
type Spec struct {
	Argv    []string
	Workdir string // path inside the sandbox
	Mounts  []Mount
	// Scratch are writable, per-run tmpfs directories layered over mounts
	// (tool caches inside read-only dependency directories). They need an
	// existing directory at the path and are ignored without isolation.
	Scratch     []string
	Env         map[string]string
	Interactive bool // keep stdin open (needed for the stdio protocol)
	// Masks are paths inside the sandbox hidden behind empty read-only
	// mounts (secret files and directories inside mounted trees).
	Masks []string
	// Name labels the container (sanitized). The container commands give an
	// unnamed spec a unique bc-v-<random> name so it can be stopped on cancel.
	Name string
}

// Sandbox builds commands that run inside an isolation boundary.
type Sandbox interface {
	Name() string
	// Isolated reports whether this sandbox is a real boundary.
	Isolated() bool
	Command(ctx context.Context, spec Spec) (*exec.Cmd, error)
}

// None runs commands directly on the host. It exists for development and
// tests; autonomous tasks refuse it unless explicitly overridden.
type None struct{}

// Name implements Sandbox.
func (None) Name() string { return "none" }

// Isolated implements Sandbox.
func (None) Isolated() bool { return false }

// Command implements Sandbox. Mount targets must equal host paths, because
// there is no path translation on the host.
func (None) Command(ctx context.Context, s Spec) (*exec.Cmd, error) {
	if len(s.Argv) == 0 {
		return nil, fmt.Errorf("sandbox: empty argv")
	}
	for _, m := range s.Mounts {
		if filepath.Clean(m.Host) != filepath.Clean(m.Target) {
			return nil, fmt.Errorf("sandbox none: mount %s -> %s needs path translation; use matching paths", m.Host, m.Target)
		}
	}
	cmd := exec.CommandContext(ctx, s.Argv[0], s.Argv[1:]...)
	cmd.Dir = s.Workdir
	cmd.Env = append(scrubbedEnv(), envList(s.Env)...)
	return cmd, nil
}

// Container runs commands in a Docker-compatible engine.
type Container struct {
	Engine  string // docker | podman
	Image   string
	Network string // none | bridge
	Memory  string
	CPUs    string
	PIDs    int
	// UID/GID run the container as the invoking user so files written to the
	// worktree keep correct ownership.
	UID, GID int

	// MaxCPUs and MaxMemBytes are the engine's own resources (Docker
	// Desktop and Podman machine run containers in a VM that is often
	// smaller than the host). Command fills them once; Args caps CPUs and
	// Memory to them, because the engine refuses a container that asks for
	// more. 0 = unknown, no cap.
	MaxCPUs     int
	MaxMemBytes int64
	probeOnce   sync.Once
}

// Name implements Sandbox.
func (c *Container) Name() string { return c.Engine }

// Isolated implements Sandbox.
func (c *Container) Isolated() bool { return true }

// Command implements Sandbox. Cancelling ctx removes the container, not just
// the engine CLI: killing `docker run` alone leaves the container running.
func (c *Container) Command(ctx context.Context, s Spec) (*exec.Cmd, error) {
	if s.Name == "" {
		s.Name = "bc-v-" + randomSuffix()
	}
	c.probeOnce.Do(func() { c.probeResources(ctx) })
	args, err := c.Args(s)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, c.Engine, args...)
	cmd.Env = os.Environ() // the engine CLI itself needs DOCKER_HOST etc.; the container does not inherit it
	name := sanitizeName(s.Name)
	cmd.Cancel = func() error {
		_ = c.Remove(context.Background(), name)
		return cmd.Process.Kill()
	}
	// Once the CLI is gone, do not wait long for its output pipes.
	cmd.WaitDelay = 10 * time.Second
	return cmd, nil
}

// removeTimeout bounds `<engine> rm -f`, which runs after the caller's
// context is already done.
const removeTimeout = 15 * time.Second

// Remove force-removes the named container (stopping it first) if it exists.
// It is used on cancellation and to clear a stale container left by a
// crashed run before reusing its name.
func (c *Container) Remove(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, removeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, c.Engine, "rm", "-f", sanitizeName(name)).CombinedOutput()
	if err != nil && !strings.Contains(strings.ToLower(string(out)), "no such container") {
		return fmt.Errorf("%s rm -f %s: %w: %s", c.Engine, name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func randomSuffix() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Args returns the engine arguments (exported for tests and dry runs).
func (c *Container) Args(s Spec) ([]string, error) {
	if len(s.Argv) == 0 {
		return nil, fmt.Errorf("sandbox: empty argv")
	}
	if c.Image == "" {
		return nil, fmt.Errorf("sandbox: no image configured")
	}
	args := []string{"run", "--rm", "--init",
		"--network", orDefault(c.Network, "none"),
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--pids-limit", fmt.Sprint(max(c.PIDs, 64)),
		"--user", fmt.Sprintf("%d:%d", c.UID, c.GID),
		// A writable, private HOME: tools expect one, and it must not be the host's.
		"--tmpfs", "/home/agent:rw,exec,size=1g,mode=1777",
		"-e", "HOME=/home/agent",
	}
	if s.Interactive {
		args = append(args, "-i")
	}
	if mem := capMemory(c.Memory, c.MaxMemBytes); mem != "" {
		args = append(args, "--memory", mem)
	}
	if cpus := capCPUs(c.CPUs, c.MaxCPUs); cpus != "" {
		args = append(args, "--cpus", cpus)
	}
	if s.Name != "" {
		args = append(args, "--name", sanitizeName(s.Name))
	}
	for _, m := range s.Mounts {
		if !filepath.IsAbs(m.Host) || !filepath.IsAbs(m.Target) {
			return nil, fmt.Errorf("sandbox: mount paths must be absolute: %s -> %s", m.Host, m.Target)
		}
		if forbiddenHostMount(m.Host) {
			return nil, fmt.Errorf("sandbox: refusing to mount sensitive host path %s", m.Host)
		}
		spec := fmt.Sprintf("type=bind,source=%s,target=%s", m.Host, ContainerPath(m.Target))
		if m.ReadOnly {
			spec += ",readonly"
		}
		args = append(args, "--mount", spec)
	}
	for _, p := range s.Scratch {
		if !filepath.IsAbs(p) {
			return nil, fmt.Errorf("sandbox: scratch path must be absolute: %s", p)
		}
		args = append(args, "--mount", fmt.Sprintf("type=tmpfs,destination=%s,tmpfs-size=%d,tmpfs-mode=1777", ContainerPath(p), scratchSize))
	}
	for _, p := range s.Masks {
		// The host path tells us the type; the mask goes where the sandbox
		// sees that path.
		fi, err := os.Stat(p)
		switch {
		case err != nil:
			continue // nothing to hide
		case fi.IsDir():
			args = append(args, "--mount", fmt.Sprintf("type=tmpfs,destination=%s,tmpfs-size=1k,tmpfs-mode=0500", ContainerPath(p)))
		default:
			// tmpfs cannot cover a file: bind an empty read-only file instead.
			empty, err := emptyFile()
			if err != nil {
				return nil, err
			}
			args = append(args, "--mount", fmt.Sprintf("type=bind,source=%s,target=%s,readonly", empty, ContainerPath(p)))
		}
	}
	if s.Workdir != "" {
		args = append(args, "-w", ContainerPath(s.Workdir))
	}
	for _, kv := range envList(s.Env) {
		if k, v, ok := strings.Cut(kv, "="); ok && hostPathValue(v) {
			kv = k + "=" + ContainerPath(v)
		}
		args = append(args, "-e", kv)
	}
	args = append(args, c.Image)
	return append(args, s.Argv...), nil
}

// forbiddenHostMount rejects mounting credential stores or the whole home.
func forbiddenHostMount(p string) bool {
	p = filepath.Clean(p)
	home, _ := os.UserHomeDir()
	if p == filepath.VolumeName(p)+string(filepath.Separator) || pathutil.Equal(p, home) ||
		p == "/var/run/docker.sock" || p == "/run/docker.sock" {
		return true
	}
	for _, d := range []string{".ssh", ".aws", ".config/gcloud", ".azure", ".kube", ".codex", ".gnupg", ".docker", ".netrc", ".config/gh",
		// Package-manager credentials next to the caches that are mounted.
		".cargo/credentials", ".cargo/credentials.toml", ".m2/settings.xml", ".m2/settings-security.xml",
		".gradle/gradle.properties", ".pypirc", ".gem/credentials", ".composer/auth.json", ".config/composer/auth.json",
		// macOS and Windows credential and cloud-tool locations.
		"Library/Keychains", "Library/Application Support/gcloud", "AppData/Roaming/gcloud", "AppData/Roaming/GitHub CLI"} {
		if pathutil.Within(p, filepath.Join(home, filepath.FromSlash(d))) {
			return true
		}
	}
	return false
}

func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func envList(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+m[k])
	}
	return out
}

// scrubbedEnv is the host environment minus variables that commonly carry
// credentials. Used by the unsandboxed runner (dev only) and verification.
func scrubbedEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if isSensitiveEnv(k) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// isSensitiveEnv reports whether an environment variable name is likely to
// carry a credential.
func isSensitiveEnv(k string) bool {
	u := strings.ToUpper(k)
	for _, pfx := range []string{"AWS_", "AZURE_", "GOOGLE_", "GCLOUD_", "CLOUDSDK_", "OPENAI_", "ANTHROPIC_", "CODEX_", "GITHUB_TOKEN", "GH_TOKEN", "GITLAB_", "SSH_AUTH_SOCK", "SSH_AGENT", "KUBECONFIG", "DOCKER_AUTH", "VAULT_", "NPM_TOKEN", "HF_TOKEN", "HUGGING", "LMNR_", "OTEL_", "LANGFUSE_", "SENTRY_"} {
		if strings.HasPrefix(u, pfx) {
			return true
		}
	}
	for _, sub := range []string{"SECRET", "PASSWORD", "PASSWD", "API_KEY", "APIKEY", "_TOKEN", "PRIVATE_KEY", "CREDENTIAL"} {
		if strings.Contains(u, sub) {
			return true
		}
	}
	return false
}

// ScrubbedEnv is exported for runners that execute on the host.
func ScrubbedEnv() []string { return scrubbedEnv() }

// emptyFile returns a read-only empty file used to mask secret files. It lives
// under the user cache dir because Docker Desktop only shares $HOME-like
// paths with its VM (a host /dev/null is not reachable).
func emptyFile() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, "boundedcode", "sandbox-empty")
	if fi, err := os.Stat(p); err == nil && fi.Size() == 0 {
		return p, nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(p, nil, 0o444); err != nil {
		return "", err
	}
	return p, nil
}

// probeResources reads the engine's CPU count and memory (a few hundred
// milliseconds; once per Container).
func (c *Container) probeResources(ctx context.Context) {
	if c.MaxCPUs > 0 || c.MaxMemBytes > 0 {
		return
	}
	format := "{{.NCPU}} {{.MemTotal}}" // docker
	if filepath.Base(c.Engine) == "podman" || strings.HasPrefix(filepath.Base(c.Engine), "podman.") {
		format = "{{.Host.CPUs}} {{.Host.MemTotal}}"
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, c.Engine, "info", "--format", format).Output()
	if err != nil {
		return // the engine check reports an unusable engine with its fix
	}
	var cpus int
	var mem int64
	if _, err := fmt.Sscan(strings.TrimSpace(string(out)), &cpus, &mem); err == nil {
		c.MaxCPUs, c.MaxMemBytes = cpus, mem
	}
}

// capCPUs limits a --cpus value to the engine's CPU count.
func capCPUs(v string, maxCPUs int) string {
	if v == "" || maxCPUs <= 0 {
		return v
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil && f > float64(maxCPUs) {
		return strconv.Itoa(maxCPUs)
	}
	return v
}

// capMemory limits a --memory value (512m, 8g, ...) to 90% of the engine's
// memory, leaving room for the engine's own VM.
func capMemory(v string, maxBytes int64) string {
	if v == "" || maxBytes <= 0 {
		return v
	}
	n, ok := parseBytes(v)
	limit := maxBytes / 10 * 9
	if ok && n > limit {
		return fmt.Sprintf("%dm", limit>>20)
	}
	return v
}

// parseBytes reads docker's memory syntax: a number with an optional b, k,
// m or g suffix.
func parseBytes(v string) (int64, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	mult := int64(1)
	switch {
	case strings.HasSuffix(v, "g"):
		mult, v = 1<<30, strings.TrimSuffix(v, "g")
	case strings.HasSuffix(v, "m"):
		mult, v = 1<<20, strings.TrimSuffix(v, "m")
	case strings.HasSuffix(v, "k"):
		mult, v = 1<<10, strings.TrimSuffix(v, "k")
	case strings.HasSuffix(v, "b"):
		v = strings.TrimSuffix(v, "b")
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 {
		return 0, false
	}
	return int64(f * float64(mult)), true
}
