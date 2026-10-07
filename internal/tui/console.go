package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// consoleView runs any CLI command and shows the output of all operations.
type consoleView struct {
	in      textinput.Model
	focus   int // 0 input, 1 jobs
	sel     int // selected job id; 0 follows the newest
	scroll  scroller
	history []string
	hpos    int
	cmds    []CommandInfo
	lines   []string
	h       int
}

func newConsoleView() *consoleView {
	in := textinput.New()
	in.Prompt = sAccent.Render("❯ ")
	in.Placeholder = "a command, e.g. bench tasks --help"
	in.PlaceholderStyle = sFaint
	in.ShowSuggestions = true
	in.CharLimit = 8192
	return &consoleView{in: in, scroll: scroller{follow: true}}
}

func (v *consoleView) name() string             { return "Console" }
func (v *consoleView) capturing() bool          { return v.focus == 0 }
func (v *consoleView) refresh(m *Model) tea.Cmd { return nil }
func (v *consoleView) init(m *Model) tea.Cmd {
	if v.cmds == nil {
		v.cmds = m.be.Commands()
		var sugg []string
		for _, c := range v.cmds {
			sugg = append(sugg, c.Path+" ")
		}
		v.in.SetSuggestions(sugg)
	}
	if v.focus == 0 {
		return v.in.Focus()
	}
	return nil
}

func (v *consoleView) focusInput() tea.Cmd {
	v.focus = 0
	return v.in.Focus()
}

func (v *consoleView) selectedJob(m *Model) *job {
	if v.sel != 0 {
		if j := m.job(v.sel); j != nil {
			return j
		}
	}
	if len(m.jobs) > 0 {
		return m.jobs[len(m.jobs)-1]
	}
	return nil
}

func (v *consoleView) submit(m *Model) tea.Cmd {
	line := strings.TrimSpace(v.in.Value())
	if line == "" {
		return nil
	}
	args, err := splitArgs(line)
	if err != nil {
		return m.toast(err.Error(), toastErr)
	}
	if len(args) > 0 && (args[0] == m.info.Name || args[0] == "boundedcode") {
		args = args[1:]
	}
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "tui", "ui":
		return m.toast("already in the terminal UI", toastInfo)
	case "clear":
		v.in.SetValue("")
		m.jobs = finishedJobsRemoved(m.jobs)
		v.sel = 0
		return nil
	}
	if len(v.history) == 0 || v.history[len(v.history)-1] != line {
		v.history = append(v.history, line)
	}
	v.hpos = len(v.history)
	v.in.SetValue("")
	v.sel = 0
	v.scroll = scroller{follow: true}
	title := strings.Join(args[:min(2, len(args))], " ")
	taskID := ""
	if len(args) >= 3 && (args[0] == "task" || args[0] == "frontier" || args[0] == "verify") {
		taskID = args[2]
	}
	if args[0] == "verify" && len(args) >= 2 {
		taskID = args[1]
	}
	return m.startJob(title, taskID, args, nil)
}

func finishedJobsRemoved(js []*job) []*job {
	var out []*job
	for _, j := range js {
		if j.running() {
			out = append(out, j)
		}
	}
	return out
}

func (v *consoleView) update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if v.focus == 0 {
			switch msg.String() {
			case "enter":
				return v.submit(m)
			case "esc":
				v.focus = 1
				v.in.Blur()
				return nil
			case "up":
				if v.hpos > 0 {
					v.hpos--
					v.in.SetValue(v.history[v.hpos])
					v.in.CursorEnd()
				}
				return nil
			case "down":
				if v.hpos < len(v.history)-1 {
					v.hpos++
					v.in.SetValue(v.history[v.hpos])
				} else {
					v.hpos = len(v.history)
					v.in.SetValue("")
				}
				v.in.CursorEnd()
				return nil
			case "pgup", "pgdown":
				v.scroll.handle(msg, len(v.lines), v.h)
				return nil
			}
			var cmd tea.Cmd
			v.in, cmd = v.in.Update(msg)
			return cmd
		}
		// Jobs list focused.
		n := len(m.jobs)
		switch msg.String() {
		case "/", ":", "i", "enter":
			if msg.String() == "enter" && v.sel != 0 {
				if j := m.job(v.sel); j != nil && j.taskID != "" {
					return m.openTask(j.taskID)
				}
			}
			return v.focusInput()
		case "x", "ctrl+x":
			if j := v.selectedJob(m); j != nil && j.running() {
				j.cancel()
				return m.toast("interrupting "+j.title, toastInfo)
			}
			return nil
		case "up", "k", "down", "j":
			// Jobs are listed newest first.
			idx := v.indexOf(m)
			if s := msg.String(); s == "up" || s == "k" {
				idx--
			} else {
				idx++
			}
			idx = max(0, min(idx, n-1))
			if n > 0 {
				v.sel = m.jobs[n-1-idx].id
				v.scroll = scroller{follow: true}
			}
			return nil
		case "e":
			if j := v.selectedJob(m); j != nil {
				v.in.SetValue(cmdline(j.args))
				v.in.CursorEnd()
				return v.focusInput()
			}
		}
		v.scroll.handle(msg, len(v.lines), v.h)
	case tea.MouseMsg:
		v.scroll.handle(msg, len(v.lines), v.h)
	default:
		var cmd tea.Cmd
		v.in, cmd = v.in.Update(msg)
		return cmd
	}
	return nil
}

func (v *consoleView) indexOf(m *Model) int {
	j := v.selectedJob(m)
	for i := len(m.jobs) - 1; i >= 0; i-- {
		if m.jobs[i] == j {
			return len(m.jobs) - 1 - i
		}
	}
	return 0
}

func (v *consoleView) view(m *Model, w, h int) string {
	var b strings.Builder
	b.WriteString(" " + sTitle.Render("Console") + sFaint.Render("  every "+m.info.Name+" command, run in-process; questions appear as dialogs") + "\n\n")
	v.in.Width = w - 6
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Width(w - 4)
	if v.focus == 0 {
		box = box.BorderForeground(cAccent)
	} else {
		box = box.BorderForeground(cBorder)
	}
	b.WriteString(" " + box.Render(v.in.View()) + "\n")
	// Help for the command being typed.
	help := sFaint.Render("tab completes · ↑/↓ history · enter runs · esc browses output")
	if typed := strings.TrimSpace(v.in.Value()); typed != "" {
		best := CommandInfo{}
		for _, c := range v.cmds {
			if strings.HasPrefix(typed+" ", c.Path+" ") && len(c.Path) > len(best.Path) {
				best = c
			}
		}
		if best.Path != "" {
			help = sAccent2.Render(m.info.Name+" "+best.Use) + sFaint.Render("  "+best.Short)
		}
	}
	b.WriteString(" " + trunc(help, w-2) + "\n\n")
	head := strings.Count(b.String(), "\n")
	bodyH := max(3, h-head)
	lw := min(34, w/3)
	ow := w - lw - 4
	// Jobs list.
	var left strings.Builder
	left.WriteString(sMuted.Render("OPERATIONS") + "\n")
	sel := v.selectedJob(m)
	if len(m.jobs) == 0 {
		left.WriteString(sFaint.Render("none yet"))
	}
	shown := 0
	for i := len(m.jobs) - 1; i >= 0 && shown < bodyH-1; i-- {
		j := m.jobs[i]
		icon := statusIcon(j.status())
		if j.running() {
			icon = m.spin.View()
		}
		el := j.elapsed().Round(time.Second).String()
		line := icon + " " + fit(sText.Render(j.title), lw-4-len(el)) + " " + sFaint.Render(el)
		if j == sel {
			st := sSel
			if v.focus != 1 {
				st = lipgloss.NewStyle().Background(cBorder)
			}
			line = st.Render(fit(line, lw))
		}
		left.WriteString(line + "\n")
		shown++
	}
	// Output of the selected job.
	var right strings.Builder
	v.h = bodyH - 1
	v.lines = nil
	if sel != nil {
		state := sMuted.Render("running " + sel.elapsed().Round(time.Second).String())
		switch {
		case sel.done && sel.err != nil:
			state = sErr.Render("failed")
		case sel.done:
			state = sOK.Render("finished in " + sel.elapsed().Round(100*time.Millisecond).String())
		}
		right.WriteString(sBold.Render(trunc(sel.title, ow/2)) + "  " + state + "\n")
		v.lines = sel.rendered(ow)
		vis, _, _ := v.scroll.window(v.lines, v.h)
		for _, l := range vis {
			right.WriteString(l + "\n")
		}
	} else {
		right.WriteString(sMuted.Render("Output of commands and of every operation started from any view appears here.\n\n"))
		right.WriteString(sFaint.Render("Examples:\n"))
		for _, ex := range []string{"doctor", "workspace show", "bench tasks --help", "bench infra --help", "model list", "stats --since 720h"} {
			right.WriteString("  " + sAccent2.Render(ex) + "\n")
		}
	}
	sep := lipgloss.NewStyle().Foreground(cBorder).Render(strings.TrimSuffix(strings.Repeat("│\n", bodyH), "\n"))
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, " "+block(left.String(), lw, bodyH), " ", sep, " ", block(right.String(), ow, bodyH)))
	return b.String()
}

func (v *consoleView) hints(m *Model) []hint {
	if v.focus == 0 {
		return []hint{{"enter", "run"}, {"tab", "complete"}, {"↑/↓", "history"}, {"esc", "browse"}}
	}
	return []hint{{"↑/↓", "select"}, {"x", "interrupt"}, {"e", "edit & rerun"}, {"enter", "open task / prompt"}, {"pgup/pgdn", "scroll"}}
}

func (v *consoleView) commands(m *Model) []command {
	cs := []command{{title: "Focus the command prompt", group: "Console", run: func(m *Model) tea.Cmd { return v.focusInput() }}}
	if j := v.selectedJob(m); j != nil && j.running() {
		cs = append(cs, command{title: "Interrupt " + j.title, group: "Console", key: "x", run: func(m *Model) tea.Cmd { j.cancel(); return nil }})
	}
	for _, c := range []string{"bench infra", "bench tasks", "bench intel"} {
		cs = append(cs, command{title: "Prepare: " + c, group: "Benchmarks", run: func(m *Model) tea.Cmd {
			v.in.SetValue(c + " ")
			v.in.CursorEnd()
			return v.focusInput()
		}})
	}
	return cs
}
