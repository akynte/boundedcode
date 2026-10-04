package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/hw"
	"github.com/akynte/boundedcode/internal/inference/llamacpp"
	"github.com/akynte/boundedcode/internal/repointel/cbm"
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
			add(llamaCUDACheck(ctx, cfg.Inference.ServerBinary))
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

	add(gitCheck(ctx))
	add(versionCheck(ctx, "go", "go", []string{"version"}, false, "needed to build and verify Go repositories outside containers"))
	add(versionCheck(ctx, "python3", "python3", []string{"--version"}, false, "the OpenHands adapter needs Python >= 3.12 (uv can provide it)"))
	add(adapterCheck(app))
	if cfg.Sandbox.Kind == "docker" {
		c := versionCheck(ctx, "container engine", cfg.Sandbox.Engine, []string{"version", "--format", "{{.Server.Version}}"}, true,
			"install Docker or Podman, or set sandbox.kind: none for development only")
		add(c)
	} else {
		add(check{"container engine", statusWarn, "sandbox.kind is none: agent tools run unsandboxed", "use docker for autonomous tasks"})
	}
	add(versionCheck(ctx, "uv", "uv", []string{"--version"}, false, "needed to build the OpenHands adapter outside containers"))
	add(cbmCheck(ctx, cfg.RepoIntel.Binary))
	for _, c := range serenaChecks(ctx, app, true) {
		add(c)
	}
	for _, c := range languageServerChecks(cfg.RepoIntel.Serena.Enabled) {
		add(c)
	}
	add(versionCheck(ctx, "gitleaks", "gitleaks", []string{"version"}, false, "secret scanning stage is skipped without it"))
	add(versionCheck(ctx, "ripgrep", "rg", []string{"--version"}, false, "used for exact lexical retrieval"))
	if cfg.Sandbox.Kind == "docker" {
		ictx, cancel := context.WithTimeout(ctx, 15*time.Second)
		out, err := exec.CommandContext(ictx, cfg.Sandbox.Engine, "image", "inspect", "--format", "{{.Id}}", cfg.Agent.Image).Output()
		cancel()
		if err != nil {
			add(check{"sandbox image", statusWarn, cfg.Agent.Image + " not built", "run `boundedcode sandbox build --dir adapters/openhands`"})
		} else {
			add(check{"sandbox image", statusOK, cfg.Agent.Image + " " + strings.TrimSpace(string(out))[:19], ""})
		}
	}
	add(frontierCheck(ctx, app))
	return out
}

// minGitVersion is the oldest git with the worktree commands tasks use
// (`git worktree add/remove`, 2.17).
var minGitVersion = [2]int{2, 17}

func gitCheck(ctx context.Context) check {
	c := versionCheck(ctx, "git", "git", []string{"--version"}, true, "install git >= 2.17 (worktree support)")
	if c.Status != statusOK {
		return c
	}
	m := regexp.MustCompile(`(\d+)\.(\d+)`).FindStringSubmatch(c.Detail)
	if m == nil {
		c.Status, c.Hint = statusWarn, "could not parse the git version; tasks need git >= 2.17"
		return c
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	if major < minGitVersion[0] || major == minGitVersion[0] && minor < minGitVersion[1] {
		c.Status, c.Hint = statusFail, "tasks use git worktrees: install git >= 2.17"
	}
	return c
}

// cbmCheck compares the installed codebase-memory-mcp with the pin.
func cbmCheck(ctx context.Context, binary string) check {
	const hint = "install the pinned release with scripts/install-deps.sh (https://github.com/DeusData/codebase-memory-mcp/releases)"
	got, err := cbm.CheckVersion(ctx, binary)
	switch {
	case err == nil:
		return check{"codebase-memory-mcp", statusOK, fmt.Sprintf("%s (want %s)", got, cbm.RequiredVersion), ""}
	case got != "":
		return check{"codebase-memory-mcp", statusWarn, fmt.Sprintf("%s, want %s", got, cbm.RequiredVersion), hint}
	default:
		return check{"codebase-memory-mcp", statusWarn, fmt.Sprintf("%v (want %s)", err, cbm.RequiredVersion), hint}
	}
}

// llamaCUDACheck asks llama-server which devices it can use; without a CUDA
// device every layer runs on the CPU.
func llamaCUDACheck(ctx context.Context, binary string) check {
	bin, err := exec.LookPath(binary)
	if err != nil {
		return check{"llama.cpp CUDA", statusWarn, err.Error(), ""}
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--list-devices")
	// Backends are shared libraries next to the binary in a CMake build.
	cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+strings.Trim(filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("LD_LIBRARY_PATH"), string(os.PathListSeparator)))
	b, err := cmd.CombinedOutput()
	if err != nil {
		return check{"llama.cpp CUDA", statusWarn, fmt.Sprintf("--list-devices: %v %s", err, firstMeaningfulLine(string(b))),
			"llama.cpp may be too old for --list-devices; rebuild with scripts/build-llama-cpp.sh"}
	}
	var devs []string
	for l := range strings.SplitSeq(string(b), "\n") {
		if l = strings.TrimSpace(l); strings.HasPrefix(l, "CUDA") {
			devs = append(devs, l)
		}
	}
	if len(devs) == 0 {
		return check{"llama.cpp CUDA", statusWarn, "no CUDA device listed (CPU-only build or driver problem)", "build llama.cpp with CUDA (scripts/build-llama-cpp.sh)"}
	}
	return check{"llama.cpp CUDA", statusOK, strings.Join(devs, "; "), ""}
}

var openhandsPinRE = regexp.MustCompile(`"openhands-sdk==([^"]+)"`)

// adapterCheck reports the OpenHands SDK pin of the adapter project and
// whether its local environment exists (needed with sandbox.kind none).
func adapterCheck(app *App) check {
	dir := app.Config.Agent.AdapterDir
	if dir == "" {
		dir = filepath.Join("adapters", "openhands", "python") // running from a checkout
	}
	b, err := os.ReadFile(filepath.Join(dir, "pyproject.toml"))
	if err != nil {
		st := statusOK
		if app.Config.Sandbox.Kind != "docker" {
			st = statusWarn // the adapter runs from adapter_dir
		}
		return check{"openhands adapter", st, "adapter project not found (agent.adapter_dir unset or missing)",
			"set agent.adapter_dir to adapters/openhands/python (`init --adapter-dir`)"}
	}
	pin := "unpinned"
	if m := openhandsPinRE.FindSubmatch(b); m != nil {
		pin = string(m[1])
	}
	venv := "venv absent"
	if fi, err := os.Stat(filepath.Join(dir, ".venv")); err == nil && fi.IsDir() {
		venv = "venv present"
	}
	where := "runs in sandbox image " + app.Config.Agent.Image
	st := statusOK
	if app.Config.Sandbox.Kind != "docker" {
		where = "runs from " + dir
		if venv == "venv absent" {
			st = statusWarn
		}
	}
	return check{"openhands adapter", st, fmt.Sprintf("openhands-sdk %s pinned; %s; %s", pin, venv, where), "run `uv sync --frozen` in " + dir}
}

// languageServerChecks reports the language servers Serena drives. They are
// informational while Serena is disabled.
func languageServerChecks(serenaEnabled bool) []check {
	missing := statusOK
	if serenaEnabled {
		missing = statusWarn
	}
	var out []check
	for _, ls := range []struct{ name, bin, hint string }{
		{"gopls", "gopls", "go install golang.org/x/tools/gopls@latest"},
		{"typescript LSP", "typescript-language-server", "`boundedcode serena setup` installs its own copy"},
	} {
		if p, err := exec.LookPath(ls.bin); err == nil {
			out = append(out, check{ls.name, statusOK, p, ""})
		} else {
			out = append(out, check{ls.name, missing, ls.bin + " not on PATH (used by Serena)", ls.hint})
		}
	}
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
