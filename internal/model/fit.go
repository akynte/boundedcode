package model

import (
	"fmt"
	"sort"

	"github.com/akynte/boundedcode/internal/hw"
)

// Fit levels, best first.
const (
	FitGPU     = "gpu"     // the whole model fits in accelerator memory
	FitOffload = "offload" // MoE: experts in system RAM, the rest on the GPU
	FitSplit   = "split"   // dense: layers split between GPU and system RAM (slow)
	FitCPU     = "cpu"     // system RAM only (slow)
	FitNo      = "too-large"
	FitUnknown = "unknown" // the profile does not record its size
)

var fitRank = map[string]int{FitGPU: 0, FitOffload: 1, FitSplit: 2, FitCPU: 3, FitUnknown: 4, FitNo: 5}

// Fit is how a model fits a machine. It is a rule of thumb (NeedMiB is an
// estimate), not a measurement: `bench infra` measures the real limits.
type Fit struct {
	Profile string `json:"profile"`
	Level   string `json:"fit"`
	NeedMiB int    `json:"need_mib"`
	Detail  string `json:"detail"`
}

// Usable reports whether the model can run (at any speed).
func (f Fit) Usable() bool { return f.Level != FitNo && f.Level != FitUnknown }

// Fast reports whether it runs on the accelerator (fully or with MoE
// offload): the levels BoundedCode's validation used.
func (f Fit) Fast() bool { return f.Level == FitGPU || f.Level == FitOffload }

// memoryNeedMiB estimates the memory a model needs to serve the agent: the
// weights, the KV cache at the profile's context (the profile's estimate,
// else 10% of the weights plus 512 MiB) and 1 GiB of compute buffers.
func memoryNeedMiB(p Profile) int {
	size := int(p.Source.SizeBytes >> 20)
	kv := p.Architecture.KVMiB
	if kv <= 0 {
		kv = size/10 + 512
	}
	return size + kv + 1024
}

// osReserveMiB is system RAM left to the operating system and other
// programs when sizing a model: a quarter of it, between 2 and 4 GiB.
func osReserveMiB(totalMiB int) int { return min(max(totalMiB/4, 2048), 4096) }

// minOffloadVRAMMiB is the smallest GPU worth an MoE expert offload: the
// attention layers, KV cache and compute buffers stay on it.
const minOffloadVRAMMiB = 6144

// FitFor rates a profile against a machine.
func FitFor(p Profile, s hw.Snapshot) Fit {
	f := Fit{Profile: p.Name}
	if p.Source.SizeBytes <= 0 {
		f.Level, f.Detail = FitUnknown, "size unknown"
		return f
	}
	need := memoryNeedMiB(p)
	f.NeedMiB = need
	ram := max(s.MemTotalMiB-osReserveMiB(s.MemTotalMiB), 0)
	acc := s.Accelerator
	gib := func(mib int) string { return fmt.Sprintf("%.1f GB", float64(mib)/1024) }
	switch {
	case acc.Kind == hw.AccelMetal || acc.Unified:
		switch {
		case need <= acc.MemoryMiB:
			f.Level, f.Detail = FitGPU, fmt.Sprintf("needs about %s; the GPU can use about %s of unified memory", gib(need), gib(acc.MemoryMiB))
		case need <= ram:
			f.Level, f.Detail = FitSplit, fmt.Sprintf("needs about %s, more than the GPU can use (%s); part runs on the CPU (slow)", gib(need), gib(acc.MemoryMiB))
		default:
			f.Level, f.Detail = FitNo, fmt.Sprintf("needs about %s; this Mac has %s", gib(need), gib(s.MemTotalMiB))
		}
	case acc.Kind == hw.AccelCUDA && acc.MemoryMiB > 0:
		vram := acc.MemoryMiB
		switch {
		case need <= vram:
			f.Level, f.Detail = FitGPU, fmt.Sprintf("needs about %s; fits in %s of GPU memory", gib(need), gib(vram))
		case p.Architecture.MoE && vram >= minOffloadVRAMMiB && need <= vram+ram:
			f.Level, f.Detail = FitOffload, fmt.Sprintf("needs about %s: attention on the %s GPU, experts in system RAM", gib(need), gib(vram))
		case need <= vram+ram:
			f.Level, f.Detail = FitSplit, fmt.Sprintf("needs about %s, more than the %s GPU; layers split with system RAM (slow)", gib(need), gib(vram))
		default:
			f.Level, f.Detail = FitNo, fmt.Sprintf("needs about %s; GPU %s + RAM %s", gib(need), gib(vram), gib(s.MemTotalMiB))
		}
	default:
		if need <= ram {
			f.Level, f.Detail = FitCPU, fmt.Sprintf("needs about %s of RAM; no supported GPU, so it runs on the CPU (slow)", gib(need))
		} else {
			f.Level, f.Detail = FitNo, fmt.Sprintf("needs about %s; this machine has %s of RAM", gib(need), gib(s.MemTotalMiB))
		}
	}
	return f
}

// Recommendation is the model set-up proposes, with the alternatives.
type Recommendation struct {
	Best   Fit    `json:"best"`
	Fits   []Fit  `json:"fits"` // every profile, best first
	Reason string `json:"reason"`
}

// Recommend picks the model for a machine. The validated default model is
// kept whenever it runs on the accelerator (fully or with MoE offload).
// Otherwise the best fit level wins: on the GPU, validated before
// experimental and then the largest; off it, the smallest memory need.
// Profiles whose license is under review are never proposed.
func Recommend(c Catalog, defaultModel string, s hw.Snapshot) Recommendation {
	var fits []Fit
	params := map[string]float64{}
	status := map[string]string{}
	for _, name := range c.Names() {
		p, _ := c.Get(name)
		fits = append(fits, FitFor(p, s))
		params[name] = p.Architecture.TotalParamsB
		status[name] = p.Status
	}
	sort.SliceStable(fits, func(i, j int) bool {
		a, b := fits[i], fits[j]
		if fitRank[a.Level] != fitRank[b.Level] {
			return fitRank[a.Level] < fitRank[b.Level]
		}
		if !a.Fast() {
			// Off the GPU, speed and headroom matter most: the smallest
			// memory need first.
			return a.NeedMiB < b.NeedMiB
		}
		if (status[a.Profile] == StatusValidated) != (status[b.Profile] == StatusValidated) {
			return status[a.Profile] == StatusValidated
		}
		return params[a.Profile] > params[b.Profile]
	})
	r := Recommendation{Fits: fits}
	for _, f := range fits {
		if f.Profile == defaultModel && f.Fast() {
			r.Best, r.Reason = f, "the default model, validated on this kind of hardware, runs on this machine's GPU"
			return r
		}
	}
	for _, f := range fits {
		if f.Usable() && status[f.Profile] != StatusReview {
			r.Best = f
			switch {
			case f.Fast():
				r.Reason = "the largest model that runs on this machine's GPU"
			default:
				r.Reason = "no model runs on this machine's GPU; this one runs, slowly, with system RAM"
			}
			if status[f.Profile] != StatusValidated {
				r.Reason += " (experimental: not yet validated by BoundedCode)"
			}
			return r
		}
	}
	r.Reason = "no model in the catalog fits this machine; use a cloud provider"
	return r
}
