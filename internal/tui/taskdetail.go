package tui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/akynte/boundedcode/internal/compat"
	"github.com/akynte/boundedcode/internal/task"
	"github.com/akynte/boundedcode/internal/telemetry"
	"github.com/akynte/boundedcode/internal/verify"
)

type detailTab int

const (
	tabOverview detailTab = iota
	tabActivity
	tabDiff
	tabVerify
	tabEscalations
	tabOutput
)

var tabNames = []string{"Overview", "Activity", "Diff", "Verification", "Escalations", "Output"}

// scroller is a scroll position over rendered lines; follow keeps the
// bottom in view as lines are added.
type scroller struct {
	top    int
	follow bool
}

func (s *scroller) handle(msg tea.Msg, total, h int) bool {
	maxTop := max(0, total-h)
	switch m := msg.(type) {
	case tea.KeyMsg:
		switch m.String() {
		case "up", "k":
			s.top--
		case "down", "j":
			s.top++
		case "pgup", "ctrl+u":
			s.top -= max(1, h-2)
		case "pgdown", "ctrl+d", " ":
			s.top += max(1, h-2)
		case "home", "g":
			s.top = 0
		case "end", "G":
			s.top = maxTop
		default:
			return false
		}
	case tea.MouseMsg:
		switch m.Button {
		case tea.MouseButtonWheelUp:
			s.top -= 3
		case tea.MouseButtonWheelDown:
			s.top += 3
		default:
			return false
		}
	default:
		return false
	}
	s.top = max(0, min(s.top, maxTop))
	s.follow = s.top >= maxTop
	return true
}

// window returns the visible lines.
func (s *scroller) window(lines []string, h int) ([]string, int, int) {
	maxTop := max(0, len(lines)-h)
	if s.follow {
		s.top = maxTop
	}
	s.top = max(0, min(s.top, maxTop))
	to := min(len(lines), s.top+h)
	return lines[s.top:to], s.top, to
}

// taskView shows one task.
type taskView struct {
	open    bool
	id      string
	tab     detailTab
	d       *TaskDetail
	err     error
	busy    bool
	events  []telemetry.Event
	lastEv  int64
	diffs   []RepoDiff
	diffErr error
	diffAt  time.Time
	verifs  []verify.Result
	gate    *compat.Report // nil: the cross-repository gate never ran
	escs    []Escalation
	scroll  [6]scroller
	h       int
	lines   []string // rendered lines of the current tab
	loadSeq int
	// actCache holds the rendered activity for (events, width).
	actCache struct {
		n, w  int
		lines []string
	}
}

type taskDetailMsg struct {
	id     string
	seq    int
	d      TaskDetail
	err    error
	events []telemetry.Event
	verifs []verify.Result
	gate   *compat.Report
	escs   []Escalation
}

type taskDiffMsg struct {
	id    string
	diffs []RepoDiff
	err   error
}

func (v *taskView) name() string          { return "Task" }
func (v *taskView) capturing() bool       { return false }
func (v *taskView) loading() bool         { return v.busy }
func (v *taskView) init(m *Model) tea.Cmd { return v.load(m) }
func (v *taskView) usesLeft() bool        { return true }
func (v *taskView) close()                { v.open = false }

func (v *taskView) openTask(m *Model, id string) tea.Cmd {
	if v.id != id {
		*v = taskView{tab: v.tab}
		v.id = id
		for i := range v.scroll {
			v.scroll[i].follow = detailTab(i) == tabActivity || detailTab(i) == tabOutput
		}
	}
	v.open = true
	return tea.Batch(v.load(m), v.loadDiff(m))
}

func (v *taskView) refresh(m *Model) tea.Cmd {
	if v.busy {
		return nil
	}
	cmds := []tea.Cmd{v.load(m)}
	// Diffs run git; refresh them less often, and only while visible.
	if v.tab == tabDiff && time.Since(v.diffAt) > 5*time.Second {
		cmds = append(cmds, v.loadDiff(m))
	}
	return tea.Batch(cmds...)
}

func (v *taskView) load(m *Model) tea.Cmd {
	v.busy = true
	v.loadSeq++
	id, seq, after := v.id, v.loadSeq, v.lastEv
	be, ctx := m.be, m.ctx
	return func() tea.Msg {
		msg := taskDetailMsg{id: id, seq: seq}
		msg.d, msg.err = be.Task(ctx, id)
		if msg.err != nil {
			return msg
		}
		msg.events, _ = be.Events(ctx, msg.d.Task.ID, after, 5000)
		msg.verifs, _ = be.Verifications(ctx, msg.d.Task.ID, 50)
		if rep, ok, err := be.Compat(ctx, msg.d.Task.ID); err == nil && ok {
			msg.gate = &rep
		}
		msg.escs, _ = be.Escalations(ctx, msg.d.Task.ID)
		return msg
	}
}

func (v *taskView) loadDiff(m *Model) tea.Cmd {
	v.diffAt = time.Now()
	id := v.id
	be, ctx := m.be, m.ctx
	return func() tea.Msg {
		ds, err := be.Diffs(ctx, id)
		return taskDiffMsg{id, ds, err}
	}
}

func (v *taskView) update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case taskDetailMsg:
		if msg.id != v.id {
			return nil
		}
		v.busy = false
		v.err = msg.err
		if msg.err != nil {
			return nil
		}
		d := msg.d
		v.d = &d
		v.id = d.Task.ID // a prefix or alias resolves to the canonical id
		v.events = append(v.events, msg.events...)
		if n := len(v.events); n > 0 {
			v.lastEv = v.events[n-1].ID
		}
		v.verifs, v.escs, v.gate = msg.verifs, msg.escs, msg.gate
		return nil
	case taskDiffMsg:
		if msg.id == v.id {
			v.diffs, v.diffErr = msg.diffs, msg.err
		}
		return nil
	case jobDoneMsg:
		if j := m.job(msg.id); j != nil && j.taskID == v.id {
			return tea.Batch(v.load(m), v.loadDiff(m))
		}
		return nil
	case tea.KeyMsg:
		if v.scroll[v.tab].handle(msg, len(v.lines), v.h) {
			return nil
		}
		switch msg.String() {
		case "esc", "backspace":
			v.close()
			return m.views[m.active].init(m)
		case "tab", "right", "l":
			v.tab = (v.tab + 1) % detailTab(len(tabNames))
			return v.onTab(m)
		case "shift+tab", "left", "h":
			v.tab = (v.tab + detailTab(len(tabNames)) - 1) % detailTab(len(tabNames))
			return v.onTab(m)
		case "i":
			if j := m.taskJob(v.id); j != nil && j.running() {
				j.cancel()
				return m.toast("interrupting "+j.title+"; resume later with r", toastInfo)
			}
			return m.toast("no run of this task is in progress here", toastInfo)
		case "d":
			v.tab = tabDiff
			return v.onTab(m)
		case "R":
			return tea.Batch(v.load(m), v.loadDiff(m))
		}
		if v.d != nil {
			return m.taskAction(msg.String(), v.d.Task)
		}
	case tea.MouseMsg:
		v.scroll[v.tab].handle(msg, len(v.lines), v.h)
	}
	return nil
}

func (v *taskView) onTab(m *Model) tea.Cmd {
	if v.tab == tabDiff {
		return v.loadDiff(m)
	}
	return nil
}

func (v *taskView) hints(m *Model) []hint {
	hs := []hint{{"esc", "back"}, {"tab", "next tab"}}
	if m.taskRunning(v.id) {
		hs = append(hs, hint{"i", "interrupt"})
	} else {
		hs = append(hs, hint{"r", "run/resume"})
	}
	return append(hs, hint{"v", "verify"}, hint{"e", "frontier review"}, hint{"c", "cancel"}, hint{"x", "clean up"})
}

func (v *taskView) commands(m *Model) []command {
	cs := []command{}
	if v.d != nil {
		cs = append(cs, taskCommands(v.d.Task)[1:]...)
	}
	for i, n := range tabNames {
		cs = append(cs, command{title: "Show " + n, group: "Task", run: func(m *Model) tea.Cmd { v.tab = detailTab(i); return v.onTab(m) }})
	}
	cs = append(cs, command{title: "Interrupt the running task", group: "Task", key: "i", run: func(m *Model) tea.Cmd {
		return v.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	}})
	return cs
}

func (v *taskView) view(m *Model, w, h int) string {
	if v.err != nil {
		return "\n " + sErr.Render("✘ "+v.err.Error()) + "\n\n " + sFaint.Render("esc to go back")
	}
	if v.d == nil {
		return "\n " + m.spin.View() + sMuted.Render(" loading "+v.id+"…")
	}
	t := v.d.Task
	var b strings.Builder
	// Title bar.
	status := string(t.Status)
	running := m.taskRunning(t.ID)
	if running {
		status = "running"
	} else if v.d.LeaseOwner != "" && t.Status == task.StatusActive {
		status = "running"
	}
	st := badge(strings.ToUpper(status), statusColor(status))
	if running {
		st = badge(m.spin.View()+" RUNNING", cInfo)
	}
	b.WriteString(" " + st + "  " + sTitle.Render(t.ID) + sFaint.Render("  ·  ") + sMuted.Render(string(t.Phase)) +
		sFaint.Render("  ·  verification ") + lipgloss.NewStyle().Foreground(statusColor(t.VerificationState)).Render(orDash(t.VerificationState)))
	if v.d.LeaseOwner != "" && !running {
		b.WriteString(sFaint.Render("  ·  run by " + v.d.LeaseOwner))
	}
	b.WriteString("\n " + sText.Render(trunc(oneLine(t.OriginalRequest), w-2)) + "\n")
	// Tabs.
	var tabs []string
	for i, n := range tabNames {
		label := n
		switch detailTab(i) {
		case tabDiff:
			label += fmt.Sprintf(" %d", len(v.diffs))
		case tabEscalations:
			label += fmt.Sprintf(" %d", len(v.escs))
		case tabActivity:
			label += fmt.Sprintf(" %d", len(v.events))
		}
		if detailTab(i) == v.tab {
			tabs = append(tabs, lipgloss.NewStyle().Foreground(cAccent).Bold(true).Underline(true).Render(label))
		} else {
			tabs = append(tabs, sMuted.Render(label))
		}
	}
	b.WriteString(" " + strings.Join(tabs, sFaint.Render("   ")) + "\n")
	b.WriteString(sFaint.Render(strings.Repeat("─", w)) + "\n")
	v.h = max(1, h-5)
	cw := w - 2
	switch v.tab {
	case tabOverview:
		v.lines = v.overview(m, cw)
	case tabActivity:
		if c := &v.actCache; c.n != len(v.events) || c.w != cw {
			c.n, c.w, c.lines = len(v.events), cw, v.activity(cw)
		}
		v.lines = v.actCache.lines
	case tabDiff:
		v.lines = v.diffLines(cw)
	case tabVerify:
		v.lines = v.verifyLines(cw)
	case tabEscalations:
		v.lines = v.escalationLines(cw)
	case tabOutput:
		v.lines = v.outputLines(m, cw)
	}
	vis, from, to := v.scroll[v.tab].window(v.lines, v.h)
	for _, l := range vis {
		b.WriteString(" " + l + "\n")
	}
	if len(v.lines) > v.h {
		pct := 100 * to / max(1, len(v.lines))
		follow := ""
		if v.scroll[v.tab].follow {
			follow = " · following"
		}
		b.WriteString(lipgloss.PlaceHorizontal(w, lipgloss.Right, sFaint.Render(fmt.Sprintf("%d–%d of %d (%d%%)%s ", from+1, to, len(v.lines), pct, follow))))
	}
	return b.String()
}

// overview renders the task card: request, budget, worktrees, attempts and
// decisions.
func (v *taskView) overview(m *Model, w int) []string {
	t := v.d.Task
	var left, right []string
	twoCol := w >= 110
	lw, rw := w, w
	if twoCol {
		rw = 44
		lw = w - rw - 3
	}
	section := func(dst *[]string, title string) {
		if len(*dst) > 0 {
			*dst = append(*dst, "")
		}
		*dst = append(*dst, sSection.Render(title))
	}
	addWrapped := func(dst *[]string, s string, width int, style lipgloss.Style, prefix string) {
		for _, l := range strings.Split(wrap(s, width-len(prefix)), "\n") {
			*dst = append(*dst, prefix+style.Render(l))
		}
	}
	section(&left, "Request")
	addWrapped(&left, t.OriginalRequest, lw, sText, "")
	if t.Goal != "" && t.Goal != t.OriginalRequest {
		section(&left, "Goal")
		addWrapped(&left, t.Goal, lw, sText, "")
	}
	if len(t.AcceptanceCriteria) > 0 {
		section(&left, "Acceptance criteria")
		for _, c := range t.AcceptanceCriteria {
			addWrapped(&left, c, lw, sText, "  • ")
		}
	}
	if len(t.ChangedFiles) > 0 {
		section(&left, fmt.Sprintf("Changed files (%d)", len(t.ChangedFiles)))
		for _, f := range t.ChangedFiles {
			left = append(left, "  "+sAccent2.Render(trunc(f, lw-2)))
		}
	}
	if len(v.d.Strategies) > 0 {
		section(&left, "Attempts")
		for _, s := range v.d.Strategies {
			icon := statusIcon(map[string]string{"succeeded": "ok", "rejected": "fail", "failed": "fail", "active": "running"}[s.Outcome])
			left = append(left, fmt.Sprintf("  %s %s %s", icon, sBold.Render(fmt.Sprintf("#%d", s.Attempt)), sMuted.Render(s.Outcome)))
			if s.Summary != "" {
				addWrapped(&left, s.Summary, lw, sText, "     ")
			}
			if s.Reason != "" {
				addWrapped(&left, s.Reason, lw, sFaint, "     ")
			}
		}
	}
	if len(t.Decisions) > 0 {
		section(&left, "Decisions")
		ds := t.Decisions
		if len(ds) > 12 {
			ds = ds[len(ds)-12:]
		}
		for _, d := range ds {
			src := lipgloss.NewStyle().Foreground(map[string]lipgloss.AdaptiveColor{"user": cAccent, "policy": cWarn, "frontier": cAccent2}[d.Source]).Render(fit(d.Source, 8))
			first := true
			for _, l := range strings.Split(wrap(d.Text, lw-12), "\n") {
				if first {
					left = append(left, "  "+src+"  "+sText.Render(l))
					first = false
				} else {
					left = append(left, strings.Repeat(" ", 12)+sText.Render(l))
				}
			}
		}
	}

	bud := t.Budget
	mw := max(6, rw-30)
	section(&right, "Budget")
	right = append(right,
		kv("Attempts", 14, fit(fmt.Sprintf("%d / %d", t.AttemptCount, bud.MaxAttempts), 16))+meter(float64(t.AttemptCount), float64(bud.MaxAttempts), mw),
		kv("Local tokens", 14, fit(human(bud.UsedLocalTokens)+" / "+human(bud.MaxLocalTokens), 16))+meter(float64(bud.UsedLocalTokens), float64(bud.MaxLocalTokens), mw),
		kv("Wall clock", 14, fit(dur(bud.UsedWallClockS)+" / "+dur(bud.MaxWallClockS), 16))+meter(bud.UsedWallClockS, bud.MaxWallClockS, mw),
		kv("Escalations", 14, fit(fmt.Sprintf("%d / %d", bud.UsedEscalations, bud.MaxEscalations), 16))+meter(float64(bud.UsedEscalations), float64(bud.MaxEscalations), mw),
	)
	section(&right, "Counters")
	right = append(right,
		kv("Generated", 14, human(bud.GeneratedTokens)+sFaint.Render(" tokens")),
		kv("Cached prompt", 14, human(bud.CachedTokens)+sFaint.Render(" tokens")),
		kv("Condensations", 14, fmt.Sprint(bud.Condensations))+sFaint.Render("   resumes ")+fmt.Sprint(bud.SessionsResumed)+sFaint.Render("   resets ")+fmt.Sprint(bud.ContextResets),
		kv("Verify runs", 14, fmt.Sprint(bud.VerificationRuns))+sFaint.Render(fmt.Sprintf("   (%d failed)", bud.FailedVerifyRuns)),
	)
	section(&right, "Agent")
	right = append(right,
		kv("Runtime", 14, orDash(t.AgentRuntime)),
		kv("Model", 14, orDash(t.ModelProfile)),
		kv("Session", 14, trunc(orDash(t.AgentSessionID), rw-14)),
		kv("Created", 14, ago(t.CreatedAt)),
		kv("Updated", 14, ago(t.UpdatedAt)),
	)
	if t.FinishedAt != "" {
		right = append(right, kv("Finished", 14, ago(t.FinishedAt)))
	}
	if len(v.d.Worktrees) > 0 {
		section(&right, "Worktrees")
		for _, wt := range v.d.Worktrees {
			base := wt.BaseCommit
			if len(base) > 10 {
				base = base[:10]
			}
			right = append(right, "  "+sBold.Render(wt.RepoName)+sFaint.Render(" "+wt.Branch+" @ "+base))
			right = append(right, "  "+sFaint.Render(trunc(wt.Path, rw-2)))
		}
	}
	if !twoCol {
		return append(append(left, ""), right...)
	}
	n := max(len(left), len(right))
	out := make([]string, n)
	for i := range n {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out[i] = fit(l, lw) + sFaint.Render(" │ ") + r
	}
	return out
}

var (
	reCommand = regexp.MustCompile(`"command"\s*:\s*"((?:[^"\\]|\\.)*)`)
	rePath    = regexp.MustCompile(`"path"\s*:\s*"((?:[^"\\]|\\.)*)`)
	reSubcmd  = regexp.MustCompile(`"command"\s*:\s*"(view|create|str_replace|insert|undo_edit)"`)
)

// actionSummary extracts the most telling part of an agent action.
func actionSummary(tool, action string) string {
	if p := rePath.FindStringSubmatch(action); p != nil {
		verb := "edit"
		if c := reSubcmd.FindStringSubmatch(action); c != nil {
			verb = c[1]
		}
		return verb + " " + unescape(p[1])
	}
	if c := reCommand.FindStringSubmatch(action); c != nil {
		return "$ " + unescape(c[1])
	}
	return oneLine(action)
}

func unescape(s string) string {
	var out string
	if json.Unmarshal([]byte(`"`+s+`"`), &out) == nil {
		return oneLine(out)
	}
	return oneLine(s)
}

// activity renders the audit log as a timeline.
func (v *taskView) activity(w int) []string {
	if len(v.events) == 0 {
		return []string{sMuted.Render("No activity recorded yet. Run the task with ") + sKey.Render("r") + sMuted.Render(".")}
	}
	return renderActivity(v.events, w, true)
}

// renderActivity renders audit events as a timeline; stamped adds the time
// of each event (the chat transcript leaves it out).
func renderActivity(events []telemetry.Event, w int, stamped bool) []string {
	var out []string
	pad := 2
	if stamped {
		pad = 11
	}
	tw := w - pad
	add := func(ts, icon, text string) {
		lines := strings.Split(wrap(text, tw), "\n")
		for i, l := range lines {
			switch {
			case i > 0:
				out = append(out, strings.Repeat(" ", pad)+l)
			case stamped:
				out = append(out, sFaint.Render(clock(ts))+" "+icon+" "+l)
			default:
				out = append(out, icon+" "+l)
			}
		}
	}
	for _, e := range events {
		var d map[string]any
		_ = json.Unmarshal(e.Data, &d)
		str := func(k string) string {
			if s, ok := d[k].(string); ok {
				return s
			}
			if d[k] == nil {
				return ""
			}
			return fmt.Sprint(d[k])
		}
		num := func(k string) string { return str(k) }
		switch e.Kind {
		case "agent.event":
			kind := str("kind")
			switch kind {
			case "ActionEvent":
				add(e.TS, sAccent.Render("⚙"), sBold.Render(str("tool"))+" "+sText.Render(actionSummary(str("tool"), str("action"))))
				if th := str("thought"); th != "" {
					for _, l := range strings.Split(wrap(oneLine(th), tw-2), "\n") {
						out = append(out, strings.Repeat(" ", pad+2)+sFaint.Render(l))
					}
				}
			case "ObservationEvent":
				if str("is_error") == "true" {
					add(e.TS, sErr.Render("↳"), sErr.Render(str("tool")+" returned an error"))
				}
			case "AgentErrorEvent":
				add(e.TS, sErr.Render("✘"), sErr.Render(str("tool")+": "+oneLine(str("error"))))
			case "MessageEvent":
				if t := str("text"); t != "" {
					add(e.TS, sAccent2.Render("»"), sText.Render(oneLine(t)))
				}
			case "Condensation", "CondensationSummaryEvent":
				add(e.TS, sInfo.Render("⇣"), sMuted.Render("context condensed"))
			}
		case "attempt.started":
			stamp := ""
			if stamped {
				stamp = sFaint.Render(clock(e.TS)) + " "
			}
			out = append(out, "", stamp+sSection.Render(fmt.Sprintf("━━ Attempt %s ", num("attempt")))+sFaint.Render(str("mode")))
		case "context.pack":
			add(e.TS, sFaint.Render("▤"), sMuted.Render(fmt.Sprintf("context pack: %s tokens (%s)", num("tokens"), str("mode"))))
		case "agent.session":
			verb := "opened"
			if str("resumed") == "true" {
				verb = "resumed"
			}
			add(e.TS, sFaint.Render("◇"), sMuted.Render("agent session "+verb+" ("+str("mode")+")"))
		case "agent.turn":
			add(e.TS, sFaint.Render("◆"), sMuted.Render(fmt.Sprintf("agent stopped: %s, %s events", str("status"), num("events_new"))))
			if f := str("final"); f != "" {
				add(e.TS, " ", sText.Render(oneLine(f)))
			}
		case "verify.result":
			ok := str("passed") == "true"
			icon, st := sOK.Render("✔"), sOK
			text := fmt.Sprintf("verification %s (%s) passed", str("repo"), str("scope"))
			if !ok {
				icon, st = sErr.Render("✘"), sErr
				text = fmt.Sprintf("verification %s (%s) failed: %s", str("repo"), str("scope"), strings.Trim(str("failures"), "[]"))
			}
			add(e.TS, icon, st.Render(text)+sFaint.Render(fmt.Sprintf("  %sms", num("ms"))))
		case "verify.evidence":
			icon := sOK.Render("✔")
			if str("verified") != "true" {
				icon = sWarn.Render("◐")
			}
			add(e.TS, icon, sMuted.Render("behavioural evidence "+str("repo")+": "+oneLine(str("reason"))))
		case "task.created":
			add(e.TS, sAccent.Render("✚"), sText.Render("task created"))
		case "task.completed":
			add(e.TS, sOK.Render("★"), sOK.Bold(true).Render(fmt.Sprintf("completed after %s attempt(s), %s tokens", num("attempts"), num("tokens"))))
		case "task.blocked":
			add(e.TS, sWarn.Render("◐"), sWarn.Render("blocked: "+oneLine(str("reason"))))
		case "task.cancelled":
			add(e.TS, sFaint.Render("○"), sMuted.Render("cancelled"))
		case "task.clarified":
			add(e.TS, sAccent.Render("?"), sText.Render("clarified by the user"))
		case "task.recovered":
			add(e.TS, sWarn.Render("↺"), sWarn.Render("recovered after an unclean stop"))
		case "strategy.stopped":
			add(e.TS, sWarn.Render("■"), sWarn.Render("strategy stopped: "+oneLine(str("reason"))))
		case "frontier.triggered", "frontier.answered", "frontier.failed", "frontier.blocked":
			add(e.TS, sAccent2.Render("⇪"), sAccent2.Render(strings.TrimPrefix(e.Kind, "frontier.")+" "+compactData(d)))
		case "policy.violation", "infra.failure", "infra.repair_failed", "agent.error", "agent.resume_failed", "checkpoint.failed":
			add(e.TS, sErr.Render("!"), sErr.Render(e.Kind+" "+compactData(d)))
		case "contract.check":
			add(e.TS, sWarn.Render("⇄"), sWarn.Render("cross-service counterparts to check: "+compactData(d)))
		default:
			add(e.TS, sFaint.Render("·"), sMuted.Render(e.Kind)+" "+sFaint.Render(compactData(d)))
		}
	}
	return out
}

// compactData renders event data as k=v pairs.
func compactData(d map[string]any) string {
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		var s string
		switch x := d[k].(type) {
		case string:
			s = x
		default:
			b, _ := json.Marshal(x)
			s = string(b)
		}
		parts = append(parts, k+"="+oneLine(s))
	}
	return strings.Join(parts, " ")
}

func (v *taskView) diffLines(w int) []string {
	if v.diffErr != nil {
		return []string{sErr.Render("✘ " + v.diffErr.Error())}
	}
	if len(v.diffs) == 0 {
		return []string{sMuted.Render("No changes against the base commits yet.")}
	}
	var out []string
	for _, d := range v.diffs {
		add, del := 0, 0
		for l := range strings.SplitSeq(d.Diff, "\n") {
			switch {
			case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"):
			case strings.HasPrefix(l, "+"):
				add++
			case strings.HasPrefix(l, "-"):
				del++
			}
		}
		out = append(out, sSection.Render("■ "+d.Repo)+"  "+sOK.Render(fmt.Sprintf("+%d", add))+" "+sErr.Render(fmt.Sprintf("-%d", del)), "")
		out = append(out, colorDiff(d.Diff, w)...)
		out = append(out, "")
	}
	return out
}

// colorDiff styles a unified diff.
func colorDiff(diff string, w int) []string {
	var out []string
	for l := range strings.SplitSeq(strings.TrimRight(diff, "\n"), "\n") {
		l = strings.ReplaceAll(l, "\t", "    ")
		l = trunc(l, w)
		switch {
		case strings.HasPrefix(l, "diff --git"):
			out = append(out, "", sBold.Render(l))
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"), strings.HasPrefix(l, "index "):
			out = append(out, sFaint.Render(l))
		case strings.HasPrefix(l, "@@"):
			out = append(out, sInfo.Render(l))
		case strings.HasPrefix(l, "+"):
			out = append(out, sOK.Render(l))
		case strings.HasPrefix(l, "-"):
			out = append(out, sErr.Render(l))
		default:
			out = append(out, sText.Render(l))
		}
	}
	return out
}

func (v *taskView) verifyLines(w int) []string {
	if len(v.verifs) == 0 {
		return []string{sMuted.Render("No verification runs yet. Press ") + sKey.Render("v") + sMuted.Render(" (targeted) or ") + sKey.Render("V") + sMuted.Render(" (full gate).")}
	}
	out := compatLines(v.gate, w)
	if len(out) > 0 {
		out = append(out, "")
	}
	for i, r := range v.verifs {
		icon, st := sOK.Render("✔"), sOK
		verdict := "passed"
		if !r.Passed {
			icon, st, verdict = sErr.Render("✘"), sErr, "failed"
		}
		when := r.Started.Local().Format("Jan 02 15:04:05")
		head := fmt.Sprintf("%s %s %s  %s", icon, sBold.Render(r.Repository), sMuted.Render("("+string(r.Scope)+")"), st.Render(verdict))
		out = append(out, head+sFaint.Render(fmt.Sprintf("  %s · %s", when, r.Duration.Round(100*time.Millisecond))))
		// Stage details for the most recent run of each repository.
		latest := true
		for _, p := range v.verifs[:i] {
			if p.Repository == r.Repository {
				latest = false
			}
		}
		if !latest {
			continue
		}
		for _, s := range r.Stages {
			line := fmt.Sprintf("    %s %s %s", statusIcon(s.Status), fit(s.Name, 16), sFaint.Render(fit(fmt.Sprintf("%dms", s.DurationMS), 9)))
			out = append(out, line+sMuted.Render(trunc(s.Command, w-34)))
			if s.Status == "fail" || s.Status == "error" {
				body := s.Output
				if s.Digest != "" {
					body = s.Digest
				}
				ls := strings.Split(strings.TrimRight(body, "\n"), "\n")
				if len(ls) > 30 {
					ls = append([]string{"…"}, ls[len(ls)-30:]...)
				}
				for _, l := range ls {
					out = append(out, "        "+sErr.Render(trunc(strings.ReplaceAll(l, "\t", "  "), w-8)))
				}
			}
		}
		out = append(out, "")
	}
	return out
}

// compatLines renders the cross-repository compatibility report: a header
// with the state, then one entry per link (result, kind, contract, the two
// sides at their commits, and why).
func compatLines(rep *compat.Report, w int) []string {
	if rep == nil {
		return nil
	}
	state := rep.State()
	color := map[string]lipgloss.AdaptiveColor{"compatible": cOK, "none": cOK, "broken": cErr, "error": cErr, "untested": cWarn}[state]
	head := sBold.Render("Cross-repository compatibility ") + lipgloss.NewStyle().Foreground(color).Render(strings.ToUpper(state))
	head += sFaint.Render(fmt.Sprintf("  %d broken · %d untested · %d compatible", rep.Count(compat.Broken), rep.Count(compat.Untested), rep.Count(compat.Compatible)))
	if rep.Stale() {
		head += "  " + sErr.Render("STALE")
	}
	out := []string{head}
	if rep.Error != "" {
		out = append(out, "    "+sErr.Render(trunc(rep.Error, w-4)))
	}
	if len(rep.Links) == 0 && rep.Error == "" {
		out = append(out, "    "+sMuted.Render("no gRPC, protobuf or OpenAPI link is affected by the change"))
	}
	icons := map[compat.Result]string{compat.Compatible: sOK.Render("✔"), compat.Broken: sErr.Render("✘"), compat.Untested: sWarn.Render("?")}
	for _, l := range rep.Links {
		line := fmt.Sprintf("  %s %s %s %s", icons[l.Result], fit(string(l.Result), 10), sFaint.Render(fit(l.Kind, 12)), l.Contract)
		if l.Stale {
			line += sErr.Render("  stale")
		}
		out = append(out, trunc(line, w+40))
		out = append(out, "      "+sMuted.Render(trunc(fmt.Sprintf("%s@%.10s %s:%d → %s@%.10s %s:%d", l.From.Repo, l.From.Commit, l.From.File, l.From.Line,
			l.To.Repo, l.To.Commit, l.To.File, l.To.Line), w-6)))
		for _, ln := range strings.Split(wrap(l.Reason, w-8), "\n") {
			out = append(out, "      "+sText.Render(ln))
		}
	}
	return out
}

func (v *taskView) escalationLines(w int) []string {
	if len(v.escs) == 0 {
		return []string{sMuted.Render("No frontier escalations for this task.")}
	}
	var out []string
	for _, e := range v.escs {
		changed := "–"
		if e.AdviceChangedCode != nil {
			changed = fmt.Sprint(*e.AdviceChangedCode)
		}
		out = append(out, fmt.Sprintf("%s %s  %s  %s", sBold.Render(fmt.Sprintf("#%d", e.ID)), badge(e.Trigger, cAccent2),
			lipgloss.NewStyle().Foreground(statusColor(e.Status)).Render(e.Status), sFaint.Render(ago(e.Created))))
		out = append(out, "   "+kv("provider", 16, orDash(e.Provider)+" "+sFaint.Render(orDash(e.Model))))
		out = append(out, "   "+kv("packet", 16, fmt.Sprintf("%d tokens", e.PacketTokens)))
		out = append(out, "   "+kv("outcome", 16, orDash(e.Outcome)+sFaint.Render("  task: "+orDash(e.TaskOutcome)+"  changed code: "+changed)))
		for i, l := range strings.Split(wrap(oneLine(e.Reason), w-19), "\n") {
			label := ""
			if i == 0 {
				label = "reason"
			}
			out = append(out, "   "+kv(label, 16, sText.Render(l)))
		}
		out = append(out, "")
	}
	return out
}

func (v *taskView) outputLines(m *Model, w int) []string {
	var out []string
	for _, j := range m.jobs {
		if j.taskID != v.id {
			continue
		}
		head := statusIcon(j.status()) + " " + sBold.Render(j.title) + sFaint.Render(fmt.Sprintf("  %s · %s", j.start.Format("15:04:05"), j.elapsed().Round(time.Second)))
		if j.running() {
			head = m.spin.View() + " " + sBold.Render(j.title) + sFaint.Render("  running "+j.elapsed().Round(time.Second).String())
		}
		out = append(out, head)
		out = append(out, j.rendered(w)...)
		out = append(out, "")
	}
	if len(out) == 0 {
		return []string{sMuted.Render("Output of runs, verifications and other operations started here appears in this tab.")}
	}
	return out
}

// styleOutput highlights command output lines.
func styleOutput(l string) string {
	switch {
	case strings.HasPrefix(l, "$ "):
		return sAccent.Render(l)
	case strings.HasPrefix(l, "✘"), strings.HasPrefix(l, "error"), strings.Contains(l, "FAIL"):
		return sErr.Render(l)
	case strings.HasPrefix(l, "✔"), strings.Contains(l, " PASS"), strings.HasPrefix(l, "[ok"):
		return sOK.Render(l)
	case strings.HasPrefix(l, "[warn"):
		return sWarn.Render(l)
	case strings.HasPrefix(l, "[fail"):
		return sErr.Render(l)
	case strings.HasPrefix(l, "## "), strings.HasPrefix(l, "### "):
		return sSection.Render(l)
	}
	return sText.Render(l)
}
