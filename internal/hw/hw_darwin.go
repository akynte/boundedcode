//go:build darwin

package hw

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func probeHost(ctx context.Context, s *Snapshot) {
	s.CPUModel, _ = unix.Sysctl("machdep.cpu.brand_string")
	if total, err := unix.SysctlUint64("hw.memsize"); err == nil {
		s.MemTotalMiB = int(total >> 20)
	} else {
		s.Notes = append(s.Notes, "hw.memsize: "+err.Error())
	}
	if avail, err := availableMiB(ctx); err == nil {
		s.MemAvailMiB = avail
	} else {
		s.Notes = append(s.Notes, "vm_stat: "+err.Error())
	}
	if runtime.GOARCH == "arm64" && s.MemTotalMiB > 0 {
		// Apple Silicon: the GPU shares system memory (Metal).
		limit, _ := unix.SysctlUint32("iogpu.wired_limit_mb")
		mem, est := metalBudget(s.MemTotalMiB, int(limit))
		s.Accelerator = Accelerator{Kind: AccelMetal, Name: strings.TrimSpace(s.CPUModel), MemoryMiB: mem, Unified: true, Estimated: est}
	}
}

// availableMiB is free + inactive + speculative pages from vm_stat: memory
// the system can hand to a new process without paging.
func availableMiB(ctx context.Context) (int, error) {
	out, err := exec.CommandContext(ctx, "vm_stat").Output()
	if err != nil {
		return 0, err
	}
	page := 4096
	if p, err := unix.SysctlUint32("hw.pagesize"); err == nil && p > 0 {
		page = int(p)
	}
	pages := 0
	for line := range strings.SplitSeq(string(out), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "Pages free", "Pages inactive", "Pages speculative":
			n, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(v), "."))
			pages += n
		}
	}
	return pages * page >> 20, nil
}

// ProcessRSSMiB returns the resident set size of pid in MiB, or 0.
func ProcessRSSMiB(pid int) int {
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0
	}
	kb, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return kb / 1024
}

// FreeDiskMiB returns the free space of the filesystem holding path.
func FreeDiskMiB(path string) (int, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int(st.Bavail * uint64(st.Bsize) >> 20), nil
}
