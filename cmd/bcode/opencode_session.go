package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/llm"
	"github.com/akynte/boundedcode/internal/opencode"
	"github.com/akynte/boundedcode/internal/sandbox"
	bcodeSession "github.com/akynte/boundedcode/internal/session"
	"github.com/akynte/boundedcode/internal/setup"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/supervisor"
)

type openCodeLaunchOptions struct {
	Unconfined bool
	Budget     bool
	// InitializeWorkspace is true for the user-facing paths. The explicit
	// `opencode setup` subcommand never calls this function.
	InitializeWorkspace bool
}

// launchOpenCode is the single session entry point for both `bcode opencode`
// and `bcode opencode run`. Keeping the lifecycle here is what makes the short
// command and the explicit command obey the same cleanup contract.
func launchOpenCode(cmd *cobra.Command, args []string, opts openCodeLaunchOptions) error {
	ctx := cmd.Context()
	if opts.InitializeWorkspace {
		if err := initWorkspaceIfNeeded(cmd); err != nil {
			return err
		}
	}
	// Project setup is idempotent and must happen before the model starts: the
	// generated OpenCode configuration is part of what the session is meant to
	// launch, not a post-launch repair.
	if err := setupOpenCode(cmd, "", false); err != nil {
		return err
	}

	ws, root, st, err := openWorkspace(ctx)
	if err != nil {
		return err
	}
	defer closeRoot(cmd, root)
	cfg, err := loadConfig(root)
	if err != nil {
		return err
	}
	if cfg.Inference.Mode == config.ModeNone || cfg.Inference.Mode == "" {
		return fmt.Errorf("BoundedCode is not configured for inference; run `bcode setup` first")
	}
	baseURL, configuredModel := inferenceEndpoint(cfg, root)
	if baseURL == "" {
		return fmt.Errorf("BoundedCode has no inference endpoint; run `bcode setup` first")
	}

	binary, err := exec.LookPath("opencode")
	if err != nil {
		return fmt.Errorf("OpenCode is not on PATH; install OpenCode 2 or run `bcode setup` again: %w", err)
	}
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating the BoundedCode executable: %w", err)
	}
	if err := ensureSessionRuntimeInputs(root, cfg); err != nil {
		return err
	}

	// The supervisor owns the model and the API for exactly this session. It
	// is started before runtime validation so validation observes the same
	// endpoint OpenCode will use, not a separately launched server.
	sessionRoot, err := makeSessionRoot(st)
	if err != nil {
		return err
	}
	runtime, err := bcodeSession.Start(ctx, bcodeSession.RuntimeOptions{
		Binary: self, DataDir: root.Layout().Root(), Dir: ws.Root,
		Env: setup.CredentialEnvironment(nil, root.Layout().Root()), LogPath: filepath.Join(sessionRoot, "supervisor.log"),
		RemoveLog: true, StartTimeout: runtimeStartTimeout(cfg), StopTimeout: runtimeStopTimeout(cfg),
	})
	if err != nil {
		_ = bcodeSession.RemoveOpenCodeSessionRoot(sessionRoot)
		return fmt.Errorf("starting the BoundedCode session runtime: %w", err)
	}
	cleanupRuntime := true
	defer func() {
		if cleanupRuntime {
			_ = runtime.Close(context.Background())
		}
		_ = bcodeSession.RemoveOpenCodeSessionRoot(sessionRoot)
	}()

	if cfg.Inference.Mode == config.ModeEmbedded {
		if _, err := opencode.ValidateRuntime(ctx, ws.Root, baseURL); err != nil {
			return fmt.Errorf("OpenCode runtime validation failed: %w", err)
		}
	} else if _, err := opencode.ValidateExternalRuntime(ctx, ws.Root, baseURL); err != nil {
		return fmt.Errorf("OpenCode external runtime validation failed: %w", err)
	}

	dirs, err := st.TaskDirs()
	if err != nil {
		return err
	}
	sessionHome := filepath.Join(sessionRoot, "home")
	controlRoot := filepath.Join(sessionRoot, "control")
	sessionTmp := filepath.Join(sessionRoot, "tmp")
	persistentData := filepath.Join(st.OpenCodeDir(), "data")
	persistentState := filepath.Join(st.OpenCodeDir(), "state")
	pluginDir := filepath.Join(st.OpenCodeDir(), "opencode-plugin")
	if err := bcodeSession.EnsureOpenCodeSessionDirs(sessionHome, controlRoot, sessionTmp, persistentData, persistentState, pluginDir); err != nil {
		return err
	}
	allowedModel := ""
	if configuredModel != "" {
		allowedModel = opencode.ModelID(configuredModel)
	}
	if _, _, err := opencode.RegisterAgent(ws.Root); err != nil {
		return err
	}
	session := opencode.Session{
		Binary: binary, Repo: ws.Root, StateDir: sessionHome, HomeDir: sessionHome,
		DataHome: persistentData, StateHome: persistentState, ControlDir: controlRoot,
		PluginDir: pluginDir, TmpDir: sessionTmp, Budget: opts.Budget,
	}
	spec, err := session.Confine(supervisor.BaseSandboxSpec(cfg, dirs))
	if err != nil {
		return err
	}
	if cfg.Inference.Mode == config.ModeExternal {
		endpoint, err := url.Parse(baseURL)
		if err != nil {
			return err
		}
		if !isLocalHostname(endpoint.Hostname()) {
			return fmt.Errorf("confined OpenCode requires a local inference endpoint, got %q", endpoint.Hostname())
		}
		port, err := strconv.ParseUint(endpoint.Port(), 10, 16)
		if err != nil || port == 0 {
			return fmt.Errorf("confined OpenCode requires an explicit local inference port")
		}
		spec.TCPConnect = append(spec.TCPConnect, uint16(port))
	}
	if err := st.EnsureSandboxDirs(spec.ReadWrite); err != nil {
		return err
	}
	binDir, err := opencode.PrepareBCodeBinary(controlRoot, self)
	if err != nil {
		return err
	}
	session.BCodeBinDir = binDir + string(os.PathListSeparator) + filepath.Dir(self)
	brokerPath, stopBroker, err := startOpenCodeBrokerWithRoute(ctx, st, root.Layout().Root(), ws.Root,
		controlRoot, self, opencode.ProviderName, allowedModel)
	if err != nil {
		return err
	}
	defer stopBroker()
	capPath, err := opencode.WriteBrokerCapability(controlRoot, brokerPath)
	if err != nil {
		return err
	}
	defer func() { _ = opencode.RemoveBrokerCapability(capPath) }()
	session.BrokerCapability = brokerPath
	spec.Env = session.Env()

	runner, report := supervisor.SelectSandbox(ctx, cfg)
	if runner == nil || (len(report.Active) == 1 && report.Active[0] == sandbox.LayerContainer && !inContainer()) {
		if !opts.Unconfined {
			return fmt.Errorf("no sandbox layer is available on this host, so the session would run unconfined with your whole environment:\n%s\nRun `bcode doctor` to see why, or pass --unconfined to accept it", inactiveReasons(report))
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "WARNING: starting unconfined. The shell's own permissions are not a boundary.")
	}

	argv := append([]string{binary}, args...)
	if len(args) == 0 {
		argv = append(argv, "--standalone")
	}
	child, err := runner.Command(ctx, spec, argv...)
	if err != nil {
		return err
	}
	bcodeSession.SetParentDeathSignal(child)
	child.Stdin, child.Stdout, child.Stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
	if err := child.Start(); err != nil {
		return fmt.Errorf("starting OpenCode: %w", err)
	}
	// If the supervisor dies unexpectedly, do not leave an editor running on
	// a dead model/API boundary. Normal shutdown closes Done after the child
	// has already exited, so this monitor is inert on the ordinary path.
	go func() {
		<-runtime.Done()
		if child.Process != nil {
			_ = child.Process.Kill()
		}
	}()
	fmt.Fprintf(cmd.OutOrStdout(), "BoundedCode session ready: runtime=%s, sandbox=%s\n", runtime.Addr(), report.Runner)
	fmt.Fprintf(cmd.OutOrStdout(), "OpenCode is running in %s. Exit it to release the model, VRAM, and temporary session resources.\n", ws.Name())

	runErr := child.Wait()
	// The session's exit status belongs to OpenCode. Cleanup is unconditional
	// below; a normal OpenCode exit therefore releases the LLM even when the
	// editor itself reports a failure.
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) && !errors.Is(runErr, context.Canceled) {
			return fmt.Errorf("running OpenCode: %w", runErr)
		}
	}
	if err := runtime.Close(context.Background()); err != nil {
		return fmt.Errorf("cleaning up the BoundedCode session runtime: %w", err)
	}
	cleanupRuntime = false
	return nil
}

func makeSessionRoot(st *store.Store) (string, error) {
	return bcodeSession.MakeOpenCodeSessionRoot(st.OpenCodeDir())
}

func ensureSessionRuntimeInputs(root *store.Root, cfg config.Config) error {
	if providers, err := llm.LoadProvidersFile(root.Layout().ConfigDir()); err == nil {
		for _, provider := range providers.Providers {
			if provider.Process != nil {
				return fmt.Errorf("provider %q declares its own process; managed bcode opencode owns the session runtime, so remove the provider process stanza or use an external endpoint", provider.Name)
			}
		}
	}
	if cfg.Inference.Mode == config.ModeExternal {
		if strings.TrimSpace(cfg.Inference.BaseURL) == "" {
			return fmt.Errorf("external inference is selected but no base URL is configured")
		}
		return nil
	}
	if strings.TrimSpace(cfg.Inference.Binary) == "" {
		return fmt.Errorf("embedded inference is selected but no llama-server executable is configured; run `bcode setup`")
	}
	if info, err := os.Stat(cfg.Inference.Binary); err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		return fmt.Errorf("configured llama-server runtime %s is not executable; run `bcode setup`", cfg.Inference.Binary)
	}
	model := cfg.Inference.Model
	if !filepath.IsAbs(model) {
		model = filepath.Join(root.Layout().ModelsDir(), model)
	}
	info, err := os.Stat(model)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return fmt.Errorf("configured model %s is missing or empty; run `bcode setup`", model)
	}
	return nil
}

func runtimeStartTimeout(cfg config.Config) time.Duration {
	seconds := cfg.Inference.StartTimeoutSeconds
	if seconds <= 0 {
		seconds = 300
	}
	// API startup and a cold model load share one budget. Leave a small margin
	// for the readiness request and process startup.
	return time.Duration(seconds+15) * time.Second
}

func runtimeStopTimeout(cfg config.Config) time.Duration {
	seconds := cfg.API.ShutdownGraceSeconds
	if seconds <= 0 {
		seconds = 30
	}
	return time.Duration(seconds+10) * time.Second
}

func isLocalHostname(host string) bool {
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}
