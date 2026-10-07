package frontier

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/contextplan"
	"github.com/akynte/boundedcode/internal/telemetry"

	"github.com/akynte/boundedcode/internal/sandbox"
)

// PathMap maps host directories to the names a packet shows instead (e.g.
// /home/u/.cache/.../repos/payment-service -> payment-service, the module
// cache -> $GOMODCACHE). Longer paths win, so nested locations map first.
type PathMap map[string]string

// BuildPacket turns a context pack into a compact escalation packet with a
// specific question. The pack is built with the frontier budget; host paths
// are rewritten (Sanitize) and the packet is redacted again. Callers must
// still run CheckPacket before sending: it is the fail-closed gate.
func BuildPacket(trigger Trigger, pack contextplan.Pack, question string, paths PathMap, home string) string {
	var b strings.Builder
	b.WriteString("You are a senior software architect advising a local coding agent. ")
	b.WriteString("You cannot run code or see the repository beyond what is below. ")
	b.WriteString("Answer concisely with: (1) diagnosis, (2) the concrete approach to take, (3) pitfalls/edge cases, ")
	b.WriteString("(4) how to verify. Prefer precise instructions the local agent can implement. Do not write the whole implementation.\n\n")
	fmt.Fprintf(&b, "ESCALATION %s: %s\n\n", trigger.Code, trigger.Reason)
	for _, s := range pack.Sections {
		if s.Key == "rules" {
			continue
		}
		fmt.Fprintf(&b, "## %s\n%s\n\n", s.Title, s.Body)
	}
	fmt.Fprintf(&b, "## SPECIFIC QUESTION\n%s\n", question)
	return telemetry.Redact(Sanitize(b.String(), paths, home))
}

// Sanitize removes host filesystem locations from text bound for a remote
// model. Every packet section can carry them: verification output, stack
// traces, compiler errors, tool output and the agent's own diff.
//
//   - Each mapped directory, and its symlink-resolved form, is replaced by
//     its name, longest first.
//   - Anything left under home becomes $HOME (the user name and layout above
//     it are not sent; the rest of the path stays useful).
//
// Replacement only happens at path boundaries, so /home/dev never rewrites
// /home/devon, and repository-relative paths are untouched. JSON-escaped
// (\/home\/u) and URL-encoded (%2Fhome%2Fu) spellings are handled too.
func Sanitize(text string, paths PathMap, home string) string {
	type rule struct {
		from, to string
		fold     bool // Windows spellings: case-insensitive
	}
	var rules []rule
	add := func(from, to string) {
		for _, sp := range spellings(from) {
			rules = append(rules, rule{sp.text, to, sp.fold})
		}
		if real, err := filepath.EvalSymlinks(from); err == nil && real != from {
			for _, sp := range spellings(real) {
				rules = append(rules, rule{sp.text, to, sp.fold})
			}
		}
	}
	for from, to := range paths {
		add(from, to)
	}
	if home != "" {
		add(home, "$HOME")
	}
	sort.SliceStable(rules, func(i, j int) bool { return len(rules[i].from) > len(rules[j].from) })
	for _, r := range rules {
		text = replaceAtBoundary(text, r.from, r.to, r.fold)
	}
	return text
}

// spelling is one way a host path appears in tool output.
type spelling struct {
	text string
	fold bool
}

// spellings are the forms a host path takes in tool output. A Unix path
// appears plain, JSON-escaped (\/home\/u) and URL-encoded (%2Fhome%2Fu). A
// Windows path also appears with either slash, JSON-escaped (C:\\Users),
// and as the sandbox mounts it (/host/c/Users/...); Windows matches paths
// without case, so these compare case-insensitively.
func spellings(p string) []spelling {
	p = strings.TrimRight(p, `/\`)
	if p == "" {
		return nil
	}
	if !windowsPath(p) {
		if !strings.HasPrefix(p, "/") {
			return nil
		}
		return []spelling{{p, false}, {strings.ReplaceAll(p, "/", `\/`), false}, {strings.ReplaceAll(p, "/", "%2F"), false}}
	}
	back := strings.ReplaceAll(p, "/", `\`)
	slash := strings.ReplaceAll(p, `\`, "/")
	inBox := sandbox.ContainerPathFor("windows", p)
	out := []spelling{{back, true}, {slash, true}, {strings.ReplaceAll(back, `\`, `\\`), true}, {strings.ReplaceAll(slash, "/", "%2F"), true}}
	for _, s := range []string{inBox, strings.ReplaceAll(inBox, "/", `\/`)} {
		out = append(out, spelling{s, true})
	}
	return out
}

// windowsPath reports a drive-letter or UNC path.
func windowsPath(p string) bool {
	return len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') &&
		('a' <= p[0] && p[0] <= 'z' || 'A' <= p[0] && p[0] <= 'Z') || strings.HasPrefix(p, `\\`)
}

// replaceAtBoundary replaces from with to where from is a whole path prefix:
// the next character is not one that continues a path component.
func replaceAtBoundary(text, from, to string, fold bool) string {
	if from == "" {
		return text
	}
	var b strings.Builder
	for {
		i := index(text, from, fold)
		if i < 0 {
			b.WriteString(text)
			return b.String()
		}
		end := i + len(from)
		b.WriteString(text[:i])
		if end < len(text) && continuesComponent(text[end]) {
			b.WriteString(text[i:end])
		} else {
			b.WriteString(to)
		}
		text = text[end:]
	}
}

// index is strings.Index, case-insensitive (ASCII) when fold is set.
func index(s, sub string, fold bool) int {
	if !fold {
		return strings.Index(s, sub)
	}
	return strings.Index(strings.ToLower(s), strings.ToLower(sub))
}

func continuesComponent(c byte) bool {
	return c == '.' || c == '-' || c == '_' || c == '@' || c == '+' || c == '~' ||
		'0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// CheckPacket refuses packets that still reveal the host home directory, in
// any of the spellings Sanitize handles. It is the fail-closed gate after
// sanitizing; the error names where the residue was found.
func CheckPacket(packet, home string) error {
	for _, sp := range spellings(home) {
		for off := 0; ; {
			i := index(packet[off:], sp.text, sp.fold)
			if i < 0 {
				break
			}
			i += off
			end := i + len(sp.text)
			if end >= len(packet) || !continuesComponent(packet[end]) {
				lo, hi := max(0, i-40), min(len(packet), end+60)
				return fmt.Errorf("frontier packet still contains the host home directory %q (near %q); refusing to send", home, packet[lo:hi])
			}
			off = end
		}
	}
	return nil
}

// DefaultQuestion returns the question for a trigger.
func DefaultQuestion(t Trigger) string {
	switch t.Code {
	case Z1:
		return "Before the local agent implements this, what is the correct design? Identify contract, consistency, idempotency or security risks and the safest implementation plan."
	case Z2:
		return "The local agent repeatedly failed verification. What is the root cause of the failures shown, and what different approach will make the acceptance criteria pass?"
	case Z3:
		return "Review this change before merge. List concrete correctness or security problems (with file and line) that must be fixed, or state that it is acceptable."
	default:
		return "Review the task state and advise the local agent on the best next step."
	}
}
