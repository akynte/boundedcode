//go:build windows

package serena

import (
	"os"
	"strconv"
	"syscall"

	"github.com/akynte/boundedcode/internal/proctree"
)

const instanceEnv = "BOUNDEDCODE_SERENA_INSTANCE"

func procAttr() *syscall.SysProcAttr { return proctree.NewGroupAttr() }

func instanceTag(key string) string { return key + ":" + strconv.Itoa(os.Getpid()) }

// Windows has no /proc to find tagged processes; killing Serena's process
// tree (taskkill /T) ends the language servers it started.
func taggedProcesses(func(string) bool) []int { return nil }

func killTagged(string) {}

func sweepOrphans(string) int { return 0 }

func alive(pid int) bool { return proctree.Alive(pid) }

func killProcessGroup(pid int) { proctree.KillTree(pid) }

// Usage is the resource use of an instance's processes.
type Usage struct {
	Processes  int     `json:"processes"`
	RSSMiB     float64 `json:"rss_mib"`
	CPUSeconds float64 `json:"cpu_seconds"`
}

func usageOf(string) Usage { return Usage{} }
