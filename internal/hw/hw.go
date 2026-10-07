// Package hw reports host hardware and live resource usage. Probes are best
// effort: a missing tool yields an empty field and a note, never an error that
// blocks the caller.
package hw

import (
	"bytes"
	"context"
	"fmt"
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
	OS          string    `json:"os"`
	Arch        string    `json:"arch"`
	CPUModel    string    `json:"cpu_model"`
	LogicalCPUs int       `json:"logical_cpus"`
	MemTotalMiB int       `json:"mem_total_mib"`
	MemAvailMiB int       `json:"mem_available_mib"`
	SwapUsedMiB int       `json:"swap_used_mib"`
	GPUs        []GPU     `json:"gpus"`
	// Accelerator is what llama.cpp can offload to: NVIDIA (CUDA), Apple
	// Silicon (Metal, unified memory) or none (CPU only).
	Accelerator Accelerator `json:"accelerator"`
	Notes       []string    `json:"notes,omitempty"`
}

// Accelerator kinds.
const (
	AccelNone   = "none"
	AccelCUDA   = "cuda"
	AccelMetal  = "metal"
	AccelOthers = "other" // a GPU llama.cpp may use through Vulkan; not sized
)

// Accelerator is the offload target model choices are sized against.
type Accelerator struct {
	Kind string `json:"kind"`
	Name string `json:"name,omitempty"`
	// MemoryMiB is the memory the model can use on the accelerator: the
	// largest NVIDIA GPU's VRAM, or for Apple Silicon the part of unified
	// memory the GPU may wire (see metalBudget). 0 for none.
	MemoryMiB int `json:"memory_mib"`
	// Unified is true when accelerator memory is system RAM (Apple Silicon):
	// MemoryMiB and MemTotalMiB are then the same memory, not additive.
	Unified bool `json:"unified,omitempty"`
	// Estimated marks a MemoryMiB that is a rule of thumb, not reported.
	Estimated bool `json:"estimated,omitempty"`
}

// Probe collects a Snapshot.
func Probe(ctx context.Context) Snapshot {
	s := Snapshot{Time: time.Now().UTC(), OS: runtime.GOOS, Arch: runtime.GOARCH, LogicalCPUs: runtime.NumCPU()}
	probeHost(ctx, &s)
	gpus, err := ProbeGPUs(ctx)
	if err != nil && s.Accelerator.Kind == "" {
		s.Notes = append(s.Notes, "nvidia-smi: "+err.Error())
	}
	s.GPUs = gpus
	if s.Accelerator.Kind == "" {
		s.Accelerator = Accelerator{Kind: AccelNone}
		for _, g := range gpus {
			if g.MemTotalMiB > s.Accelerator.MemoryMiB {
				s.Accelerator = Accelerator{Kind: AccelCUDA, Name: g.Name, MemoryMiB: g.MemTotalMiB}
			}
		}
	}
	return s
}

// metalBudget estimates how much unified memory the GPU may use on Apple
// Silicon when the user has not set iogpu.wired_limit_mb: macOS wires up to
// about two thirds of RAM for the GPU on smaller machines and more on larger
// ones; two thirds is used as the conservative estimate.
func metalBudget(totalMiB, wiredLimitMiB int) (int, bool) {
	if wiredLimitMiB > 0 {
		return min(wiredLimitMiB, totalMiB), false
	}
	return totalMiB * 2 / 3, true
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
