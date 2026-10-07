package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// frontierView shows frontier escalation settings and history.
type frontierView struct {
	escs     []Escalation
	groups   []EscalationGroup
	login    string
	loginErr error
	err      error
	loaded   bool
	busy     bool
	cur      cursor
	h        int
}

type frontierMsg struct {
	escs   []Escalation
	groups []EscalationGroup
	err    error
}

type frontierLoginMsg struct {
	s   string
	err error
}

func (v *frontierView) name() string    { return "Frontier" }
func (v *frontierView) capturing() bool { return false }
func (v *frontierView) loading() bool   { return v.busy }
func (v *frontierView) init(m *Model) tea.Cmd {
	cmds := []tea.Cmd{v.load(m)}
	if m.info.FrontierEnabled && m.info.FrontierProvider == "codex" && v.login == "" {
		be, ctx := m.be, m.ctx
		cmds = append(cmds, func() tea.Msg {
			s, err := be.FrontierLogin(ctx)
			return frontierLoginMsg{s, err}
		})
	}
	return tea.Batch(cmds...)
}

func (v *frontierView) refresh(m *Model) tea.Cmd {
	if v.busy || m.ticks%5 != 0 {
		return nil
	}
	return v.load(m)
}

func (v *frontierView) load(m *Model) tea.Cmd {
	v.busy = true
	be, ctx := m.be, m.ctx
	return func() tea.Msg {
		var msg frontierMsg
		msg.escs, msg.err = be.Escalations(ctx, "")
		if msg.err == nil {
			msg.groups, msg.err = be.EscalationSummary(ctx)
		}
		return msg
	}
}

func (v *frontierView) update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case frontierMsg:
		v.busy, v.loaded = false, true
		// Newest first.
		escs := make([]Escalation, len(msg.escs))
		for i, e := range msg.escs {
			escs[len(escs)-1-i] = e
		}
		v.escs, v.groups, v.err = escs, msg.groups, msg.err
	case frontierLoginMsg:
		v.login, v.loginErr = msg.s, msg.err
		if v.login == "" && msg.err == nil {
			v.login = "unknown"
		}
	case tea.KeyMsg:
		if v.cur.handle(msg, len(v.escs), v.h) {
			return nil
		}
		switch msg.String() {
		case "enter", "o":
			if v.cur.pos < len(v.escs) {
				return m.openTask(v.escs[v.cur.pos].Task)
			}
		case "a":
			id := ""
			if v.cur.pos < len(v.escs) {
				id = v.escs[v.cur.pos].Task
			}
			return m.answerForm(id)
		case "R":
			v.login = ""
			return v.init(m)
		}
	case tea.MouseMsg:
		v.cur.handle(msg, len(v.escs), v.h)
	}
	return nil
}

func (v *frontierView) view(m *Model, w, h int) string {
	var b strings.Builder
	b.WriteString(" " + sTitle.Render("Frontier escalation") + sFaint.Render("  Z1–Z4 gates · packets leave the machine only with approval") + "\n\n")
	in := m.info
	state := badge("DISABLED", cMuted)
	if in.FrontierEnabled {
		state = badge("ENABLED", cOK)
	}
	approval := sOK.Render("required")
	if !in.FrontierApproval {
		approval = sWarn.Render("not required")
	}
	card := []string{
		state + "  " + sBold.Render(orDash(in.FrontierProvider)),
		"",
		kv("Approval", 18, approval) + "     " + kv("Max packet", 12, fmt.Sprintf("%s tokens", human(in.FrontierMaxPacket))),
	}
	if in.FrontierEnabled && in.FrontierProvider == "codex" {
		login := m.spin.View() + sMuted.Render(" checking codex login…")
		switch {
		case v.loginErr != nil:
			login = sWarn.Render(firstLine(v.login + " " + v.loginErr.Error()))
		case v.login != "":
			login = sText.Render(v.login)
		}
		card = append(card, kv("Codex", 18, login))
	}
	if !in.FrontierEnabled {
		card = append(card, sFaint.Render("Local-only. Enable with frontier.enabled: true in "+in.ConfigFile))
	}
	b.WriteString(sPanel.Width(w - 4).Render(strings.Join(card, "\n")))
	b.WriteString("\n\n")
	if len(v.groups) > 0 {
		b.WriteString(" " + sTitle.Render("Summary") + "\n")
		for _, g := range v.groups {
			fmt.Fprintf(&b, "  %s %s %s %s\n", badge(g.Trigger, cAccent2), lipgloss.NewStyle().Foreground(statusColor(g.Status)).Render(fit(g.Status, 16)),
				sMuted.Render(fit("outcome "+orDash(g.Outcome), 32)), sText.Render(fmt.Sprintf("%d × · %s packet tokens", g.Count, human(g.PacketTokens))))
		}
		b.WriteString("\n")
	}
	b.WriteString(" " + sTitle.Render("Escalations") + "\n")
	widths := []int{4, 4, 16, 12, 16, 7, 9, 0}
	b.WriteString(" " + sFaint.Render(row(w-2, widths, "#", "GATE", "TASK", "STATUS", "MODEL", "TOKENS", "WHEN", "REASON")) + "\n")
	used := strings.Count(b.String(), "\n")
	v.h = max(1, h-used-1)
	switch {
	case !v.loaded:
		b.WriteString(" " + m.spin.View() + sMuted.Render(" loading…"))
	case v.err != nil:
		b.WriteString(" " + sErr.Render("✘ "+v.err.Error()))
	case len(v.escs) == 0:
		b.WriteString(" " + sMuted.Render("No escalations yet. Tasks escalate when a gate triggers; request one with ") + sKey.Render("e") + sMuted.Render(" on a task."))
	}
	from, to := v.cur.window(len(v.escs), v.h)
	for i := from; i < to; i++ {
		e := v.escs[i]
		line := row(w-2, widths, sFaint.Render(fmt.Sprint(e.ID)), sAccent2.Render(e.Trigger), sText.Render(e.Task),
			lipgloss.NewStyle().Foreground(statusColor(e.Status)).Render(e.Status), sMuted.Render(orDash(e.Model)),
			sMuted.Render(human(e.PacketTokens)), sFaint.Render(ago(e.Created)), sText.Render(oneLine(e.Reason)))
		if i == v.cur.pos {
			line = sSel.Render(fit(line, w-2))
		}
		b.WriteString(" " + line + "\n")
	}
	return b.String()
}

func (v *frontierView) hints(m *Model) []hint {
	return []hint{{"enter", "open task"}, {"a", "store answer"}, {"R", "refresh"}}
}

func (v *frontierView) commands(m *Model) []command {
	return []command{
		{title: "Store a frontier answer…", group: "Frontier", key: "a", run: func(m *Model) tea.Cmd { return v.update(m, keyMsg("a")) }},
		{title: "Refresh frontier status", group: "Frontier", key: "R", run: func(m *Model) tea.Cmd { v.login = ""; return v.init(m) }},
	}
}

func keyMsg(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
