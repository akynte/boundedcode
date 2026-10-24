package direct

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

// shells are background commands started with bash run_in_background.
type shells struct {
	mu   sync.Mutex
	next int
	byID map[string]*shell
}

type shell struct {
	id, desc string
	cmd      *exec.Cmd
	out      bytes.Buffer
	read     int // bytes already returned
	done     bool
	err      error
	started  time.Time
	mu       sync.Mutex
}

func (r *Runner) shells() *shells {
	if r.bg == nil {
		r.bg = &shells{byID: map[string]*shell{}}
	}
	return r.bg
}

func (s *shells) start(argv []string, dir, desc string) (string, error) {
	s.mu.Lock()
	s.next++
	id := fmt.Sprintf("shell-%d", s.next)
	s.mu.Unlock()
	c := exec.CommandContext(context.Background(), argv[0], argv[1:]...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "CI=1", "NO_COLOR=1", "TERM=dumb", "PAGER=cat")
	sh := &shell{id: id, desc: desc, cmd: c, started: time.Now()}
	c.Stdout, c.Stderr = &lockedWriter{sh}, &lockedWriter{sh}
	setProcessGroup(c)
	if err := c.Start(); err != nil {
		return "", err
	}
	s.mu.Lock()
	s.byID[id] = sh
	s.mu.Unlock()
	go func() {
		err := c.Wait()
		sh.mu.Lock()
		sh.done, sh.err = true, err
		sh.mu.Unlock()
	}()
	return fmt.Sprintf("started %s in the background (%s); read its output with bash_output, stop it with kill_shell", id, oneLine(strings.Join(argv[2:], " "), 80)), nil
}

type lockedWriter struct{ s *shell }

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.s.mu.Lock()
	defer w.s.mu.Unlock()
	if w.s.out.Len() > 4<<20 {
		w.s.out.Reset()
		w.s.read = 0
		w.s.out.WriteString("[earlier output dropped]\n")
	}
	return w.s.out.Write(p)
}

func (s *shells) get(id string) (*shell, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sh := s.byID[id]; sh != nil {
		return sh, nil
	}
	var ids []string
	for k := range s.byID {
		ids = append(ids, k)
	}
	sort.Strings(ids)
	return nil, fmt.Errorf("no background shell %q (running: %s)", id, strings.Join(ids, ", "))
}

// output returns new output since the last read and the status.
func (sh *shell) output() (string, string) {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	all := sh.out.Bytes()
	if sh.read > len(all) {
		sh.read = 0
	}
	fresh := string(all[sh.read:])
	sh.read = len(all)
	status := "running"
	if sh.done {
		status = "exited 0"
		var ee *exec.ExitError
		if errors.As(sh.err, &ee) {
			status = fmt.Sprintf("exited %d", ee.ExitCode())
		} else if sh.err != nil {
			status = "failed: " + sh.err.Error()
		}
	}
	return fresh, status
}

func (sh *shell) kill() string {
	sh.mu.Lock()
	done := sh.done
	sh.mu.Unlock()
	if done {
		return sh.id + " had already exited"
	}
	if sh.cmd.Cancel != nil {
		_ = sh.cmd.Cancel()
	} else if sh.cmd.Process != nil {
		_ = sh.cmd.Process.Kill()
	}
	return "killed " + sh.id
}

// Close stops every background shell (end of session).
func (s *shells) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sh := range s.byID {
		sh.kill()
	}
}

func bashOutputTool() Tool {
	return Tool{
		Name:        "bash_output",
		Description: "Read new output from a background shell started with bash run_in_background, with its status.",
		Schema:      schema(map[string]any{"shell_id": prop("string", "The id returned when the command was started")}, "shell_id"),
		Summary:     func(a map[string]any) string { return "output of " + str(a, "shell_id") },
		Run: func(_ context.Context, r *Runner, a map[string]any) (string, error) {
			sh, err := r.shells().get(str(a, "shell_id"))
			if err != nil {
				return "", err
			}
			out, status := sh.output()
			if out == "" {
				out = "(no new output)"
			}
			return fmt.Sprintf("[%s %s, %s]\n%s", sh.id, status, time.Since(sh.started).Round(time.Second), out), nil
		},
	}
}

func killShellTool() Tool {
	return Tool{
		Name:        "kill_shell",
		Description: "Stop a background shell started with bash run_in_background.",
		Schema:      schema(map[string]any{"shell_id": prop("string", "The shell id")}, "shell_id"),
		Summary:     func(a map[string]any) string { return "kill " + str(a, "shell_id") },
		Run: func(_ context.Context, r *Runner, a map[string]any) (string, error) {
			sh, err := r.shells().get(str(a, "shell_id"))
			if err != nil {
				return "", err
			}
			return sh.kill(), nil
		},
	}
}
