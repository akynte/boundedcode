package cbm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/akynte/boundedcode/internal/jsonrpc"
)

// session is a persistent MCP stdio connection to codebase-memory-mcp.
// Measured on the reference machine: ~4 s one-time start, then ~12 ms per
// search_graph call, versus ~4 s per one-shot CLI call (ADR-0006).
type session struct {
	cmd   *exec.Cmd
	peer  *jsonrpc.Peer
	stdin io.WriteCloser
	done  chan struct{}
	once  sync.Once
}

// privateSettings are applied to our isolated cache dir: no web UI listener
// and no background file watchers in processes we spawn.
var privateSettings = [][2]string{{"ui_enabled", "false"}, {"auto_watch", "false"}, {"watcher_enabled", "false"}}

func (c *Client) env() ([]string, error) {
	env := os.Environ()
	if c.CacheDir != "" {
		if err := os.MkdirAll(c.CacheDir, 0o700); err != nil {
			return nil, err
		}
		env = append(env, "CBM_CACHE_DIR="+c.CacheDir)
	}
	return env, nil
}

// configure writes privateSettings into the isolated cache dir once.
func (c *Client) configure(ctx context.Context) error {
	if c.CacheDir == "" {
		return errors.New("cbm: refusing to change settings without an isolated CacheDir")
	}
	marker := c.CacheDir + "/.boundedcode-configured"
	if _, err := os.Stat(marker); err == nil {
		return nil
	}
	env, err := c.env()
	if err != nil {
		return err
	}
	for _, kv := range privateSettings {
		cmd := exec.CommandContext(ctx, c.Binary, "config", "set", kv[0], kv[1])
		cmd.Env, cmd.Dir = env, os.TempDir()
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("cbm config set %s: %w: %s", kv[0], err, out)
		}
	}
	return os.WriteFile(marker, nil, 0o600)
}

// Open starts a persistent MCP session; subsequent calls use it. Close ends it.
func (c *Client) Open(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sess != nil {
		return nil
	}
	if err := c.configure(ctx); err != nil {
		return err
	}
	env, err := c.env()
	if err != nil {
		return err
	}
	cmd := exec.Command(c.Binary) //nolint:noctx // lives until Close
	cmd.Env, cmd.Dir = env, os.TempDir()
	cmd.SysProcAttr = procAttr()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start codebase-memory-mcp: %w", err)
	}
	s := &session{cmd: cmd, peer: jsonrpc.New(stdout, stdin), stdin: stdin, done: make(chan struct{})}
	go s.peer.Serve()
	go func() { _ = cmd.Wait(); close(s.done) }()
	init := map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "boundedcode", "version": "0"}}
	ictx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	if err := s.peer.Call(ictx, "initialize", init, nil); err != nil {
		s.close()
		return fmt.Errorf("mcp initialize: %w", err)
	}
	if err := s.peer.Notify("notifications/initialized", map[string]any{}); err != nil {
		s.close()
		return err
	}
	c.sess = s
	return nil
}

// Close ends the persistent session, if any.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sess != nil {
		c.sess.close()
		c.sess = nil
	}
	return nil
}

// drop discards s if it is still the client's session.
func (c *Client) drop(s *session) {
	c.mu.Lock()
	if c.sess == s {
		c.sess = nil
	}
	c.mu.Unlock()
	s.close()
}

// startTimeout bounds the MCP handshake; closeGrace is how long close waits
// for the server to exit on stdin EOF before killing its process group.
var (
	startTimeout = time.Minute
	closeGrace   = 3 * time.Second
)

// close ends the session; it never blocks longer than closeGrace plus the
// time the kernel takes to reap a killed process. Safe to call twice.
func (s *session) close() {
	s.once.Do(func() {
		_ = s.stdin.Close()
		select {
		case <-s.done:
		case <-time.After(closeGrace):
			killGroup(s.cmd.Process.Pid)
			<-s.done
		}
		// The killed group no longer holds stdout, so the reader ends too.
		select {
		case <-s.peer.Done():
		case <-time.After(closeGrace):
		}
	})
}

// dead reports whether err means the server process or its pipes are gone.
func (s *session) dead(err error) bool {
	select {
	case <-s.done:
		return true
	case <-s.peer.Done():
		return true
	default:
	}
	return errors.Is(err, jsonrpc.ErrClosed) || errors.Is(err, os.ErrClosed) || errors.Is(err, io.ErrClosedPipe) || strings.Contains(err.Error(), "broken pipe")
}

type toolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

func (s *session) call(ctx context.Context, tool string, args map[string]any) ([]byte, error) {
	var res toolResult
	if err := s.peer.Call(ctx, "tools/call", map[string]any{"name": tool, "arguments": args}, &res); err != nil {
		return nil, fmt.Errorf("codebase-memory-mcp %s (session): %w", tool, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	if res.IsError {
		return nil, fmt.Errorf("codebase-memory-mcp %s: %s", tool, lastN(b.String(), 800))
	}
	return []byte(strings.TrimSpace(b.String())), nil
}

// decodeJSON is a helper for tools whose text payload is JSON.
func decodeJSON(b []byte, v any) error { return json.Unmarshal(b, v) }
