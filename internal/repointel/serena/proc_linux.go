package serena

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// instanceEnv tags every process of an instance. Serena v1.7.0 starts
// language servers in their own session (start_new_session=True), so a
// process-group kill does not reach them; they do inherit the environment,
// and so do their own children (tsserver, go list).
const instanceEnv = "BOUNDEDCODE_SERENA_INSTANCE"

// procAttr puts Serena in its own process group and kills it if the control
// plane dies.
func procAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
}

func instanceTag(key string) string { return key + ":" + strconv.Itoa(os.Getpid()) }

// taggedProcesses returns the pids whose environment carries a tag
// accepted by match. Only our own processes are readable.
func taggedProcesses(match func(tag string) bool) []int {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	prefix := []byte(instanceEnv + "=")
	var out []int
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		env, err := os.ReadFile(filepath.Join("/proc", e.Name(), "environ"))
		if err != nil {
			continue
		}
		for kv := range bytes.SplitSeq(env, []byte{0}) {
			if bytes.HasPrefix(kv, prefix) && match(string(kv[len(prefix):])) {
				out = append(out, pid)
				break
			}
		}
	}
	return out
}

// killTagged kills every process of one instance.
func killTagged(tag string) {
	for _, pid := range taggedProcesses(func(t string) bool { return t == tag }) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

// sweepOrphans kills processes of an instance key whose owning control plane
// is gone (crash, kill -9), so a restart does not accumulate language servers.
func sweepOrphans(key string) int {
	pids := taggedProcesses(func(t string) bool {
		k, owner, ok := strings.Cut(t, ":")
		if !ok || k != key {
			return false
		}
		n, err := strconv.Atoi(owner)
		if err != nil {
			return true
		}
		return n != os.Getpid() && !alive(n)
	})
	for _, pid := range pids {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	return len(pids)
}

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// Usage is the resource use of an instance's processes (Serena and its
// language servers).
type Usage struct {
	Processes  int     `json:"processes"`
	RSSMiB     float64 `json:"rss_mib"`
	CPUSeconds float64 `json:"cpu_seconds"`
}

// clockTicks is USER_HZ, 100 on every Linux platform Go supports.
const clockTicks = 100

func usageOf(tag string) Usage {
	var u Usage
	for _, pid := range taggedProcesses(func(t string) bool { return t == tag }) {
		dir := filepath.Join("/proc", strconv.Itoa(pid))
		u.Processes++
		if b, err := os.ReadFile(filepath.Join(dir, "status")); err == nil {
			for line := range strings.SplitSeq(string(b), "\n") {
				if v, ok := strings.CutPrefix(line, "VmRSS:"); ok {
					kb, _ := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "kB")))
					u.RSSMiB += float64(kb) / 1024
				}
			}
		}
		if b, err := os.ReadFile(filepath.Join(dir, "stat")); err == nil {
			// Fields after the parenthesised command: utime is 14th, stime 15th overall.
			if i := bytes.LastIndexByte(b, ')'); i > 0 {
				f := strings.Fields(string(b[i+1:]))
				if len(f) > 12 {
					ut, _ := strconv.ParseFloat(f[11], 64)
					st, _ := strconv.ParseFloat(f[12], 64)
					u.CPUSeconds += (ut + st) / clockTicks
				}
			}
		}
	}
	return u
}
