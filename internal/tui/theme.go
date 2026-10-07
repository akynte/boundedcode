package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/akynte/boundedcode/internal/task"
)

// Colours adapt to the terminal background.
var (
	cAccent   = lipgloss.AdaptiveColor{Light: "#5B4BDB", Dark: "#9D8CFF"}
	cAccent2  = lipgloss.AdaptiveColor{Light: "#0B8A7D", Dark: "#3CCFBF"}
	cText     = lipgloss.AdaptiveColor{Light: "#1F2330", Dark: "#E4E6F0"}
	cMuted    = lipgloss.AdaptiveColor{Light: "#6B7085", Dark: "#8A8FA6"}
	cFaint    = lipgloss.AdaptiveColor{Light: "#A3A8BA", Dark: "#555A6E"}
	cBorder   = lipgloss.AdaptiveColor{Light: "#D5D8E3", Dark: "#363A4A"}
	cSelBg    = lipgloss.AdaptiveColor{Light: "#E6E2FF", Dark: "#2E2A4F"}
	cAccentBg = lipgloss.AdaptiveColor{Light: "#D4CCFF", Dark: "#3F3770"}
	cOK       = lipgloss.AdaptiveColor{Light: "#1E8E3E", Dark: "#5BD47E"}
	cWarn     = lipgloss.AdaptiveColor{Light: "#B26A00", Dark: "#F2B84B"}
	cErr      = lipgloss.AdaptiveColor{Light: "#C62828", Dark: "#FF6B6B"}
	cInfo     = lipgloss.AdaptiveColor{Light: "#1565C0", Dark: "#6CB6FF"}
	cOnBadge  = lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#11131A"}
)

var (
	sText    = lipgloss.NewStyle().Foreground(cText)
	sMuted   = lipgloss.NewStyle().Foreground(cMuted)
	sFaint   = lipgloss.NewStyle().Foreground(cFaint)
	sBold    = lipgloss.NewStyle().Foreground(cText).Bold(true)
	sAccent  = lipgloss.NewStyle().Foreground(cAccent)
	sAccentB = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	sAccent2 = lipgloss.NewStyle().Foreground(cAccent2)
	sOK      = lipgloss.NewStyle().Foreground(cOK)
	sWarn    = lipgloss.NewStyle().Foreground(cWarn)
	sErr     = lipgloss.NewStyle().Foreground(cErr)
	sInfo    = lipgloss.NewStyle().Foreground(cInfo)
	sKey     = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	sSel     = lipgloss.NewStyle().Background(cSelBg)
	sTitle   = lipgloss.NewStyle().Foreground(cText).Bold(true)
	sSection = lipgloss.NewStyle().Foreground(cAccent2).Bold(true)

	sPanel = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBorder).Padding(0, 1)
	sModal = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).Padding(1, 2)
)

// badge renders a filled label.
func badge(text string, bg lipgloss.TerminalColor) string {
	return lipgloss.NewStyle().Background(bg).Foreground(cOnBadge).Bold(true).Padding(0, 1).Render(text)
}

// statusColor maps task, check and verification states to colours.
func statusColor(s string) lipgloss.AdaptiveColor {
	switch s {
	case string(task.StatusActive), "running", "info":
		return cInfo
	case string(task.StatusBlocked), "warn", "warning", "tests_green", "pending", "skipped", "declined":
		return cWarn
	case string(task.StatusCompleted), "ok", "pass", "passed", "task_verified", "targeted_pass", "full_pass", "answered", "answered_manual", "helped", "healthy":
		return cOK
	case string(task.StatusFailed), "fail", "error", "failing", "blocked_packet":
		return cErr
	default:
		return cMuted
	}
}

// statusIcon is a one-cell glyph for a state.
func statusIcon(s string) string {
	st := lipgloss.NewStyle().Foreground(statusColor(s))
	switch s {
	case string(task.StatusActive), "running":
		return st.Render("●")
	case string(task.StatusBlocked), "warn":
		return st.Render("◐")
	case string(task.StatusCompleted), "ok", "pass":
		return st.Render("✔")
	case string(task.StatusFailed), "fail", "error":
		return st.Render("✘")
	case string(task.StatusCancelled):
		return st.Render("○")
	case "skipped":
		return st.Render("–")
	default:
		return st.Render("·")
	}
}
