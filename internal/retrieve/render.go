package retrieve

import (
	"fmt"
	"strings"
)

// Render writes the items as a block for the model, most relevant first,
// within tokens (estimated at 3.2 characters a token). Items already in
// the model's context are named, not repeated. "" when there is nothing.
func Render(res Result, tokens int) string {
	if len(res.Items) == 0 {
		return ""
	}
	var methods []string
	seen := map[string]bool{}
	for _, st := range res.Steps {
		if st.Ran && st.Found > 0 && !seen[string(st.Strategy)] {
			seen[string(st.Strategy)] = true
			methods = append(methods, strings.ReplaceAll(string(st.Strategy), "_", " "))
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Likely relevant places, ranked (found by %s; verify before relying on them):\n", strings.Join(methods, ", "))
	head := b.Len()
	var known []string
	for _, it := range res.Items {
		if it.Known {
			known = append(known, it.File)
			continue
		}
		loc := it.File
		if it.Line > 0 {
			loc += ":" + fmt.Sprint(it.Line)
		}
		if it.Symbol != "" {
			loc += " (" + it.Symbol + ")"
		}
		var why []string
		for i, w := range it.Why {
			if i == 2 {
				break
			}
			why = append(why, describe(w))
		}
		line := fmt.Sprintf("- %s: %s\n", loc, strings.Join(why, "; "))
		if (b.Len()+len(line))*10/32 > tokens {
			break
		}
		b.WriteString(line)
	}
	if len(known) > 0 {
		fmt.Fprintf(&b, "- already read in this conversation, unchanged: %s\n", strings.Join(known, ", "))
	}
	if b.Len() == head {
		return ""
	}
	return strings.TrimRight(b.String(), "\n")
}

func describe(w Reason) string {
	switch w.Strategy {
	case StackTrace:
		return "stack frame " + w.Anchor
	case BuildError:
		return "reported at " + w.Anchor
	case TestToImpl:
		return w.Anchor
	case Lexical:
		return fmt.Sprintf("contains %q", w.Anchor)
	case Symbols, Graph:
		return w.Anchor
	case Paths:
		return "named in the request (" + w.Anchor + ")"
	case Modules:
		return w.Anchor
	case Discovery:
		return w.Anchor
	case Contracts:
		return "route " + w.Anchor
	case DocsConfig:
		return w.Anchor
	case History:
		return "changed by " + w.Anchor
	}
	return string(w.Strategy)
}
