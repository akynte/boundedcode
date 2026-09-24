package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// RuntimeOptions describes the short-lived BoundedCode supervisor that owns
// the model server and other session services.
//
// The supervisor is a child process rather than a goroutine hidden inside the
// OpenCode launcher. That gives the whole session one owner: the launcher can
// wait for readiness, forward termination, and tear down the process tree
// without relying on a deferred function in a library that a future editor
// adapter might forget to call.
type RuntimeOptions struct {
	// Binary is the bcode executable used to start the supervisor.
	Binary string
	// DataDir is the installation data root.
	DataDir string
	// Dir is the workspace directory the supervisor should run in.
	Dir string
	// Addr is the loopback API address. When empty, Start reserves one.
	Addr string
	// Env contains additional environment entries. Existing BC_* values are
	// replaced deterministically by the options above.
	Env []string
	// LogPath receives supervisor diagnostics. It is removed by Close when
	// RemoveLog is true.
	LogPath      string
	RemoveLog    bool
	StartTimeout time.Duration
	StopTimeout  time.Duration
}

// Runtime is a running supervisor owned by one OpenCode session.
type Runtime struct {
	cmd  *exec.Cmd
	addr string

	mu      sync.Mutex
	waitErr error
	done    chan struct{}
	closed  bool
	closing bool

	logFile     *os.File
	removeLog   bool
	stopTimeout time.Duration
}

// ReserveLoopbackAddr returns a currently unused loopback TCP address. The
// listener is closed before the supervisor binds it; a short race remains, so
// callers should still handle an address collision by retrying Start.
func ReserveLoopbackAddr() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return "", err
	}
	return addr, nil
}

// Start launches `bcode api` and waits until its readiness endpoint says every
// essential child is ready. A model load can take minutes, so the caller gets
// one bounded startup context rather than a short health-probe timeout.
func Start(ctx context.Context, opts RuntimeOptions) (*Runtime, error) {
	if strings.TrimSpace(opts.Binary) == "" {
		return nil, errors.New("session: runtime binary is empty")
	}
	if strings.TrimSpace(opts.DataDir) == "" {
		return nil, errors.New("session: runtime data directory is empty")
	}
	addr := opts.Addr
	if addr == "" {
		var err error
		addr, err = ReserveLoopbackAddr()
		if err != nil {
			return nil, fmt.Errorf("session: reserve supervisor address: %w", err)
		}
	}
	if !strings.HasPrefix(addr, "127.0.0.1:") && !strings.HasPrefix(addr, "localhost:") {
		return nil, fmt.Errorf("session: supervisor address %q is not loopback", addr)
	}
	startTimeout := opts.StartTimeout
	if startTimeout <= 0 {
		startTimeout = 45 * time.Second
	}
	stopTimeout := opts.StopTimeout
	if stopTimeout <= 0 {
		stopTimeout = 35 * time.Second
	}

	var logFile *os.File
	if opts.LogPath != "" {
		if err := os.MkdirAll(filepath.Dir(opts.LogPath), 0o700); err != nil {
			return nil, err
		}
		var err error
		logFile, err = os.OpenFile(opts.LogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return nil, err
		}
	}

	// Do not tie the supervisor's lifetime to the request context. A canceled
	// context must reach Close, which sends SIGTERM and lets the supervisor
	// stop its model gracefully; CommandContext would SIGKILL the supervisor
	// first and could strand the model child.
	cmd := exec.Command(opts.Binary, "api", "--addr", addr) //nolint:gosec // the binary is the trusted bcode executable
	cmd.Dir = opts.Dir
	cmd.Env = mergeEnvironment(os.Environ(), opts.Env, map[string]string{
		storeEnvData:  opts.DataDir,
		"BC_API_ADDR": addr,
	})
	cmd.Stdout = io.Discard
	if logFile != nil {
		cmd.Stderr = logFile
	} else {
		cmd.Stderr = io.Discard
	}
	// A separate process group lets the launcher signal the supervisor and
	// its descendants together during emergency cleanup. The normal path
	// still gives the supervisor a chance to flush SQLite and stop its model
	// child gracefully.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	SetParentDeathSignal(cmd)
	if err := cmd.Start(); err != nil {
		if logFile != nil {
			_ = logFile.Close()
		}
		return nil, fmt.Errorf("session: start supervisor: %w", err)
	}
	r := &Runtime{
		cmd: cmd, addr: addr, done: make(chan struct{}), logFile: logFile,
		removeLog: opts.RemoveLog, stopTimeout: stopTimeout,
	}
	go func() {
		err := cmd.Wait()
		r.mu.Lock()
		r.waitErr = err
		r.mu.Unlock()
		close(r.done)
	}()

	readyCtx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	if err := r.waitReady(readyCtx, opts.LogPath); err != nil {
		_ = r.Close(context.Background())
		return nil, err
	}
	return r, nil
}

const storeEnvData = "BC_DATA"

// Addr reports the address on which the session supervisor is serving.
func (r *Runtime) Addr() string {
	if r == nil {
		return ""
	}
	return r.addr
}

// PID reports the supervisor process id, or zero after it has exited.
func (r *Runtime) PID() int {
	if r == nil || r.cmd == nil || r.cmd.Process == nil {
		return 0
	}
	return r.cmd.Process.Pid
}

// Done is closed when the supervisor exits. It is useful to a caller that
// wants to stop waiting for a model while the editor is still open.
func (r *Runtime) Done() <-chan struct{} {
	if r == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return r.done
}

// WaitReady checks /readyz until it returns HTTP 200. The response body is
// retained in the error when readiness is explicitly denied, because "not
// ready" without the reason is the least useful startup diagnostic there is.
func (r *Runtime) waitReady(ctx context.Context, logPath string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	url := "http://" + r.addr + "/readyz"
	var last string
	for {
		if r.exited() {
			return fmt.Errorf("session: supervisor exited before becoming ready%s", runtimeExitDetail(r.waitError(), logPath))
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			last = strings.TrimSpace(string(body))
		} else {
			last = err.Error()
		}
		select {
		case <-ctx.Done():
			if last != "" {
				return fmt.Errorf("session: supervisor did not become ready: %s (last response: %s)", ctx.Err(), last)
			}
			return fmt.Errorf("session: supervisor did not become ready: %w", ctx.Err())
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (r *Runtime) exited() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

func (r *Runtime) waitError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.waitErr
}

// Close terminates the supervisor and every model process it owns, then waits
// for the process tree to disappear. It is deliberately idempotent: command
// cleanup is often reached through both a normal return and an error defer.
func (r *Runtime) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	if r.closing {
		wait := r.done
		r.mu.Unlock()
		select {
		case <-wait:
			return nil
		case <-time.After(r.stopTimeout):
			return errors.New("session: another shutdown is still in progress")
		}
	}
	r.closing = true
	wait := r.done
	children := r.childPIDs()
	r.mu.Unlock()

	// The supervisor is the owner. Ask it to stop first so procman can drain
	// its model and services in the intended order. Child groups are only an
	// emergency fallback; killing them first could make procman restart a model
	// while the API is still shutting down.
	if !r.exited() && r.cmd.Process != nil {
		_ = syscall.Kill(-r.cmd.Process.Pid, syscall.SIGTERM)
	}
	waitTimeout := r.stopTimeout
	if waitTimeout <= 0 {
		waitTimeout = time.Second
	}
	// Cleanup must not inherit a canceled request context. The caller may have
	// canceled precisely because the editor exited, and that is when the
	// bounded detached cleanup is most important.
	_ = ctx
	select {
	case <-wait:
	case <-time.After(waitTimeout):
		for _, pid := range children {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
		if r.cmd.Process != nil {
			_ = syscall.Kill(-r.cmd.Process.Pid, syscall.SIGKILL)
		}
		select {
		case <-wait:
		case <-time.After(5 * time.Second):
			r.mu.Lock()
			r.closing = false
			r.mu.Unlock()
			return fmt.Errorf("session: supervisor did not exit after SIGKILL")
		}
	}

	// A broken procman implementation must not leave a model behind after its
	// supervisor has exited. Normal shutdown has already removed these groups;
	// this is a bounded last check, not part of the normal path.
	for _, pid := range children {
		if syscall.Kill(-pid, 0) == nil {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	}
	if r.logFile != nil {
		_ = r.logFile.Close()
	}
	if r.removeLog && r.logFile != nil {
		_ = os.Remove(r.logFile.Name())
	}
	r.mu.Lock()
	r.closed = true
	r.closing = false
	r.mu.Unlock()
	return nil
}

type runtimeStatus struct {
	Children []struct {
		PID int `json:"pid"`
	} `json:"children"`
}

func (r *Runtime) childPIDs() []int {
	req, err := http.NewRequest(http.MethodGet, "http://"+r.addr+"/v1/status", nil)
	if err != nil {
		return nil
	}
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var status runtimeStatus
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&status) != nil {
		return nil
	}
	out := make([]int, 0, len(status.Children))
	for _, child := range status.Children {
		if child.PID > 0 {
			out = append(out, child.PID)
		}
	}
	return out
}

func mergeEnvironment(base, extra []string, overrides map[string]string) []string {
	values := append([]string(nil), base...)
	values = append(values, extra...)
	// Apply overrides last, removing earlier occurrences. This makes a
	// session's data root and API address authoritative even if the caller's
	// shell contains stale BoundedCode variables.
	for key, value := range overrides {
		prefix := key + "="
		filtered := values[:0]
		for _, item := range values {
			if !strings.HasPrefix(item, prefix) {
				filtered = append(filtered, item)
			}
		}
		values = append(filtered, prefix+value)
	}
	return values
}

func runtimeExitDetail(err error, logPath string) string {
	if err != nil {
		return ": " + err.Error()
	}
	if logPath == "" {
		return ""
	}
	body, readErr := os.ReadFile(logPath) //nolint:gosec // path is owned by the session launcher
	if readErr != nil || len(body) == 0 {
		return ""
	}
	const max = 2 << 10
	if len(body) > max {
		body = body[len(body)-max:]
	}
	return "; supervisor log: " + strings.TrimSpace(string(body))
}
