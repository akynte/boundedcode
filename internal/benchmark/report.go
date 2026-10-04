package benchmark

import (
	"fmt"
	"io"
	"strings"
)

// WriteInfraMarkdown renders a human-readable summary of an infra report.
func WriteInfraMarkdown(w io.Writer, r *InfraReport) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Infrastructure benchmark `%s`\n\n", r.ID)
	fmt.Fprintf(&b, "* model: `%s` (`%s`)\n* runtime: %s `%s`\n", r.Model, r.ModelFile, r.Runtime, r.RuntimeVer)
	m := r.Machine
	fmt.Fprintf(&b, "* machine: %s, %d threads, %d MiB RAM", m.CPUModel, m.LogicalCPUs, m.MemTotalMiB)
	for _, g := range m.GPUs {
		fmt.Fprintf(&b, ", %s %d MiB (driver %s, power limit %.0f W)", g.Name, g.MemTotalMiB, g.Driver, g.PowerLimitW)
	}
	fmt.Fprintf(&b, "\n* started: %s, finished: %s\n", r.Started.Format("2006-01-02 15:04Z"), r.Finished.Format("2006-01-02 15:04Z"))
	fmt.Fprintf(&b, "* decode tokens per request: %d; prompts: %v tokens (cold = unique prompt; warm = the conversation continued with the reply plus a new observation)\n", r.Options.DecodeTokens, r.Options.PromptSizes)
	loadKind := "warm page cache"
	if r.Options.ColdLoad {
		loadKind = "cold (model evicted from page cache before each load)"
	}
	fmt.Fprintf(&b, "* model load: %s\n* conditions: %s\n\n", loadKind, orDash(r.Options.Conditions))

	b.WriteString("## Feasibility (minimum `n_cpu_moe` that loads)\n\n| ctx | KV type | ubatch | min n_cpu_moe | probes |\n|---|---|---|---|---|\n")
	for _, f := range r.Feasibility {
		fmt.Fprintf(&b, "| %d | %s | %d | %d | %v |\n", f.CtxSize, f.CacheType, f.UBatch, f.MinNCPUMoE, f.Probes)
	}
	b.WriteString("\n## Candidates\n\n| ctx | KV | ubatch | n_cpu_moe | load s | VRAM used/free MiB | RSS MiB | prompt t/s (median cold) | decode t/s (median) | error |\n|---|---|---|---|---|---|---|---|---|---|\n")
	for _, c := range r.Candidates {
		fmt.Fprintf(&b, "| %d | %s | %d | %d | %.1f | %d / %d | %d | %.0f | %.1f | %s |\n",
			c.CtxSize, c.CacheType, c.UBatch, c.NCPUMoE, c.LoadSeconds, c.VRAMUsedMiB, c.VRAMFreeMiB, c.ServerRSSMiB, c.PromptTPS, c.DecodeTPS, firstLine(c.Error))
	}
	b.WriteString("\n## Per-prompt detail\n\n| config | prompt tokens | mode | cache_n | prompt ms | prompt t/s | decode t/s |\n|---|---|---|---|---|---|---|\n")
	for _, c := range r.Candidates {
		for _, run := range c.Runs {
			mode := "cold"
			if run.Cached {
				mode = "warm"
			}
			fmt.Fprintf(&b, "| ctx%d/%s/ub%d/moe%d | %d | %s | %d | %.0f | %.0f | %.1f |\n",
				c.CtxSize, c.CacheType, c.UBatch, c.NCPUMoE, run.PromptN+run.CacheN, mode, run.CacheN, run.PromptMS, run.PromptTPS, run.DecodeTPS)
		}
	}
	if rc := r.Recommended; rc != nil {
		fmt.Fprintf(&b, "\n## Recommended\n\nctx=%d, KV=%s, ubatch=%d, n_cpu_moe=%d: decode %.1f t/s, prompt %.0f t/s, %d MiB VRAM free.\n\n```\n%s\n```\n",
			rc.CtxSize, rc.CacheType, rc.UBatch, rc.NCPUMoE, rc.DecodeTPS, rc.PromptTPS, rc.VRAMFreeMiB, strings.Join(rc.Args, " "))
	}
	if s := r.Sustained; s != nil {
		fmt.Fprintf(&b, "\n## Sustained run (%s, %d requests)\n\ndecode t/s first quarter %.1f → last quarter %.1f; max GPU temp %d °C\n\n| t (s) | decode t/s | temp °C | power W | SM MHz | throttle |\n|---|---|---|---|---|---|\n",
			s.Duration, s.Requests, s.FirstTPS, s.LastTPS, s.MaxTempC)
		step := max(1, len(s.Samples)/20)
		for i := 0; i < len(s.Samples); i += step {
			x := s.Samples[i]
			fmt.Fprintf(&b, "| %.0f | %.1f | %d | %.1f | %d | %s |\n", x.T, x.DecodeTPS, x.TempC, x.PowerW, x.SMClockMHz, x.Throttle)
		}
	}
	for _, n := range r.Notes {
		fmt.Fprintf(&b, "\n> note: %s\n", n)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}

// WriteSuiteMarkdown renders an engineering suite report.
func WriteSuiteMarkdown(w io.Writer, r *SuiteReport) error {
	var b strings.Builder
	s := r.Summary
	fmt.Fprintf(&b, "# Engineering benchmark `%s`\n\nmodel: `%s` · started %s · finished %s\n\n", r.ID, r.Model,
		r.Started.Format("2006-01-02 15:04Z"), r.Finished.Format("2006-01-02 15:04Z"))
	fmt.Fprintf(&b, "| metric | value |\n|---|---|\n| tasks | %d |\n| verified (hidden checks) | %d (%.0f%%) |\n| local-only completion rate | %.0f%% |\n| frontier escalation rate | %.0f%% |\n| verified tasks / hour | %.2f |\n| attempts / successful task | %.2f |\n| wall-clock / task | %.0f s |\n| processed local tokens / task (uncached prompt + generated) | %.0f |\n| self-verified but hidden checks failed | %d |\n\n",
		s.Tasks, s.Verified, 100*s.SuccessRate, 100*s.LocalOnlyRate, 100*s.FrontierEscalationRate, s.VerifiedPerHour, s.MeanAttemptsSuccess, s.MeanWallSeconds, s.MeanLocalTokens, s.SelfVerifyFalsePass)
	b.WriteString("| task | category | success | status | self-verified | attempts | tokens | escalations | wall s | notes |\n|---|---|---|---|---|---|---|---|---|---|\n")
	for _, t := range r.Results {
		note := firstLine(t.Error)
		if len(t.FailedChecks) > 0 {
			note = firstLine(t.FailedChecks[0])
		}
		fmt.Fprintf(&b, "| %s | %s | %v | %s | %v | %d | %d | %d | %.0f | %s |\n", t.ID, t.Category, t.Success, t.TaskStatus, t.SelfVerified,
			t.Attempts, t.LocalTokens, t.Escalations, t.WallSeconds, strings.ReplaceAll(note, "|", "/"))
	}
	b.WriteString("\nRepository intelligence (context packs and agent tool use):\n\n" +
		"| task | packs | pack tokens | code tokens | serena calls (err) | serena ms | graph calls | graph ms | symbols serena/graph | fallbacks | agent tool calls |\n" +
		"|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, t := range r.Results {
		m := t.Intel
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d (%d) | %.0f | %d | %.0f | %d/%d | %d | %d |\n", t.ID, m.Packs, m.PackTokens, m.CodeTokens,
			m.NavCalls, m.NavErrors, m.NavMillis, m.GraphCalls, m.GraphMillis, m.NavSymbols, m.GraphSymbols, m.Fallbacks, m.AgentToolCalls)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func orDash(s string) string {
	if s == "" {
		return "not recorded"
	}
	return s
}
