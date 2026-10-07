package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/akynte/boundedcode/configs"
	"github.com/akynte/boundedcode/internal/buildinfo"
	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/repointel"
	"github.com/akynte/boundedcode/internal/repointel/serena"
	"github.com/akynte/boundedcode/internal/store"
)

// serenaLanguages are validated by setup, status and doctor (ADR-0008).
var serenaLanguages = []string{"go", "typescript"}

// serenaExecutable is the configured command or the setup-managed install.
func (a *App) serenaExecutable() string {
	if c := a.Config.RepoIntel.Serena.Command; c != "" {
		return c
	}
	return serena.DefaultExecutable(a.Paths.Data)
}

// serenaManager builds a process manager. Installation-owned state (the
// executable, language servers) comes from the user's data directory;
// instance homes and logs follow paths, which benchmarks isolate per task.
func (a *App) serenaManager(paths config.Paths) *serena.Manager {
	c := a.Config.RepoIntel.Serena
	return &serena.Manager{Executable: a.serenaExecutable(), Root: filepath.Join(paths.Cache, "serena"),
		LanguageServers: serena.LanguageServersDir(a.Paths.Data), LogDir: filepath.Join(paths.State, "serena"),
		MaxInstances: c.MaxInstances, IdleTimeout: c.IdleTimeout.D(), StartupTimeout: c.StartupTimeout.D(),
		CallTimeout: c.CallTimeout.D(), Log: a.Log}
}

// serenaNavigator returns the navigator for task runs, or nil when Serena
// is disabled or unusable; tasks then use graph-only symbol context.
func (a *App) serenaNavigator(ctx context.Context, paths config.Paths) (*serena.Navigator, func()) {
	if !a.Config.RepoIntel.Serena.Enabled {
		return nil, func() {}
	}
	if runtime.GOOS == "windows" {
		fmt.Fprintf(a.Err, "serena: not supported on Windows yet (continuing without LSP navigation)\n")
		return nil, func() {}
	}
	m := a.serenaManager(paths)
	if err := m.Verify(ctx); err != nil {
		a.Log.Warn("serena enabled but unusable; continuing without LSP navigation", "err", err)
		fmt.Fprintf(a.Err, "serena: %v (continuing without it; see `%s serena status`)\n", err, buildinfo.Command())
		return nil, func() {}
	}
	return &serena.Navigator{M: m}, func() { _ = m.Close() }
}

// serenaChecks reports installation, version, license and (when probe is
// set and the install is supported) MCP start and language servers.
func serenaChecks(ctx context.Context, app *App, probe bool) []check {
	exe := app.serenaExecutable()
	cfg := app.Config.RepoIntel.Serena
	optional := statusWarn
	if !cfg.Enabled {
		optional = statusOK // not installed and not enabled is a valid setup
	}
	state := "disabled"
	if cfg.Enabled {
		state = "enabled"
	}
	inst, err := serena.Detect(ctx, exe)
	if errors.Is(err, serena.ErrNotInstalled) {
		return []check{{"serena", optional, fmt.Sprintf("not installed (%s); %s in config", exe, state),
			"optional LSP symbol navigation: `" + buildinfo.Command() + " serena setup`"}}
	}
	if err != nil {
		return []check{{"serena", statusWarn, err.Error(), "reinstall with `" + buildinfo.Command() + " serena setup --reinstall`"}}
	}
	if err := serena.Check(inst); err != nil {
		return []check{{"serena", statusWarn, fmt.Sprintf("detected %s at %s: not supported", firstNonEmpty(inst.PackageVersion, inst.CLIVersion), inst.Executable),
			err.Error() + ". Your installation is left unchanged; `" + buildinfo.Command() + " serena setup` installs the pinned v" + serena.RequiredVersion + " separately"}}
	}
	out := []check{{"serena", statusOK, fmt.Sprintf("v%s (license %s, LICENSE sha256 verified; commit %.8s), %s (%s)",
		inst.PackageVersion, serena.ExpectedLicense, serena.PinnedCommit, state, inst.Executable), ""}}
	if !probe {
		return out
	}
	m := app.serenaManager(app.Paths)
	m.Log = app.Log
	for _, lang := range serenaLanguages {
		name := "serena " + lang + " LSP"
		if miss := serena.MissingPrerequisites(lang); len(miss) > 0 {
			out = append(out, check{name, statusWarn, "missing " + strings.Join(miss, ", "), prereqHint(lang)})
			continue
		}
		t0 := time.Now()
		if err := serena.ProbeLanguage(ctx, m, lang, false); err != nil {
			out = append(out, check{name, statusWarn, "MCP start or language server failed: " + trunc(err.Error(), 300),
				"run `" + buildinfo.Command() + " serena setup` (installs language servers); logs in " + m.LogDir})
			continue
		}
		out = append(out, check{name, statusOK, fmt.Sprintf("MCP start and symbol lookup ok (%.1fs)", time.Since(t0).Seconds()), ""})
	}
	return out
}

func prereqHint(lang string) string {
	switch lang {
	case "go":
		return "install Go and gopls (`go install golang.org/x/tools/gopls@latest`)"
	case "typescript":
		return "install Node.js and npm; `serena setup` then installs the TypeScript language server"
	}
	return ""
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func newSerenaCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "serena", Short: "Set up and check Serena (optional LSP symbol navigation, pinned to v" + serena.RequiredVersion + ")"}
	var yes, reinstall bool
	setup := &cobra.Command{
		Use:   "setup",
		Short: "Install the pinned, MIT-licensed Serena v" + serena.RequiredVersion + " (with permission) and its language servers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if runtime.GOOS == "windows" {
				return errors.New("the Serena integration is not supported on Windows yet; BoundedCode works without it (it is optional)")
			}
			uv, err := exec.LookPath("uv")
			if err != nil {
				return errors.New("uv not found: install it from https://docs.astral.sh/uv/ (it installs Serena from the locked environment)")
			}
			exe := app.serenaExecutable()
			managed := app.Config.RepoIntel.Serena.Command == ""
			inst, derr := serena.Detect(ctx, exe)
			switch {
			case derr == nil && serena.Check(inst) == nil && !reinstall:
				app.printf("Serena %s already installed at %s\n", inst.PackageVersion, inst.Executable)
			case derr != nil && !errors.Is(derr, serena.ErrNotInstalled) && !managed,
				derr == nil && serena.Check(inst) != nil && !managed:
				// A user-provided installation is never replaced.
				if derr == nil {
					derr = serena.Check(inst)
				}
				return fmt.Errorf("repointel.serena.command %s: %w\nNot changing it. Remove repointel.serena.command to use the pinned install managed by `serena setup`", exe, derr)
			default:
				if !managed {
					return fmt.Errorf("%s not found; set repointel.serena.command to an existing Serena %s or remove it to use `serena setup`", exe, serena.RequiredVersion)
				}
				dir := serena.InstallDir(app.Paths.Data)
				app.printf("Serena %s (MIT, oraios/serena tag v%s, commit %.8s) will be installed from PyPI (serena-agent==%s,\n"+
					"exact versions and hashes from the embedded uv.lock) into\n  %s\n",
					serena.RequiredVersion, serena.RequiredVersion, serena.PinnedCommit, serena.RequiredVersion, dir)
				if !yes && !confirm(app, "Install now? [y/N] ") {
					return errors.New("not installed (pass --yes to install non-interactively)")
				}
				if err := serena.Install(ctx, uv, dir, configs.SerenaEnv, app.Err); err != nil {
					return err
				}
				if inst, err = serena.Detect(ctx, exe); err != nil {
					return err
				}
				if err := serena.Check(inst); err != nil {
					return err
				}
				app.printf("installed Serena %s (%s)\n", inst.PackageVersion, inst.Executable)
			}
			// Language servers: validate Go, install/validate TypeScript. Only
			// this step may use the network (npm).
			m := app.serenaManager(app.Paths)
			failed := 0
			for _, lang := range serenaLanguages {
				if miss := serena.MissingPrerequisites(lang); len(miss) > 0 {
					app.printf("  %-11s skipped: missing %s (%s)\n", lang, strings.Join(miss, ", "), prereqHint(lang))
					continue
				}
				t0 := time.Now()
				if err := serena.ProbeLanguage(ctx, m, lang, true); err != nil {
					failed++
					app.printf("  %-11s FAILED: %v\n", lang, err)
					continue
				}
				app.printf("  %-11s ok (%.1fs)\n", lang, time.Since(t0).Seconds())
			}
			if !app.Config.RepoIntel.Serena.Enabled {
				app.printf("Serena is installed but disabled. Enable it with `repointel.serena.enabled: true` in %s\n", app.configFile())
			}
			if failed > 0 {
				return fmt.Errorf("%d language server check(s) failed; logs in %s", failed, m.LogDir)
			}
			return nil
		},
	}
	setup.Flags().BoolVarP(&yes, "yes", "y", false, "install without asking")
	setup.Flags().BoolVar(&reinstall, "reinstall", false, "re-sync the managed install from the lock file")
	status := &cobra.Command{
		Use:   "status",
		Short: "Show the Serena installation, version and license check, and probe MCP start and language servers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			checks := serenaChecks(cmd.Context(), app, true)
			if app.jsonOut {
				return app.printJSON(checks)
			}
			for _, c := range checks {
				app.printf("[%-4s] %-22s %s\n", c.Status, c.Name, c.Detail)
				if c.Hint != "" && c.Status != statusOK {
					app.printf("       %-22s hint: %s\n", "", c.Hint)
				}
			}
			return nil
		},
	}
	cmd.AddCommand(setup, status)
	return cmd
}

func confirm(app *App, prompt string) bool {
	if app.confirmFn != nil {
		return app.confirmFn(prompt)
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false
	}
	fmt.Fprint(app.Err, prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.EqualFold(strings.TrimSpace(line), "y")
}

// addSerenaIntelCmds adds LSP-backed queries to `intel`. They run against
// the repositories' checkouts (use a task's worktree path with --root).
func addSerenaIntelCmds(app *App, intel *cobra.Command, wsFlag, repoFlag *string) {
	var root string
	run := func(f func(ctx context.Context, nav *serena.Navigator, root string) (any, error)) func(*cobra.Command, []string) error {
		return func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			m := app.serenaManager(app.Paths)
			defer m.Close()
			if err := m.Verify(ctx); err != nil {
				return err
			}
			nav := &serena.Navigator{M: m}
			roots := []string{}
			if root != "" {
				roots = append(roots, root)
			} else {
				w, err := app.resolveWorkspace(ctx, *wsFlag)
				if err != nil {
					return err
				}
				ws, _ := app.workspaces(ctx)
				repos, err := ws.Repos(ctx, w.ID)
				if err != nil {
					return err
				}
				for _, r := range repos {
					if *repoFlag == "" || r.Name == *repoFlag {
						roots = append(roots, r.Path)
					}
				}
			}
			if len(roots) == 0 {
				return fmt.Errorf("no repositories matched: %w", store.ErrNotFound)
			}
			for _, r := range roots {
				v, err := f(ctx, nav, r)
				if err != nil {
					return fmt.Errorf("%s: %w", r, err)
				}
				if app.jsonOut {
					if err := app.printJSON(map[string]any{"root": r, "result": v}); err != nil {
						return err
					}
					continue
				}
				app.printf("## %s\n", r)
				switch x := v.(type) {
				case []repointel.Symbol:
					for _, s := range x {
						app.printf("%s (%s) %s:%d-%d\n", s.NamePath, strings.ToLower(s.Kind), s.File, s.StartLine, s.EndLine)
						if s.Body != "" {
							app.printf("%s\n", s.Body)
						}
					}
				case []repointel.Reference:
					for _, ref := range x {
						app.printf("%s:%d in %s: %s\n", ref.File, ref.Line, ref.Symbol, ref.Snippet)
					}
				}
			}
			return nil
		}
	}
	first := func(ctx context.Context, nav *serena.Navigator, root, name string) (repointel.Symbol, bool, error) {
		syms, err := nav.FindSymbol(ctx, root, name, repointel.FindOptions{})
		if err != nil || len(syms) == 0 {
			return repointel.Symbol{}, false, err
		}
		return syms[0], true, nil
	}
	var body bool
	sym := &cobra.Command{Use: "symbol NAME", Args: cobra.ExactArgs(1), Short: "Find a symbol (Serena/LSP)",
		RunE: func(c *cobra.Command, a []string) error {
			return run(func(ctx context.Context, nav *serena.Navigator, r string) (any, error) {
				return nav.FindSymbol(ctx, r, a[0], repointel.FindOptions{IncludeBody: body})
			})(c, a)
		}}
	sym.Flags().BoolVar(&body, "body", false, "include the symbol's source")
	refs := &cobra.Command{Use: "refs NAME", Args: cobra.ExactArgs(1), Short: "Symbols referencing NAME (Serena/LSP)",
		RunE: func(c *cobra.Command, a []string) error {
			return run(func(ctx context.Context, nav *serena.Navigator, r string) (any, error) {
				s, ok, err := first(ctx, nav, r, a[0])
				if !ok {
					return []repointel.Reference{}, err
				}
				return nav.References(ctx, r, s)
			})(c, a)
		}}
	impls := &cobra.Command{Use: "impls NAME", Args: cobra.ExactArgs(1), Short: "Implementations of an interface (Serena/LSP)",
		RunE: func(c *cobra.Command, a []string) error {
			return run(func(ctx context.Context, nav *serena.Navigator, r string) (any, error) {
				s, ok, err := first(ctx, nav, r, a[0])
				if !ok {
					return []repointel.Symbol{}, err
				}
				return nav.Implementations(ctx, r, s)
			})(c, a)
		}}
	for _, c := range []*cobra.Command{sym, refs, impls} {
		c.Flags().StringVar(&root, "root", "", "checkout to analyze (e.g. a task worktree) instead of workspace repositories")
		intel.AddCommand(c)
	}
}
