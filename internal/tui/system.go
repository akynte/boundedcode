package tui

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// systemView runs environment checks and set-up operations.
type systemView struct {
	checks []Check
	serena []Check
	busy   bool
	sbusy  bool
	loaded bool
	scroll scroller
	lines  []string
	h      int
}

type doctorMsg []Check
type serenaMsg []Check

func (v *systemView) name() string    { return "System" }
func (v *systemView) capturing() bool { return false }
func (v *systemView) loading() bool   { return v.busy || v.sbusy }
func (v *systemView) init(m *Model) tea.Cmd {
	if v.loaded || v.busy {
		return nil
	}
	return v.doctor(m)
}
func (v *systemView) refresh(m *Model) tea.Cmd { return nil }

func (v *systemView) doctor(m *Model) tea.Cmd {
	v.busy = true
	be, ctx := m.be, m.ctx
	return tea.Batch(func() tea.Msg { return doctorMsg(be.Doctor(ctx)) }, m.spinTick())
}

func (v *systemView) probeSerena(m *Model) tea.Cmd {
	v.sbusy = true
	be, ctx := m.be, m.ctx
	return tea.Batch(func() tea.Msg { return serenaMsg(be.SerenaChecks(ctx)) }, m.spinTick())
}

func (v *systemView) update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case doctorMsg:
		v.busy, v.loaded, v.checks = false, true, msg
	case serenaMsg:
		v.sbusy, v.serena = false, msg
	case tea.KeyMsg:
		if v.scroll.handle(msg, len(v.lines), v.h) {
			return nil
		}
		return v.action(m, msg.String())
	case tea.MouseMsg:
		v.scroll.handle(msg, len(v.lines), v.h)
	}
	return nil
}

func (v *systemView) action(m *Model, key string) tea.Cmd {
	switch key {
	case "r", "R":
		if !v.busy {
			return v.doctor(m)
		}
	case "s":
		if !v.sbusy {
			return v.probeSerena(m)
		}
	case "S":
		return m.openForm(newForm("Set up Serena",
			"Installs the pinned, MIT-licensed Serena v1.7.0 from a hash-locked environment, then checks the Go and TypeScript language servers. You are asked before anything is installed.",
			"Set up", []*field{toggleField("reinstall", "Re-sync the managed install from the lock file", false)},
			func(f formValues) (tea.Cmd, error) {
				args := []string{"serena", "setup"}
				if f.on("reinstall") {
					args = append(args, "--reinstall")
				}
				return m.startJob("serena setup", "", args, func(m *Model, err error) tea.Cmd {
					if err != nil {
						return m.errToast(err)
					}
					return tea.Batch(m.toast("Serena is set up", toastOK), v.probeSerena(m))
				}), nil
			}))
	case "b":
		dir := defaultSandboxDir(m.info.AdapterDir)
		return m.openForm(newForm("Build the sandbox image",
			"Builds "+orDash(m.info.AgentImage)+" locally with the container engine. The image is never pushed.",
			"Build", []*field{textField("dir", "Directory with the sandbox Dockerfile", dir, "empty: the copy built into this program").
				withHelp("Only needed to build from a source checkout (adapters/openhands).")},
			func(f formValues) (tea.Cmd, error) {
				args := []string{"sandbox", "build"}
				if d := f.str("dir"); d != "" {
					args = append(args, "--dir", expandHome(d))
				}
				return m.startJob("sandbox build", "", args, func(m *Model, err error) tea.Cmd {
					if err != nil {
						return m.errToast(err)
					}
					return tea.Batch(m.toast("sandbox image built", toastOK), v.doctor(m))
				}), nil
			}))
	case "i":
		return v.initForm(m)
	}
	return nil
}

func defaultSandboxDir(adapterDir string) string {
	if adapterDir != "" {
		return filepath.Dir(adapterDir)
	}
	if _, err := os.Stat(filepath.Join("adapters", "openhands", "Dockerfile")); err == nil {
		abs, _ := filepath.Abs(filepath.Join("adapters", "openhands"))
		return abs
	}
	return ""
}

func (v *systemView) initForm(m *Model) tea.Cmd {
	fields := []*field{
		textField("models", "Models directory (GGUF files)", "", "default: <data dir>/models"),
		textField("server", "llama-server path", "", "default: llama-server on PATH"),
		textField("bench", "llama-bench path", "", "default: llama-bench on PATH"),
		textField("external", "External OpenAI-compatible URL", "", "leave empty to manage llama-server").
			withHelp("Setting this switches inference.mode to external."),
		textField("adapter", "OpenHands adapter project", "", "adapters/openhands/python (needed without a container sandbox)"),
	}
	intro := "Writes " + m.info.ConfigFile + " with explicit paths and creates the state database."
	if m.info.ConfigExists {
		fields = append(fields, toggleField("force", "Overwrite the existing configuration", false))
		intro += " A configuration already exists; it is only replaced when you allow it."
	}
	return m.openForm(newForm("Initialize BoundedCode", intro, "Initialize", fields, func(f formValues) (tea.Cmd, error) {
		args := []string{"init"}
		add := func(flag, key string) {
			if s := f.str(key); s != "" {
				args = append(args, flag, expandHome(s))
			}
		}
		add("--models-dir", "models")
		add("--llama-server", "server")
		add("--llama-bench", "bench")
		if s := f.str("external"); s != "" {
			args = append(args, "--external-url", s)
		}
		add("--adapter-dir", "adapter")
		if _, ok := f["force"]; ok && f.on("force") {
			args = append(args, "--force")
		}
		return m.startJob("init", "", args, func(m *Model, err error) tea.Cmd {
			if err != nil {
				return m.errToast(err)
			}
			return tea.Batch(m.toast("configuration written", toastOK), m.loadInfo(), v.doctor(m))
		}), nil
	}))
}

func (v *systemView) view(m *Model, w, h int) string {
	var b strings.Builder
	in := m.info
	b.WriteString(" " + sTitle.Render("System") + sFaint.Render("  "+productName(in.Name)+" "+in.Version+commitSuffix(in.Commit)) + "\n\n")
	cfg := in.ConfigFile
	if !in.ConfigExists {
		cfg += "  " + badge("not initialized", cWarn) + sMuted.Render("  press ") + sKey.Render("i")
	}
	card := []string{
		kv("Config", 12, cfg),
		kv("State", 12, orDash(in.StateDB)),
		kv("UI log", 12, orDash(in.LogFile)),
		kv("Sandbox", 12, orDash(in.SandboxKind)+sFaint.Render("  image "+orDash(in.AgentImage))),
		kv("Inference", 12, inferenceSummary(in)),
		kv("Intel", 12, onOff(in.CrossService, "cross-service analysis")+sFaint.Render(" · ")+onOff(in.SerenaEnabled, "Serena LSP")),
	}
	if in.Err != "" {
		card = append(card, sErr.Render("✘ configuration: "+in.Err))
	}
	b.WriteString(sPanel.Width(w - 4).Render(strings.Join(card, "\n")))
	b.WriteString("\n\n")
	head := strings.Count(b.String(), "\n")
	v.h = max(1, h-head-1)

	var lines []string
	title := sTitle.Render("Doctor")
	if v.busy {
		title += "  " + m.spin.View() + sMuted.Render(" checking the environment…")
	}
	lines = append(lines, title)
	ok, warn, fail := 0, 0, 0
	for _, c := range v.checks {
		switch c.Status {
		case "ok":
			ok++
		case "warn":
			warn++
		default:
			fail++
		}
	}
	if v.loaded {
		lines[0] += "  " + sOK.Render(itoa(ok)+" ok") + sFaint.Render(" · ") + sWarn.Render(itoa(warn)+" warnings") + sFaint.Render(" · ") + sErr.Render(itoa(fail)+" failed")
	}
	lines = append(lines, checkLines(v.checks, w-2)...)
	lines = append(lines, "")
	st := sTitle.Render("Serena")
	switch {
	case v.sbusy:
		st += "  " + m.spin.View() + sMuted.Render(" probing MCP start and language servers…")
	case v.serena == nil:
		st += "  " + sFaint.Render("press ") + sKey.Render("s") + sFaint.Render(" to probe MCP start and language servers")
	}
	lines = append(lines, st)
	lines = append(lines, checkLines(v.serena, w-2)...)
	v.lines = lines
	vis, _, _ := v.scroll.window(lines, v.h)
	for _, l := range vis {
		b.WriteString(" " + l + "\n")
	}
	return b.String()
}

func onOff(on bool, s string) string {
	if on {
		return sOK.Render("●") + " " + s
	}
	return sFaint.Render("○ " + s)
}

func checkLines(cs []Check, w int) []string {
	var out []string
	for _, c := range cs {
		st := lipgloss.NewStyle().Foreground(statusColor(c.Status))
		out = append(out, "  "+statusIcon(c.Status)+" "+st.Render(fit(c.Name, 22))+" "+sText.Render(trunc(c.Detail, w-28)))
		if c.Hint != "" && c.Status != "ok" {
			out = append(out, "    "+strings.Repeat(" ", 23)+sFaint.Render("↳ "+trunc(c.Hint, w-32)))
		}
	}
	return out
}

func (v *systemView) hints(m *Model) []hint {
	return []hint{{"r", "rerun doctor"}, {"s", "probe Serena"}, {"S", "set up Serena"}, {"b", "build sandbox"}, {"i", "initialize"}}
}

func (v *systemView) commands(m *Model) []command {
	k := func(key string) func(m *Model) tea.Cmd { return func(m *Model) tea.Cmd { return v.action(m, key) } }
	return []command{
		{title: "Run doctor", group: "System", key: "r", run: k("r")},
		{title: "Probe Serena (MCP start, language servers)", group: "System", key: "s", run: k("s")},
		{title: "Set up Serena…", group: "System", key: "S", run: k("S")},
		{title: "Build the sandbox image…", group: "System", key: "b", run: k("b")},
		{title: "Initialize configuration…", group: "System", key: "i", run: k("i")},
	}
}

func commitSuffix(c string) string {
	if c == "" {
		return ""
	}
	return " (" + c + ")"
}

// inferenceSummary describes the model provider in the System view.
func inferenceSummary(in Info) string {
	if in.ProviderModel != "" {
		return in.Provider + sFaint.Render("  model "+in.ProviderModel+" (cloud API)")
	}
	return orDash(in.InferenceMode) + sFaint.Render("  default model "+orDash(in.DefaultModel))
}
