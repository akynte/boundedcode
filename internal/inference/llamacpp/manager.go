package llamacpp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/buildinfo"
	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/hw"
	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/model"
)

// Manager starts, adopts and stops a llama-server process. The server runs
// in its own session so it outlives the CLI invocation that started it; its
// identity is persisted in a state file.
type Manager struct {
	Binary         string
	Host           string
	Port           int
	ModelsDir      string
	StateDir       string
	StartupTimeout time.Duration
	// IdleSleep makes servers started by Ensure unload the model after this
	// much inactivity (--sleep-idle-seconds); the next request reloads it.
	// Zero disables it. Start (benchmarks) passes its args verbatim.
	IdleSleep time.Duration
	Log       *slog.Logger
}

var _ inference.Runtime = (*Manager)(nil)

// serverState is persisted to StateDir/llama-server.json.
type serverState struct {
	PID       int       `json:"pid"`
	Binary    string    `json:"binary"`
	Args      []string  `json:"args"`
	ArgsHash  string    `json:"args_hash"`
	Profile   string    `json:"profile"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	StartedAt time.Time `json:"started_at"`
	LogPath   string    `json:"log_path"`
	// LoadSeconds is the time from spawn until /health returned 200.
	LoadSeconds float64 `json:"load_seconds"`
}

// Name implements inference.Runtime.
func (m *Manager) Name() string { return "llama.cpp" }

func (m *Manager) statePath() string { return filepath.Join(m.StateDir, "llama-server.json") }
func (m *Manager) logPath() string   { return filepath.Join(m.StateDir, "llama-server.log") }
func (m *Manager) baseURL() string {
	return "http://" + net.JoinHostPort(m.Host, strconv.Itoa(m.Port))
}

func (m *Manager) log() *slog.Logger {
	if m.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return m.Log
}

func (m *Manager) readState() (*serverState, error) {
	b, err := os.ReadFile(m.statePath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s serverState
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("corrupt %s: %w", m.statePath(), err)
	}
	return &s, nil
}

// Ensure implements inference.Runtime.
func (m *Manager) Ensure(ctx context.Context, p model.Profile) (inference.Endpoint, error) {
	ep := inference.Endpoint{BaseURL: m.baseURL(), Model: p.Name}
	modelPath := p.ResolveFile(m.ModelsDir)
	if _, err := os.Stat(modelPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ep, fmt.Errorf("model file for %s: %w (run `%s setup --only model` to download the default model)", p.Name, err, buildinfo.Command())
		}
		return ep, fmt.Errorf("model file for %s: %w", p.Name, err)
	}
	args := m.ServerArgs(p, modelPath)
	hash := ArgsHash(m.Binary, args)

	st, err := m.readState()
	if err != nil {
		return ep, err
	}
	if st != nil && alive(st.PID) {
		if st.ArgsHash == hash {
			if err := m.waitHealthy(ctx, nil, m.StartupTimeout); err != nil {
				return ep, err
			}
			m.log().Debug("reusing llama-server", "pid", st.PID)
			return ep, nil
		}
		m.log().Info("restarting llama-server with new configuration", "old_profile", st.Profile, "new_profile", p.Name)
		if err := m.Stop(ctx); err != nil {
			return ep, err
		}
	}
	d := net.Dialer{Timeout: 300 * time.Millisecond}
	if conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(m.Host, strconv.Itoa(m.Port))); err == nil {
		conn.Close()
		return ep, fmt.Errorf("port %d is in use by a process this tool did not start; stop it or use inference.mode: external", m.Port)
	}
	_, err = m.start(ctx, p, args, hash)
	return ep, err
}

// ServerArgs is BuildArgs plus the manager's runtime settings (idle sleep).
func (m *Manager) ServerArgs(p model.Profile, modelPath string) []string {
	return WithIdleSleep(BuildArgs(p, modelPath, m.Host, m.Port), m.IdleSleep)
}

// Start launches the server unconditionally (used by benchmarks that need a
// cold start). It fails if a managed server is already running.
func (m *Manager) Start(ctx context.Context, p model.Profile, args []string) (time.Duration, error) {
	if st, _ := m.readState(); st != nil && alive(st.PID) {
		return 0, fmt.Errorf("llama-server already running (pid %d)", st.PID)
	}
	return m.start(ctx, p, args, ArgsHash(m.Binary, args))
}

func (m *Manager) start(ctx context.Context, p model.Profile, args []string, hash string) (time.Duration, error) {
	if err := os.MkdirAll(m.StateDir, 0o700); err != nil {
		return 0, err
	}
	bin, err := exec.LookPath(m.Binary)
	if err != nil {
		return 0, fmt.Errorf("llama-server binary: %w (run `%s setup --only inference` to build it, or set inference.mode: external to use a server you run)", err, buildinfo.Command())
	}
	if _, err := os.Stat(m.logPath()); err == nil {
		_ = os.Rename(m.logPath(), m.logPath()+".1")
	}
	logf, err := os.OpenFile(m.logPath(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	defer logf.Close()

	// Deliberately not exec.CommandContext: the server must outlive ctx.
	cmd := exec.Command(bin, args...) //nolint:noctx // the server must outlive ctx and this process
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = detachedAttr()
	// Prebuilt release archives ship shared libraries next to the binary.
	cmd.Env = libraryEnv(bin)
	started := time.Now()
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start llama-server: %w", err)
	}
	st := serverState{
		PID: cmd.Process.Pid, Binary: bin, Args: args, ArgsHash: hash, Profile: p.Name,
		Host: m.Host, Port: m.Port, StartedAt: started.UTC(), LogPath: m.logPath(),
	}
	if err := m.writeState(st); err != nil {
		_ = cmd.Process.Kill()
		return 0, err
	}
	// Reap the child while this process lives so an early exit is observed;
	// the goroutine ends when the child exits.
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	m.log().Info("llama-server starting", "pid", st.PID, "profile", p.Name, "log", m.logPath())
	if err := m.waitHealthy(ctx, exited, m.StartupTimeout); err != nil {
		_ = m.Stop(context.WithoutCancel(ctx))
		return 0, err
	}
	load := time.Since(started)
	st.LoadSeconds = load.Seconds()
	_ = m.writeState(st)
	return load, nil
}

func (m *Manager) writeState(st serverState) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(m.statePath(), b, 0o600)
}

func (m *Manager) waitHealthy(ctx context.Context, exited <-chan error, timeout time.Duration) error {
	c := inference.NewClient(m.baseURL(), 5*time.Second)
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		if ok, _ := c.Healthy(ctx); ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-exited:
			return m.classifyExit(err)
		case <-deadline.C:
			return fmt.Errorf("llama-server not healthy after %s (stall?); see %s", timeout, m.logPath())
		case <-tick.C:
		}
	}
}

var failurePatterns = []struct {
	re   *regexp.Regexp
	kind string
}{
	{regexp.MustCompile(`(?i)out of memory|cudaMalloc failed|failed to allocate|unable to allocate|CUDA error: out of memory`), "out of memory (reduce ctx_size or gpu layers, or raise n_cpu_moe)"},
	{regexp.MustCompile(`(?i)couldn't bind|address already in use`), "port already in use"},
	{regexp.MustCompile(`(?i)failed to load model|error loading model|unknown model architecture`), "model failed to load (file or llama.cpp version incompatible)"},
	{regexp.MustCompile(`(?i)invalid argument|unknown argument|error: invalid`), "invalid server arguments"},
}

func (m *Manager) classifyExit(waitErr error) error {
	tail := tailFile(m.logPath(), 40)
	for _, f := range failurePatterns {
		if f.re.MatchString(tail) {
			return fmt.Errorf("llama-server exited: %s: %w\n--- log tail ---\n%s", f.kind, waitErr, lastLines(tail, 12))
		}
	}
	return fmt.Errorf("llama-server exited during startup: %w\n--- log tail ---\n%s", waitErr, lastLines(tail, 12))
}

// Stop implements inference.Runtime.
func (m *Manager) Stop(ctx context.Context) error {
	st, err := m.readState()
	if err != nil || st == nil {
		return err
	}
	if alive(st.PID) {
		_ = terminate(st.PID)
		deadline := time.Now().Add(30 * time.Second)
		for alive(st.PID) && time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}
		if alive(st.PID) {
			_ = kill(st.PID)
		}
	}
	if err := os.Remove(m.statePath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// props is the subset of GET /props we use.
type props struct {
	ModelPath  string `json:"model_path"`
	BuildInfo  string `json:"build_info"`
	DefaultGen struct {
		NCtx int `json:"n_ctx"`
	} `json:"default_generation_settings"`
	TotalSlots int  `json:"total_slots"`
	IsSleeping bool `json:"is_sleeping"`
}

// Status implements inference.Runtime.
func (m *Manager) Status(ctx context.Context) (inference.Status, error) {
	s := inference.Status{Managed: true, Endpoint: inference.Endpoint{BaseURL: m.baseURL()}, LogPath: m.logPath()}
	st, err := m.readState()
	if err != nil {
		return s, err
	}
	if st != nil && alive(st.PID) {
		s.Running, s.PID, s.Profile, s.StartedAt, s.Args = true, st.PID, st.Profile, st.StartedAt, st.Args
		s.Endpoint.Model = st.Profile
		s.RSSMiB = hw.ProcessRSSMiB(st.PID)
	} else if st != nil {
		s.Detail = "stale state file (server not running)"
	}
	c := inference.NewClient(m.baseURL(), 3*time.Second)
	if ok, _ := c.Healthy(ctx); ok {
		s.Healthy = true
		var p props
		if _, err := c.Get(ctx, "/props", &p); err == nil {
			s.Version, s.CtxSize, s.Sleeping = p.BuildInfo, p.DefaultGen.NCtx, p.IsSleeping
		}
		if !s.Running {
			s.Detail = "a server is answering on this port but was not started by this tool"
		}
	}
	return s, nil
}

// Version runs `llama-server --version` and returns e.g.
// "0.5.0 (build 11321, commit b0aca3c65)".
func Version(ctx context.Context, binary string) (string, error) {
	bin, err := exec.LookPath(binary)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--version")
	cmd.Env = libraryEnv(bin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s --version: %w", bin, err)
	}
	for l := range strings.SplitSeq(string(out), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(l), "version: "); ok {
			return v, nil
		}
	}
	return "", fmt.Errorf("unrecognized --version output: %q", lastLines(string(out), 3))
}

func joinEnvPath(a, b string) string {
	if b == "" {
		return a
	}
	return a + string(os.PathListSeparator) + b
}

func tailFile(path string, maxLines int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(b) > 64<<10 {
		b = b[len(b)-64<<10:]
	}
	return lastLines(string(b), maxLines)
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
