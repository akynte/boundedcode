package direct

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/akynte/boundedcode/internal/jsonrpc"
)

// MCPServer is a Model Context Protocol server started over stdio whose
// tools are offered to the model as mcp__<name>__<tool>.
type MCPServer struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string
	Cwd     string
	Timeout time.Duration // per call (0 = 60 s)
	// Project is the project root when a project file defines the server
	// ("" for the user's configuration): it then needs trust (mcptrust.go).
	Project string
}

type mcpClient struct {
	srv   MCPServer
	cmd   *exec.Cmd
	peer  *jsonrpc.Peer
	stdin io.WriteCloser
	tools []mcpTool
	err   error
	mu    sync.Mutex
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// mcpTools starts the configured servers once and returns their tools.
func (r *Runner) mcpTools() []Tool {
	r.mcpOnce.Do(func() {
		ctx := context.Background()
		for _, s := range r.Opts.MCP {
			c := &mcpClient{srv: s}
			if ok, why := r.trustServer(ctx, s); !ok {
				c.err = fmt.Errorf("not started: %s", why)
			} else {
				c.start()
			}
			if c.err == nil {
				c.tools = r.pinTools(ctx, s, c.tools)
			}
			r.mcp = append(r.mcp, c)
			if c.err != nil {
				r.emit(Event{Kind: EventNote, Text: fmt.Sprintf("MCP server %s unavailable: %v", s.Name, c.err)})
			}
		}
	})
	var out []Tool
	for _, c := range r.mcp {
		if c.err != nil {
			continue
		}
		for _, t := range c.tools {
			out = append(out, c.tool(t))
		}
	}
	return out
}

// MCPStatus reports each server and its tool count.
func (r *Runner) MCPStatus() []string {
	r.mcpTools()
	var out []string
	for _, c := range r.mcp {
		if c.err != nil {
			out = append(out, fmt.Sprintf("%s: unavailable (%v)", c.srv.Name, c.err))
			continue
		}
		names := make([]string, 0, len(c.tools))
		for _, t := range c.tools {
			names = append(names, t.Name)
		}
		out = append(out, fmt.Sprintf("%s: %d tool(s): %s", c.srv.Name, len(c.tools), strings.Join(names, ", ")))
	}
	return out
}

func (c *mcpClient) timeout() time.Duration {
	if c.srv.Timeout > 0 {
		return c.srv.Timeout
	}
	return 60 * time.Second
}

func (c *mcpClient) start() {
	if strings.TrimSpace(c.srv.Command) == "" {
		c.err = fmt.Errorf("no command")
		return
	}
	cmd := exec.CommandContext(context.Background(), c.srv.Command, c.srv.Args...)
	if c.srv.Cwd != "" {
		cmd.Dir = c.srv.Cwd
	}
	cmd.Env = os.Environ()
	for k, v := range c.srv.Env {
		cmd.Env = append(cmd.Env, k+"="+os.ExpandEnv(v))
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		c.err = err
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		c.err = err
		return
	}
	cmd.Stderr = io.Discard
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		c.err = err
		return
	}
	c.cmd, c.stdin = cmd, stdin
	c.peer = jsonrpc.New(stdout, stdin)
	go c.peer.Serve()
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout())
	defer cancel()
	var initRes map[string]any
	if err := c.peer.Call(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "boundedcode", "version": "direct"},
	}, &initRes); err != nil {
		c.err = fmt.Errorf("initialize: %w", err)
		c.close()
		return
	}
	_ = c.peer.Notify("notifications/initialized", map[string]any{})
	var list struct {
		Tools []mcpTool `json:"tools"`
	}
	if err := c.peer.Call(ctx, "tools/list", map[string]any{}, &list); err != nil {
		c.err = fmt.Errorf("tools/list: %w", err)
		c.close()
		return
	}
	c.tools = list.Tools
}

func (c *mcpClient) close() {
	if c.cmd == nil || c.cmd.Process == nil {
		return
	}
	_ = c.stdin.Close()
	done := make(chan struct{})
	go func() { _ = c.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = c.cmd.Process.Kill()
	}
	c.cmd = nil
}

func (c *mcpClient) tool(t mcpTool) Tool {
	name := "mcp__" + sanitize(c.srv.Name) + "__" + sanitize(t.Name)
	sch := t.InputSchema
	if sch == nil {
		sch = schema(map[string]any{})
	}
	desc := t.Description
	if desc == "" {
		desc = "Tool " + t.Name + " of the MCP server " + c.srv.Name
	}
	return Tool{
		Name:        name,
		Description: "[" + c.srv.Name + "] " + desc,
		Schema:      sch,
		Mutates:     false,
		Summary:     func(a map[string]any) string { return c.srv.Name + "/" + t.Name },
		Consent: func(r *Runner, a map[string]any) (string, bool) {
			if r.Opts.Approval == ApprovalAuto {
				return "", false
			}
			b, _ := json.MarshalIndent(a, "", "  ")
			return fmt.Sprintf("Call %s on the MCP server %s with:\n\n%s", t.Name, c.srv.Name, oneLine(string(b), 1500)), true
		},
		Run: func(ctx context.Context, r *Runner, a map[string]any) (string, error) {
			c.mu.Lock()
			defer c.mu.Unlock()
			cctx, cancel := context.WithTimeout(ctx, c.timeout())
			defer cancel()
			var res struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
					Data string `json:"data"`
					Mime string `json:"mimeType"`
				} `json:"content"`
				IsError bool `json:"isError"`
			}
			if err := c.peer.Call(cctx, "tools/call", map[string]any{"name": t.Name, "arguments": a}, &res); err != nil {
				return "", err
			}
			var b strings.Builder
			for _, p := range res.Content {
				switch p.Type {
				case "text":
					b.WriteString(p.Text + "\n")
				case "image":
					fmt.Fprintf(&b, "[image %s, %d bytes]\n", p.Mime, len(p.Data))
				default:
					fmt.Fprintf(&b, "[%s content]\n", p.Type)
				}
			}
			out := strings.TrimRight(b.String(), "\n")
			if res.IsError {
				return out, fmt.Errorf("the tool reported an error")
			}
			if out == "" {
				out = "(no content)"
			}
			return out, nil
		},
	}
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, s)
}
