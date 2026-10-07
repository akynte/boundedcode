package serena

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/akynte/boundedcode/internal/jsonrpc"
	"github.com/akynte/boundedcode/internal/sandbox"
)

// session is one Serena MCP server over stdio, bound to one project root.
type session struct {
	root    string
	tag     string // instanceEnv value
	cmd     *exec.Cmd
	peer    *jsonrpc.Peer
	stdin   io.WriteCloser
	logf    *os.File
	done    chan struct{} // closed when the process has exited
	started time.Time

	mu       sync.Mutex
	lastUsed time.Time
	inUse    int
	closed   bool
}

type startSpec struct {
	Executable string
	Layout     layout
	Project    Project
	LogPath    string
	// Timeout bounds process start plus MCP initialization.
	Timeout time.Duration
	// Env is extra environment (tests).
	Env []string
	Tag string
}

// env builds the server environment: the scrubbed host environment (no
// credentials), a private SERENA_HOME, no usage reporting, and no network
// for the language servers' toolchains.
func (s startSpec) env() []string {
	env := sandbox.ScrubbedEnv()
	env = append(env,
		"SERENA_HOME="+s.Layout.Home,
		instanceEnv+"="+s.Tag,
		"SERENA_USAGE_REPORTING=false", // Serena otherwise pings oraios-software.de on start
		"PYTHONDONTWRITEBYTECODE=1",
		"PYTHONUNBUFFERED=1",
		"GOTOOLCHAIN=local", // never download a toolchain named by go.mod
		"GOPROXY=off",       // gopls resolves modules from the local cache only
		// GOFLAGS is left to Go's default (-mod=readonly, or vendor), so go
		// list never rewrites go.mod/go.sum in the worktree.
		"npm_config_offline=true", // language servers are pre-installed by `serena setup`
	)
	return append(env, s.Env...)
}

func start(ctx context.Context, spec startSpec) (*session, error) {
	args := []string{"start-mcp-server",
		"--project", spec.Project.Root,
		"--context", spec.Layout.ContextFile,
		"--transport", "stdio", // no listener: nothing is reachable from the network
		"--enable-web-dashboard", "false", "--open-web-dashboard", "false",
		"--enable-gui-log-window", "false",
	}
	cmd := exec.Command(spec.Executable, args...) //nolint:noctx // lives until close
	cmd.Env = spec.env()
	cmd.Dir = spec.Project.Root
	// Own process group: stopping the instance kills Serena and the language
	// servers it spawned. Pdeathsig ends Serena if the control plane dies.
	cmd.SysProcAttr = procAttr()
	cmd.WaitDelay = 5 * time.Second
	if err := os.MkdirAll(filepath.Dir(spec.LogPath), 0o700); err != nil {
		return nil, err
	}
	logf, err := os.OpenFile(spec.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(logf, "\n=== %s start %s ===\n", time.Now().UTC().Format(time.RFC3339), spec.Project.Root)
	cmd.Stderr = logf
	stdin, err := cmd.StdinPipe()
	if err != nil {
		logf.Close()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logf.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		logf.Close()
		return nil, fmt.Errorf("start serena: %w", err)
	}
	s := &session{root: spec.Project.Root, tag: spec.Tag, cmd: cmd, peer: jsonrpc.New(stdout, stdin), stdin: stdin, logf: logf,
		done: make(chan struct{}), started: time.Now(), lastUsed: time.Now()}
	go s.peer.Serve()
	go func() { _ = cmd.Wait(); close(s.done) }()

	ictx, cancel := context.WithTimeout(ctx, spec.Timeout)
	defer cancel()
	init := map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "boundedcode", "version": "0"}}
	if err := s.peer.Call(ictx, "initialize", init, nil); err != nil {
		s.kill()
		return nil, fmt.Errorf("serena mcp initialize: %w%s", err, s.exitDetail())
	}
	if err := s.peer.Notify("notifications/initialized", map[string]any{}); err != nil {
		s.kill()
		return nil, err
	}
	// Positive check that this instance serves the expected project and
	// that its language server came up.
	if err := s.verifyProject(ictx, spec.Layout.ProjectName); err != nil {
		s.kill()
		return nil, err
	}
	return s, nil
}

var activeProjectRE = regexp.MustCompile(`(?m)^Active project: (.+)$`)

func (s *session) verifyProject(ctx context.Context, want string) error {
	out, err := s.call(ctx, "get_current_config", map[string]any{})
	if err != nil {
		return fmt.Errorf("serena get_current_config: %w%s", err, s.exitDetail())
	}
	m := activeProjectRE.FindStringSubmatch(out)
	if m == nil || strings.TrimSpace(m[1]) != want {
		got := "none"
		if m != nil {
			got = strings.TrimSpace(m[1])
		}
		return fmt.Errorf("serena serves project %q, want %q (%s)", got, want, s.root)
	}
	return nil
}

type toolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

// errCall marks a tool that ran and reported an error (as opposed to a
// transport failure, which ends the session).
type errCall struct{ msg string }

func (e *errCall) Error() string { return e.msg }

func (s *session) call(ctx context.Context, tool string, args map[string]any) (string, error) {
	var res toolResult
	err := s.peer.Call(ctx, "tools/call", map[string]any{"name": tool, "arguments": args}, &res)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, c := range res.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	if res.IsError {
		return "", &errCall{msg: fmt.Sprintf("serena %s: %s", tool, lastN(strings.TrimSpace(b.String()), 600))}
	}
	return b.String(), nil
}

// ping is a cheap liveness check that does not touch the language server.
func (s *session) ping(ctx context.Context) error {
	if !s.alive() {
		return errors.New("serena process exited" + s.exitDetail())
	}
	return s.peer.Call(ctx, "tools/list", map[string]any{}, nil)
}

func (s *session) alive() bool {
	select {
	case <-s.done:
		return false
	case <-s.peer.Done():
		return false
	default:
		return true
	}
}

func (s *session) pid() int {
	if s.cmd.Process == nil {
		return 0
	}
	return s.cmd.Process.Pid
}

// close ends the server: EOF on stdin lets Serena shut its language servers
// down; the process group is killed if that takes too long.
func (s *session) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	_ = s.stdin.Close()
	select {
	case <-s.done:
	case <-time.After(3 * time.Second):
	}
	s.killGroup()
	<-s.done
	_ = s.logf.Close()
}

// kill ends the server immediately (hung or failed instance).
func (s *session) kill() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	_ = s.stdin.Close()
	s.killGroup()
	<-s.done
	_ = s.logf.Close()
}

// killGroup kills Serena's process group and every process carrying the
// instance tag, including language servers that run in their own session
// and outlived Serena.
func (s *session) killGroup() {
	if pid := s.pid(); pid > 0 {
		killProcessGroup(pid)
	}
	if s.tag != "" {
		killTagged(s.tag)
	}
}

func (s *session) exitDetail() string {
	select {
	case <-s.done:
		if st := s.cmd.ProcessState; st != nil {
			return " (serena exited: " + st.String() + "; see " + s.logf.Name() + ")"
		}
	default:
	}
	return ""
}
