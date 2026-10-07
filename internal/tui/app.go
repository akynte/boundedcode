package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/akynte/boundedcode/internal/inference"
)

// view is one screen reachable from the sidebar.
type view interface {
	name() string
	// init is called each time the view becomes active.
	init(m *Model) tea.Cmd
	update(m *Model, msg tea.Msg) tea.Cmd
	view(m *Model, w, h int) string
	hints(m *Model) []hint
	// commands are this view's entries in the command palette.
	commands(m *Model) []command
	// capturing reports that a text input has focus, so single-key global
	// shortcuts must not fire.
	capturing() bool
	// refresh reloads data on the periodic tick.
	refresh(m *Model) tea.Cmd
}

type hint struct{ key, desc string }

// command is a command-palette entry.
type command struct {
	title, group, key string
	run               func(m *Model) tea.Cmd
}

type toastKind int

const (
	toastInfo toastKind = iota
	toastOK
	toastErr
)

type toast struct {
	id   int
	text string
	kind toastKind
}

type toastExpiredMsg struct{ id int }
type tickMsg time.Time
type infoMsg Info
type runtimeMsg struct {
	st  inference.Status
	err error
}

// Options configure the interface.
type Options struct {
	// Task opens this task on start.
	Task string
	// Dir is the directory the chat works in (the current directory).
	Dir string
}

// Model is the root Bubble Tea model.
type Model struct {
	ctx  context.Context
	be   Backend
	send func(tea.Msg)
	opt  Options

	w, h    int
	info    Info
	rt      *inference.Status
	rtErr   error
	rtAt    time.Time
	views   []view
	active  int
	modals  []modal
	toasts  []toast
	toastID int
	jobs    []*job
	nextJob int
	models  []ModelRow
	spin    spinner.Model
	ticks   int
	// quitting is set while running jobs are being interrupted on exit.
	quitting bool
	quitAt   time.Time
	// sideFocus: arrow keys move through the sidebar instead of the view.
	sideFocus bool
	// noTimers disables periodic commands (tests drive the model directly).
	noTimers bool

	chat      *chatView
	tasks     *tasksView
	detail    *taskView
	console   *consoleView
	workspace *workspacesView
}

const (
	sidebarW = 22
)

func newModel(ctx context.Context, be Backend, opt Options) *Model {
	m := &Model{ctx: ctx, be: be, opt: opt, send: func(tea.Msg) {}}
	m.spin = spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(sAccent))
	m.chat = newChatView()
	m.tasks = &tasksView{}
	m.detail = &taskView{}
	m.console = newConsoleView()
	m.workspace = &workspacesView{}
	m.views = []view{m.chat, m.tasks, m.workspace, newIntelView(), &runtimeView{}, &frontierView{}, &statsView{}, &systemView{}, m.console}
	m.info = be.Info(ctx)
	return m
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinTick(), m.tick(), m.loadRuntime(), tea.SetWindowTitle(m.info.Name)}
	if m.opt.Task != "" {
		cmds = append(cmds, m.chat.loadProject(m), m.chat.loadSetup(m), m.openTask(m.opt.Task))
	} else {
		cmds = append(cmds, m.current().init(m))
	}
	if m.info.Err != "" {
		cmds = append(cmds, m.toast("Configuration error: "+m.info.Err, toastErr))
	}
	return tea.Batch(cmds...)
}

func (m *Model) tick() tea.Cmd {
	if m.noTimers {
		return nil
	}
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// spinTick starts the spinner (it stops itself while idle).
func (m *Model) spinTick() tea.Cmd {
	if m.noTimers {
		return nil
	}
	return m.spin.Tick
}

func (m *Model) current() view {
	if m.detail.open {
		return m.detail
	}
	return m.views[m.active]
}

func (m *Model) contentSize() (int, int) {
	return max(20, m.w-sidebarW-1), max(5, m.h-2)
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, m.current().update(m, msg)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		if m.noTimers || m.runningJobs() == 0 && !m.anyLoading() {
			return m, nil // stop spinning while idle; startJob restarts it
		}
		return m, cmd
	case tickMsg:
		m.ticks++
		if m.quitting {
			if m.runningJobs() == 0 || time.Since(m.quitAt) > 20*time.Second {
				return m, tea.Quit
			}
			return m, m.tick()
		}
		cmds := []tea.Cmd{m.tick(), m.current().refresh(m)}
		if time.Since(m.rtAt) > 10*time.Second {
			cmds = append(cmds, m.loadRuntime())
		}
		return m, tea.Batch(cmds...)
	case infoMsg:
		m.info = Info(msg)
		return m, nil
	case runtimeMsg:
		m.rtAt = time.Now()
		if msg.err == nil {
			st := msg.st
			m.rt, m.rtErr = &st, nil
		} else {
			m.rt, m.rtErr = nil, msg.err
		}
		return m, nil
	case toastExpiredMsg:
		m.toasts = slices.DeleteFunc(m.toasts, func(t toast) bool { return t.id == msg.id })
		return m, nil
	case jobOutputMsg, jobDoneMsg:
		cmd := m.handleJobMsg(msg)
		cmds := []tea.Cmd{cmd, m.current().update(m, msg)}
		if _, done := msg.(jobDoneMsg); done && m.current() != view(m.chat) {
			cmds = append(cmds, m.chat.update(m, msg))
		}
		return m, tea.Batch(cmds...)
	case newTaskDataMsg:
		return m, m.showNewTaskForm(msg)
	case taskCreatedMsg:
		return m, m.taskCreated(msg)
	case showTextMsg:
		if msg.err != nil {
			return m, m.errToast(msg.err)
		}
		m.push(newTextModal(msg.title, msg.text))
		return m, nil
	case runEndedMsg:
		return m, tea.Batch(m.runEnded(msg.t), m.current().refresh(m))
	case promptMsg:
		m.push(&confirm{title: msg.title, body: msg.body, onAnswer: func(ok bool) tea.Cmd {
			msg.reply <- ok
			return nil
		}})
		return m, nil
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	case tea.MouseMsg:
		if len(m.modals) > 0 {
			return m, nil
		}
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && msg.X < sidebarW && msg.Y >= 2 {
			if i := msg.Y - 2; i < len(m.views) {
				m.sideFocus = false
				return m, m.switchView(i)
			}
		}
		return m, m.current().update(m, msg)
	}
	// Other messages (data loads, cursor blinks) go to the top dialog and
	// to the views that may be waiting for them.
	var cmds []tea.Cmd
	if len(m.modals) > 0 {
		done, cmd := m.modals[len(m.modals)-1].update(msg)
		if done {
			m.pop()
		}
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, m.current().update(m, msg))
	switch msg.(type) {
	case projectMsg, setupMsg, chatTaskMsg, chatDiffMsg, chatCreatedMsg, queuedMsg:
		if m.current() != view(m.chat) {
			cmds = append(cmds, m.chat.update(m, msg))
		}
	}
	if m.detail.open {
		// List views keep receiving their data while a task is open.
		cmds = append(cmds, m.views[m.active].update(m, msg))
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) handleKey(k tea.KeyMsg) tea.Cmd {
	if len(m.modals) > 0 {
		done, cmd := m.modals[len(m.modals)-1].update(k)
		if done {
			m.pop()
		}
		return cmd
	}
	if k.String() == "ctrl+c" {
		return m.quit()
	}
	cur := m.current()
	if m.sideFocus {
		if cmd, ok := m.sidebarKey(k); ok {
			return cmd
		}
		m.sideFocus = false // any other key acts on the view
	}
	if !cur.capturing() {
		switch s := k.String(); s {
		case "left", "h":
			if l, ok := cur.(interface{ usesLeft() bool }); !ok || !l.usesLeft() {
				m.sideFocus = true
				return nil
			}
		case "q":
			if m.detail.open {
				m.detail.close()
				return m.views[m.active].init(m)
			}
			return m.quit()
		case "ctrl+k", ":":
			m.push(newPalette(m))
			return nil
		case "?":
			m.push(&helpModal{m: m})
			return nil
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			return m.switchView(int(s[0] - '1'))
		case "J":
			return m.showJobs()
		}
	} else if k.String() == "ctrl+k" {
		m.push(newPalette(m))
		return nil
	}
	return cur.update(m, k)
}

// sidebarKey handles keys while the sidebar has focus: ↑/↓ switch views,
// →/enter/esc return to the view.
func (m *Model) sidebarKey(k tea.KeyMsg) (tea.Cmd, bool) {
	switch k.String() {
	case "up", "k", "shift+tab":
		return m.switchView((m.active + len(m.views) - 1) % len(m.views)), true
	case "down", "j", "tab":
		return m.switchView((m.active + 1) % len(m.views)), true
	case "home", "g":
		return m.switchView(0), true
	case "end", "G":
		return m.switchView(len(m.views) - 1), true
	case "right", "l", "enter", "esc", "space", " ":
		m.sideFocus = false
		return nil, true
	case "left", "h":
		return nil, true
	}
	return nil, false
}

func (m *Model) switchView(i int) tea.Cmd {
	if i < 0 || i >= len(m.views) {
		return nil
	}
	m.detail.close()
	m.active = i
	return m.current().init(m)
}

// openTask shows the task detail view.
func (m *Model) openTask(id string) tea.Cmd {
	m.active = slices.Index(m.views, view(m.tasks))
	return m.detail.openTask(m, id)
}

func (m *Model) showJobs() tea.Cmd {
	return m.switchView(slices.Index(m.views, view(m.console)))
}

func (m *Model) quit() tea.Cmd {
	n := m.runningJobs()
	if n == 0 {
		return tea.Quit
	}
	m.push(&confirm{title: "Quit BoundedCode?", danger: true,
		body: fmt.Sprintf("%d operation(s) are still running. Quitting interrupts them; tasks keep their work and can be resumed later.", n),
		onAnswer: func(ok bool) tea.Cmd {
			if !ok {
				return nil
			}
			for _, j := range m.jobs {
				if j.running() {
					j.cancel()
				}
			}
			m.quitting, m.quitAt = true, time.Now()
			return nil
		}})
	return nil
}

func (m *Model) push(md modal) {
	if f, ok := md.(*form); ok {
		f.init()
	}
	m.modals = append(m.modals, md)
}

func (m *Model) pop() {
	if len(m.modals) > 0 {
		m.modals = m.modals[:len(m.modals)-1]
	}
}

// openForm shows a form and focuses its first field.
func (m *Model) openForm(f *form) tea.Cmd {
	m.modals = append(m.modals, f)
	return f.init()
}

// ask shows a confirmation; yes runs the command.
func (m *Model) ask(title, body string, danger bool, yes func() tea.Cmd) tea.Cmd {
	m.push(&confirm{title: title, body: body, danger: danger, onAnswer: func(ok bool) tea.Cmd {
		if ok {
			return yes()
		}
		return nil
	}})
	return nil
}

func (m *Model) toast(text string, kind toastKind) tea.Cmd {
	m.toastID++
	id := m.toastID
	m.toasts = append(m.toasts, toast{id, text, kind})
	if len(m.toasts) > 3 {
		m.toasts = m.toasts[len(m.toasts)-3:]
	}
	d := 4 * time.Second
	if kind == toastErr {
		d = 8 * time.Second
	}
	if m.noTimers {
		return nil
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return toastExpiredMsg{id} })
}

func (m *Model) errToast(err error) tea.Cmd {
	if err == nil {
		return nil
	}
	return m.toast(firstLine(err.Error()), toastErr)
}

func (m *Model) loadRuntime() tea.Cmd {
	be, ctx := m.be, m.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		st, err := be.Runtime(ctx)
		return runtimeMsg{st, err}
	}
}

func (m *Model) loadInfo() tea.Cmd {
	be, ctx := m.be, m.ctx
	return func() tea.Msg { return infoMsg(be.Info(ctx)) }
}

// refreshAll reloads the header and the active view after an operation.
func (m *Model) refreshAll() tea.Cmd {
	return tea.Batch(m.loadInfo(), m.loadRuntime(), m.current().refresh(m))
}

func (m *Model) anyLoading() bool {
	if l, ok := m.current().(interface{ loading() bool }); ok {
		return l.loading()
	}
	return false
}

// View implements tea.Model.
func (m *Model) View() string {
	if m.w == 0 {
		return ""
	}
	if m.w < 60 || m.h < 16 {
		msg := wrap(fmt.Sprintf("Terminal too small (%d×%d); need at least 60×16", m.w, m.h), m.w)
		return block(lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, sMuted.Render(msg)), m.w, m.h)
	}
	cw, ch := m.contentSize()
	content := block(m.current().view(m, cw, ch), cw, ch)
	if len(m.modals) > 0 {
		content = overlay(content, m.modals[len(m.modals)-1].view(cw, ch), cw, ch)
	}
	sep := lipgloss.NewStyle().Foreground(cBorder).Render(strings.TrimSuffix(strings.Repeat("│\n", ch), "\n"))
	body := lipgloss.JoinHorizontal(lipgloss.Top, block(m.sidebar(ch), sidebarW, ch), sep, content)
	return m.header() + "\n" + body + "\n" + m.footer()
}

func (m *Model) header() string {
	logo := lipgloss.NewStyle().Foreground(cAccent).Bold(true).Render(" ◆ " + productName(m.info.Name))
	ver := sFaint.Render(" " + m.info.Version)
	sep := sFaint.Render("  │  ")
	var segs []string
	ws := m.info.CurrentWorkspace
	if ws == "" {
		ws = sFaint.Render("no workspace")
	} else {
		ws = sText.Render(ws)
	}
	segs = append(segs, sMuted.Render("ws ")+ws)
	segs = append(segs, sMuted.Render("model ")+sText.Render(orDash(m.modelName()))+" "+m.runtimeDot())
	sb := m.info.SandboxKind
	if sb == "none" {
		sb = sWarn.Render("unsandboxed")
	} else {
		sb = sText.Render(sb)
	}
	segs = append(segs, sMuted.Render("sandbox ")+sb)
	fr := sFaint.Render("off")
	if m.info.FrontierEnabled {
		fr = sText.Render(m.info.FrontierProvider)
	}
	segs = append(segs, sMuted.Render("frontier ")+fr)
	left := logo + ver + "   " + strings.Join(segs, sep)
	right := ""
	if n := m.runningJobs(); n > 0 {
		right = m.spin.View() + sAccent.Render(fmt.Sprintf(" %d running ", n))
	}
	right += sFaint.Render(time.Now().Format("15:04") + " ")
	gap := m.w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		left = trunc(left, m.w-lipgloss.Width(right)-1)
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func productName(name string) string {
	if name == "boundedcode" || name == "" {
		return "BoundedCode"
	}
	return name
}

func (m *Model) modelName() string {
	if m.info.ProviderModel != "" {
		return m.info.Provider + " " + m.info.ProviderModel
	}
	if m.rt != nil && m.rt.Profile != "" {
		return m.rt.Profile
	}
	return m.info.DefaultModel
}

func (m *Model) runtimeDot() string {
	switch {
	case m.rt == nil && m.rtErr == nil:
		return sFaint.Render("○")
	case m.rt == nil:
		return sErr.Render("● error")
	case m.info.ProviderModel != "" && m.rt.Healthy:
		return sOK.Render("● cloud")
	case m.info.ProviderModel != "":
		return sErr.Render("● no API key")
	case m.rt.Healthy && m.rt.Sleeping:
		return sInfo.Render("● sleeping")
	case m.rt.Healthy:
		return sOK.Render("● serving")
	case m.rt.Running:
		return sWarn.Render("● starting")
	}
	return sFaint.Render("○ stopped")
}

var viewIcons = []string{"❯", "▤", "◫", "⌕", "⚙", "⇪", "▥", "✚", "▸"}

func (m *Model) sidebar(h int) string {
	var b strings.Builder
	b.WriteString(sFaint.Render(" NAVIGATE") + "\n")
	for i, v := range m.views {
		label := fmt.Sprintf(" %s  %-12s", viewIcons[i], v.name())
		key := sFaint.Render(fmt.Sprint(i + 1))
		if i == m.active {
			line := lipgloss.NewStyle().Foreground(cAccent).Bold(true).Render(label)
			bar := sAccent.Render("▍")
			bg := cSelBg
			if m.sideFocus {
				bar = sAccent.Render("▶")
				bg = cAccentBg
			}
			b.WriteString(lipgloss.NewStyle().Background(bg).Render(fit(bar+line+" "+key, sidebarW)) + "\n")
		} else {
			b.WriteString(fit(" "+sMuted.Render(label)+" "+key, sidebarW) + "\n")
		}
	}
	b.WriteString("\n" + sFaint.Render(" ACTIVITY") + "\n")
	shown := 0
	room := h - len(m.views) - 4
	for i := len(m.jobs) - 1; i >= 0 && shown < room; i-- {
		j := m.jobs[i]
		icon := statusIcon(j.status())
		if j.running() {
			icon = m.spin.View()
		}
		b.WriteString(fit(" "+icon+" "+sText.Render(trunc(j.title, sidebarW-5)), sidebarW) + "\n")
		shown++
	}
	if len(m.jobs) == 0 {
		b.WriteString(sFaint.Render("  nothing yet") + "\n")
	}
	return b.String()
}

func (m *Model) footer() string {
	var parts []string
	hs := m.current().hints(m)
	if m.sideFocus {
		hs = []hint{{"↑/↓", "switch view"}, {"→/enter", "open"}}
	} else if l, ok := m.current().(interface{ usesLeft() bool }); !m.current().capturing() && (!ok || !l.usesLeft()) {
		hs = append([]hint{{"←", "views"}}, hs...)
	}
	for _, h := range hs {
		parts = append(parts, sKey.Render(h.key)+" "+sMuted.Render(h.desc))
	}
	parts = append(parts, sKey.Render("ctrl+k")+" "+sMuted.Render("commands"), sKey.Render("?")+" "+sMuted.Render("help"))
	left := " " + strings.Join(parts, sFaint.Render("  ·  "))
	right := ""
	if len(m.toasts) > 0 {
		t := m.toasts[len(m.toasts)-1]
		switch t.kind {
		case toastOK:
			right = badge("✔", cOK) + " " + sText.Render(t.text)
		case toastErr:
			right = badge("✘", cErr) + " " + sErr.Render(t.text)
		default:
			right = badge("i", cInfo) + " " + sText.Render(t.text)
		}
		right = trunc(right, m.w*3/5) + " "
	}
	if m.quitting {
		right = m.spin.View() + sWarn.Render(" stopping running operations… ")
	}
	lw := m.w - lipgloss.Width(right)
	return fit(trunc(left, lw), lw) + right
}

// globalCommands are always available in the palette.
func (m *Model) globalCommands() []command {
	var cs []command
	for i, v := range m.views {
		cs = append(cs, command{title: "Go to " + v.name(), group: "Navigate", key: fmt.Sprint(i + 1), run: func(m *Model) tea.Cmd { return m.switchView(i) }})
	}
	cs = append(cs,
		command{title: "New task…", group: "Tasks", key: "n", run: func(m *Model) tea.Cmd { return m.newTaskForm() }},
		command{title: "Open task by id…", group: "Tasks", run: func(m *Model) tea.Cmd { return m.openTaskForm() }},
		command{title: "Run a CLI command…", group: "Console", run: func(m *Model) tea.Cmd { m.showJobs(); return m.console.focusInput() }},
		command{title: "Show activity / jobs", group: "Console", key: "J", run: func(m *Model) tea.Cmd { return m.showJobs() }},
		command{title: "Keyboard shortcuts", group: "Help", key: "?", run: func(m *Model) tea.Cmd { m.push(&helpModal{m: m}); return nil }},
		command{title: "Quit", group: "Help", key: "q", run: func(m *Model) tea.Cmd { return m.quit() }},
	)
	return cs
}

func (m *Model) openTaskForm() tea.Cmd {
	return m.openForm(newForm("Open task", "", "Open", []*field{textField("id", "Task id", "", "t-…").mustFill()},
		func(v formValues) (tea.Cmd, error) { return m.openTask(v.str("id")), nil }))
}

// Run starts the interface and blocks until it exits.
func Run(ctx context.Context, be Backend, opt Options) error {
	// Ask the terminal for its background colour now, while it is still in
	// normal mode: done lazily during rendering, the query races Bubble Tea
	// for input and swallows keystrokes (and stalls up to its timeout).
	lipgloss.SetHasDarkBackground(lipgloss.HasDarkBackground())
	m := newModel(ctx, be, opt)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithContext(ctx))
	m.send = p.Send
	be.SetPrompter(m.prompt)
	_, err := p.Run()
	for _, j := range m.jobs {
		if j.running() {
			j.cancel()
		}
	}
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		err = nil // interrupted by a signal: a normal exit
	}
	return err
}
