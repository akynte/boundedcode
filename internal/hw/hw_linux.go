//go:build linux

package hw

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func probeHost(_ context.Context, s *Snapshot) {
	s.CPUModel = cpuModel()
	if mi, err := readMeminfo(); err == nil {
		s.MemTotalMiB = mi["MemTotal"] / 1024
		s.MemAvailMiB = mi["MemAvailable"] / 1024
		s.SwapUsedMiB = (mi["SwapTotal"] - mi["SwapFree"]) / 1024
	} else {
		s.Notes = append(s.Notes, "meminfo: "+err.Error())
	}
}

func cpuModel() string {
	b, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == "model name" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func readMeminfo() (map[string]int, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]int{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(v)
		if len(fields) == 0 {
			continue
		}
		n, err := strconv.Atoi(fields[0])
		if err == nil {
			out[k] = n // kB
		}
	}
	return out, sc.Err()
}

// ProcessRSSMiB returns the resident set size of pid in MiB, or 0.
func ProcessRSSMiB(pid int) int {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				return atoi(f[1]) / 1024
			}
		}
	}
	return 0
}

// FreeDiskMiB returns the free space of the filesystem holding path.
func FreeDiskMiB(path string) (int, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int(st.Bavail * uint64(st.Bsize) >> 20), nil
}
