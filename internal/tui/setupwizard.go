package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// The set-up wizard chooses where the agent's model runs, without editing
// files: a local model (from the catalog, rated against this machine) or a
// cloud provider (with its API key, stored by the backend, never passed on
// a command line). It then runs `setup` for whatever is still missing.
// Every step is a form; a step opens the next through openFormMsg, so the
// finished form is closed first.

// openFormMsg opens a form once the current dialog has closed.
type openFormMsg struct{ f *form }

func openNext(f *form) tea.Cmd { return func() tea.Msg { return openFormMsg{f} } }

// wizardDataMsg carries what the first steps show.
type wizardDataMsg struct {
	hw        HardwareInfo
	models    []ModelRow
	providers []ProviderRow
	err       error
}

// wizardModelsMsg is a cloud provider's model list (or the error).
type wizardModelsMsg struct {
	provider, baseURL string
	models            []ProviderModel
	err               error
}

// wizardTestMsg is the result of the provider connection test.
type wizardTestMsg struct {
	result string
	err    error
}

// startWizard loads the machine and the catalogs, then shows the first step.
func (m *Model) startWizard() tea.Cmd {
	be, ctx := m.be, m.ctx
	return func() tea.Msg {
		models, err := be.Models(ctx)
		return wizardDataMsg{hw: be.Hardware(ctx), models: models, providers: be.Providers(ctx), err: err}
	}
}

// handleWizardMsg reacts to the wizard's messages; ok is false for others.
func (m *Model) handleWizardMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case openFormMsg:
		return m.openForm(msg.f), true
	case wizardDataMsg:
		if msg.err != nil {
			return m.errToast(msg.err), true
		}
		return m.openForm(m.wizardChoose(msg)), true
	case promptlessConfirm:
		return m.ask(msg.title, msg.body, false, func() tea.Cmd { return msg.yes(m) }), true
	case wizardModelsMsg:
		return m.openForm(m.wizardCloudModel(msg)), true
	case wizardTestMsg:
		if msg.err != nil {
			m.push(newTextModal("Connection test failed", msg.err.Error()+
				"\n\nThe provider is selected, but tasks will fail until this works. Open Settings again (palette: \"Model and provider\") to change the key or the model."))
			return nil, true
		}
		return tea.Batch(m.toast(msg.result, toastOK), m.wizardFinish(true)), true
	}
	return nil, false
}

func (m *Model) wizardChoose(d wizardDataMsg) *form {
	cloudSelected := false
	for _, p := range d.providers {
		if p.Selected && p.Name != "local" {
			cloudSelected = true
		}
	}
	localDesc := "Private: your code stays on this machine. No model fits this machine; consider a cloud API."
	if d.hw.Recommended != "" {
		localDesc = "Private: your code stays on this machine. Suggested here: " + d.hw.Recommended + "."
	}
	items := []listItem{
		{value: "local", title: "On this computer (a local model)", desc: localDesc},
		{value: "cloud", title: "A cloud model API", desc: "OpenAI, Anthropic, Gemini or OpenAI-compatible: fast on any machine, billed per token; your code is sent to the provider."},
	}
	sel := "local"
	if cloudSelected || d.hw.Recommended == "" {
		sel = "cloud"
	}
	f := newForm("Model and provider", "This machine: "+d.hw.Summary, "Next",
		[]*field{listField("where", "Where should the agent's model run?", items, sel)},
		func(v formValues) (tea.Cmd, error) {
			if v.str("where") == "local" {
				return openNext(m.wizardLocal(d)), nil
			}
			return openNext(m.wizardProvider(d.providers)), nil
		})
	return f
}

func (m *Model) wizardLocal(d wizardDataMsg) *form {
	var items []listItem
	sel := ""
	for _, r := range d.models {
		if r.Status == "review" {
			continue // license under review: not offered
		}
		// Most important first: the line is cut at the dialog's width.
		var badges []string
		if r.Recommended {
			badges = append(badges, "suggested")
			sel = r.Name
		}
		if r.Fit != "" {
			badges = append(badges, fitLabel(r.Fit))
		}
		if r.SizeBytes > 0 {
			badges = append(badges, fmt.Sprintf("%.1f GB", float64(r.SizeBytes)/1e9))
		}
		badges = append(badges, r.Status)
		if r.Present {
			badges = append(badges, "downloaded")
		}
		desc := r.Description
		if r.FitDetail != "" {
			desc = strings.TrimSpace(desc + " " + r.FitDetail + ".")
		}
		title := r.Name // short; the display name is in the confirmation
		items = append(items, listItem{value: r.Name, title: title, desc: desc, badges: badges})
		if sel == "" && r.Default {
			sel = r.Name
		}
	}
	rows := map[string]ModelRow{}
	for _, r := range d.models {
		rows[r.Name] = r
	}
	intro := "This machine: " + d.hw.Summary + ". Fit is an estimate from the model's size; only validated models have been measured on BoundedCode's tasks."
	return newForm("Local model", intro, "Use this model",
		[]*field{listField("model", "Model", items, sel)},
		func(v formValues) (tea.Cmd, error) {
			r := rows[v.str("model")]
			if r.Name == "" {
				return nil, fmt.Errorf("choose a model")
			}
			body := fmt.Sprintf("Use %s (license %s).", nameOr(r.Display, r.Name), r.License)
			if !r.Present {
				body += fmt.Sprintf(" It is downloaded now (%.1f GB) at a pinned revision and checked against its sha256.", float64(r.SizeBytes)/1e9)
			}
			if r.Fit == "split" || r.Fit == "cpu" {
				body += " On this machine it runs partly or fully on the CPU, which is slow."
			}
			body += " Set-up then installs whatever else is missing (repository tools, the llama.cpp server, the sandbox image). Continue?"
			name := r.Name
			return func() tea.Msg {
				return promptlessConfirm{title: "Set up the local model", body: body, yes: func(m *Model) tea.Cmd {
					return m.startJob("use model "+name, "", []string{"model", "use", name}, func(m *Model, err error) tea.Cmd {
						if err != nil {
							return m.toast("model use: "+firstLine(err.Error()), toastErr)
						}
						return m.wizardFinish(false)
					})
				}}
			}, nil
		})
}

// promptlessConfirm asks a question after the current form closed.
type promptlessConfirm struct {
	title, body string
	yes         func(m *Model) tea.Cmd
}

// refreshInfo re-reads the configuration summary (after a settings change).
func (m *Model) refreshInfo() { m.info = m.be.Info(m.ctx) }

func nameOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func fitLabel(f string) string {
	switch f {
	case "gpu":
		return "fits the GPU"
	case "offload":
		return "GPU + RAM"
	case "split":
		return "partly CPU"
	case "cpu":
		return "CPU only"
	case "too-large":
		return "too large"
	}
	return f
}

var providerInfo = map[string]struct{ title, desc string }{
	"openai":            {"OpenAI", "GPT models through api.openai.com."},
	"anthropic":         {"Anthropic", "Claude models through api.anthropic.com."},
	"gemini":            {"Google Gemini", "Gemini models through generativelanguage.googleapis.com."},
	"openai-compatible": {"OpenAI-compatible service", "OpenRouter, Groq, Together, DeepSeek, Mistral, or a vLLM/Ollama/LM Studio server: needs its URL."},
}

func (m *Model) wizardProvider(providers []ProviderRow) *form {
	var items []listItem
	sel := ""
	keys := map[string]string{}
	urls := map[string]string{}
	for _, p := range providers {
		info, ok := providerInfo[p.Name]
		if !ok {
			continue
		}
		var badges []string
		if p.KeySource != "" {
			badges = append(badges, "key stored")
			keys[p.Name] = p.KeySource
		}
		if p.Selected {
			badges = append(badges, "selected")
			sel = p.Name
		}
		urls[p.Name] = p.BaseURL
		items = append(items, listItem{value: p.Name, title: info.title, desc: info.desc, badges: badges})
	}
	prov := listField("provider", "Provider", items, sel)
	base := textField("base_url", "API URL (OpenAI-compatible only)", urls["openai-compatible"], "https://api.example.com/v1").
		withHelp("The service's OpenAI-style endpoint, ending in the API version (for example /v1).")
	key := secretField("key", "API key", "paste the key").
		withHelp("Stored in the system's credential store (or an owner-only file), never in the configuration; only BoundedCode's host process uses it.")
	f := newForm("Cloud provider", "Your code and the agent's conversation are sent to the provider you choose, under its terms, and billed per token.", "Next",
		[]*field{prov, base, key},
		func(v formValues) (tea.Cmd, error) {
			p := v.str("provider")
			url := strings.TrimSpace(v.str("base_url"))
			k := strings.TrimSpace(v["key"].input.Value())
			if p == "openai-compatible" && url == "" {
				return nil, fmt.Errorf("an OpenAI-compatible service needs its API URL")
			}
			if p != "openai-compatible" {
				url = ""
			}
			if k == "" && keys[p] == "" {
				return nil, fmt.Errorf("enter the API key for %s", providerInfo[p].title)
			}
			be, ctx := m.be, m.ctx
			return func() tea.Msg {
				if k != "" {
					if _, err := be.SetProviderKey(ctx, p, k); err != nil {
						return wizardModelsMsg{provider: p, baseURL: url, err: fmt.Errorf("storing the key: %w", err)}
					}
				}
				list, err := be.ProviderModels(ctx, p, url)
				return wizardModelsMsg{provider: p, baseURL: url, models: list, err: err}
			}, nil
		})
	f.onChange = func(f *form) {
		p := f.fields[0].value()
		if keys[p] != "" {
			f.fields[2].input.Placeholder = "stored in the " + keys[p] + "; leave empty to keep it"
		} else {
			f.fields[2].input.Placeholder = "paste the key"
		}
	}
	f.onChange(f)
	return f
}

// wizardCloudModel chooses the provider's model. When listing failed the
// model id is typed in; context windows the provider does not report are
// asked for.
func (m *Model) wizardCloudModel(d wizardModelsMsg) *form {
	var fields []*field
	windows := map[string]int{}
	intro := providerInfo[d.provider].title + ": choose the model."
	if d.err != nil {
		intro = "The model list could not be read (" + firstLine(d.err.Error()) + "). Check the key, or type the model id."
	}
	if len(d.models) > 0 {
		var items []listItem
		for _, pm := range d.models {
			desc := pm.Display
			if pm.ContextWindow > 0 {
				desc = strings.TrimSpace(fmt.Sprintf("%s  context %d tokens", pm.Display, pm.ContextWindow))
				windows[pm.ID] = pm.ContextWindow
			}
			items = append(items, listItem{value: pm.ID, title: pm.ID, desc: desc})
		}
		fields = append(fields, listField("model", "Model", items, ""))
	}
	other := textField("other", "Model id (if not in the list)", "", "e.g. a model id from the provider's documentation")
	ctxw := textField("ctx", "Context window (tokens)", "", "from the provider's documentation").
		withHelp("Needed when the provider does not report it (OpenAI and compatible APIs). The agent caps its working context at 200000 tokens.")
	effort := choiceField("effort", "Reasoning effort", []string{"default", "low", "medium", "high"}, "default")
	fields = append(fields, other, ctxw, effort)
	return newForm("Cloud model", intro, "Save and test", fields, func(v formValues) (tea.Cmd, error) {
		id := strings.TrimSpace(v.str("other"))
		if id == "" && v["model"] != nil {
			id = v.str("model")
		}
		if id == "" {
			return nil, fmt.Errorf("choose or type a model")
		}
		args := []string{"provider", "use", d.provider, "--model", id}
		if d.baseURL != "" {
			args = append(args, "--base-url", d.baseURL)
		}
		if w := strings.TrimSpace(v.str("ctx")); w != "" {
			n, err := strconv.Atoi(w)
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("context window: a positive number of tokens")
			}
			args = append(args, "--context-window", strconv.Itoa(n))
		} else if windows[id] == 0 && (d.provider == "openai" || d.provider == "openai-compatible") {
			return nil, fmt.Errorf("enter the model's context window: %s does not report it", providerInfo[d.provider].title)
		}
		if e := v.str("effort"); e != "default" {
			args = append(args, "--effort", e)
		}
		return m.startJob("use "+d.provider+" "+id, "", args, func(m *Model, err error) tea.Cmd {
			if err != nil {
				return m.toast("provider: "+firstLine(err.Error()), toastErr)
			}
			m.refreshInfo()
			be, ctx := m.be, m.ctx
			return func() tea.Msg {
				res, err := be.TestProvider(ctx)
				return wizardTestMsg{result: res, err: err}
			}
		}), nil
	})
}

// wizardFinish runs `setup` for what is still missing. cloud skips the
// local inference steps (setup reports them as not needed).
func (m *Model) wizardFinish(cloud bool) tea.Cmd {
	m.refreshInfo()
	what := "repository tools, the llama.cpp server, the model download and the sandbox image"
	if cloud {
		what = "repository tools and the sandbox image"
	}
	m.push(&confirm{title: "Install what is missing?", body: "Set-up now installs what is still missing: " + what +
		". Downloads are pinned and checksum-verified; the server is built from source when needed. Output is in the activity panel (J).",
		yes: true, onAnswer: func(ok bool) tea.Cmd {
			if !ok {
				return m.toast("Set-up skipped; type /setup to run it later", toastInfo)
			}
			return m.startJob("setup", "", []string{"setup", "--yes"}, func(m *Model, err error) tea.Cmd {
				m.refreshInfo()
				if err != nil {
					return m.toast("Set-up did not finish: "+firstLine(err.Error()), toastErr)
				}
				return tea.Batch(m.toast("Set-up complete", toastOK), m.chat.loadSetup(m))
			})
		}})
	return nil
}
