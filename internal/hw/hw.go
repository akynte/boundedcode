// Package hw reports host hardware and live resource usage. Probes are best
// effort: a missing tool yields an empty field and a note, never an error that
// blocks the caller.
package hw

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// GPU is one NVIDIA GPU as reported by nvidia-smi.
type GPU struct {
	Index         int     `json:"index"`
	Name          string  `json:"name"`
	ComputeCap    string  `json:"compute_cap,omitempty"`
	Driver        string  `json:"driver,omitempty"`
	MemTotalMiB   int     `json:"mem_total_mib"`
	MemUsedMiB    int     `json:"mem_used_mib"`
	UtilPct       int     `json:"util_pct"`
	TempC         int     `json:"temp_c"`
	PowerW        float64 `json:"power_w"`
	PowerLimitW   float64 `json:"power_limit_w,omitempty"`
	SMClockMHz    int     `json:"sm_clock_mhz,omitempty"`
	ThrottleFlags string  `json:"throttle,omitempty"`
}

// Snapshot is a point-in-time view of the machine.
type Snapshot struct {
	Time        time.Time `json:"time"`
	CPUModel    string    `json:"cpu_model"`
	LogicalCPUs int       `json:"logical_cpus"`
	MemTotalMiB int       `json:"mem_total_mib"`
	MemAvailMiB int       `json:"mem_available_mib"`
	SwapUsedMiB int       `json:"swap_used_mib"`
	GPUs        []GPU     `json:"gpus"`
	Notes       []string  `json:"notes,omitempty"`
}

// Probe collects a Snapshot.
func Probe(ctx context.Context) Snapshot {
	s := Snapshot{Time: time.Now().UTC(), LogicalCPUs: runtime.NumCPU(), CPUModel: cpuModel()}
	if mi, err := readMeminfo(); err == nil {
		s.MemTotalMiB = mi["MemTotal"] / 1024
		s.MemAvailMiB = mi["MemAvailable"] / 1024
		s.SwapUsedMiB = (mi["SwapTotal"] - mi["SwapFree"]) / 1024
	} else {
		s.Notes = append(s.Notes, "meminfo: "+err.Error())
	}
	gpus, err := ProbeGPUs(ctx)
	if err != nil {
		s.Notes = append(s.Notes, "nvidia-smi: "+err.Error())
	}
	s.GPUs = gpus
	return s
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

const gpuQuery = "index,name,compute_cap,driver_version,memory.total,memory.used,utilization.gpu,temperature.gpu,power.draw,power.max_limit,clocks.sm,clocks_event_reasons.active"

// ProbeGPUs queries nvidia-smi. It returns (nil, err) when unavailable.
func ProbeGPUs(ctx context.Context) ([]GPU, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu="+gpuQuery, "--format=csv,noheader,nounits").Output()
	if err != nil {
		return nil, err
	}
	return parseGPUs(out)
}

func parseGPUs(out []byte) ([]GPU, error) {
	var gpus []GPU
	for line := range bytes.SplitSeq(bytes.TrimSpace(out), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		f := strings.Split(string(line), ",")
		for i := range f {
			f[i] = strings.TrimSpace(f[i])
		}
		if len(f) < 12 {
			return gpus, fmt.Errorf("unexpected nvidia-smi line: %q", line)
		}
		g := GPU{
			Index: atoi(f[0]), Name: f[1], ComputeCap: f[2], Driver: f[3],
			MemTotalMiB: atoi(f[4]), MemUsedMiB: atoi(f[5]), UtilPct: atoi(f[6]), TempC: atoi(f[7]),
			PowerW: atof(f[8]), PowerLimitW: atof(f[9]), SMClockMHz: atoi(f[10]), ThrottleFlags: f[11],
		}
		gpus = append(gpus, g)
	}
	return gpus, nil
}

func atoi(s string) int     { n, _ := strconv.Atoi(s); return n }
func atof(s string) float64 { n, _ := strconv.ParseFloat(s, 64); return n }

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
