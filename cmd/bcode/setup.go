package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/setup"
)

// newSetupCmd is the one supported installation/configuration entry point.
//
// The UI intentionally lives at the command boundary: setup is an operator
// experience, while the state transitions it performs live in internal/setup
// and are shared with non-interactive callers and tests.
func newSetupCmd() *cobra.Command {
	var (
		dataDir        string
		nonInteractive bool
		assumeYes      bool
		downloadModel  bool
		skipJudgment   bool
		modelURL       string
		modelSHA256    string
		runtimeBinary  string
		installRuntime bool
		modelPath      string
		externalURL    string
		providerModel  string
		profile        string
		judgmentKey    string
		force          bool
		asJSON         bool
	)
	cmd := &cobra.Command{
		Use:     "setup",
		Aliases: []string{"install", "init"},
		Short:   "Install and configure BoundedCode in one guided TUI",
		Long: "Set up BoundedCode from one guided terminal flow.\n\n" +
			"The setup checks dependencies, chooses a hardware profile, configures the\n" +
			"model/runtime, prepares the decision plane, writes the data directory, and\n" +
			"validates the result. It is safe to run again: existing values are kept unless\n" +
			"you explicitly choose to replace them.\n\n" +
			"After setup, enter any project and run `bcode opencode`. That command owns the\n" +
			"short-lived runtime and cleans it up when OpenCode exits.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dataDir != "" && g.dataDir != "" && filepath.Clean(dataDir) != filepath.Clean(g.dataDir) {
				return fmt.Errorf("conflicting data directories: --data-dir %s and --data %s", dataDir, g.dataDir)
			}
			opts := setup.Options{
				DataDir: dataDir, NonInteractive: nonInteractive, AssumeYes: assumeYes,
				DownloadModel: downloadModel, SkipJudgment: skipJudgment, ModelURL: modelURL,
				ModelSHA256: modelSHA256, RuntimeBinary: runtimeBinary, InstallRuntime: installRuntime, ModelPath: modelPath,
				ExternalURL: externalURL, ProviderModel: providerModel, Profile: profile, APIKey: judgmentKey, Force: force,
				JSON: asJSON,
			}
			if opts.DataDir == "" {
				opts.DataDir = g.dataDir
			}
			report, err := runSetupTUI(cmd, opts)
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(report)
			}
			return nil
		},
	}
	pf := cmd.Flags()
	pf.StringVar(&dataDir, "data-dir", "", "data directory (defaults to $BC_DATA or ~/.local/share/boundedcode)")
	pf.BoolVar(&nonInteractive, "non-interactive", false, "never prompt; use discovered values and flags")
	pf.BoolVarP(&assumeYes, "yes", "y", false, "accept defaults, authorize the pinned runtime build, and download the reference model when needed")
	pf.BoolVar(&downloadModel, "download-model", false, "download the reference GGUF when it is not present")
	pf.StringVar(&modelURL, "model-url", setup.DefaultModelURL, "model download URL")
	pf.StringVar(&modelSHA256, "model-sha256", setup.DefaultModelSHA256, "expected SHA-256 for a downloaded model")
	pf.StringVar(&runtimeBinary, "runtime", "", "llama-server executable to manage")
	pf.BoolVar(&installRuntime, "install-runtime", false, "build the pinned Prism llama-server runtime when no runtime is found (skipped with --external-url)")
	pf.StringVar(&modelPath, "model", "", "existing GGUF path or file under the data directory")
	pf.StringVar(&externalURL, "external-url", "", "configure an already-running local OpenAI-compatible endpoint instead")
	pf.StringVar(&providerModel, "provider-model", "", "model/alias exposed by an external endpoint (default: local)")
	pf.StringVar(&profile, "profile", "", "hardware profile name")
	pf.StringVar(&judgmentKey, "judgment-key", "", "TypeSafe API key; prefer TYPESAFE_API_KEY or the owner-only credential file")
	pf.BoolVar(&skipJudgment, "skip-judgment", false, "do not configure the hosted decision plane")
	pf.BoolVar(&force, "force", false, "replace generated configuration files when they already exist")
	pf.BoolVar(&asJSON, "json", false, "print the completed setup report as JSON")
	return cmd
}

func runSetupTUI(cmd *cobra.Command, opts setup.Options) (setup.Report, error) {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()
	if opts.JSON {
		out = io.Discard
	}
	in := cmd.InOrStdin()
	reader := bufio.NewReader(in)
	dataDir := setup.DataDir(opts.DataDir)
	// JSON output is a machine-facing mode: never hide a prompt behind
	// io.Discard or leave a TTY invocation apparently hung.
	interactive := !opts.JSON && !opts.NonInteractive && isReaderTerminal(in) && !opts.AssumeYes

	printSetupHeader(out, dataDir)
	printSetupChecks(out, setup.Detect(ctx))

	choice := setup.DefaultChoice(dataDir)
	if existing, err := loadSetupConfig(dataDir); err == nil {
		mergeExistingChoice(&choice, existing, dataDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return setup.Report{}, fmt.Errorf("load existing setup configuration: %w", err)
	}
	if opts.RuntimeBinary != "" {
		choice.RuntimeBinary = resolveRuntimePath(opts.RuntimeBinary)
		if opts.ExternalURL == "" {
			choice.Mode = config.ModeEmbedded
			choice.BaseURL = ""
		}
	}
	if opts.ModelPath != "" {
		choice.ModelPath = resolveModelPath(dataDir, opts.ModelPath)
		choice.ModelName = filepath.Base(choice.ModelPath)
		choice.ProviderModel = strings.TrimSuffix(choice.ModelName, ".gguf")
		if opts.ExternalURL == "" {
			choice.Mode = config.ModeEmbedded
			choice.BaseURL = ""
		}
	}
	if opts.Profile != "" {
		choice.Profile = opts.Profile
	}
	if opts.ExternalURL != "" {
		choice.Mode = config.ModeExternal
		choice.BaseURL = strings.TrimRight(opts.ExternalURL, "/")
		choice.ProviderModel = firstNonEmpty(opts.ProviderModel, "local")
	}
	if opts.InstallRuntime && opts.ExternalURL == "" {
		// An explicit installation request selects the session-owned embedded
		// route. A pre-existing external configuration remains external unless
		// the caller supplied this flag or another embedded-mode override.
		choice.Mode = config.ModeEmbedded
		choice.BaseURL = ""
	}
	if opts.AssumeYes && choice.Mode == config.ModeNone && opts.ExternalURL == "" {
		// A fresh host with no discovered runtime still has a deterministic
		// embedded default for unattended setup. An existing external profile
		// is preserved by the condition above.
		choice.Mode = config.ModeEmbedded
		choice.BaseURL = ""
	}
	if opts.DataDir != "" {
		choice.DataDir = dataDir
	}

	if interactive {
		if err := setupConversation(cmd, reader, &choice, opts); err != nil {
			return setup.Report{}, err
		}
	} else {
		// Non-interactive mode still gets the same deterministic choices and
		// the same validation; it simply never waits for a person.
		if err := checkEmbeddedRuntimeRequest(&choice, opts); err != nil {
			return setup.Report{}, err
		}
		if choice.Mode == config.ModeEmbedded && (opts.DownloadModel || opts.AssumeYes) && choice.ModelPath == "" {
			model, err := setup.DownloadModel(ctx, dataDir, firstNonEmpty(opts.ModelURL, setup.DefaultModelURL),
				setup.DefaultModelName, opts.ModelSHA256, progressWriter(out))
			if err != nil {
				return setup.Report{}, err
			}
			choice.ModelPath = model
			choice.ModelName = filepath.Base(model)
			choice.ProviderModel = strings.TrimSuffix(choice.ModelName, ".gguf")
		}
		if err := ensureEmbeddedRuntime(ctx, out, dataDir, &choice, opts); err != nil {
			return setup.Report{}, err
		}
	}

	if choice.ModelPath != "" {
		if info, err := os.Stat(choice.ModelPath); err == nil && !info.IsDir() {
			choice.ModelName = filepath.Base(choice.ModelPath)
			choice.ProviderModel = strings.TrimSuffix(choice.ModelName, ".gguf")
		}
	}
	if choice.Mode == config.ModeEmbedded {
		if choice.RuntimeBinary == "" {
			return setup.Report{}, fmt.Errorf("setup: no llama-server runtime was found; install the runtime or pass --runtime, then run `bcode setup` again")
		}
		if choice.ModelPath == "" && !opts.DownloadModel && !interactive {
			return setup.Report{}, fmt.Errorf("setup: no model was found; pass --model PATH or --download-model")
		}
	}
	if choice.Mode == config.ModeNone {
		return setup.Report{}, fmt.Errorf("setup: no inference runtime was discovered; install llama-server/model or pass --external-url")
	}
	choice.DataDir = dataDir
	choice.Force = opts.Force

	if !opts.SkipJudgment {
		key, err := setup.LoadCredential(dataDir)
		if err != nil {
			return setup.Report{}, err
		}
		if opts.APIKey != "" {
			key = opts.APIKey
			if err := setup.StoreCredential(dataDir, "TYPESAFE_API_KEY", key); err != nil {
				return setup.Report{}, err
			}
		}
		if key == "" {
			key = os.Getenv("TYPESAFE_API_KEY")
		}
		choice.JudgmentEnabled = key != ""
		if !choice.JudgmentEnabled && interactive {
			answer, err := ask(ctx, reader, out, "Configure the hosted decision plane now? [Y/n] ", true)
			if err != nil {
				return setup.Report{}, err
			}
			if strings.EqualFold(strings.TrimSpace(answer), "y") || strings.TrimSpace(answer) == "" {
				// The value is not echoed by this small reader, but the
				// terminal itself may echo it. Production users should export
				// TYPESAFE_API_KEY or use the owner-only file instead.
				value, err := ask(ctx, reader, out, "TYPESAFE_API_KEY (or leave blank to configure later): ", false)
				if err != nil {
					return setup.Report{}, err
				}
				value = strings.TrimSpace(value)
				if value != "" {
					if err := setup.StoreCredential(dataDir, "TYPESAFE_API_KEY", value); err != nil {
						return setup.Report{}, err
					}
					choice.JudgmentEnabled = true
				}
			}
		}
	}

	report, err := setup.Apply(ctx, dataDir, choice)
	if err != nil {
		printSetupFailure(out, err)
		return setup.Report{}, err
	}
	printSetupSuccess(out, report, choice.JudgmentEnabled)
	return report, nil
}

var (
	installPrismRuntime    = setup.InstallPrismRuntime
	checkPrismRuntimeBuild = setup.CheckPrismRuntimeBuild
)

func checkEmbeddedRuntimeRequest(choice *setup.Choice, opts setup.Options) error {
	if choice.Mode != config.ModeEmbedded || choice.RuntimeBinary != "" {
		return nil
	}
	if !opts.InstallRuntime && !opts.AssumeYes {
		return errors.New("setup: embedded inference needs a llama-server runtime; pass --runtime PATH or use --install-runtime (or --yes) to authorize the pinned build")
	}
	return checkPrismRuntimeBuild()
}

func ensureEmbeddedRuntime(ctx context.Context, out io.Writer, dataDir string, choice *setup.Choice, opts setup.Options) error {
	if choice.Mode != config.ModeEmbedded || choice.RuntimeBinary != "" {
		return nil
	}
	if !opts.InstallRuntime && !opts.AssumeYes {
		return errors.New("setup: no llama-server runtime was found; pass --runtime PATH or use --install-runtime (or --yes) to build the pinned Prism runtime")
	}
	// Do not spend minutes compiling a runtime when the rest of the requested
	// setup cannot possibly validate. The model is downloaded before this helper
	// in unattended mode, and an explicitly supplied path is checked here.
	if strings.TrimSpace(choice.ModelPath) == "" {
		return errors.New("setup: no model was found; pass --model PATH or --download-model before building the runtime")
	}
	info, err := os.Stat(choice.ModelPath)
	if err != nil {
		return fmt.Errorf("setup: model %s is not available: %w", choice.ModelPath, err)
	}
	if info.IsDir() || info.Size() == 0 {
		return fmt.Errorf("setup: model %s is empty", choice.ModelPath)
	}
	fmt.Fprintf(out, "  building pinned Prism runtime %s\n", setup.PrismRuntimeRevision)
	path, err := installPrismRuntime(ctx, dataDir, func(format string, args ...any) {
		fmt.Fprintf(out, "  "+format+"\n", args...)
	})
	if err != nil {
		return err
	}
	choice.RuntimeBinary = path
	choice.Mode = config.ModeEmbedded
	return nil
}

func printSetupHeader(out interface{ Write([]byte) (int, error) }, dataDir string) {
	fmt.Fprintln(out, "BoundedCode setup")
	fmt.Fprintln(out, "────────────────────")
	fmt.Fprintf(out, "Data directory: %s\n\n", dataDir)
}

func printSetupChecks(out interface{ Write([]byte) (int, error) }, checks []setup.Check) {
	for _, check := range checks {
		mark := map[string]string{"ok": "✓", "warning": "!", "missing": "!", "cancelled": "!"}[check.Status]
		if mark == "" {
			mark = "-"
		}
		fmt.Fprintf(out, "  %s %-24s %s\n", mark, check.Name, check.Detail)
		if check.Fix != "" {
			fmt.Fprintf(out, "      → %s\n", check.Fix)
		}
	}
	fmt.Fprintln(out)
}

func printSetupFailure(out interface{ Write([]byte) (int, error) }, err error) {
	fmt.Fprintf(out, "\nSetup did not complete: %v\n", err)
	fmt.Fprintln(out, "Nothing is presented as ready until validation succeeds. Fix the item above and run `bcode setup` again.")
}

func printSetupSuccess(out interface{ Write([]byte) (int, error) }, report setup.Report, judgment bool) {
	fmt.Fprintln(out, "\nSetup complete.")
	fmt.Fprintf(out, "  profile:  %s\n", report.Profile)
	runtimeLabel := report.Runtime
	if runtimeLabel == "" {
		runtimeLabel = "external endpoint (operator-owned)"
	}
	modelLabel := report.Model
	if modelLabel == "" {
		modelLabel = "external model"
	}
	fmt.Fprintf(out, "  runtime:  %s\n", runtimeLabel)
	fmt.Fprintf(out, "  model:    %s\n", modelLabel)
	fmt.Fprintf(out, "  provider: %s\n", report.ProviderModel)
	if judgment {
		fmt.Fprintln(out, "  judgment: configured")
	} else {
		fmt.Fprintln(out, "  judgment: not configured; task execution will require it")
	}
	fmt.Fprintln(out, "\nFrom any project directory:")
	fmt.Fprintln(out, "  cd my-project")
	fmt.Fprintln(out, "  bcode opencode")
	fmt.Fprintln(out, "\nBoundedCode will start the runtime and required services for that session,")
	fmt.Fprintln(out, "then stop them and remove temporary session state when OpenCode exits.")
}

func setupConversation(cmd *cobra.Command, reader *bufio.Reader, choice *setup.Choice, opts setup.Options) error {
	out := cmd.OutOrStdout()
	// A fresh local setup has no mode until it has either a discovered runtime
	// or an explicit request to build one. Resolve that before asking about a
	// model so the questions follow the route the operator selected.
	if choice.Mode == config.ModeNone && opts.ExternalURL == "" {
		choice.Mode = config.ModeEmbedded
	}
	buildRuntime := false
	if choice.Mode == config.ModeEmbedded && choice.RuntimeBinary == "" {
		fmt.Fprintln(out, "No local llama-server runtime was found.")
		fmt.Fprintf(out, "Pinned source: %s @ %s\n", setup.PrismRuntimeRepo, setup.PrismRuntimeRevision)
		fmt.Fprintln(out, "The pinned Prism runtime is built locally; this can take several minutes and requires Git, CMake, Ninja, and nvcc.")
		answer, err := ask(cmd.Context(), reader, out, "Build the pinned Prism runtime now? [y/N] ", false)
		if err != nil {
			return err
		}
		if isYes(answer) {
			// Check build prerequisites before asking about or downloading a
			// multi-gigabyte model. The later installer repeats the check at
			// its boundary, so a changed PATH cannot turn a failed preflight
			// into a claimed installation.
			if err := checkPrismRuntimeBuild(); err != nil {
				return err
			}
			buildRuntime = true
		} else {
			// Retain the useful escape hatch for operators who already have a
			// runtime in a nonstandard location, without making that an
			// undocumented second installation path.
			fmt.Fprintln(out, "You can instead enter an existing llama-server executable.")
			value, err := ask(cmd.Context(), reader, out, "Path to llama-server (leave blank to cancel): ", false)
			if err != nil {
				return err
			}
			if strings.TrimSpace(value) == "" {
				return errors.New("setup cancelled: a runtime is required; allow the build, pass --runtime, or configure --external-url")
			}
			choice.RuntimeBinary = resolveRuntimePath(value)
			choice.Mode = config.ModeEmbedded
		}
	}
	if choice.Mode == config.ModeEmbedded && choice.ModelPath == "" {
		fmt.Fprintln(out, "No local GGUF model was found.")
		answer, err := ask(cmd.Context(), reader, out, "Download the reference model now? [Y/n] ", true)
		if err != nil {
			return err
		}
		if !isYes(answer) {
			return errors.New("setup cancelled: a model is required; provide --model or allow the download")
		}
		model, err := setup.DownloadModel(cmd.Context(), choice.DataDir,
			firstNonEmpty(opts.ModelURL, setup.DefaultModelURL), setup.DefaultModelName,
			opts.ModelSHA256, progressWriter(out))
		if err != nil {
			return err
		}
		choice.ModelPath = model
		choice.ModelName = filepath.Base(model)
		choice.ProviderModel = strings.TrimSuffix(choice.ModelName, ".gguf")
	}
	if !buildRuntime {
		return nil
	}

	// The TUI confirmation is intentionally separate from the non-interactive
	// flag. A build can consume substantial CPU, disk, and time, so an
	// interactive operator gets one explicit yes.
	installOpts := opts
	installOpts.InstallRuntime = true
	return ensureEmbeddedRuntime(cmd.Context(), out, choice.DataDir, choice, installOpts)
}

func loadSetupConfig(dataDir string) (config.Config, error) {
	configDir := filepath.Join(dataDir, "config")
	if _, err := os.Stat(config.Path(configDir)); err != nil {
		return config.Config{}, err
	}
	return config.Load(configDir)
}

func mergeExistingChoice(choice *setup.Choice, cfg config.Config, dataDir string) {
	if cfg.Profile != "" {
		choice.Profile = cfg.Profile
	}
	if cfg.Inference.Mode != "" {
		choice.Mode = cfg.Inference.Mode
	}
	// config.Load supplies a default binary name even when no configuration
	// exists. Do not let that placeholder suppress a fresh runtime build, and
	// do not let a stale absolute path prevent rediscovery after an update.
	if cfg.Inference.Binary != "" {
		binary := resolveRuntimePath(cfg.Inference.Binary)
		if info, err := os.Stat(binary); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			choice.RuntimeBinary = binary
		}
	}
	if cfg.Inference.Model != "" {
		model := cfg.Inference.Model
		if !filepath.IsAbs(model) {
			model = filepath.Join(setup.DataDir(dataDir), "models", model)
		}
		if info, err := os.Stat(model); err == nil && !info.IsDir() && info.Size() > 0 {
			choice.ModelPath = model
			choice.ModelName = filepath.Base(model)
			choice.ProviderModel = strings.TrimSuffix(choice.ModelName, ".gguf")
		}
	}
	if cfg.Inference.BaseURL != "" {
		choice.BaseURL = cfg.Inference.BaseURL
	}
}

func resolveRuntimePath(runtimePath string) string {
	runtimePath = strings.TrimSpace(runtimePath)
	if runtimePath == "" || filepath.IsAbs(runtimePath) {
		return runtimePath
	}
	if absolute, err := filepath.Abs(runtimePath); err == nil {
		return absolute
	}
	return runtimePath
}

func resolveModelPath(dataDir, model string) string {
	if filepath.IsAbs(model) {
		return model
	}
	candidate := filepath.Join(dataDir, "models", model)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return model
}

func progressWriter(out interface{ Write([]byte) (int, error) }) func(int64, int64) {
	last := time.Time{}
	return func(done, total int64) {
		if time.Since(last) < 250*time.Millisecond && done != total {
			return
		}
		last = time.Now()
		if total > 0 {
			fmt.Fprintf(out, "\r  downloading model: %d%%", done*100/total)
		} else {
			fmt.Fprintf(out, "\r  downloading model: %d bytes", done)
		}
		if done == total {
			fmt.Fprintln(out)
		}
	}
}

func ask(ctx context.Context, reader *bufio.Reader, out interface{ Write([]byte) (int, error) }, prompt string, defaultYes bool) (string, error) {
	if out != nil {
		fmt.Fprint(out, prompt)
	}
	type result struct {
		value string
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := reader.ReadString('\n')
		ch <- result{strings.TrimSpace(line), err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ch:
		if r.err != nil && r.value == "" {
			return "", r.err
		}
		if r.value == "" && defaultYes {
			return "y", nil
		}
		return r.value, nil
	}
}

func isReaderTerminal(in any) bool {
	f, ok := in.(*os.File)
	return ok && isTerminal(f)
}

func isYes(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "y" || value == "yes"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
