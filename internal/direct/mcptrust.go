package direct

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// MCP servers and their tool definitions are untrusted until the user
// trusts them.
//
//   - A server a project defines (.mcp.json, .boundedcode/mcp.json) runs a
//     command from the repository: it is started only once the user
//     approved that exact definition (command, arguments, environment,
//     directory) for that project. A changed definition is asked again.
//     The user's own configuration needs no approval.
//   - Each tool's definition (name, description, input schema) is pinned
//     when first approved or first seen. A tool whose definition later
//     changes, or a new tool, is withheld from the model until the user
//     approves it: a description is text the model reads as guidance, so
//     a server changing it is a change in what instructs the agent.
//
// Servers are listed once, when the session first needs their tools;
// notifications/tools/list_changed during a session are not acted on, so
// definitions cannot change under a running session.

// MCPTrust is the store of trusted project servers and pinned tool
// definitions, a JSON file in BoundedCode's data directory.
type MCPTrust struct {
	Path string
	mu   sync.Mutex
}

type mcpTrustDoc struct {
	// Servers maps project root + server name to the approved definition's
	// digest.
	Servers map[string]string `json:"servers"`
	// Pins maps a server's identity to its tools' definition digests.
	Pins map[string]map[string]string `json:"pins"`
}

func (s *MCPTrust) load() mcpTrustDoc {
	doc := mcpTrustDoc{}
	if b, err := os.ReadFile(s.Path); err == nil {
		_ = json.Unmarshal(b, &doc)
	}
	if doc.Servers == nil {
		doc.Servers = map[string]string{}
	}
	if doc.Pins == nil {
		doc.Pins = map[string]map[string]string{}
	}
	return doc
}

func (s *MCPTrust) save(doc mcpTrustDoc) error {
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

func (s *MCPTrust) update(f func(*mcpTrustDoc)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc := s.load()
	f(&doc)
	return s.save(doc)
}

func (s *MCPTrust) get() mcpTrustDoc {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

// definitionDigest identifies a server's definition.
func (srv MCPServer) definitionDigest() string {
	keys := make([]string, 0, len(srv.Env))
	for k := range srv.Env {
		keys = append(keys, k+"="+srv.Env[k])
	}
	sort.Strings(keys)
	b, _ := json.Marshal([]any{srv.Command, srv.Args, keys, srv.Cwd})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (srv MCPServer) trustKey() string { return srv.Project + "\x00" + srv.Name }

// pinKey identifies a server for pinning: where it is defined and what it
// runs (a different command is a different server).
func (srv MCPServer) pinKey() string {
	scope := srv.Project
	if scope == "" {
		scope = "user"
	}
	return scope + "\x00" + srv.Name + "\x00" + srv.definitionDigest()[:16]
}

func toolDigest(t mcpTool) string {
	b, _ := json.Marshal(t.InputSchema) // map keys are sorted
	sum := sha256.Sum256([]byte(t.Name + "\x00" + t.Description + "\x00" + string(b)))
	return hex.EncodeToString(sum[:])
}

// trustServer reports whether a server may be started.
func (r *Runner) trustServer(ctx context.Context, srv MCPServer) (bool, string) {
	if srv.Project == "" {
		return true, ""
	}
	store := r.Opts.MCPTrust
	digest := srv.definitionDigest()
	if store != nil && store.get().Servers[srv.trustKey()] == digest {
		return true, ""
	}
	if r.Approve == nil {
		return false, "it is defined by the project and has not been trusted; start an interactive session to approve it, or add it to your own configuration"
	}
	env := make([]string, 0, len(srv.Env))
	for k, v := range srv.Env {
		env = append(env, k+"="+v)
	}
	sort.Strings(env)
	detail := fmt.Sprintf("The project defines an MCP server %q. Starting it runs this command on your machine, with your permissions:\n\n  %s %s\n  in %s\n  env: %s\n\nRepository files are untrusted: approve only if you trust this repository's author.",
		srv.Name, srv.Command, strings.Join(srv.Args, " "), srv.Cwd, strings.Join(env, " "))
	if !r.Approve(ctx, ApprovalRequest{Tool: "mcp", Summary: "Start the project's MCP server " + srv.Name + "?", Detail: detail}) {
		return false, "you did not approve starting it"
	}
	if store != nil {
		if err := store.update(func(d *mcpTrustDoc) { d.Servers[srv.trustKey()] = digest }); err != nil {
			r.emit(Event{Kind: EventNote, Text: "could not record MCP trust: " + err.Error()})
		}
	}
	return true, ""
}

// pinTools returns the tools whose definitions are pinned or approved now;
// the others are withheld, with a note.
func (r *Runner) pinTools(ctx context.Context, srv MCPServer, tools []mcpTool) []mcpTool {
	store := r.Opts.MCPTrust
	if store == nil {
		return tools
	}
	key := srv.pinKey()
	pins, known := store.get().Pins[key]
	current := map[string]string{}
	var changed, added []mcpTool
	for _, t := range tools {
		d := toolDigest(t)
		current[t.Name] = d
		switch old, ok := pins[t.Name]; {
		case !known:
		case !ok:
			added = append(added, t)
		case old != d:
			changed = append(changed, t)
		}
	}
	record := func(names map[string]string) {
		if err := store.update(func(doc *mcpTrustDoc) { doc.Pins[key] = names }); err != nil {
			r.emit(Event{Kind: EventNote, Text: "could not record MCP pins: " + err.Error()})
		}
	}
	if !known {
		// First use: a project server was just approved by the user, a
		// user server is the user's own configuration.
		record(current)
		return tools
	}
	if len(changed)+len(added) == 0 {
		record(current) // removed tools drop out
		return tools
	}
	var b strings.Builder
	for _, t := range changed {
		fmt.Fprintf(&b, "  changed: %s — %s\n", t.Name, oneLine(t.Description, 300))
	}
	for _, t := range added {
		fmt.Fprintf(&b, "  new:     %s — %s\n", t.Name, oneLine(t.Description, 300))
	}
	approved := r.Approve != nil && r.Approve(ctx, ApprovalRequest{Tool: "mcp",
		Summary: "MCP server " + srv.Name + " changed its tools",
		Detail:  "The MCP server " + srv.Name + " now defines tools differently from when you last approved it:\n\n" + b.String() + "\nTool descriptions are read by the model as guidance. Offer the changed tools?"})
	if approved {
		record(current)
		return tools
	}
	withheld := map[string]bool{}
	var names []string
	for _, t := range append(changed, added...) {
		withheld[t.Name] = true
		names = append(names, t.Name)
	}
	r.emit(Event{Kind: EventNote, Text: fmt.Sprintf("MCP server %s: withheld %d changed or new tool(s) (%s) until approved", srv.Name, len(names), strings.Join(names, ", "))})
	r.emit(Event{Kind: EventGuard, Tool: "mcp", Summary: "mcp-definitions " + srv.Name,
		Args: map[string]any{"category": "mcp-definitions", "subject": srv.Name, "decision": "refused", "by": map[bool]string{true: "not approved", false: "no approver"}[r.Approve != nil]}})
	var out []mcpTool
	for _, t := range tools {
		if !withheld[t.Name] {
			out = append(out, t)
		}
	}
	return out
}
