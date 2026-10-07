package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/akynte/boundedcode/internal/workspace"
)

// workspacesView manages workspaces and their repositories.
type workspacesView struct {
	data   []Workspace
	err    error
	loaded bool
	busy   bool
	wsCur  cursor
	repCur cursor
	focus  int // 0 workspaces, 1 repositories
	h      int
}

type workspacesMsg struct {
	ws  []Workspace
	err error
}

func (v *workspacesView) name() string          { return "Workspaces" }
func (v *workspacesView) usesLeft() bool        { return v.focus == 1 }
func (v *workspacesView) capturing() bool       { return false }
func (v *workspacesView) loading() bool         { return v.busy }
func (v *workspacesView) init(m *Model) tea.Cmd { return v.load(m) }
func (v *workspacesView) refresh(m *Model) tea.Cmd {
	if v.busy || m.ticks%5 != 0 {
		return nil
	}
	return v.load(m)
}

func (v *workspacesView) load(m *Model) tea.Cmd {
	v.busy = true
	be, ctx := m.be, m.ctx
	return func() tea.Msg {
		ws, err := be.Workspaces(ctx)
		return workspacesMsg{ws, err}
	}
}

func (v *workspacesView) selected() *Workspace {
	if v.wsCur.pos < len(v.data) {
		return &v.data[v.wsCur.pos]
	}
	return nil
}

func (v *workspacesView) selectedRepo() *workspace.Repository {
	w := v.selected()
	if w == nil || v.repCur.pos >= len(w.Repos) {
		return nil
	}
	return &w.Repos[v.repCur.pos]
}

func (v *workspacesView) update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case workspacesMsg:
		first := !v.loaded
		v.busy, v.loaded = false, true
		v.data, v.err = msg.ws, msg.err
		if first {
			for i, w := range v.data {
				if w.Current {
					v.wsCur.pos = i
				}
			}
		}
		return nil
	case tea.KeyMsg:
		w := v.selected()
		switch msg.String() {
		case "tab", "right", "l":
			if v.focus == 0 && w != nil {
				v.focus = 1
			} else {
				v.focus = 0
			}
			return nil
		case "left", "h", "esc":
			v.focus = 0
			return nil
		}
		if v.focus == 0 {
			if v.wsCur.handle(msg, len(v.data), v.h) {
				v.repCur = cursor{}
				return nil
			}
		} else if w != nil && v.repCur.handle(msg, len(w.Repos), v.h-8) {
			return nil
		}
		return v.action(m, msg.String())
	}
	return nil
}

func (v *workspacesView) action(m *Model, key string) tea.Cmd {
	w := v.selected()
	r := v.selectedRepo()
	reload := func(m *Model, err error) tea.Cmd {
		if err != nil {
			return tea.Batch(m.errToast(err), v.load(m))
		}
		return tea.Batch(m.toast("done", toastOK), v.load(m), m.loadInfo())
	}
	switch key {
	case "n":
		return m.openForm(newForm("New workspace", "A workspace groups the repositories a task may change together.", "Create",
			[]*field{textField("name", "Name", "", "payments").mustFill()},
			func(f formValues) (tea.Cmd, error) {
				return m.startJob("workspace create "+f.str("name"), "", []string{"workspace", "create", f.str("name")}, reload), nil
			}))
	case "u", "enter":
		if w != nil && v.focus == 0 {
			return m.startJob("use "+w.Name, "", []string{"workspace", "use", w.Name}, reload)
		}
	case "a":
		if w == nil {
			return m.toast("create a workspace first (n)", toastInfo)
		}
		return m.openForm(newForm("Add repository to "+w.Name, "", "Add", []*field{
			textField("path", "Path to a git repository", "", "~/src/orders-service").mustFill(),
			textField("name", "Name (optional)", "", "default: directory name"),
		}, func(f formValues) (tea.Cmd, error) {
			args := []string{"workspace", "add", expandHome(f.str("path")), "-w", w.Name}
			if n := f.str("name"); n != "" {
				args = append(args, "--name", n)
			}
			return m.startJob("add repo to "+w.Name, "", args, reload), nil
		}))
	case "i":
		if w == nil {
			return nil
		}
		return m.indexForm(w, r)
	case "I":
		if w == nil {
			return nil
		}
		return m.indexForm(w, nil)
	case "d":
		if r != nil && v.focus == 1 {
			verb := "disable"
			if !r.Enabled {
				verb = "enable"
			}
			return m.startJob(verb+" "+r.Name, "", []string{"workspace", verb, r.Name, "-w", w.Name}, reload)
		}
	case "D", "delete":
		if r != nil && v.focus == 1 {
			return m.ask("Remove "+r.Name+" from "+w.Name+"?",
				"Removal is refused once tasks used the repository; disable it instead to keep its history.", true,
				func() tea.Cmd {
					return m.startJob("remove "+r.Name, "", []string{"workspace", "remove", r.Name, "-w", w.Name}, reload)
				})
		}
	case "R":
		return v.load(m)
	}
	return nil
}

func (m *Model) indexForm(w *Workspace, r *workspace.Repository) tea.Cmd {
	target := "all enabled repositories of " + w.Name
	if r != nil {
		target = r.Name
	}
	return m.openForm(newForm("Index "+target,
		"Builds the code graph with codebase-memory-mcp and, when enabled, scans cross-service contracts (HTTP, topics, env, Terraform).",
		"Index", []*field{choiceField("mode", "Mode", []string{"full", "moderate", "fast", "cross-repo-intelligence"}, "full")},
		func(f formValues) (tea.Cmd, error) {
			args := []string{"index", "-w", w.Name, "--mode", f.str("mode")}
			if r != nil {
				args = append(args, r.Name)
			}
			return m.startJob("index "+target, "", args, func(m *Model, err error) tea.Cmd {
				if err != nil {
					return m.errToast(err)
				}
				return tea.Batch(m.toast("indexed "+target, toastOK), m.workspace.load(m))
			}), nil
		}))
}

func (v *workspacesView) view(m *Model, w, h int) string {
	v.h = h - 3
	if !v.loaded {
		return "\n " + m.spin.View() + sMuted.Render(" loading workspaces…")
	}
	if v.err != nil {
		return "\n " + sErr.Render("✘ "+v.err.Error())
	}
	if len(v.data) == 0 {
		return emptyState(w, "No workspaces",
			"A workspace groups the git repositories a task may change.",
			"Press "+sKey.Render("n")+" to create one, then "+sKey.Render("a")+" to add repositories and "+sKey.Render("I")+" to index them.")
	}
	lw := min(30, w/3)
	rw := w - lw - 4
	var left strings.Builder
	left.WriteString(v.title("Workspaces", v.focus == 0) + "\n\n")
	from, to := v.wsCur.window(len(v.data), v.h)
	for i := from; i < to; i++ {
		ws := v.data[i]
		mark := "  "
		if ws.Current {
			mark = sAccent.Render("★ ")
		}
		line := mark + sText.Render(ws.Name) + sFaint.Render(fmt.Sprintf(" %d", len(ws.Repos)))
		if i == v.wsCur.pos {
			st := sSel
			if v.focus != 0 {
				st = lipgloss.NewStyle().Background(cBorder)
			}
			line = st.Render(fit(line, lw))
		}
		left.WriteString(line + "\n")
	}
	ws := v.selected()
	var right strings.Builder
	right.WriteString(v.title(ws.Name, v.focus == 1))
	if ws.Current {
		right.WriteString("  " + badge("current", cAccent))
	}
	right.WriteString("\n" + sFaint.Render(ws.ID+" · created "+ago(ws.CreatedAt)) + "\n\n")
	if len(ws.Repos) == 0 {
		right.WriteString(sMuted.Render("No repositories. Press ") + sKey.Render("a") + sMuted.Render(" to add one."))
	} else {
		widths := []int{1, 20, 0, 14, 22}
		right.WriteString(sFaint.Render(row(rw, widths, "", "REPOSITORY", "PATH", "LANGUAGES", "INDEX")) + "\n")
		rf, rt := v.repCur.window(len(ws.Repos), v.h-6)
		for i := rf; i < rt; i++ {
			r := ws.Repos[i]
			idx := sWarn.Render("not indexed")
			if r.IndexedAt != "" {
				idx = sOK.Render("✔ ") + sMuted.Render(ago(r.IndexedAt))
			}
			icon := statusIcon("ok")
			name := sText.Render(r.Name)
			if !r.Enabled {
				icon, name = sFaint.Render("○"), sFaint.Render(r.Name+" (disabled)")
			}
			line := row(rw, widths, icon, name, sMuted.Render(r.Path), sMuted.Render(strings.Join(r.Languages, ",")), idx)
			if v.focus == 1 && i == v.repCur.pos {
				line = sSel.Render(fit(line, rw))
			}
			right.WriteString(line + "\n")
		}
	}
	sep := lipgloss.NewStyle().Foreground(cBorder).Render(strings.TrimSuffix(strings.Repeat("│\n", h-1), "\n"))
	return lipgloss.JoinHorizontal(lipgloss.Top, " "+block(left.String(), lw, h-1), " ", sep, " ", block(right.String(), rw, h-1))
}

func (v *workspacesView) title(s string, focused bool) string {
	if focused {
		return sAccentB.Render(s)
	}
	return sTitle.Render(s)
}

func (v *workspacesView) hints(m *Model) []hint {
	if v.focus == 1 {
		return []hint{{"a", "add"}, {"i", "index repo"}, {"d", "enable/disable"}, {"D", "remove"}, {"esc", "workspaces"}}
	}
	return []hint{{"n", "new"}, {"enter", "use"}, {"a", "add repo"}, {"I", "index all"}, {"tab", "repositories"}}
}

func (v *workspacesView) commands(m *Model) []command {
	k := func(key string) func(m *Model) tea.Cmd { return func(m *Model) tea.Cmd { return v.action(m, key) } }
	cs := []command{
		{title: "New workspace…", group: "Workspaces", key: "n", run: k("n")},
		{title: "Add repository…", group: "Workspaces", key: "a", run: k("a")},
		{title: "Index all repositories…", group: "Workspaces", key: "I", run: k("I")},
	}
	if w := v.selected(); w != nil {
		cs = append(cs, command{title: "Use workspace " + w.Name, group: "Workspaces", key: "enter", run: func(m *Model) tea.Cmd {
			v.focus = 0
			return v.action(m, "u")
		}})
	}
	if r := v.selectedRepo(); r != nil && v.focus == 1 {
		cs = append(cs,
			command{title: "Index " + r.Name + "…", group: "Repository", key: "i", run: k("i")},
			command{title: "Enable/disable " + r.Name, group: "Repository", key: "d", run: k("d")},
			command{title: "Remove " + r.Name + "…", group: "Repository", key: "D", run: k("D")},
		)
	}
	return cs
}
