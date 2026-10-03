// Package benchmark measures inference infrastructure and engineering task
// performance. Every result is persisted with the machine snapshot and exact
// configuration that produced it, so numbers in docs are reproducible.
package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/hw"
	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/inference/llamacpp"
	"github.com/akynte/boundedcode/internal/model"
)

// InfraOptions configures an infrastructure benchmark.
type InfraOptions struct {
	Profile model.Profile
	// CtxSizes to test. Each is validated by actually loading the server.
	CtxSizes []int
	// CacheTypes are KV cache types applied to both K and V (e.g. q8_0, f16).
	CacheTypes []string
	// UBatches are physical batch sizes to compare.
	UBatches []int
	// MoESpan is how many n_cpu_moe values above the minimum feasible one to
	// measure (step 2). 0 measures only the minimum.
	MoESpan int
	// MoEMax bounds the n_cpu_moe search (number of MoE layers).
	MoEMax int
	// PromptSizes in tokens; sizes that don't fit the context are skipped.
	PromptSizes []int
	// DecodeTokens generated per request.
	DecodeTokens int
	// Sustained runs a decode loop on the best configuration for this long.
	Sustained time.Duration
	// MinFreeVRAMMiB is the headroom required for a configuration to be
	// recommended (the desktop and other apps share the GPU).
	MinFreeVRAMMiB int
	OutPath        string
	// Only measures exactly these configurations (skips the feasibility
	// search), e.g. to re-measure finalists on an idle machine.
	Only []Config `json:"only,omitempty"`
	// ColdLoad evicts the model from the page cache before each load.
	ColdLoad bool
	// Conditions is a free-text note about the run (idle, AC power, ...).
	Conditions string
}

// Config identifies one server configuration.
type Config struct {
	CtxSize   int    `json:"ctx_size"`
	CacheType string `json:"cache_type"`
	UBatch    int    `json:"ubatch"`
	NCPUMoE   int    `json:"n_cpu_moe"`
}

// InfraReport is the persisted result.
type InfraReport struct {
	ID          string        `json:"id"`
	Kind        string        `json:"kind"`
	Model       string        `json:"model"`
	ModelFile   string        `json:"model_file"`
	Runtime     string        `json:"runtime"`
	RuntimeVer  string        `json:"runtime_version"`
	Started     time.Time     `json:"started"`
	Finished    time.Time     `json:"finished,omitzero"`
	Machine     hw.Snapshot   `json:"machine"`
	Options     InfraOptions  `json:"options"`
	Feasibility []Feasibility `json:"feasibility"`
	Candidates  []Candidate   `json:"candidates"`
	Sustained   *Sustained    `json:"sustained,omitempty"`
	Recommended *Candidate    `json:"recommended,omitempty"`
	Notes       []string      `json:"notes,omitempty"`
}

// Feasibility records the minimum n_cpu_moe that loads for a ctx/cache type.
type Feasibility struct {
	CtxSize    int    `json:"ctx_size"`
	CacheType  string `json:"cache_type"`
	UBatch     int    `json:"ubatch"`
	MinNCPUMoE int    `json:"min_n_cpu_moe"` // -1: infeasible even with all experts on CPU
	Probes     []int  `json:"probes"`
}

// Candidate is one measured server configuration.
type Candidate struct {
	CtxSize      int           `json:"ctx_size"`
	CacheType    string        `json:"cache_type"`
	UBatch       int           `json:"ubatch"`
	NCPUMoE      int           `json:"n_cpu_moe"`
	LoadSeconds  float64       `json:"load_seconds"`
	VRAMUsedMiB  int           `json:"vram_used_mib"`
	VRAMFreeMiB  int           `json:"vram_free_mib"`
	ServerRSSMiB int           `json:"server_rss_mib"`
	Runs         []PromptRun   `json:"runs"`
	Error        string        `json:"error,omitempty"`
	Args         []string      `json:"args"`
	Score        float64       `json:"score"`
	DecodeTPS    float64       `json:"decode_tps_median"`
	PromptTPS    float64       `json:"prompt_tps_median"`
	Elapsed      time.Duration `json:"elapsed_ns"`
}

// PromptRun is one request measurement.
type PromptRun struct {
	TargetTokens int     `json:"target_tokens"`
	Cached       bool    `json:"cached"`
	PromptN      int     `json:"prompt_n"`
	CacheN       int     `json:"cache_n"`
	PromptMS     float64 `json:"prompt_ms"`
	PromptTPS    float64 `json:"prompt_tps"`
	PredictedN   int     `json:"predicted_n"`
	DecodeTPS    float64 `json:"decode_tps"`
	WallMS       float64 `json:"wall_ms"`
	Error        string  `json:"error,omitempty"`
}

// Sustained is a thermal stability run.
type Sustained struct {
	Duration time.Duration `json:"duration_ns"`
	Requests int           `json:"requests"`
	Samples  []ThermSample `json:"samples"`
	// FirstMinTPS and LastMinTPS compare decode speed at start and end.
	FirstTPS float64 `json:"first_quarter_decode_tps"`
	LastTPS  float64 `json:"last_quarter_decode_tps"`
	MaxTempC int     `json:"max_temp_c"`
}

// ThermSample is one periodic reading during the sustained run.
type ThermSample struct {
	T          float64 `json:"t_s"`
	DecodeTPS  float64 `json:"decode_tps"`
	TempC      int     `json:"temp_c"`
	PowerW     float64 `json:"power_w"`
	SMClockMHz int     `json:"sm_clock_mhz"`
	Throttle   string  `json:"throttle"`
}

// InfraRunner executes the benchmark against a llama.cpp manager.
type InfraRunner struct {
	Mgr *llamacpp.Manager
	Log *slog.Logger
	// Progress receives human-readable progress lines (may be nil).
	Progress func(string)
}

func (r *InfraRunner) progress(format string, args ...any) {
	if r.Progress != nil {
		r.Progress(fmt.Sprintf(format, args...))
	}
}

// Run executes the benchmark. It stops any managed server first and leaves
// none running. Partial results are written to OutPath after each step.
func (r *InfraRunner) Run(ctx context.Context, opt InfraOptions) (*InfraReport, error) {
	ver, _ := llamacpp.Version(ctx, r.Mgr.Binary)
	rep := &InfraReport{
		ID: "infra-" + time.Now().UTC().Format("20060102T150405Z"), Kind: "infra",
		Model: opt.Profile.Name, ModelFile: filepath.Base(opt.Profile.File),
		Runtime: "llama.cpp", RuntimeVer: ver, Started: time.Now().UTC(), Options: opt,
	}
	if err := r.Mgr.Stop(ctx); err != nil {
		return rep, err
	}
	rep.Machine = hw.Probe(ctx)
	save := func() {
		if opt.OutPath == "" {
			return
		}
		b, err := json.MarshalIndent(rep, "", "  ")
		if err == nil {
			err = config.WriteFileAtomic(opt.OutPath, b, 0o644)
		}
		if err != nil {
			r.Log.Error("save partial report", "err", err)
		}
	}
	defer func() { _ = r.Mgr.Stop(context.WithoutCancel(ctx)) }()

	if len(opt.Only) > 0 {
		for _, cf := range opt.Only {
			rep.Candidates = append(rep.Candidates, r.measure(ctx, opt, cf.CtxSize, cf.CacheType, cf.UBatch, cf.NCPUMoE))
			save()
			if ctx.Err() != nil {
				return rep, ctx.Err()
			}
		}
		rep.Recommended = recommend(rep.Candidates, opt.MinFreeVRAMMiB)
		if rep.Recommended != nil && opt.Sustained > 0 {
			s, err := r.sustained(ctx, opt, *rep.Recommended)
			if err != nil {
				rep.Notes = append(rep.Notes, "sustained run failed: "+err.Error())
			}
			rep.Sustained = s
		}
		rep.Finished = time.Now().UTC()
		save()
		return rep, nil
	}
	ub0 := opt.UBatches[0]
	for _, ctxSize := range opt.CtxSizes {
		for _, ct := range opt.CacheTypes {
			f, err := r.findMinMoE(ctx, opt, ctxSize, ct, ub0)
			if err != nil {
				return rep, err
			}
			rep.Feasibility = append(rep.Feasibility, f)
			save()
			if f.MinNCPUMoE < 0 {
				rep.Notes = append(rep.Notes, fmt.Sprintf("ctx %d with %s KV does not fit", ctxSize, ct))
				continue
			}
			for n := f.MinNCPUMoE; n <= min(f.MinNCPUMoE+opt.MoESpan, opt.MoEMax); n += 2 {
				for _, ub := range opt.UBatches {
					nn := n
					if ub != ub0 {
						// Larger ubatches need larger compute buffers; re-probe.
						nn, err = r.minMoEFrom(ctx, opt, ctxSize, ct, ub, n)
						if err != nil {
							return rep, err
						}
						if nn < 0 || nn > n+1 {
							continue // already covered by a larger n
						}
					}
					c := r.measure(ctx, opt, ctxSize, ct, ub, nn)
					rep.Candidates = append(rep.Candidates, c)
					save()
					if ctx.Err() != nil {
						return rep, ctx.Err()
					}
				}
			}
		}
	}
	rep.Recommended = recommend(rep.Candidates, opt.MinFreeVRAMMiB)
	save()
	if rep.Recommended != nil && opt.Sustained > 0 {
		s, err := r.sustained(ctx, opt, *rep.Recommended)
		if err != nil {
			rep.Notes = append(rep.Notes, "sustained run failed: "+err.Error())
		}
		rep.Sustained = s
	}
	rep.Finished = time.Now().UTC()
	save()
	return rep, nil
}

func (r *InfraRunner) profileFor(opt InfraOptions, ctxSize int, ct string, ub, ncmoe int) model.Profile {
	p := opt.Profile
	p.Server.CtxSize = ctxSize
	p.Server.CacheTypeK, p.Server.CacheTypeV = ct, ct
	p.Server.UBatchSize = ub
	if p.Server.BatchSize < ub {
		p.Server.BatchSize = ub
	}
	p.Server.NCPUMoE = ncmoe
	if p.Server.Parallel == 0 {
		p.Server.Parallel = 1
	}
	return p
}

// tryLoad starts the server and reports whether it became healthy.
func (r *InfraRunner) tryLoad(ctx context.Context, p model.Profile) (time.Duration, error) {
	_ = r.Mgr.Stop(ctx)
	args := llamacpp.BuildArgs(p, p.ResolveFile(r.Mgr.ModelsDir), r.Mgr.Host, r.Mgr.Port)
	return r.Mgr.Start(ctx, p, args)
}

func isOOM(err error) bool {
	return err != nil && strings.Contains(err.Error(), "out of memory")
}

// findMinMoE binary-searches the smallest n_cpu_moe in [0, MoEMax] that loads.
// Feasibility is monotonic: more experts on CPU never needs more VRAM.
func (r *InfraRunner) findMinMoE(ctx context.Context, opt InfraOptions, ctxSize int, ct string, ub int) (Feasibility, error) {
	f := Feasibility{CtxSize: ctxSize, CacheType: ct, UBatch: ub, MinNCPUMoE: -1}
	lo, hi := 0, opt.MoEMax
	r.progress("feasibility: ctx=%d kv=%s ub=%d: searching n_cpu_moe in [%d,%d]", ctxSize, ct, ub, lo, hi)
	for lo <= hi {
		mid := (lo + hi) / 2
		f.Probes = append(f.Probes, mid)
		_, err := r.tryLoad(ctx, r.profileFor(opt, ctxSize, ct, ub, mid))
		switch {
		case err == nil:
			f.MinNCPUMoE = mid
			hi = mid - 1
		case isOOM(err):
			lo = mid + 1
		case ctx.Err() != nil:
			return f, ctx.Err()
		default:
			return f, fmt.Errorf("probe n_cpu_moe=%d: %w", mid, err)
		}
	}
	_ = r.Mgr.Stop(ctx)
	r.progress("feasibility: ctx=%d kv=%s ub=%d: min n_cpu_moe=%d (probes %v)", ctxSize, ct, ub, f.MinNCPUMoE, f.Probes)
	return f, nil
}

// minMoEFrom finds the smallest feasible n_cpu_moe >= start (linear, small range).
func (r *InfraRunner) minMoEFrom(ctx context.Context, opt InfraOptions, ctxSize int, ct string, ub, start int) (int, error) {
	for n := start; n <= opt.MoEMax; n++ {
		_, err := r.tryLoad(ctx, r.profileFor(opt, ctxSize, ct, ub, n))
		if err == nil {
			return n, nil
		}
		if !isOOM(err) {
			return -1, err
		}
	}
	return -1, nil
}

func (r *InfraRunner) measure(ctx context.Context, opt InfraOptions, ctxSize int, ct string, ub, ncmoe int) Candidate {
	t0 := time.Now()
	p := r.profileFor(opt, ctxSize, ct, ub, ncmoe)
	c := Candidate{CtxSize: ctxSize, CacheType: ct, UBatch: ub, NCPUMoE: ncmoe,
		Args: llamacpp.BuildArgs(p, p.ResolveFile(r.Mgr.ModelsDir), r.Mgr.Host, r.Mgr.Port)}
	r.progress("measure: ctx=%d kv=%s ub=%d n_cpu_moe=%d", ctxSize, ct, ub, ncmoe)
	_ = r.Mgr.Stop(ctx)
	if opt.ColdLoad {
		if err := EvictFromPageCache(p.ResolveFile(r.Mgr.ModelsDir)); err != nil {
			r.progress("  page-cache eviction failed: %v", err)
		}
	}
	load, err := r.Mgr.Start(ctx, p, c.Args)
	if err != nil {
		c.Error = err.Error()
		return c
	}
	c.LoadSeconds = load.Seconds()
	if gpus, err := hw.ProbeGPUs(ctx); err == nil && len(gpus) > 0 {
		c.VRAMUsedMiB, c.VRAMFreeMiB = gpus[0].MemUsedMiB, gpus[0].MemTotalMiB-gpus[0].MemUsedMiB
	}
	if st, err := r.Mgr.Status(ctx); err == nil {
		c.ServerRSSMiB = st.RSSMiB
	}
	client := inference.NewClient("http://"+r.Mgr.Host+":"+fmt.Sprint(r.Mgr.Port), 30*time.Minute)
	charsPerTok := calibrate(ctx, client)
	for _, size := range opt.PromptSizes {
		if size+opt.DecodeTokens+64 > ctxSize {
			continue
		}
		seed := fmt.Sprintf("%d-%d", time.Now().UnixNano(), size)
		text := fillerText(int(float64(size)*charsPerTok), seed)
		msgs := benchMessages(text)
		cold, reply := runPrompt(ctx, client, opt.Profile.Name, msgs, opt.DecodeTokens)
		cold.TargetTokens = size
		c.Runs = append(c.Runs, cold)
		r.progress("  %6d tok cold: pp %.0f t/s, tg %.1f t/s", cold.PromptN, cold.PromptTPS, cold.DecodeTPS)
		// Agent turns append to the conversation: previous messages + the
		// assistant reply + a new observation. This measures prompt-cache
		// reuse for that pattern (hybrid recurrent models can only roll back
		// to checkpoints, so mid-prompt edits would not be representative).
		follow := slices.Concat(msgs, []inference.Message{
			{Role: "assistant", Content: reply},
			{Role: "user", Content: "Observation: go test ./... failed in Handle3 with a nil pointer. What is the fix?"}})
		warm, _ := runPrompt(ctx, client, opt.Profile.Name, follow, opt.DecodeTokens)
		warm.TargetTokens, warm.Cached = size, true
		c.Runs = append(c.Runs, warm)
		r.progress("  %6d tok warm: cache_n %d, new %d, prompt %.0f ms, tg %.1f t/s", warm.PromptN+warm.CacheN, warm.CacheN, warm.PromptN, warm.PromptMS, warm.DecodeTPS)
		if ctx.Err() != nil {
			break
		}
	}
	c.DecodeTPS, c.PromptTPS = medians(c.Runs)
	c.Score = c.DecodeTPS
	c.Elapsed = time.Since(t0)
	return c
}

func calibrate(ctx context.Context, c *inference.Client) float64 {
	sample := fillerText(20000, "calibration")
	n, err := c.Tokenize(ctx, sample)
	if err != nil || n == 0 {
		return 3.2
	}
	return float64(len(sample)) / float64(n)
}

func benchMessages(text string) []inference.Message {
	return []inference.Message{
		{Role: "system", Content: "You are a code reviewer."},
		{Role: "user", Content: text + "\n\nList three risks in the code above."},
	}
}

func runPrompt(ctx context.Context, c *inference.Client, modelName string, msgs []inference.Message, decode int) (PromptRun, string) {
	temp := 0.0
	seed := 1
	req := inference.ChatRequest{
		Model: modelName, Messages: msgs,
		MaxTokens: decode, Temperature: &temp, Seed: &seed, IgnoreEOS: true,
		ChatTemplateKwargs: map[string]bool{"enable_thinking": false},
	}
	resp, err := c.Chat(ctx, req)
	if err != nil {
		return PromptRun{Error: err.Error()}, ""
	}
	pr := PromptRun{WallMS: resp.WallMS}
	if t := resp.Timings; t != nil {
		pr.PromptN, pr.CacheN, pr.PromptMS, pr.PromptTPS = t.PromptN, t.CacheN, t.PromptMS, t.PromptPerSecond
		pr.PredictedN, pr.DecodeTPS = t.PredictedN, t.PredictedPerSecond
	}
	return pr, resp.Text()
}

func medians(runs []PromptRun) (decode, prompt float64) {
	var d, p []float64
	for _, r := range runs {
		if r.Error != "" {
			continue
		}
		if r.DecodeTPS > 0 {
			d = append(d, r.DecodeTPS)
		}
		if !r.Cached && r.PromptTPS > 0 {
			p = append(p, r.PromptTPS)
		}
	}
	return median(d), median(p)
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

// recommend picks, among error-free candidates at the largest context that
// keeps MinFreeVRAMMiB headroom, the one with the best decode throughput;
// prompt throughput breaks near-ties (within 3%).
func recommend(cands []Candidate, minFree int) *Candidate {
	var ok []Candidate
	maxCtx := 0
	for _, c := range cands {
		if c.Error == "" && c.DecodeTPS > 0 && c.VRAMFreeMiB >= minFree {
			ok = append(ok, c)
			maxCtx = max(maxCtx, c.CtxSize)
		}
	}
	var best *Candidate
	for i := range ok {
		c := &ok[i]
		if c.CtxSize != maxCtx {
			continue
		}
		if best == nil || c.DecodeTPS > best.DecodeTPS*1.03 ||
			(c.DecodeTPS >= best.DecodeTPS*0.97 && c.PromptTPS > best.PromptTPS) {
			best = c
		}
	}
	return best
}

func (r *InfraRunner) sustained(ctx context.Context, opt InfraOptions, c Candidate) (*Sustained, error) {
	p := r.profileFor(opt, c.CtxSize, c.CacheType, c.UBatch, c.NCPUMoE)
	r.progress("sustained: %s on ctx=%d n_cpu_moe=%d", opt.Sustained, c.CtxSize, c.NCPUMoE)
	if _, err := r.tryLoad(ctx, p); err != nil {
		return nil, err
	}
	client := inference.NewClient("http://"+r.Mgr.Host+":"+fmt.Sprint(r.Mgr.Port), 10*time.Minute)
	s := &Sustained{Duration: opt.Sustained}
	text := fillerText(int(4000*calibrate(ctx, client)), "sustained")
	start := time.Now()
	for time.Since(start) < opt.Sustained {
		run, _ := runPrompt(ctx, client, opt.Profile.Name, benchMessages(text), 256)
		if run.Error != "" {
			return s, errors.New(run.Error)
		}
		s.Requests++
		smp := ThermSample{T: time.Since(start).Seconds(), DecodeTPS: run.DecodeTPS}
		if gpus, err := hw.ProbeGPUs(ctx); err == nil && len(gpus) > 0 {
			g := gpus[0]
			smp.TempC, smp.PowerW, smp.SMClockMHz, smp.Throttle = g.TempC, g.PowerW, g.SMClockMHz, g.ThrottleFlags
			s.MaxTempC = max(s.MaxTempC, g.TempC)
		}
		s.Samples = append(s.Samples, smp)
		if ctx.Err() != nil {
			return s, ctx.Err()
		}
	}
	q := max(1, len(s.Samples)/4)
	var first, last []float64
	for i, smp := range s.Samples {
		if i < q {
			first = append(first, smp.DecodeTPS)
		}
		if i >= len(s.Samples)-q {
			last = append(last, smp.DecodeTPS)
		}
	}
	s.FirstTPS, s.LastTPS = median(first), median(last)
	return s, nil
}

// LoadInfraReport reads a persisted report.
func LoadInfraReport(path string) (*InfraReport, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r InfraReport
	return &r, json.Unmarshal(b, &r)
}
