// Package openhands implements agent.Runtime with the OpenHands Software Agent
// SDK, via the thin Python adapter in adapters/openhands/python (ADR-0004).
package openhands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/akynte/boundedcode/internal/agent"
	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/jsonrpc"
	"github.com/akynte/boundedcode/internal/sandbox"
)

// Runtime launches one adapter process per session.
type Runtime struct {
	Sandbox sandbox.Sandbox
	// Argv starts the adapter inside the sandbox, e.g.
	// ["bc-openhands-adapter"] in the image, or
	// ["uv","run","--frozen","--project",DIR,"bc-openhands-adapter"] on the host.
	Argv []string
	// Gateway returns the LLM gateway for a task (meters and forwards calls).
	Gateway func(taskID string) *inference.Gateway
	// LogDir receives per-session adapter stderr logs.
	LogDir       string
	ReadyTimeout time.Duration
	Log          *slog.Logger
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
	cancel  context.CancelFunc
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
	var masks []string
	for _, m := range req.Masks {
		masks = append(masks, filepath.Join(req.Workspace, m))
	}
	if !r.Sandbox.Isolated() && len(masks) > 0 && r.Log != nil {
		r.Log.Warn("sandbox is not isolated: secret path masks cannot be enforced", "masks", len(masks))
	}
	spec := sandbox.Spec{
		Argv: r.Argv, Workdir: req.Workspace, Mounts: mounts, Masks: masks, Interactive: true,
		Env:  map[string]string{"OPENHANDS_SUPPRESS_BANNER": "1", "BC_ADAPTER_LOG": "INFO", "PYTHONUNBUFFERED": "1"},
		Name: "bc-" + req.TaskID,
	}
	// The adapter must outlive request contexts; Close() ends it.
	procCtx, cancel := context.WithCancel(context.Background())
	cmd, err := r.Sandbox.Command(procCtx, spec)
	if err != nil {
		cancel()
		return nil, err
	}
	if err := os.MkdirAll(r.LogDir, 0o700); err != nil {
		cancel()
		return nil, err
	}
	logf, err := os.OpenFile(filepath.Join(r.LogDir, "adapter.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		cancel()
		return nil, err
	}
	fmt.Fprintf(logf, "\n=== %s adapter start (sandbox=%s) ===\n", time.Now().UTC().Format(time.RFC3339), r.Sandbox.Name())
	cmd.Stderr = logf
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
		return nil, fmt.Errorf("start adapter: %w", err)
	}
	s := &session{peer: jsonrpc.New(stdout, stdin), cmd: cmd, stdin: stdin, logf: logf, cancel: cancel, exited: make(chan struct{})}
	gw := r.Gateway(req.TaskID)
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
	s.peer.OnNotify(func(method string, params json.RawMessage) {
		switch method {
		case "ready":
			readyOnce.Do(func() { close(ready) })
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
		return nil, fmt.Errorf("adapter exited before ready: %w (see %s)", s.waitErr, logf.Name())
	case <-time.After(timeout):
		_ = s.Close()
		return nil, fmt.Errorf("adapter not ready after %s (see %s)", timeout, logf.Name())
	case <-ctx.Done():
		_ = s.Close()
		return nil, ctx.Err()
	}

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
	}
	if err := s.peer.Call(ctx, "session.open", params, &res); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("session.open: %w", err)
	}
	s.id, s.resumed = res.ConversationID, res.Resumed
	return s, nil
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

func (s *session) Send(ctx context.Context, message string) (agent.Result, error) {
	var res agent.Result
	err := s.call(ctx, "session.send", map[string]any{"message": message}, &res)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		// Stop the agent between steps so the conversation persists cleanly.
		ictx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = s.Interrupt(ictx)
	}
	return res, err
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
			s.cancel() // kills the process (or the engine CLI, which stops the container)
			<-s.exited
		}
		s.cancel()
		<-s.peer.Done()
		_ = s.logf.Close()
	})
	return nil
}
