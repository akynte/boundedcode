//go:build unix

package llamacpp

import (
	"os"
	"path/filepath"
	"syscall"
)

// detachedAttr makes the server a session leader, so it outlives this
// process and its process group can be signalled as a whole.
func detachedAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }

// terminate asks the server's process group to stop.
func terminate(pid int) error { return syscall.Kill(-pid, syscall.SIGTERM) }

// kill stops the server's process group.
func kill(pid int) error { return syscall.Kill(-pid, syscall.SIGKILL) }

// libraryEnv lets a binary from a release archive find the shared libraries
// next to it.
func libraryEnv(bin string) []string {
	v := libraryPathVar()
	return append(os.Environ(), v+"="+joinEnvPath(filepath.Dir(bin), os.Getenv(v)))
}
