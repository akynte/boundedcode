package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// modal is a dialog drawn over the current view. update returns true when
// the dialog is finished and should be closed.
type modal interface {
	update(msg tea.Msg) (done bool, cmd tea.Cmd)
	view(w, h int) string
}

type fieldKind int

const (
	fText fieldKind = iota
	fArea
	fToggle
	fChoice
	fMulti
)

type field struct {
	key, label, help string
	kind             fieldKind
	required         bool
	input            textinput.Model
	area             textarea.Model
	on               bool
	options          []string
	choice           int
	checked          []bool
	mcur             int
}

func textField(key, label, value, placeholder string) *field {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = placeholder
	in.SetValue(value)
	in.CharLimit = 4096
	in.PlaceholderStyle = sFaint
	in.TextStyle = sText
	in.Cursor.Style = sAccent
	return &field{key: key, label: label, kind: fText, input: in}
}

func areaField(key, label, value, placeholder string, height int) *field {
	ta := textarea.New()
	ta.Placeholder = placeholder
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.CharLimit = 0
	ta.SetHeight(height)
	ta.SetValue(value)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Placeholder = sFaint
	ta.BlurredStyle.Placeholder = sFaint
	ta.FocusedStyle.Text = sText
	ta.BlurredStyle.Text = sMuted
	return &field{key: key, label: label, kind: fArea, area: ta}
}

func toggleField(key, label string, on bool) *field {
	return &field{key: key, label: label, kind: fToggle, on: on}
}

func choiceField(key, label string, options []string, selected string) *field {
	f := &field{key: key, label: label, kind: fChoice, options: options}
	for i, o := range options {
		if o == selected {
			f.choice = i
		}
	}
	return f
}

func multiField(key, label string, options []string, checked bool) *field {
	f := &field{key: key, label: label, kind: fMulti, options: options, checked: make([]bool, len(options))}
	for i := range f.checked {
		f.checked[i] = checked
	}
	return f
}

func (f *field) withHelp(h string) *field { f.help = h; return f }
func (f *field) mustFill() *field         { f.required = true; return f }
func (f *field) focus() tea.Cmd           { return f.setFocus(true) }
func (f *field) blur()                    { f.setFocus(false) }
func (f *field) textValue() string        { return strings.TrimSpace(f.input.Value()) }
func (f *field) areaValue() string        { return strings.TrimSpace(f.area.Value()) }
func (f *field) selected() string         { return f.options[f.choice] }
func (f *field) multiline() bool          { return f.kind == fArea }
func (f *field) wantsArrows() bool        { return f.kind == fArea || f.kind == fMulti }
func (f *field) empty() bool              { return f.value() == "" }
func (f *field) setWidth(w int) {
	switch f.kind {
	case fText:
		f.input.Width = w - 1
	case fArea:
		f.area.SetWidth(w)
	}
}
func (f *field) setFocus(on bool) (c tea.Cmd) {
	switch f.kind {
	case fText:
		if on {
			return f.input.Focus()
		}
		f.input.Blur()
	case fArea:
		if on {
			return f.area.Focus()
		}
		f.area.Blur()
	}
	return nil
}

func (f *field) value() string {
	switch f.kind {
	case fText:
		return f.textValue()
	case fArea:
		return f.areaValue()
	case fChoice:
		return f.selected()
	case fToggle:
		if f.on {
			return "true"
		}
	case fMulti:
		return strings.Join(f.selection(), ",")
	}
	return ""
}

func (f *field) selection() []string {
	var out []string
	for i, c := range f.checked {
		if c {
			out = append(out, f.options[i])
		}
	}
	return out
}

// formValues gives typed access to submitted values.
type formValues map[string]*field

func (v formValues) str(k string) string { return v[k].value() }
func (v formValues) on(k string) bool    { return v[k].kind == fToggle && v[k].on }
func (v formValues) multi(k string) []string {
	return v[k].selection()
}

// lines returns the non-empty lines of a text area.
func (v formValues) lines(k string) []string {
	var out []string
	for l := range strings.SplitSeq(v[k].areaValue(), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// form is a modal with labelled fields. ctrl+s submits from anywhere; enter
// submits from a single-line field when it is the last one.
type form struct {
	title, intro, submitLabel string
	fields                    []*field
	idx                       int
	err                       string
	width                     int
	onSubmit                  func(formValues) (tea.Cmd, error)
	// onChange runs after every update (dependent fields).
	onChange func(*form)
}

func newForm(title, intro, submit string, fields []*field, onSubmit func(formValues) (tea.Cmd, error)) *form {
	f := &form{title: title, intro: intro, submitLabel: submit, fields: fields, onSubmit: onSubmit, width: 64}
	return f
}

func (f *form) init() tea.Cmd {
	for i, fl := range f.fields {
		fl.setWidth(f.width)
		if i != f.idx {
			fl.blur()
		}
	}
	if len(f.fields) == 0 {
		return nil
	}
	return f.fields[f.idx].focus()
}

func (f *form) move(d int) tea.Cmd {
	if len(f.fields) == 0 {
		return nil
	}
	f.fields[f.idx].blur()
	f.idx = (f.idx + d + len(f.fields)) % len(f.fields)
	return f.fields[f.idx].focus()
}

func (f *form) submit() (bool, tea.Cmd) {
	vals := formValues{}
	for _, fl := range f.fields {
		if fl.required && fl.empty() {
			f.err = fl.label + " is required"
			return false, nil
		}
		vals[fl.key] = fl
	}
	cmd, err := f.onSubmit(vals)
	if err != nil {
		f.err = err.Error()
		return false, nil
	}
	return true, cmd
}

func (f *form) update(msg tea.Msg) (bool, tea.Cmd) {
	done, cmd := f.handle(msg)
	if !done && f.onChange != nil {
		f.onChange(f)
	}
	return done, cmd
}

func (f *form) handle(msg tea.Msg) (bool, tea.Cmd) {
	if len(f.fields) == 0 {
		if k, ok := msg.(tea.KeyMsg); ok {
			switch k.String() {
			case "esc":
				return true, nil
			case "enter", "ctrl+s":
				return f.submit()
			}
		}
		return false, nil
	}
	cur := f.fields[f.idx]
	if k, ok := msg.(tea.KeyMsg); ok {
		f.err = ""
		switch k.String() {
		case "esc":
			return true, nil
		case "ctrl+s":
			return f.submit()
		case "tab":
			return false, f.move(1)
		case "shift+tab":
			return false, f.move(-1)
		case "up":
			if !cur.wantsArrows() {
				return false, f.move(-1)
			}
			if cur.kind == fMulti {
				if cur.mcur == 0 {
					return false, f.move(-1)
				}
				cur.mcur--
				return false, nil
			}
		case "down":
			if !cur.wantsArrows() {
				return false, f.move(1)
			}
			if cur.kind == fMulti {
				if cur.mcur >= len(cur.options)-1 {
					return false, f.move(1)
				}
				cur.mcur++
				return false, nil
			}
		case "enter":
			if cur.multiline() {
				break
			}
			if f.idx == len(f.fields)-1 {
				return f.submit()
			}
			return false, f.move(1)
		case " ", "x":
			switch cur.kind {
			case fToggle:
				cur.on = !cur.on
				return false, nil
			case fMulti:
				if len(cur.checked) > 0 {
					cur.checked[cur.mcur] = !cur.checked[cur.mcur]
				}
				return false, nil
			}
		case "a":
			if cur.kind == fMulti {
				all := true
				for _, c := range cur.checked {
					all = all && c
				}
				for i := range cur.checked {
					cur.checked[i] = !all
				}
				return false, nil
			}
		case "left", "h", "right", "l":
			if cur.kind == fChoice && len(cur.options) > 0 {
				d := 1
				if s := k.String(); s == "left" || s == "h" {
					d = -1
				}
				cur.choice = (cur.choice + d + len(cur.options)) % len(cur.options)
				return false, nil
			}
		}
	}
	var cmd tea.Cmd
	switch cur.kind {
	case fText:
		cur.input, cmd = cur.input.Update(msg)
	case fArea:
		cur.area, cmd = cur.area.Update(msg)
	}
	return false, cmd
}

func (f *form) view(w, h int) string {
	fw := min(f.width, w-8)
	for _, fl := range f.fields {
		fl.setWidth(fw)
	}
	var b strings.Builder
	b.WriteString(sAccentB.Render(f.title))
	b.WriteString("\n")
	if f.intro != "" {
		b.WriteString(sMuted.Render(wrap(f.intro, fw)))
		b.WriteString("\n")
	}
	for i, fl := range f.fields {
		focused := i == f.idx
		b.WriteString("\n")
		label := fl.label
		if fl.required {
			label += " *"
		}
		mark := "  "
		ls := sMuted
		if focused {
			mark = sAccent.Render("▍ ")
			ls = sBold
		}
		switch fl.kind {
		case fToggle:
			box := sFaint.Render("[ ]")
			if fl.on {
				box = sAccent.Render("[✔]")
			}
			b.WriteString(mark + box + " " + ls.Render(label))
		case fChoice:
			var opts []string
			for j, o := range fl.options {
				if j == fl.choice {
					opts = append(opts, badge(o, cAccent))
				} else {
					opts = append(opts, sMuted.Render(" "+o+" "))
				}
			}
			b.WriteString(mark + ls.Render(label) + "\n  " + strings.Join(opts, " "))
		case fMulti:
			b.WriteString(mark + ls.Render(label))
			if len(fl.options) == 0 {
				b.WriteString("\n  " + sFaint.Render("(none)"))
			}
			for j, o := range fl.options {
				box := sFaint.Render("[ ]")
				if fl.checked[j] {
					box = sAccent.Render("[✔]")
				}
				line := "  " + box + " " + o
				if focused && j == fl.mcur {
					line = "  " + box + " " + sSel.Render(o)
				}
				b.WriteString("\n" + line)
			}
		case fText:
			b.WriteString(mark + ls.Render(label) + "\n  ")
			b.WriteString(underline(fl.input.View(), fw, focused))
		case fArea:
			b.WriteString(mark + ls.Render(label) + "\n")
			b.WriteString(indentBlock(fl.area.View(), "  "))
		}
		if fl.help != "" && focused {
			b.WriteString("\n  " + sFaint.Render(wrap(fl.help, fw)))
		}
	}
	b.WriteString("\n\n")
	if f.err != "" {
		b.WriteString(sErr.Render("✘ "+wrap(f.err, fw)) + "\n\n")
	}
	b.WriteString(badge(" "+f.submitLabel+" ", cAccent) + "  " + sKey.Render("ctrl+s") + sMuted.Render(" submit  ") +
		sKey.Render("tab") + sMuted.Render(" next  ") + sKey.Render("esc") + sMuted.Render(" cancel"))
	return sModal.Width(fw + 6).Render(b.String())
}

func underline(s string, w int, focused bool) string {
	c := cBorder
	if focused {
		c = cAccent
	}
	return s + "\n  " + lipgloss.NewStyle().Foreground(c).Render(strings.Repeat("─", w))
}

func indentBlock(s, pfx string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = pfx + l
	}
	return strings.Join(lines, "\n")
}

// confirm is a yes/no dialog.
type confirm struct {
	title, body string
	danger      bool
	yes         bool
	onAnswer    func(bool) tea.Cmd
}

func (c *confirm) update(msg tea.Msg) (bool, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	switch k.String() {
	case "y", "Y":
		return true, c.onAnswer(true)
	case "n", "N", "esc", "q":
		return true, c.onAnswer(false)
	case "left", "right", "tab", "h", "l":
		c.yes = !c.yes
	case "enter":
		return true, c.onAnswer(c.yes)
	}
	return false, nil
}

func (c *confirm) view(w, h int) string {
	bw := min(64, w-8)
	yes, no := sMuted.Render("  Yes  "), sMuted.Render("  No  ")
	col := cAccent
	if c.danger {
		col = cErr
	}
	if c.yes {
		yes = badge(" Yes ", col)
	} else {
		no = badge(" No ", cMuted)
	}
	title := lipgloss.NewStyle().Foreground(col).Bold(true).Render(c.title)
	body := sText.Render(wrap(c.body, bw))
	keys := sKey.Render("y") + sMuted.Render("/") + sKey.Render("n") + sMuted.Render("  ←/→ choose  enter confirm")
	st := sModal.BorderForeground(col)
	return st.Width(bw + 6).Render(title + "\n\n" + body + "\n\n" + yes + " " + no + "\n\n" + keys)
}
