package benchmark

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/hw"
	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/inference/llamacpp"
	"github.com/akynte/boundedcode/internal/model"
)

// SpecBench measures speculative decoding on one model: each
// configuration loads the server alone and answers the same prompts, which
// stand for what the agent generates (a file rewritten with a small change,
// new code, an explanation, and reasoning with thinking on). Generation is
// greedy, so each configuration should produce the baseline's answer; the
// report says where it did not.
type SpecBench struct {
	Mgr      *llamacpp.Manager
	Profile  model.Profile
	Configs  []SpecConfig
	Repeats  int
	Progress func(string)
}

// SpecConfig is one way to serve the model.
type SpecConfig struct {
	Name string `json:"name"`
	// Spec is the speculative decoding (nil: none, the baseline).
	Spec *model.Speculative `json:"speculative,omitempty"`
	// File replaces the profile's weights (the MTP build of a model).
	File string `json:"file,omitempty"`
	// NCPUMoE replaces the profile's n_cpu_moe (a draft model or MTP
	// layers need VRAM; 0 keeps it).
	NCPUMoE int `json:"n_cpu_moe,omitempty"`
}

// SpecPrompt is one prompt.
type SpecPrompt struct {
	ID       string
	Messages []inference.Message
	Thinking bool
	MaxTok   int
}

// SpecRun is one prompt's answer under one configuration.
type SpecRun struct {
	Prompt    string  `json:"prompt"`
	Repeat    int     `json:"repeat"`
	Generated int     `json:"generated_tokens"`
	DecodeTPS float64 `json:"decode_tps"`
	DecodeMS  float64 `json:"decode_ms"`
	PromptMS  float64 `json:"prompt_ms"`
	WallMS    float64 `json:"wall_ms"`
	Drafted   int     `json:"drafted_tokens"`
	Accepted  int     `json:"accepted_tokens"`
	// Same: the answer equals the baseline's for this prompt (greedy).
	Same  bool   `json:"same_as_baseline"`
	Text  string `json:"-"`
	Error string `json:"error,omitempty"`
}

// SpecResult is one configuration's.
type SpecResult struct {
	Config       SpecConfig `json:"config"`
	Args         []string   `json:"args"`
	LoadSeconds  float64    `json:"load_seconds"`
	ServerRSSMiB float64    `json:"server_rss_mib"`
	PeakVRAMMiB  float64    `json:"peak_vram_mib"`
	PeakRSSMiB   float64    `json:"peak_server_rss_mib"`
	Runs         []SpecRun  `json:"runs"`
	Error        string     `json:"error,omitempty"`
}

// SpecReport is the bench's.
type SpecReport struct {
	Started time.Time    `json:"started"`
	Model   string       `json:"model"`
	Runtime string       `json:"runtime"`
	Machine hw.Snapshot  `json:"machine"`
	Results []SpecResult `json:"results"`
}

// SpecPrompts are the prompts, built from a Go source file of the
// repository (the rewrite prompt's input).
func SpecPrompts(source string) []SpecPrompt {
	sys := inference.Message{Role: "system", Content: "You are a coding agent. Answer with code only when asked for code."}
	return []SpecPrompt{
		{ID: "rewrite", MaxTok: 3000, Messages: []inference.Message{sys, {Role: "user", Content: "Here is a file:\n\n```go\n" + source +
			"\n```\n\nRewrite the whole file with every exported function's doc comment ending in a full stop, and nothing else changed. Output only the file in one code block."}}},
		{ID: "new_code", MaxTok: 800, Messages: []inference.Message{sys, {Role: "user", Content: "Write a Go package `ratelimit` with a token-bucket limiter: New(rate, burst), Allow() bool, Wait(ctx) error, safe for concurrent use. Code only."}}},
		{ID: "explain", MaxTok: 600, Messages: []inference.Message{sys, {Role: "user", Content: "Here is a file:\n\n```go\n" + source + "\n```\n\nExplain in prose what this file does and what could go wrong in it."}}},
		{ID: "reasoning", MaxTok: 2500, Thinking: true, Messages: []inference.Message{sys, {Role: "user", Content: "A Go HTTP handler stores an idempotency key with `if _, ok := m[key]; !ok { m[key] = id; publish(id) }` under no lock, and two concurrent requests with the same key both publish. Explain the race and give the fix."}}},
	}
}

// Run measures every configuration; the first is the baseline the others'
// answers are compared with.
func (b *SpecBench) Run(ctx context.Context, prompts []SpecPrompt) (*SpecReport, error) {
	ver, _ := llamacpp.Version(ctx, b.Mgr.Binary)
	rep := &SpecReport{Started: time.Now().UTC(), Model: b.Profile.Name, Runtime: ver, Machine: hw.Probe(ctx)}
	defer func() { _ = b.Mgr.Stop(context.WithoutCancel(ctx)) }()
	baseline := map[string]string{}
	for i, cfg := range b.Configs {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		res := b.runConfig(ctx, cfg, prompts, baseline, i == 0)
		rep.Results = append(rep.Results, res)
		b.progress("%s: %s", cfg.Name, specLine(res))
	}
	return rep, nil
}

func (b *SpecBench) progress(format string, args ...any) {
	if b.Progress != nil {
		b.Progress(fmt.Sprintf(format, args...))
	}
}

func (b *SpecBench) runConfig(ctx context.Context, cfg SpecConfig, prompts []SpecPrompt, baseline map[string]string, isBase bool) SpecResult {
	p := b.Profile
	if cfg.File != "" {
		p.File = cfg.File
	}
	p.Server.Speculative = cfg.Spec
	if cfg.NCPUMoE > 0 {
		p.Server.NCPUMoE = cfg.NCPUMoE
	}
	res := SpecResult{Config: cfg}
	path := p.ResolveFile(b.Mgr.ModelsDir)
	if _, err := os.Stat(path); err != nil {
		res.Error = "weights: " + err.Error()
		return res
	}
	if d := p.ResolveDraft(b.Mgr.ModelsDir); d != "" {
		if _, err := os.Stat(d); err != nil {
			res.Error = "draft: " + err.Error()
			return res
		}
	}
	res.Args = b.Mgr.ServerArgs(p, path)
	_ = b.Mgr.Stop(ctx)
	load, err := b.Mgr.Start(ctx, p, res.Args)
	if err != nil {
		res.Error = "load: " + err.Error()
		return res
	}
	res.LoadSeconds = load.Seconds()
	if st, err := b.Mgr.Status(ctx); err == nil {
		res.ServerRSSMiB = float64(st.RSSMiB)
	}
	peaks := sampleResources(ctx, false)
	c := inference.NewClient("http://"+b.Mgr.Host+":"+fmt.Sprint(b.Mgr.Port), 20*time.Minute)
	for rep := 1; rep <= max(b.Repeats, 1); rep++ {
		for _, pr := range prompts {
			run := specAsk(ctx, c, p.Name, pr)
			run.Repeat = rep
			if isBase && rep == 1 {
				baseline[pr.ID] = run.Text
			}
			run.Same = run.Error == "" && run.Text == baseline[pr.ID]
			res.Runs = append(res.Runs, run)
			if ctx.Err() != nil {
				break
			}
		}
	}
	pk := peaks()
	res.PeakVRAMMiB, res.PeakRSSMiB = pk.GPUMemMiB, pk.ServerRSSMiB
	return res
}

func specAsk(ctx context.Context, c *inference.Client, modelName string, pr SpecPrompt) SpecRun {
	temp, seed := 0.0, 7
	req := inference.ChatRequest{Model: modelName, Messages: pr.Messages, MaxTokens: pr.MaxTok, Temperature: &temp, Seed: &seed,
		ChatTemplateKwargs: map[string]bool{"enable_thinking": pr.Thinking}}
	cache := false
	req.CachePrompt = &cache // every repeat processes its prompt
	resp, err := c.Chat(ctx, req)
	run := SpecRun{Prompt: pr.ID}
	if err != nil {
		run.Error = err.Error()
		return run
	}
	run.WallMS = resp.WallMS
	run.Text = resp.Text()
	if len(resp.Choices) > 0 {
		run.Text = resp.Choices[0].Message.ReasoningContent + "\x00" + run.Text
	}
	if t := resp.Timings; t != nil {
		run.Generated, run.DecodeTPS, run.DecodeMS, run.PromptMS = t.PredictedN, t.PredictedPerSecond, t.PredictedMS, t.PromptMS
		run.Drafted, run.Accepted = t.DraftN, t.DraftNAccepted
	}
	return run
}

// SpecSummary is one configuration's figures for one prompt (or all, "").
type SpecSummary struct {
	Prompt    string
	DecodeTPS float64 // generated tokens over decode time, over the runs
	PromptMS  float64 // mean
	WallMS    float64 // mean
	Accept    float64 // accepted / drafted
	Drafted   int
	Same      int
	Runs      int
	Errors    int
}

// Summarize returns res's figures per prompt and over all prompts.
func (res SpecResult) Summarize() []SpecSummary {
	by := map[string]*SpecSummary{}
	var order []string
	gen, ms := map[string]float64{}, map[string]float64{}
	add := func(key string, r SpecRun) {
		s := by[key]
		if s == nil {
			s = &SpecSummary{Prompt: key}
			by[key] = s
			order = append(order, key)
		}
		s.Runs++
		if r.Error != "" {
			s.Errors++
			return
		}
		gen[key] += float64(r.Generated)
		ms[key] += r.DecodeMS
		s.PromptMS += r.PromptMS
		s.WallMS += r.WallMS
		s.Drafted += r.Drafted
		s.Accept += float64(r.Accepted)
		if r.Same {
			s.Same++
		}
	}
	for _, r := range res.Runs {
		add(r.Prompt, r)
		add("", r)
	}
	var out []SpecSummary
	for _, k := range order {
		s := by[k]
		ok := float64(s.Runs - s.Errors)
		if ms[k] > 0 {
			s.DecodeTPS = gen[k] / (ms[k] / 1000)
		}
		if ok > 0 {
			s.PromptMS /= ok
			s.WallMS /= ok
		}
		if s.Drafted > 0 {
			s.Accept /= float64(s.Drafted)
		} else {
			s.Accept = 0
		}
		out = append(out, *s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Prompt != "" && out[j].Prompt == "" })
	return out
}

func specLine(r SpecResult) string {
	if r.Error != "" {
		return "error: " + r.Error
	}
	var parts []string
	for _, s := range r.Summarize() {
		name := s.Prompt
		if name == "" {
			name = "all"
		}
		parts = append(parts, fmt.Sprintf("%s %.1f t/s (accept %.0f%%, same %d/%d)", name, s.DecodeTPS, 100*s.Accept, s.Same, s.Runs))
	}
	return strings.Join(parts, "; ") + fmt.Sprintf("; load %.1f s, peak VRAM %.0f MiB", r.LoadSeconds, r.PeakVRAMMiB)
}

// WriteSpecMarkdown writes the report's tables.
func WriteSpecMarkdown(w io.Writer, r *SpecReport) error {
	fmt.Fprintf(w, "# Speculative decoding: %s\n\nStarted %s; llama.cpp %s; greedy decoding, prompt cache off.\n\n", r.Model, r.Started.Format(time.RFC3339), r.Runtime)
	fmt.Fprintln(w, "| Configuration | Prompt | Decode t/s | vs. baseline | Accepted | Prompt ms | Wall ms | Same answer | Load s | Peak VRAM MiB | Peak server RSS MiB |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|---|---|---|")
	base := map[string]float64{}
	for i, res := range r.Results {
		if res.Error != "" {
			fmt.Fprintf(w, "| %s | | | | | | | | | | error: %s |\n", res.Config.Name, strings.ReplaceAll(res.Error, "|", "/"))
			continue
		}
		for _, s := range res.Summarize() {
			name := s.Prompt
			if name == "" {
				name = "**all**"
			}
			if i == 0 {
				base[s.Prompt] = s.DecodeTPS
			}
			ratio := ""
			if b := base[s.Prompt]; b > 0 {
				ratio = fmt.Sprintf("%.2f×", s.DecodeTPS/b)
			}
			acc := "–"
			if s.Drafted > 0 {
				acc = fmt.Sprintf("%.0f%% of %d", 100*s.Accept, s.Drafted)
			}
			fmt.Fprintf(w, "| %s | %s | %.1f | %s | %s | %.0f | %.0f | %d/%d | %.1f | %.0f | %.0f |\n", res.Config.Name, name, s.DecodeTPS, ratio, acc,
				s.PromptMS, s.WallMS, s.Same, s.Runs, res.LoadSeconds, res.PeakVRAMMiB, res.PeakRSSMiB)
		}
	}
	return nil
}
