package direct

import (
	"regexp"
	"strings"
)

// Trust boundaries. Each message in a session records where its content
// came from (Provenance). Only two sources are trusted:
//
//   - the user's own words in a message they typed (not the files they
//     attach with @path, not input piped on stdin, not a hook's output);
//   - the runner's own messages (nudges, notes).
//
// Everything else is data, whatever it says: repository files and
// instruction files (AGENTS.md and the like guide the agent but cannot
// authorize anything), terminal output, web pages, MCP results and tool
// definitions, sub-agent reports, the model's own messages, summaries of
// the conversation, and results recalled after being set aside. Sensitive
// operations (guard.go) are authorized only by the user's words, the
// user's permission rules or the user's approval, never by such content
// or by the model's description of what it is doing.

// Provenance says where a message's content came from.
type Provenance struct {
	Source string `json:"source"`
	// Origin names the file, URL, command, server or tool it came from.
	Origin string `json:"origin,omitempty"`
	// Trusted is set only for the user's own words and the runner's own
	// messages.
	Trusted bool `json:"trusted,omitempty"`
}

// Sources of content.
const (
	SourceUser       = "user"       // what the user typed
	SourceRunner     = "runner"     // the runner's own messages
	SourceHook       = "hook"       // the user's configured hooks
	SourceModel      = "model"      // the model's messages
	SourceRepository = "repository" // files of the project read by tools
	SourceTerminal   = "terminal"   // command output
	SourceWeb        = "web"        // fetched pages and search results
	SourceMCP        = "mcp"        // MCP tool results
	SourceSubagent   = "subagent"   // a sub-agent's report
	SourceSummary    = "summary"    // a summary of earlier messages (derived from all of the above)
	SourceRecall     = "recall"     // a result brought back after being set aside
	SourceSkill      = "skill"      // skill and command templates (repository or user files)
	SourceTool       = "tool"       // other tool results
)

func trusted(source, origin string) *Provenance {
	return &Provenance{Source: source, Origin: origin, Trusted: true}
}

func untrusted(source, origin string) *Provenance {
	return &Provenance{Source: source, Origin: origin}
}

// resultProvenance is the provenance of a tool call's result.
func (r *Runner) resultProvenance(call ToolCall, args map[string]any) *Provenance {
	name := call.Function.Name
	switch {
	case name == "read" || name == "read_many" || name == "grep" || name == "glob" || name == "ls" || name == "notebook_read":
		o := str(args, "path")
		if o == "" {
			o = str(args, "pattern")
		}
		return untrusted(SourceRepository, name+" "+o)
	case name == "bash" || name == "bash_output":
		return untrusted(SourceTerminal, oneLine(str(args, "command"), 200))
	case name == "web_fetch":
		return untrusted(SourceWeb, str(args, "url"))
	case name == "web_search":
		return untrusted(SourceWeb, "search: "+str(args, "query"))
	case strings.HasPrefix(name, "mcp__"):
		return untrusted(SourceMCP, strings.TrimPrefix(name, "mcp__"))
	case name == "task":
		return untrusted(SourceSubagent, str(args, "subagent_type"))
	case name == "skill":
		return untrusted(SourceSkill, str(args, "name"))
	case name == "recall":
		id := str(args, "id")
		for _, m := range r.Session.Messages {
			if m.Role == "tool" && m.ToolCallID == id && m.Provenance != nil {
				return untrusted(SourceRecall, m.Provenance.Source+": "+m.Provenance.Origin)
			}
		}
		return untrusted(SourceRecall, id)
	}
	return untrusted(SourceTool, name)
}

var (
	stdinBlock  = regexp.MustCompile(`(?s)<stdin>.*?</stdin>`)
	fileBlock   = regexp.MustCompile(`(?s)<file path="[^"]*">.*?</file>`)
	quotedBlock = regexp.MustCompile("(?s)```.*?```")
)

// authorizingText is the part of a typed message that speaks for the
// user: without input piped on stdin, attached files or pasted code
// blocks, which are data the user passes along, not their own request.
func authorizingText(text string) string {
	text = stdinBlock.ReplaceAllString(text, " ")
	text = fileBlock.ReplaceAllString(text, " ")
	text = quotedBlock.ReplaceAllString(text, " ")
	return text
}
