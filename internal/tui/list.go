package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// cursor is a selection over n rows shown h at a time.
type cursor struct {
	pos, offset int
}

// clamp keeps the selection within n rows and scrolled into a window of h.
func (c *cursor) clamp(n, h int) {
	if n <= 0 {
		c.pos, c.offset = 0, 0
		return
	}
	c.pos = max(0, min(c.pos, n-1))
	if h <= 0 {
		return
	}
	if c.pos < c.offset {
		c.offset = c.pos
	}
	if c.pos >= c.offset+h {
		c.offset = c.pos - h + 1
	}
	c.offset = max(0, min(c.offset, max(0, n-h)))
}

// window returns the visible row range [from, to).
func (c *cursor) window(n, h int) (int, int) {
	c.clamp(n, h)
	return c.offset, min(n, c.offset+h)
}

// handle moves the selection for navigation keys and mouse wheel; it
// reports whether msg was consumed.
func (c *cursor) handle(msg tea.Msg, n, h int) bool {
	switch m := msg.(type) {
	case tea.KeyMsg:
		switch m.String() {
		case "up", "k":
			c.pos--
		case "down", "j":
			c.pos++
		case "pgup", "ctrl+u":
			c.pos -= max(1, h-1)
		case "pgdown", "ctrl+d":
			c.pos += max(1, h-1)
		case "home", "g":
			c.pos = 0
		case "end", "G":
			c.pos = n - 1
		default:
			return false
		}
	case tea.MouseMsg:
		switch m.Button {
		case tea.MouseButtonWheelUp:
			c.pos -= 3
		case tea.MouseButtonWheelDown:
			c.pos += 3
		default:
			return false
		}
	default:
		return false
	}
	c.clamp(n, h)
	return true
}
