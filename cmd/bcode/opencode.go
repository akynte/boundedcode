package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/graph"
	"github.com/akynte/boundedcode/internal/llm"
	"github.com/akynte/boundedcode/internal/memory"
	"github.com/akynte/boundedcode/internal/opencode"
	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/supervisor"
	"github.com/akynte/boundedcode/internal/workspace"
)

func newOpenCodeCmd() *cobra.Command {
	var unconfined bool
	var budget bool
	cmd := &cobra.Command{
		Use:   "opencode [-- opencode args...]",
		Short: "Start OpenCode with a managed BoundedCode session",
		Long: "Start a complete BoundedCode session from any project directory.\n\n" +
			"BoundedCode initializes or refreshes the workspace, starts the configured local\n" +
			"model runtime and required services, waits for readiness, and launches OpenCode\n" +
			"with the supervised tools. When OpenCode exits, the runtime, model process,\n" +
			"temporary state, and session services are stopped automatically.\n\n" +
			"Arguments after -- are passed to OpenCode unchanged. The explicit `bcode opencode\n" +
			"run` spelling remains available for scripts that prefer it.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return launchOpenCode(cmd, args, openCodeLaunchOptions{
				Unconfined: unconfined, Budget: budget, InitializeWorkspace: true,
			})
		},
	}
	cmd.Flags().BoolVar(&unconfined, "unconfined", false,
		"start even when no sandbox layer is available, accepting that the session is not confined")
	cmd.Flags().BoolVar(&budget, "budget", false, "record tokenizer-based OpenCode request budget by category")
	cmd.AddCommand(newOpenCodeSetupCmd())
	cmd.AddCommand(newOpenCodeRunCmd())
	cmd.AddCommand(newOpenCodeContextCmd())
	cmd.AddCommand(newOpenCodePromptCmd())
	cmd.AddCommand(newOpenCodeBudgetCmd())
	return cmd
}

func newOpenCodeBudgetCmd() *cobra.Command {
	var sessionID string
	cmd := &cobra.Command{Use: "budget", Short: "Show the latest tokenizer-based OpenCode request budget", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		_, root, st, err := openWorkspace(cmd.Context())
		if err != nil {
			return err
		}
		defer closeRoot(cmd, root)
		path := filepath.Join(st.OpenCodeDir(), "state", "opencode", "boundedcode-budget.jsonl")
		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("OpenCode request accounting is unavailable; start bcode opencode run --budget: %w", err)
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		var last []byte
		for scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			var record struct {
				SessionID string `json:"session_id"`
			}
			if json.Unmarshal(line, &record) == nil && (sessionID == "" || record.SessionID == sessionID) {
				last = line
			}
		}
		if err := scanner.Err(); err != nil {
			return err
		}
		if len(last) == 0 {
			return fmt.Errorf("no recorded OpenCode request for session %q", sessionID)
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), string(last))
		return err
	}}
	cmd.Flags().StringVar(&sessionID, "session", "", "OpenCode session ID")
	return cmd
}

func newOpenCodePromptCmd() *cobra.Command {
	var sessionID, messageID string
	cmd := &cobra.Command{Use: "record-prompt", Hidden: true, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), (1<<20)+1))
		if err != nil {
			return err
		}
		if brokerSocket() != "" {
			_, err = brokerCall(cmd.Context(), brokerRequest{Kind: "prompt", Session: sessionID, Message: messageID, Body: string(body)})
			return err
		}
		_, root, st, err := openWorkspace(cmd.Context())
		if err != nil {
			return err
		}
		defer closeRoot(cmd, root)
		_, err = supervisor.RecordOpenCodePrompt(cmd.Context(), st, sessionID, messageID, string(body))
		return err
	}}
	cmd.Flags().StringVar(&sessionID, "session", "", "OpenCode session ID")
	cmd.Flags().StringVar(&messageID, "message", "", "OpenCode message ID")
	_ = cmd.MarkFlagRequired("session")
	_ = cmd.MarkFlagRequired("message")
	return cmd
}

// The OpenCode plugin calls this at each model-request boundary. A fresh Go
// process reads the ledger, so neither OpenCode compaction nor either process
// restarting can silently erase the objective and recorded decisions.
func newOpenCodeContextCmd() *cobra.Command {
	var sessionID string
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Render durable task state for one OpenCode session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if brokerSocket() != "" {
				body, err := brokerCall(cmd.Context(), brokerRequest{Kind: "context", Session: sessionID})
				if err != nil {
					return err
				}
				_, err = fmt.Fprint(cmd.OutOrStdout(), body)
				return err
			}
			ws, root, st, err := openWorkspace(cmd.Context())
			if err != nil {
				return err
			}
			defer closeRoot(cmd, root)
			body, err := supervisor.OpenCodeContext(cmd.Context(), st, ws.Root, sessionID)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), body)
			return err
		},
	}
	cmd.Flags().StringVar(&sessionID, "session", "", "OpenCode session ID")
	_ = cmd.MarkFlagRequired("session")
	return cmd
}

// newOpenCodeSetupCmd registers the MCP server and writes the project context
// OpenCode reads on its own.
//
// The setup is also used by the default `bcode opencode` entry point. Its
// writes are idempotent, so normal launches refresh generated context safely.
func newOpenCodeSetupCmd() *cobra.Command {
	var dataDir string
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Register the MCP server and write AGENTS.md for this repository",
		Long: "setup refreshes the project-side OpenCode registration without starting a session.\n\n" +
			"Most users should run `bcode opencode`, which performs this setup and owns the\n" +
			"runtime lifecycle automatically. This subcommand is useful when preparing a\n" +
			"repository for a later session or refreshing its managed context.\n\n" +
			"It registers `bcode mcp` in opencode.json, writes the managed context block in\n" +
			"AGENTS.md, installs the context plugin, and leaves unrelated user settings alone.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return setupOpenCode(cmd, dataDir, true)
		},
	}
	cmd.Flags().StringVar(&dataDir, "data-dir", "",
		"pass an explicit --data to the registered `bcode mcp` command")
	return cmd
}

func setupOpenCode(cmd *cobra.Command, dataDir string, printNext bool) error {
	ctx := cmd.Context()
	ws, root, st, err := openWorkspace(ctx)
	if err != nil {
		return err
	}
	defer closeRoot(cmd, root)
	out := cmd.OutOrStdout()
	globalPath, globalChanged, err := opencode.ApplyGlobalInstructions()
	if err != nil {
		return fmt.Errorf("install global OpenCode tool guidance: %w", err)
	}
	fmt.Fprintf(out, "%s %s\n", verb(globalChanged), globalPath)

	command := []string{"bcode", "mcp"}
	if dataDir != "" {
		command = append(command, "--data", dataDir)
	}
	cfgPath, cfgChanged, err := opencode.RegisterMCP(ws.Root, command)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s %s\n", verb(cfgChanged), cfgPath)

	// The editor needs a model of its own, and the operator already configured
	// one for the supervisor. Copying it across lets the user start chatting
	// immediately when that endpoint is configured.
	contextTokens := 32768
	if cfg, err := loadConfig(root); err == nil {
		base, model := inferenceEndpoint(cfg, root)
		contextTokens, outputTokens := 32768, 8192
		if profile := loadProfile(root, cfg); profile != nil {
			contextTokens, outputTokens = profile.ContextTokens, profile.ReservedOutput
		}
		if _, modelChanged, err := opencode.RegisterManagedModelWithLimits(ws.Root, base, model, contextTokens, outputTokens); err != nil {
			return err
		} else if modelChanged {
			fmt.Fprintf(out, "wired the editor to %s (%s)\n", base, shortName(model))
		} else if base == "" {
			fmt.Fprintf(out, "no local endpoint configured yet — set inference.base_url "+
				"in bcode.yaml, or choose a model inside OpenCode\n")
		}
	}
	pluginDir, _, err := opencode.InstallPlugin(st.OpenCodeDir())
	if err != nil {
		return fmt.Errorf("installing the OpenCode context plugin: %w", err)
	}
	if _, changed, err := opencode.RegisterContextPolicyWithLimits(ws.Root, pluginDir, contextTokens, 0); err != nil {
		return err
	} else if changed {
		fmt.Fprintln(out, "configured OpenCode task context and compaction policy")
	}

	// The restricted agent, so `bcode opencode run` has one to select.
	if _, _, err := opencode.RegisterAgent(ws.Root); err != nil {
		return err
	}

	facts := opencode.Facts{WorkspaceName: ws.Name()}
	if stats, err := graph.New(st).Stats(ctx); err == nil {
		facts.Nodes, facts.Edges = stats.Nodes, stats.Edges
	}
	if notes, err := memory.Open(ws.Root, memory.DefaultCaps()).All(); err == nil {
		facts.Notes = notes
	}
	agentsPath, agentsChanged, err := opencode.Apply(ws.Root, opencode.Render(facts))
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s %s\n", verb(agentsChanged), agentsPath)

	if facts.Nodes == 0 {
		fmt.Fprintf(out, "\nThis repository is not indexed yet. Run `bcode index` to build its source graph.\n")
	}
	if printNext {
		fmt.Fprintf(out, "\nSupervised tools are registered. Start the managed session with `bcode opencode`; its runtime and cleanup are automatic.\n")
	}
	return nil
}

// initWorkspaceIfNeeded makes the short `bcode opencode` path work in a
// previously uninitialized project. Existing workspace markers, including one
// found in a parent directory, are left intact.
func initWorkspaceIfNeeded(cmd *cobra.Command) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if _, err := workspace.Open(cwd); err == nil {
		return nil
	} else if !errors.Is(err, workspace.ErrNotAWorkspace) {
		return err
	}
	ws, err := workspace.Init(cwd, workspace.InitOptions{})
	if err != nil {
		// Another invocation may have initialized this directory between the
		// lookup and creation.
		if _, openErr := workspace.Open(cwd); openErr == nil {
			return nil
		} else {
			return err
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "initialized workspace %s in %s\n", ws.ID(), ws.Root)
	return nil
}

func verb(changed bool) string {
	if changed {
		return "wrote"
	}
	return "already current:"
}

// newOpenCodeRunCmd is kept as an explicit spelling for scripts and older
// documentation. It has the same lifecycle as the bare command.
func newOpenCodeRunCmd() *cobra.Command {
	var unconfined bool
	var budget bool
	cmd := &cobra.Command{
		Use:   "run [-- opencode args...]",
		Short: "Start OpenCode with a managed BoundedCode session",
		Long: "Start the same managed session as `bcode opencode`.\n\n" +
			"The launcher owns the model runtime, services, sandbox, and temporary state for\n" +
			"the lifetime of the editor. Cleanup is automatic on every exit path.",
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return launchOpenCode(cmd, args, openCodeLaunchOptions{
				Unconfined: unconfined, Budget: budget, InitializeWorkspace: true,
			})
		},
	}
	cmd.Flags().BoolVar(&unconfined, "unconfined", false,
		"start even when no sandbox layer is available, accepting that the session is not confined")
	cmd.Flags().BoolVar(&budget, "budget", false, "record tokenizer-based OpenCode request budget by category")
	return cmd
}

func inContainer() bool { in, _ := sandbox.InContainer(); return in }

func inactiveReasons(report sandbox.Report) string {
	var b strings.Builder
	for _, note := range report.Inactive {
		fmt.Fprintf(&b, "  %s: %s\n", note.Layer, note.Reason)
	}
	if b.Len() == 0 {
		b.WriteString("  no layer reported a reason\n")
	}
	return b.String()
}

// inferenceEndpoint reports the base URL and model the supervisor is using, so
// the editor can be pointed at the same one.
//
// The provider file is the authority for the model name: it is what the
// supervisor sends, so it is what the endpoint will answer to.
func inferenceEndpoint(cfg config.Config, root *store.Root) (baseURL, model string) {
	switch cfg.Inference.Mode {
	case config.ModeExternal:
		baseURL = cfg.Inference.BaseURL
	case config.ModeEmbedded:
		baseURL = fmt.Sprintf("http://127.0.0.1:%d", cfg.Inference.Port)
	default:
		return "", ""
	}
	specs, err := llm.LoadProvidersFile(root.Layout().ConfigDir())
	if err != nil {
		return baseURL, ""
	}
	for _, spec := range specs.Providers {
		if spec.Name == specs.Default {
			return baseURL, spec.Model
		}
	}
	return baseURL, ""
}

func shortName(model string) string {
	return strings.TrimSuffix(filepath.Base(model), ".gguf")
}
