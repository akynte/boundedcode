package frontier

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Provider answers an escalation packet.
type Provider interface {
	Name() string
	// Ask sends the packet and returns the advice text. dir is a scratch
	// directory the provider may use (never a repository).
	Ask(ctx context.Context, packet, dir string) (string, error)
}

// ErrPending means the answer will arrive out of band (manual provider).
var ErrPending = errors.New("frontier answer pending")

// Codex uses the Codex CLI with the user's ChatGPT sign-in. It never needs an
// API key and never sees the repositories: it runs read-only in an empty
// directory and receives only the packet on stdin.
type Codex struct {
	Binary  string
	Model   string
	Timeout time.Duration
	// Container, when set, runs codex inside a container so that commands
	// codex's own agent executes cannot read the host filesystem: only an
	// empty workdir and the codex credential dir are mounted. (codex's
	// read-only sandbox restricts writes, not reads.)
	Container *CodexContainer
}

// CodexContainer configures containment for codex exec.
type CodexContainer struct {
	Engine string // docker | podman
	Image  string // any image with CA certificates (the agent sandbox image works)
	UID    int
	GID    int
	// LinuxBinary is a Linux build of codex to mount instead of the host's
	// (macOS and Windows hosts: their codex cannot run in a Linux
	// container). "" mounts the host binary (Linux hosts).
	LinuxBinary string
}

// Name implements Provider.
func (c *Codex) Name() string { return "codex" }

// LoginStatus reports the Codex auth mode, e.g. "Logged in using ChatGPT".
func (c *Codex) LoginStatus(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, c.Binary, "login", "status").CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// Ask implements Provider.
func (c *Codex) Ask(ctx context.Context, packet, dir string) (string, error) {
	st, err := c.LoginStatus(ctx)
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return "", fmt.Errorf("codex CLI is not installed (%w): install it (https://github.com/openai/codex), run `codex login`, "+
			"or set frontier.provider: manual", err)
	case err != nil && st == "":
		return "", fmt.Errorf("codex login status: %w: run `codex login`", err)
	case err != nil:
		return "", fmt.Errorf("codex not logged in (%s): run `codex login`", st)
	}
	if strings.Contains(strings.ToLower(st), "api key") {
		// Policy: never silently spend API credits. Subscription sign-in only.
		return "", fmt.Errorf("codex is authenticated with an API key (%q); boundedcode only uses ChatGPT subscription sign-in", st)
	}
	empty := filepath.Join(dir, "codex-cwd")
	if err := os.MkdirAll(empty, 0o700); err != nil {
		return "", err
	}
	out := filepath.Join(dir, "codex-last-message.md")
	args := []string{"exec", "--sandbox", "read-only", "--skip-git-repo-check", "--ephemeral", "-C", empty, "-o", out}
	if c.Model != "" {
		args = append(args, "-m", c.Model)
	}
	args = append(args, "-")
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 15 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var cmd *exec.Cmd
	if c.Container != nil {
		cmd, err = c.containerCmd(ctx, args, dir)
		if err != nil {
			return "", err
		}
	} else {
		cmd = exec.CommandContext(ctx, c.Binary, args...)
	}
	cmd.Stdin = strings.NewReader(packet)
	// Drop API-key variables so codex cannot fall back to metered API auth.
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if k == "OPENAI_API_KEY" || k == "CODEX_API_KEY" {
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("codex exec: %w: %s", err, tailStr(stderr.String(), 1500))
	}
	b, err := os.ReadFile(out)
	if err != nil {
		return "", fmt.Errorf("codex produced no answer: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

// Manual writes the packet for the user to paste into any frontier chat and
// expects the answer via `boundedcode frontier answer`.
type Manual struct{}

// Name implements Provider.
func (Manual) Name() string { return "manual" }

// Ask implements Provider.
func (Manual) Ask(context.Context, string, string) (string, error) { return "", ErrPending }

func tailStr(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// containerCmd wraps `codex <args>` in a container. Paths in args (the
// empty cwd and the output file) live under dir, which is mounted at the
// same path. CODEX_HOME is mounted read-write for token refresh.
func (c *Codex) containerCmd(ctx context.Context, args []string, dir string) (*exec.Cmd, error) {
	bin := c.Container.LinuxBinary
	if bin == "" {
		var err error
		if bin, err = exec.LookPath(c.Binary); err != nil {
			return nil, err
		}
		if bin, err = filepath.EvalSymlinks(bin); err != nil {
			return nil, err
		}
	} else if _, err := os.Stat(bin); err != nil {
		return nil, fmt.Errorf("the Linux codex build for the frontier container is missing (%s): run `setup --only frontier`", bin)
	}
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		codexHome = filepath.Join(h, ".codex")
	}
	cc := c.Container
	// A named container is removed on cancellation; killing only the engine
	// CLI would leave it running.
	var rnd [6]byte
	_, _ = rand.Read(rnd[:])
	name := "bc-codex-" + hex.EncodeToString(rnd[:])
	run := []string{"run", "--rm", "-i", "--init", "--name", name,
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--user", fmt.Sprintf("%d:%d", cc.UID, cc.GID),
		"--tmpfs", "/home/agent:rw,exec,size=256m,mode=1777", "-e", "HOME=/home/agent",
		"--mount", fmt.Sprintf("type=bind,source=%s,target=/usr/local/bin/codex-host,readonly", bin),
		"--mount", fmt.Sprintf("type=bind,source=%s,target=/home/agent/.codex", codexHome),
		"--mount", fmt.Sprintf("type=bind,source=%s,target=%s", dir, dir),
		"-e", "CODEX_HOME=/home/agent/.codex",
		"-w", dir,
		cc.Image, "/usr/local/bin/codex-host"}
	cmd := exec.CommandContext(ctx, cc.Engine, append(run, args...)...)
	cmd.Cancel = func() error {
		rctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = exec.CommandContext(rctx, cc.Engine, "rm", "-f", name).Run()
		return cmd.Process.Kill()
	}
	cmd.WaitDelay = 10 * time.Second
	return cmd, nil
}
