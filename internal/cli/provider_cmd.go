package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/akynte/boundedcode/internal/buildinfo"
	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/secrets"
)

// providerRow is one provider in `provider show`.
type providerRow struct {
	Name      string `json:"name"`
	Selected  bool   `json:"selected"`
	Model     string `json:"model,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
	KeySource string `json:"key_source,omitempty"` // "" = no key
}

func (a *App) providerRows() []providerRow {
	ic := a.Config.Inference
	store := a.secretStore()
	rows := []providerRow{{Name: inference.ProviderLocal, Selected: !ic.IsCloud(), Model: a.Config.DefaultModel}}
	for _, name := range config.CloudProviders {
		p := ic.Providers[name]
		r := providerRow{Name: name, Selected: ic.Provider == name, Model: p.Model, BaseURL: p.BaseURL}
		if ok, src := store.Status(name); ok {
			r.KeySource = src
		}
		rows = append(rows, r)
	}
	return rows
}

func newProviderCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "provider", Short: "Choose the model provider: local llama.cpp or a cloud API (OpenAI, Anthropic, Gemini, OpenAI-compatible)",
		Long: `The provider serves the agent's model. "local" runs a model on this machine
with llama.cpp. A cloud provider needs an API key, which is stored in the
operating system's credential store (or an owner-only file where none
exists), never in the configuration file, and never enters the agent
sandbox: the host-side gateway adds it to each request.`}

	show := &cobra.Command{Use: "show", Short: "Show the selected provider and stored keys", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			rows := app.providerRows()
			if app.jsonOut {
				return app.printJSON(rows)
			}
			for _, r := range rows {
				mark := "  "
				if r.Selected {
					mark = "▸ "
				}
				detail := r.Model
				if r.Name != inference.ProviderLocal {
					key := "no key"
					if r.KeySource != "" {
						key = "key in " + r.KeySource
					}
					detail = strings.TrimSpace(orNone(r.Model) + "  (" + key + ")")
					if r.BaseURL != "" {
						detail += "  " + r.BaseURL
					}
				}
				app.printf("%s%-18s %s\n", mark, r.Name, detail)
			}
			return nil
		}}

	var pf providerFlags
	use := &cobra.Command{Use: "use PROVIDER", Short: "Select a provider (and its model and settings)", Args: cobra.ExactArgs(1),
		Example: `  ` + buildinfo.Command() + ` provider use anthropic --model claude-opus-5-5
  ` + buildinfo.Command() + ` provider use openai-compatible --base-url https://api.groq.com/openai/v1 --model MODEL --context-window 131072
  ` + buildinfo.Command() + ` provider use local --model qwen3.6-35b-a3b`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := normalizeProvider(args[0])
			if err != nil {
				return err
			}
			if err := app.useProvider(name, pf, cmd.Flags().Changed); err != nil {
				return err
			}
			app.printf("provider: %s\n", app.providerLabel())
			if inference.IsCloud(name) {
				if ok, _ := app.secretStore().Status(name); !ok {
					app.printf("no API key for %s yet: run `%s provider key set %s`\n", name, buildinfo.Command(), name)
				}
			}
			return nil
		}}
	pf.register(use)

	key := &cobra.Command{Use: "key", Short: "Store or remove a provider's API key"}
	keySet := &cobra.Command{Use: "set PROVIDER", Short: "Store an API key (read from the terminal without echo, or from stdin)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := normalizeProvider(args[0])
			if err != nil {
				return err
			}
			if !inference.IsCloud(name) {
				return errors.New("the local provider needs no key")
			}
			value, err := readSecret(cmd.InOrStdin(), app.Err, "API key for "+name+": ")
			if err != nil {
				return err
			}
			src, err := app.secretStore().Set(name, value)
			if err != nil {
				return err
			}
			app.printf("stored the %s key in the %s\n", name, describeSource(src, app))
			return nil
		}}
	keyDel := &cobra.Command{Use: "delete PROVIDER", Short: "Remove a stored API key", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name, err := normalizeProvider(args[0])
			if err != nil {
				return err
			}
			if err := app.secretStore().Delete(name); err != nil {
				return err
			}
			app.printf("removed the %s key\n", name)
			if os.Getenv(secrets.EnvVar(name)) != "" {
				app.printf("note: %s is still set in the environment and overrides stored keys\n", secrets.EnvVar(name))
			}
			return nil
		}}
	key.AddCommand(keySet, keyDel)

	models := &cobra.Command{Use: "models [PROVIDER]", Short: "List the models a provider offers (needs its key)", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := app.Config.Inference.Provider
			if len(args) == 1 {
				var err error
				if name, err = normalizeProvider(args[0]); err != nil {
					return err
				}
			}
			list, err := app.providerModels(cmd.Context(), name)
			if err != nil {
				return err
			}
			if app.jsonOut {
				return app.printJSON(list)
			}
			for _, m := range list {
				line := m.ID
				if m.ContextWindow > 0 {
					line += fmt.Sprintf("  (context %d, output %d)", m.ContextWindow, m.MaxOutputTokens)
				}
				app.printf("%s\n", line)
			}
			return nil
		}}

	test := &cobra.Command{Use: "test", Short: "Send one short request to the selected provider (uses a few tokens)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, err := app.testProvider(cmd.Context())
			if err != nil {
				return err
			}
			if app.jsonOut {
				return app.printJSON(res)
			}
			app.printf("%s answered in %s: %q (%d prompt + %d output tokens)\n", res.Provider, res.Latency, res.Reply, res.Prompt, res.Output)
			return nil
		}}

	cmd.AddCommand(show, use, key, models, test)
	cmd.RunE = show.RunE
	return cmd
}

func orNone(s string) string {
	if s == "" {
		return "(no model chosen)"
	}
	return s
}

func describeSource(src string, a *App) string {
	if src == secrets.SourceFile {
		return "owner-only file " + a.secretStore().File + " (no OS credential store was available)"
	}
	return "OS credential store"
}

// providerFlags are the settings `provider use` can change.
type providerFlags struct {
	model, baseURL, effort      string
	contextWindow, contextLimit int
	inPrice, cachedPrice, out   float64
}

func (f *providerFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.model, "model", "", "model id (cloud) or model profile (local)")
	cmd.Flags().StringVar(&f.baseURL, "base-url", "", "API endpoint (required for openai-compatible)")
	cmd.Flags().StringVar(&f.effort, "effort", "", "reasoning effort: minimal|low|medium|high|xhigh|max (\"\" = model default)")
	cmd.Flags().IntVar(&f.contextWindow, "context-window", 0, "model input limit in tokens (0 = ask the provider)")
	cmd.Flags().IntVar(&f.contextLimit, "context-limit", 0, fmt.Sprintf("cap on the agent's working context (0 = %d)", config.DefaultContextLimit))
	cmd.Flags().Float64Var(&f.inPrice, "input-price", 0, "USD per million input tokens, for cost estimates")
	cmd.Flags().Float64Var(&f.cachedPrice, "cached-input-price", 0, "USD per million cached input tokens")
	cmd.Flags().Float64Var(&f.out, "output-price", 0, "USD per million output tokens")
}

// useProvider selects a provider and applies the changed settings.
func (a *App) useProvider(name string, f providerFlags, changed func(string) bool) error {
	if name == inference.ProviderLocal {
		if f.model != "" {
			if _, err := a.Models.Get(f.model); err != nil {
				return err
			}
		}
		return a.updateConfig(func(c *config.Config) {
			c.Inference.Provider = inference.ProviderLocal
			if f.model != "" {
				c.DefaultModel = f.model
			}
		})
	}
	return a.updateConfig(func(c *config.Config) {
		if c.Inference.Providers == nil {
			c.Inference.Providers = map[string]config.ProviderConfig{}
		}
		p := c.Inference.Providers[name]
		if changed("model") {
			p.Model = strings.TrimSpace(f.model)
		}
		if changed("base-url") {
			p.BaseURL = strings.TrimRight(strings.TrimSpace(f.baseURL), "/")
		}
		if changed("effort") {
			p.Effort = f.effort
		}
		if changed("context-window") {
			p.ContextWindow = f.contextWindow
		}
		if changed("context-limit") {
			p.ContextLimit = f.contextLimit
		}
		if changed("input-price") {
			p.InputPrice = f.inPrice
		}
		if changed("cached-input-price") {
			p.CachedInputPrice = f.cachedPrice
		}
		if changed("output-price") {
			p.OutputPrice = f.out
		}
		c.Inference.Providers[name] = p
		c.Inference.Provider = name
	})
}

// readSecret reads one line: from the terminal without echo, else from r.
func readSecret(r io.Reader, prompt io.Writer, label string) (string, error) {
	if f, ok := r.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(prompt, label)
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(prompt)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	line, err := bufio.NewReader(io.LimitReader(r, 64<<10)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if strings.TrimSpace(line) == "" {
		return "", errors.New("no key given (type or pipe it on standard input)")
	}
	return strings.TrimSpace(line), nil
}

// providerModels lists a provider's models, sorted by id.
func (a *App) providerModels(ctx context.Context, name string) ([]inference.ModelLimits, error) {
	if !inference.IsCloud(name) {
		var out []inference.ModelLimits
		for _, n := range a.Models.Names() {
			out = append(out, inference.ModelLimits{ID: n})
		}
		return out, nil
	}
	key, err := a.cloudKey(name)
	if err != nil {
		return nil, err
	}
	up, err := upstreamFor(name, a.Config.Inference.Providers[name], key, nil, 30*time.Second)
	if err != nil {
		return nil, err
	}
	l, ok := up.(lister)
	if !ok {
		if w, isW := up.(*openAIEffortUpstream); isW {
			l = w.OpenAIUpstream
		} else {
			return nil, fmt.Errorf("%s cannot list models", name)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	list, err := l.Models(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list, nil
}

// providerTest is the result of `provider test`.
type providerTest struct {
	Provider string        `json:"provider"`
	Model    string        `json:"model"`
	Reply    string        `json:"reply"`
	Prompt   int           `json:"prompt_tokens"`
	Output   int           `json:"output_tokens"`
	Latency  time.Duration `json:"latency_ns"`
}

// testProvider sends one short request through the selected provider.
func (a *App) testProvider(ctx context.Context) (providerTest, error) {
	ic := a.Config.Inference
	if !ic.IsCloud() {
		return providerTest{}, fmt.Errorf("the local provider is checked by `%s doctor` and `%s runtime status`", buildinfo.Command(), buildinfo.Command())
	}
	key, err := a.cloudKey(ic.Provider)
	if err != nil {
		return providerTest{}, err
	}
	up, err := upstreamFor(ic.Provider, ic.Cloud(), key, nil, 2*time.Minute)
	if err != nil {
		return providerTest{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	start := time.Now()
	status, resp, err := up.Complete(ctx, map[string]any{"model": ic.Cloud().Model,
		"messages": []any{map[string]any{"role": "user", "content": "Reply with the single word OK."}}, "max_tokens": 64})
	res := providerTest{Provider: ic.Provider, Model: ic.Cloud().Model, Latency: time.Since(start).Round(time.Millisecond)}
	if err != nil {
		return res, fmt.Errorf("%s did not answer: %w", ic.Provider, err)
	}
	if status != 200 {
		msg := strings.TrimSpace(string(resp))
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(resp, &e) == nil && e.Error.Message != "" {
			msg = e.Error.Message
		}
		if status == 401 || status == 403 {
			return res, fmt.Errorf("%s rejected the API key (http %d: %s); replace it with `%s provider key set %s`", ic.Provider, status, msg, buildinfo.Command(), ic.Provider)
		}
		return res, fmt.Errorf("%s: http %d: %s", ic.Provider, status, msg)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage inference.Usage `json:"usage"`
	}
	_ = json.Unmarshal(resp, &out)
	if len(out.Choices) > 0 {
		res.Reply = strings.TrimSpace(out.Choices[0].Message.Content)
	}
	res.Prompt, res.Output = out.Usage.PromptTokens, out.Usage.CompletionTokens
	return res, nil
}
