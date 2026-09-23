package task

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/akynte/boundedcode/internal/workflow"
)

// How much of a continuation is spent telling the model what it already
// looked at.
//
// The whole point of a boundary is that the context was full, so this section
// has to be a great deal smaller than the conversation it replaces. It is
// bounded twice: by a byte budget for the section, and by a count of entries,
// whichever binds first.
const (
	// evidenceByteBudget caps the rendered section.
	evidenceByteBudget = 3500
	// maxEvidenceShown caps how many files the section names.
	maxEvidenceShown = 28
	// maxTriedShown caps the do-not-repeat list. Larger than the ten it
	// replaces, and ordered by what a repeat would cost.
	maxTriedShown = 24
	// excerptInSummary is how much of a record's excerpt survives here, on
	// top of the bound the engine already applied when recording it.
	excerptInSummary = 150
)

// renderEvidence writes what inspection has already established.
//
// The order is deliberate and fixed, because the section is truncated and
// what survives truncation is what the model sees. A file the plan named
// matters more than the twentieth file a search happened to return, and a
// file already modified matters most of all: re-reading that one after a
// boundary is how an edit gets silently reverted.
func renderEvidence(s *workflow.State, changed []string) string {
	if len(s.Reads) == 0 {
		return ""
	}
	ranked := rankEvidence(s, changed)
	if len(ranked) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\nWhat you have already inspected, and what was in it. This is the " +
		"evidence from the closed conversation: do not read these again to recover it.\n")

	shown, spent := 0, 0
	var omitted int
	for _, ev := range ranked {
		if shown >= maxEvidenceShown || spent >= evidenceByteBudget {
			omitted++
			continue
		}
		line := renderOneRead(ev)
		if spent+len(line) > evidenceByteBudget && shown > 0 {
			omitted++
			continue
		}
		b.WriteString(line)
		spent += len(line)
		shown++
	}
	if omitted > 0 {
		fmt.Fprintf(&b, "  … and %d further inspection(s) whose detail did not fit. "+
			"Ask for a specific range if you need one of them again.\n", omitted)
	}

	if unread := unreadPlanFiles(s); len(unread) > 0 {
		b.WriteString("\nFiles the plan names that you have not inspected yet:\n")
		for _, f := range clipStrings(unread, 12) {
			fmt.Fprintf(&b, "  - %s\n", f)
		}
	}
	if barren := barrenReads(ranked); len(barren) > 0 {
		b.WriteString("\nInspections that returned nothing useful. Looking there again " +
			"will return nothing again:\n")
		for _, f := range clipStrings(barren, 10) {
			fmt.Fprintf(&b, "  - %s\n", f)
		}
	}
	return b.String()
}

// renderOneRead is one file's line, with what was found in it.
func renderOneRead(ev workflow.ReadEvidence) string {
	var b strings.Builder
	label := ev.Path
	if label == "" {
		label = ev.Tool
	}
	b.WriteString("  - " + label)
	if ev.Detail != "" {
		b.WriteString(" (" + ev.Detail + ")")
	}
	if ev.Bytes > 0 {
		fmt.Fprintf(&b, " [%s]", humanBytes(ev.Bytes))
	}
	b.WriteString("\n")
	if len(ev.Decls) > 0 {
		fmt.Fprintf(&b, "      declares: %s\n", strings.Join(clipStrings(ev.Decls, 6), "; "))
	} else if ev.Excerpt != "" {
		fmt.Fprintf(&b, "      begins: %s\n", clipString(ev.Excerpt, excerptInSummary))
	}
	return b.String()
}

// rankEvidence orders the cache by how much the model would lose by not being
// told about it.
func rankEvidence(s *workflow.State, changed []string) []workflow.ReadEvidence {
	planned := map[string]bool{}
	for _, f := range s.Plan.Files {
		planned[path.Clean(f.Path)] = true
	}
	modified := map[string]bool{}
	for _, f := range changed {
		modified[path.Clean(f)] = true
	}
	obliged := map[string]bool{}
	for _, o := range s.Plan.Obligations {
		if o.Resolution.Action != workflow.ActionNoChange {
			obliged[path.Clean(o.Path)] = true
		}
	}

	out := append([]workflow.ReadEvidence(nil), s.Reads...)
	rank := func(ev workflow.ReadEvidence) int {
		p := path.Clean(ev.Path)
		switch {
		case ev.Path != "" && modified[p]:
			return 0
		case ev.Path != "" && planned[p]:
			return 1
		case ev.Path != "" && obliged[p]:
			return 2
		case !ev.Empty:
			return 3
		default:
			return 4
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank(out[i]), rank(out[j])
		if ri != rj {
			return ri < rj
		}
		// Within a band, most recent first: recency is the best available
		// proxy for what the model was in the middle of doing.
		return out[i].Seq > out[j].Seq
	})
	return out
}

// unreadPlanFiles names what the plan declared and inspection never opened.
//
// A continuation that lists only what was read invites the model to re-read
// it looking for the part it has not seen. Saying which files are still
// unopened points the fresh budget at new ground.
func unreadPlanFiles(s *workflow.State) []string {
	seen := map[string]bool{}
	for _, ev := range s.Reads {
		if ev.Path != "" {
			seen[path.Clean(ev.Path)] = true
		}
	}
	var out []string
	for _, f := range s.Plan.Files {
		if !seen[path.Clean(f.Path)] {
			out = append(out, f.Path)
		}
	}
	return out
}

// barrenReads names the inspections that found nothing.
func barrenReads(ranked []workflow.ReadEvidence) []string {
	var out []string
	for _, ev := range ranked {
		if !ev.Empty {
			continue
		}
		label := ev.Path
		if label == "" {
			label = ev.Tool
		}
		if ev.Detail != "" {
			label += " (" + ev.Detail + ")"
		}
		out = append(out, label)
	}
	return out
}

func humanBytes(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%dB", n)
	}
	return fmt.Sprintf("%.1fKB", float64(n)/1024)
}

func clipString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
