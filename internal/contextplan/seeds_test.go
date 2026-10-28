package contextplan

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/task"
)

func seedTexts(ss []Seed) []string {
	var out []string
	for _, s := range ss {
		out = append(out, s.Kind+":"+s.Text)
	}
	return out
}

// TestDiagnosticLocatesOrigin: the quoted error is found where the code
// raises it, ahead of a generic sample identifier.
func TestDiagnosticLocatesOrigin(t *testing.T) {
	needRipgrep(t)
	w := lexicalWorktree(t, map[string]string{
		"dispenser.go": "package caddyfile\n\nfunc (d *Dispenser) ArgErr() error {\n\treturn d.Errf(\"wrong argument count or unexpected line ending after '%s'\", d.Val())\n}\n",
		"other.go":     "package caddyfile\n\nfunc NewRequest() {}\n",
	})
	req := "Shorthand matchers fail:\n```\nNewRequest()\nError: wrong argument count or unexpected line ending after 'expression', at Caddyfile:2\n```"
	in := Inputs{Task: &task.Task{ID: "t", OriginalRequest: req}, Worktrees: []task.Worktree{w}}
	var st IntelStats
	out := requestSymbols(context.Background(), in, &st)
	if st.DiagnosticHits != 1 || !strings.Contains(out, "raised at ./billing/dispenser.go:4") {
		t.Fatalf("diagnostic not located: %+v\n%s", st, out)
	}
	if i, j := strings.Index(out, "dispenser.go"), strings.Index(out, "other.go"); j >= 0 && j < i {
		t.Fatalf("sample identifier came before the diagnostic:\n%s", out)
	}
}

// TestCommonSampleNameDemoted: a name from a code sample that matches a large
// share of the repository is skipped; a rare domain term still falls back to
// lexical search.
func TestCommonSampleNameDemoted(t *testing.T) {
	needRipgrep(t)
	files := map[string]string{"lexer.go": "package l\n\n// heredoc handling\nfunc heredoc() {}\n"}
	for i := range 30 {
		files[fmt.Sprintf("t%d_test.go", i)] = "package l\n\nvar _ = NewRecorder\n"
	}
	w := lexicalWorktree(t, files)
	req := "The `heredoc` form is mis-parsed.\n```\nw := NewRecorder()\n```"
	in := Inputs{Task: &task.Task{ID: "t", OriginalRequest: req}, Worktrees: []task.Worktree{w}}
	var st IntelStats
	out := requestSymbols(context.Background(), in, &st)
	if len(st.Demoted) != 1 || st.Demoted[0] != "NewRecorder" || strings.Contains(out, "NewRecorder") {
		t.Fatalf("common sample name not demoted: %+v\n%s", st, out)
	}
	if !strings.Contains(out, "heredoc (lexical) ./billing/lexer.go") {
		t.Fatalf("domain term did not reach lexical fallback:\n%s", out)
	}
}

// TestSeedsFromLinksAndMentions: a link to a file names that file; URL
// fragments, hashes and @user names are not code.
func TestSeedsFromLinksAndMentions(t *testing.T) {
	req := "Reported by @someUser. The bug is in https://github.com/org/repo/blob/main/pkg/router/tree.go#L120 " +
		"and reproduces at https://play.example.com/#eNqAbCd. Thanks @anotherPerson."
	got := seedTexts(Seeds(req))
	if len(got) == 0 || got[0] != "name:pkg/router/tree.go" {
		t.Fatalf("file link not used: %v", got)
	}
	for _, g := range got {
		if strings.Contains(g, "someUser") || strings.Contains(g, "anotherPerson") || strings.Contains(g, "eNqAbCd") {
			t.Fatalf("URL or mention part used as a seed: %v", got)
		}
	}
}

// TestLexicalPrefersCodeOverDocsAndBuildOutput: changelogs, READMEs and
// built bundles mention every name; the implementation comes first.
func TestLexicalPrefersCodeOverDocsAndBuildOutput(t *testing.T) {
	needRipgrep(t)
	files := map[string]string{"lib/core/join.js": "function joinURL(a, b) {}\n", "CHANGELOG.md": "- fixed joinURL\n",
		"README.md": "Use joinURL.\n", "dist/bundle.js": "function joinURL(a, b) {}\n", "dist/bundle.js.map": "joinURL"}
	w := lexicalWorktree(t, files)
	in := Inputs{Task: &task.Task{ID: "t", OriginalRequest: "Fix `joinURL` for protocol-relative input"}, Worktrees: []task.Worktree{w}}
	var st IntelStats
	out := requestSymbols(context.Background(), in, &st)
	i, j, k := strings.Index(out, "lib/core/join.js"), strings.Index(out, "CHANGELOG.md"), strings.Index(out, "dist/bundle.js:")
	if i < 0 || (j >= 0 && j < i) || (k >= 0 && k < i) || strings.Contains(out, ".map") {
		t.Fatalf("implementation must come first, maps excluded:\n%s", out)
	}
}
