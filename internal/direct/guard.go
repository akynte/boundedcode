package direct

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/akynte/boundedcode/internal/policy"
)

// The guard authorizes sensitive operations (policy.Finding) before a tool
// runs, in every approval mode, auto included: auto skips the prompts for
// ordinary work, not for these. A finding is authorized by, in order:
//
//  1. the user's permission rules: a deny rule refuses, an allow rule such
//     as network(api.github.com), install(lodash), credentials(.env) or
//     instructions(AGENTS.md) authorizes;
//  2. the user's own words in this turn's message (authorizingText):
//     the package, host or file named there, or "remember …" for memory;
//  3. the user's approval when asked.
//
// Nothing the model says and nothing a tool returned can authorize: the
// decision reads the operation itself, the user's configuration and the
// user's words. Without someone to ask (a script, a sub-process), an
// unauthorized operation is refused. Every decision is reported as an
// EventGuard for the audit log.

// EventGuard reports a guard decision; Args holds category, subject,
// decision (authorized | approved | refused) and by (rule | message |
// approval | no approver | not approved | deny rule).
const EventGuard EventKind = "guard"

// findings returns the sensitive aspects of a tool call.
func (r *Runner) findings(t *Tool, args map[string]any) []policy.Finding {
	switch t.Name {
	case "bash":
		return policy.ClassifyCommand(str(args, "command"), r.commandContext())
	case "write", "edit", "multi_edit", "notebook_edit":
		abs, _, err := r.resolve(str(args, "path"))
		if err != nil {
			return nil
		}
		return policy.ClassifyPath(r.rootRel(abs))
	case "save_memory":
		return []policy.Finding{{Category: policy.CatInstructions, Subject: "memory",
			Reason: "it adds to your instruction file, which every future session reads"}}
	case "web_fetch":
		h := hostOfURL(str(args, "url"))
		if r.Opts.Web.hostAllowed(h) {
			return nil
		}
		return []policy.Finding{{Category: policy.CatNetwork, Subject: h, Reason: "the URL, and anything in it, goes to " + h}}
	case "web_search":
		return []policy.Finding{{Category: policy.CatNetwork, Subject: "web search", Reason: "the query goes to a search engine"}}
	}
	return nil
}

// commandContext locates commands for classification.
func (r *Runner) commandContext() policy.CommandContext {
	root, err := filepath.EvalSymlinks(r.Opts.Root)
	if err != nil {
		root = r.Opts.Root
	}
	dir, err := filepath.EvalSymlinks(r.cwd())
	if err != nil {
		dir = r.cwd()
	}
	home, _ := os.UserHomeDir()
	return policy.CommandContext{Root: root, Dir: dir, Home: home, ConfigDir: r.Opts.UserDir,
		Written: r.wroteInSession, ReadFile: os.ReadFile}
}

// noteWritten records a file the agent wrote, so that running it later is
// judged by its contents.
func (r *Runner) noteWritten(abs string) {
	if r.writtenBy != nil {
		r.writtenBy.noteWritten(abs)
		return
	}
	r.writtenMu.Lock()
	defer r.writtenMu.Unlock()
	if r.written == nil {
		r.written = map[string]bool{}
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	r.written[filepath.Clean(abs)] = true
}

func (r *Runner) wroteInSession(abs string) bool {
	if r.writtenBy != nil {
		return r.writtenBy.wroteInSession(abs)
	}
	r.writtenMu.Lock()
	defer r.writtenMu.Unlock()
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	return r.written[filepath.Clean(abs)]
}

// guard authorizes the findings of a tool call. It returns whether the
// call may run, whether the user approved it (so it is not asked again),
// and, when refused, the reason for the model.
func (r *Runner) guard(ctx context.Context, tool, summary string, fs []policy.Finding) (ok, approved bool, refusal string) {
	var need []policy.Finding
	for _, f := range fs {
		spec := string(f.Category) + "(" + f.Subject + ")"
		if why, denied := r.Opts.Permissions.Denied(spec); denied {
			r.guardEvent(tool, f, "refused", "deny rule")
			return false, false, fmt.Sprintf("refused: %s %s (%s)", f.Category, f.Subject, why)
		}
		switch {
		case r.Opts.Permissions.Allowed(spec):
			r.guardEvent(tool, f, "authorized", "rule")
		case r.namedByUser(f):
			r.guardEvent(tool, f, "authorized", "message")
		default:
			need = append(need, f)
		}
	}
	if len(need) == 0 {
		return true, false, ""
	}
	var b strings.Builder
	for _, f := range need {
		fmt.Fprintf(&b, "- %s: %s", f.Category, f.Reason)
		if f.Subject != "" {
			fmt.Fprintf(&b, " [%s]", f.Subject)
		}
		b.WriteString("\n")
	}
	if r.Approve == nil {
		for _, f := range need {
			r.guardEvent(tool, f, "refused", "no approver")
		}
		return false, false, "refused: this needs the user's authorization, and no one can be asked here:\n" + b.String() +
			"The user can approve it in an interactive session, name it in their message, or allow it with a permission rule such as " +
			ruleExample(need[0]) + "."
	}
	detail := "This is sensitive, and nothing the user wrote or configured authorizes it:\n" + b.String() + "\n" + summary
	if !r.Approve(ctx, ApprovalRequest{Tool: tool, Summary: summary, Detail: detail}) {
		for _, f := range need {
			r.guardEvent(tool, f, "refused", "not approved")
		}
		return false, false, "refused: the user did not approve this (" + string(need[0].Category) + "). Do not retry it; ask the user or take another approach."
	}
	for _, f := range need {
		r.guardEvent(tool, f, "approved", "approval")
	}
	return true, true, ""
}

func ruleExample(f policy.Finding) string {
	s := f.Subject
	if s == "" {
		s = "*"
	}
	return string(f.Category) + "(" + s + ")"
}

func (r *Runner) guardEvent(tool string, f policy.Finding, decision, by string) {
	r.emit(Event{Kind: EventGuard, Tool: tool, Summary: string(f.Category) + " " + f.Subject, Text: f.Reason,
		Args: map[string]any{"category": string(f.Category), "subject": f.Subject, "decision": decision, "by": by}})
}

var memoryRequest = regexp.MustCompile(`(?i)\b(remember|memori[sz]e|save (this|that|it)|keep in mind|note (this|that) (down|for later)|add (this|that|it) to (your |the )?memory|from now on)\b`)

// namedByUser reports whether the user's own words in this turn name what
// a finding concerns.
func (r *Runner) namedByUser(f policy.Finding) bool {
	text := r.authText
	if strings.TrimSpace(text) == "" {
		return false
	}
	switch {
	case f.Category == policy.CatInstructions && f.Subject == "memory":
		return memoryRequest.MatchString(text)
	case f.Category == policy.CatPrivilege:
		return mentions(text, f.Subject)
	case f.Category == policy.CatNetwork && f.Subject == "web search":
		return regexp.MustCompile(`(?i)\b(search|look (it |this )?up|google|online|on the web|the internet)\b`).MatchString(text)
	case f.Subject == "" || f.Subject == "*":
		return false
	case f.Category == policy.CatCredentials && f.Subject == "environment":
		return regexp.MustCompile(`(?i)\b(env|environment( variables?)?|printenv)\b`).MatchString(text)
	case f.Category == policy.CatInstall:
		for _, p := range strings.Fields(f.Subject) {
			if !mentions(text, p) {
				return false
			}
		}
		return true
	case f.Category == policy.CatNetwork:
		return mentions(text, f.Subject)
	}
	// A path: the user names it, or its file name.
	return mentions(text, f.Subject) || mentions(text, filepath.Base(f.Subject))
}

// mentions reports s as a whole word of text (case-insensitive).
func mentions(text, s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	re, err := regexp.Compile(`(?i)(^|[^\w.@/~-])` + regexp.QuoteMeta(s) + `($|[^\w@-]|\.[^\w])`)
	return err == nil && re.MatchString(text+" ")
}

func hostOfURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}
