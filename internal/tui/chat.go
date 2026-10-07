package tui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/telemetry"
)

// The chat is the default view: each message becomes a task in the
// repository the interface was started in, or steers the current one.
//
//   - no current task, or the last one failed or was cancelled: a new task
//     from the checkout's HEAD;
//   - the current task completed: a follow-up task that starts from its
//     branch, so it builds on that work;
//   - the current task is blocked or paused: the message is passed to it
//     as a clarification and the task resumes;
//   - the current task is running: the message is queued and sent when the
//     run ends.

type entryKind int

const (
	entryUser entryKind = iota
	entryNote
	entryError
	entryTask
	entryBlock // pre-rendered lines (diff, help)
)

type chatEntry struct {
	kind   entryKind
	text   string
	taskID string
	lines  []string
}

// chatTask is what the transcript knows about one task.
type chatTask struct {
	d       *TaskDetail
	events  []telemetry.Event
	lastEv  int64
	loading bool
	diffs   []RepoDiff
	diffErr error
	// render cache of the activity lines
	cacheN, cacheW int
	cache          []string
}

type slashCmd struct {
	name, args, help string
}

var slashCmds = []slashCmd{
	{"/help", "", "show what you can do here"},
	{"/diff", "", "show the current task's changes"},
	{"/apply", "[--commit]", "bring the current task's changes into your checkout (staged, or committed)"},
	{"/verify", "[--full]", "run verification on the current task"},
	{"/review", "", "ask the frontier model to review the current task (if enabled)"},
	{"/cancel", "", "cancel the current task"},
	{"/new", "", "start over: the next message is a new task from your HEAD"},
	{"/open", "", "open the current task's full view (activity, diff, verification)"},
	{"/status", "", "show the current task's status and budget"},
	{"/model", "NAME", "use another model profile for the next runs"},
	{"/index", "", "re-index this repository"},
	{"/setup", "", "choose the model or provider and install what is missing"},
	{"/git-init", "", "make this folder a git repository"},
	{"/clear", "", "clear the transcript"},
	{"/quit", "", "quit"},
}

type chatView struct {
	input   textarea.Model
	focused bool
	entries []chatEntry
	tasks   map[string]*chatTask
	current string // task id the conversation is about
	queue   []string
	model   string // model profile override for runs
	history []string
	hpos    int

	project     *Project
	projErr     error
	setup       []SetupStep
	setupLoaded bool
	indexing    bool

	scroll scroller
	lines  []string
	h      int
	sugg   []slashCmd
	scur   int
}

type projectMsg struct {
	p   Project
	err error
}

type setupMsg []SetupStep

type chatTaskMsg struct {
	id     string
	d      TaskDetail
	err    error
	events []telemetry.Event
}

type chatDiffMsg struct {
	id    string
	diffs []RepoDiff
	err   error
	show  bool
}

type chatCreatedMsg struct {
	id   string
	err  error
	from string
}

func newChatView() *chatView {
	ta := textarea.New()
	ta.Placeholder = "Describe a change, ask for a fix, or type / for commands"
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.CharLimit = 0
	ta.SetHeight(1)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Placeholder = sFaint
	ta.BlurredStyle.Placeholder = sFaint
	ta.FocusedStyle.Text = sText
	ta.BlurredStyle.Text = sMuted
	ta.KeyMap.InsertNewline.SetEnabled(false) // enter sends; alt+enter / ctrl+j add a line
	v := &chatView{input: ta, tasks: map[string]*chatTask{}, scroll: scroller{follow: true}}
	return v
}

func (v *chatView) name() string    { return "Chat" }
func (v *chatView) capturing() bool { return v.focused }
func (v *chatView) loading() bool {
	return v.project == nil || !v.setupLoaded
}

func (v *chatView) init(m *Model) tea.Cmd {
	cmds := []tea.Cmd{v.focus()}
	if v.project == nil && v.projErr == nil {
		cmds = append(cmds, v.loadProject(m), v.loadSetup(m))
	}
	return tea.Batch(cmds...)
}

func (v *chatView) focus() tea.Cmd {
	v.focused = true
	return v.input.Focus()
}

func (v *chatView) loadProject(m *Model) tea.Cmd {
	be, ctx, dir := m.be, m.ctx, m.opt.Dir
	return func() tea.Msg {
		p, err := be.Project(ctx, dir)
		return projectMsg{p, err}
	}
}

func (v *chatView) loadSetup(m *Model) tea.Cmd {
	be, ctx := m.be, m.ctx
	return func() tea.Msg { return setupMsg(be.Setup(ctx)) }
}

func (v *chatView) refresh(m *Model) tea.Cmd {
	if v.current == "" {
		return nil
	}
	return v.loadTask(m, v.current)
}

func (v *chatView) loadTask(m *Model, id string) tea.Cmd {
	ct := v.tasks[id]
	if ct == nil {
		ct = &chatTask{}
		v.tasks[id] = ct
	}
	if ct.loading {
		return nil
	}
	ct.loading = true
	after := ct.lastEv
	be, ctx := m.be, m.ctx
	return func() tea.Msg {
		msg := chatTaskMsg{id: id}
		msg.d, msg.err = be.Task(ctx, id)
		if msg.err == nil {
			msg.events, _ = be.Events(ctx, msg.d.Task.ID, after, 5000)
		}
		return msg
	}
}

func (v *chatView) loadDiff(m *Model, id string, show bool) tea.Cmd {
	be, ctx := m.be, m.ctx
	return func() tea.Msg {
		ds, err := be.Diffs(ctx, id)
		return chatDiffMsg{id, ds, err, show}
	}
}

// ready reports whether tasks can be started, and why not.
func (v *chatView) ready() (bool, string) {
	switch {
	case v.project == nil:
		return false, "still looking at this folder…"
	case v.project.Root == "":
		return false, "this folder is not a git repository; type /git-init to make it one (tasks work on git worktrees)"
	}
	for _, s := range v.setup {
		if !s.OK {
			return false, "setup is not complete (" + s.Title + ": " + s.Detail + "); type /setup"
		}
	}
	return true, ""
}

func (v *chatView) note(text string) {
	v.entries = append(v.entries, chatEntry{kind: entryNote, text: text})
}
func (v *chatView) fail(text string) {
	v.entries = append(v.entries, chatEntry{kind: entryError, text: text})
}

func (v *chatView) currentTask() *task.Task {
	if ct := v.tasks[v.current]; ct != nil && ct.d != nil {
		return ct.d.Task
	}
	return nil
}

func (v *chatView) running(m *Model) bool { return v.current != "" && m.taskRunning(v.current) }

func (v *chatView) update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case projectMsg:
		v.projErr = msg.err
		p := msg.p
		v.project = &p
		if msg.err != nil {
			v.fail("Could not register this folder: " + msg.err.Error())
			return nil
		}
		if p.Created {
			v.note(fmt.Sprintf("Registered %s as workspace %s.", p.Root, p.Workspace))
		}
		return tea.Batch(m.loadInfo(), v.maybeIndex(m))
	case setupMsg:
		v.setup, v.setupLoaded = msg, true
		return v.maybeIndex(m)
	case chatTaskMsg:
		ct := v.tasks[msg.id]
		if ct == nil {
			return nil
		}
		ct.loading = false
		if msg.err != nil {
			return nil
		}
		d := msg.d
		ct.d = &d
		ct.events = append(ct.events, msg.events...)
		if n := len(ct.events); n > 0 {
			ct.lastEv = ct.events[n-1].ID
		}
		return nil
	case chatDiffMsg:
		ct := v.tasks[msg.id]
		if ct == nil {
			return nil
		}
		ct.diffs, ct.diffErr = msg.diffs, msg.err
		if msg.show {
			v.showDiff(msg.id)
		}
		return nil
	case chatCreatedMsg:
		if msg.err != nil {
			v.fail("Could not create the task: " + msg.err.Error())
			return nil
		}
		v.current = msg.id
		v.entries = append(v.entries, chatEntry{kind: entryTask, taskID: msg.id})
		v.scroll.follow = true
		args := []string{"task", "run", msg.id}
		if v.model != "" {
			args = append(args, "--model", v.model)
		}
		return tea.Batch(v.loadTask(m, msg.id), v.startRun(m, msg.id, "run "+msg.id, args))
	case jobDoneMsg:
		j := m.job(msg.id)
		if j == nil {
			return nil
		}
		var cmds []tea.Cmd
		if j.taskID != "" && v.tasks[j.taskID] != nil {
			cmds = append(cmds, v.loadTask(m, j.taskID), v.loadDiff(m, j.taskID, false))
		}
		if len(j.args) > 0 && (j.args[0] == "setup" || j.args[0] == "init") {
			cmds = append(cmds, v.loadSetup(m), m.loadInfo())
		}
		if len(j.args) > 0 && j.args[0] == "index" {
			v.indexing = false
			if msg.err == nil && v.project != nil {
				v.project.Indexed = true
			}
		}
		// Send what was typed while the task ran.
		if j.taskID == v.current && len(v.queue) > 0 && !v.running(m) {
			next := v.queue[0]
			v.queue = v.queue[1:]
			cmds = append(cmds, func() tea.Msg { return queuedMsg(next) })
		}
		return tea.Batch(cmds...)
	case queuedMsg:
		return v.send(m, string(msg))
	case tea.KeyMsg:
		return v.key(m, msg)
	case tea.MouseMsg:
		v.scroll.handle(msg, len(v.lines), v.h)
		return nil
	}
	if v.focused {
		var cmd tea.Cmd
		v.input, cmd = v.input.Update(msg)
		return cmd
	}
	return nil
}

type queuedMsg string

func (v *chatView) key(m *Model, k tea.KeyMsg) tea.Cmd {
	s := k.String()
	// Scrolling works whether or not the input has focus.
	switch s {
	case "pgup", "pgdown", "ctrl+u", "ctrl+d":
		v.scroll.handle(k, len(v.lines), v.h)
		return nil
	}
	if !v.focused {
		switch s {
		case "enter", "i", "/":
			cmd := v.focus()
			if s == "/" {
				v.input.SetValue("/")
				v.input.CursorEnd()
				v.suggest()
			}
			return cmd
		case "esc":
			if v.running(m) {
				return v.interrupt(m)
			}
			return nil
		}
		v.scroll.handle(k, len(v.lines), v.h)
		return nil
	}
	switch s {
	case "enter":
		if len(v.sugg) > 0 && strings.HasPrefix(v.input.Value(), "/") && !strings.Contains(v.input.Value(), " ") {
			c := v.sugg[min(v.scur, len(v.sugg)-1)]
			if c.args == "" || strings.TrimSpace(v.input.Value()) == c.name {
				v.input.SetValue(c.name)
			} else {
				v.input.SetValue(c.name + " ")
				v.input.CursorEnd()
				v.suggest()
				return nil
			}
		}
		text := strings.TrimSpace(v.input.Value())
		if text == "" {
			return nil
		}
		v.input.Reset()
		v.sugg = nil
		v.resize()
		if len(v.history) == 0 || v.history[len(v.history)-1] != text {
			v.history = append(v.history, text)
		}
		v.hpos = len(v.history)
		v.scroll.follow = true
		return v.send(m, text)
	case "alt+enter", "ctrl+j":
		v.input.InsertString("\n")
		v.resize()
		return nil
	case "tab":
		if len(v.sugg) > 0 {
			c := v.sugg[min(v.scur, len(v.sugg)-1)]
			v.input.SetValue(c.name + " ")
			v.input.CursorEnd()
			v.suggest()
		}
		return nil
	case "esc":
		switch {
		case v.running(m):
			return v.interrupt(m)
		case v.input.Value() != "":
			v.input.Reset()
			v.sugg = nil
			v.resize()
		default:
			v.focused = false
			v.input.Blur()
		}
		return nil
	case "up", "down":
		if len(v.sugg) > 0 {
			if s == "up" {
				v.scur = max(0, v.scur-1)
			} else {
				v.scur = min(len(v.sugg)-1, v.scur+1)
			}
			return nil
		}
		if v.input.LineCount() <= 1 && len(v.history) > 0 {
			if s == "up" && v.hpos > 0 {
				v.hpos--
				v.input.SetValue(v.history[v.hpos])
			} else if s == "down" {
				if v.hpos < len(v.history)-1 {
					v.hpos++
					v.input.SetValue(v.history[v.hpos])
				} else {
					v.hpos = len(v.history)
					v.input.Reset()
				}
			}
			v.resize()
			return nil
		}
	}
	var cmd tea.Cmd
	v.input, cmd = v.input.Update(k)
	v.suggest()
	v.resize()
	return cmd
}

func (v *chatView) suggest() {
	val := v.input.Value()
	v.sugg = nil
	if !strings.HasPrefix(val, "/") || strings.ContainsAny(val, " \n") {
		return
	}
	for _, c := range slashCmds {
		if strings.HasPrefix(c.name, val) {
			v.sugg = append(v.sugg, c)
		}
	}
	v.scur = min(v.scur, max(0, len(v.sugg)-1))
}

func (v *chatView) resize() {
	v.input.SetHeight(max(1, min(8, v.input.LineCount())))
}

func (v *chatView) interrupt(m *Model) tea.Cmd {
	if j := m.taskJob(v.current); j != nil && j.running() {
		j.cancel()
		v.note("Interrupting… the work so far is kept; send a message to continue, or /new to start over.")
	}
	return nil
}

func (v *chatView) startRun(m *Model, id, title string, args []string) tea.Cmd {
	return m.startJob(title, id, args, func(m *Model, err error) tea.Cmd {
		if err != nil {
			v.fail(title + " failed: " + err.Error())
		}
		return nil
	})
}

// send handles a submitted message or slash command.
func (v *chatView) send(m *Model, text string) tea.Cmd {
	if strings.HasPrefix(text, "/") {
		v.entries = append(v.entries, chatEntry{kind: entryUser, text: text})
		return v.slash(m, text)
	}
	v.entries = append(v.entries, chatEntry{kind: entryUser, text: text})
	if ok, why := v.ready(); !ok {
		v.fail("Can't start a task: " + why)
		return nil
	}
	if v.running(m) {
		v.queue = append(v.queue, text)
		v.note("Queued: it will be sent when the current run ends (esc interrupts the run).")
		return nil
	}
	t := v.currentTask()
	switch {
	case t != nil && (t.Status == task.StatusBlocked || t.Status == task.StatusActive):
		args := []string{"task", "run", t.ID, "--clarify", text}
		if v.model != "" {
			args = append(args, "--model", v.model)
		}
		v.entries = append(v.entries, chatEntry{kind: entryNote, text: "Continuing " + t.ID + " with your answer."})
		return v.startRun(m, t.ID, "resume "+t.ID, args)
	default:
		req := CreateTaskRequest{Workspace: v.project.Workspace, Request: text, Model: v.model}
		if t != nil && t.Status == task.StatusCompleted {
			req.FromTask = t.ID
		}
		be, ctx := m.be, m.ctx
		return func() tea.Msg {
			id, err := be.CreateTask(ctx, req)
			return chatCreatedMsg{id, err, req.FromTask}
		}
	}
}

func (v *chatView) slash(m *Model, text string) tea.Cmd {
	fields := strings.Fields(text)
	name, rest := fields[0], fields[1:]
	t := v.currentTask()
	needTask := func() bool {
		if t == nil {
			v.fail("No task yet in this conversation: describe a change first.")
			return false
		}
		return true
	}
	switch name {
	case "/help":
		var ls []string
		ls = append(ls, sSection.Render("Talk to BoundedCode"),
			sText.Render("Describe what you want changed. Each request runs as a task in an isolated worktree"),
			sText.Render("on branch agent/<task>, inside a sandbox; your checkout is untouched until you /apply."),
			sText.Render("While a task runs you can keep typing (messages are queued); esc interrupts."),
			sText.Render("If a task stops to ask a question, just answer. After it finishes, a new message"),
			sText.Render("continues from its result."), "", sSection.Render("Commands"))
		for _, c := range slashCmds {
			ls = append(ls, sKey.Render(fit(c.name+" "+c.args, 22))+sMuted.Render(c.help))
		}
		ls = append(ls, "", sMuted.Render("Keys: enter sends · alt+enter or ctrl+j new line · ↑/↓ history · pgup/pgdn scroll · esc leaves the input"),
			sMuted.Render("      ctrl+k all commands · with the input left: ← views, 1–9 switch view"))
		v.entries = append(v.entries, chatEntry{kind: entryBlock, lines: ls})
	case "/clear":
		v.entries = nil
	case "/new":
		v.current = ""
		v.note("Starting fresh: the next message creates a new task from your current HEAD.")
	case "/quit", "/exit":
		return m.quit()
	case "/open":
		if needTask() {
			return m.openTask(t.ID)
		}
	case "/status":
		if needTask() {
			b := t.Budget
			v.note(fmt.Sprintf("%s · %s · %s · verification %s · attempts %d/%d · %s tokens · %s",
				t.ID, t.Status, t.Phase, orDash(t.VerificationState), t.AttemptCount, b.MaxAttempts, human(b.UsedLocalTokens), dur(b.UsedWallClockS)))
		}
	case "/diff":
		if needTask() {
			return v.loadDiff(m, t.ID, true)
		}
	case "/apply":
		if !needTask() {
			return nil
		}
		commit := slices.Contains(rest, "--commit")
		args := []string{"task", "apply", t.ID}
		what := "staged in your checkout for you to review and commit"
		if commit {
			args = append(args, "--commit")
			what = "committed on your current branch"
		}
		body := "The changes of " + t.ID + " will be " + what + "."
		if t.Status != task.StatusCompleted {
			args = append(args, "--force")
			body += " The task is " + string(t.Status) + ", so they are NOT verified."
		}
		return m.ask("Apply "+t.ID+" to your checkout?", body, t.Status != task.StatusCompleted, func() tea.Cmd {
			return m.startJob("apply "+t.ID, t.ID, args, func(m *Model, err error) tea.Cmd {
				if err != nil {
					v.fail("Apply failed: " + err.Error())
				} else {
					v.note("Applied. Your checkout now has the changes of " + t.ID + ".")
				}
				return nil
			})
		})
	case "/verify":
		if needTask() {
			return m.verifyTask(t, slices.Contains(rest, "--full"))
		}
	case "/review":
		if needTask() {
			if !m.info.FrontierEnabled {
				v.fail("Frontier review is disabled (frontier.enabled in " + m.info.ConfigFile + ").")
				return nil
			}
			return v.startRun(m, t.ID, "review "+t.ID, []string{"frontier", "review", t.ID})
		}
	case "/cancel":
		if needTask() {
			return m.taskAction("c", t)
		}
	case "/model":
		if len(rest) == 0 {
			cur := v.model
			if cur == "" {
				cur = m.info.DefaultModel + " (default)"
			}
			var names []string
			m.cachedModels(func(rs []ModelRow) {
				for _, r := range rs {
					names = append(names, r.Name)
				}
			})
			v.note("Model: " + cur + ". Available: " + strings.Join(names, ", "))
			return nil
		}
		v.model = rest[0]
		v.note("Next runs use model profile " + v.model + ".")
	case "/index":
		if v.project == nil || v.project.Root == "" {
			v.fail("Nothing to index: this folder is not a git repository.")
			return nil
		}
		return v.index(m)
	case "/setup":
		v.note("Choose where the model runs; set-up then installs what is missing (you are asked first).")
		return m.startWizard()
	case "/git-init":
		if v.project != nil && v.project.Root != "" {
			v.note("This folder is already in a git repository: " + v.project.Root)
			return nil
		}
		dir := m.opt.Dir
		return m.ask("Make "+dir+" a git repository?", "Runs git init and commits the current files as the initial commit, so tasks can work on worktrees of it.", false, func() tea.Cmd {
			be, ctx := m.be, m.ctx
			return func() tea.Msg {
				if err := be.GitInit(ctx, dir); err != nil {
					return projectMsg{Project{Dir: dir}, err}
				}
				p, err := be.Project(ctx, dir)
				return projectMsg{p, err}
			}
		})
	default:
		v.fail("Unknown command " + name + " (type /help).")
	}
	return nil
}

// maybeIndex indexes a repository registered for the first time once the
// tools are installed.
func (v *chatView) maybeIndex(m *Model) tea.Cmd {
	if v.project == nil || v.project.Root == "" || v.project.Indexed || v.indexing || !v.setupLoaded {
		return nil
	}
	for _, s := range v.setup {
		if s.Name == "tools" && !s.OK {
			return nil
		}
	}
	return v.index(m)
}

func (v *chatView) index(m *Model) tea.Cmd {
	v.indexing = true
	v.note("Indexing " + v.project.Repo + " in the background (code graph and cross-service contracts)…")
	return m.startJob("index "+v.project.Repo, "", []string{"index", "-w", v.project.Workspace, v.project.Repo}, func(m *Model, err error) tea.Cmd {
		if err != nil {
			v.fail("Indexing failed: " + err.Error() + " (tasks still work, with less repository context)")
		}
		return nil
	})
}

func (v *chatView) showDiff(id string) {
	ct := v.tasks[id]
	if ct.diffErr != nil {
		v.fail("Diff: " + ct.diffErr.Error())
		return
	}
	if len(ct.diffs) == 0 {
		v.note("No changes yet in " + id + ".")
		return
	}
	var ls []string
	for _, d := range ct.diffs {
		ls = append(ls, sSection.Render("■ "+d.Repo))
		ls = append(ls, colorDiff(d.Diff, 400)...)
	}
	v.entries = append(v.entries, chatEntry{kind: entryBlock, lines: ls})
}

func (v *chatView) hints(m *Model) []hint {
	if v.running(m) {
		return []hint{{"esc", "interrupt"}, {"enter", "queue a message"}, {"pgup/pgdn", "scroll"}}
	}
	if !v.focused {
		return []hint{{"enter", "type"}, {"↑/↓", "scroll"}}
	}
	return []hint{{"enter", "send"}, {"/", "commands"}, {"alt+enter", "new line"}, {"esc", "leave input"}}
}

func (v *chatView) commands(m *Model) []command {
	var cs []command
	for _, c := range slashCmds {
		if c.args != "" && c.args[0] != '[' {
			continue
		}
		name := c.name
		cs = append(cs, command{title: name + " — " + c.help, group: "Chat", run: func(m *Model) tea.Cmd {
			v.entries = append(v.entries, chatEntry{kind: entryUser, text: name})
			return v.slash(m, name)
		}})
	}
	return cs
}

// view renders the transcript above the input.
func (v *chatView) view(m *Model, w, h int) string {
	iw := w - 4
	v.input.SetWidth(iw - 2)
	// Input box.
	border := cBorder
	if v.focused {
		border = cAccent
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(0, 1).Width(iw)
	inputBox := box.Render(sAccent.Render("❯ ") + v.input.View())
	// Status line between transcript and input.
	status := v.statusLine(m, w)
	// Slash command suggestions.
	var sugg []string
	for i, c := range v.sugg {
		line := "  " + fit(c.name+" "+c.args, 24) + sMuted.Render(c.help)
		if i == v.scur {
			line = sSel.Render(fit(line, w-2))
		}
		sugg = append(sugg, line)
	}
	bottom := strings.Join(append(sugg, status, inputBox), "\n")
	v.h = max(1, h-lipgloss.Height(bottom))
	v.lines = v.transcript(m, w-2)
	vis, _, _ := v.scroll.window(v.lines, v.h)
	var b strings.Builder
	for i := 0; i < v.h-len(vis); i++ {
		b.WriteString("\n") // keep the conversation anchored at the bottom
	}
	for _, l := range vis {
		b.WriteString(" " + trunc(l, w-2) + "\n")
	}
	b.WriteString(bottom)
	return b.String()
}

func (v *chatView) statusLine(m *Model, w int) string {
	left := ""
	switch {
	case v.running(m):
		j := m.taskJob(v.current)
		info := ""
		if t := v.currentTask(); t != nil {
			info = fmt.Sprintf(" · attempt %d/%d · %s tokens", max(1, t.AttemptCount), t.Budget.MaxAttempts, human(t.Budget.UsedLocalTokens))
		}
		left = m.spin.View() + sAccent.Render(" Working") + sMuted.Render(fmt.Sprintf(" %s%s", j.elapsed().Round(time.Second), info)) + sFaint.Render("  esc to interrupt")
		if n := len(v.queue); n > 0 {
			left += sFaint.Render(fmt.Sprintf(" · %d queued", n))
		}
	case v.indexing:
		left = m.spin.View() + sMuted.Render(" indexing "+v.project.Repo+"…")
	}
	right := ""
	if p := v.project; p != nil && p.Root != "" {
		right = sMuted.Render(p.Repo) + sFaint.Render(" ⎇ "+orDash(p.Branch))
		if v.current != "" {
			right += sFaint.Render(" · task " + v.current)
		}
	}
	gap := w - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		return " " + trunc(left, w-2)
	}
	return " " + left + strings.Repeat(" ", gap) + right
}

func (v *chatView) transcript(m *Model, w int) []string {
	var out []string
	out = append(out, v.welcome(m, w)...)
	for _, e := range v.entries {
		out = append(out, "")
		switch e.kind {
		case entryUser:
			for i, l := range strings.Split(wrap(e.text, w-4), "\n") {
				mark := "  "
				if i == 0 {
					mark = sAccent.Render("❯ ")
				}
				out = append(out, mark+sBold.Render(l))
			}
		case entryNote:
			for _, l := range strings.Split(wrap(e.text, w-4), "\n") {
				out = append(out, sMuted.Render("  "+l))
			}
		case entryError:
			for i, l := range strings.Split(wrap(e.text, w-4), "\n") {
				mark := "  "
				if i == 0 {
					mark = sErr.Render("✘ ")
				}
				out = append(out, mark+sErr.Render(l))
			}
		case entryBlock:
			for _, l := range e.lines {
				out = append(out, "  "+trunc(l, w-2))
			}
		case entryTask:
			out = append(out, v.taskLines(m, e.taskID, w)...)
		}
	}
	return out
}

func (v *chatView) welcome(m *Model, w int) []string {
	out := []string{
		sAccentB.Render("◆ "+productName(m.info.Name)) + sFaint.Render(" "+m.info.Version+" · coding agent"),
	}
	switch p := v.project; {
	case p == nil:
		out = append(out, m.spin.View()+sMuted.Render(" looking at "+m.opt.Dir+"…"))
	case p.Root == "":
		out = append(out, sWarn.Render("  "+p.Dir+" is not a git repository.")+sMuted.Render(" Type /git-init to make it one."))
	default:
		dirty := ""
		if p.Dirty {
			dirty = sWarn.Render("  (uncommitted changes are not visible to tasks; commit them first)")
		}
		out = append(out, sMuted.Render("  "+p.Root+" ⎇ "+orDash(p.Branch))+dirty)
	}
	if v.setupLoaded {
		var todo []SetupStep
		for _, s := range v.setup {
			if !s.OK {
				todo = append(todo, s)
			}
		}
		if len(todo) > 0 {
			out = append(out, "", sWarn.Render("  Setup needed before the first task:"))
			for _, s := range v.setup {
				icon := sOK.Render("✔")
				if !s.OK {
					icon = sWarn.Render("•")
				}
				out = append(out, "    "+icon+" "+sText.Render(fit(s.Title, 48))+sFaint.Render(trunc(s.Detail, max(10, w-58))))
			}
			out = append(out, sMuted.Render("  Type ")+sKey.Render("/setup")+sMuted.Render(" to choose a local model or a cloud provider and install what is missing; you are asked first."))
		} else if len(v.entries) == 0 {
			out = append(out, "")
			for _, l := range strings.Split(wrap("Describe a change to make, e.g. \"add input validation to the signup handler, with tests\"", w-4), "\n") {
				out = append(out, sMuted.Render("  "+l))
			}
			out = append(out, sMuted.Render("  Type ")+sKey.Render("/help")+sMuted.Render(" for commands."))
		}
	}
	return out
}

// taskLines renders a task in the transcript: its live activity, then its
// outcome with what to do next.
func (v *chatView) taskLines(m *Model, id string, w int) []string {
	ct := v.tasks[id]
	if ct == nil || ct.d == nil {
		return []string{m.spin.View() + sMuted.Render(" starting task "+id+"…")}
	}
	t := ct.d.Task
	if ct.cacheN != len(ct.events) || ct.cacheW != w {
		ct.cacheN, ct.cacheW = len(ct.events), w
		ct.cache = compactActivity(ct.events, w-2)
	}
	head := sAccent.Render("◆ ") + sBold.Render("Task "+t.ID) + sFaint.Render(" · branch agent/"+t.ID)
	out := []string{head}
	for _, l := range ct.cache {
		out = append(out, "  "+l)
	}
	if m.taskRunning(id) {
		return out
	}
	// Outcome.
	out = append(out, "")
	switch t.Status {
	case task.StatusCompleted:
		ver := "checks green"
		if t.VerificationState == "task_verified" {
			ver = "verified with behavioural evidence"
		}
		out = append(out, "  "+sOK.Bold(true).Render("✔ Done")+sMuted.Render(fmt.Sprintf(" · %s · %d attempt(s) · %s tokens · %s", ver, t.AttemptCount, human(t.Budget.UsedLocalTokens), dur(t.Budget.UsedWallClockS))))
	case task.StatusBlocked:
		reason := lastBlock(t)
		if strings.Contains(reason, "SPEC_AMBIGUOUS") {
			out = append(out, "  "+sWarn.Bold(true).Render("? The request is ambiguous. Please answer:"))
			for _, l := range strings.Split(ambiguityQuestions(t), "\n") {
				out = append(out, "    "+sText.Render(l))
			}
			out = append(out, "  "+sMuted.Render("Type your answer and press enter."))
			return out
		}
		out = append(out, "  "+sWarn.Bold(true).Render("◐ Paused: ")+sWarn.Render(trunc(oneLine(strings.TrimPrefix(reason, "blocked: ")), w*2)))
		out = append(out, "  "+sMuted.Render("Send a message to continue with more guidance, or /new to start over."))
		return out
	case task.StatusActive:
		out = append(out, "  "+sMuted.Render("Stopped. Send a message to continue it, or /new to start over."))
		return out
	case task.StatusFailed, task.StatusCancelled:
		out = append(out, "  "+sErr.Render("✘ "+string(t.Status)))
		return out
	}
	if final := lastFinal(ct.events); final != "" {
		for _, l := range strings.Split(wrap(final, w-6), "\n") {
			out = append(out, "    "+sText.Render(l))
		}
	}
	if len(ct.diffs) > 0 {
		add, del, files := diffStats(ct.diffs)
		out = append(out, "  "+sMuted.Render(fmt.Sprintf("%d file(s) changed ", files))+sOK.Render(fmt.Sprintf("+%d", add))+" "+sErr.Render(fmt.Sprintf("-%d", del)))
	}
	out = append(out, "  "+sKey.Render("/diff")+sMuted.Render(" review  ·  ")+sKey.Render("/apply")+sMuted.Render(" bring into your checkout  ·  type to follow up"))
	return out
}

// compactActivity is the transcript form of the activity timeline: agent
// actions, verification and notable events, without timestamps.
func compactActivity(events []telemetry.Event, w int) []string {
	var keep []telemetry.Event
	for _, e := range events {
		switch e.Kind {
		case "task.created", "agent.session", "context.pack", "task.completed", "agent.turn":
			continue // shown in the outcome, or noise in a conversation
		}
		keep = append(keep, e)
	}
	return renderActivity(keep, w, false)
}

func lastFinal(events []telemetry.Event) string {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind == "agent.turn" {
			var d struct{ Final string }
			if jsonUnmarshal(events[i].Data, &d) == nil && d.Final != "" {
				return d.Final
			}
		}
	}
	return ""
}

func lastBlock(t *task.Task) string {
	for i := len(t.Decisions) - 1; i >= 0; i-- {
		if strings.HasPrefix(t.Decisions[i].Text, "blocked:") {
			return t.Decisions[i].Text
		}
	}
	return ""
}

func diffStats(ds []RepoDiff) (add, del, files int) {
	for _, d := range ds {
		for l := range strings.SplitSeq(d.Diff, "\n") {
			switch {
			case strings.HasPrefix(l, "diff --git"):
				files++
			case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"):
			case strings.HasPrefix(l, "+"):
				add++
			case strings.HasPrefix(l, "-"):
				del++
			}
		}
	}
	return
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
