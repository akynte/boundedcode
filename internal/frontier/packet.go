package frontier

import (
	"fmt"
	"strings"

	"github.com/akynte/boundedcode/internal/contextplan"
	"github.com/akynte/boundedcode/internal/telemetry"
)

// BuildPacket turns a context pack into a compact escalation packet with a
// specific question. The pack is built with the frontier budget; the packet
// is redacted again as defence in depth.
func BuildPacket(trigger Trigger, pack contextplan.Pack, question string) string {
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
	return telemetry.Redact(b.String())
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
