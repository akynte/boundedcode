// Package cbm implements repointel.Intelligence with the codebase-memory-mcp
// CLI (`codebase-memory-mcp cli <tool> '<json args>'`).
package cbm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/akynte/boundedcode/internal/repointel"
)

// Client invokes the codebase-memory-mcp binary.
type Client struct {
	Binary string
	// CacheDir isolates our graph store from any other codebase-memory-mcp
	// use on the machine (CBM_CACHE_DIR).
	CacheDir string
	// AllowedRoot confines indexing (CBM_ALLOWED_ROOT); empty = unrestricted.
	AllowedRoot string
	Timeout     time.Duration

	mu   sync.Mutex
	sess *session
}

var _ repointel.Intelligence = (*Client)(nil)

// Name implements repointel.Intelligence.
func (c *Client) Name() string { return "codebase-memory-mcp" }

// Call runs one tool with JSON args: over the persistent MCP session when
// one is open, otherwise as a one-shot CLI process.
func (c *Client) Call(ctx context.Context, tool string, args map[string]any) ([]byte, error) {
	c.mu.Lock()
	s := c.sess
	c.mu.Unlock()
	if s != nil {
		return s.call(ctx, tool, args)
	}
	if err := c.configure(ctx); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Binary, "cli", "--quiet", tool, string(raw))
	env, err := c.env()
	if err != nil {
		return nil, err
	}
	cmd.Env = env
	// Run from a neutral directory: some tools infer context from cwd.
	cmd.Dir = os.TempDir()
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

// SearchCode implements repointel.Intelligence.
func (c *Client) SearchCode(ctx context.Context, project, pattern string, limit int) (string, error) {
	return c.text(ctx, "search_code", map[string]any{"project": project, "pattern": pattern, "limit": orInt(limit, 10), "max_output_tokens": 2500})
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
