package tui

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type intelKind struct {
	name, arg, help string
	serena          bool
	noArg           bool
}

var intelKinds = []intelKind{
	{name: "search", arg: "symbol query", help: "Search symbols in the code graph"},
	{name: "trace", arg: "function", help: "Callers and callees of a function"},
	{name: "snippet", arg: "symbol", help: "Source of a symbol"},
	{name: "impact", noArg: true, help: "Change impact of the working tree against the default branch"},
	{name: "architecture", noArg: true, help: "Architecture overview of each repository"},
	{name: "links", arg: "kind filter: http | topic | topic_infra | env (optional)", help: "Cross-service contract links across the workspace"},
	{name: "endpoints", noArg: true, help: "Cross-service endpoints found by the analyzers"},
	{name: "symbol", arg: "symbol name", serena: true, help: "Find a symbol with Serena/LSP (includes its body)"},
	{name: "refs", arg: "symbol name", serena: true, help: "Symbols referencing a symbol (Serena/LSP)"},
	{name: "impls", arg: "interface name", serena: true, help: "Implementations of an interface (Serena/LSP)"},
}

// intelView queries repository intelligence.
type intelView struct {
	kind    int
	focus   int // 0 kind, 1 query, 2 repo, 3 root, 4 results
	query   textinput.Model
	repo    textinput.Model
	root    textinput.Model
	running bool
	cancel  context.CancelFunc
	out     string
	err     error
	took    time.Duration
	ran     string
	scroll  scroller
	lines   []string
	linesW  int // width lines were wrapped for; 0 after new output
	h       int
}

type intelResultMsg struct {
	out  string
	err  error
	took time.Duration
}

func newIntelView() *intelView {
	mk := func(ph string) textinput.Model {
		in := textinput.New()
		in.Prompt = ""
		in.Placeholder = ph
		in.PlaceholderStyle = sFaint
		in.Cursor.Style = sAccent
		return in
	}
	v := &intelView{query: mk("query"), repo: mk("all indexed repositories"), root: mk("workspace checkouts (or a task worktree path)")}
	v.focus = 1
	return v
}

func (v *intelView) name() string    { return "Intel" }
func (v *intelView) usesLeft() bool  { return v.focus != 4 }
func (v *intelView) loading() bool   { return v.running }
func (v *intelView) capturing() bool { return v.focus >= 1 && v.focus <= 3 }
func (v *intelView) refresh(m *Model) tea.Cmd {
	return nil
}

func (v *intelView) init(m *Model) tea.Cmd { return v.setFocus(v.focus) }

func (v *intelView) fields() []int {
	k := intelKinds[v.kind]
	f := []int{0}
	if !k.noArg {
		f = append(f, 1)
	}
	if k.name != "links" {
		f = append(f, 2)
	}
	if k.serena {
		f = append(f, 3)
	}
	return append(f, 4)
}

func (v *intelView) setFocus(f int) tea.Cmd {
	v.focus = f
	v.query.Blur()
	v.repo.Blur()
	v.root.Blur()
	switch f {
	case 1:
		return v.query.Focus()
	case 2:
		return v.repo.Focus()
	case 3:
		return v.root.Focus()
	}
	return nil
}

func (v *intelView) cycle(d int) tea.Cmd {
	fs := v.fields()
	i := 0
	for j, f := range fs {
		if f == v.focus {
			i = j
		}
	}
	return v.setFocus(fs[(i+d+len(fs))%len(fs)])
}

func (v *intelView) args() []string {
	k := intelKinds[v.kind]
	args := []string{"intel", k.name}
	q := strings.TrimSpace(v.query.Value())
	switch {
	case k.name == "links":
		if q != "" {
			args = append(args, "--kind", q)
		}
	case !k.noArg:
		args = append(args, q)
	}
	if r := strings.TrimSpace(v.repo.Value()); r != "" && k.name != "links" {
		args = append(args, "--repo", r)
	}
	if k.serena {
		if r := strings.TrimSpace(v.root.Value()); r != "" {
			args = append(args, "--root", expandHome(r))
		}
		if k.name == "symbol" {
			args = append(args, "--body")
		}
	}
	return args
}

func (v *intelView) run(m *Model) tea.Cmd {
	k := intelKinds[v.kind]
	if !k.noArg && k.name != "links" && strings.TrimSpace(v.query.Value()) == "" {
		return m.toast(k.name+" needs a "+k.arg, toastInfo)
	}
	if v.cancel != nil {
		v.cancel()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	v.cancel, v.running, v.err = cancel, true, nil
	args := v.args()
	v.ran = cmdline(args)
	be := m.be
	t0 := time.Now()
	return tea.Batch(func() tea.Msg {
		defer cancel()
		var buf syncBuffer
		err := be.Exec(ctx, args, &buf)
		return intelResultMsg{buf.String(), err, time.Since(t0)}
	}, m.spinTick())
}

func (v *intelView) update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case intelResultMsg:
		v.running, v.cancel = false, nil
		v.out, v.err, v.took = msg.out, msg.err, msg.took
		v.scroll, v.linesW = scroller{}, 0
		return v.setFocus(4)
	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			return v.cycle(1)
		case "shift+tab":
			return v.cycle(-1)
		case "esc":
			if v.running && v.cancel != nil {
				v.cancel()
				return m.toast("query cancelled", toastInfo)
			}
			if v.focus != 4 {
				return v.setFocus(4)
			}
			return v.setFocus(0)
		case "enter":
			if v.focus <= 3 {
				return v.run(m)
			}
		case "ctrl+r":
			return v.run(m)
		}
		switch v.focus {
		case 0:
			switch msg.String() {
			case "left", "h":
				v.kind = (v.kind + len(intelKinds) - 1) % len(intelKinds)
			case "right", "l":
				v.kind = (v.kind + 1) % len(intelKinds)
			case "down", "j":
				return v.cycle(1)
			default:
				if s := msg.String(); s == "r" {
					return v.run(m)
				}
			}
			return nil
		case 1:
			var cmd tea.Cmd
			v.query, cmd = v.query.Update(msg)
			return cmd
		case 2:
			var cmd tea.Cmd
			v.repo, cmd = v.repo.Update(msg)
			return cmd
		case 3:
			var cmd tea.Cmd
			v.root, cmd = v.root.Update(msg)
			return cmd
		case 4:
			if msg.String() == "/" || msg.String() == "i" {
				return v.setFocus(1)
			}
			if msg.String() == "r" {
				return v.run(m)
			}
			if (msg.String() == "up" || msg.String() == "k") && v.scroll.top == 0 {
				return v.cycle(-1)
			}
			v.scroll.handle(msg, len(v.lines), v.h)
		}
	case tea.MouseMsg:
		v.scroll.handle(msg, len(v.lines), v.h)
	default:
		var c1, c2, c3 tea.Cmd
		v.query, c1 = v.query.Update(msg)
		v.repo, c2 = v.repo.Update(msg)
		v.root, c3 = v.root.Update(msg)
		return tea.Batch(c1, c2, c3)
	}
	return nil
}

func (v *intelView) view(m *Model, w, h int) string {
	var b strings.Builder
	k := intelKinds[v.kind]
	b.WriteString(" " + sTitle.Render("Repository intelligence") + sFaint.Render("  code graph · cross-service contracts · Serena/LSP") + "\n\n")
	var chips []string
	for i, kk := range intelKinds {
		label := kk.name
		if kk.serena {
			label += "ˢ"
		}
		switch {
		case i == v.kind && v.focus == 0:
			chips = append(chips, badge(label, cAccent))
		case i == v.kind:
			chips = append(chips, lipgloss.NewStyle().Foreground(cAccent).Bold(true).Underline(true).Render(label))
		default:
			chips = append(chips, sMuted.Render(label))
		}
	}
	b.WriteString(" " + strings.Join(chips, "  ") + "\n")
	b.WriteString(" " + sFaint.Render(k.help) + "\n\n")
	iw := max(10, w-16)
	field := func(f int, label string, in *textinput.Model) {
		in.Width = iw - 2
		mark, ls := "  ", sMuted
		if v.focus == f {
			mark, ls = sAccent.Render("▍ "), sBold
		}
		b.WriteString(mark + ls.Render(fit(label, 10)) + " " + in.View() + "\n")
	}
	if !k.noArg {
		label := "Query"
		if k.name == "links" {
			label = "Kind"
		}
		v.query.Placeholder = k.arg
		field(1, label, &v.query)
	}
	if k.name != "links" {
		field(2, "Repo", &v.repo)
	}
	if k.serena {
		field(3, "Root", &v.root)
	}
	b.WriteString("\n")
	header := 0
	for range strings.Split(b.String(), "\n") {
		header++
	}
	v.h = max(1, h-header-2)
	switch {
	case v.running:
		b.WriteString(" " + m.spin.View() + sMuted.Render(" running "+v.ran+"…  ") + sFaint.Render("esc cancels") + "\n")
	case v.ran == "":
		b.WriteString(" " + sMuted.Render("Choose a query with ←/→ (tab to the kinds), type, and press enter.") + "\n")
		b.WriteString(" " + sFaint.Render("ˢ marks Serena/LSP queries (optional; see System).") + "\n")
	default:
		status := sOK.Render("✔")
		if v.err != nil {
			status = sErr.Render("✘")
		}
		b.WriteString(" " + status + " " + sAccent.Render(v.ran) + sFaint.Render("  "+v.took.Round(time.Millisecond).String()) + "\n")
		if v.linesW != w {
			v.linesW, v.lines = w, nil
			text := strings.TrimRight(v.out, "\n")
			if v.err != nil {
				text += "\n✘ " + v.err.Error()
			}
			for l := range strings.SplitSeq(text, "\n") {
				v.lines = append(v.lines, wrapStyled(l, w-3)...)
			}
			if len(v.lines) == 0 || (len(v.lines) == 1 && ansiEmpty(v.lines[0])) {
				v.lines = []string{sMuted.Render("(no results)")}
			}
		}
		vis, from, to := v.scroll.window(v.lines, v.h)
		for _, l := range vis {
			b.WriteString(" " + l + "\n")
		}
		if len(v.lines) > v.h {
			b.WriteString(lipgloss.PlaceHorizontal(w, lipgloss.Right, sFaint.Render(itoa(from+1)+"–"+itoa(to)+" of "+itoa(len(v.lines))+" ")))
		}
	}
	return b.String()
}

func (v *intelView) hints(m *Model) []hint {
	switch v.focus {
	case 0:
		return []hint{{"←/→", "query kind"}, {"tab", "next"}, {"enter", "run"}}
	case 4:
		return []hint{{"↑/↓", "scroll"}, {"/", "edit query"}, {"r", "rerun"}, {"tab", "next"}}
	}
	return []hint{{"enter", "run"}, {"tab", "next field"}, {"esc", "results"}}
}

func (v *intelView) commands(m *Model) []command {
	var cs []command
	for i, k := range intelKinds {
		cs = append(cs, command{title: "Intel: " + k.name, group: "Intel", run: func(m *Model) tea.Cmd {
			v.kind = i
			if k.noArg {
				return v.run(m)
			}
			return v.setFocus(1)
		}})
	}
	return cs
}

// syncBuffer is a goroutine-safe bytes.Buffer.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func ansiEmpty(s string) bool { return strings.TrimSpace(stripANSI(s)) == "" }

// expandHome expands a leading ~/ to the home directory.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
