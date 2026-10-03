package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/hw"
	"github.com/akynte/boundedcode/internal/inference/llamacpp"
)

// checkStatus is the outcome of one doctor check.
type checkStatus string

const (
	statusOK   checkStatus = "ok"
	statusWarn checkStatus = "warn"
	statusFail checkStatus = "fail"
)

type check struct {
	Name   string      `json:"name"`
	Status checkStatus `json:"status"`
	Detail string      `json:"detail"`
	Hint   string      `json:"hint,omitempty"`
}

func newDoctorCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check the local environment and dependencies",
		RunE: func(cmd *cobra.Command, _ []string) error {
			checks := runDoctor(cmd.Context(), app)
			if app.jsonOut {
				return app.printJSON(checks)
			}
			failed := 0
			for _, c := range checks {
				app.printf("[%-4s] %-22s %s\n", c.Status, c.Name, c.Detail)
				if c.Hint != "" && c.Status != statusOK {
					app.printf("       %-22s hint: %s\n", "", c.Hint)
				}
				if c.Status == statusFail {
					failed++
				}
			}
			if failed > 0 {
				return fmt.Errorf("%d required check(s) failed", failed)
			}
			return nil
		},
	}
}

func runDoctor(ctx context.Context, app *App) []check {
	var out []check
	add := func(c check) { out = append(out, c) }

	if _, err := os.Stat(app.configFile()); err != nil {
		add(check{"config", statusWarn, "no config file; using defaults", "run `init`"})
	} else {
		add(check{"config", statusOK, app.configFile(), ""})
	}
	if s, err := app.Store(ctx); err != nil {
		add(check{"state db", statusFail, err.Error(), ""})
	} else {
		add(check{"state db", statusOK, s.Path(), ""})
	}

	snap := hw.Probe(ctx)
	add(check{"cpu", statusOK, fmt.Sprintf("%s (%d threads)", snap.CPUModel, snap.LogicalCPUs), ""})
	ram := check{"ram", statusOK, fmt.Sprintf("%d MiB total, %d MiB available", snap.MemTotalMiB, snap.MemAvailMiB), ""}
	if snap.MemAvailMiB < 24*1024 {
		ram.Status, ram.Hint = statusWarn, "MoE expert offload of a ~22 GB model needs roughly 24 GB free RAM"
	}
	add(ram)
	if len(snap.GPUs) == 0 {
		add(check{"gpu", statusWarn, "no NVIDIA GPU detected (CPU-only inference will be slow)", "install the NVIDIA driver"})
	}
	for _, g := range snap.GPUs {
		add(check{fmt.Sprintf("gpu%d", g.Index), statusOK,
			fmt.Sprintf("%s, CC %s, %d/%d MiB used, driver %s", g.Name, g.ComputeCap, g.MemUsedMiB, g.MemTotalMiB, g.Driver), ""})
	}

	cfg := app.Config
	if cfg.Inference.Mode == "external" {
		add(check{"inference", statusOK, "external server at " + cfg.Inference.ExternalURL, ""})
	} else {
		if v, err := llamacpp.Version(ctx, cfg.Inference.ServerBinary); err != nil {
			add(check{"llama-server", statusFail, err.Error(), "build llama.cpp with CUDA (scripts/build-llama-cpp.sh) or pass `init --llama-server PATH`"})
		} else {
			add(check{"llama-server", statusOK, v + " (" + cfg.Inference.ServerBinary + ")", ""})
		}
	}
	if p, err := app.Models.Get(cfg.DefaultModel); err != nil {
		add(check{"default model", statusFail, err.Error(), ""})
	} else {
		path := p.ResolveFile(cfg.ModelsDir)
		if fi, err := os.Stat(path); err != nil {
			add(check{"default model", statusFail, path + " not found",
				fmt.Sprintf("download %s from huggingface.co/%s (license: %s)", p.Source.File, p.Source.Repo, p.Source.License)})
		} else {
			add(check{"default model", statusOK, fmt.Sprintf("%s (%.1f GiB)", path, float64(fi.Size())/(1<<30)), ""})
		}
	}

	add(versionCheck(ctx, "git", "git", []string{"--version"}, true, ""))
	if cfg.Sandbox.Kind == "docker" {
		c := versionCheck(ctx, "container engine", cfg.Sandbox.Engine, []string{"version", "--format", "{{.Server.Version}}"}, true,
			"install Docker or Podman, or set sandbox.kind: none for development only")
		add(c)
	} else {
		add(check{"container engine", statusWarn, "sandbox.kind is none: agent tools run unsandboxed", "use docker for autonomous tasks"})
	}
	add(versionCheck(ctx, "uv", "uv", []string{"--version"}, false, "needed to build the OpenHands adapter outside containers"))
	add(versionCheck(ctx, "codebase-memory-mcp", cfg.RepoIntel.Binary, []string{"--version"}, false,
		"install from https://github.com/DeusData/codebase-memory-mcp/releases (scripts/install-deps.sh)"))
	add(versionCheck(ctx, "gitleaks", "gitleaks", []string{"version"}, false, "secret scanning stage is skipped without it"))
	add(versionCheck(ctx, "ripgrep", "rg", []string{"--version"}, false, "used for exact lexical retrieval"))
	if cfg.Sandbox.Kind == "docker" {
		out, err := exec.CommandContext(ctx, cfg.Sandbox.Engine, "image", "inspect", "--format", "{{.Id}}", cfg.Agent.Image).Output()
		if err != nil {
			add(check{"sandbox image", statusWarn, cfg.Agent.Image + " not built", "run `boundedcode sandbox build --dir adapters/openhands`"})
		} else {
			add(check{"sandbox image", statusOK, cfg.Agent.Image + " " + strings.TrimSpace(string(out))[:19], ""})
		}
	}
	add(frontierCheck(ctx, app))
	return out
}

func versionCheck(ctx context.Context, name, bin string, args []string, required bool, hint string) check {
	path, err := exec.LookPath(bin)
	st := statusWarn
	if required {
		st = statusFail
	}
	if err != nil {
		return check{name, st, bin + " not found", hint}
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, path, args...).CombinedOutput()
	line := firstMeaningfulLine(string(b))
	if err != nil {
		return check{name, st, fmt.Sprintf("%s: %v %s", path, err, line), hint}
	}
	return check{name, statusOK, fmt.Sprintf("%s (%s)", line, path), ""}
}

func firstMeaningfulLine(s string) string {
	for l := range strings.SplitSeq(s, "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "ggml_") && !strings.HasPrefix(l, "load_backend") && !strings.HasPrefix(l, "Device ") {
			return l
		}
	}
	return ""
}

func frontierCheck(ctx context.Context, app *App) check {
	cfg := app.Config.Frontier
	if !cfg.Enabled {
		return check{"frontier", statusOK, "disabled (local-only)", ""}
	}
	if cfg.Provider == "manual" {
		return check{"frontier", statusOK, "manual provider (packets written to disk)", ""}
	}
	path, err := exec.LookPath(cfg.Binary)
	if err != nil {
		return check{"frontier", statusWarn, "codex CLI not found", "install the Codex CLI and run `codex login`"}
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, path, "login", "status").CombinedOutput()
	line := firstMeaningfulLine(string(b))
	if err != nil {
		return check{"frontier", statusWarn, "codex not logged in: " + line, "run `codex login` (ChatGPT sign-in)"}
	}
	return check{"frontier", statusOK, "codex: " + line, ""}
}
