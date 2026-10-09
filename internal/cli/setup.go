package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode"
	"github.com/akynte/boundedcode/internal/buildinfo"
	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/inference/llamacpp"
	"github.com/akynte/boundedcode/internal/install"
	"github.com/akynte/boundedcode/internal/model"
	"github.com/akynte/boundedcode/internal/repointel/cbm"
	"github.com/akynte/boundedcode/internal/sandbox"
)

// setupStep is one prerequisite: check reports whether it is satisfied and
// run satisfies it. Steps are ordered; later steps may depend on earlier ones.
type setupStep struct {
	Name, Title string
	// Ask, when set, is shown before run and must be confirmed (downloads,
	// long builds).
	Ask   func(a *App) string
	check func(ctx context.Context, a *App) (ok bool, detail string)
	run   func(ctx context.Context, a *App) error
}

// setupState is the outcome of a step's check.
type setupState struct {
	Name   string `json:"name"`
	Title  string `json:"title"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// setupHint tells the user to run one setup step, naming the program as
// they invoked it (e.g. "run `bcode setup --only tools`"), with any extra
// flags.
func setupHint(step string, flags ...string) string {
	return "run `" + strings.Join(append([]string{buildinfo.Command(), "setup", "--only", step}, flags...), " ") + "`"
}

// toolsDir holds tools installed by setup; it is put on PATH at start-up.
func (a *App) toolsDir() string { return filepath.Join(a.Paths.Data, "bin") }

// llamaPrefix is where setup builds llama.cpp (scripts/build-llama-cpp.sh).
func (a *App) llamaPrefix() string {
	return filepath.Join(a.Paths.Data, "runtimes", "llama.cpp", "v0.5.0")
}

func setupSteps() []setupStep {
	return []setupStep{
		{
			Name: "config", Title: "Configuration",
			check: func(_ context.Context, a *App) (bool, string) {
				if _, err := os.Stat(a.configFile()); err != nil {
					return false, "not created yet"
				}
				return true, a.configFile()
			},
			run: func(ctx context.Context, a *App) error {
				cfg := config.Defaults()
				cfg.ModelsDir = filepath.Join(a.Paths.Data, "models")
				if p := a.findLlamaServer(); p != "" {
					cfg.Inference.ServerBinary = p
					cfg.Inference.BenchBinary = filepath.Join(filepath.Dir(p), "llama-bench")
				}
				if err := config.Save(a.configFile(), cfg); err != nil {
					return err
				}
				a.Config = cfg
				a.printf("wrote %s\n", a.configFile())
				return nil
			},
		},
		{
			Name: "tools", Title: "Repository tools (codebase-memory-mcp, gitleaks)",
			Ask: func(a *App) string {
				return "Download the pinned codebase-memory-mcp and gitleaks releases from GitHub (checksum-verified, a few MB) into " + a.toolsDir() + "?"
			},
			check: func(ctx context.Context, a *App) (bool, string) {
				var missing []string
				if _, err := cbm.CheckVersion(ctx, a.Config.RepoIntel.Binary); err != nil {
					missing = append(missing, "codebase-memory-mcp")
				}
				if _, err := exec.LookPath("gitleaks"); err != nil {
					missing = append(missing, "gitleaks")
				}
				if len(missing) > 0 {
					return false, "missing " + strings.Join(missing, ", ")
				}
				return true, "installed"
			},
			run: func(ctx context.Context, a *App) error {
				var tools []string
				if _, err := cbm.CheckVersion(ctx, a.Config.RepoIntel.Binary); err != nil {
					tools = append(tools, "codebase-memory-mcp")
				}
				if _, err := exec.LookPath("gitleaks"); err != nil {
					tools = append(tools, "gitleaks")
				}
				for _, t := range tools {
					pins := install.Gitleaks
					if t == "codebase-memory-mcp" {
						pins = install.CBM
					}
					if err := a.installTool(ctx, t, pins); err != nil {
						return err
					}
				}
				return nil
			},
		},
		{
			Name: "inference", Title: "Local inference server (llama.cpp)",
			Ask: func(a *App) string {
				return llamaPlan() + " into " + a.llamaPrefix() +
					"? (To use a server you already run, set inference.mode: external and inference.external_url in " + a.configFile() +
					" instead. To use a cloud model API instead of a local model, answer no and run `" + buildinfo.Command() + " provider use NAME`; this step and the model download are then skipped.)"
			},
			check: func(ctx context.Context, a *App) (bool, string) {
				c := a.Config.Inference
				if c.IsCloud() {
					return true, "not needed: using " + a.providerLabel()
				}
				if c.Mode == "external" {
					ok, _ := inference.NewClient(c.ExternalURL, 3*time.Second).Healthy(ctx)
					if !ok {
						return true, "external server " + c.ExternalURL + " (not answering right now)"
					}
					return true, "external server " + c.ExternalURL
				}
				v, err := llamacpp.Version(ctx, c.ServerBinary)
				if err != nil {
					return false, "llama-server not found (" + c.ServerBinary + "); or use a cloud model API: `" + buildinfo.Command() + " provider use NAME`"
				}
				return true, v
			},
			run: func(ctx context.Context, a *App) error {
				bin := filepath.Join(a.llamaPrefix(), "bin")
				if useLlamaSourceBuild() {
					// Linux with a compiler: the validated, locally tuned build.
					if err := a.runSetupScript(ctx, "build-llama-cpp.sh", install.LlamaTag, a.llamaPrefix()); err != nil {
						return err
					}
				} else {
					var err error
					if bin, err = a.installLlamaPrebuilt(ctx); err != nil {
						return err
					}
				}
				return a.updateConfig(func(c *config.Config) {
					c.Inference.ServerBinary = filepath.Join(bin, exeName("llama-server"))
					c.Inference.BenchBinary = filepath.Join(bin, exeName("llama-bench"))
				})
			},
		},
		{
			Name: "model", Title: "Default model weights",
			Ask: func(a *App) string {
				p, err := a.Models.Get(a.Config.DefaultModel)
				if err != nil {
					return "Download the default model?"
				}
				msg := fmt.Sprintf("Download %s (%s) from huggingface.co/%s at a pinned revision into %s? %s",
					p.DisplayName, formatGB(p.Source.SizeBytes), p.Source.Repo, a.modelsDir(), licenseNotice(p))
				if f := model.FitFor(p, a.hardware(context.Background())); !f.Fast() {
					msg += fmt.Sprintf(" Note: %s (%s); `%s model recommend` suggests a model for this machine.", f.Detail, f.Level, buildinfo.Command())
				}
				return msg
			},
			check: func(_ context.Context, a *App) (bool, string) {
				if a.Config.Inference.IsCloud() {
					return true, "not needed: using " + a.providerLabel()
				}
				if a.Config.Inference.Mode == "external" {
					return true, "served by the external server"
				}
				p, err := a.Models.Get(a.Config.DefaultModel)
				if err != nil {
					return false, err.Error()
				}
				path := p.ResolveFile(a.modelsDir())
				fi, err := os.Stat(path)
				if err != nil {
					return false, fmt.Sprintf("%s not downloaded (%s); or use a cloud model API: `%s provider use NAME`", p.Name, formatGB(p.Source.SizeBytes), buildinfo.Command())
				}
				return true, fmt.Sprintf("%s (%.1f GiB)", p.Name, float64(fi.Size())/(1<<30))
			},
			run: func(ctx context.Context, a *App) error {
				p, err := a.Models.Get(a.Config.DefaultModel)
				if err != nil {
					return err
				}
				if p.Status == model.StatusReview {
					return fmt.Errorf("%s's license is under review; choose another model with `%s model use`", p.Name, buildinfo.Command())
				}
				path, err := a.fetchModel(ctx, p)
				if err != nil {
					return err
				}
				a.printf("downloaded and verified: %s\n", path)
				return nil
			},
		},
		{
			Name: "sandbox", Title: "Agent sandbox image (Docker)",
			Ask: func(a *App) string {
				return "Build the agent sandbox image " + a.Config.Agent.Image + " locally with " + a.Config.Sandbox.Engine +
					"? It downloads pinned base images with the toolchains verification uses (about 5 GB) and takes a few minutes. It is never pushed."
			},
			check: func(ctx context.Context, a *App) (bool, string) {
				c := a.Config.Sandbox
				if c.Kind != "docker" {
					return true, "sandbox.kind is " + c.Kind + " (unsandboxed)"
				}
				var ee *sandbox.EngineError
				if err := sandbox.CheckEngine(ctx, c.Engine, a.Config.Agent.Image); errors.As(err, &ee) {
					return false, ee.Problem()
				} else if err != nil {
					return false, err.Error()
				}
				if !sandboxImageCurrent(ctx, c.Engine, a.Config.Agent.Image) {
					// Built from an older definition: it may lack toolchains
					// that verification presets now use.
					return false, a.Config.Agent.Image + " was built from an older sandbox definition; it needs a rebuild"
				}
				return true, a.Config.Agent.Image
			},
			run: func(ctx context.Context, a *App) error {
				return a.buildSandboxImage(ctx, "", a.Out)
			},
		},
		{
			Name: "frontier", Title: "Frontier container (Codex for Linux)",
			Ask: func(a *App) string {
				return fmt.Sprintf("Download the Linux build of the Codex CLI %s (about 100 MB, checksum-verified) for the contained frontier route? This machine's own codex cannot run inside the Linux container.", install.CodexVersion)
			},
			check: func(_ context.Context, a *App) (bool, string) {
				if !a.needsContainerCodex() {
					return true, "not needed"
				}
				if _, err := os.Stat(a.containerCodex()); err != nil {
					return false, "the contained frontier route needs a Linux codex"
				}
				return true, a.containerCodex()
			},
			run: func(ctx context.Context, a *App) error { return a.installContainerCodex(ctx) },
		},
	}
}

// buildSandboxImage builds the agent sandbox image from dir, or from the
// build context embedded in the binary when dir is empty (no checkout
// needed).
func (a *App) buildSandboxImage(ctx context.Context, dir string, out io.Writer) error {
	// The image is what is built here; the engine itself must work.
	if err := sandbox.CheckEngine(ctx, a.Config.Sandbox.Engine, ""); err != nil {
		return err
	}
	if dir == "" {
		tmp, err := os.MkdirTemp("", "bc-sandbox-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		if err := extract(boundedcode.SandboxContext, tmp); err != nil {
			return err
		}
		dir = filepath.Join(tmp, "adapters", "openhands")
	}
	cmd := exec.CommandContext(ctx, a.Config.Sandbox.Engine, "build", "--label", sandboxContextLabel+"="+sandboxContextHash(dir),
		"-t", a.Config.Agent.Image, dir)
	cmd.Stdout, cmd.Stderr = out, out
	return cmd.Run()
}

// sandboxContextLabel records which sandbox definition an image was built
// from, so setup can tell an outdated image from a current one.
const sandboxContextLabel = "io.boundedcode.sandbox-context"

// sandboxContextHash fingerprints the sandbox build context in dir (the
// files embedded as boundedcode.SandboxContext); with dir empty, the
// embedded ones.
func sandboxContextHash(dir string) string {
	h := sha256.New()
	_ = fs.WalkDir(boundedcode.SandboxContext, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		var b []byte
		if dir == "" {
			b, err = fs.ReadFile(boundedcode.SandboxContext, p)
		} else {
			b, err = os.ReadFile(filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(p, "adapters/openhands/"))))
		}
		if err != nil {
			b = nil
		}
		fmt.Fprintf(h, "%s\x00%d\x00", p, len(b))
		h.Write(b)
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// sandboxImageCurrent reports whether image was built from this binary's
// sandbox definition. An image without the label predates it.
func sandboxImageCurrent(ctx context.Context, engine, image string) bool {
	out, err := exec.CommandContext(ctx, engine, "image", "inspect", "-f", `{{index .Config.Labels "`+sandboxContextLabel+`"}}`, image).Output()
	return err == nil && strings.TrimSpace(string(out)) == sandboxContextHash("")
}

// checkSetup reports every step.
func (a *App) checkSetup(ctx context.Context) []setupState {
	var out []setupState
	for _, s := range setupSteps() {
		ok, detail := s.check(ctx, a)
		out = append(out, setupState{s.Name, s.Title, ok, detail})
	}
	return out
}

// findLlamaServer locates an existing llama-server: on PATH or built by
// setup or scripts/build-llama-cpp.sh.
func (a *App) findLlamaServer() string {
	if p, err := exec.LookPath("llama-server"); err == nil {
		return p
	}
	matches, _ := filepath.Glob(filepath.Join(a.Paths.Data, "runtimes", "llama.cpp", "*", "bin", "llama-server"))
	slices.Sort(matches)
	if len(matches) > 0 {
		return matches[len(matches)-1]
	}
	return ""
}

func (a *App) updateConfig(f func(*config.Config)) error {
	cfg, err := config.Load(a.configFile())
	if err != nil {
		return err
	}
	f(&cfg)
	if err := config.Save(a.configFile(), cfg); err != nil {
		return err
	}
	a.Config = cfg
	return nil
}

// runSetupScript runs one of the embedded pinned installers with bash.
func (a *App) runSetupScript(ctx context.Context, name string, args ...string) error {
	b, err := boundedcode.SetupScripts.ReadFile("scripts/" + name)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp("", "bc-"+name)
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "bash", append([]string{f.Name()}, args...)...)
	cmd.Stdout, cmd.Stderr = a.Out, a.Out
	cmd.Env = append(os.Environ(), "XDG_DATA_HOME="+filepath.Dir(a.Paths.Data))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// extract writes an embedded tree under dir.
func extract(fsys fs.FS, dir string) error {
	return fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, p)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
}

func newSetupCmd(app *App) *cobra.Command {
	var yes, check, force bool
	var only []string
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Install and configure everything BoundedCode needs (asks before downloads and builds)",
		Long: `Checks and, with your permission, satisfies each prerequisite in order:
configuration, repository tools, the llama.cpp inference server, the default
model weights and the agent sandbox image. Steps already satisfied are
skipped, so setup can be re-run at any time; --force with --only runs the
named steps again (for example --only inference to rebuild llama.cpp after
installing the CUDA toolkit). Everything is pinned and checksum-verified, and
installed under your user directories.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := app.Paths.Ensure(); err != nil {
				return err
			}
			names := make([]string, 0, len(setupSteps()))
			for _, s := range setupSteps() {
				names = append(names, s.Name)
			}
			for _, o := range only {
				if !slices.Contains(names, o) {
					return fmt.Errorf("unknown setup step %q (steps: %s)", o, strings.Join(names, ","))
				}
			}
			if force && len(only) == 0 {
				return errors.New("--force needs --only: name the steps to run again")
			}
			if force && slices.Contains(only, "config") {
				return errors.New("--force does not rewrite the configuration; edit it, or remove it and run setup")
			}
			if check {
				st := app.checkSetup(ctx)
				todo := 0
				for _, s := range st {
					if !s.OK {
						todo++
					}
				}
				if app.jsonOut {
					if err := app.printJSON(st); err != nil {
						return err
					}
				} else {
					for _, s := range st {
						mark := "ok  "
						if !s.OK {
							mark = "todo"
						}
						app.printf("[%s] %-48s %s\n", mark, s.Title, s.Detail)
					}
				}
				// A non-zero exit lets scripts and CI tell a complete set-up
				// from an incomplete one.
				if todo > 0 {
					return fmt.Errorf("%d setup step(s) to do: run `%s setup`", todo, buildinfo.Command())
				}
				return nil
			}
			failed := 0
			for _, s := range setupSteps() {
				if len(only) > 0 && !slices.Contains(only, s.Name) {
					continue
				}
				ok, detail := s.check(ctx, app)
				if ok && !force {
					app.printf("✔ %s: %s\n", s.Title, detail)
					continue
				}
				if ok {
					detail += " (running again: --force)"
				}
				app.printf("• %s: %s\n", s.Title, detail)
				if s.Ask != nil && !yes && !confirm(app, s.Ask(app)+" [y/N] ") {
					app.printf("  skipped (run `setup` again to continue)\n")
					failed++
					continue
				}
				if err := s.run(ctx, app); err != nil {
					app.printf("✘ %s: %v\n", s.Title, err)
					failed++
					if ctx.Err() != nil {
						return ctx.Err()
					}
					continue
				}
				// Configuration changes must be visible to the next steps.
				if err := app.load(); err != nil {
					return err
				}
				if ok, detail := s.check(ctx, app); ok {
					app.printf("✔ %s: %s\n", s.Title, detail)
				} else {
					app.printf("✘ %s: still not satisfied: %s\n", s.Title, detail)
					failed++
				}
			}
			if failed > 0 {
				return fmt.Errorf("%d setup step(s) not completed", failed)
			}
			app.printf("BoundedCode is ready.\n")
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask before downloads and builds")
	cmd.Flags().BoolVar(&check, "check", false, "only report what is missing")
	cmd.Flags().StringSliceVar(&only, "only", nil, "run only these steps: config,tools,inference,model,sandbox,frontier")
	cmd.Flags().BoolVar(&force, "force", false, "with --only: run the named steps even if they look complete (not config)")
	return cmd
}
