package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/akynte/boundedcode/internal/task"
)

var statusFilters = []string{"all", "active", "blocked", "completed", "failed", "cancelled"}

// tasksView lists recent tasks.
type tasksView struct {
	tasks   []*task.Task
	err     error
	loaded  bool
	busy    bool
	cur     cursor
	filter  int
	search  textinput.Model
	editing bool
	h       int
}

type tasksMsg struct {
	tasks []*task.Task
	err   error
}

func (v *tasksView) name() string          { return "Tasks" }
func (v *tasksView) capturing() bool       { return v.editing }
func (v *tasksView) loading() bool         { return v.busy }
func (v *tasksView) init(m *Model) tea.Cmd { return v.load(m) }
func (v *tasksView) refresh(m *Model) tea.Cmd {
	if v.busy {
		return nil
	}
	return v.load(m)
}

func (v *tasksView) load(m *Model) tea.Cmd {
	v.busy = true
	be, ctx := m.be, m.ctx
	return func() tea.Msg {
		ts, err := be.Tasks(ctx, 500)
		return tasksMsg{ts, err}
	}
}

func (v *tasksView) visible() []*task.Task {
	q := strings.ToLower(strings.TrimSpace(v.search.Value()))
	var out []*task.Task
	for _, t := range v.tasks {
		if v.filter > 0 && string(t.Status) != statusFilters[v.filter] {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(t.ID+" "+t.OriginalRequest+" "+string(t.Phase)), q) {
			continue
		}
		out = append(out, t)
	}
	return out
}

func (v *tasksView) selected() *task.Task {
	vis := v.visible()
	if v.cur.pos < len(vis) {
		return vis[v.cur.pos]
	}
	return nil
}

func (v *tasksView) update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tasksMsg:
		v.busy, v.loaded = false, true
		v.tasks, v.err = msg.tasks, msg.err
		return nil
	case tea.KeyMsg:
		if v.editing {
			switch msg.String() {
			case "esc":
				v.editing = false
				v.search.SetValue("")
				v.search.Blur()
				return nil
			case "enter":
				v.editing = false
				v.search.Blur()
				return nil
			}
			var cmd tea.Cmd
			v.search, cmd = v.search.Update(msg)
			v.cur.pos = 0
			return cmd
		}
		vis := v.visible()
		if v.cur.handle(msg, len(vis), v.h) {
			return nil
		}
		t := v.selected()
		switch msg.String() {
		case "enter", "o":
			if t != nil {
				return m.openTask(t.ID)
			}
		case "n":
			return m.newTaskForm()
		case "/":
			v.editing = true
			if v.search.Prompt == "" {
				v.search = textinput.New()
				v.search.Prompt = sAccent.Render("/ ")
				v.search.Placeholder = "search id or request"
				v.search.PlaceholderStyle = sFaint
			}
			return v.search.Focus()
		case "f":
			v.filter = (v.filter + 1) % len(statusFilters)
			v.cur.pos = 0
		case "F":
			v.filter = (v.filter + len(statusFilters) - 1) % len(statusFilters)
			v.cur.pos = 0
		case "esc":
			v.search.SetValue("")
			v.filter = 0
		case "R":
			return v.load(m)
		}
		if t != nil {
			return m.taskAction(msg.String(), t)
		}
	case tea.MouseMsg:
		v.cur.handle(msg, len(v.visible()), v.h)
	}
	return nil
}

// taskAction runs the single-key task actions shared by the list and the
// detail view.
func (m *Model) taskAction(key string, t *task.Task) tea.Cmd {
	switch key {
	case "r":
		return m.runTaskForm(t, false)
	case "e":
		return m.runTaskForm(t, true)
	case "c":
		if t.Status == task.StatusCancelled || t.Status == task.StatusCompleted {
			return m.toast("task is already "+string(t.Status), toastInfo)
		}
		return m.ask("Cancel task "+t.ID+"?", "The task stops (a run in progress notices within ~15 s). Worktrees and the agent/"+t.ID+" branch are kept.", true,
			func() tea.Cmd { return m.startJob("cancel "+t.ID, t.ID, []string{"task", "cancel", t.ID}, nil) })
	case "x":
		return m.cleanupForm(t)
	case "v":
		return m.verifyTask(t, false)
	case "V":
		return m.verifyTask(t, true)
	case "a":
		return m.answerForm(t.ID)
	}
	return nil
}

func (m *Model) verifyTask(t *task.Task, full bool) tea.Cmd {
	args := []string{"verify", t.ID}
	title := "verify " + t.ID
	if full {
		args = append(args, "--full")
		title = "full verify " + t.ID
	}
	start := func() tea.Cmd {
		return m.startJob(title, t.ID, args, func(m *Model, err error) tea.Cmd {
			if err != nil {
				return m.toast(title+": "+firstLine(err.Error()), toastErr)
			}
			return m.toast(title+": passed", toastOK)
		})
	}
	if m.info.SandboxKind == "none" {
		args = append(args, "--unsafe-no-sandbox")
		return m.ask("Verify on the host?", "sandbox.kind is none, so verification commands (tests, linters) would run directly on this machine instead of in a container.", true, start)
	}
	return start()
}

func (m *Model) cleanupForm(t *task.Task) tea.Cmd {
	switch t.Status {
	case task.StatusCompleted, task.StatusCancelled, task.StatusFailed:
	default:
		return m.toast(fmt.Sprintf("task %s is %s; cancel it first", t.ID, t.Status), toastErr)
	}
	return m.openForm(newForm("Clean up "+t.ID,
		"Removes the task's worktrees, code-graph projects and build caches. The ledger, audit log and frontier packets are kept.",
		"Clean up", []*field{toggleField("branch", "Also delete the agent/"+t.ID+" branch (discards its commits)", false)},
		func(v formValues) (tea.Cmd, error) {
			args := []string{"task", "cleanup", t.ID}
			if v.on("branch") {
				args = append(args, "--delete-branch")
			}
			return m.startJob("cleanup "+t.ID, t.ID, args, nil), nil
		}))
}

func (m *Model) answerForm(taskID string) tea.Cmd {
	fields := []*field{}
	if taskID == "" {
		fields = append(fields, textField("task", "Task id", "", "t-…").mustFill())
	}
	fields = append(fields, textField("file", "Answer file", "", "/path/to/answer.md").mustFill().
		withHelp("A frontier answer obtained manually (manual provider), stored for the task's pending escalation."))
	return m.openForm(newForm("Store a frontier answer", "", "Store", fields, func(v formValues) (tea.Cmd, error) {
		id := taskID
		if id == "" {
			id = v.str("task")
		}
		return m.startJob("answer "+id, id, []string{"frontier", "answer", id, v.str("file")}, func(m *Model, err error) tea.Cmd {
			if err != nil {
				return m.errToast(err)
			}
			return m.toast("answer stored; resume the task to use it", toastOK)
		}), nil
	}))
}

// newTaskForm creates a task, optionally running it.
func (m *Model) newTaskForm() tea.Cmd {
	be, ctx := m.be, m.ctx
	return func() tea.Msg {
		ws, err := be.Workspaces(ctx)
		models, _ := be.Models(ctx)
		return newTaskDataMsg{ws, models, err}
	}
}

type newTaskDataMsg struct {
	ws     []Workspace
	models []ModelRow
	err    error
}

func (m *Model) showNewTaskForm(d newTaskDataMsg) tea.Cmd {
	if d.err != nil {
		return m.errToast(d.err)
	}
	if len(d.ws) == 0 {
		return m.toast("Create a workspace and add repositories first (Workspaces, 2)", toastErr)
	}
	var names []string
	cur := ""
	for _, w := range d.ws {
		names = append(names, w.Name)
		if w.Current {
			cur = w.Name
		}
	}
	if cur == "" {
		cur = names[0]
	}
	repoNames := func(ws string) []string {
		for _, w := range d.ws {
			if w.Name == ws {
				var rs []string
				for _, r := range w.Repos {
					if r.Enabled {
						rs = append(rs, r.Name)
					}
				}
				return rs
			}
		}
		return nil
	}
	models := []string{"default"}
	for _, p := range d.models {
		models = append(models, p.Name)
	}
	wsField := choiceField("ws", "Workspace", names, cur)
	repos := multiField("repos", "Repositories (none selected = all enabled)", repoNames(cur), false)
	f := newForm("New task", "Describe the change. The task runs in isolated worktrees on branch agent/<task-id>; nothing touches your checkouts.", "Create",
		[]*field{
			wsField,
			areaField("request", "Request", "", "e.g. Rename the order.created event field total to amount_cents in all producers and consumers", 4).mustFill(),
			repos,
			areaField("criteria", "Acceptance criteria (one per line, optional)", "", "", 3),
			choiceField("model", "Model profile", models, "default"),
			toggleField("run", "Run immediately", true),
		},
		func(v formValues) (tea.Cmd, error) {
			req := CreateTaskRequest{Workspace: v.str("ws"), Request: v.str("request"), Repos: v.multi("repos"), Criteria: v.lines("criteria")}
			if mdl := v.str("model"); mdl != "default" {
				req.Model = mdl
			}
			run := v.on("run")
			be, ctx := m.be, m.ctx
			return func() tea.Msg {
				id, err := be.CreateTask(ctx, req)
				return taskCreatedMsg{id, req.Model, run, err}
			}, nil
		})
	lastWS := cur
	f.onChange = func(f *form) {
		if ws := wsField.selected(); ws != lastWS {
			lastWS = ws
			*repos = *multiField("repos", repos.label, repoNames(ws), false)
		}
	}
	return m.openForm(f)
}

type taskCreatedMsg struct {
	id, model string
	run       bool
	err       error
}

func (m *Model) taskCreated(msg taskCreatedMsg) tea.Cmd {
	if msg.err != nil {
		return m.toast("create task: "+firstLine(msg.err.Error()), toastErr)
	}
	cmds := []tea.Cmd{m.toast("created task "+msg.id, toastOK), m.openTask(msg.id)}
	if msg.run {
		args := []string{"task", "run", msg.id}
		if msg.model != "" {
			args = append(args, "--model", msg.model)
		}
		cmds = append(cmds, m.startTaskRun(msg.id, "run "+msg.id, args))
		m.detail.tab = tabActivity
	}
	return tea.Batch(cmds...)
}

// runTaskForm asks for run options; review requests a frontier review (Z4).
func (m *Model) runTaskForm(t *task.Task, review bool) tea.Cmd {
	if m.taskRunning(t.ID) {
		return m.toast("task "+t.ID+" is already running here", toastInfo)
	}
	if review && !m.info.FrontierEnabled {
		return m.toast("frontier is disabled (frontier.enabled in config.yaml)", toastErr)
	}
	switch t.Status {
	case task.StatusCompleted, task.StatusCancelled, task.StatusFailed:
		return m.toast("task is "+string(t.Status)+"; it cannot be resumed", toastInfo)
	}
	verb, title := "Run", "Run "+t.ID
	if t.AttemptCount > 0 || t.Status == task.StatusBlocked {
		verb, title = "Resume", "Resume "+t.ID
	}
	if review {
		verb, title = "Request review", "Frontier review of "+t.ID
	}
	intro := oneLine(t.OriginalRequest)
	if len(intro) > 220 {
		intro = intro[:220] + "…"
	}
	fields := []*field{}
	clarify := t.Status == task.StatusBlocked && lastDecisionHas(t, "SPEC_AMBIGUOUS")
	if clarify {
		fields = append(fields, areaField("clarify", "Clarification", "", "Answer the questions the task was blocked on", 3).
			withHelp(ambiguityQuestions(t)))
	}
	models := []string{"task default"}
	m.cachedModels(func(rows []ModelRow) {
		for _, r := range rows {
			models = append(models, r.Name)
		}
	})
	fields = append(fields,
		choiceField("model", "Model profile", models, "task default"),
		choiceField("serena", "Serena LSP navigation", []string{"config", "on", "off"}, "config"),
		toggleField("approve", "Pre-approve frontier escalations (otherwise you are asked)", false),
		toggleField("condense", "Condense context before every retry (continuity testing)", false),
	)
	if m.info.SandboxKind == "none" {
		fields = append(fields, toggleField("unsafe", "Run without a container sandbox (sandbox.kind is none)", false).
			withHelp("Development only: agent tools then run directly on this machine."))
	}
	return m.openForm(newForm(title, intro, verb, fields, func(v formValues) (tea.Cmd, error) {
		args := []string{"task", "run", t.ID}
		if review {
			args = []string{"frontier", "review", t.ID}
		}
		if clarify {
			if c := v.str("clarify"); c != "" {
				args = append(args, "--clarify", c)
			}
		}
		if mdl := v.str("model"); mdl != "task default" {
			args = append(args, "--model", mdl)
		}
		if s := v.str("serena"); s != "config" {
			args = append(args, "--serena", s)
		}
		if v.on("approve") {
			args = append(args, "--approve-frontier")
		}
		if v.on("condense") {
			args = append(args, "--condense-each-retry")
		}
		if _, ok := v["unsafe"]; ok && v.on("unsafe") {
			args = append(args, "--unsafe-no-sandbox")
		}
		m.detail.tab = tabActivity
		return tea.Batch(m.openTask(t.ID), m.startTaskRun(t.ID, strings.ToLower(verb)+" "+t.ID, args)), nil
	}))
}

// startTaskRun starts a run job and reports its outcome from the ledger.
func (m *Model) startTaskRun(id, title string, args []string) tea.Cmd {
	return m.startJob(title, id, args, func(m *Model, err error) tea.Cmd {
		if err != nil {
			return m.toast(title+": "+firstLine(err.Error()), toastErr)
		}
		be, ctx := m.be, m.ctx
		return func() tea.Msg {
			d, err := be.Task(ctx, id)
			if err != nil {
				return nil
			}
			return runEndedMsg{d.Task}
		}
	})
}

type runEndedMsg struct{ t *task.Task }

func (m *Model) runEnded(t *task.Task) tea.Cmd {
	text := fmt.Sprintf("%s: %s · %s · verification %s", t.ID, t.Status, t.Phase, orDash(t.VerificationState))
	switch t.Status {
	case task.StatusCompleted:
		return m.toast(text, toastOK)
	case task.StatusFailed, task.StatusBlocked:
		return m.toast(text, toastErr)
	}
	return m.toast(text, toastInfo)
}

func lastDecisionHas(t *task.Task, s string) bool {
	for i := len(t.Decisions) - 1; i >= 0; i-- {
		if strings.HasPrefix(t.Decisions[i].Text, "blocked:") {
			return strings.Contains(t.Decisions[i].Text, s)
		}
	}
	return false
}

func ambiguityQuestions(t *task.Task) string {
	for i := len(t.Decisions) - 1; i >= 0; i-- {
		if d := t.Decisions[i].Text; strings.Contains(d, "SPEC_AMBIGUOUS") {
			if _, q, ok := strings.Cut(d, "\n"); ok {
				return strings.TrimSpace(q)
			}
			return d
		}
	}
	return ""
}

// cachedModels calls f with the model profiles, loading them synchronously
// on first use (profiles are local files).
func (m *Model) cachedModels(f func([]ModelRow)) {
	if m.models == nil {
		ctx, cancel := context.WithCancel(m.ctx)
		defer cancel()
		m.models, _ = m.be.Models(ctx)
		if m.models == nil {
			m.models = []ModelRow{}
		}
	}
	f(m.models)
}

func (v *tasksView) view(m *Model, w, h int) string {
	var b strings.Builder
	counts := map[string]int{}
	for _, t := range v.tasks {
		counts[string(t.Status)]++
	}
	title := sTitle.Render("Tasks")
	var chips []string
	for i, f := range statusFilters {
		n := len(v.tasks)
		if i > 0 {
			n = counts[f]
		}
		label := fmt.Sprintf("%s %d", f, n)
		if i == v.filter {
			chips = append(chips, badge(label, cAccent))
		} else {
			chips = append(chips, sMuted.Render(" "+label+" "))
		}
	}
	b.WriteString(" " + title + "   " + strings.Join(chips, " ") + "\n")
	if v.editing || v.search.Value() != "" {
		v.search.Width = w - 6
		b.WriteString(" " + v.search.View() + "\n")
	} else {
		b.WriteString("\n")
	}
	vis := v.visible()
	widths := []int{1, 16, 9, 12, 13, 5, 6, 9, 0}
	head := row(w-2, widths, "", "ID", "STATUS", "PHASE", "VERIFICATION", "TRIES", "TOKENS", "UPDATED", "REQUEST")
	b.WriteString(" " + sFaint.Render(head) + "\n")
	v.h = max(1, h-4)
	switch {
	case !v.loaded:
		b.WriteString("\n " + m.spin.View() + sMuted.Render(" loading tasks…"))
		return b.String()
	case v.err != nil:
		b.WriteString("\n " + sErr.Render("✘ "+v.err.Error()))
		return b.String()
	case len(v.tasks) == 0:
		b.WriteString(emptyState(w, "No tasks yet",
			"Press "+sKey.Render("n")+" to describe a change. BoundedCode creates isolated worktrees,",
			"runs the local agent in a sandbox and verifies the result deterministically."))
		return b.String()
	case len(vis) == 0:
		b.WriteString("\n " + sMuted.Render("No tasks match the filter ") + sFaint.Render("(esc clears)"))
		return b.String()
	}
	from, to := v.cur.window(len(vis), v.h)
	for i := from; i < to; i++ {
		t := vis[i]
		status := string(t.Status)
		if m.taskRunning(t.ID) {
			status = "running"
		}
		icon := statusIcon(string(t.Status))
		if m.taskRunning(t.ID) {
			icon = m.spin.View()
		}
		vs := orDash(t.VerificationState)
		if vs == "none" {
			vs = "–"
		}
		cells := []string{
			icon,
			sText.Render(t.ID),
			lipgloss.NewStyle().Foreground(statusColor(status)).Render(status),
			sMuted.Render(string(t.Phase)),
			lipgloss.NewStyle().Foreground(statusColor(t.VerificationState)).Render(vs),
			sMuted.Render(fmt.Sprintf("%d/%d", t.AttemptCount, t.Budget.MaxAttempts)),
			sMuted.Render(human(t.Budget.UsedLocalTokens)),
			sFaint.Render(ago(t.UpdatedAt)),
			sText.Render(oneLine(t.OriginalRequest)),
		}
		line := row(w-2, widths, cells...)
		if i == v.cur.pos {
			line = sSel.Render(fit(line, w-2))
			b.WriteString(sAccent.Render("▍") + line + "\n")
		} else {
			b.WriteString(" " + line + "\n")
		}
	}
	if len(vis) > v.h {
		b.WriteString(sFaint.Render(fmt.Sprintf(" %d–%d of %d", from+1, to, len(vis))))
	}
	return b.String()
}

func emptyState(w int, title string, lines ...string) string {
	body := sTitle.Render(title) + "\n\n" + sMuted.Render(strings.Join(lines, "\n"))
	return "\n\n" + lipgloss.PlaceHorizontal(w, lipgloss.Center, lipgloss.NewStyle().Align(lipgloss.Center).Render(body))
}

func (v *tasksView) hints(m *Model) []hint {
	if v.editing {
		return []hint{{"enter", "apply"}, {"esc", "clear"}}
	}
	return []hint{{"enter", "open"}, {"n", "new"}, {"r", "run"}, {"v", "verify"}, {"c", "cancel"}, {"f", "filter"}, {"/", "search"}}
}

func (v *tasksView) commands(m *Model) []command {
	cs := []command{
		{title: "Refresh tasks", group: "Tasks", key: "R", run: func(m *Model) tea.Cmd { return v.load(m) }},
		{title: "Filter by status", group: "Tasks", key: "f", run: func(m *Model) tea.Cmd { v.filter = (v.filter + 1) % len(statusFilters); return nil }},
		{title: "Store a frontier answer…", group: "Frontier", run: func(m *Model) tea.Cmd { return m.answerForm("") }},
	}
	if t := v.selected(); t != nil {
		cs = append(slices.Clip(taskCommands(t)), cs...)
	}
	return cs
}

// taskCommands are the palette entries for one task.
func taskCommands(t *task.Task) []command {
	act := func(k string) func(m *Model) tea.Cmd {
		return func(m *Model) tea.Cmd { return m.taskAction(k, t) }
	}
	id := t.ID
	return []command{
		{title: "Open " + id, group: "Task", key: "enter", run: func(m *Model) tea.Cmd { return m.openTask(id) }},
		{title: "Run / resume " + id + "…", group: "Task", key: "r", run: act("r")},
		{title: "Frontier review of " + id + "…", group: "Task", key: "e", run: act("e")},
		{title: "Verify " + id + " (targeted)", group: "Task", key: "v", run: act("v")},
		{title: "Verify " + id + " (full gate)", group: "Task", key: "V", run: act("V")},
		{title: "Cancel " + id, group: "Task", key: "c", run: act("c")},
		{title: "Clean up " + id + "…", group: "Task", key: "x", run: act("x")},
		{title: "Store frontier answer for " + id + "…", group: "Task", key: "a", run: act("a")},
	}
}
