//go:build darwin

package llamacpp

import (
	"strings"

	"golang.org/x/sys/unix"
)

// libraryPathVar is the macOS library search variable. Release archives
// also set an @loader_path rpath, so it is a fallback.
func libraryPathVar() string { return "DYLD_LIBRARY_PATH" }

// sZomb is a zombie process's p_stat (sys/proc.h).
const sZomb = 5

// alive reports whether pid is a running llama-server (guards against PID
// reuse), from the kernel's process table.
func alive(pid int) bool {
	if pid <= 0 || unix.Kill(pid, 0) != nil {
		return false
	}
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || kp.Proc.P_stat == sZomb {
		return false
	}
	return strings.Contains(unix.ByteSliceToString(kp.Proc.P_comm[:]), "llama-server")
}
