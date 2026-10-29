package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/benchmark"
	"github.com/akynte/boundedcode/internal/frontier"
	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/model"
)

func newBenchModelsCmd(app *App) *cobra.Command {
	var (
		names []string
		out   string
		long  int
	)
	cmd := &cobra.Command{
		Use:   "models [NAME...]",
		Short: "Probe local models with BoundedCode's own requests: tool calls, code in arguments, JSON schemas, thinking control, long context, speed",
		Long: `Each model with downloaded weights is loaded alone and asked what the
agent asks: a tool call, an answer from a tool's result, an edit whose
arguments hold code, an answer constrained to a JSON schema, a request with
thinking off, and a fact from a long prompt. Decode and prompt speed, load
time and memory are recorded, and the profile's declared capabilities are
checked against the weights' metadata. Vendor scores are not evidence that
a model works with BoundedCode; this is.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			names = append(names, args...)
			if len(names) == 0 {
				names = app.Models.Names()
			}
			var profiles []model.Profile
			for _, n := range names {
				p, err := app.profile(n)
				if err != nil {
					return err
				}
				profiles = append(profiles, p)
			}
			if out == "" {
				out = filepath.Join("benchmarks", "reports", time.Now().UTC().Format("20060102T150405Z")+"-models.json")
			}
			b := &benchmark.ModelProbe{Mgr: app.llamaManager(), LongTokens: long,
				Progress: func(s string) { fmt.Fprintf(app.Err, "%s %s\n", time.Now().Format("15:04:05"), s) }}
			rep, runErr := b.Run(cmd.Context(), profiles)
			if err := writeReport(out, rep, func(buf *bytes.Buffer) error { return benchmark.WriteProbeMarkdown(buf, rep) }); err != nil {
				return err
			}
			// Model routing reads the load times from here.
			if err := recordBenchmark(cmd, app, "models-"+rep.Started.Format("20060102T150405Z"), "models", "", names, rep); err != nil {
				return err
			}
			app.printf("report: %s\n", strings.TrimSuffix(out, ".json")+".md")
			return runErr
		},
	}
	f := cmd.Flags()
	f.StringSliceVar(&names, "model", nil, "profiles to probe (default: every profile; those without weights are listed as such)")
	f.StringVar(&out, "out", "", "report path (.json; .md written alongside)")
	f.IntVar(&long, "long", 32768, "the long-context check's prompt in tokens (at most half the profile's context)")
	return cmd
}

func newBenchSpecCmd(app *App) *cobra.Command {
	var (
		modelName, out, source string
		configs                []string
		repeats                int
	)
	cmd := &cobra.Command{
		Use:   "spec",
		Short: "Measure speculative decoding (n-gram, draft model, MTP) on one model against no speculation",
		Long: `Every configuration loads the model server alone and answers the same
prompts: a file rewritten with a small change, new code, an explanation,
and reasoning with thinking on. Decoding is greedy, so a configuration
that changes an answer is reported. The first configuration is always the
baseline, without speculation.

--config NAME=TYPE[,draft=FILE][,n_max=N][,n_min=N][,ngl=N][,file=GGUF][,moe=N]
  TYPE is a llama-server --spec-type; file= serves other weights (an MTP
  build); moe= replaces n_cpu_moe (a draft model or MTP layers need VRAM).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := app.profile(modelName)
			if err != nil {
				return err
			}
			cfgs := []benchmark.SpecConfig{{Name: "none"}}
			for _, c := range configs {
				sc, err := parseSpecConfig(c)
				if err != nil {
					return err
				}
				cfgs = append(cfgs, sc)
			}
			src, err := os.ReadFile(source)
			if err != nil {
				return fmt.Errorf("--source: %w", err)
			}
			if out == "" {
				out = filepath.Join("benchmarks", "reports", time.Now().UTC().Format("20060102T150405Z")+"-spec-"+p.Name+".json")
			}
			b := &benchmark.SpecBench{Mgr: app.llamaManager(), Profile: p, Configs: cfgs, Repeats: repeats,
				Progress: func(s string) { fmt.Fprintf(app.Err, "%s %s\n", time.Now().Format("15:04:05"), s) }}
			rep, runErr := b.Run(cmd.Context(), benchmark.SpecPrompts(string(src)))
			if err := writeReport(out, rep, func(buf *bytes.Buffer) error { return benchmark.WriteSpecMarkdown(buf, rep) }); err != nil {
				return err
			}
			app.printf("report: %s\n", strings.TrimSuffix(out, ".json")+".md")
			return runErr
		},
	}
	f := cmd.Flags()
	f.StringVarP(&modelName, "model", "m", "", "model profile (default: the configured one)")
	f.StringArrayVar(&configs, "config", nil, "a configuration to measure (repeatable; see above)")
	f.IntVar(&repeats, "repeats", 2, "runs of each prompt per configuration")
	f.StringVar(&source, "source", "internal/frontier/policy.go", "the Go file the rewrite and explain prompts use")
	f.StringVar(&out, "out", "", "report path (.json; .md written alongside)")
	return cmd
}

// parseSpecConfig reads NAME=TYPE[,key=value...] (see bench spec --help).
func parseSpecConfig(v string) (benchmark.SpecConfig, error) {
	name, rest, ok := strings.Cut(v, "=")
	if !ok || name == "" {
		return benchmark.SpecConfig{}, fmt.Errorf("--config %q: want NAME=TYPE[,key=value...]", v)
	}
	sc := benchmark.SpecConfig{Name: name}
	var keep []string
	for i, part := range strings.Split(rest, ",") {
		k, val, _ := strings.Cut(part, "=")
		switch {
		case i == 0:
			keep = append(keep, part)
		case k == "file":
			sc.File = val
		case k == "moe":
			n, err := strconv.Atoi(val)
			if err != nil {
				return sc, fmt.Errorf("--config %q: moe=%q", v, val)
			}
			sc.NCPUMoE = n
		default:
			keep = append(keep, part)
		}
	}
	if keep[0] == "none" && len(keep) == 1 {
		return sc, nil
	}
	sp, err := parseSpeculative(strings.Join(keep, ","))
	if err != nil {
		return sc, fmt.Errorf("--config %q: %w", v, err)
	}
	sc.Spec = sp
	return sc, nil
}

// writeReport writes v as indented JSON at path and its markdown beside
// it.
func writeReport(path string, v any, md func(*bytes.Buffer) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := md(&buf); err != nil {
		return err
	}
	return os.WriteFile(strings.TrimSuffix(path, ".json")+".md", buf.Bytes(), 0o644)
}

func newBenchRouterCmd(app *App) *cobra.Command {
	var (
		modelName, out string
		runs           int
	)
	cmd := &cobra.Command{
		Use:   "router",
		Short: "Measure the router's model classification of requests its rules cannot classify, with the model's thinking and without",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ep, err := app.directEndpoint(cmd.Context(), modelName)
			if err != nil {
				return err
			}
			if out == "" {
				out = filepath.Join("benchmarks", "reports", time.Now().UTC().Format("20060102T150405Z")+"-router-"+ep.model+".json")
			}
			n := 0
			rep, runErr := benchmark.RunRouter(cmd.Context(), ep.model, func() *inference.Gateway { n++; return ep.newGateway(fmt.Sprintf("router-%d", n)) }, runs,
				func(s string) { fmt.Fprintf(app.Err, "%s %s\n", time.Now().Format("15:04:05"), s) })
			if err := writeReport(out, rep, func(buf *bytes.Buffer) error { return benchmark.WriteRouterMarkdown(buf, rep) }); err != nil {
				return err
			}
			app.printf("report: %s\n", strings.TrimSuffix(out, ".json")+".md")
			return runErr
		},
	}
	f := cmd.Flags()
	f.StringVarP(&modelName, "model", "m", "", "model profile (local) or model id (cloud); default: the configured one")
	f.IntVar(&runs, "runs", 2, "runs of every request in each arm")
	f.StringVar(&out, "out", "", "report path (.json; .md written alongside)")
	return cmd
}

func newBenchEscalationCmd(app *App) *cobra.Command {
	var pattern, out string
	cmd := &cobra.Command{
		Use:   "escalation",
		Short: "Replay the escalation policies (rules, adaptive) on recorded bounded-mode runs and compare them with the runs' hidden checks",
		Long: `Each bench tasks report whose tasks' ledgers are still on disk is read
back: the attempts, their failures, the contract and the triggers the run's
own policy fired. Both policies are replayed at the points the task runner
evaluates them, counting every trigger as sent. A run that ended failing its
hidden checks needed help; one that passed did not. The attempts are the
recorded ones: what advice would have changed is not known.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			runs, err := benchmark.LoadEscalationRuns(cmd.Context(), pattern)
			if err != nil {
				return err
			}
			if len(runs) == 0 {
				return fmt.Errorf("no recorded runs with ledgers match %s", pattern)
			}
			rep := benchmark.ReplayEscalations(app.Config.Escalation, runs, []string{frontier.PolicyRules, frontier.PolicyAdaptive})
			if out == "" {
				out = filepath.Join("benchmarks", "reports", time.Now().UTC().Format("20060102T150405Z")+"-escalation.json")
			}
			if err := writeReport(out, rep, func(buf *bytes.Buffer) error { return benchmark.WriteEscalationMarkdown(buf, rep) }); err != nil {
				return err
			}
			app.printf("report: %s\n", strings.TrimSuffix(out, ".json")+".md")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&pattern, "reports", "benchmarks/reports/*tasks*.json", "bench tasks reports to read")
	f.StringVar(&out, "out", "", "report path (.json; .md written alongside)")
	return cmd
}

func newBenchPacketsCmd(app *App) *cobra.Command {
	var pattern, out string
	cmd := &cobra.Command{
		Use:   "packets",
		Short: "Rebuild escalation packets from recorded runs in full and abstract mode and measure what of the repositories they carry",
		RunE: func(cmd *cobra.Command, _ []string) error {
			runs, err := benchmark.LoadEscalationRuns(cmd.Context(), pattern)
			if err != nil {
				return err
			}
			if len(runs) == 0 {
				return fmt.Errorf("no recorded runs with ledgers match %s", pattern)
			}
			work := filepath.Join(app.Paths.Cache, "bench-packets")
			if err := os.MkdirAll(work, 0o700); err != nil {
				return err
			}
			rep, err := benchmark.MeasurePackets(cmd.Context(), runs, work, app.Config.Frontier.MaxPacketTokens)
			if err != nil {
				return err
			}
			if out == "" {
				out = filepath.Join("benchmarks", "reports", time.Now().UTC().Format("20060102T150405Z")+"-packets.json")
			}
			if err := writeReport(out, rep, func(buf *bytes.Buffer) error { return benchmark.WritePacketMarkdown(buf, rep) }); err != nil {
				return err
			}
			app.printf("report: %s\n", strings.TrimSuffix(out, ".json")+".md")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&pattern, "reports", "benchmarks/reports/*tasks*.json", "bench tasks reports to read")
	f.StringVar(&out, "out", "", "report path (.json; .md written alongside)")
	return cmd
}
