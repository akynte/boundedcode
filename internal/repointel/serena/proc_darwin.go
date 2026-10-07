//go:build unix && !linux

package serena

import (
	"os"
	"strconv"
	"syscall"
)

const instanceEnv = "BOUNDEDCODE_SERENA_INSTANCE"

func procAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }

func instanceTag(key string) string { return key + ":" + strconv.Itoa(os.Getpid()) }

// Without /proc, cleanup relies on the process group and on Serena stopping
// its language servers.
func taggedProcesses(func(string) bool) []int { return nil }

func killTagged(string) {}

func sweepOrphans(string) int { return 0 }

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

// Usage is the resource use of an instance's processes.
type Usage struct {
	Processes  int     `json:"processes"`
	RSSMiB     float64 `json:"rss_mib"`
	CPUSeconds float64 `json:"cpu_seconds"`
}

func usageOf(string) Usage { return Usage{} }

// killProcessGroup kills the process group led by pid.
func killProcessGroup(pid int) { _ = syscall.Kill(-pid, syscall.SIGKILL) }
