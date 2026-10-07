package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// fit truncates or pads s to exactly w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) > w {
		return ansi.Truncate(s, w, "…")
	}
	return s + strings.Repeat(" ", w-ansi.StringWidth(s))
}

// trunc truncates s to w cells.
func trunc(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

// oneLine collapses whitespace.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// row renders cells into fixed-width columns separated by two spaces. A
// width of 0 takes the remaining space.
func row(total int, widths []int, cells ...string) string {
	fixed := 0
	flex := -1
	for i, w := range widths {
		if w == 0 {
			flex = i
			continue
		}
		fixed += w
	}
	gaps := 2 * (len(widths) - 1)
	var b strings.Builder
	for i, c := range cells {
		w := widths[i]
		if i == flex {
			w = max(1, total-fixed-gaps)
		}
		b.WriteString(fit(c, w))
		if i < len(cells)-1 {
			b.WriteString("  ")
		}
	}
	return b.String()
}

// block pads every line of s to w cells and the block to h lines.
func block(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for i, l := range lines {
		lines[i] = fit(l, w)
	}
	for len(lines) < h {
		lines = append(lines, strings.Repeat(" ", w))
	}
	return strings.Join(lines, "\n")
}

// overlay draws fg centred over bg; bg is dimmed.
func overlay(bg, fg string, w, h int) string {
	bgl := strings.Split(block(bg, w, h), "\n")
	for i, l := range bgl {
		bgl[i] = sFaint.Render(ansi.Strip(l))
	}
	fgl := strings.Split(fg, "\n")
	fw := lipgloss.Width(fg)
	x := max(0, (w-fw)/2)
	y := max(0, (h-len(fgl))/3)
	for i, l := range fgl {
		r := y + i
		if r >= len(bgl) {
			break
		}
		left := ansi.Truncate(bgl[r], x, "")
		right := ansi.TruncateLeft(bgl[r], x+fw, "")
		bgl[r] = left + fit(l, fw) + right
	}
	return strings.Join(bgl, "\n")
}

// wrap word-wraps s to w cells.
func wrap(s string, w int) string {
	if w <= 0 {
		return s
	}
	return ansi.Wrap(s, w, "")
}

// meter renders a used/max bar.
func meter(used, limit float64, w int) string {
	if w < 4 {
		return ""
	}
	frac := 0.0
	if limit > 0 {
		frac = min(1, used/limit)
	}
	n := int(frac*float64(w) + 0.5)
	c := cOK
	switch {
	case frac >= 0.9:
		c = cErr
	case frac >= 0.7:
		c = cWarn
	}
	return lipgloss.NewStyle().Foreground(c).Render(strings.Repeat("━", n)) + sFaint.Render(strings.Repeat("━", w-n))
}

// fuzzyScore returns a match score of pattern in s (higher is better), or
// -1 when pattern is not a subsequence of s.
func fuzzyScore(pattern, s string) int {
	if pattern == "" {
		return 0
	}
	p := []rune(strings.ToLower(pattern))
	t := []rune(strings.ToLower(s))
	score, pi, prev := 0, 0, -2
	for i, r := range t {
		if pi < len(p) && r == p[pi] {
			score += 1
			if i == prev+1 {
				score += 3 // consecutive
			}
			if i == 0 || !unicode.IsLetter(t[i-1]) {
				score += 2 // word start
			}
			prev = i
			pi++
		}
	}
	if pi < len(p) {
		return -1
	}
	return score*10 - len(t)/4
}

// ago formats a timestamp relative to now.
func ago(ts string) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return ts
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
	return t.Local().Format("2006-01-02")
}

// clock formats a timestamp as local HH:MM:SS.
func clock(ts string) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		if len(ts) >= 19 {
			return ts[11:19]
		}
		return ts
	}
	return t.Local().Format("15:04:05")
}

// human formats a count compactly.
func human(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 10_000:
		return fmt.Sprintf("%.0fk", float64(n)/1e3)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

// dur formats seconds as a short duration.
func dur(s float64) string {
	d := time.Duration(s * float64(time.Second)).Round(time.Second)
	if d >= time.Hour {
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	if d >= time.Minute {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return d.String()
}

// orDash returns "–" for empty strings.
func orDash(s string) string {
	if s == "" {
		return "–"
	}
	return s
}

// kv renders an aligned label/value line.
func kv(label string, w int, value string) string {
	return sMuted.Render(fit(label, w)) + value
}

// splitArgs splits a command line into arguments, honouring single and
// double quotes and backslash escapes.
func splitArgs(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	inArg := false
	var quote rune
	escaped := false
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, inArg = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, inArg = r, true
		case unicode.IsSpace(r):
			if inArg {
				out = append(out, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote", quote)
	}
	if escaped {
		return nil, fmt.Errorf("trailing backslash")
	}
	if inArg {
		out = append(out, cur.String())
	}
	return out, nil
}

// quoteArg quotes an argument for display when needed.
func quoteArg(a string) string {
	if a == "" || strings.ContainsAny(a, " \t\"'\\") {
		return "\"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(a) + "\""
	}
	return a
}

// cmdline renders args for display.
func cmdline(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = quoteArg(a)
	}
	return strings.Join(q, " ")
}

func itoa(n int) string { return fmt.Sprint(n) }

func stripANSI(s string) string { return ansi.Strip(s) }
