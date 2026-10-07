package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/akynte/boundedcode/internal/stats"
)

var statsWindows = []struct {
	label string
	d     time.Duration
}{{"all time", 0}, {"24 hours", 24 * time.Hour}, {"7 days", 7 * 24 * time.Hour}, {"30 days", 30 * 24 * time.Hour}}

// statsView shows local-first metrics.
type statsView struct {
	win    int
	sum    *stats.Summary
	err    error
	busy   bool
	loaded bool
}

type statsMsg struct {
	win int
	sum stats.Summary
	err error
}

func (v *statsView) name() string          { return "Stats" }
func (v *statsView) capturing() bool       { return false }
func (v *statsView) loading() bool         { return v.busy }
func (v *statsView) init(m *Model) tea.Cmd { return v.load(m) }
func (v *statsView) refresh(m *Model) tea.Cmd {
	if v.busy || m.ticks%10 != 0 {
		return nil
	}
	return v.load(m)
}

func (v *statsView) load(m *Model) tea.Cmd {
	v.busy = true
	win := v.win
	be, ctx := m.be, m.ctx
	return func() tea.Msg {
		s, err := be.Stats(ctx, statsWindows[win].d)
		return statsMsg{win, s, err}
	}
}

func (v *statsView) update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case statsMsg:
		if msg.win != v.win {
			return nil
		}
		v.busy, v.loaded = false, true
		v.err = msg.err
		if msg.err == nil {
			s := msg.sum
			v.sum = &s
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "right", "l", "tab", "w", "]":
			v.win = (v.win + 1) % len(statsWindows)
			return v.load(m)
		case "shift+tab", "[":
			v.win = (v.win + len(statsWindows) - 1) % len(statsWindows)
			return v.load(m)
		case "R":
			return v.load(m)
		}
	}
	return nil
}

func pct(f float64) string { return fmt.Sprintf("%.0f%%", 100*f) }

func (v *statsView) view(m *Model, w, h int) string {
	var b strings.Builder
	var chips []string
	for i, s := range statsWindows {
		if i == v.win {
			chips = append(chips, badge(s.label, cAccent))
		} else {
			chips = append(chips, sMuted.Render(" "+s.label+" "))
		}
	}
	b.WriteString(" " + sTitle.Render("Local-first metrics") + "   " + strings.Join(chips, " ") + "\n\n")
	if !v.loaded {
		return b.String() + " " + m.spin.View() + sMuted.Render(" computing…")
	}
	if v.err != nil {
		return b.String() + " " + sErr.Render("✘ "+v.err.Error())
	}
	s := v.sum
	tile := func(label, value, sub string, c lipgloss.TerminalColor, tw int) string {
		body := sMuted.Render(label) + "\n" + lipgloss.NewStyle().Foreground(c).Bold(true).Render(value) + "\n" + sFaint.Render(trunc(sub, tw-4))
		return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBorder).Padding(0, 1).Width(tw - 2).Render(body)
	}
	cols := 4
	if w < 100 {
		cols = 2
	}
	tw := (w - 2) / cols
	tiles := []string{
		tile("Tasks", fmt.Sprint(s.Tasks), fmt.Sprintf("%d completed", s.Completed), cText, tw),
		tile("Task-verified", fmt.Sprint(s.CompletedVerified), fmt.Sprintf("%d checks green, unverified", s.Completed-s.CompletedVerified), cOK, tw),
		tile("Local-only rate", pct(s.LocalOnlyRate), fmt.Sprintf("%d of %d completed", s.CompletedLocalOnly, s.Completed), cAccent, tw),
		tile("Verified tasks / hour", fmt.Sprintf("%.2f", s.VerifiedTasksPerHour), fmt.Sprintf("%.2f wall hours", s.WallHours), cAccent2, tw),
		tile("Escalation rate", pct(s.EscalationRate), fmt.Sprintf("%d sent · %d declined · %d blocked", s.EscalationsSent, s.EscalationsDeclined, s.EscalationsBlocked), cWarn, tw),
		tile("Frontier token share", fmt.Sprintf("%.2f%%", 100*s.FrontierTokenShare), human(s.FrontierPacketTok)+" packet tokens", cText, tw),
		tile("Local tokens", human(s.LocalTokens), human(s.GeneratedTokens)+" generated · "+human(s.CachedPromptTokens)+" cached", cText, tw),
		tile("Attempts / completed", fmt.Sprintf("%.2f", s.AttemptsPerCompleted), fmt.Sprintf("%d verification runs (%d failed)", s.VerificationRuns, s.FailedVerificationRuns), cText, tw),
	}
	for i := 0; i < len(tiles); i += cols {
		b.WriteString(" " + lipgloss.JoinHorizontal(lipgloss.Top, tiles[i:min(i+cols, len(tiles))]...) + "\n")
	}
	b.WriteString("\n " + sTitle.Render("By status") + "\n")
	keys := make([]string, 0, len(s.ByStatus))
	for k := range s.ByStatus {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return s.ByStatus[keys[i]] > s.ByStatus[keys[j]] })
	bw := max(10, w-30)
	for _, k := range keys {
		n := s.ByStatus[k]
		bar := int(float64(n) / float64(max(1, s.Tasks)) * float64(bw))
		fmt.Fprintf(&b, "  %s %s %s %s\n", statusIcon(k), sText.Render(fit(k, 10)),
			lipgloss.NewStyle().Foreground(statusColor(k)).Render(strings.Repeat("█", max(bar, min(1, n)))), sMuted.Render(fmt.Sprint(n)))
	}
	b.WriteString("\n " + sMuted.Render(fmt.Sprintf("context: %d condensations · %d sessions resumed · %d context resets", s.Condensations, s.SessionsResumed, s.ContextResets)) + "\n")
	if s.Tasks == 0 {
		b.WriteString(" " + sFaint.Render("No tasks recorded; benchmark runs keep their own state (see benchmarks/reports).") + "\n")
	}
	return b.String()
}

func (v *statsView) hints(m *Model) []hint {
	return []hint{{"tab/→", "time window"}, {"R", "refresh"}}
}

func (v *statsView) commands(m *Model) []command {
	var cs []command
	for i, s := range statsWindows {
		cs = append(cs, command{title: "Stats for " + s.label, group: "Stats", run: func(m *Model) tea.Cmd { v.win = i; return v.load(m) }})
	}
	return cs
}
