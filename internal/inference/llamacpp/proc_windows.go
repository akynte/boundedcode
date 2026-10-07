//go:build windows

package llamacpp

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// detachedAttr starts the server in its own process group, without a
// console, so it outlives this process and does not get its Ctrl+C.
func detachedAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS}
}

// terminate stops the server. Windows has no graceful signal for a
// detached process; llama-server keeps no state that needs a clean exit.
func terminate(pid int) error { return kill(pid) }

func kill(pid int) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.TerminateProcess(h, 1)
}

// libraryEnv puts the binary's directory first on PATH (DLLs next to the
// executable are found anyway; this covers helper processes).
func libraryEnv(bin string) []string {
	return append(os.Environ(), "PATH="+joinEnvPath(filepath.Dir(bin), os.Getenv("PATH")))
}

// stillActive is GetExitCodeProcess's code for a running process.
const stillActive = 259

// alive reports whether pid is a running llama-server (guards against PID reuse).
func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil || code != stillActive {
		return false
	}
	buf := make([]uint16, windows.MAX_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(windows.UTF16ToString(buf[:n])), "llama-server")
}
