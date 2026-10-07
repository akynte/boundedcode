package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// palette is the fuzzy command finder (ctrl+k).
type palette struct {
	m       *Model
	in      textinput.Model
	all     []command
	matches []command
	cur     cursor
}

func newPalette(m *Model) *palette {
	in := textinput.New()
	in.Prompt = sAccent.Render("❯ ")
	in.Placeholder = "Type a command…"
	in.PlaceholderStyle = sFaint
	in.Focus()
	// The active view's commands first, then the global ones.
	all := append(m.current().commands(m), m.globalCommands()...)
	p := &palette{m: m, in: in, all: all}
	p.filter()
	return p
}

func (p *palette) filter() {
	q := strings.TrimSpace(p.in.Value())
	type scored struct {
		c command
		s int
		i int
	}
	var ss []scored
	for i, c := range p.all {
		s := fuzzyScore(q, c.title+" "+c.group)
		if s >= 0 {
			ss = append(ss, scored{c, s, i})
		}
	}
	if q != "" {
		sort.SliceStable(ss, func(a, b int) bool { return ss[a].s > ss[b].s })
	}
	p.matches = p.matches[:0]
	for _, s := range ss {
		p.matches = append(p.matches, s.c)
	}
	p.cur.pos = 0
}

func (p *palette) update(msg tea.Msg) (bool, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc", "ctrl+k":
			return true, nil
		case "enter":
			if p.cur.pos < len(p.matches) {
				c := p.matches[p.cur.pos]
				p.m.pop() // close before running: the command may open a dialog
				return false, c.run(p.m)
			}
			return false, nil
		case "up", "ctrl+p":
			p.cur.pos--
			p.cur.clamp(len(p.matches), 12)
			return false, nil
		case "down", "ctrl+n":
			p.cur.pos++
			p.cur.clamp(len(p.matches), 12)
			return false, nil
		}
	}
	var cmd tea.Cmd
	before := p.in.Value()
	p.in, cmd = p.in.Update(msg)
	if p.in.Value() != before {
		p.filter()
	}
	return false, cmd
}

func (p *palette) view(w, h int) string {
	bw := min(72, w-6)
	p.in.Width = bw - 4
	var b strings.Builder
	b.WriteString(p.in.View() + "\n")
	b.WriteString(sFaint.Render(strings.Repeat("─", bw)) + "\n")
	rows := min(12, max(3, h-10))
	from, to := p.cur.window(len(p.matches), rows)
	for i := from; i < to; i++ {
		c := p.matches[i]
		key := ""
		if c.key != "" {
			key = sKey.Render(c.key)
		}
		group := sFaint.Render(c.group)
		line := row(bw, []int{0, 12, 8}, sText.Render(c.title), group, key)
		if i == p.cur.pos {
			line = lipgloss.NewStyle().Background(cSelBg).Render(fit(line, bw))
		}
		b.WriteString(line + "\n")
	}
	if len(p.matches) == 0 {
		b.WriteString(sFaint.Render("No matching commands") + "\n")
	}
	b.WriteString(sFaint.Render("↑/↓ select · enter run · esc close"))
	return sModal.Padding(0, 1).Width(bw + 4).Render(b.String())
}

// helpModal lists keyboard shortcuts.
type helpModal struct{ m *Model }

func (hm *helpModal) update(msg tea.Msg) (bool, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc", "?", "q", "enter":
			return true, nil
		}
	}
	return false, nil
}

func (hm *helpModal) view(w, h int) string {
	m := hm.m
	col := func(title string, hs []hint) string {
		var b strings.Builder
		b.WriteString(sSection.Render(title) + "\n")
		for _, x := range hs {
			b.WriteString(sKey.Render(fit(x.key, 12)) + sText.Render(x.desc) + "\n")
		}
		return b.String()
	}
	global := []hint{
		{"1 … 9", "switch view"}, {"ctrl+k  :", "command palette"}, {"J", "activity / jobs"},
		{"esc", "back / close"}, {"q", "back, or quit"}, {"ctrl+c", "quit"}, {"?", "this help"},
	}
	nav := []hint{
		{"↑/↓  j/k", "move"}, {"pgup/pgdn", "page"}, {"g / G", "top / bottom"}, {"enter", "open / run"},
		{"tab", "next field / tab"}, {"ctrl+s", "submit a form"}, {"wheel", "scroll"},
	}
	left := col("Global", global) + "\n" + col("Navigation", nav)
	right := col(m.current().name(), m.current().hints(m))
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, "    ", right)
	if lipgloss.Width(body)+8 > w {
		body = left + "\n" + right
	}
	title := sAccentB.Render("Keyboard shortcuts")
	return sModal.Render(title + "\n\n" + body + "\n" + sFaint.Render("esc to close"))
}

// textModal shows long read-only text with scrolling.
type textModal struct {
	title string
	raw   []string
	lines []string // raw, wrapped to the last drawn width
	cur   cursor
	h     int
}

func newTextModal(title, text string) *textModal {
	return &textModal{title: title, raw: strings.Split(strings.TrimRight(text, "\n"), "\n")}
}

func (t *textModal) update(msg tea.Msg) (bool, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc", "q", "enter":
			return true, nil
		}
	}
	// Scroll by moving the window's top line.
	n := max(1, len(t.lines)-t.h+1)
	t.cur.handle(msg, n, 1)
	return false, nil
}

func (t *textModal) view(w, h int) string {
	bw := min(100, w-8)
	t.h = max(3, h-8)
	var b strings.Builder
	b.WriteString(sAccentB.Render(t.title) + "\n\n")
	t.lines = t.lines[:0]
	for _, l := range t.raw {
		t.lines = append(t.lines, strings.Split(wrap(l, bw), "\n")...)
	}
	from := min(t.cur.pos, max(0, len(t.lines)-t.h))
	to := min(len(t.lines), from+t.h)
	for _, l := range t.lines[from:to] {
		b.WriteString(fit(l, bw) + "\n")
	}
	pos := ""
	if len(t.lines) > t.h {
		pos = sFaint.Render(fmt.Sprintf("lines %d–%d of %d · ", from+1, to, len(t.lines)))
	}
	b.WriteString("\n" + pos + sFaint.Render("↑/↓ scroll · esc close"))
	return sModal.Padding(1, 2).Width(bw + 6).Render(b.String())
}
