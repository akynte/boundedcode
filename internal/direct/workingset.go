package direct

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/akynte/boundedcode/internal/contextplan"
	"github.com/akynte/boundedcode/internal/inference"
)

// The working set is the part of the history the model is asked to attend
// to. It is sized for the model, not for the context window: a small local
// model loses track of a 100K-token history long before the window is
// full, and every token of it is re-processed whenever the prefix changes.
// Before each model call the oldest large tool results are elided to a
// one-line placeholder until the history fits (elideIfNeeded); an elided
// result stays in the session and comes back through the recall tool.
// Only when eliding is not enough is the history summarised (compact).

const (
	// defaultWorkingSetLocal is the budget for a local model.
	defaultWorkingSetLocal = 32 << 10
	// defaultWorkingSetCloud is the budget for a cloud provider.
	defaultWorkingSetCloud = 96 << 10
	// keepRecentResults is how many of the newest tool results stay
	// verbatim: what the model is working with right now.
	keepRecentResults = 2
	// elideMin is the smallest tool result worth eliding.
	elideMin = 600
	// defaultToolOutputLocal and defaultToolOutputCloud cap one tool
	// result for the model (bytes); larger results are cut in the middle.
	defaultToolOutputLocal = 16 << 10
	defaultToolOutputCloud = 48 << 10
)

// workingSetTokens is the history budget: direct.working_set_tokens, or
// a default by provider, never above three quarters of the window.
func (r *Runner) workingSetTokens() int {
	ceiling := r.contextTokens() * 3 / 4
	if r.Opts.WorkingSetTokens > 0 {
		return min(r.Opts.WorkingSetTokens, ceiling)
	}
	if r.localProvider() {
		return min(defaultWorkingSetLocal, ceiling)
	}
	return min(defaultWorkingSetCloud, ceiling)
}

// maxToolOutput caps one tool result for the model: direct.max_tool_output
// or a default by provider.
func (r *Runner) maxToolOutput() int {
	if r.Opts.MaxToolOutput > 0 {
		return r.Opts.MaxToolOutput
	}
	if r.localProvider() {
		return defaultToolOutputLocal
	}
	return defaultToolOutputCloud
}

func (r *Runner) localProvider() bool {
	return r.Gateway == nil || r.Gateway.Provider() == inference.ProviderLocal
}

// clip cuts a tool result to the runner's cap, keeping the head and the
// tail (a test run's failures are at the end).
func (r *Runner) clip(s string) string {
	return clipTo(s, r.maxToolOutput())
}

func clipTo(s string, n int) string {
	if len(s) <= n {
		return s
	}
	head := s[:n*2/3]
	tail := s[len(s)-n/3:]
	return head + fmt.Sprintf("\n… [%d bytes cut] …\n", len(s)-len(head)-len(tail)) + tail
}

// elideIfNeeded replaces the oldest large tool results with placeholders
// until the history is within the working set (with some headroom, so it
// does not run every turn). Only results the model has already been sent
// (r.seen) are eligible, and the newest keepRecentResults never are. It
// reports how many results were elided.
func (r *Runner) elideIfNeeded() int {
	if r.Session == nil {
		return 0
	}
	budget := r.workingSetTokens()
	have := r.estimateTokens()
	if have <= budget {
		return 0
	}
	target := budget * 4 / 5
	msgs := r.Session.Messages
	// The index below which tool results may be set aside: before the
	// newest keepRecentResults results, and only among messages the model
	// has seen.
	limit := min(r.seen, len(msgs))
	for n, i := 0, len(msgs)-1; i >= 0 && n < keepRecentResults; i-- {
		if msgs[i].Role == "tool" {
			n++
			limit = min(limit, i)
		}
	}
	elided, bytes := 0, 0
	for i := 0; i < limit && have > target; i++ {
		m := &msgs[i]
		if m.Role != "tool" || m.Elided != "" || len(m.Content) < elideMin {
			continue
		}
		stub := elideStub(m.Name, len(m.Content), m.ToolCallID)
		have -= contextplan.EstimateTokens(m.Content) - contextplan.EstimateTokens(stub)
		bytes += len(m.Content)
		m.Elided, m.Content = m.Content, stub
		if err := r.Session.elide(i, stub); err != nil {
			r.emit(Event{Kind: EventNote, Text: "could not record the elision in the session file: " + err.Error()})
		}
		elided++
	}
	if elided > 0 {
		r.emit(Event{Kind: EventCompact, Text: fmt.Sprintf("Set aside %d older tool result(s) (%d KB) to stay within the model's working set; recall brings one back.", elided, (bytes+1023)>>10)})
	}
	return elided
}

func elideStub(tool string, size int, id string) string {
	if tool == "" {
		tool = "tool"
	}
	return fmt.Sprintf("[%s result of %d bytes set aside to save context; call recall with id %q to see it again]", tool, size, id)
}

// recallTool brings back an elided tool result.
func recallTool() Tool {
	return Tool{
		Name:        "recall",
		Description: "Bring back an earlier tool result that was set aside to save context, by the id in its placeholder.",
		Schema: schema(map[string]any{
			"id": prop("string", "The id given in the placeholder"),
		}, "id"),
		Summary: func(a map[string]any) string { return "recall " + str(a, "id") },
		Run: func(_ context.Context, r *Runner, a map[string]any) (string, error) {
			id := str(a, "id")
			if r.Session == nil || id == "" {
				return "", errors.New("no such result")
			}
			// The live history first, then what a summary replaced: a
			// compaction keeps the originals (Session.Archive).
			found := false
			for k, ms := range [][]Message{r.Session.Messages, r.Session.Archive} {
				archived := k == 1
				for i := range ms {
					m := &ms[i]
					if m.Role != "tool" || m.ToolCallID != id {
						continue
					}
					found = true
					text := m.Content
					if m.Elided != "" {
						text = m.Elided
					} else if !archived {
						continue // still in the conversation: nothing to bring back
					}
					if stale := r.staleIn(m); len(stale) > 0 {
						text = "[note: " + strings.Join(stale, ", ") + " changed since this result was produced; read it again for the current content]\n" + text
					}
					return text, nil
				}
			}
			if found {
				return "", fmt.Errorf("the result of %s is still in the conversation", id)
			}
			return "", fmt.Errorf("no result with id %q", id)
		},
	}
}

// collapseMatches summarises grep output that is too long to show: the
// match count per file, most first. lines are "path:line:text" (or
// "path-line-text" context lines, which are not counted).
func collapseMatches(lines []string, limit int) string {
	counts := map[string]int{}
	var order []string
	total := 0
	for _, l := range lines {
		p, _, ok := strings.Cut(l, ":")
		if !ok || l == "--" {
			continue
		}
		if _, seen := counts[p]; !seen {
			order = append(order, p)
		}
		counts[p]++
		total++
	}
	// Stable: by count, then by first appearance.
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && counts[order[j]] > counts[order[j-1]]; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d matches in %d files: too many to show (limit %d). Matches per file:\n", total, len(order), limit)
	shown := 0
	for _, p := range order {
		if shown == 40 {
			fmt.Fprintf(&b, "… and %d more files\n", len(order)-shown)
			break
		}
		fmt.Fprintf(&b, "%s: %d\n", p, counts[p])
		shown++
	}
	b.WriteString("Narrow the pattern, add a glob or a path, or use output_mode files.")
	return b.String()
}
