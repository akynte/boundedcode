// Package cbm implements repointel.Intelligence with codebase-memory-mcp:
// a persistent MCP stdio session when one is open, otherwise the one-shot
// CLI (`codebase-memory-mcp cli <tool> '<json args>'`), which is also the
// fallback when the session dies (ADR-0006).
package cbm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/akynte/boundedcode/internal/buildinfo"
	"github.com/akynte/boundedcode/internal/repointel"
)

// RequiredVersion is the codebase-memory-mcp release the integration is
// built and tested against (the pin in scripts/install-deps.sh).
const RequiredVersion = "0.11.0"

var versionRE = regexp.MustCompile(`\b(\d+\.\d+\.\d+)\b`)

// CheckVersion runs `binary --version` and fails unless it reports
// RequiredVersion. got is the reported version when one was parsed.
func CheckVersion(ctx context.Context, binary string) (got string, err error) {
	path, err := exec.LookPath(binary)
	if err != nil {
		return "", fmt.Errorf("codebase-memory-mcp is not installed: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Dir = os.TempDir()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s --version: %w: %s", path, err, lastN(strings.TrimSpace(string(out)), 200))
	}
	m := versionRE.FindStringSubmatch(string(out))
	if m == nil {
		return "", fmt.Errorf("%s --version: unrecognized output %q", path, lastN(strings.TrimSpace(string(out)), 200))
	}
	if m[1] != RequiredVersion {
		return m[1], fmt.Errorf("codebase-memory-mcp %s at %s is not the supported %s", m[1], path, RequiredVersion)
	}
	return m[1], nil
}

// Preflight is CheckVersion as one actionable message: the problem and the
// setup step that installs the pinned release. Commands check once, up
// front, instead of failing on every call.
func Preflight(ctx context.Context, binary string) error {
	if _, err := CheckVersion(ctx, binary); err != nil {
		return fmt.Errorf("%w; run `%s setup --only tools`", err, buildinfo.Command())
	}
	return nil
}

// Client invokes the codebase-memory-mcp binary.
type Client struct {
	Binary string
	// CacheDir isolates our graph store from any other codebase-memory-mcp
	// use on the machine (CBM_CACHE_DIR).
	CacheDir string
	// Timeout bounds every call, over the session or the CLI (default 30m:
	// indexing a large repository is the slowest call).
	Timeout time.Duration

	mu   sync.Mutex
	sess *session
}

var _ repointel.Intelligence = (*Client)(nil)

// Name implements repointel.Intelligence.
func (c *Client) Name() string { return "codebase-memory-mcp" }

func (c *Client) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return 30 * time.Minute
}

// Call runs one tool with JSON args: over the persistent MCP session when
// one is open, otherwise as a one-shot CLI process. A session that died is
// dropped and the call is retried with the CLI; one that stopped answering
// within Timeout is killed and the call fails.
func (c *Client) Call(ctx context.Context, tool string, args map[string]any) ([]byte, error) {
	c.mu.Lock()
	s := c.sess
	c.mu.Unlock()
	if s != nil {
		cctx, cancel := context.WithTimeout(ctx, c.timeout())
		b, err := s.call(cctx, tool, args)
		cancel()
		switch {
		case err == nil || ctx.Err() != nil:
			return b, err
		case s.dead(err):
			c.drop(s)
			// fall through to the CLI
		case errors.Is(err, context.DeadlineExceeded):
			c.drop(s)
			return nil, fmt.Errorf("codebase-memory-mcp %s: no answer within %s; session closed: %w", tool, c.timeout(), err)
		default:
			return nil, err
		}
	}
	if err := c.configure(ctx); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Binary, "cli", "--quiet", tool, string(raw))
	env, err := c.env()
	if err != nil {
		return nil, err
	}
	cmd.Env = env
	// Run from a neutral directory: some tools infer context from cwd.
	cmd.Dir = os.TempDir()
	cmd.WaitDelay = 5 * time.Second // don't wait on pipes a killed child's children hold
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("codebase-memory-mcp %s: %w: %s", tool, err, strings.TrimSpace(lastN(errb.String(), 800)))
	}
	b := bytes.TrimSpace(out.Bytes())
	if bytes.HasPrefix(b, []byte(`{"error"`)) {
		return nil, fmt.Errorf("codebase-memory-mcp %s: %s", tool, lastN(string(b), 800))
	}
	return b, nil
}

func (c *Client) text(ctx context.Context, tool string, args map[string]any) (string, error) {
	b, err := c.Call(ctx, tool, args)
	return string(b), err
}

// Index implements repointel.Intelligence.
func (c *Client) Index(ctx context.Context, repoPath, name, mode string) (repointel.IndexResult, error) {
	if mode == "" {
		mode = "full"
	}
	args := map[string]any{"repo_path": repoPath, "mode": mode}
	if name != "" {
		args["name"] = name
	}
	t0 := time.Now()
	b, err := c.Call(ctx, "index_repository", args)
	var res repointel.IndexResult
	if err != nil {
		return res, err
	}
	if err := decodeJSON(b, &res); err != nil {
		return res, fmt.Errorf("decode index result: %w: %s", err, lastN(string(b), 300))
	}
	res.Seconds = time.Since(t0).Seconds()
	return res, nil
}

// Search implements repointel.Intelligence.
func (c *Client) Search(ctx context.Context, project, query string, limit int) (string, error) {
	return c.text(ctx, "search_graph", map[string]any{"project": project, "query": query, "limit": orInt(limit, 20), "max_output_tokens": 2000})
}

// Trace implements repointel.Intelligence.
func (c *Client) Trace(ctx context.Context, project, function, direction string, depth int) (string, error) {
	if direction == "" {
		direction = "both"
	}
	return c.text(ctx, "trace_path", map[string]any{"project": project, "function_name": function, "direction": direction, "depth": orInt(depth, 2), "max_output_tokens": 2000})
}

// Snippet implements repointel.Intelligence.
func (c *Client) Snippet(ctx context.Context, project, name string) (string, error) {
	return c.text(ctx, "get_code_snippet", map[string]any{"project": project, "qualified_name": name, "max_output_tokens": 2500})
}

// Impact implements repointel.Intelligence.
func (c *Client) Impact(ctx context.Context, project, baseBranch string, depth int) (string, error) {
	args := map[string]any{"project": project, "scope": "impact", "depth": orInt(depth, 2), "max_output_tokens": 3000}
	if baseBranch != "" {
		args["base_branch"] = baseBranch
	}
	return c.text(ctx, "detect_changes", args)
}

// Architecture implements repointel.Intelligence.
func (c *Client) Architecture(ctx context.Context, project string) (string, error) {
	return c.text(ctx, "get_architecture", map[string]any{"project": project, "aspects": []string{"overview"}})
}

func orInt(v, d int) int {
	if v <= 0 {
		return d
	}
	return v
}

func lastN(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
