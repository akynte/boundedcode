package frontier

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/contextplan"
	"github.com/akynte/boundedcode/internal/telemetry"
)

// What a packet may carry (frontier.data).
const (
	// DataFull: the context pack as the local agent sees it, with code,
	// the diff, contracts and decisions (sanitized and redacted).
	DataFull = "full"
	// DataAbstract: the problem without the source. The task, the
	// verification failures and the strategies tried, with every line that
	// repeats a line of the repositories withheld, and the change as files
	// and line counts. Identifiers, paths and error messages remain: an
	// abstract packet still describes the code, so it is not proof that
	// confidential source cannot be inferred from it.
	DataAbstract = "abstract"
)

// SourceLines are a repository's lines, normalized, that an abstract
// packet must not repeat.
type SourceLines map[string]bool

// minSourceLine is the shortest line compared: shorter ones ("}", "return
// nil", "end") are common to every program and identify nothing.
const minSourceLine = 16

func normLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// CollectSourceLines reads the text files git tracks (or would, untracked
// but not ignored) under each root, skipping files over maxBytes.
func CollectSourceLines(ctx context.Context, roots []string, maxBytes int64) (SourceLines, error) {
	src := SourceLines{}
	for _, root := range roots {
		out, err := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", root, err)
		}
		for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
			if rel == "" {
				continue
			}
			p := filepath.Join(root, rel)
			st, err := os.Lstat(p)
			if err != nil || !st.Mode().IsRegular() || st.Size() > maxBytes {
				continue
			}
			b, err := os.ReadFile(p)
			if err != nil || bytes.IndexByte(b, 0) >= 0 {
				continue // unreadable or binary
			}
			sc := bufio.NewScanner(bytes.NewReader(b))
			sc.Buffer(make([]byte, 64<<10), 1<<20)
			for sc.Scan() {
				if l := normLine(sc.Text()); len(l) >= minSourceLine {
					src[l] = true
				}
			}
		}
	}
	return src, nil
}

// repeats reports whether a packet line repeats a source line, as itself
// or as a diff line (a leading +, - or space).
func (s SourceLines) repeats(line string) bool {
	l := normLine(line)
	if len(l) < minSourceLine {
		return false
	}
	if s[l] {
		return true
	}
	if len(line) > 0 && strings.ContainsRune("+- ", rune(line[0])) {
		if t := normLine(line[1:]); len(t) >= minSourceLine && s[t] {
			return true
		}
	}
	return false
}

// withhold replaces the lines of text that repeat source lines.
func (s SourceLines) withhold(text string) (string, int) {
	lines := strings.Split(text, "\n")
	n := 0
	for i, l := range lines {
		if s.repeats(l) {
			lines[i] = "[a line of the repository's source, withheld]"
			n++
		}
	}
	return strings.Join(lines, "\n"), n
}

// abstractSections are the pack sections an abstract packet keeps; the
// diff becomes an outline, and code, contracts, impact (which quotes
// call sites) and architecture decisions are left out.
var abstractSections = map[string]bool{"task": true, "verification": true, "rejected": true, "advice": true}

// BuildAbstractPacket is BuildPacket for DataAbstract.
func BuildAbstractPacket(trigger Trigger, pack contextplan.Pack, question string, paths PathMap, home string, src SourceLines) string {
	var b strings.Builder
	b.WriteString("You are a senior software architect advising a local coding agent. ")
	b.WriteString("You cannot run code or see the repository, and this request shows none of its source: the problem is described ")
	b.WriteString("by the task, the verification failures and what was tried. Answer concisely with: (1) the likely cause, (2) the approach to take, ")
	b.WriteString("(3) pitfalls/edge cases, (4) how to verify. Give precise instructions the local agent can implement.\n\n")
	fmt.Fprintf(&b, "ESCALATION %s: %s\n\n", trigger.Code, trigger.Reason)
	for _, s := range pack.Sections {
		switch {
		case s.Key == "diff":
			if o := diffOutline(s.Body); o != "" {
				fmt.Fprintf(&b, "## CHANGES SO FAR (files and line counts; content withheld)\n%s\n\n", o)
			}
		case abstractSections[s.Key]:
			body, _ := src.withhold(s.Body)
			fmt.Fprintf(&b, "## %s\n%s\n\n", s.Title, body)
		}
	}
	fmt.Fprintf(&b, "## SPECIFIC QUESTION\n%s\n", question)
	out := telemetry.Redact(Sanitize(b.String(), paths, home))
	// Sanitizing can join text into a line that repeats the source; check
	// again after it.
	out, _ = src.withhold(out)
	return out
}

// diffOutline turns a unified diff into one line per file: its path and
// the lines added and removed.
func diffOutline(diff string) string {
	type count struct{ add, del int }
	files := map[string]*count{}
	var order []string
	cur := ""
	for _, l := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "+++ "):
			cur = strings.TrimPrefix(strings.TrimPrefix(l, "+++ "), "b/")
			if files[cur] == nil {
				files[cur] = &count{}
				order = append(order, cur)
			}
		case strings.HasPrefix(l, "--- "), strings.HasPrefix(l, "diff --git"):
		case cur != "" && strings.HasPrefix(l, "+"):
			files[cur].add++
		case cur != "" && strings.HasPrefix(l, "-"):
			files[cur].del++
		}
	}
	sort.Strings(order)
	var out []string
	for _, f := range order {
		out = append(out, fmt.Sprintf("- %s: +%d -%d", f, files[f].add, files[f].del))
	}
	return strings.Join(out, "\n")
}

// CheckAbstract is the fail-closed gate for DataAbstract: it refuses a
// packet that still repeats a source line, naming the first.
func CheckAbstract(packet string, src SourceLines) error {
	for i, l := range strings.Split(packet, "\n") {
		if src.repeats(l) {
			return fmt.Errorf("line %d of the packet repeats a line of the repository's source (%q)", i+1, trunc(normLine(l), 60))
		}
	}
	return nil
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
