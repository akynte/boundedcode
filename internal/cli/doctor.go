package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/buildinfo"
	"github.com/akynte/boundedcode/internal/hw"
	"github.com/akynte/boundedcode/internal/inference/llamacpp"
	"github.com/akynte/boundedcode/internal/model"
	"github.com/akynte/boundedcode/internal/repointel/cbm"
	"github.com/akynte/boundedcode/internal/sandbox"
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
		add(check{"config", statusWarn, "no config file; using defaults", setupHint("config")})
	} else {
		add(check{"config", statusOK, app.configFile(), ""})
	}
	if s, err := app.Store(ctx); err != nil {
		add(check{"state db", statusFail, err.Error(), ""})
	} else {
		add(check{"state db", statusOK, s.Path(), ""})
	}

	snap := app.hardware(ctx)
	add(check{"cpu", statusOK, fmt.Sprintf("%s (%d threads, %s/%s)", snap.CPUModel, snap.LogicalCPUs, snap.OS, snap.Arch), ""})
	add(check{"ram", statusOK, fmt.Sprintf("%d MiB total, %d MiB available", snap.MemTotalMiB, snap.MemAvailMiB), ""})
	switch snap.Accelerator.Kind {
	case hw.AccelMetal:
		add(check{"gpu", statusOK, describeHardware(snap), ""})
	case hw.AccelNone:
		if !app.Config.Inference.IsCloud() {
			add(check{"gpu", statusWarn, "no supported GPU detected: local models run on the CPU (slow)",
				"an NVIDIA GPU with its driver, Apple Silicon, or a cloud provider (`" + buildinfo.Command() + " provider use`)"})
		}
	}
	if !app.Config.Inference.IsCloud() {
		add(modelFitCheck(app, snap))
	}
	for _, g := range snap.GPUs {
		add(check{fmt.Sprintf("gpu%d", g.Index), statusOK,
			fmt.Sprintf("%s, CC %s, %d/%d MiB used, driver %s", g.Name, g.ComputeCap, g.MemUsedMiB, g.MemTotalMiB, g.Driver), ""})
	}

	cfg := app.Config
	for _, c := range inferenceChecks(ctx, app) {
		add(c)
	}

	add(gitCheck(ctx))
	add(versionCheck(ctx, "go", "go", []string{"version"}, false, "needed to build and verify Go repositories outside containers"))
	add(versionCheck(ctx, "python3", "python3", []string{"--version"}, false, "the OpenHands adapter needs Python >= 3.12 (uv can provide it)"))
	add(adapterCheck(app))
	// engineErr is why the container engine cannot run the sandbox (nil:
	// it can); the image is only checked against a working engine.
	var engineErr error
	if cfg.Sandbox.Kind == "docker" {
		add(engineCheck(ctx, cfg.Sandbox.Engine, &engineErr))
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
	add(versionCheck(ctx, "gitleaks", "gitleaks", []string{"version"}, false,
		"the secret scan is skipped while iterating and the full gate fails without it; "+setupHint("tools")))
	add(versionCheck(ctx, "ripgrep", "rg", []string{"--version"}, false, "used for exact lexical retrieval"))
	if cfg.Sandbox.Kind == "docker" {
		add(imageCheck(ctx, cfg.Sandbox.Engine, cfg.Agent.Image, engineErr))
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

// inferenceChecks reports the inference server and the default model's
// weights (served by the server itself in external mode).
func inferenceChecks(ctx context.Context, app *App) []check {
	var out []check
	add := func(c check) { out = append(out, c) }
	cfg := app.Config
	if cfg.Inference.IsCloud() {
		return cloudChecks(ctx, app)
	}
	if cfg.Inference.Mode == "external" {
		if err := app.checkExternalInference(ctx); err != nil {
			add(check{"inference", statusWarn, "external server at " + cfg.Inference.ExternalURL + " is not reachable",
				"start it, or correct inference.external_url in " + app.configFile()})
		} else {
			add(check{"inference", statusOK, "external server at " + cfg.Inference.ExternalURL, ""})
		}
	} else {
		if v, err := llamacpp.Version(ctx, cfg.Inference.ServerBinary); err != nil {
			add(check{"llama-server", statusFail, err.Error(), setupHint("inference") + " (or pass `init --llama-server PATH`, or use inference.mode: external)"})
		} else {
			add(check{"llama-server", statusOK, v + " (" + cfg.Inference.ServerBinary + ")", ""})
			add(llamaCUDACheck(ctx, cfg.Inference.ServerBinary))
		}
	}
	if cfg.Inference.Mode == "external" {
		add(check{"default model", statusOK, "served by the external server", ""})
	} else if p, err := app.Models.Get(cfg.DefaultModel); err != nil {
		add(check{"default model", statusFail, err.Error(), ""})
	} else {
		path := p.ResolveFile(app.modelsDir())
		if fi, err := os.Stat(path); err != nil {
			add(check{"default model", statusFail, path + " not found",
				fmt.Sprintf("%s (downloads %s from huggingface.co/%s, license: %s)", setupHint("model"), p.Source.File, p.Source.Repo, p.Source.License)})
		} else {
			add(check{"default model", statusOK, fmt.Sprintf("%s (%.1f GiB)", path, float64(fi.Size())/(1<<30)), ""})
		}
	}
	return out
}

// cloudChecks reports the selected cloud provider: its API key (present,
// and accepted by the provider) and the model's context.
func cloudChecks(ctx context.Context, app *App) []check {
	ic := app.Config.Inference
	name := "provider " + ic.Provider
	key, src, err := app.secretStore().Get(ic.Provider)
	if err != nil {
		_, err = app.cloudKey(ic.Provider) // the error that names the fixes
		return []check{{name, statusFail, "no API key", err.Error()}}
	}
	out := []check{{name + " key", statusOK, "stored in the " + src, ""}}
	up, err := upstreamFor(ic.Provider, ic.Cloud(), key, nil, 30*time.Second)
	if err != nil {
		return append(out, check{name, statusFail, err.Error(), ""})
	}
	pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	size, err := cloudContext(pctx, ic.Provider, ic.Cloud(), up)
	if err != nil {
		return append(out, check{name, statusFail, err.Error(), ""})
	}
	return append(out, check{name, statusOK, fmt.Sprintf("model %s, working context %d tokens", ic.Cloud().Model, size), ""})
}

// engineCheck reports whether the container engine can run the sandbox:
// not installed and installed-but-unusable (daemon down, no permission) are
// different problems with different fixes. *engineErr is set when it cannot.
func engineCheck(ctx context.Context, engine string, engineErr *error) check {
	const name = "container engine"
	var ee *sandbox.EngineError
	if err := sandbox.CheckEngine(ctx, engine, ""); errors.As(err, &ee) {
		*engineErr = err
		return check{name, statusFail, ee.Problem(), ee.Hint() + " (or set sandbox.kind: none for development only)"}
	} else if err != nil {
		*engineErr = err
		return check{name, statusFail, err.Error(), ""}
	}
	return versionCheck(ctx, name, engine, []string{"version", "--format", "{{.Server.Version}}"}, true, "")
}

// imageCheck reports whether the sandbox image is built. With an unusable
// engine it cannot tell, and says so rather than "not built".
func imageCheck(ctx context.Context, engine, image string, engineErr error) check {
	const name = "sandbox image"
	if engineErr != nil {
		return check{name, statusWarn, image + " not checked: the container engine is not usable", "fix the container engine first"}
	}
	// CheckEngine has its own probe timeout and tells a missing image from
	// an engine that stopped answering.
	if err := sandbox.CheckEngine(ctx, engine, image); err != nil {
		var ee *sandbox.EngineError
		if errors.As(err, &ee) {
			return check{name, statusWarn, ee.Problem(), ee.Hint()}
		}
		return check{name, statusWarn, image + ": " + err.Error(), ""}
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, engine, "image", "inspect", "--format", "{{.Id}}", image).Output()
	if err != nil {
		return check{name, statusWarn, image + ": " + err.Error(), ""}
	}
	id := strings.TrimSpace(string(out))
	return check{name, statusOK, image + " " + id[:min(len(id), 19)], ""}
}

// cbmCheck compares the installed codebase-memory-mcp with the pin.
func cbmCheck(ctx context.Context, binary string) check {
	hint := setupHint("tools") + " (installs the pinned release)"
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
		return check{"llama.cpp GPU", statusWarn, err.Error(), ""}
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--list-devices")
	// Backends are shared libraries next to the binary in a CMake build.
	cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+strings.Trim(filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("LD_LIBRARY_PATH"), string(os.PathListSeparator)))
	b, err := cmd.CombinedOutput()
	if err != nil {
		return check{"llama.cpp GPU", statusWarn, fmt.Sprintf("--list-devices: %v %s", err, firstMeaningfulLine(string(b))),
			"llama.cpp may be too old for --list-devices; rebuild it: " + setupHint("inference", "--force")}
	}
	var devs []string
	for l := range strings.SplitSeq(string(b), "\n") {
		// GPU devices: CUDA0, MTL0 (Metal), Vulkan0.
		if l = strings.TrimSpace(l); strings.HasPrefix(l, "CUDA") || strings.HasPrefix(l, "MTL") || strings.HasPrefix(l, "Vulkan") {
			devs = append(devs, l)
		}
	}
	if len(devs) == 0 {
		hint := "install the NVIDIA driver and CUDA toolkit (nvcc), then rebuild llama.cpp: " + setupHint("inference", "--force")
		if runtime.GOOS == "darwin" {
			hint = "Metal needs an Apple Silicon Mac; on an Intel Mac llama.cpp runs on the CPU"
		}
		return check{"llama.cpp GPU", statusWarn, "no GPU device listed (CPU-only build or driver problem)", hint}
	}
	return check{"llama.cpp GPU", statusOK, strings.Join(devs, "; "), ""}
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
		{"typescript LSP", "typescript-language-server", "`" + buildinfo.Command() + " serena setup` installs its own copy"},
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

// modelFitCheck rates the default model against the machine.
func modelFitCheck(app *App, snap hw.Snapshot) check {
	p, err := app.Models.Get(app.Config.DefaultModel)
	if err != nil {
		return check{"model fit", statusFail, err.Error(), ""}
	}
	f := model.FitFor(p, snap)
	c := check{"model fit", statusOK, fmt.Sprintf("%s: %s (%s)", p.Name, f.Detail, f.Level), ""}
	if !f.Fast() {
		c.Status = statusWarn
		rec := model.Recommend(app.Models, app.Config.DefaultModel, snap)
		c.Hint = "`" + buildinfo.Command() + " model recommend`"
		if rec.Best.Profile != "" && rec.Best.Profile != p.Name {
			c.Hint = fmt.Sprintf("this machine suits %s: `%s model use %s`", rec.Best.Profile, buildinfo.Command(), rec.Best.Profile)
		} else if rec.Best.Profile == "" {
			c.Hint = "no local model fits; use a cloud provider: `" + buildinfo.Command() + " provider use`"
		}
	}
	return c
}
