// Package execbox runs direct mode's subprocesses (shell commands,
// background shells, template commands, post-edit checks) under operating-
// system containment, independent of the project's language.
//
// Backends, strongest first:
//
//   - bwrap (Linux): bubblewrap with mount, pid, network, ipc and uts
//     namespaces. The host is read-only; home directories, /run and other
//     users' files are hidden; the project, a private /tmp and the tool
//     caches are writable; secret files in the project are masked; the
//     repository's hooks and git configuration are read-only. There is no
//     network: approved hosts are reached through an allowlisting proxy
//     over a Unix socket, so a program that ignores the proxy variables has
//     no network at all. Every process ends with the command.
//   - landlock (Linux, partial): the filesystem only. Landlock cannot hide
//     a path beneath an allowed one (secrets in the project, .git/hooks),
//     does not mediate pathname Unix sockets, and filters TCP by port only,
//     so the network is not confined.
//   - none: macOS, Windows, and Linux without either. Process trees are
//     still ended (process groups; Windows job objects), and secret-looking
//     environment variables are still removed.
//
// The Report says, per resource, what is enforced. Direct mode asks before
// running commands under anything weaker than strong containment.
package execbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Levels of containment.
const (
	LevelStrong  = "strong"  // filesystem, network and processes confined
	LevelPartial = "partial" // some of them
	LevelNone    = "none"    // no operating-system containment
	LevelOff     = "off"     // turned off in the configuration
)

// Network modes.
const (
	NetProxy = "proxy" // no network; allowed hosts through the proxy (default)
	NetNone  = "none"  // no network at all
	NetHost  = "host"  // the host's network, unconfined
)

// Config is the sandbox configuration (direct.sandbox).
type Config struct {
	// Mode is "auto" (the strongest available backend, default) or "off".
	Mode string
	// Backend forces one ("bwrap", "landlock", "none"); "" picks.
	Backend string
	// Network is NetProxy (default), NetNone or NetHost.
	Network string
	// AllowHosts may always be reached through the proxy.
	AllowHosts []string
	// Registries lets commands reach the package registries of the
	// project's ecosystems and of the package managers they run.
	Registries bool
	// ReadPaths and WritePaths are further host paths to expose.
	ReadPaths, WritePaths []string
	// EnvAllow are environment variable names passed through although
	// they look secret.
	EnvAllow []string
	// StateDir holds the per-session /tmp and proxy sockets.
	StateDir string
	// Unavailable treats these backends as unavailable, as on a machine
	// without them (tests; not a configuration setting).
	Unavailable []string
}

// Report says what is enforced.
type Report struct {
	Backend     string `json:"backend"`
	Level       string `json:"level"`
	Filesystem  string `json:"filesystem"`
	Network     string `json:"network"`
	Processes   string `json:"processes"`
	Credentials string `json:"credentials"`
	// Missing says why a stronger backend is not used.
	Missing []string `json:"missing,omitempty"`
	// ByConfig is set when the configuration chose the weaker containment
	// (a backend, network host): that choice is the user's authorization.
	ByConfig bool `json:"by_config,omitempty"`
}

// String is a multi-line description.
func (r Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "containment: %s (%s)\n", r.Level, r.Backend)
	fmt.Fprintf(&b, "  filesystem:  %s\n", r.Filesystem)
	fmt.Fprintf(&b, "  network:     %s\n", r.Network)
	fmt.Fprintf(&b, "  processes:   %s\n", r.Processes)
	fmt.Fprintf(&b, "  credentials: %s\n", r.Credentials)
	for _, m := range r.Missing {
		fmt.Fprintf(&b, "  not available: %s\n", m)
	}
	return strings.TrimRight(b.String(), "\n")
}

// NetEvent is a proxy decision.
type NetEvent struct {
	Host    string
	Port    string
	Allowed bool
	Reason  string
}

// Spec is one command to run.
type Spec struct {
	Argv []string
	Dir  string
	// Env is added to the scrubbed environment.
	Env []string
	// Hosts may be reached through the proxy, beyond Config.AllowHosts.
	Hosts []string
	// Tools are the programs the command runs (for registry access).
	Tools []string
	// OnNet reports proxy decisions.
	OnNet func(NetEvent)
}

// Box runs commands for one project.
type Box struct {
	root string
	cfg  Config

	once    sync.Once
	backend backend
	report  Report

	maskOnce sync.Once
	masks    []string // secret files and directories in the project

	ecoOnce   sync.Once
	ecoTools  []string
	identOnce sync.Once
	ident     []string
}

type backend interface {
	name() string
	report(b *Box) Report
	// wrap rewrites the command to run under the backend.
	wrap(b *Box, s Spec, net *proxyHandle, tmp string) (argv []string, env []string, err error)
}

// New returns a Box for the project at root.
func New(root string, cfg Config) *Box {
	if cfg.Network == "" {
		cfg.Network = NetProxy
	}
	if cfg.StateDir == "" {
		cfg.StateDir = filepath.Join(os.TempDir(), "boundedcode-sandbox")
	}
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	return &Box{root: root, cfg: cfg}
}

// Report describes the containment commands get.
func (b *Box) Report() Report {
	b.pick()
	return b.report
}

// Root is the project the box confines commands to.
func (b *Box) Root() string { return b.root }

func (b *Box) pick() {
	b.once.Do(func() {
		var missing []string
		if b.cfg.Mode == "off" {
			b.backend = noneBackend{off: true}
			b.report = b.backend.report(b)
			return
		}
		for _, cand := range platformBackends() {
			if b.cfg.Backend != "" && cand.name() != b.cfg.Backend {
				continue
			}
			if slices.Contains(b.cfg.Unavailable, cand.name()) {
				missing = append(missing, cand.name()+": unavailable")
				continue
			}
			if why := probe(cand); why != "" {
				missing = append(missing, cand.name()+": "+why)
				continue
			}
			b.backend = cand
			break
		}
		if b.backend == nil {
			b.backend = noneBackend{}
			if b.cfg.Backend != "" && b.cfg.Backend != "none" {
				missing = append(missing, "the configured backend "+b.cfg.Backend+" is not available")
			}
		}
		b.report = b.backend.report(b)
		if b.report.Level != LevelStrong {
			b.report.Missing = append(missing, unsupported()...)
			b.report.ByConfig = b.cfg.Network == NetHost || b.cfg.Backend != "" && b.cfg.Backend != "bwrap"
		}
	})
}

var (
	probeMu    sync.Mutex
	probeCache = map[string]string{}
)

func probe(be backend) string {
	probeMu.Lock()
	defer probeMu.Unlock()
	if why, ok := probeCache[be.name()]; ok {
		return why
	}
	why := probeBackend(be)
	probeCache[be.name()] = why
	return why
}

// Cmd is a contained command.
type Cmd struct {
	*exec.Cmd
	cleanup func()
	tree    *tree
	done    sync.Once
}

// Command prepares s to run in the box; ctx cancels it and ends its
// whole process tree. The caller sets Stdout, Stderr and Stdin and calls
// Run, or Start and Wait.
func (b *Box) Command(ctx context.Context, s Spec) (*Cmd, error) {
	if len(s.Argv) == 0 {
		return nil, errors.New("no command")
	}
	b.pick()
	env := b.environment(s)
	var cleanups []func()
	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}
	tmp, err := b.sessionTmp()
	if err != nil {
		return nil, err
	}
	var ph *proxyHandle
	if b.backend.name() == "bwrap" && b.cfg.Network == NetProxy {
		// Even with no host allowed, the proxy answers: an attempt is
		// refused with a reason the model can act on, and recorded.
		ph, err = startProxy(b.cfg.StateDir, b.allowedHosts(s), s.OnNet, true)
		if err != nil {
			return nil, fmt.Errorf("network proxy: %w", err)
		}
		cleanups = append(cleanups, ph.close)
	}
	argv, extra, err := b.backend.wrap(b, s, ph, tmp)
	if err != nil {
		cleanup()
		return nil, err
	}
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Dir = s.Dir
	c.Env = append(env, extra...)
	c.WaitDelay = 2 * time.Second
	t := newTree(c)
	return &Cmd{Cmd: c, cleanup: cleanup, tree: t}, nil
}

// Start starts the command and puts it in its process tree.
func (c *Cmd) Start() error {
	if err := c.Cmd.Start(); err != nil {
		c.release()
		return err
	}
	c.tree.started(c.Cmd)
	return nil
}

// Wait waits for it and releases the proxy and the tree.
func (c *Cmd) Wait() error {
	err := c.Cmd.Wait()
	c.tree.kill() // whatever it left running in its tree ends with it
	c.release()
	return err
}

// Run starts and waits.
func (c *Cmd) Run() error {
	if err := c.Start(); err != nil {
		return err
	}
	return c.Wait()
}

// Kill ends the whole process tree.
func (c *Cmd) Kill() { c.tree.kill() }

func (c *Cmd) release() {
	c.done.Do(func() {
		c.tree.close()
		if c.cleanup != nil {
			c.cleanup()
		}
	})
}

// Plain prepares a command outside any sandbox (hooks, which the user
// configured) that still ends its whole process tree.
func Plain(ctx context.Context, argv []string, dir string, env []string) *Cmd {
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Dir = dir
	c.Env = env
	c.WaitDelay = 2 * time.Second
	return &Cmd{Cmd: c, tree: newTree(c)}
}

func (b *Box) sessionTmp() (string, error) {
	d := filepath.Join(b.cfg.StateDir, "tmp")
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	return d, nil
}

// secretEnv are names of variables that carry credentials.
var secretEnv = regexp.MustCompile(`(?i)(token|secret|passw|api_?key|access_?key|private_?key|credential|(^|_)auth($|_)|oauth|authorization|session|cookie|signing|_pat$|^pat_)`)

// dropEnv are further variables that lead to credentials or the user's
// session: agent sockets, the session bus, cloud and cluster configuration.
var dropEnv = map[string]bool{"SSH_AUTH_SOCK": true, "SSH_AGENT_PID": true, "GPG_AGENT_INFO": true, "DBUS_SESSION_BUS_ADDRESS": true,
	"KUBECONFIG": true, "DOCKER_HOST": true, "DOCKER_CONFIG": true, "GOOGLE_APPLICATION_CREDENTIALS": true, "AWS_PROFILE": true,
	"AWS_SHARED_CREDENTIALS_FILE": true, "AWS_CONFIG_FILE": true, "AZURE_CONFIG_DIR": true, "CLOUDSDK_CONFIG": true,
	"NETRC": true, "NPM_CONFIG_USERCONFIG": true, "XDG_RUNTIME_DIR": true, "HTTP_PROXY": true, "HTTPS_PROXY": true,
	"ALL_PROXY": true, "http_proxy": true, "https_proxy": true, "all_proxy": true, "NO_PROXY": true, "no_proxy": true}

// environment is the host environment without credentials, with the
// command's additions and the git identity.
func (b *Box) environment(s Spec) []string {
	if b.Report().Level == LevelOff {
		return append(os.Environ(), s.Env...) // on the host as they are
	}
	allow := map[string]bool{}
	for _, n := range b.cfg.EnvAllow {
		allow[n] = true
	}
	var out []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !allow[name] && (dropEnv[name] || secretEnv.MatchString(name)) {
			continue
		}
		out = append(out, kv)
	}
	out = append(out, b.identity()...)
	out = append(out, "GIT_TERMINAL_PROMPT=0")
	return append(out, s.Env...)
}

// identity carries the user's git name and e-mail into the sandbox, where
// their git configuration (which may hold credentials) is hidden.
func (b *Box) identity() []string {
	b.identOnce.Do(func() {
		if b.Report().Level == LevelOff {
			return
		}
		for _, kv := range [][2]string{{"user.name", "NAME"}, {"user.email", "EMAIL"}} {
			c := exec.Command("git", "-c", "core.fsmonitor=false", "config", "--get", kv[0]) //nolint:noctx // quick read
			c.Dir = b.root
			out, err := c.Output()
			if err != nil || strings.TrimSpace(string(out)) == "" {
				continue
			}
			v := strings.TrimSpace(string(out))
			b.ident = append(b.ident, "GIT_AUTHOR_"+kv[1]+"="+v, "GIT_COMMITTER_"+kv[1]+"="+v)
		}
	})
	return b.ident
}

// allowedHosts are the hosts a command may reach through the proxy.
func (b *Box) allowedHosts(s Spec) []string {
	set := map[string]bool{}
	for _, h := range append(append([]string{}, b.cfg.AllowHosts...), s.Hosts...) {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			set[h] = true
		}
	}
	if b.cfg.Registries {
		for _, t := range append(b.projectTools(), s.Tools...) {
			for _, h := range registries[t] {
				set[h] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for h := range set {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// registries are the hosts package managers download from.
var registries = map[string][]string{
	"go":       {"proxy.golang.org", "sum.golang.org", "storage.googleapis.com"},
	"npm":      {"registry.npmjs.org"},
	"npx":      {"registry.npmjs.org"},
	"pnpm":     {"registry.npmjs.org"},
	"yarn":     {"registry.yarnpkg.com", "registry.npmjs.org", "repo.yarnpkg.com"},
	"bun":      {"registry.npmjs.org"},
	"deno":     {"deno.land", "jsr.io", "registry.npmjs.org"},
	"pip":      {"pypi.org", "files.pythonhosted.org"},
	"pip3":     {"pypi.org", "files.pythonhosted.org"},
	"python":   {"pypi.org", "files.pythonhosted.org"},
	"python3":  {"pypi.org", "files.pythonhosted.org"},
	"uv":       {"pypi.org", "files.pythonhosted.org"},
	"poetry":   {"pypi.org", "files.pythonhosted.org"},
	"pipenv":   {"pypi.org", "files.pythonhosted.org"},
	"pdm":      {"pypi.org", "files.pythonhosted.org"},
	"cargo":    {"index.crates.io", "static.crates.io", "crates.io"},
	"mvn":      {"repo.maven.apache.org", "repo1.maven.org"},
	"mvnw":     {"repo.maven.apache.org", "repo1.maven.org"},
	"gradle":   {"repo.maven.apache.org", "repo1.maven.org", "plugins.gradle.org", "services.gradle.org", "downloads.gradle.org"},
	"gradlew":  {"repo.maven.apache.org", "repo1.maven.org", "plugins.gradle.org", "services.gradle.org", "downloads.gradle.org"},
	"dotnet":   {"api.nuget.org"},
	"gem":      {"rubygems.org", "index.rubygems.org"},
	"bundle":   {"rubygems.org", "index.rubygems.org"},
	"composer": {"repo.packagist.org", "packagist.org"},
	"mix":      {"repo.hex.pm", "hex.pm"},
	"dart":     {"pub.dev"},
	"flutter":  {"pub.dev"},
	"swift":    {"github.com"},
}

// Registries returns the hosts of a tool's package registry.
func Registries(tool string) []string { return registries[tool] }

var unsupportedOS = map[string]string{
	"darwin":  "macOS: sandbox-exec (Seatbelt) is deprecated and is not verified by this project; no operating-system containment is claimed",
	"windows": "Windows: AppContainer and Windows Sandbox are not used by this project yet; no operating-system containment is claimed",
}

func unsupported() []string {
	if why, ok := unsupportedOS[runtime.GOOS]; ok {
		return []string{why}
	}
	return nil
}

// Close removes the session's sandbox state (its /tmp).
func (b *Box) Close() {
	if b.cfg.StateDir != "" {
		_ = os.RemoveAll(b.cfg.StateDir)
	}
}

// SafeGitArgs are prepended to git invocations BoundedCode makes itself on
// a repository it does not trust: a repository's configuration cannot make
// them run a program.
var SafeGitArgs = []string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "diff.external=", "-c", "core.pager=cat"}
