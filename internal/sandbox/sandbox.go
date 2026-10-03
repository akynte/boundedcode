// Package sandbox runs commands in an isolated environment. The container
// implementation is the security boundary for agent tool execution
// (ADR-0003); agent-level permission prompts are not.
package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Mount binds a host path into the sandbox.
type Mount struct {
	Host     string
	Target   string
	ReadOnly bool
}

// Spec describes one command execution.
type Spec struct {
	Argv        []string
	Workdir     string // path inside the sandbox
	Mounts      []Mount
	Env         map[string]string
	Interactive bool // keep stdin open (needed for the stdio protocol)
	// Masks are paths inside the sandbox hidden behind empty read-only
	// mounts (secret files and directories inside mounted trees).
	Masks []string
	// Name labels the container (sanitized) for diagnostics.
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
}

// Name implements Sandbox.
func (c *Container) Name() string { return c.Engine }

// Isolated implements Sandbox.
func (c *Container) Isolated() bool { return true }

// Command implements Sandbox.
func (c *Container) Command(ctx context.Context, s Spec) (*exec.Cmd, error) {
	args, err := c.Args(s)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, c.Engine, args...)
	cmd.Env = os.Environ() // the engine CLI itself needs DOCKER_HOST etc.; the container does not inherit it
	return cmd, nil
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
	if c.Memory != "" {
		args = append(args, "--memory", c.Memory)
	}
	if c.CPUs != "" {
		args = append(args, "--cpus", c.CPUs)
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
		spec := fmt.Sprintf("type=bind,source=%s,target=%s", m.Host, m.Target)
		if m.ReadOnly {
			spec += ",readonly"
		}
		args = append(args, "--mount", spec)
	}
	for _, p := range s.Masks {
		// An empty, read-only tmpfs hides the path. Docker creates a mount point
		// for files too, so this masks both files and directories.
		args = append(args, "--mount", fmt.Sprintf("type=tmpfs,destination=%s,tmpfs-size=1k,tmpfs-mode=0500", p))
	}
	if s.Workdir != "" {
		args = append(args, "-w", s.Workdir)
	}
	for _, kv := range envList(s.Env) {
		args = append(args, "-e", kv)
	}
	args = append(args, c.Image)
	return append(args, s.Argv...), nil
}

// forbiddenHostMount rejects mounting credential stores or the whole home.
func forbiddenHostMount(p string) bool {
	p = filepath.Clean(p)
	home, _ := os.UserHomeDir()
	if p == "/" || p == home || p == "/var/run/docker.sock" || p == "/run/docker.sock" {
		return true
	}
	for _, d := range []string{".ssh", ".aws", ".config/gcloud", ".azure", ".kube", ".codex", ".gnupg", ".docker", ".netrc", ".config/gh"} {
		sens := filepath.Join(home, d)
		if p == sens || strings.HasPrefix(p, sens+string(filepath.Separator)) {
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
