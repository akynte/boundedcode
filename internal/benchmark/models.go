package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/hw"
	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/inference/llamacpp"
	"github.com/akynte/boundedcode/internal/model"
)

// ModelProbe checks what local models do with BoundedCode's own requests:
// tool calls and their results, tool arguments holding code, answers
// constrained to a JSON schema, thinking turned off per request, a fact
// recalled from a long prompt, and decode and prompt speed. A model card or
// a leaderboard says what a model can do in its vendor's harness; this
// says what it does here, with this runtime and these requests.
type ModelProbe struct {
	Mgr      *llamacpp.Manager
	Progress func(string)
	// LongTokens is the long-context check's prompt size (0 = 32768, never
	// more than half the profile's context).
	LongTokens int
}

// ProbeCheck is one check's outcome.
type ProbeCheck struct {
	Name    string  `json:"name"`
	Pass    bool    `json:"pass"`
	Detail  string  `json:"detail,omitempty"`
	Seconds float64 `json:"seconds"`
}

// ProbeResult is one model's.
type ProbeResult struct {
	Profile  string             `json:"profile"`
	File     string             `json:"file"`
	Fit      model.Fit          `json:"fit"`
	Declared model.Capabilities `json:"declared"`
	Weights  *model.GGUFMeta    `json:"weights,omitempty"`
	// Disagreements are declared capabilities the weights contradict.
	Disagreements []string     `json:"disagreements,omitempty"`
	LoadSeconds   float64      `json:"load_seconds"`
	ServerRSSMiB  float64      `json:"server_rss_mib,omitempty"`
	VRAMUsedMiB   float64      `json:"vram_used_mib,omitempty"`
	Checks        []ProbeCheck `json:"checks"`
	// Thinking is the measured thinking capability: "toggle" (reasons by
	// default and not when told), "always", "none" or "" (not measured).
	Thinking  string  `json:"thinking,omitempty"`
	DecodeTPS float64 `json:"decode_tps,omitempty"`
	PromptTPS float64 `json:"prompt_tps,omitempty"`
	Error     string  `json:"error,omitempty"`
}

// ProbeReport is the run's.
type ProbeReport struct {
	Started  time.Time     `json:"started"`
	Runtime  string        `json:"runtime"`
	Machine  hw.Snapshot   `json:"machine"`
	Profiles []ProbeResult `json:"profiles"`
}

// Run probes each profile in turn, loading each alone, and stops the
// server at the end.
func (b *ModelProbe) Run(ctx context.Context, profiles []model.Profile) (*ProbeReport, error) {
	ver, _ := llamacpp.Version(ctx, b.Mgr.Binary)
	rep := &ProbeReport{Started: time.Now().UTC(), Runtime: ver, Machine: hw.Probe(ctx)}
	defer func() { _ = b.Mgr.Stop(context.WithoutCancel(ctx)) }()
	for _, p := range profiles {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		res := b.probe(ctx, p, rep.Machine)
		rep.Profiles = append(rep.Profiles, res)
		b.progress("%s: %s", p.Name, probeLine(res))
	}
	return rep, nil
}

func (b *ModelProbe) progress(format string, args ...any) {
	if b.Progress != nil {
		b.Progress(fmt.Sprintf(format, args...))
	}
}

func probeLine(r ProbeResult) string {
	if r.Error != "" {
		return "error: " + r.Error
	}
	var parts []string
	for _, c := range r.Checks {
		mark := "pass"
		if !c.Pass {
			mark = "FAIL"
		}
		parts = append(parts, c.Name+" "+mark)
	}
	return fmt.Sprintf("%s; thinking %s; %.1f t/s decode, %.0f t/s prompt; load %.1f s", strings.Join(parts, ", "), r.Thinking, r.DecodeTPS, r.PromptTPS, r.LoadSeconds)
}

func (b *ModelProbe) probe(ctx context.Context, p model.Profile, machine hw.Snapshot) ProbeResult {
	path := p.ResolveFile(b.Mgr.ModelsDir)
	res := ProbeResult{Profile: p.Name, File: filepath.Base(path), Fit: model.FitFor(p, machine), Declared: p.Capabilities}
	if _, err := os.Stat(path); err != nil {
		res.Error = "weights not downloaded"
		return res
	}
	if m, err := model.ReadGGUFMeta(path); err == nil {
		res.Weights = &m
		res.Disagreements = model.CheckDeclared(p.Capabilities, m)
	}
	_ = b.Mgr.Stop(ctx)
	load, err := b.Mgr.Start(ctx, p, b.Mgr.ServerArgs(p, path))
	if err != nil {
		res.Error = "load: " + err.Error()
		return res
	}
	res.LoadSeconds = load.Seconds()
	c := inference.NewClient("http://"+b.Mgr.Host+":"+fmt.Sprint(b.Mgr.Port), 10*time.Minute)
	pr := prober{c: c, model: p.Name}
	res.Checks = append(res.Checks, pr.toolCall(ctx), pr.toolResult(ctx), pr.codeArguments(ctx), pr.structured(ctx))
	think, thinkOn, thinkOff := pr.thinking(ctx)
	res.Thinking = think
	res.Checks = append(res.Checks, thinkOn, thinkOff)
	long := b.LongTokens
	if long <= 0 {
		long = 32768
	}
	if p.Server.CtxSize > 0 {
		long = min(long, p.Server.CtxSize/2)
	}
	lc, ptps := pr.longContext(ctx, long)
	res.Checks = append(res.Checks, lc)
	res.PromptTPS = ptps
	res.DecodeTPS = pr.decode(ctx)
	if st, err := b.Mgr.Status(ctx); err == nil {
		res.ServerRSSMiB = float64(st.RSSMiB)
	}
	if gpus, err := hw.ProbeGPUs(ctx); err == nil && len(gpus) > 0 {
		res.VRAMUsedMiB = float64(gpus[0].MemUsedMiB)
	}
	return res
}

type prober struct {
	c     *inference.Client
	model string
}

type probeMessage struct {
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content"`
	ToolCalls        []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

type probeAnswer struct {
	Message probeMessage
	Timings *inference.Timings
	Seconds float64
}

func (p prober) ask(ctx context.Context, body map[string]any) (probeAnswer, error) {
	body["model"] = p.model
	if _, ok := body["temperature"]; !ok {
		body["temperature"] = 0
	}
	raw, _ := json.Marshal(body)
	out, d, err := p.c.Raw(ctx, "/v1/chat/completions", raw)
	if err != nil {
		return probeAnswer{}, err
	}
	var resp struct {
		Choices []struct {
			Message probeMessage `json:"message"`
		} `json:"choices"`
		Timings *inference.Timings `json:"timings"`
		Error   *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return probeAnswer{}, fmt.Errorf("unreadable answer: %.200s", out)
	}
	if resp.Error != nil {
		return probeAnswer{}, fmt.Errorf("%s", resp.Error.Message)
	}
	if len(resp.Choices) == 0 {
		return probeAnswer{}, fmt.Errorf("no choices")
	}
	return probeAnswer{Message: resp.Choices[0].Message, Timings: resp.Timings, Seconds: d.Seconds()}, nil
}

var probeTools = []map[string]any{
	{"type": "function", "function": map[string]any{"name": "read_file", "description": "Read a file of the project.",
		"parameters": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"}}}},
	{"type": "function", "function": map[string]any{"name": "edit_file", "description": "Replace old text with new text in a file.",
		"parameters": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"},
			"old": map[string]any{"type": "string"}, "new": map[string]any{"type": "string"}}, "required": []string{"path", "old", "new"}}}},
}

const probeSystem = "You are a coding agent working in a Go repository. Use the tools to act; do not describe what you would do."

func check(name string, start time.Time, pass bool, detail string) ProbeCheck {
	return ProbeCheck{Name: name, Pass: pass, Detail: trunc1(detail, 300), Seconds: time.Since(start).Seconds()}
}

// toolCall: a request that needs a file read gets a read_file call with
// arguments that parse.
func (p prober) toolCall(ctx context.Context) ProbeCheck {
	t0 := time.Now()
	a, err := p.ask(ctx, map[string]any{"messages": []map[string]any{{"role": "system", "content": probeSystem},
		{"role": "user", "content": "What Go version does this module declare in go.mod?"}}, "tools": probeTools, "max_tokens": 2048})
	if err != nil {
		return check("tool_call", t0, false, err.Error())
	}
	for _, tc := range a.Message.ToolCalls {
		var args map[string]any
		if tc.Function.Name == "read_file" && json.Unmarshal([]byte(tc.Function.Arguments), &args) == nil && strings.Contains(fmt.Sprint(args["path"]), "go.mod") {
			return check("tool_call", t0, true, "read_file "+tc.Function.Arguments)
		}
	}
	return check("tool_call", t0, false, fmt.Sprintf("no read_file(go.mod) call; content %q, %d calls", a.Message.Content, len(a.Message.ToolCalls)))
}

// toolResult: given the tool's result, the model answers from it.
func (p prober) toolResult(ctx context.Context) ProbeCheck {
	t0 := time.Now()
	msgs := []map[string]any{{"role": "system", "content": probeSystem},
		{"role": "user", "content": "What Go version does this module declare in go.mod?"},
		{"role": "assistant", "content": "", "tool_calls": []map[string]any{{"id": "c1", "type": "function",
			"function": map[string]any{"name": "read_file", "arguments": `{"path":"go.mod"}`}}}},
		{"role": "tool", "tool_call_id": "c1", "content": "module example.com/ledger\n\ngo 1.23.4\n\nrequire github.com/google/uuid v1.6.0\n"}}
	a, err := p.ask(ctx, map[string]any{"messages": msgs, "tools": probeTools, "max_tokens": 2048})
	if err != nil {
		return check("tool_result", t0, false, err.Error())
	}
	return check("tool_result", t0, strings.Contains(a.Message.Content, "1.23"), a.Message.Content)
}

// codeArguments: an edit whose arguments hold code with quotes, tabs and
// newlines comes back as JSON that parses to the exact text.
func (p prober) codeArguments(ctx context.Context) ProbeCheck {
	t0 := time.Now()
	src := "package ledger\n\nfunc Label(n int) string {\n\tif n < 0 {\n\t\treturn \"negative\"\n\t}\n\treturn fmt.Sprintf(\"%d \\\"units\\\"\", n)\n}\n"
	old := "\t\treturn \"negative\""
	msgs := []map[string]any{{"role": "system", "content": probeSystem},
		{"role": "user", "content": "In label.go, make Label return \"below zero\" instead of \"negative\". Use edit_file."},
		{"role": "assistant", "content": "", "tool_calls": []map[string]any{{"id": "c1", "type": "function",
			"function": map[string]any{"name": "read_file", "arguments": `{"path":"label.go"}`}}}},
		{"role": "tool", "tool_call_id": "c1", "content": src}}
	a, err := p.ask(ctx, map[string]any{"messages": msgs, "tools": probeTools, "max_tokens": 4096})
	if err != nil {
		return check("code_arguments", t0, false, err.Error())
	}
	for _, tc := range a.Message.ToolCalls {
		var args struct{ Path, Old, New string }
		if tc.Function.Name != "edit_file" {
			continue
		}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			return check("code_arguments", t0, false, "arguments do not parse: "+tc.Function.Arguments)
		}
		ok := strings.Contains(src, args.Old) && strings.Contains(args.Old, "negative") && strings.Contains(args.New, "below zero")
		return check("code_arguments", t0, ok, fmt.Sprintf("old %q new %q (want old within the file, e.g. %q)", args.Old, args.New, old))
	}
	return check("code_arguments", t0, false, fmt.Sprintf("no edit_file call; content %q", a.Message.Content))
}

// structured: an answer constrained to a JSON schema parses and keeps to
// it.
func (p prober) structured(ctx context.Context) ProbeCheck {
	t0 := time.Now()
	schema := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"file", "line", "severity", "summary"},
		"properties": map[string]any{"file": map[string]any{"type": "string"}, "line": map[string]any{"type": "integer"},
			"severity": map[string]any{"type": "string", "enum": []string{"low", "medium", "high"}}, "summary": map[string]any{"type": "string"}}}
	a, err := p.ask(ctx, map[string]any{"messages": []map[string]any{{"role": "user", "content": "store/db.go line 42 builds an SQL query by concatenating the user's input. Report it."}},
		"response_format":      map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "finding", "strict": true, "schema": schema}},
		"chat_template_kwargs": map[string]any{"enable_thinking": false}, "max_tokens": 1500})
	if err != nil {
		return check("structured", t0, false, err.Error())
	}
	var f struct {
		File     string
		Line     *int
		Severity string
		Summary  string
	}
	dec := json.NewDecoder(strings.NewReader(a.Message.Content))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return check("structured", t0, false, "does not parse: "+a.Message.Content)
	}
	ok := f.Line != nil && *f.Line == 42 && slices.Contains([]string{"low", "medium", "high"}, f.Severity) && strings.Contains(f.File, "db.go")
	return check("structured", t0, ok, a.Message.Content)
}

// thinking: does the model reason by default, and stop when a request
// says enable_thinking false?
func (p prober) thinking(ctx context.Context) (string, ProbeCheck, ProbeCheck) {
	q := []map[string]any{{"role": "user", "content": "A function returns early when n == 0 but the caller passes len(xs)-1. For xs of length 1, what happens? One sentence."}}
	t0 := time.Now()
	on, err := p.ask(ctx, map[string]any{"messages": q, "max_tokens": 3000})
	onCheck := check("thinking_default", t0, err == nil, "")
	if err != nil {
		onCheck.Detail = err.Error()
		return "", onCheck, check("thinking_off", time.Now(), false, "not run")
	}
	reasons := strings.TrimSpace(on.Message.ReasoningContent) != ""
	onCheck.Detail = fmt.Sprintf("reasoning %d chars, answer %q", len(on.Message.ReasoningContent), trunc1(on.Message.Content, 80))
	t1 := time.Now()
	off, err := p.ask(ctx, map[string]any{"messages": q, "max_tokens": 3000, "chat_template_kwargs": map[string]any{"enable_thinking": false}})
	if err != nil {
		return "", onCheck, check("thinking_off", t1, false, err.Error())
	}
	offReasons := strings.TrimSpace(off.Message.ReasoningContent) != "" || strings.Contains(off.Message.Content, "<think>")
	offCheck := check("thinking_off", t1, !offReasons && strings.TrimSpace(off.Message.Content) != "",
		fmt.Sprintf("reasoning %d chars, answer %q", len(off.Message.ReasoningContent), trunc1(off.Message.Content, 80)))
	switch {
	case reasons && !offReasons:
		return "toggle", onCheck, offCheck
	case reasons:
		return "always", onCheck, offCheck
	default:
		return "none", onCheck, offCheck
	}
}

// longContext: a fact stated early in a long prompt is recalled; the
// prompt's processing speed is returned.
func (p prober) longContext(ctx context.Context, tokens int) (ProbeCheck, float64) {
	t0 := time.Now()
	charsPerTok := calibrate(ctx, p.c)
	filler := fillerText(int(float64(tokens)*charsPerTok), fmt.Sprintf("probe-%d", time.Now().UnixNano()))
	cut := len(filler) / 5
	text := filler[:cut] + "\n// NOTE: the deployment key for the ledger service is K7Q9-ZX31.\n" + filler[cut:]
	a, err := p.ask(ctx, map[string]any{"messages": []map[string]any{{"role": "user", "content": text + "\n\nWhat is the deployment key for the ledger service? Answer with the key only."}},
		"max_tokens": 64, "chat_template_kwargs": map[string]any{"enable_thinking": false}})
	if err != nil {
		return check("long_context", t0, false, err.Error()), 0
	}
	tps := 0.0
	detail := a.Message.Content
	if a.Timings != nil {
		tps = a.Timings.PromptPerSecond
		detail = fmt.Sprintf("%d tokens; answer %q", a.Timings.PromptN+a.Timings.CacheN, a.Message.Content)
	}
	return check("long_context", t0, strings.Contains(a.Message.Content, "K7Q9-ZX31"), detail), tps
}

// decode returns the decode speed of 256 tokens at a short context.
func (p prober) decode(ctx context.Context) float64 {
	a, err := p.ask(ctx, map[string]any{"messages": []map[string]any{{"role": "user", "content": "Write a Go function that parses an ISO 8601 duration, with tests."}},
		"max_tokens": 256, "ignore_eos": true, "chat_template_kwargs": map[string]any{"enable_thinking": false}})
	if err != nil || a.Timings == nil {
		return 0
	}
	return a.Timings.PredictedPerSecond
}

// WriteProbeMarkdown writes the report as tables.
func WriteProbeMarkdown(w io.Writer, r *ProbeReport) error {
	fmt.Fprintf(w, "# Model compatibility probe\n\nStarted %s on %s; llama.cpp %s.\n\n", r.Started.Format(time.RFC3339), r.Machine.Accelerator.Name, r.Runtime)
	names := []string{"tool_call", "tool_result", "code_arguments", "structured", "thinking_default", "thinking_off", "long_context"}
	fmt.Fprintf(w, "| Profile | Fit | Load s | %s | Thinking (declared / measured) | Decode t/s | Prompt t/s | Server RSS GiB | VRAM GiB |\n", strings.Join(names, " | "))
	fmt.Fprintf(w, "|---|---|---|%s---|---|---|---|---|\n", strings.Repeat("---|", len(names)))
	for _, p := range r.Profiles {
		if p.Error != "" {
			msg := p.Error
			if i := strings.Index(msg, "\n"); i >= 0 {
				msg = msg[:i]
			}
			fmt.Fprintf(w, "| %s | %s | | %s | | | | | |\n", p.Profile, p.Fit.Level, strings.Repeat(" |", len(names)-1)+" error: "+strings.ReplaceAll(msg, "|", "/"))
			continue
		}
		var cells []string
		for _, n := range names {
			cell := ""
			for _, c := range p.Checks {
				if c.Name == n {
					cell = "fail"
					if c.Pass {
						cell = "pass"
					}
				}
			}
			cells = append(cells, cell)
		}
		fmt.Fprintf(w, "| %s | %s | %.1f | %s | %s / %s | %.1f | %.0f | %.1f | %.1f |\n", p.Profile, p.Fit.Level, p.LoadSeconds, strings.Join(cells, " | "),
			dash(p.Declared.Thinking), dash(p.Thinking), p.DecodeTPS, p.PromptTPS, p.ServerRSSMiB/1024, p.VRAMUsedMiB/1024)
	}
	fmt.Fprintln(w)
	for _, p := range r.Profiles {
		if len(p.Disagreements) > 0 {
			fmt.Fprintf(w, "- **%s**: the weights contradict the profile: %s.\n", p.Profile, strings.Join(p.Disagreements, "; "))
		}
		for _, c := range p.Checks {
			if !c.Pass {
				fmt.Fprintf(w, "- **%s** failed %s: %s\n", p.Profile, c.Name, strings.ReplaceAll(c.Detail, "\n", " "))
			}
		}
	}
	return nil
}

func dash(s string) string {
	if s == "" {
		return "–"
	}
	return s
}
