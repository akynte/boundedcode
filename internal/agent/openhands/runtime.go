// Package openhands implements agent.Runtime with the OpenHands Software Agent
// SDK, via the thin Python adapter in adapters/openhands/python (ADR-0004).
package openhands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/akynte/boundedcode/internal/agent"
	"github.com/akynte/boundedcode/internal/buildinfo"
	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/jsonrpc"
	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/telemetry"
)

// ProtocolVersion is the adapter protocol this control plane speaks. The
// adapter reports its own in the `ready` notification; a mismatch is refused
// (ADR-0004). Keep in sync with PROTOCOL_VERSION in the adapter's main.py.
const ProtocolVersion = 1

// Runtime launches one adapter process per session.
type Runtime struct {
	Sandbox sandbox.Sandbox
	// Argv starts the adapter inside the sandbox, e.g.
	// ["bc-openhands-adapter"] in the image, or
	// ["uv","run","--frozen","--project",DIR,"bc-openhands-adapter"] on the host.
	Argv []string
	// Gateway returns the LLM gateway for a task (meters and forwards calls).
	Gateway func(taskID string) *inference.Gateway
	// LogDir is unused: adapter stderr goes to a per-task log next to the
	// session's PersistenceDir (see AdapterLogPath).
	//
	// Deprecated: kept so existing callers compile.
	LogDir       string
	ReadyTimeout time.Duration
	// InterruptGrace bounds how long a cancelled Send waits for the agent to
	// stop after session.interrupt before the adapter is killed (default 30s).
	InterruptGrace time.Duration
	Log            *slog.Logger
}

// AdapterLogPath is where the adapter's (redacted) stderr for a session is
// written: one file per task, beside the task's persistence directory.
func AdapterLogPath(req agent.OpenRequest) string {
	return filepath.Join(filepath.Dir(req.PersistenceDir), "adapter.log")
}

// containerRemover is implemented by sandboxes whose sessions are named
// containers that can outlive the engine CLI (sandbox.Container).
type containerRemover interface {
	Remove(ctx context.Context, name string) error
}

var _ agent.Runtime = (*Runtime)(nil)

// Name implements agent.Runtime.
func (r *Runtime) Name() string { return "openhands" }

type session struct {
	id      string
	resumed bool
	peer    *jsonrpc.Peer
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	logf    *os.File
	stderr  *redactWriter
	cancel  context.CancelFunc
	remove  func() // force-removes the container, if any
	grace   time.Duration
	exited  chan struct{}
	waitErr error
	once    sync.Once
}

// Open implements agent.Runtime.
func (r *Runtime) Open(ctx context.Context, req agent.OpenRequest) (agent.Session, error) {
	for _, p := range []string{req.Workspace, req.PersistenceDir} {
		if !filepath.IsAbs(p) {
			return nil, fmt.Errorf("openhands: path must be absolute: %q", p)
		}
	}
	if err := os.MkdirAll(req.PersistenceDir, 0o700); err != nil {
		return nil, err
	}
	// Identity mounts: host paths appear unchanged inside the sandbox, so the
	// worktree's absolute gitdir pointer and persisted paths stay valid.
	mounts := []sandbox.Mount{
		{Host: req.Workspace, Target: req.Workspace},
		{Host: req.PersistenceDir, Target: req.PersistenceDir},
	}
	// The shared git dir is read-only so the agent cannot plant hooks or
	// config that would later run on the host; only the worktree's own
	// admin dir (HEAD, index) is writable. Commits are made host-side.
	for _, d := range req.GitCommonDirs {
		mounts = append(mounts, sandbox.Mount{Host: d, Target: d, ReadOnly: true})
	}
	for _, d := range req.GitAdminDirs {
		mounts = append(mounts, sandbox.Mount{Host: d, Target: d})
	}
	var scratch []string
	if r.Sandbox.Isolated() { // without a container there is no path translation
		for _, d := range req.DependencyMounts {
			mounts = append(mounts, sandbox.Mount{Host: d.Host, Target: d.Target, ReadOnly: true})
		}
		scratch = req.DependencyScratch
	}
	var masks []string
	for _, m := range req.Masks {
		masks = append(masks, filepath.Join(req.Workspace, m))
	}
	if !r.Sandbox.Isolated() && len(masks) > 0 && r.Log != nil {
		r.Log.Warn("sandbox is not isolated: secret path masks cannot be enforced", "masks", len(masks))
	}
	env := map[string]string{"OPENHANDS_SUPPRESS_BANNER": "1", "BC_ADAPTER_LOG": "INFO", "PYTHONUNBUFFERED": "1"}
	if r.Sandbox.Isolated() {
		mounts = append(mounts, toolchainMounts(req.Toolchain, env)...)
	}
	spec := sandbox.Spec{
		Argv: r.Argv, Workdir: req.Workspace, Mounts: mounts, Scratch: scratch, Masks: masks, Interactive: true,
		Env:  env,
		Name: "bc-" + req.TaskID,
	}
	remove := func() {}
	if rm, ok := r.Sandbox.(containerRemover); ok {
		// A container left by a crashed run holds the name and would make
		// `run --name` fail, blocking resume.
		if err := rm.Remove(ctx, spec.Name); err != nil {
			r.log().Warn("could not remove stale adapter container", "name", spec.Name, "err", err)
		}
		remove = func() { _ = rm.Remove(context.Background(), spec.Name) }
	}
	// The adapter must outlive request contexts; Close() ends it.
	procCtx, cancel := context.WithCancel(context.Background())
	cmd, err := r.Sandbox.Command(procCtx, spec)
	if err != nil {
		cancel()
		return nil, err
	}
	if !r.Sandbox.Isolated() {
		hostProcessGroup(cmd)
	}
	logPath := AdapterLogPath(req)
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		cancel()
		return nil, err
	}
	fmt.Fprintf(logf, "\n=== %s adapter start (sandbox=%s) ===\n", time.Now().UTC().Format(time.RFC3339), r.Sandbox.Name())
	// This run's output starts here; earlier runs of the task precede it.
	var runStart int64
	if fi, err := logf.Stat(); err == nil {
		runStart = fi.Size()
	}
	stderr := &redactWriter{w: logf}
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		logf.Close()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		logf.Close()
		return nil, err
	}
	cmd.WaitDelay = 10 * time.Second
	if err := cmd.Start(); err != nil {
		cancel()
		logf.Close()
		if c, ok := r.Sandbox.(sandbox.Checker); ok && errors.Is(err, exec.ErrNotFound) {
			if cerr := c.Check(ctx); cerr != nil {
				return nil, &startupError{msg: "start adapter: " + cerr.Error(), err: err}
			}
		}
		return nil, fmt.Errorf("start adapter: %w", err)
	}
	grace := r.InterruptGrace
	if grace == 0 {
		grace = 30 * time.Second
	}
	s := &session{peer: jsonrpc.New(stdout, stdin), cmd: cmd, stdin: stdin, logf: logf, stderr: stderr, cancel: cancel,
		remove: remove, grace: grace, exited: make(chan struct{})}
	s.peer.SetLogger(r.Log)
	gw := req.Gateway
	if gw == nil {
		gw = r.Gateway(req.TaskID)
	}
	s.peer.Handle("llm.complete", func(ctx context.Context, params json.RawMessage) (any, error) {
		var p struct {
			Path string         `json:"path"`
			Body map[string]any `json:"body"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		status, body, err := gw.Forward(ctx, p.Path, p.Body)
		if err != nil && !errors.Is(err, inference.ErrBudgetExhausted) {
			return nil, err
		}
		return map[string]any{"status": status, "body": body}, nil
	})
	ready := make(chan struct{})
	var readyOnce sync.Once
	var hello struct {
		ProtocolVersion *int   `json:"protocol_version"`
		SDKVersion      string `json:"sdk_version"`
	}
	s.peer.OnNotify(func(method string, params json.RawMessage) {
		switch method {
		case "ready":
			readyOnce.Do(func() {
				_ = json.Unmarshal(params, &hello)
				close(ready)
			})
		case "event":
			if req.OnEvent != nil {
				var ev agent.Event
				if json.Unmarshal(params, &ev) == nil {
					ev.Raw = append(json.RawMessage(nil), params...)
					req.OnEvent(ev)
				}
			}
		}
	})
	go s.peer.Serve()
	go func() {
		s.waitErr = cmd.Wait()
		close(s.exited)
	}()

	timeout := r.ReadyTimeout
	if timeout == 0 {
		timeout = 3 * time.Minute
	}
	select {
	case <-ready:
	case <-s.exited:
		_ = s.Close() // flushes the log
		return nil, r.exitedBeforeReady(ctx, s.waitErr, logPath, runStart)
	case <-time.After(timeout):
		_ = s.Close()
		return nil, fmt.Errorf("adapter not ready after %s (see %s)", timeout, logf.Name())
	case <-ctx.Done():
		_ = s.Close()
		return nil, ctx.Err()
	}
	if err := checkProtocol(hello.ProtocolVersion); err != nil {
		_ = s.Close()
		return nil, err
	}
	r.log().Debug("adapter ready", "sdk_version", hello.SDKVersion, "log", logPath)

	var res struct {
		ConversationID string `json:"conversation_id"`
		Resumed        bool   `json:"resumed"`
		EventCount     int    `json:"event_count"`
	}
	params := map[string]any{
		"workspace": req.Workspace, "persistence_dir": req.PersistenceDir, "model": gw.Model,
		"conversation_id": req.SessionID, "max_iterations": nilIfZero(req.MaxIterations),
		"max_input_tokens": nilIfZero(req.MaxInputTokens), "max_output_tokens": nilIfZero(req.MaxOutputTokens),
		"condenser_max_events": nilIfZero(req.CondenserMaxEvents), "condenser_max_tokens": nilIfZero(req.CondenserMaxTokens),
		"llm_timeout": llmTimeout(req.LLMTimeout),
	}
	if err := s.peer.Call(ctx, "session.open", params, &res); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("session.open: %w", err)
	}
	s.id, s.resumed = res.ConversationID, res.Resumed
	return s, nil
}

// startupTailLines is how much of the adapter log a startup failure quotes.
const startupTailLines = 10

// exitedBeforeReady explains an adapter that exited during startup: the
// sandbox's own diagnosis when its engine is not ready, and the last lines
// of this run's (already redacted) log, so the cause is in the error rather
// than only in the file.
func (r *Runtime) exitedBeforeReady(ctx context.Context, waitErr error, logPath string, runStart int64) error {
	var b strings.Builder
	fmt.Fprintf(&b, "adapter exited before ready: %v", waitErr)
	if c, ok := r.Sandbox.(sandbox.Checker); ok {
		if err := c.Check(ctx); err != nil {
			fmt.Fprintf(&b, "; %v", err)
		}
	}
	fmt.Fprintf(&b, " (see %s)", logPath)
	if lines := logTail(logPath, runStart, startupTailLines); len(lines) > 0 {
		b.WriteString("\nlast adapter output:\n  " + strings.Join(lines, "\n  "))
	}
	return &startupError{msg: b.String(), err: waitErr}
}

// startupError is a startup failure whose message already quotes err.
type startupError struct {
	msg string
	err error
}

func (e *startupError) Error() string { return e.msg }
func (e *startupError) Unwrap() error { return e.err }

// logTail returns the last n non-empty lines written to path from offset on.
func logTail(path string, offset int64, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	const maxRead = 64 << 10
	if fi, err := f.Stat(); err == nil && fi.Size()-offset > maxRead {
		offset = fi.Size() - maxRead
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(f, maxRead))
	if err != nil {
		return nil
	}
	var lines []string
	for l := range strings.SplitSeq(string(b), "\n") {
		if l = strings.TrimRight(l, "\r\t "); strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines[max(0, len(lines)-n):]
}

// checkProtocol refuses an adapter that speaks another protocol version.
func checkProtocol(v *int) error {
	fix := "rebuild the sandbox image with `" + buildinfo.Command() + " sandbox build` (host mode: update the adapter checkout and `uv sync`)"
	switch {
	case v == nil:
		return fmt.Errorf("adapter reported no protocol version, control plane expects v%d; %s", ProtocolVersion, fix)
	case *v != ProtocolVersion:
		return fmt.Errorf("adapter protocol v%d, control plane expects v%d; %s", *v, ProtocolVersion, fix)
	}
	return nil
}

// llmTimeoutMargin keeps the adapter's per-call LLM timeout above the
// gateway's, so the control plane's timeout fires first and the adapter does
// not retry a request the model server is still working on.
const llmTimeoutMargin = 30 * time.Second

// llmTimeout is the adapter's per-call timeout in seconds; nil keeps the
// adapter default.
func llmTimeout(d time.Duration) any {
	if d <= 0 {
		return nil
	}
	return int((d + llmTimeoutMargin + time.Second - 1) / time.Second)
}

func (r *Runtime) log() *slog.Logger {
	if r.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return r.Log
}

func nilIfZero(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func (s *session) ID() string    { return s.id }
func (s *session) Resumed() bool { return s.resumed }

// call wraps peer.Call so a dead adapter is reported as such.
func (s *session) call(ctx context.Context, method string, params, out any) error {
	err := s.peer.Call(ctx, method, params, out)
	if errors.Is(err, jsonrpc.ErrClosed) {
		select {
		case <-s.exited:
			return fmt.Errorf("adapter process exited (%w); see %s", s.waitErr, s.logf.Name())
		case <-time.After(2 * time.Second):
			return fmt.Errorf("adapter connection closed; see %s", s.logf.Name())
		}
	}
	return err
}

// Send runs the agent until it stops or ctx ends. On cancellation or deadline
// the agent is interrupted between steps so the conversation persists
// cleanly; if it does not stop within the grace period the adapter is killed.
// Either way ctx's error is returned.
func (s *session) Send(ctx context.Context, message string) (agent.Result, error) {
	if err := ctx.Err(); err != nil {
		return agent.Result{}, err
	}
	var res agent.Result
	// The call itself outlives ctx so the interrupted run's reply is awaited.
	callCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	defer stop()
	done := make(chan error, 1)
	go func() { done <- s.call(callCtx, "session.send", map[string]any{"message": message}, &res) }()
	select {
	case err := <-done:
		return res, err
	case <-ctx.Done():
	}
	ictx, cancel := context.WithTimeout(context.Background(), s.grace)
	defer cancel()
	if err := s.Interrupt(ictx); err == nil {
		select {
		case <-done:
			return res, ctx.Err()
		case <-ictx.Done():
		}
	}
	s.kill()
	stop()
	<-done
	return agent.Result{}, ctx.Err()
}

// kill terminates the adapter: the sandbox's cancel hook removes the
// container (or kills the host process group).
func (s *session) kill() {
	s.cancel()
	s.remove()
}

func (s *session) Condense(ctx context.Context) error {
	return s.call(ctx, "session.condense", map[string]any{}, nil)
}

func (s *session) Interrupt(ctx context.Context) error {
	return s.call(ctx, "session.interrupt", map[string]any{}, nil)
}

func (s *session) State(ctx context.Context) (agent.State, error) {
	var st agent.State
	return st, s.call(ctx, "session.state", map[string]any{}, &st)
}

// Close asks the adapter to shut down, then terminates it if needed.
func (s *session) Close() error {
	s.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		_ = s.peer.Call(ctx, "shutdown", map[string]any{}, nil)
		cancel()
		_ = s.stdin.Close()
		select {
		case <-s.exited:
		case <-time.After(15 * time.Second):
			s.kill()
			<-s.exited
		}
		s.cancel()
		<-s.peer.Done()
		s.stderr.Flush()
		_ = s.logf.Close()
	})
	return nil
}

// redactWriter writes adapter stderr line by line through telemetry.Redact so
// credentials echoed by tools or libraries do not land in the log.
type redactWriter struct {
	mu  sync.Mutex
	w   io.Writer
	buf []byte
}

const maxPartialLine = 64 << 10

func (r *redactWriter) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	for {
		i := bytes.IndexByte(r.buf, '\n')
		if i < 0 {
			break
		}
		_, _ = io.WriteString(r.w, telemetry.Redact(string(r.buf[:i+1])))
		r.buf = r.buf[i+1:]
	}
	if len(r.buf) > maxPartialLine {
		_, _ = io.WriteString(r.w, telemetry.Redact(string(r.buf)))
		r.buf = nil
	}
	r.buf = append([]byte(nil), r.buf...)
	return len(p), nil
}

// Flush writes a trailing partial line.
func (r *redactWriter) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.buf) > 0 {
		_, _ = io.WriteString(r.w, telemetry.Redact(string(r.buf)))
		r.buf = nil
	}
}

// toolchainMounts gives the agent the offline Go environment verification
// uses (read-only module cache, GOPROXY=off) and its own build cache. It
// sets env and returns the mounts; missing host directories are skipped.
func toolchainMounts(tc agent.Toolchain, env map[string]string) []sandbox.Mount {
	var out []sandbox.Mount
	env["GOTOOLCHAIN"], env["GOFLAGS"] = "local", "-buildvcs=false"
	if tc.GoModCache != "" {
		if st, err := os.Stat(tc.GoModCache); err == nil && st.IsDir() {
			out = append(out, sandbox.Mount{Host: tc.GoModCache, Target: tc.GoModCache, ReadOnly: true})
			env["GOMODCACHE"], env["GOPROXY"], env["GOFLAGS"] = tc.GoModCache, "off", "-buildvcs=false -mod=mod"
		}
	}
	if tc.GoCache != "" {
		if err := os.MkdirAll(tc.GoCache, 0o700); err == nil {
			out = append(out, sandbox.Mount{Host: tc.GoCache, Target: tc.GoCache})
			env["GOCACHE"] = tc.GoCache
		}
	}
	return out
}
