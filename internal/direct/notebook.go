package direct

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Jupyter notebooks: read as cells, edit by cell.

type notebook struct {
	raw   map[string]any
	cells []map[string]any
}

func loadNotebook(abs string) (*notebook, error) {
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("not a notebook (invalid JSON): %w", err)
	}
	nb := &notebook{raw: raw}
	for _, c := range asSlice(raw["cells"]) {
		if m, ok := c.(map[string]any); ok {
			nb.cells = append(nb.cells, m)
		}
	}
	return nb, nil
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func cellSource(c map[string]any) string {
	switch s := c["source"].(type) {
	case string:
		return s
	case []any:
		var b strings.Builder
		for _, p := range s {
			if ps, ok := p.(string); ok {
				b.WriteString(ps)
			}
		}
		return b.String()
	}
	return ""
}

func cellOutputs(c map[string]any) string {
	var b strings.Builder
	for _, o := range asSlice(c["outputs"]) {
		m, _ := o.(map[string]any)
		switch m["output_type"] {
		case "stream":
			b.WriteString(cellText(m["text"]))
		case "execute_result", "display_data":
			if d, ok := m["data"].(map[string]any); ok {
				if t, ok := d["text/plain"]; ok {
					b.WriteString(cellText(t))
				} else {
					for k := range d {
						fmt.Fprintf(&b, "[%s output]\n", k)
					}
				}
			}
		case "error":
			fmt.Fprintf(&b, "%v: %v\n", m["ename"], m["evalue"])
		}
	}
	return b.String()
}

func cellText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		var b strings.Builder
		for _, p := range t {
			if s, ok := p.(string); ok {
				b.WriteString(s)
			}
		}
		return b.String()
	}
	return ""
}

// notebookText renders a notebook for read.
func notebookText(abs, rel string) (string, error) {
	nb, err := loadNotebook(abs)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "notebook %s: %d cells\n", rel, len(nb.cells))
	for i, c := range nb.cells {
		id, _ := c["id"].(string)
		fmt.Fprintf(&b, "\n--- cell %d (%v%s) ---\n%s\n", i, c["cell_type"], idSuffix(id), strings.TrimRight(cellSource(c), "\n"))
		if out := strings.TrimSpace(cellOutputs(c)); out != "" {
			if len(out) > 2000 {
				out = out[:2000] + "…"
			}
			fmt.Fprintf(&b, "[output]\n%s\n", out)
		}
	}
	return clipOutput(b.String()), nil
}

func idSuffix(id string) string {
	if id == "" {
		return ""
	}
	return ", id " + id
}

func notebookReadTool() Tool {
	return Tool{
		Name:        "notebook_read",
		Description: "Read a Jupyter notebook (.ipynb) as numbered cells with their outputs.",
		Schema:      schema(map[string]any{"path": prop("string", "Notebook path")}, "path"),
		Summary:     func(a map[string]any) string { return "notebook " + str(a, "path") },
		Run: func(_ context.Context, r *Runner, a map[string]any) (string, error) {
			abs, rel, err := r.resolve(str(a, "path"))
			if err != nil {
				return "", err
			}
			if err := r.checkReadable(abs); err != nil {
				return "", err
			}
			return notebookText(abs, rel)
		},
	}
}

func notebookEditTool() Tool {
	return Tool{
		Name:        "notebook_edit",
		Description: "Edit a Jupyter notebook cell: replace its source, insert a new cell after it, or delete it. Cells are addressed by index (from notebook_read) or id.",
		Schema: schema(map[string]any{
			"path":      prop("string", "Notebook path"),
			"cell":      prop("string", "Cell index (0-based) or cell id"),
			"mode":      map[string]any{"type": "string", "enum": []string{"replace", "insert", "delete"}, "description": "replace (default), insert after the cell (or at 0 with cell -1), delete"},
			"source":    prop("string", "New cell source (replace, insert)"),
			"cell_type": map[string]any{"type": "string", "enum": []string{"code", "markdown"}, "description": "Type for an inserted cell (default code)"},
		}, "path", "cell"),
		Mutates: true,
		Summary: func(a map[string]any) string {
			mode := str(a, "mode")
			if mode == "" {
				mode = "replace"
			}
			return fmt.Sprintf("notebook %s cell %s in %s", mode, str(a, "cell"), str(a, "path"))
		},
		Consent: mutationConsent(func(a map[string]any) string {
			return fmt.Sprintf("%s cell %s of %s:\n\n%s", firstNonEmpty(str(a, "mode"), "replace"), str(a, "cell"), str(a, "path"), str(a, "source"))
		}),
		Run: func(_ context.Context, r *Runner, a map[string]any) (string, error) {
			abs, rel, err := r.resolve(str(a, "path"))
			if err != nil {
				return "", err
			}
			if err := r.checkReadable(abs); err != nil {
				return "", err
			}
			nb, err := loadNotebook(abs)
			if err != nil {
				return "", err
			}
			idx := -2
			sel := strings.TrimSpace(str(a, "cell"))
			if n, err := fmt.Sscanf(sel, "%d", &idx); n != 1 || err != nil {
				idx = -2
				for i, c := range nb.cells {
					if id, _ := c["id"].(string); id == sel {
						idx = i
					}
				}
			}
			mode := str(a, "mode")
			if mode == "" {
				mode = "replace"
			}
			if idx == -2 || idx >= len(nb.cells) || (idx < 0 && (mode != "insert" || idx != -1)) {
				return "", fmt.Errorf("no cell %q (the notebook has %d cells)", sel, len(nb.cells))
			}
			source := str(a, "source")
			lines := splitKeep(source)
			switch mode {
			case "replace":
				nb.cells[idx]["source"] = lines
				if nb.cells[idx]["cell_type"] == "code" {
					nb.cells[idx]["outputs"] = []any{}
					nb.cells[idx]["execution_count"] = nil
				}
			case "insert":
				ct := str(a, "cell_type")
				if ct == "" {
					ct = "code"
				}
				cell := map[string]any{"cell_type": ct, "metadata": map[string]any{}, "source": lines}
				if ct == "code" {
					cell["outputs"] = []any{}
					cell["execution_count"] = nil
				}
				at := idx + 1
				nb.cells = append(nb.cells[:at], append([]map[string]any{cell}, nb.cells[at:]...)...)
			case "delete":
				nb.cells = append(nb.cells[:idx], nb.cells[idx+1:]...)
			default:
				return "", errors.New("mode must be replace, insert or delete")
			}
			cells := make([]any, 0, len(nb.cells))
			for _, c := range nb.cells {
				cells = append(cells, c)
			}
			nb.raw["cells"] = cells
			out, err := json.MarshalIndent(nb.raw, "", " ")
			if err != nil {
				return "", err
			}
			if err := os.WriteFile(abs, append(out, '\n'), 0o644); err != nil {
				return "", err
			}
			return fmt.Sprintf("%s cell %s in %s (%d cells now)", mode, sel, rel, len(nb.cells)), nil
		},
	}
}

// splitKeep splits source into the line list notebooks use (newlines kept).
func splitKeep(s string) []any {
	var out []any
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			out = append(out, s)
			break
		}
		out = append(out, s[:i+1])
		s = s[i+1:]
	}
	return out
}
