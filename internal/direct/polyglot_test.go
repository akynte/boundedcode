package direct

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/lang"
)

// Working in a repository of many languages: every file is edited and
// searched the same way; each language gets its check, or one note saying
// why it is not checked (no check exists, or its tool is missing), never
// silence that reads as "passed"; symbols are found in any language.
func TestPolyglotSession(t *testing.T) {
	lang.Register(lang.Language{ID: "nimtest2", Name: "Nim", Kind: lang.KindProgramming, Extensions: []string{".nimt"},
		Definitions: []*regexp.Regexp{regexp.MustCompile(`^proc\s+(?P<name>\w+)`)},
		Check:       &lang.Check{What: "nim check", Tools: []string{"nim-not-installed-anywhere"}, Args: []string{"check", "{file}"}}})
	up := &scripted{replies: []reply{
		{calls: []call{{"write", map[string]any{"path": "ml/score.py", "content": "def score(xs):\n    return sum(xs)\n"}}}},
		{calls: []call{{"write", map[string]any{"path": "engine/src/lib.rs", "content": "pub fn restock(n: u32) -> u32 {\n    n + 1\n}\n"}}}},
		{calls: []call{{"write", map[string]any{"path": "engine/src/more.rs", "content": "pub fn other() {}\n"}}}},
		{calls: []call{{"write", map[string]any{"path": "db/001.sql", "content": "CREATE TABLE orders (id BIGINT);\n"}}}},
		{calls: []call{{"write", map[string]any{"path": "web/user.ts", "content": "export interface User { id: string }\n"}}}},
		{calls: []call{{"write", map[string]any{"path": "web/tsconfig.json", "content": "{\n  // comments are fine\n  \"compilerOptions\": {}\n}\n"}}}},
		{calls: []call{{"write", map[string]any{"path": "app/main.nimt", "content": "proc greet() =\n  echo 1\n"}}}},
		{calls: []call{{"write", map[string]any{"path": "notes.weird", "content": "plain text in no known format\n"}}}},
		{calls: []call{{"edit", map[string]any{"path": "notes.weird", "old_string": "plain", "new_string": "edited"}}}},
		{calls: []call{{"find_symbol", map[string]any{"name": "restock", "include_body": true}}}},
		{calls: []call{{"find_symbol", map[string]any{"name": "orders"}}}},
		{calls: []call{{"find_references", map[string]any{"name_path": "greet", "file": "app/main.nimt"}}}},
		{text: "done"},
	}}
	r, root, _ := newRunner(t, up, ApprovalAuto)
	r.Opts.AllowRisky = true // .sql is a risky path; routing is not what this test is about
	writeFile(t, root, "app/use.nimt", "greet()\n")
	if _, err := r.Send(context.Background(), "work across the languages"); err != nil {
		t.Fatal(err)
	}
	var res []string
	for _, m := range r.Session.Messages {
		if m.Role == "tool" {
			res = append(res, m.Content)
		}
	}
	if len(res) != 12 {
		t.Fatalf("%d results", len(res))
	}
	if _, err := exec.LookPath("python3"); err == nil && !strings.Contains(res[0], "check: ok (python parse)") {
		t.Errorf("python: %q", res[0])
	}
	if !strings.Contains(res[1], "Rust files are not checked after edits (no per-file check: cargo check") {
		t.Errorf("rust, first edit: %q", res[1])
	}
	if strings.Contains(res[2], "check:") {
		t.Errorf("rust, second edit: the note is said once: %q", res[2])
	}
	if strings.Contains(res[3], "check:") || strings.Contains(res[5], "check:") {
		t.Errorf("SQL and JSON with comments are data, with no note, and tsconfig.json is not strict JSON: %q %q", res[3], res[5])
	}
	if !strings.Contains(res[4], "TypeScript files are not checked after edits") {
		t.Errorf("typescript: %q", res[4])
	}
	if !strings.Contains(res[6], "Nim files are not checked here: nim-not-installed-anywhere is not installed (missing tool") {
		t.Errorf("nim (missing tool): %q", res[6])
	}
	if strings.Contains(res[7], "check") || !strings.Contains(res[8], "1 replacement") {
		t.Errorf("unknown text: %q %q", res[7], res[8])
	}
	if b, _ := os.ReadFile(root + "/notes.weird"); string(b) != "edited text in no known format\n" {
		t.Errorf("unknown text not edited: %q", b)
	}
	if !strings.Contains(res[9], "engine/src/lib.rs:1-3") || !strings.Contains(res[9], "n + 1") {
		t.Errorf("rust symbol: %q", res[9])
	}
	if !strings.Contains(res[10], "db/001.sql:1") {
		t.Errorf("sql symbol: %q", res[10])
	}
	if !strings.Contains(res[11], "app/use.nimt:1") {
		t.Errorf("references in a registered language: %q", res[11])
	}
}
