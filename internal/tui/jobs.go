package tui

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// maxJobLines bounds the output kept per job.
const maxJobLines = 5000

// job is one background operation, usually a CLI command run in-process.
type job struct {
	id      int
	title   string
	args    []string
	taskID  string
	start   time.Time
	end     time.Time
	err     error
	done    bool
	lines   []string
	partial string
	cancel  context.CancelFunc
	onDone  func(m *Model, err error) tea.Cmd
	// view caches the styled, wrapped complete lines for one width.
	view struct {
		width, n int
		lines    []string
	}
}

// rendered returns the output styled and wrapped to width. Complete lines
// are wrapped once and cached; only new lines are processed per frame.
func (j *job) rendered(width int) []string {
	c := &j.view
	if c.width != width || c.n > len(j.lines) {
		c.width, c.n, c.lines = width, 0, nil
	}
	for _, l := range j.lines[c.n:] {
		c.lines = append(c.lines, wrapStyled(l, width)...)
	}
	c.n = len(j.lines)
	if j.partial == "" {
		return c.lines
	}
	p := j.partial
	if i := strings.LastIndexByte(p, '\r'); i >= 0 {
		p = p[i+1:]
	}
	return append(c.lines[:len(c.lines):len(c.lines)], wrapStyled(p, width)...)
}

// wrapStyled wraps one output line and styles each piece.
func wrapStyled(l string, width int) []string {
	parts := strings.Split(wrap(strings.ReplaceAll(l, "\t", "    "), width), "\n")
	for i, p := range parts {
		parts[i] = styleOutput(p)
	}
	return parts
}

func (j *job) running() bool { return !j.done }

func (j *job) elapsed() time.Duration {
	if j.done {
		return j.end.Sub(j.start)
	}
	return time.Since(j.start)
}

// append adds output, splitting it into lines; carriage returns rewrite
// the current line as a terminal would.
func (j *job) append(s string) {
	s = j.partial + s
	parts := strings.Split(s, "\n")
	j.partial = parts[len(parts)-1]
	for _, l := range parts[:len(parts)-1] {
		if i := strings.LastIndexByte(l, '\r'); i >= 0 {
			l = l[i+1:]
		}
		j.lines = append(j.lines, l)
	}
	if over := len(j.lines) - maxJobLines; over > 0 {
		j.lines = append([]string{fmt.Sprintf("… %d earlier lines dropped", over)}, j.lines[over+1:]...)
		j.view.n = len(j.lines) + 1 // invalidate
	}
}

// output returns all lines including an unterminated last line.
func (j *job) output() []string {
	if j.partial == "" {
		return j.lines
	}
	p := j.partial
	if i := strings.LastIndexByte(p, '\r'); i >= 0 {
		p = p[i+1:]
	}
	return append(j.lines[:len(j.lines):len(j.lines)], p)
}

func (j *job) status() string {
	switch {
	case !j.done:
		return "running"
	case j.err != nil:
		return "fail"
	}
	return "ok"
}

type jobOutputMsg struct {
	id   int
	text string
}

type jobDoneMsg struct {
	id  int
	err error
}

// jobWriter forwards output to the UI. Commands may write from several
// goroutines.
type jobWriter struct {
	mu   sync.Mutex
	id   int
	send func(tea.Msg)
}

func (w *jobWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.send(jobOutputMsg{w.id, string(p)})
	return len(p), nil
}

// startJob runs args through the backend in the background. onDone, if
// set, runs on the UI goroutine when the job ends.
func (m *Model) startJob(title, taskID string, args []string, onDone func(m *Model, err error) tea.Cmd) tea.Cmd {
	m.nextJob++
	ctx, cancel := context.WithCancel(m.ctx)
	j := &job{id: m.nextJob, title: title, args: args, taskID: taskID, start: time.Now(), cancel: cancel, onDone: onDone}
	m.jobs = append(m.jobs, j)
	w := &jobWriter{id: j.id, send: m.send}
	j.append(fmt.Sprintf("$ %s %s\n", m.info.Name, cmdline(args)))
	be := m.be
	return tea.Batch(func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				msg = jobDoneMsg{j.id, fmt.Errorf("internal error: %v\n%s", r, debug.Stack())}
			}
		}()
		defer cancel()
		return jobDoneMsg{j.id, be.Exec(ctx, args, w)}
	}, m.spinTick())
}

func (m *Model) job(id int) *job {
	for _, j := range m.jobs {
		if j.id == id {
			return j
		}
	}
	return nil
}

// taskJob returns the latest job of a task.
func (m *Model) taskJob(taskID string) *job {
	for i := len(m.jobs) - 1; i >= 0; i-- {
		if m.jobs[i].taskID == taskID {
			return m.jobs[i]
		}
	}
	return nil
}

func (m *Model) runningJobs() int {
	n := 0
	for _, j := range m.jobs {
		if j.running() {
			n++
		}
	}
	return n
}

// taskRunning reports whether this UI is running the task.
func (m *Model) taskRunning(taskID string) bool {
	j := m.taskJob(taskID)
	return j != nil && j.running() && len(j.args) > 1 && (j.args[0] == "task" || j.args[0] == "frontier")
}

func (m *Model) handleJobMsg(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case jobOutputMsg:
		if j := m.job(msg.id); j != nil {
			j.append(msg.text)
		}
	case jobDoneMsg:
		j := m.job(msg.id)
		if j == nil {
			return nil
		}
		j.done, j.end, j.err = true, time.Now(), msg.err
		if msg.err != nil {
			j.append("\n✘ " + msg.err.Error() + "\n")
		} else {
			j.append(fmt.Sprintf("\n✔ done in %s\n", j.elapsed().Round(100*time.Millisecond)))
		}
		cmds := []tea.Cmd{m.refreshAll()}
		if j.onDone != nil {
			cmds = append(cmds, j.onDone(m, msg.err))
		} else if msg.err != nil {
			cmds = append(cmds, m.toast(j.title+": "+firstLine(msg.err.Error()), toastErr))
		} else {
			cmds = append(cmds, m.toast(j.title+" finished", toastOK))
		}
		return tea.Batch(cmds...)
	}
	return nil
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

// prompt asks a yes/no question from a background goroutine.
func (m *Model) prompt(ctx context.Context, title, body string) bool {
	reply := make(chan bool, 1)
	m.send(promptMsg{title: title, body: body, reply: reply})
	select {
	case ok := <-reply:
		return ok
	case <-ctx.Done():
		return false
	}
}

type promptMsg struct {
	title, body string
	reply       chan bool
}
