//go:build linux

package llamacpp

import (
	"bytes"
	"fmt"
	"os"
	"syscall"
)

func libraryPathVar() string { return "LD_LIBRARY_PATH" }

// alive reports whether pid is a running llama-server (guards against PID reuse).
func alive(pid int) bool {
	if pid <= 0 || syscall.Kill(pid, 0) != nil {
		return false
	}
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return false
	}
	if st, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		// Field 3 is the state; Z means zombie (exited, not yet reaped).
		if i := bytes.LastIndexByte(st, ')'); i > 0 && i+2 < len(st) && st[i+2] == 'Z' {
			return false
		}
	}
	return bytes.Contains(b, []byte("llama-server"))
}
