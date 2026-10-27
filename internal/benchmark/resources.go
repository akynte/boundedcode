package benchmark

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ResourcePeaks are the largest memory figures sampled while a task ran.
// Sampling is periodic, so a peak shorter than the interval can be missed.
type ResourcePeaks struct {
	HarnessRSSMiB   float64  `json:"harness_rss_mib"`                // this process (the harness, and direct mode's agent)
	ServerRSSMiB    float64  `json:"model_server_rss_mib,omitempty"` // every llama-server process
	GPUMemMiB       float64  `json:"gpu_mem_used_mib,omitempty"`     // all GPUs, whole device (other processes included)
	ContainerMemMiB float64  `json:"sandbox_mem_mib,omitempty"`      // the task's running containers
	Samples         int      `json:"samples"`
	Unavailable     []string `json:"unavailable,omitempty"` // figures this platform cannot sample
}

// Sampling intervals: /proc reads are cheap; nvidia-smi and docker stats
// start a process, so they run less often.
const (
	procInterval  = time.Second
	slowInterval  = 5 * time.Second
	containerName = "bc-"
)

// sampleResources samples until the returned function is called, which
// returns the peaks. containers also samples running sandbox containers.
func sampleResources(ctx context.Context, containers bool) func() ResourcePeaks {
	ctx, cancel := context.WithCancel(ctx)
	var (
		mu sync.Mutex
		p  ResourcePeaks
		wg sync.WaitGroup
	)
	linux := runtime.GOOS == "linux"
	if !linux {
		p.Unavailable = append(p.Unavailable, "process memory (Linux /proc only)")
	}
	nvsmi, _ := exec.LookPath("nvidia-smi")
	if nvsmi == "" {
		p.Unavailable = append(p.Unavailable, "GPU memory (no nvidia-smi)")
	}
	docker := ""
	if containers {
		if docker, _ = exec.LookPath("docker"); docker == "" {
			p.Unavailable = append(p.Unavailable, "sandbox memory (no docker)")
		}
	}
	update := func(f func(*ResourcePeaks)) {
		mu.Lock()
		f(&p)
		mu.Unlock()
	}
	sampleProc := func() {
		if !linux {
			return
		}
		self := rssMiB("/proc/self/status")
		server := 0.0
		for _, pid := range pidsNamed("llama-server") {
			server += rssMiB(filepath.Join("/proc", pid, "status"))
		}
		update(func(p *ResourcePeaks) {
			p.Samples++
			p.HarnessRSSMiB = max(p.HarnessRSSMiB, self)
			p.ServerRSSMiB = max(p.ServerRSSMiB, server)
		})
	}
	sampleSlow := func() {
		if nvsmi != "" {
			if v, ok := gpuUsedMiB(ctx, nvsmi); ok {
				update(func(p *ResourcePeaks) { p.GPUMemMiB = max(p.GPUMemMiB, v) })
			}
		}
		if docker != "" {
			if v, ok := containersMiB(ctx, docker); ok {
				update(func(p *ResourcePeaks) { p.ContainerMemMiB = max(p.ContainerMemMiB, v) })
			}
		}
	}
	loop := func(every time.Duration, f func()) {
		defer wg.Done()
		f()
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				f()
			}
		}
	}
	wg.Add(2)
	go loop(procInterval, sampleProc)
	go loop(slowInterval, sampleSlow)
	return func() ResourcePeaks {
		sampleProc()
		cancel()
		wg.Wait()
		mu.Lock()
		defer mu.Unlock()
		return p
	}
}

// rssMiB reads VmRSS from a /proc status file (0 when unreadable).
func rssMiB(status string) float64 {
	f, err := os.Open(status)
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "VmRSS:"); ok {
			kb, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "kB")), 64)
			return kb / 1024
		}
	}
	return 0
}

// pidsNamed lists processes whose command name is name.
func pidsNamed(name string) []string {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join("/proc", e.Name(), "comm"))
		if err == nil && strings.TrimSpace(string(b)) == name {
			out = append(out, e.Name())
		}
	}
	return out
}

func gpuUsedMiB(ctx context.Context, nvsmi string) (float64, bool) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, nvsmi, "--query-gpu=memory.used", "--format=csv,noheader,nounits").Output()
	if err != nil {
		return 0, false
	}
	total := 0.0
	for _, l := range strings.Fields(string(out)) {
		v, err := strconv.ParseFloat(l, 64)
		if err != nil {
			return 0, false
		}
		total += v
	}
	return total, true
}

// containersMiB sums the memory of running sandbox containers (names
// starting with bc-).
func containersMiB(ctx context.Context, docker string) (float64, bool) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, docker, "stats", "--no-stream", "--format", "{{.Name}}\t{{.MemUsage}}").Output()
	if err != nil {
		return 0, false
	}
	total := 0.0
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name, usage, ok := strings.Cut(l, "\t")
		if !ok || !strings.HasPrefix(name, containerName) {
			continue
		}
		used, _, _ := strings.Cut(usage, "/")
		total += parseMiB(strings.TrimSpace(used))
	}
	return total, true
}

// parseMiB reads docker's "512.3MiB" / "1.2GiB" / "800kB".
func parseMiB(s string) float64 {
	for _, u := range []struct {
		suffix string
		mib    float64
	}{{"GiB", 1024}, {"MiB", 1}, {"KiB", 1.0 / 1024}, {"GB", 1e9 / (1 << 20)}, {"MB", 1e6 / (1 << 20)}, {"kB", 1e3 / (1 << 20)}, {"B", 1.0 / (1 << 20)}} {
		if v, ok := strings.CutSuffix(s, u.suffix); ok {
			f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil {
				return 0
			}
			return f * u.mib
		}
	}
	return 0
}
