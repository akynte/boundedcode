package direct

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/repointel"
)

// ---- multi_edit

func multiEditTool() Tool {
	return Tool{
		Name:        "multi_edit",
		Description: "Apply several edits to one file atomically, in order; each edit is like edit (old_string must occur once unless replace_all). Nothing is written if any edit fails.",
		Schema: schema(map[string]any{
			"path": prop("string", "File path, relative to the project root"),
			"edits": map[string]any{"type": "array", "description": "The edits, applied in order", "items": map[string]any{"type": "object", "properties": map[string]any{
				"old_string": prop("string", "Exact text to replace"), "new_string": prop("string", "Replacement"), "replace_all": prop("boolean", "Replace every occurrence")}, "required": []string{"old_string", "new_string"}}},
		}, "path", "edits"),
		Mutates: true,
		Summary: func(a map[string]any) string {
			n := 0
			if es, ok := a["edits"].([]any); ok {
				n = len(es)
			}
			return fmt.Sprintf("multi_edit %s (%d edits)", str(a, "path"), n)
		},
		Consent: mutationConsent(func(a map[string]any) string {
			var b strings.Builder
			b.WriteString("Edit " + str(a, "path") + ":\n")
			if es, ok := a["edits"].([]any); ok {
				for _, e := range es {
					if m, ok := e.(map[string]any); ok {
						b.WriteString("\n" + unifiedSnippet(str(m, "old_string"), str(m, "new_string")))
					}
				}
			}
			return b.String()
		}),
		Run: func(ctx context.Context, r *Runner, a map[string]any) (string, error) {
			abs, rel, err := r.resolve(str(a, "path"))
			if err != nil {
				return "", err
			}
			if err := r.checkReadable(abs); err != nil {
				return "", err
			}
			es, _ := a["edits"].([]any)
			if len(es) == 0 {
				return "", errors.New("edits is empty")
			}
			b, err := os.ReadFile(abs)
			if err != nil {
				return "", err
			}
			src := string(b)
			for i, e := range es {
				m, _ := e.(map[string]any)
				oldS, newS := str(m, "old_string"), str(m, "new_string")
				if oldS == "" {
					return "", fmt.Errorf("edit %d: old_string is empty", i+1)
				}
				n := strings.Count(src, oldS)
				switch {
				case n == 0:
					out, ferr := fuzzyReplace(src, oldS, newS)
					if ferr != nil {
						return "", fmt.Errorf("edit %d: %w in %s (nothing was written)", i+1, ferr, rel)
					}
					src = out
				case n > 1 && !boolean(m, "replace_all"):
					return "", fmt.Errorf("edit %d: old_string occurs %d times in %s (nothing was written)", i+1, n, rel)
				case boolean(m, "replace_all"):
					src = strings.ReplaceAll(src, oldS, newS)
				default:
					src = strings.Replace(src, oldS, newS, 1)
				}
			}
			st, _ := os.Stat(abs)
			mode := os.FileMode(0o644)
			if st != nil {
				mode = st.Mode().Perm()
			}
			if err := os.WriteFile(abs, []byte(src), mode); err != nil {
				return "", err
			}
			return fmt.Sprintf("applied %d edit(s) to %s", len(es), rel), nil
		},
	}
}

// ---- read_many

func readManyTool() Tool {
	return Tool{
		Name:        "read_many",
		Description: "Read several files at once (paths or glob patterns) and return their contents, each under a header. Large files are truncated; binary and ignored files are skipped.",
		Schema: schema(map[string]any{
			"paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "File paths or glob patterns (e.g. src/**/*.go)"},
		}, "paths"),
		Summary: func(a map[string]any) string {
			ps, _ := a["paths"].([]any)
			return fmt.Sprintf("read %d path(s)", len(ps))
		},
		Run: func(ctx context.Context, r *Runner, a map[string]any) (string, error) {
			raw, _ := a["paths"].([]any)
			if len(raw) == 0 {
				return "", errors.New("paths is empty")
			}
			var files []string
			seen := map[string]bool{}
			add := func(rel string) {
				if !seen[rel] {
					seen[rel] = true
					files = append(files, rel)
				}
			}
			for _, p := range raw {
				ps, _ := p.(string)
				if ps == "" {
					continue
				}
				if strings.ContainsAny(ps, "*?[") {
					dir, pat := ".", ps
					if i := strings.IndexAny(ps, "*?["); i > 0 {
						if j := strings.LastIndex(ps[:i], "/"); j >= 0 {
							dir, pat = ps[:j], ps[j+1:]
						}
					}
					abs, _, err := r.resolve(dir)
					if err != nil {
						continue
					}
					_ = r.walk(abs, func(file, rel string, _ os.DirEntry) error {
						if matchGlob(pat, strings.TrimPrefix(strings.TrimPrefix(rel, strings.TrimPrefix(dir, "./")), "/")) || matchGlob(ps, rel) {
							add(rel)
						}
						return nil
					})
					continue
				}
				abs, rel, err := r.resolve(ps)
				if err != nil {
					continue
				}
				if st, err := os.Stat(abs); err == nil && !st.IsDir() {
					add(rel)
				}
			}
			if len(files) == 0 {
				return "(no files matched)", nil
			}
			sort.Strings(files)
			var b strings.Builder
			total := 0
			for _, rel := range files {
				abs, _, err := r.resolve(rel)
				if err != nil || r.checkReadable(abs) != nil {
					continue
				}
				data, err := os.ReadFile(abs)
				if err != nil || isBinary(data) {
					continue
				}
				if len(data) > maxAttachBytes {
					data = append(data[:maxAttachBytes], []byte("\n… (truncated)")...)
				}
				fmt.Fprintf(&b, "==> %s <==\n%s\n\n", rel, strings.TrimRight(string(data), "\n"))
				if total += len(data); total > maxToolOutput*2 {
					fmt.Fprintf(&b, "… (%d more files not shown; narrow the request)\n", len(files))
					break
				}
			}
			return b.String(), nil
		},
	}
}

func isBinary(b []byte) bool {
	for _, c := range b[:min(len(b), 8000)] {
		if c == 0 {
			return true
		}
	}
	return false
}

// ---- todo_write

// Todo is one item of the session's task list.
type Todo struct {
	Content string `json:"content"`
	Status  string `json:"status"` // pending | in_progress | completed
}

func todoTool() Tool {
	return Tool{
		Name:        "todo_write",
		Description: "Replace the session's task list. Use it for multi-step work: list the steps, mark one in_progress at a time, and complete them as you go.",
		Schema: schema(map[string]any{
			"todos": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{
				"content": prop("string", "The step"), "status": map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "completed"}}}, "required": []string{"content", "status"}}},
		}, "todos"),
		Summary: func(a map[string]any) string {
			ts, _ := a["todos"].([]any)
			return fmt.Sprintf("todo list (%d items)", len(ts))
		},
		Run: func(_ context.Context, r *Runner, a map[string]any) (string, error) {
			raw, _ := a["todos"].([]any)
			var todos []Todo
			for _, t := range raw {
				m, _ := t.(map[string]any)
				c, st := strings.TrimSpace(str(m, "content")), str(m, "status")
				if c == "" {
					continue
				}
				if st != "pending" && st != "in_progress" && st != "completed" {
					st = "pending"
				}
				todos = append(todos, Todo{c, st})
			}
			r.Todos = todos
			text := RenderTodos(todos)
			r.emit(Event{Kind: EventTodos, Text: text})
			return "task list updated:\n" + text, nil
		},
	}
}

// RenderTodos formats a task list.
func RenderTodos(todos []Todo) string {
	if len(todos) == 0 {
		return "(empty)"
	}
	var b strings.Builder
	for _, t := range todos {
		mark := "[ ]"
		switch t.Status {
		case "in_progress":
			mark = "[>]"
		case "completed":
			mark = "[x]"
		}
		fmt.Fprintf(&b, "%s %s\n", mark, t.Content)
	}
	return strings.TrimRight(b.String(), "\n")
}

// ---- plan mode

func enterPlanTool() Tool {
	return Tool{
		Name:        "enter_plan_mode",
		Description: "Switch to plan mode for a non-trivial change: explore and write a plan without changing files. Call exit_plan_mode with the plan when it is ready; the user approves it before any edit.",
		Schema:      schema(map[string]any{}),
		Summary:     func(map[string]any) string { return "enter plan mode" },
		Run: func(_ context.Context, r *Runner, _ map[string]any) (string, error) {
			r.plan = true
			r.emit(Event{Kind: EventMode, Text: "plan", Summary: "planning: no files change until the plan is approved"})
			return "Plan mode is on. Read and search as needed, then call exit_plan_mode with the plan (goal, steps, files to change, how to verify).", nil
		},
	}
}

func exitPlanTool() Tool {
	return Tool{
		Name:        "exit_plan_mode",
		Description: "Present the plan to the user for approval and leave plan mode when approved.",
		Schema:      schema(map[string]any{"plan": prop("string", "The plan in markdown")}, "plan"),
		Summary:     func(map[string]any) string { return "plan ready for approval" },
		Run: func(ctx context.Context, r *Runner, a map[string]any) (string, error) {
			plan := strings.TrimSpace(str(a, "plan"))
			if plan == "" {
				return "", errors.New("plan is empty")
			}
			r.emit(Event{Kind: EventText, Text: "Plan:\n\n" + plan})
			approved := r.Opts.Approval == ApprovalAuto
			if !approved && r.Approve != nil {
				approved = r.Approve(ctx, ApprovalRequest{Tool: "exit_plan_mode", Summary: "Approve the plan and start the work?", Detail: plan})
			}
			if !approved {
				return "The user did not approve the plan. Ask what should change, or revise it; stay in plan mode.", nil
			}
			r.plan = false
			r.emit(Event{Kind: EventMode, Text: ModeCode, Summary: "plan approved"})
			return "Plan approved. Carry it out now.", nil
		},
	}
}

// ---- ask_user

// Asker answers a question from the agent; options may be empty (free
// text). An error means no user is available (headless).
type Asker func(ctx context.Context, question string, options []string) (string, error)

func askUserTool() Tool {
	return Tool{
		Name:        "ask_user",
		Description: "Ask the user one question when the request is genuinely ambiguous and the answer changes the work. Offer options when there are natural ones.",
		Schema: schema(map[string]any{
			"question": prop("string", "The question"),
			"options":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional choices"},
		}, "question"),
		Summary: func(a map[string]any) string { return "question: " + oneLine(str(a, "question"), 80) },
		Run: func(ctx context.Context, r *Runner, a map[string]any) (string, error) {
			q := strings.TrimSpace(str(a, "question"))
			if q == "" {
				return "", errors.New("question is empty")
			}
			var opts []string
			if raw, ok := a["options"].([]any); ok {
				for _, o := range raw {
					if s, _ := o.(string); strings.TrimSpace(s) != "" {
						opts = append(opts, strings.TrimSpace(s))
					}
				}
			}
			if r.Asker == nil {
				return "", errors.New("no user is available to answer (headless run); proceed with the most reasonable reading and say which you chose")
			}
			ans, err := r.Asker(ctx, q, opts)
			if err != nil {
				return "", err
			}
			r.emit(Event{Kind: EventQuestion, Text: q, Output: ans})
			return "The user answered: " + ans, nil
		},
	}
}

// ---- save_memory

func saveMemoryTool() Tool {
	return Tool{
		Name:        "save_memory",
		Description: "Remember a fact or preference the user states for future sessions (appended to the user's instruction file). Use it only for durable facts, never for secrets.",
		Schema:      schema(map[string]any{"fact": prop("string", "One short fact or preference")}, "fact"),
		Summary:     func(a map[string]any) string { return "remember: " + oneLine(str(a, "fact"), 80) },
		Run: func(_ context.Context, r *Runner, a map[string]any) (string, error) {
			fact := strings.TrimSpace(str(a, "fact"))
			if fact == "" {
				return "", errors.New("fact is empty")
			}
			if err := os.MkdirAll(filepath.Dir(r.Opts.MemoryFile), 0o700); err != nil {
				return "", err
			}
			f, err := os.OpenFile(r.Opts.MemoryFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
			if err != nil {
				return "", err
			}
			defer f.Close()
			if st, _ := f.Stat(); st != nil && st.Size() == 0 {
				_, _ = f.WriteString("# Memory\n\nFacts and preferences saved by the agent (edit freely).\n\n")
			}
			if _, err := fmt.Fprintf(f, "- %s (%s)\n", strings.ReplaceAll(fact, "\n", " "), time.Now().Format("2006-01-02")); err != nil {
				return "", err
			}
			return "saved to " + r.Opts.MemoryFile, nil
		},
	}
}

// ---- symbols (language server through Serena, when enabled)

func findSymbolTool() Tool {
	return Tool{
		Name:        "find_symbol",
		Description: "Find a symbol (function, type, method) by name with a language server: file, lines and optionally its body. More precise than grep.",
		Schema: schema(map[string]any{
			"name":         prop("string", "Symbol name or name path (Type/Method)"),
			"include_body": prop("boolean", "Return the symbol's source"),
			"within":       prop("string", "Restrict to a file or directory"),
		}, "name"),
		Summary: func(a map[string]any) string { return "symbol " + str(a, "name") },
		Run: func(ctx context.Context, r *Runner, a map[string]any) (string, error) {
			syms, err := r.Nav.FindSymbol(ctx, r.Opts.Root, str(a, "name"), repointel.FindOptions{IncludeBody: boolean(a, "include_body"), Within: str(a, "within")})
			if err != nil {
				return "", err
			}
			return formatSymbols(syms), nil
		},
	}
}

func findReferencesTool() Tool {
	return Tool{
		Name:        "find_references",
		Description: "List the places that reference a symbol (from find_symbol: its name_path and file).",
		Schema:      schema(map[string]any{"name_path": prop("string", "The symbol's name path"), "file": prop("string", "The file that defines it")}, "name_path", "file"),
		Summary:     func(a map[string]any) string { return "references of " + str(a, "name_path") },
		Run: func(ctx context.Context, r *Runner, a map[string]any) (string, error) {
			refs, err := r.Nav.References(ctx, r.Opts.Root, repointel.Symbol{NamePath: str(a, "name_path"), File: str(a, "file")})
			if err != nil {
				return "", err
			}
			if len(refs) == 0 {
				return "(no references)", nil
			}
			var b strings.Builder
			for _, ref := range refs {
				fmt.Fprintf(&b, "%s:%d in %s (%s): %s\n", ref.File, ref.Line, ref.Symbol, ref.Kind, oneLine(ref.Snippet, 160))
			}
			return strings.TrimRight(b.String(), "\n"), nil
		},
	}
}

func findImplementationsTool() Tool {
	return Tool{
		Name:        "find_implementations",
		Description: "List the implementations of an interface or interface method (from find_symbol: its name_path and file).",
		Schema:      schema(map[string]any{"name_path": prop("string", "The symbol's name path"), "file": prop("string", "The file that defines it")}, "name_path", "file"),
		Summary:     func(a map[string]any) string { return "implementations of " + str(a, "name_path") },
		Run: func(ctx context.Context, r *Runner, a map[string]any) (string, error) {
			syms, err := r.Nav.Implementations(ctx, r.Opts.Root, repointel.Symbol{NamePath: str(a, "name_path"), File: str(a, "file")})
			if err != nil {
				return "", err
			}
			return formatSymbols(syms), nil
		},
	}
}

func formatSymbols(syms []repointel.Symbol) string {
	if len(syms) == 0 {
		return "(no symbols)"
	}
	var b strings.Builder
	for _, s := range syms {
		fmt.Fprintf(&b, "%s %s  %s:%d-%d\n", s.Kind, s.NamePath, s.File, s.StartLine, s.EndLine)
		if s.Body != "" {
			b.WriteString(s.Body + "\n\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
