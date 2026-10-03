package frontier

import (
	"fmt"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/contextplan"
	"github.com/akynte/boundedcode/internal/telemetry"
)

// PathMap rewrites host paths to workspace-relative names in packets
// (e.g. /home/u/.cache/.../repos/payment-service -> payment-service).
type PathMap map[string]string

// BuildPacket turns a context pack into a compact escalation packet with a
// specific question. The pack is built with the frontier budget; host paths
// are rewritten via paths and the packet is redacted again.
func BuildPacket(trigger Trigger, pack contextplan.Pack, question string, paths PathMap) string {
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
	out := b.String()
	// Longest prefixes first so nested paths map correctly.
	keys := make([]string, 0, len(paths))
	for k := range paths {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, k := range keys {
		out = strings.ReplaceAll(out, k, paths[k])
	}
	return telemetry.Redact(out)
}

// CheckPacket refuses packets that still reveal host filesystem locations.
func CheckPacket(packet, home string) error {
	if home != "" && strings.Contains(packet, home) {
		return fmt.Errorf("frontier packet still contains the host home directory %q; refusing to send", home)
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
