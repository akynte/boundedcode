package frontier

import (
	"bytes"
	"context"
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
	if err != nil {
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
	cmd := exec.CommandContext(ctx, c.Binary, args...)
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
