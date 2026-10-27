package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/akynte/boundedcode/internal/direct"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/telemetry"
)

// The chat is the default view. It has two modes:
//
// Direct mode (default) is the everyday coding agent: each message goes to
// the native agent loop, which works in this checkout with file, search and
// shell tools and answers in the transcript (ADR-0011).
//
// Bounded mode (/task, or /mode bounded) turns a message into a task in an
// isolated worktree and sandbox, with verification and behavioural
// evidence:
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
	entryBlock  // pre-rendered lines (diff, help)
	entryDirect // one direct-mode turn
)

type chatEntry struct {
	kind   entryKind
	text   string
	taskID string
	lines  []string
	run    *directRun
}

// Modes of the chat: auto routes each message (the default); the others
// force one of direct.ModeChat, direct.ModeCode or direct.ModeVerify.
const (
	modeAuto = "auto"
	// aliases kept for people used to the earlier names
	modeDirect  = "direct"
	modeBounded = "bounded"
)

// directRun is one direct-mode turn: its activity, answer and outcome.
type directRun struct {
	id       int
	activity []directActivity
	text     string
	partial  string // streamed text of the model call in progress
	notes    []string
	done     bool
	err      error
	cancel   context.CancelFunc
	decision *direct.Decision
	start    time.Time
	end      time.Time
	usage    *direct.Usage
	changed  []string
	inGit    bool
	mode     string // the mode the turn runs in (chat or code)
	request  string
	todos    string
	// render cache
	cacheW, cacheN int
	cacheText      string
	cache          []string
}

type directActivity struct {
	summary string
	err     string
	denied  bool
	running bool
}

type directEventMsg struct {
	id int
	e  direct.Event
}

// routedMsg is the router's decision for a message about to be sent.
type routedMsg struct {
	text string
	d    direct.Decision
	err  error
}

type directDoneMsg struct {
	id  int
	res ChatResult
	err error
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
	{"/task", "REQUEST", "run a request as a verified task now (isolated worktree, sandbox, evidence)"},
	{"/mode", "[auto|chat|code|verify]", "auto (default) picks the mode per message; the others force one"},
	{"/diff", "", "show the current task's changes"},
	{"/apply", "[--commit]", "bring the current task's changes into your checkout (staged, or committed)"},
	{"/verify", "[--full]", "run verification on the current task"},
	{"/review", "", "ask the frontier model to review the current task (if enabled)"},
	{"/cancel", "", "cancel the current task"},
	{"/new", "", "start over: a new session, and the next task starts from your HEAD"},
	{"/open", "", "open the current task's full view (activity, diff, verification)"},
	{"/status", "", "show the session's tokens, or the current task's status and budget"},
	{"/compact", "", "summarise the conversation so far to free context"},
	{"/sessions", "", "list earlier sessions in this project"},
	{"/resume", "[ID]", "continue an earlier session (the most recent without ID)"},
	{"/approval", "[ask|edits|auto]", "what needs your consent in direct mode"},
	{"/undo", "", "restore the files changed by the last turn (repeat to go further back)"},
	{"/export", "[FILE]", "write this session's transcript as Markdown (session-ID.md here by default)"},
	{"/tools", "", "list the tools, skills, agents and MCP servers available here"},
	{"/todos", "", "show the agent's task list"},
	{"/init", "", "write an AGENTS.md for this project (the agent studies the code first)"},
	{"/model", "NAME", "use another model profile for the next runs"},
	{"/index", "", "re-index this repository"},
	{"/setup", "", "choose the model or provider and install what is missing"},
	{"/git-init", "", "make this folder a git repository"},
	{"/clear", "", "clear the transcript"},
	{"!", "COMMAND", "run a shell command here and show its output (!! runs it without telling the agent)"},
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

	// Direct mode.
	mode     string // modeAuto, or a forced direct.Mode*
	session  string // direct session id ("" = start one; "last" = the most recent)
	approval string // "" = configured
	direct   *directRun
	nextRun  int
	resumed  bool             // session names a stored session to continue
	custom   []direct.Command // project and user /commands
	customOK bool
	todos    string

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

type sessionsMsg struct {
	list []direct.SessionInfo
	err  error
}

type directInfoMsg struct {
	info DirectInfo
	err  error
}

type customCmdsMsg []direct.Command

type expandedMsg struct {
	name, prompt string
	ok           bool
	err          error
}

type undoMsg struct {
	restored []string
	err      error
}

type exportMsg struct {
	path string
	err  error
}

type shellMsg struct {
	cmd, out string
	err      error
	tell     bool
}

func newChatView() *chatView {
	ta := textarea.New()
	ta.Placeholder = "Ask about the code, describe a change, or type / for commands"
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
	v.mode = modeAuto
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

func (v *chatView) running(m *Model) bool {
	return v.directRunning() || (v.current != "" && m.taskRunning(v.current))
}

func (v *chatView) directRunning() bool { return v.direct != nil && !v.direct.done }

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
		return tea.Batch(m.loadInfo(), v.maybeIndex(m), v.loadCustom(m))
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
	case customCmdsMsg:
		v.custom, v.customOK = msg, true
		return nil
	case directInfoMsg:
		if msg.err != nil {
			v.fail("Tools: " + msg.err.Error())
			return nil
		}
		ls := []string{sSection.Render("Tools")}
		for _, t := range msg.info.Tools {
			name, desc, _ := strings.Cut(t, ": ")
			ls = append(ls, sKey.Render(fit(name, 24))+sMuted.Render(trunc(desc, 90)))
		}
		if len(msg.info.Skills) > 0 {
			ls = append(ls, "", sSection.Render("Skills (/name, or the agent loads them)"))
			for _, sk := range msg.info.Skills {
				ls = append(ls, sKey.Render(fit("/"+sk.Name, 24))+sMuted.Render(trunc(sk.Description, 90)))
			}
		}
		if len(msg.info.Agents) > 0 {
			ls = append(ls, "", sSection.Render("Sub-agents (task tool)"))
			for _, ag := range msg.info.Agents {
				ls = append(ls, sKey.Render(fit(ag.Name, 24))+sMuted.Render(trunc(ag.Description, 90)))
			}
		}
		if len(msg.info.MCP) > 0 {
			ls = append(ls, "", sSection.Render("MCP servers"))
			for _, l := range msg.info.MCP {
				ls = append(ls, sText.Render(trunc(l, 110)))
			}
		}
		if n := len(msg.info.Commands); n > 0 {
			ls = append(ls, "", sMuted.Render(fmt.Sprintf("%d command template(s): type / to see them", n)))
		}
		v.entries = append(v.entries, chatEntry{kind: entryBlock, lines: ls})
		return nil
	case expandedMsg:
		if msg.err != nil {
			v.fail("/" + msg.name + ": " + msg.err.Error())
			return nil
		}
		if !msg.ok {
			v.fail("Unknown command /" + msg.name + " (type /help).")
			return nil
		}
		return v.send(m, msg.prompt)
	case undoMsg:
		if msg.err != nil {
			v.fail("Undo: " + msg.err.Error())
			return nil
		}
		v.note("Restored " + strings.Join(msg.restored, ", ") + ".")
		return nil
	case exportMsg:
		if msg.err != nil {
			v.fail("Export: " + msg.err.Error())
			return nil
		}
		v.note("Transcript written to " + msg.path + ".")
		return nil
	case shellMsg:
		ls := []string{sKey.Render("$ " + msg.cmd)}
		out := strings.TrimRight(msg.out, "\n")
		if out != "" {
			for _, l := range strings.Split(out, "\n") {
				ls = append(ls, sText.Render(l))
			}
		}
		if msg.err != nil {
			ls = append(ls, sErr.Render("✘ "+msg.err.Error()))
		}
		v.entries = append(v.entries, chatEntry{kind: entryBlock, lines: ls})
		if msg.tell {
			text := "I ran `" + msg.cmd + "` in the project. Output:\n\n```\n" + trunc(out, 20000) + "\n```"
			if msg.err != nil {
				text += "\n(" + msg.err.Error() + ")"
			}
			return v.sendDirect(m, text, direct.ModeChat)
		}
		return nil
	case sessionsMsg:
		if msg.err != nil {
			v.fail("Sessions: " + msg.err.Error())
			return nil
		}
		if len(msg.list) == 0 {
			v.note("No earlier sessions in this project.")
			return nil
		}
		ls := []string{sSection.Render("Sessions (newest first) · /resume ID continues one")}
		for i, s := range msg.list {
			if i >= 20 {
				ls = append(ls, sMuted.Render(fmt.Sprintf("… %d more", len(msg.list)-i)))
				break
			}
			ls = append(ls, sKey.Render(fit(s.ID, 22))+sMuted.Render(fit(s.Updated.Local().Format("Jan 2 15:04"), 14))+sText.Render(trunc(s.Title, 60)))
		}
		v.entries = append(v.entries, chatEntry{kind: entryBlock, lines: ls})
		return nil
	case directEventMsg:
		r := v.direct
		if r == nil || r.id != msg.id {
			return nil
		}
		r.apply(msg.e)
		if msg.e.Kind == direct.EventTodos {
			v.todos = msg.e.Text
		}
		return nil
	case routedMsg:
		if msg.err != nil {
			v.fail("Could not route the message: " + msg.err.Error())
			return nil
		}
		return v.dispatch(m, msg.text, msg.d)
	case directDoneMsg:
		r := v.direct
		if r == nil || r.id != msg.id {
			return nil
		}
		r.done, r.end, r.err = true, time.Now(), msg.err
		var h *direct.Handoff
		if errors.As(msg.err, &h) {
			r.err = nil
			note := "Handing off to verification: " + h.Reason + "."
			if len(h.Changed) > 0 {
				note += " Earlier changes of this turn stay in your checkout: " + strings.Join(h.Changed, ", ") + "."
			}
			v.note(note)
			if msg.res.SessionID != "" {
				v.session = msg.res.SessionID
			}
			return v.sendBounded(m, r.request)
		}
		if msg.res.SessionID != "" {
			v.session = msg.res.SessionID
			v.resumed = false
		}
		u := msg.res.Usage
		r.usage = &u
		var cmds []tea.Cmd
		if msg.err == nil {
			cmds = append(cmds, m.loadInfo())
		}
		if len(v.queue) > 0 {
			next := v.queue[0]
			v.queue = v.queue[1:]
			cmds = append(cmds, func() tea.Msg { return queuedMsg(next) })
		}
		return tea.Batch(cmds...)
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
	for _, c := range v.custom {
		if name := "/" + c.Name; strings.HasPrefix(name, val) {
			v.sugg = append(v.sugg, slashCmd{name, "[args]", firstNonEmpty(c.Description, "command template")})
		}
	}
	v.scur = min(v.scur, max(0, len(v.sugg)-1))
}

func (v *chatView) resize() {
	v.input.SetHeight(max(1, min(8, v.input.LineCount())))
}

func (v *chatView) interrupt(m *Model) tea.Cmd {
	if v.directRunning() {
		v.direct.cancel()
		v.note("Interrupting… what was done so far is kept; send a message to continue.")
		return nil
	}
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

// send handles a submitted message, a slash command or a ! shell command.
func (v *chatView) send(m *Model, text string) tea.Cmd {
	if strings.HasPrefix(text, "!") {
		v.entries = append(v.entries, chatEntry{kind: entryUser, text: text})
		tell := !strings.HasPrefix(text, "!!")
		cmd := strings.TrimSpace(strings.TrimLeft(text, "!"))
		if cmd == "" {
			v.note("Usage: !COMMAND runs a shell command here and shows the agent its output; !!COMMAND keeps it to yourself.")
			return nil
		}
		return v.runShell(m, cmd, tell)
	}
	if strings.HasPrefix(text, "/") {
		v.entries = append(v.entries, chatEntry{kind: entryUser, text: text})
		return v.slash(m, text)
	}
	v.entries = append(v.entries, chatEntry{kind: entryUser, text: text})
	switch v.mode {
	case direct.ModeVerify, modeBounded:
		return v.sendBounded(m, text)
	case direct.ModeChat, direct.ModeCode, modeDirect:
		mode := v.mode
		if mode == modeDirect {
			mode = direct.ModeCode
		}
		return v.sendDirect(m, text, mode)
	}
	if v.running(m) {
		v.queue = append(v.queue, text)
		v.note("Queued: it will be sent when the current run ends (esc interrupts the run).")
		return nil
	}
	be, ctx, dir := m.be, m.ctx, v.projectDir(m)
	return func() tea.Msg {
		d, err := be.Route(ctx, dir, text)
		return routedMsg{text, d, err}
	}
}

// dispatch acts on the router's decision.
func (v *chatView) dispatch(m *Model, text string, d direct.Decision) tea.Cmd {
	label := d.Mode
	if d.Fallback != "" {
		label += " (verification unavailable)"
	}
	v.entries = append(v.entries, chatEntry{kind: entryNote, text: "◇ " + label + " · " + d.Reason})
	if d.Mode == direct.ModeVerify {
		return v.sendBounded(m, text)
	}
	return v.sendDirectRouted(m, text, d)
}

// sendDirect sends a message to the native agent loop in this checkout,
// in mode direct.ModeChat or direct.ModeCode.
func (v *chatView) sendDirect(m *Model, text, mode string) tea.Cmd {
	return v.sendDirectRouted(m, text, direct.Decision{Mode: mode})
}

// sendDirectRouted sends a message with the router's decision.
func (v *chatView) sendDirectRouted(m *Model, text string, d direct.Decision) tea.Cmd {
	mode := d.Mode
	if ok, why := v.readyDirect(); !ok {
		v.fail("Can't send: " + why)
		return nil
	}
	if v.running(m) {
		v.queue = append(v.queue, text)
		v.note("Queued: it will be sent when the current run ends (esc interrupts the run).")
		return nil
	}
	v.nextRun++
	ctx, cancel := context.WithCancel(m.ctx)
	dec := d
	r := &directRun{id: v.nextRun, cancel: cancel, start: time.Now(), mode: mode, request: text, decision: &dec}
	v.direct = r
	v.entries = append(v.entries, chatEntry{kind: entryDirect, run: r})
	v.scroll.follow = true
	be, send := m.be, m.send
	dir, session, model, approval := v.projectDir(m), v.session, v.model, v.approval
	return tea.Batch(func() tea.Msg {
		defer cancel()
		req := ChatRequest{Dir: dir, SessionID: session, Message: text, Model: model, Approval: approval, Mode: mode, Decision: r.decision}
		res, err := be.Chat(ctx, req, func(e direct.Event) { send(directEventMsg{r.id, e}) })
		return directDoneMsg{r.id, res, err}
	}, m.spinTick())
}

// compact summarises the direct session on request.
func (v *chatView) compact(m *Model) tea.Cmd {
	if v.session == "" || v.resumed {
		v.note("Nothing to summarise yet in this session.")
		return nil
	}
	if v.running(m) {
		v.fail("Wait for the current run to end.")
		return nil
	}
	v.nextRun++
	ctx, cancel := context.WithCancel(m.ctx)
	r := &directRun{id: v.nextRun, cancel: cancel, start: time.Now()}
	v.direct = r
	v.entries = append(v.entries, chatEntry{kind: entryDirect, run: r})
	req := ChatRequest{Dir: v.projectDir(m), SessionID: v.session, Compact: true}
	be, send := m.be, m.send
	return func() tea.Msg {
		defer cancel()
		res, err := be.Chat(ctx, req, func(e direct.Event) { send(directEventMsg{r.id, e}) })
		return directDoneMsg{r.id, res, err}
	}
}

// projectDir is where direct mode works: the directory the interface was
// started in (the backend confines tools to its git root).
func (v *chatView) projectDir(m *Model) string { return m.opt.Dir }

// readyDirect reports whether direct mode can run: the configuration and
// the model (local server and weights, or a cloud provider). The sandbox
// and repository tools are only needed for bounded mode.
func (v *chatView) readyDirect() (bool, string) {
	if v.project == nil {
		return false, "still looking at this folder…"
	}
	for _, s := range v.setup {
		if !s.OK && (s.Name == "config" || s.Name == "inference" || s.Name == "model") {
			return false, "setup is not complete (" + s.Title + ": " + s.Detail + "); type /setup"
		}
	}
	return true, ""
}

// sendBounded turns a message into a task, or steers the current one.
func (v *chatView) sendBounded(m *Model, text string) tea.Cmd {
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
			sText.Render("Each message is routed automatically to one of three modes:"),
			sText.Render("  chat    answers from the code without changing files;"),
			sText.Render("  code    makes the change directly in this checkout (edits apply; shell commands"),
			sText.Render("          other than builds, tests and read-only ones ask first; /approval changes that);"),
			sText.Render("  verify  runs the request as a task in an isolated worktree and sandbox, backed by a"),
			sText.Render("          test that fails without the change; your checkout is untouched until /apply."),
			sText.Render("A chat turn that needs an edit becomes code; a code turn that reaches authentication,"),
			sText.Render("payments, migrations, contracts or infrastructure hands off to verify before editing."),
			sText.Render("While a run is going you can keep typing (messages are queued); esc interrupts."),
			"", sSection.Render("Commands"))
		for _, c := range slashCmds {
			ls = append(ls, sKey.Render(fit(c.name+" "+c.args, 22))+sMuted.Render(c.help))
		}
		ls = append(ls, "", sMuted.Render("Keys: enter sends · alt+enter or ctrl+j new line · ↑/↓ history · pgup/pgdn scroll · esc leaves the input"),
			sMuted.Render("      ctrl+k all commands · with the input left: ← views, 1–9 switch view"))
		v.entries = append(v.entries, chatEntry{kind: entryBlock, lines: ls})
	case "/clear":
		v.entries = nil
	case "/new":
		v.current, v.session, v.resumed = "", "", false
		v.note("Starting fresh: a new session; the next task starts from your current HEAD.")
	case "/task":
		if len(rest) == 0 {
			v.note("Usage: /task REQUEST — runs the request in bounded mode. /mode bounded makes every message a task.")
			return nil
		}
		return v.sendBounded(m, strings.TrimSpace(strings.TrimPrefix(text, name)))
	case "/direct", "/code":
		if len(rest) == 0 {
			v.note("Usage: " + name + " MESSAGE — makes the change directly in your checkout, whatever the router would pick.")
			return nil
		}
		return v.sendDirect(m, strings.TrimSpace(strings.TrimPrefix(text, name)), direct.ModeCode)
	case "/chat":
		if len(rest) == 0 {
			v.note("Usage: /chat MESSAGE — answers without changing files.")
			return nil
		}
		return v.sendDirect(m, strings.TrimSpace(strings.TrimPrefix(text, name)), direct.ModeChat)
	case "/mode":
		if len(rest) == 0 {
			v.note("Mode: " + v.mode + ". auto picks chat, code or verify per message from its intent and risk; /mode chat|code|verify forces one.")
			return nil
		}
		switch rest[0] {
		case modeAuto:
			v.mode = modeAuto
			v.note("Automatic: each message is routed to chat, code or verify.")
		case direct.ModeChat:
			v.mode = direct.ModeChat
			v.note("Chat mode: messages are answered without changing files (an edit the answer needs switches that turn to code).")
		case direct.ModeCode, modeDirect:
			v.mode = direct.ModeCode
			v.note("Code mode: changes are made directly in " + v.projectDir(m) + " (risky paths still hand off to verification).")
		case direct.ModeVerify, modeBounded:
			v.mode = direct.ModeVerify
			v.note("Verify mode: each message becomes a task in an isolated worktree and sandbox, with verification.")
		default:
			v.fail("Mode must be auto, chat, code or verify.")
		}
	case "/approval":
		if len(rest) == 0 {
			cur := v.approval
			if cur == "" {
				cur = "configured default"
			}
			v.note("Approval: " + cur + ". ask: every change and command; edits: file edits and known build/test commands run freely; auto: nothing asks (policy still refuses pushes and infrastructure changes).")
			return nil
		}
		switch rest[0] {
		case direct.ApprovalAsk, direct.ApprovalEdits, direct.ApprovalAuto:
			v.approval = rest[0]
			v.note("Approval mode for the next messages: " + v.approval + ".")
		default:
			v.fail("Approval must be ask, edits or auto.")
		}
	case "/compact":
		return v.compact(m)
	case "/sessions":
		be, ctx, dir := m.be, m.ctx, v.projectDir(m)
		return func() tea.Msg {
			list, err := be.Sessions(ctx, dir)
			return sessionsMsg{list, err}
		}
	case "/resume":
		if v.running(m) {
			v.fail("Wait for the current run to end.")
			return nil
		}
		id := "last"
		if len(rest) > 0 {
			id = rest[0]
		}
		v.session, v.resumed = id, true
		if id == "last" {
			v.note("The next message continues the most recent session of this project.")
		} else {
			v.note("The next message continues session " + id + ".")
		}
	case "/quit", "/exit":
		return m.quit()
	case "/open":
		if needTask() {
			return m.openTask(t.ID)
		}
	case "/status":
		if v.mode != direct.ModeVerify && t == nil {
			if v.direct == nil || v.direct.usage == nil {
				v.note("No session yet. Mode: " + v.mode + ".")
				return nil
			}
			u := v.direct.usage
			v.note(fmt.Sprintf("Session %s · %s tokens processed · %s generated · %s cached · mode %s", v.session, human(u.Processed), human(u.Generated), human(u.Cached), v.mode))
			return nil
		}
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
	case "/undo":
		if v.session == "" || v.resumed {
			v.fail("Nothing to undo yet in this session.")
			return nil
		}
		if v.running(m) {
			v.fail("Wait for the current run to end.")
			return nil
		}
		be, ctx, dir, sess := m.be, m.ctx, v.projectDir(m), v.session
		return func() tea.Msg {
			restored, err := be.Undo(ctx, dir, sess)
			return undoMsg{restored, err}
		}
	case "/export":
		if v.session == "" {
			v.fail("Nothing to export yet in this session.")
			return nil
		}
		be, ctx, dir, sess := m.be, m.ctx, v.projectDir(m), v.session
		out := strings.Join(rest, " ")
		return func() tea.Msg {
			path, err := be.Export(ctx, dir, sess, out)
			return exportMsg{path, err}
		}
	case "/tools", "/skills", "/mcp", "/agents":
		be, ctx, dir := m.be, m.ctx, v.projectDir(m)
		return func() tea.Msg {
			info, err := be.DirectInfo(ctx, dir)
			return directInfoMsg{info, err}
		}
	case "/todos":
		if v.todos == "" {
			v.note("No task list yet; the agent keeps one for multi-step work.")
			return nil
		}
		ls := []string{sSection.Render("Tasks")}
		for _, l := range strings.Split(v.todos, "\n") {
			ls = append(ls, sText.Render(l))
		}
		v.entries = append(v.entries, chatEntry{kind: entryBlock, lines: ls})
	case "/init":
		return v.sendDirect(m, "Study this project (layout, languages, build and test commands, conventions, anything an agent must know) and write an AGENTS.md at the project root for coding agents: concise, factual, under 150 lines. If one exists, improve it instead of replacing it. Show what you wrote.", direct.ModeCode)
	default:
		// A project or user command template: /name args.
		be, ctx, dir := m.be, m.ctx, v.projectDir(m)
		cname := strings.TrimPrefix(name, "/")
		args := strings.TrimSpace(strings.TrimPrefix(text, name))
		return func() tea.Msg {
			prompt, ok, err := be.ExpandCommand(ctx, dir, cname, args)
			return expandedMsg{cname, prompt, ok, err}
		}
	}
	return nil
}

// loadCustom lists the project's and user's command templates for the
// slash menu.
func (v *chatView) loadCustom(m *Model) tea.Cmd {
	be, ctx, dir := m.be, m.ctx, v.projectDir(m)
	return func() tea.Msg {
		info, err := be.DirectInfo(ctx, dir)
		if err != nil {
			return customCmdsMsg(nil)
		}
		return customCmdsMsg(info.Commands)
	}
}

// runShell runs a ! command in the project and shows its output; with
// tell, the agent is told about it in chat mode.
func (v *chatView) runShell(m *Model, cmd string, tell bool) tea.Cmd {
	dir, ctx := v.projectDir(m), m.ctx
	return func() tea.Msg {
		argv := []string{"bash", "-c", cmd}
		if _, err := exec.LookPath("bash"); err != nil {
			argv = []string{"sh", "-c", cmd}
		}
		c := exec.CommandContext(ctx, argv[0], argv[1:]...)
		c.Dir = dir
		c.Env = append(os.Environ(), "NO_COLOR=1", "TERM=dumb", "PAGER=cat", "GIT_PAGER=cat")
		out, err := c.CombinedOutput()
		return shellMsg{cmd: cmd, out: string(out), err: err, tell: tell}
	}
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
	case v.directRunning():
		r := v.direct
		info := ""
		for i := len(r.activity) - 1; i >= 0; i-- {
			if r.activity[i].running {
				info = " · " + trunc(r.activity[i].summary, 40)
				break
			}
		}
		left = m.spin.View() + sAccent.Render(" Working") + sMuted.Render(fmt.Sprintf(" %s%s", time.Since(r.start).Round(time.Second), info)) + sFaint.Render("  esc to interrupt")
		if n := len(v.queue); n > 0 {
			left += sFaint.Render(fmt.Sprintf(" · %d queued", n))
		}
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
	if v.mode != modeAuto {
		right = sWarn.Render(v.mode+" ") + right
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
		case entryDirect:
			out = append(out, e.run.lines(m, w)...)
		}
	}
	return out
}

// apply folds an event into the run.
func (r *directRun) apply(e direct.Event) {
	switch e.Kind {
	case direct.EventToolStart:
		r.activity = append(r.activity, directActivity{summary: e.Summary, running: true})
	case direct.EventToolEnd:
		for i := len(r.activity) - 1; i >= 0; i-- {
			if r.activity[i].running {
				r.activity[i].running = false
				r.activity[i].err = e.Err
				break
			}
		}
	case direct.EventDenied:
		r.activity = append(r.activity, directActivity{summary: e.Summary, err: e.Err, denied: true})
	case direct.EventTextDelta:
		r.partial += e.Text
	case direct.EventText:
		r.partial = ""
		if r.text != "" {
			r.text += "\n\n"
		}
		r.text += e.Text
	case direct.EventCompact, direct.EventNote:
		r.notes = append(r.notes, e.Text)
	case direct.EventSandbox:
		if e.Args["level"] != nil || e.Args["allowed"] == false {
			r.notes = append(r.notes, e.Summary)
		}
	case direct.EventMode:
		r.mode = e.Text
		r.notes = append(r.notes, "◇ "+e.Text+" · "+e.Summary)
	case direct.EventTodos:
		r.todos = e.Text
	case direct.EventQuestion:
		r.activity = append(r.activity, directActivity{summary: "? " + oneLine(e.Text) + " → " + oneLine(e.Output)})
	case direct.EventSubagent:
		if e.Text == string(direct.EventToolStart) {
			r.activity = append(r.activity, directActivity{summary: "↳ " + e.Tool + ": " + e.Summary})
		}
	case direct.EventDone:
		if e.Usage != nil {
			u := *e.Usage
			r.usage = &u
		}
		r.changed, r.inGit = e.Changed, e.Changed != nil
	}
	r.cacheN = -1 // invalidate
}

// lines renders the run: activity, answer, outcome.
func (r *directRun) lines(m *Model, w int) []string {
	n := len(r.activity)*2 + len(r.notes)
	if r.done {
		n++
	}
	if r.cacheW == w && r.cacheN == n && r.cacheText == r.text && r.partial == "" && !r.hasRunning() {
		return r.cache
	}
	var out []string
	for _, a := range r.activity {
		switch {
		case a.denied:
			out = append(out, "  "+sErr.Render("✘ ")+sMuted.Render(trunc(a.summary, w-20))+sErr.Render(" · "+a.err))
		case a.running:
			out = append(out, "  "+m.spin.View()+" "+sMuted.Render(trunc(a.summary, w-6)))
		case a.err != "":
			out = append(out, "  "+sWarn.Render("⚙ ")+sMuted.Render(trunc(a.summary, w-20))+sWarn.Render(" · "+trunc(a.err, 60)))
		default:
			out = append(out, "  "+sFaint.Render("⚙ ")+sMuted.Render(trunc(a.summary, w-6)))
		}
	}
	for _, n := range r.notes {
		out = append(out, "  "+sFaint.Render("· "+trunc(n, w-6)))
	}
	if r.todos != "" {
		for _, l := range strings.Split(r.todos, "\n") {
			mark := sMuted
			if strings.HasPrefix(l, "[>]") {
				mark = sAccent
			} else if strings.HasPrefix(l, "[x]") {
				mark = sOK
			}
			out = append(out, "  "+mark.Render(trunc(l, w-4)))
		}
	}
	text := r.text
	if r.partial != "" {
		if text != "" {
			text += "\n\n"
		}
		text += r.partial
	}
	if text != "" {
		if len(out) > 0 {
			out = append(out, "")
		}
		for i, l := range strings.Split(wrap(text, w-4), "\n") {
			mark := "  "
			if i == 0 {
				mark = sAccent.Render("◆ ")
			}
			out = append(out, mark+sText.Render(l))
		}
	}
	switch {
	case !r.done:
		if len(r.activity) == 0 && text == "" {
			out = append(out, "  "+m.spin.View()+sMuted.Render(" thinking…"))
		}
	case r.err != nil:
		out = append(out, "  "+sErr.Render("✘ "+trunc(r.err.Error(), w*2)))
	default:
		foot := fmt.Sprintf("· %s", r.end.Sub(r.start).Round(time.Second))
		if r.usage != nil {
			foot += fmt.Sprintf(" · %s tokens", human(r.usage.Processed))
		}
		switch {
		case len(r.changed) > 0:
			foot += fmt.Sprintf(" · %d file(s) changed: %s", len(r.changed), trunc(strings.Join(r.changed, ", "), max(20, w-40)))
		case r.inGit:
			foot += " · no files changed"
		}
		out = append(out, "  "+sFaint.Render(foot))
	}
	r.cacheW, r.cacheN, r.cacheText, r.cache = w, n, r.text, out
	return out
}

func (r *directRun) hasRunning() bool {
	for _, a := range r.activity {
		if a.running {
			return true
		}
	}
	return false
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
			for _, l := range strings.Split(wrap("Ask about the code or describe a change, e.g. \"add input validation to the signup handler, with tests\". The agent works in this checkout.", w-4), "\n") {
				out = append(out, sMuted.Render("  "+l))
			}
			out = append(out, sMuted.Render("  Questions are answered, small changes are made here, and sensitive ones run as verified tasks, automatically. ")+sKey.Render("/help")+sMuted.Render(" explains."))
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

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
