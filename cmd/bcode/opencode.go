package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	cmd := &cobra.Command{
		Use:   "opencode",
		Short: "Set up and start OpenCode for this project",
		Long: "Run `bcode opencode` from a project directory to initialize its workspace " +
			"if needed, register BoundedCode's MCP tools, and start OpenCode. Setup is " +
			"idempotent: later runs refresh generated context only when it has changed.\n\n" +
			"This starts a regular OpenCode session. Use `bcode opencode run` for the " +
			"confined session.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := initWorkspaceIfNeeded(cmd); err != nil {
				return err
			}
			if err := setupOpenCode(cmd, "", false); err != nil {
				return err
			}
			return startOpenCode(cmd)
		},
	}
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
		Long: "setup makes BoundedCode part of an ordinary OpenCode session.\n\n" +
			"It registers `bcode mcp` in opencode.json, and writes a block into AGENTS.md —\n" +
			"which OpenCode reads into every session — telling the agent that a\n" +
			"compiler-backed index of this repository exists, which questions it answers\n" +
			"better than search, and what this repository has already recorded about\n" +
			"itself. It also installs a small managed block in OpenCode's global\n" +
			"AGENTS.md so tool-language guidance applies in every repository.\n\n" +
			"Re-run it after recording notes or re-indexing. It replaces only its own\n" +
			"block in AGENTS.md and merges into opencode.json, so anything you have\n" +
			"written in either file is left alone.",
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
	if cfg, err := loadConfig(root); err == nil {
		base, model := inferenceEndpoint(cfg, root)
		if _, modelChanged, err := opencode.RegisterModel(ws.Root, base, model); err != nil {
			return err
		} else if modelChanged {
			fmt.Fprintf(out, "wired the editor to %s (%s)\n", base, shortName(model))
		} else if base == "" {
			fmt.Fprintf(out, "no local endpoint configured yet — set inference.base_url "+
				"in bcode.yaml, or choose a model inside OpenCode\n")
		}
	}
	if _, changed, err := opencode.RegisterContextPolicy(ws.Root); err != nil {
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
		fmt.Fprintf(out, "\nOpen this directory in OpenCode and work normally.\n")
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

// startOpenCode starts the regular OpenCode UI, with the exact bcode binary
// that launched this command available to its MCP subprocesses. This avoids
// accidentally starting an older bcode elsewhere on PATH after a local build.
func startOpenCode(cmd *cobra.Command) error {
	binary, err := exec.LookPath("opencode")
	if err != nil {
		return fmt.Errorf("opencode is not on PATH: %w", err)
	}
	bcodePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating bcode executable: %w", err)
	}
	bcodePath, err = filepath.Abs(bcodePath)
	if err != nil {
		return fmt.Errorf("resolving bcode executable: %w", err)
	}
	path := filepath.Dir(bcodePath) + string(os.PathListSeparator) + os.Getenv("PATH")
	child := exec.CommandContext(cmd.Context(), binary)
	child.Env = setEnv(os.Environ(), "PATH", path)
	child.Stdin, child.Stdout, child.Stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
	if err := child.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil
		}
		return fmt.Errorf("starting opencode: %w", err)
	}
	return nil
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, item := range env {
		if !strings.HasPrefix(item, prefix) {
			out = append(out, item)
		}
	}
	return append(out, prefix+value)
}

func verb(changed bool) string {
	if changed {
		return "wrote"
	}
	return "already current:"
}

// newOpenCodeRunCmd starts OpenCode inside the sandbox.
//
// `bcode opencode setup` made the supervisor's tools reachable from a session the
// developer starts themselves. That session is an ordinary process with the
// developer's whole environment: their home directory, their SSH agent, their
// cloud credentials, and a shell tool. The architecture review is direct about
// this — the coding shell's permission system is not part of the firewall,
// because its enforcement has documented bypasses — so the boundary has to be
// the OS sandbox around the process, which nothing was applying because nothing
// here started the process.
//
// This starts it: the strongest confinement the host permits, a scrubbed
// environment, this workspace's own XDG directories, and the restricted agent.
func newOpenCodeRunCmd() *cobra.Command {
	var unconfined bool
	var budget bool
	cmd := &cobra.Command{
		Use:   "run [-- opencode args...]",
		Short: "Start OpenCode confined to this workspace",
		Long: "run starts an OpenCode session inside BoundedCode's sandbox.\n\n" +
			"The session can write its worktree and read the toolchain paths the\n" +
			"operator granted. It has no home directory, no inherited environment and\n" +
			"no network beyond the inference endpoint. Its shell, web and subagent\n" +
			"tools are refused, so verification goes through bc_verify, where the\n" +
			"command is one the operator froze and the result is tied to a content\n" +
			"hash.\n\n" +
			"Arguments after -- are passed to OpenCode unchanged.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			ws, root, st, err := openWorkspace(ctx)
			if err != nil {
				return err
			}
			defer closeRoot(cmd, root)
			out := cmd.OutOrStdout()

			binary, err := exec.LookPath("opencode")
			if err != nil {
				return fmt.Errorf("opencode is not on PATH: %w", err)
			}
			cfg, err := loadConfig(root)
			if err != nil {
				return err
			}
			baseURL, _ := inferenceEndpoint(cfg, root)
			if _, err := opencode.ValidateRuntime(ctx, ws.Root, baseURL); err != nil {
				return fmt.Errorf("OpenCode runtime mismatch: %w", err)
			}
			dirs, err := st.TaskDirs()
			if err != nil {
				return err
			}

			// The agent is registered on every run rather than only by setup:
			// a developer who edits opencode.json between sessions should not
			// end up with a session whose restrictions silently went missing.
			if _, _, err := opencode.RegisterAgent(ws.Root); err != nil {
				return err
			}
			session := opencode.Session{
				Binary: binary, Repo: ws.Root,
				StateDir: st.OpenCodeDir(), TmpDir: dirs.Tmp, Budget: budget,
			}
			spec, err := session.Confine(supervisor.BaseSandboxSpec(cfg, dirs))
			if err != nil {
				return err
			}
			if cfg.Inference.Mode == config.ModeExternal {
				endpoint, err := url.Parse(cfg.Inference.BaseURL)
				if err != nil {
					return err
				}
				if endpoint.Hostname() != "127.0.0.1" && endpoint.Hostname() != "localhost" && endpoint.Hostname() != "::1" {
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
			self, err := os.Executable()
			if err != nil {
				return err
			}
			binDir := filepath.Join(session.StateDir, "bin")
			if err := os.MkdirAll(binDir, 0700); err != nil {
				return err
			}
			link := filepath.Join(binDir, "bcode")
			if info, err := os.Lstat(link); err == nil {
				if info.Mode()&os.ModeSymlink == 0 {
					return fmt.Errorf("refusing to replace non-symlink %s", link)
				}
				if err := os.Remove(link); err != nil {
					return err
				}
			} else if !os.IsNotExist(err) {
				return err
			}
			if err := os.Symlink(self, link); err != nil {
				return err
			}
			session.BCodeBinDir = binDir
			brokerPath, stopBroker, err := startOpenCodeBroker(ctx, st, root.Layout().Root(), ws.Root, session.StateDir, self)
			if err != nil {
				return err
			}
			defer stopBroker()
			capPath := filepath.Join(session.StateDir, brokerCapabilityFile)
			if err := os.WriteFile(capPath, []byte(brokerPath), 0600); err != nil {
				return err
			}
			defer os.Remove(capPath)
			session.BrokerCapability = brokerPath
			spec.Env = session.Env()

			runner, report := supervisor.SelectSandbox(ctx, cfg)
			if runner == nil || (len(report.Active) == 1 && report.Active[0] == sandbox.LayerContainer && !inContainer()) {
				// Saying "confined" when nothing is confining is the failure
				// this refuses to make. The escape hatch is explicit and named.
				if !unconfined {
					return fmt.Errorf(
						"no sandbox layer is available on this host, so the session would run "+
							"unconfined with your whole environment:\n%s\n"+
							"Run `bcode doctor` to see why, or pass --unconfined to accept it",
						inactiveReasons(report))
				}
				fmt.Fprintf(out, "WARNING: starting unconfined. The shell's own permissions are not a boundary.\n\n")
			}

			// OpenCode 2 selects the project default agent from configuration.
			// The old --pure and global --agent flags are rejected by 2.0.15.
			argv := append([]string{binary}, args...)
			if len(args) == 0 {
				argv = append(argv, "--standalone")
			}
			child, err := runner.Command(ctx, spec, argv...)
			if err != nil {
				return err
			}
			child.Stdin, child.Stdout, child.Stderr = cmd.InOrStdin(), out, cmd.ErrOrStderr()

			fmt.Fprintf(out, "Starting OpenCode in %s under %s (%s).\n", ws.Name(), runner.Name(), layerList(report.Active))
			fmt.Fprintf(out, "Refused in this session:\n%s\n", opencode.DeniedSummary())

			if err := child.Run(); err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					// The session's own exit code is the developer's business,
					// not an error from this command.
					return nil
				}
				return fmt.Errorf("starting opencode: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&unconfined, "unconfined", false,
		"start even when no sandbox layer is available, accepting that the session is not confined")
	cmd.Flags().BoolVar(&budget, "budget", false, "record tokenizer-based OpenCode request budget by category")
	return cmd
}

func inContainer() bool { in, _ := sandbox.InContainer(); return in }

func layerList(layers []sandbox.Layer) string {
	names := make([]string, len(layers))
	for i, l := range layers {
		names[i] = string(l)
	}
	return strings.Join(names, " + ")
}

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
