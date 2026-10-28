package direct

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/policy"
)

// Tool is one function the model may call.
type Tool struct {
	Name        string
	Description string
	// Schema is the JSON schema of the arguments.
	Schema map[string]any
	// Mutates reports that the tool changes files (approval in "ask" mode;
	// "edits" mode allows it inside the project).
	Mutates bool
	// Run executes the call. The string result is what the model sees.
	Run func(ctx context.Context, r *Runner, args map[string]any) (string, error)
	// Summary is a one-line description of a call, for the transcript.
	Summary func(args map[string]any) string
	// Consent returns what the user is asked to approve ("" = no consent
	// needed in the current mode).
	Consent func(r *Runner, args map[string]any) (detail string, needed bool)
	// Precheck applies deterministic policy before anyone is asked; an
	// error refuses the call.
	Precheck func(r *Runner, args map[string]any) error
}

const (
	maxReadBytes   = 256 << 10 // per read call
	maxReadLines   = 2000
	maxToolOutput  = 48 << 10 // tool result kept for the model
	maxGrepMatches = 200
	maxGlobResults = 500
)

func toolSchemas(tools []Tool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, map[string]any{"type": "function", "function": map[string]any{
			"name": t.Name, "description": t.Description, "parameters": t.Schema}})
	}
	return out
}

func (r *Runner) toolList() []Tool {
	if r.tools == nil {
		r.tools = r.buildTools()
	}
	return r.tools
}

// buildTools assembles the tool set for this runner's options.
func (r *Runner) buildTools() []Tool {
	r.loadOnce.Do(r.loadDefinitions)
	var ts []Tool
	if r.Opts.ReadOnly {
		ts = []Tool{readTool(), readManyTool(), bashTool(), grepTool(), globTool(), lsTool()}
	} else {
		ts = append(defaultTools(), multiEditTool(), readManyTool(), notebookReadTool(), notebookEditTool(),
			bashOutputTool(), killShellTool(), todoTool(), enterPlanTool(), exitPlanTool())
	}
	ts = append(ts, webFetchTool(), webSearchTool(), recallTool())
	if r.Asker != nil {
		ts = append(ts, askUserTool())
	}
	if r.Opts.MemoryFile != "" && !r.Opts.ReadOnly {
		ts = append(ts, saveMemoryTool())
	}
	if len(r.skills) > 0 || len(r.Commands(r.Opts.UserDir)) > 0 {
		ts = append(ts, skillTool())
	}
	if !r.Opts.NoSubagents && !r.Opts.ReadOnly && r.Depth < 2 {
		ts = append(ts, taskTool())
	}
	ts = append(ts, findSymbolTool(), findReferencesTool())
	if r.Nav != nil { // implementations need a language server
		ts = append(ts, findImplementationsTool())
	}
	ts = append(ts, r.mcpTools()...)
	if len(r.only) > 0 {
		var kept []Tool
		for _, t := range ts {
			if toolAllowed(r.only, t.Name) {
				kept = append(kept, t)
			}
		}
		ts = kept
	}
	return ts
}

// toolAllowed matches a tool name against a custom agent's tools list,
// ignoring case and underscores so other agents' names (Read, WebFetch,
// TodoWrite) work too; "mcp" allows every MCP tool.
func toolAllowed(only []string, name string) bool {
	norm := func(s string) string { return strings.ReplaceAll(strings.ToLower(s), "_", "") }
	n := norm(name)
	for _, o := range only {
		switch on := norm(o); {
		case on == n, on == "*":
			return true
		case on == "mcp" && strings.HasPrefix(name, "mcp__"):
			return true
		}
	}
	return false
}

// permissionArg is the part of a call that permission rules match:
// the command for bash, the path for file tools, the host for web tools.
func permissionArg(t *Tool, args map[string]any) string {
	switch t.Name {
	case "bash":
		return str(args, "command")
	case "web_fetch":
		return hostOf(str(args, "url"))
	case "web_search":
		return str(args, "query")
	}
	if p := str(args, "path"); p != "" {
		return p
	}
	return ""
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Tools lists the tools this runner offers.
func (r *Runner) Tools() []Tool { return r.toolList() }

func (r *Runner) findTool(name string) *Tool {
	for i := range r.toolList() {
		if r.tools[i].Name == name {
			return &r.tools[i]
		}
	}
	return nil
}

// runTool executes one model tool call with policy and approval applied
// and returns the text result for the model.
func (r *Runner) runTool(ctx context.Context, call ToolCall) string {
	t := r.findTool(call.Function.Name)
	if t == nil {
		r.emit(Event{Kind: EventDenied, Tool: call.Function.Name, CallID: call.ID, Err: "unknown tool"})
		return "error: unknown tool " + call.Function.Name
	}
	args := map[string]any{}
	if a := strings.TrimSpace(call.Function.Arguments); a != "" {
		if err := json.Unmarshal([]byte(a), &args); err != nil {
			r.emit(Event{Kind: EventDenied, Tool: t.Name, CallID: call.ID, Err: "malformed arguments"})
			return "error: arguments are not a JSON object: " + err.Error()
		}
	}
	summary := t.Summary(args)
	if t.Precheck != nil {
		if err := t.Precheck(r, args); err != nil {
			r.emit(Event{Kind: EventDenied, Tool: t.Name, CallID: call.ID, Summary: summary, Err: err.Error()})
			return "error: " + err.Error()
		}
	}
	spec := t.Name + "(" + permissionArg(t, args) + ")"
	if why, denied := r.Opts.Permissions.Denied(spec); denied {
		r.emit(Event{Kind: EventDenied, Tool: t.Name, CallID: call.ID, Summary: summary, Err: why})
		return "error: " + why
	}
	if t.Mutates && r.plan && (t.Name != "bash" || !isSafeCommand(str(args, "command"))) {
		r.emit(Event{Kind: EventDenied, Tool: t.Name, CallID: call.ID, Summary: summary, Err: "plan mode"})
		return "Plan mode is on: no files are changed and no commands other than read-only ones run until the plan is approved. Finish the plan and call exit_plan_mode."
	}
	if t.Mutates {
		rel := ""
		if t.Name != "bash" {
			abs, _, err := r.resolve(str(args, "path"))
			if err == nil {
				rel = r.rootRel(abs)
			}
		} else if isSafeCommand(str(args, "command")) || r.Opts.Mode != ModeChat && (r.Opts.VerifyDeserved == "" || r.directOK) {
			// A safe command is not a change; an unsafe one is, in chat
			// mode or in a turn that deserved verification.
			goto hooks
		}
		if ok, refusal := r.mutating(ctx, t.Name, rel); !ok {
			if r.handoff == nil {
				r.emit(Event{Kind: EventDenied, Tool: t.Name, CallID: call.ID, Summary: summary, Err: refusal})
				return "error: " + refusal
			}
			r.emit(Event{Kind: EventDenied, Tool: t.Name, CallID: call.ID, Summary: summary, Err: "handed off to verification"})
			return "This change is being handed off to a verified task; stop here."
		}
	}
hooks:
	if res := r.runHooks(ctx, HookPreTool, HookInput{Tool: t.Name, Args: args}); res.Block {
		r.emit(Event{Kind: EventDenied, Tool: t.Name, CallID: call.ID, Summary: summary, Err: "blocked by a hook: " + res.Reason})
		return "error: blocked by a hook: " + res.Reason
	}
	ok, approved, refusal := r.guard(ctx, t.Name, summary, r.findings(t, args))
	if !ok {
		r.emit(Event{Kind: EventDenied, Tool: t.Name, CallID: call.ID, Summary: summary, Err: refusal})
		return "error: " + refusal
	}
	if t.Consent != nil && !approved && !r.Opts.Permissions.Allowed(spec) {
		if detail, needed := t.Consent(r, args); needed {
			if r.Approve == nil || !r.Approve(ctx, ApprovalRequest{Tool: t.Name, Summary: summary, Detail: detail}) {
				r.emit(Event{Kind: EventDenied, Tool: t.Name, CallID: call.ID, Summary: summary, Err: "not approved"})
				return "The user did not approve this action. Ask what they would like instead, or try a different approach; do not retry the same action."
			}
		}
	}
	var before []byte // a Go file's content before the change, for the check
	if t.Mutates && t.Name != "bash" {
		if abs, _, err := r.resolve(str(args, "path")); err == nil {
			r.checkpoint(abs)
			if strings.HasSuffix(abs, ".go") {
				before, _ = os.ReadFile(abs)
			}
		}
	}
	r.emit(Event{Kind: EventToolStart, Tool: t.Name, CallID: call.ID, Summary: summary, Args: args})
	start := time.Now()
	out, err := t.Run(ctx, r, args)
	if err == nil && t.Mutates && t.Name != "bash" {
		if abs, rel, rerr := r.resolve(str(args, "path")); rerr == nil {
			r.noteWritten(abs)
			if !r.Opts.NoChecks {
				t0 := time.Now()
				if diag := r.checkFile(ctx, abs, rel, before); diag != "" {
					out += "\n" + diag
				}
				r.phase(PhaseChecks, t0)
			}
			if !r.Opts.NoHistoryNotes {
				t0 := time.Now()
				if note := r.coChangeNote(r.rootRel(abs)); note != "" {
					out += "\n" + note
				}
				r.phase(PhaseHistoryNotes, t0)
			}
		}
	}
	el := time.Since(start) // the tool, its check and history note
	if res := r.runHooks(ctx, HookPostTool, HookInput{Tool: t.Name, Args: args, Result: r.clip(out), Error: errString(err)}); res.Extra != "" {
		out += "\n\n[hook] " + res.Extra
	}
	if err != nil {
		r.emit(Event{Kind: EventToolEnd, Tool: t.Name, CallID: call.ID, Summary: summary, Err: err.Error(), Elapsed: el, Output: out})
		if out == "" {
			return "error: " + err.Error()
		}
		return r.clip(out) + "\nerror: " + err.Error()
	}
	r.emit(Event{Kind: EventToolEnd, Tool: t.Name, CallID: call.ID, Summary: summary, Output: out, Elapsed: el})
	return r.clip(out)
}

// clipOutput applies the package-wide cap; runTool applies the runner's
// (provider-dependent) cap on top.
func clipOutput(s string) string { return clipTo(s, maxToolOutput) }

// cwd is the working directory tools use (Opts.Dir, or the root).
func (r *Runner) cwd() string {
	if r.Opts.Dir != "" {
		return r.Opts.Dir
	}
	return r.Opts.Root
}

// resolve confines a model-supplied path to the project root and returns
// the absolute path and the slash-separated path relative to the working
// directory. Relative paths are resolved against the working directory;
// absolute paths inside the root are accepted; anything else, including
// symlinks that leave the root, is refused.
func (r *Runner) resolve(p string) (abs, rel string, err error) {
	if strings.TrimSpace(p) == "" {
		return "", "", errors.New("path is required")
	}
	root, err := filepath.EvalSymlinks(r.Opts.Root)
	if err != nil {
		root = filepath.Clean(r.Opts.Root)
	}
	cwd, err := filepath.EvalSymlinks(r.cwd())
	if err != nil {
		cwd = filepath.Clean(r.cwd())
	}
	if !filepath.IsAbs(p) {
		abs = filepath.Join(cwd, p)
	} else {
		abs = filepath.Clean(p)
	}
	// Resolve the longest existing prefix so planted symlinks cannot point
	// outside the project.
	real := abs
	rest := ""
	for {
		if ev, err := filepath.EvalSymlinks(real); err == nil {
			real = ev
			break
		}
		parent := filepath.Dir(real)
		if parent == real {
			break
		}
		rest = filepath.Join(filepath.Base(real), rest)
		real = parent
	}
	real = filepath.Join(real, rest)
	relRoot, err := filepath.Rel(root, real)
	if err != nil || relRoot == ".." || strings.HasPrefix(relRoot, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("%s is outside the project (%s)", p, r.Opts.Root)
	}
	relp, err := filepath.Rel(cwd, real)
	if err != nil {
		relp = relRoot
	}
	if relp == "." {
		relp = ""
	}
	return real, filepath.ToSlash(relp), nil
}

// checkReadable applies the secret-path mask and keeps .git closed. The
// path is checked relative to the project root (abs is the resolved file).
func (r *Runner) checkReadable(abs string) error {
	root, err := filepath.EvalSymlinks(r.Opts.Root)
	if err != nil {
		root = filepath.Clean(r.Opts.Root)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." {
		return nil //nolint:nilerr // outside the root or the root itself: nothing to mask
	}
	rel = filepath.ToSlash(rel)
	if policy.IsSecretPath(rel) {
		return fmt.Errorf("%s looks like a secret store and is not readable by the agent", rel)
	}
	if rel == ".git" || strings.HasPrefix(rel, ".git/") {
		return errors.New("the .git directory is not readable through tools; use git commands")
	}
	return nil
}

func str(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func num(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
			return 0
		}
		return n
	}
	return 0
}

func boolean(args map[string]any, key string) bool {
	switch v := args[key].(type) {
	case bool:
		return v
	case string:
		return v == "true"
	}
	return false
}

func schema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func prop(typ, desc string) map[string]any { return map[string]any{"type": typ, "description": desc} }

func defaultTools() []Tool {
	return []Tool{readTool(), writeTool(), editTool(), bashTool(), grepTool(), globTool(), lsTool()}
}

// ---- read

func readTool() Tool {
	return Tool{
		Name:        "read",
		Description: "Read a file in the project. Returns numbered lines. A file over 400 lines returns an outline of its definitions unless offset or limit is given; then read the region you need.",
		Schema: schema(map[string]any{
			"path":   prop("string", "File path, relative to the project root"),
			"offset": prop("integer", "First line to return (1-based, default 1)"),
			"limit":  prop("integer", "Maximum lines to return (default 2000)"),
		}, "path"),
		Summary: func(a map[string]any) string { return "read " + str(a, "path") },
		Run: func(ctx context.Context, r *Runner, a map[string]any) (string, error) {
			abs, rel, err := r.resolve(str(a, "path"))
			if err != nil {
				return "", err
			}
			if err := r.checkReadable(abs); err != nil {
				return "", err
			}
			st, err := os.Stat(abs)
			if err != nil {
				return "", err
			}
			if st.IsDir() {
				return "", fmt.Errorf("%s is a directory; use ls", rel)
			}
			f, err := os.Open(abs)
			if err != nil {
				return "", err
			}
			defer f.Close()
			if mime := imageMime(abs); mime != "" {
				if st.Size() > maxImageBytes {
					return fmt.Sprintf("%s is an image of %d bytes, above the %d-byte limit", rel, st.Size(), maxImageBytes), nil
				}
				data, err := os.ReadFile(abs)
				if err != nil {
					return "", err
				}
				r.attachImage(rel, mime, data)
				return fmt.Sprintf("[image %s (%s, %d bytes) attached to the conversation; look at it in the next message]", rel, mime, len(data)), nil
			}
			if strings.HasSuffix(strings.ToLower(abs), ".ipynb") {
				return notebookText(abs, rel)
			}
			buf := make([]byte, min(st.Size(), 4<<20))
			n, _ := f.Read(buf)
			buf = buf[:n]
			if bytes.IndexByte(buf[:min(n, 8000)], 0) >= 0 {
				return fmt.Sprintf("%s is a binary file (%d bytes)", rel, st.Size()), nil
			}
			offset := max(num(a, "offset"), 1)
			limit := num(a, "limit")
			if limit <= 0 || limit > maxReadLines {
				limit = maxReadLines
			}
			lines := strings.Split(string(buf), "\n")
			if n == len(buf) && int64(n) < st.Size() {
				lines = lines[:len(lines)-1]
			}
			total := len(lines)
			if total > 0 && lines[total-1] == "" && strings.HasSuffix(string(buf), "\n") {
				total--
			}
			if offset > total {
				return fmt.Sprintf("(file has %d lines; offset %d is past the end)", total, offset), nil
			}
			if num(a, "offset") <= 0 && num(a, "limit") <= 0 && total > outlineLines {
				if o, ok := outline(rel, lines[:total]); ok {
					return o, nil
				}
			}
			end := min(total, offset-1+limit)
			var b strings.Builder
			size := 0
			for i := offset - 1; i < end; i++ {
				l := lines[i]
				if len(l) > 2000 {
					l = l[:2000] + "…"
				}
				fmt.Fprintf(&b, "%6d\t%s\n", i+1, l)
				size += len(l)
				if size > maxReadBytes {
					end = i + 1
					break
				}
			}
			if end < total {
				fmt.Fprintf(&b, "… (%d more lines; use offset %d)\n", total-end, end+1)
			}
			return b.String(), nil
		},
	}
}

// ---- write

func writeTool() Tool {
	return Tool{
		Name:        "write",
		Description: "Create or overwrite a file in the project with the given content. Prefer edit for changes to existing files.",
		Schema: schema(map[string]any{
			"path":    prop("string", "File path, relative to the project root"),
			"content": prop("string", "The complete file content"),
		}, "path", "content"),
		Mutates: true,
		Summary: func(a map[string]any) string { return "write " + str(a, "path") },
		Consent: mutationConsent(func(a map[string]any) string {
			c := str(a, "content")
			if len(c) > 1500 {
				c = c[:1500] + "\n…"
			}
			return "Write " + str(a, "path") + ":\n\n" + c
		}),
		Run: func(ctx context.Context, r *Runner, a map[string]any) (string, error) {
			abs, rel, err := r.resolve(str(a, "path"))
			if err != nil {
				return "", err
			}
			if err := r.checkReadable(abs); err != nil {
				return "", err
			}
			content := str(a, "content")
			existed := false
			if _, err := os.Stat(abs); err == nil {
				existed = true
			}
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return "", err
			}
			if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
				return "", err
			}
			verb := "created"
			if existed {
				verb = "overwrote"
			}
			return fmt.Sprintf("%s %s (%d bytes, %d lines)", verb, rel, len(content), strings.Count(content, "\n")+1), nil
		},
	}
}

// ---- edit

func editTool() Tool {
	return Tool{
		Name:        "edit",
		Description: "Replace text in a file. old_string must occur exactly once (include enough surrounding lines to make it unique), unless replace_all is true.",
		Schema: schema(map[string]any{
			"path":        prop("string", "File path, relative to the project root"),
			"old_string":  prop("string", "The exact text to replace"),
			"new_string":  prop("string", "The replacement text"),
			"replace_all": prop("boolean", "Replace every occurrence (default false)"),
		}, "path", "old_string", "new_string"),
		Mutates: true,
		Summary: func(a map[string]any) string { return "edit " + str(a, "path") },
		Consent: mutationConsent(func(a map[string]any) string {
			return "Edit " + str(a, "path") + ":\n\n" + unifiedSnippet(str(a, "old_string"), str(a, "new_string"))
		}),
		Run: func(ctx context.Context, r *Runner, a map[string]any) (string, error) {
			abs, rel, err := r.resolve(str(a, "path"))
			if err != nil {
				return "", err
			}
			if err := r.checkReadable(abs); err != nil {
				return "", err
			}
			oldS, newS := str(a, "old_string"), str(a, "new_string")
			if oldS == "" {
				return "", errors.New("old_string is empty; use write to create a file")
			}
			b, err := os.ReadFile(abs)
			if err != nil {
				return "", err
			}
			src := string(b)
			n := strings.Count(src, oldS)
			var out string
			switch {
			case n == 0:
				// Tolerate indentation differences (tabs vs spaces, a
				// re-indented block): match line by line ignoring leading
				// and trailing whitespace, and re-indent the replacement
				// the way the file is indented.
				var ferr error
				out, ferr = fuzzyReplace(src, oldS, newS)
				if ferr != nil {
					return "", fmt.Errorf("%w in %s; read the file and copy the text exactly (indentation is matched loosely, other characters must be identical)", ferr, rel)
				}
				n = 1
			case n > 1 && !boolean(a, "replace_all"):
				return "", fmt.Errorf("old_string occurs %d times in %s; include more context or set replace_all", n, rel)
			case boolean(a, "replace_all"):
				out = strings.ReplaceAll(src, oldS, newS)
			default:
				out = strings.Replace(src, oldS, newS, 1)
			}
			st, _ := os.Stat(abs)
			mode := fs.FileMode(0o644)
			if st != nil {
				mode = st.Mode().Perm()
			}
			if err := os.WriteFile(abs, []byte(out), mode); err != nil {
				return "", err
			}
			return fmt.Sprintf("edited %s (%d replacement(s))", rel, max(n, 1)), nil
		},
	}
}

// mutationConsent asks in "ask" mode always, and in "edits" mode only for
// protected paths (CI, verification config, ...).
func mutationConsent(detail func(a map[string]any) string) func(r *Runner, a map[string]any) (string, bool) {
	return func(r *Runner, a map[string]any) (string, bool) {
		// Protected paths, instruction files and git hooks are the guard's
		// (guard.go): it asks for them in every mode.
		if r.Opts.Approval == ApprovalAsk {
			return detail(a), true
		}
		return "", false
	}
}

// unifiedSnippet shows an edit as removed and added lines.
func unifiedSnippet(oldS, newS string) string {
	var b strings.Builder
	for _, l := range strings.Split(strings.TrimRight(oldS, "\n"), "\n") {
		b.WriteString("- " + l + "\n")
	}
	for _, l := range strings.Split(strings.TrimRight(newS, "\n"), "\n") {
		b.WriteString("+ " + l + "\n")
	}
	s := b.String()
	if len(s) > 3000 {
		s = s[:3000] + "\n…"
	}
	return s
}

// ---- bash

// safeCommands may run without asking in "edits" mode: read-only
// inspection and the usual build and test commands. The deterministic
// policy still applies to everything.
var safeCommands = []*regexp.Regexp{
	regexp.MustCompile(`^(ls|cat|head|tail|wc|pwd|echo|printf|stat|file|which|type|env|date|tree|du|df|uname|whoami|id)(\s|$)`),
	regexp.MustCompile(`^(grep|rg|egrep|fgrep|find|fd|awk|sed -n|sort|uniq|cut|tr|diff|jq|yq|xargs grep)(\s|$)`),
	regexp.MustCompile(`^git (status|diff|log|show|blame|branch|rev-parse|ls-files|grep|describe|remote -v|stash list|tag( -l)?|worktree list)(\s|$)`),
	regexp.MustCompile(`^go (build|test|vet|list|fmt|mod (tidy|download|verify|why|graph)|env|version|doc|run)(\s|$)`),
	regexp.MustCompile(`^gofmt(\s|$)|^golangci-lint run(\s|$)|^staticcheck(\s|$)`),
	regexp.MustCompile(`^(npm|pnpm|yarn|bun) (test|run (test|lint|build|typecheck|check)|lint|build|ls|list|outdated|why)(\s|$)`),
	regexp.MustCompile(`^(npx|pnpm dlx|bunx) (tsc|eslint|prettier|vitest|jest|mocha|playwright test|biome)(\s|$)`),
	regexp.MustCompile(`^(python3?|pytest|ruff|mypy|black --check|flake8|pylint|uv run (pytest|ruff|mypy|python))(\s|$)`),
	regexp.MustCompile(`^cargo (build|test|check|clippy|fmt|doc|tree|metadata)(\s|$)`),
	regexp.MustCompile(`^(mvn|\./mvnw) (test|compile|verify|package)(\s|$)|^(gradle|\./gradlew) (test|build|check|assemble)(\s|$)`),
	regexp.MustCompile(`^(make|just) (test|build|check|lint|vet|fmt)(\s|$)`),
	regexp.MustCompile(`^(dotnet (build|test)|swift (build|test)|zig (build|test)|mix test|bundle exec rspec|rspec|rake test|composer test|phpunit|ctest|cmake --build)(\s|$)`),
}

func isSafeCommand(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return false
	}
	// Each simple command must be safe; redirections and substitutions make
	// a command line unsafe.
	if strings.ContainsAny(cmd, ">`$") || strings.Contains(cmd, "<(") {
		return false
	}
	for _, part := range regexp.MustCompile(`\|\||&&|;|\|`).Split(cmd, -1) {
		part = strings.TrimSpace(part)
		if part == "" {
			return false
		}
		ok := false
		for _, re := range safeCommands {
			if re.MatchString(part) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func bashTool() Tool {
	desc := "Run a shell command in the project directory and return its output (stdout and stderr). Use it for builds, tests, git and anything the other tools do not cover. Commands that push, deploy or destroy are refused by policy."
	if runtime.GOOS == "windows" {
		desc = "Run a PowerShell command in the project directory and return its output. Use it for builds, tests, git and anything the other tools do not cover. Commands that push, deploy or destroy are refused by policy."
	}
	return Tool{
		Name:        "bash",
		Description: desc,
		Schema: schema(map[string]any{
			"command":           prop("string", "The command line to run"),
			"timeout":           prop("integer", "Seconds before the command is killed (default from configuration)"),
			"description":       prop("string", "What the command is for, in a few words"),
			"run_in_background": prop("boolean", "Start the command and return a shell id at once; read its output later with bash_output and stop it with kill_shell (servers, long builds)"),
		}, "command"),
		Mutates: true,
		Summary: func(a map[string]any) string {
			return "$ " + oneLine(str(a, "command"), 160)
		},
		Precheck: func(r *Runner, a map[string]any) error {
			cmd := str(a, "command")
			if strings.TrimSpace(cmd) == "" {
				return errors.New("command is empty")
			}
			if r.Opts.ReadOnly && !isSafeCommand(cmd) {
				return errors.New("this agent is read-only: only inspection, build and test commands run")
			}
			if r.plan && !isSafeCommand(cmd) {
				return errors.New("plan mode: only read-only, build and test commands run until the plan is approved")
			}
			return policy.CheckCommand(shellArgv(cmd))
		},
		Consent: func(r *Runner, a map[string]any) (string, bool) {
			cmd := str(a, "command")
			switch r.Opts.Approval {
			case ApprovalAuto:
				return "", false
			case ApprovalEdits:
				if isSafeCommand(cmd) {
					return "", false
				}
			}
			d := "Run in " + r.cwd() + ":\n\n  " + strings.ReplaceAll(cmd, "\n", "\n  ")
			if why := str(a, "description"); why != "" {
				d += "\n\nThe model's description (unverified): " + why
			}
			return d, true
		},
		Run: func(ctx context.Context, r *Runner, a map[string]any) (string, error) {
			cmd := str(a, "command")
			if strings.TrimSpace(cmd) == "" {
				return "", errors.New("command is empty")
			}
			argv := shellArgv(cmd)
			timeout := r.Opts.CommandTimeout
			if timeout <= 0 {
				timeout = 3 * time.Minute
			}
			if s := num(a, "timeout"); s > 0 {
				timeout = time.Duration(s) * time.Second
			}
			if ok, refusal := r.containment(ctx, cmd); !ok {
				return "", errors.New(strings.TrimPrefix(refusal, "refused: "))
			}
			env := []string{"GIT_TERMINAL_PROMPT=0", "CI=1", "NO_COLOR=1", "TERM=dumb", "PAGER=cat", "GIT_PAGER=cat"}
			if boolean(a, "run_in_background") {
				c, err := r.command(context.Background(), argv, r.cwd(), env, cmd)
				if err != nil {
					return "", err
				}
				return r.shells().start(c, cmd, str(a, "description"))
			}
			cctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			var refused netRefusals
			c, err := r.command(cctx, argv, r.cwd(), env, cmd, &refused)
			if err != nil {
				return "", err
			}
			c.Stdin = nil
			var out bytes.Buffer
			c.Stdout, c.Stderr = &out, &out
			err = c.Run()
			res := out.String() + refused.note()
			if cctx.Err() == context.DeadlineExceeded {
				return res, fmt.Errorf("command killed after %s", timeout)
			}
			if err != nil {
				var ee *exec.ExitError
				if errors.As(err, &ee) {
					return res, fmt.Errorf("exit status %d", ee.ExitCode())
				}
				return res, err
			}
			if strings.TrimSpace(res) == "" {
				res = "(no output)"
			}
			return res, nil
		},
	}
}

func shellArgv(cmd string) []string {
	if runtime.GOOS == "windows" {
		return []string{"powershell", "-NoProfile", "-NonInteractive", "-Command", cmd}
	}
	sh := "bash"
	if _, err := exec.LookPath(sh); err != nil {
		sh = "sh"
	}
	return []string{sh, "-c", cmd}
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// ---- grep

func grepTool() Tool {
	return Tool{
		Name:        "grep",
		Description: "Search file contents with a regular expression. Returns file:line:text matches. Ignored and binary files are skipped.",
		Schema: schema(map[string]any{
			"pattern":     prop("string", "Regular expression (RE2/ripgrep syntax)"),
			"path":        prop("string", "Directory or file to search (default: the project root)"),
			"glob":        prop("string", "Only files matching this glob, e.g. *.go or src/**/*.ts"),
			"ignore_case": prop("boolean", "Case-insensitive search"),
			"output_mode": map[string]any{"type": "string", "enum": []string{"content", "files", "count"}, "description": "content: matching lines (default); files: only the paths that match; count: matches per file"},
			"context":     prop("integer", "Lines of context around each match (content mode)"),
		}, "pattern"),
		Summary: func(a map[string]any) string {
			s := "grep " + oneLine(str(a, "pattern"), 60)
			if p := str(a, "path"); p != "" {
				s += " in " + p
			}
			return s
		},
		Run: func(ctx context.Context, r *Runner, a map[string]any) (string, error) {
			pattern := str(a, "pattern")
			if pattern == "" {
				return "", errors.New("pattern is required")
			}
			p := str(a, "path")
			if p == "" {
				p = "."
			}
			abs, _, err := r.resolve(p)
			if err != nil {
				return "", err
			}
			if err := r.checkReadable(abs); err != nil {
				return "", err
			}
			mode := str(a, "output_mode")
			switch mode {
			case "files", "files_with_matches":
				mode = "files"
			case "count":
			default:
				mode = "content"
			}
			ctxLines := num(a, "context")
			if rg, err := exec.LookPath("rg"); err == nil {
				return r.rgGrep(ctx, rg, abs, pattern, str(a, "glob"), boolean(a, "ignore_case"), mode, ctxLines)
			}
			return r.goGrep(ctx, abs, pattern, str(a, "glob"), boolean(a, "ignore_case"), mode, ctxLines)
		},
	}
}

// rgGrep searches with ripgrep, reading its JSON output so that every file
// it reports passes the same readability rule as the other file tools
// (secret paths, .git): ripgrep's own ignore rules do not know the
// secret-path policy. The output has the Go fallback's form.
func (r *Runner) rgGrep(ctx context.Context, rg, abs, pattern, glob string, ignoreCase bool, mode string, ctxLines int) (string, error) {
	ctxLines = min(max(ctxLines, 0), 10)
	args := []string{"--json", "--no-messages"}
	switch mode {
	case "files":
		args = append(args, "--max-count", "1")
	case "content":
		args = append(args, "--max-count", "50")
		if ctxLines > 0 {
			args = append(args, "-C", fmt.Sprint(ctxLines))
		}
	}
	if ignoreCase {
		args = append(args, "-i")
	}
	if glob != "" {
		args = append(args, "--glob", glob)
	}
	args = append(args, "-e", pattern, abs)
	c := exec.CommandContext(ctx, rg, args...)
	c.Dir = r.cwd()
	out, err := c.Output()
	var ee *exec.ExitError
	if err != nil && (!errors.As(err, &ee) || ee.ExitCode() != 1) {
		if ee != nil && ee.ExitCode() == 2 {
			return "", fmt.Errorf("invalid pattern: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	root, _ := filepath.EvalSymlinks(r.cwd())
	if root == "" {
		root = r.cwd()
	}
	var (
		lines    []string
		counts   = map[string]int{}
		order    []string
		allowed  = map[string]bool{}
		lastFile string
		lastLine int
	)
	for _, raw := range bytes.Split(out, []byte("\n")) {
		var m rgMessage
		if len(raw) == 0 || json.Unmarshal(raw, &m) != nil {
			continue
		}
		file := m.Data.Path.String()
		if file == "" {
			continue
		}
		ok, seen := allowed[file]
		if !seen {
			ok = r.checkReadable(file) == nil
			allowed[file] = ok
		}
		if !ok {
			continue
		}
		rel := file
		if rr, err := filepath.Rel(root, file); err == nil && !strings.HasPrefix(rr, "..") {
			rel = filepath.ToSlash(rr)
		}
		switch m.Type {
		case "match":
			if counts[rel] == 0 {
				order = append(order, rel)
			}
			counts[rel]++
			if mode != "content" {
				continue
			}
			if ctxLines > 0 && len(lines) > 0 && (rel != lastFile || m.Data.LineNumber > lastLine+1) {
				lines = append(lines, "--")
			}
			lines = append(lines, fmt.Sprintf("%s:%d:%s", rel, m.Data.LineNumber, clipLine(m.Data.Lines.line())))
			lastFile, lastLine = rel, m.Data.LineNumber
		case "context":
			if mode != "content" {
				continue
			}
			if len(lines) > 0 && (rel != lastFile || m.Data.LineNumber > lastLine+1) {
				lines = append(lines, "--")
			}
			lines = append(lines, fmt.Sprintf("%s-%d-%s", rel, m.Data.LineNumber, clipLine(m.Data.Lines.line())))
			lastFile, lastLine = rel, m.Data.LineNumber
		}
	}
	switch mode {
	case "files":
		lines = order
	case "count":
		lines = nil
		for _, f := range order {
			lines = append(lines, fmt.Sprintf("%s:%d", f, counts[f]))
		}
	}
	if len(lines) == 0 {
		return "(no matches)", nil
	}
	if len(lines) > maxGrepMatches {
		if mode == "content" {
			var hits []string
			for _, f := range order {
				for i := 0; i < counts[f]; i++ {
					hits = append(hits, f+":0:")
				}
			}
			return collapseMatches(hits, maxGrepMatches), nil
		}
		return strings.Join(lines[:maxGrepMatches], "\n") + fmt.Sprintf("\n… (more than %d files; narrow the pattern or path)", maxGrepMatches), nil
	}
	return strings.Join(lines, "\n"), nil
}

// rgMessage is one line of ripgrep's --json output.
type rgMessage struct {
	Type string `json:"type"`
	Data struct {
		Path       rgText `json:"path"`
		Lines      rgText `json:"lines"`
		LineNumber int    `json:"line_number"`
	} `json:"data"`
}

// rgText is ripgrep's text: UTF-8 in text, anything else base64 in bytes.
type rgText struct {
	Text  string `json:"text"`
	Bytes string `json:"bytes"`
}

func (t rgText) String() string {
	if t.Text != "" || t.Bytes == "" {
		return t.Text
	}
	b, err := base64.StdEncoding.DecodeString(t.Bytes)
	if err != nil {
		return ""
	}
	return string(b)
}

func (t rgText) line() string { return strings.TrimRight(t.String(), "\r\n") }

// goGrep is the fallback without ripgrep. mode is content, files or count;
// ctxLines adds context around matches in content mode.
func (r *Runner) goGrep(ctx context.Context, abs, pattern, glob string, ignoreCase bool, mode string, ctxLines int) (string, error) {
	if ignoreCase {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid pattern: %w", err)
	}
	ctxLines = min(max(ctxLines, 0), 10)
	var b strings.Builder
	n := 0
	perFile := map[string]int{}
	var fileOrder []string
	err = r.walk(abs, func(file, rel string, d fs.DirEntry) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if glob != "" && !matchGlob(glob, rel) {
			return nil
		}
		data, err := os.ReadFile(file)
		if err != nil || bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
			return nil //nolint:nilerr // unreadable or binary files are skipped
		}
		lines := strings.Split(string(data), "\n")
		hits := 0
		lastShown := -1
		for i, l := range lines {
			if !re.MatchString(l) {
				continue
			}
			hits++
			if hits == 1 && mode == "content" {
				fileOrder = append(fileOrder, rel)
			}
			if mode == "files" {
				break
			}
			if mode == "count" {
				continue
			}
			show := n < maxGrepMatches
			if ctxLines > 0 && show {
				if lastShown >= 0 && i-ctxLines > lastShown+1 {
					b.WriteString("--\n")
				}
				for j := max(i-ctxLines, lastShown+1); j < i; j++ {
					fmt.Fprintf(&b, "%s-%d-%s\n", rel, j+1, clipLine(lines[j]))
				}
			}
			if show {
				fmt.Fprintf(&b, "%s:%d:%s\n", rel, i+1, clipLine(l))
			}
			perFile[rel]++
			lastShown = i
			for j := i + 1; ctxLines > 0 && show && j <= i+ctxLines && j < len(lines); j++ {
				if re.MatchString(lines[j]) {
					break
				}
				fmt.Fprintf(&b, "%s-%d-%s\n", rel, j+1, clipLine(lines[j]))
				lastShown = j
			}
			if n++; n >= maxGrepMatches*50 {
				return errStop
			}
		}
		switch {
		case hits == 0:
		case mode == "files":
			fmt.Fprintf(&b, "%s\n", rel)
			if n++; n >= maxGrepMatches {
				return errStop
			}
		case mode == "count":
			fmt.Fprintf(&b, "%s:%d\n", rel, hits)
			if n++; n >= maxGrepMatches {
				return errStop
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return "", err
	}
	if b.Len() == 0 {
		return "(no matches)", nil
	}
	if mode == "content" && n > maxGrepMatches {
		// Too many matches to show: the count per file instead.
		var lines []string
		for _, f := range fileOrder {
			for i := 0; i < perFile[f]; i++ {
				lines = append(lines, f+":0:")
			}
		}
		return collapseMatches(lines, maxGrepMatches), nil
	}
	s := strings.TrimRight(b.String(), "\n")
	if n >= maxGrepMatches {
		s += fmt.Sprintf("\n… (more than %d matches; narrow the pattern or path)", maxGrepMatches)
	}
	return s, nil
}

var errStop = errors.New("stop")

func clipLine(l string) string {
	if len(l) > 300 {
		return l[:300] + "…"
	}
	return l
}

var skippedDirs = map[string]bool{".git": true, "node_modules": true, ".venv": true, "venv": true, "__pycache__": true, "target": true, "dist": true, "build": true, ".next": true, ".cache": true, "vendor": true, ".idea": true, ".vscode": false}

// walk visits regular files under dir (a file visits itself), skipping
// build and dependency directories, with paths relative to the root.
func (r *Runner) walk(dir string, fn func(file, rel string, d fs.DirEntry) error) error {
	cwd, _ := filepath.EvalSymlinks(r.cwd())
	if cwd == "" {
		cwd = r.cwd()
	}
	st, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		rel, _ := filepath.Rel(cwd, dir)
		return fn(dir, filepath.ToSlash(rel), fs.FileInfoToDirEntry(st))
	}
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // unreadable entries are skipped
		}
		rel, _ := filepath.Rel(cwd, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if p != dir && (skippedDirs[d.Name()] || r.checkReadable(p) != nil) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || r.checkReadable(p) != nil {
			return nil //nolint:nilerr // masked or non-regular entries are skipped
		}
		return fn(p, rel, d)
	})
}

// matchGlob matches a slash path against a glob; "**/" means any
// directory depth, and a bare file glob (no slash) matches the base name.
func matchGlob(glob, rel string) bool {
	glob = filepath.ToSlash(glob)
	if !strings.Contains(glob, "/") {
		ok, _ := path.Match(glob, path.Base(rel))
		return ok
	}
	if strings.HasPrefix(glob, "**/") {
		suffix := strings.TrimPrefix(glob, "**/")
		for rest := rel; ; {
			if ok, _ := path.Match(suffix, rest); ok {
				return true
			}
			i := strings.Index(rest, "/")
			if i < 0 {
				return false
			}
			rest = rest[i+1:]
		}
	}
	if strings.Contains(glob, "**") {
		// a/**/b.go: prefix must match, then any depth, then the suffix.
		pre, post, _ := strings.Cut(glob, "**")
		pre = strings.TrimSuffix(pre, "/")
		post = strings.TrimPrefix(post, "/")
		if pre != "" && !strings.HasPrefix(rel, pre+"/") {
			return false
		}
		rest := strings.TrimPrefix(strings.TrimPrefix(rel, pre), "/")
		if post == "" {
			return true
		}
		return matchGlob("**/"+post, rest)
	}
	ok, _ := path.Match(glob, rel)
	return ok
}

// ---- glob

func globTool() Tool {
	return Tool{
		Name:        "glob",
		Description: "Find files by name pattern (e.g. **/*.go, src/**/*.test.ts, Makefile). Returns paths sorted by modification time, newest first.",
		Schema: schema(map[string]any{
			"pattern": prop("string", "Glob pattern; ** matches any directory depth"),
			"path":    prop("string", "Directory to search (default: the project root)"),
		}, "pattern"),
		Summary: func(a map[string]any) string { return "glob " + str(a, "pattern") },
		Run: func(ctx context.Context, r *Runner, a map[string]any) (string, error) {
			pattern := str(a, "pattern")
			if pattern == "" {
				return "", errors.New("pattern is required")
			}
			p := str(a, "path")
			if p == "" {
				p = "."
			}
			abs, _, err := r.resolve(p)
			if err != nil {
				return "", err
			}
			if err := r.checkReadable(abs); err != nil {
				return "", err
			}
			type hit struct {
				rel string
				mod time.Time
			}
			var hits []hit
			err = r.walk(abs, func(file, rel string, d fs.DirEntry) error {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if matchGlob(pattern, rel) {
					info, _ := d.Info()
					var mod time.Time
					if info != nil {
						mod = info.ModTime()
					}
					hits = append(hits, hit{rel, mod})
				}
				return nil
			})
			if err != nil {
				return "", err
			}
			if len(hits) == 0 {
				return "(no files match)", nil
			}
			sort.Slice(hits, func(i, j int) bool { return hits[i].mod.After(hits[j].mod) })
			cut := len(hits) > maxGlobResults
			if cut {
				hits = hits[:maxGlobResults]
			}
			var b strings.Builder
			for _, h := range hits {
				b.WriteString(h.rel + "\n")
			}
			if cut {
				fmt.Fprintf(&b, "… (more than %d files; narrow the pattern)\n", maxGlobResults)
			}
			return strings.TrimRight(b.String(), "\n"), nil
		},
	}
}

// ---- ls

func lsTool() Tool {
	return Tool{
		Name:        "ls",
		Description: "List a directory: names with a trailing / for directories.",
		Schema: schema(map[string]any{
			"path": prop("string", "Directory, relative to the project root (default: the root)"),
		}),
		Summary: func(a map[string]any) string {
			p := str(a, "path")
			if p == "" {
				p = "."
			}
			return "ls " + p
		},
		Run: func(ctx context.Context, r *Runner, a map[string]any) (string, error) {
			p := str(a, "path")
			if p == "" {
				p = "."
			}
			abs, _, err := r.resolve(p)
			if err != nil {
				return "", err
			}
			if err := r.checkReadable(abs); err != nil {
				return "", err
			}
			entries, err := os.ReadDir(abs)
			if err != nil {
				return "", err
			}
			var b strings.Builder
			n := 0
			for _, e := range entries {
				name := e.Name()
				if e.IsDir() {
					name += "/"
				}
				b.WriteString(name + "\n")
				if n++; n >= 500 {
					fmt.Fprintf(&b, "… (%d more entries)\n", len(entries)-n)
					break
				}
			}
			if b.Len() == 0 {
				return "(empty directory)", nil
			}
			return strings.TrimRight(b.String(), "\n"), nil
		},
	}
}

var errNotFound = errors.New("old_string not found")

// fuzzyReplace replaces old with new in src when old matches a unique run
// of lines ignoring leading and trailing whitespace. The replacement's
// lines are re-indented from old's indentation to the file's.
func fuzzyReplace(src, old, new string) (string, error) {
	oldLines := strings.Split(strings.TrimRight(old, "\n"), "\n")
	if len(oldLines) == 0 || strings.TrimSpace(old) == "" {
		return "", errNotFound
	}
	srcLines := strings.Split(src, "\n")
	trim := strings.TrimSpace
	start := -1
	for i := 0; i+len(oldLines) <= len(srcLines); i++ {
		ok := true
		for j, ol := range oldLines {
			if trim(srcLines[i+j]) != trim(ol) {
				ok = false
				break
			}
		}
		if ok {
			if start >= 0 {
				return "", errors.New("old_string matches more than once ignoring indentation")
			}
			start = i
		}
	}
	if start < 0 {
		return "", errNotFound
	}
	// Indentation mapping: old's first line indent -> the file's.
	fileIndent := leadingWS(srcLines[start])
	oldIndent := leadingWS(oldLines[0])
	unit := indentUnit(oldLines, srcLines[start:start+len(oldLines)])
	var repl []string
	for _, nl := range strings.Split(strings.TrimRight(new, "\n"), "\n") {
		if strings.TrimSpace(nl) == "" {
			repl = append(repl, "")
			continue
		}
		ws := leadingWS(nl)
		body := nl[len(ws):]
		var extra string
		if strings.HasPrefix(ws, oldIndent) {
			extra = ws[len(oldIndent):]
		} else {
			extra = strings.TrimLeft(ws, " \t") // shallower than old's first line: keep the file's indent
		}
		if strings.HasPrefix(fileIndent, "\t") || (fileIndent == "" && strings.Contains(strings.Join(srcLines[start:start+len(oldLines)], ""), "\t")) {
			if unit > 0 && !strings.Contains(extra, "\t") {
				extra = strings.Repeat("\t", len(extra)/unit)
			}
		} else if unit > 0 && strings.Contains(extra, "\t") && !strings.Contains(fileIndent, "\t") {
			extra = strings.Repeat(" ", strings.Count(extra, "\t")*unit)
		}
		repl = append(repl, fileIndent+extra+body)
	}
	out := append(append(append([]string{}, srcLines[:start]...), repl...), srcLines[start+len(oldLines):]...)
	return strings.Join(out, "\n"), nil
}

func leadingWS(s string) string { return s[:len(s)-len(strings.TrimLeft(s, " \t"))] }

// indentUnit guesses how many spaces one indentation level is in old (or,
// when the file indents with spaces and old with tabs, in the file): the
// smallest positive difference between consecutive lines' indentation.
func indentUnit(oldLines, fileLines []string) int {
	best := 0
	for _, ls := range [][]string{oldLines, fileLines} {
		for i := 1; i < len(ls); i++ {
			a, b := leadingWS(ls[i-1]), leadingWS(ls[i])
			if strings.Contains(a+b, "\t") || strings.TrimSpace(ls[i]) == "" || strings.TrimSpace(ls[i-1]) == "" {
				continue
			}
			if d := len(b) - len(a); d < 0 {
				d = -d
				if d > 0 && (best == 0 || d < best) {
					best = d
				}
			} else if d > 0 && (best == 0 || d < best) {
				best = d
			}
		}
		if best > 0 {
			return best
		}
	}
	return 4
}

// rootRel is abs relative to the project root (slash-separated).
func (r *Runner) rootRel(abs string) string {
	root, err := filepath.EvalSymlinks(r.Opts.Root)
	if err != nil {
		root = filepath.Clean(r.Opts.Root)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}
