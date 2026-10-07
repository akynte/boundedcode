package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/akynte/boundedcode/internal/inference"
)

// runtimeView manages the local inference server and model profiles.
type runtimeView struct {
	st      *inference.Status
	err     error
	models  []ModelRow
	modErr  error
	logPath string
	log     []string
	logErr  error
	busy    bool
	cur     cursor
	focus   int // 0 models, 1 log
	scroll  scroller
	h       int
	logH    int
}

type runtimeViewMsg struct {
	st      inference.Status
	err     error
	models  []ModelRow
	modErr  error
	logPath string
	log     []string
	logErr  error
}

func (v *runtimeView) name() string          { return "Runtime" }
func (v *runtimeView) capturing() bool       { return false }
func (v *runtimeView) loading() bool         { return v.busy }
func (v *runtimeView) init(m *Model) tea.Cmd { v.scroll.follow = true; return v.load(m) }
func (v *runtimeView) refresh(m *Model) tea.Cmd {
	if v.busy || m.ticks%3 != 0 {
		return nil
	}
	return v.load(m)
}

func (v *runtimeView) load(m *Model) tea.Cmd {
	v.busy = true
	be, ctx := m.be, m.ctx
	return func() tea.Msg {
		var msg runtimeViewMsg
		msg.st, msg.err = be.Runtime(ctx)
		msg.models, msg.modErr = be.Models(ctx)
		msg.logPath, msg.log, msg.logErr = be.RuntimeLog(ctx, 400)
		return msg
	}
}

func (v *runtimeView) selected() *ModelRow {
	if v.cur.pos < len(v.models) {
		return &v.models[v.cur.pos]
	}
	return nil
}

func (v *runtimeView) update(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case runtimeViewMsg:
		v.busy = false
		if msg.err == nil {
			st := msg.st
			v.st, v.err = &st, nil
			m.rt, m.rtErr = &st, nil
		} else {
			v.st, v.err = nil, msg.err
		}
		v.models, v.modErr = msg.models, msg.modErr
		if msg.modErr == nil {
			m.models = msg.models
		}
		v.logPath, v.log, v.logErr = msg.logPath, msg.log, msg.logErr
		return nil
	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			v.focus = 1 - v.focus
			return nil
		}
		if v.focus == 0 {
			if v.cur.handle(msg, len(v.models), v.h) {
				return nil
			}
		} else if v.scroll.handle(msg, len(v.log), v.logH) {
			return nil
		}
		return v.action(m, msg.String())
	case tea.MouseMsg:
		if v.focus == 1 {
			v.scroll.handle(msg, len(v.log), v.logH)
		} else {
			v.cur.handle(msg, len(v.models), v.h)
		}
	}
	return nil
}

func (v *runtimeView) action(m *Model, key string) tea.Cmd {
	reload := func(m *Model, err error) tea.Cmd {
		if err != nil {
			return tea.Batch(m.errToast(err), v.load(m))
		}
		return tea.Batch(m.toast("done", toastOK), v.load(m), m.loadRuntime())
	}
	switch key {
	case "p":
		return m.startWizard()
	case "u":
		if p := v.selected(); p != nil {
			name := p.Name
			return m.startJob("use model "+name, "", []string{"model", "use", name}, func(m *Model, err error) tea.Cmd {
				m.refreshInfo()
				return reload(m, err)
			})
		}
	case "d":
		if p := v.selected(); p != nil {
			if p.Status == "review" {
				return m.toast(p.Name+": license under review; not offered for download", toastInfo)
			}
			if p.Present {
				return m.toast(p.Name+" is already downloaded", toastInfo)
			}
			name := p.Name
			body := fmt.Sprintf("Download %s (%.1f GB, license %s) at its pinned revision and check its sha256? It goes to the models folder.", name, float64(p.SizeBytes)/1e9, p.License)
			if p.FitDetail != "" {
				body += " On this machine: " + p.FitDetail + "."
			}
			return m.ask("Download model", body, false, func() tea.Cmd {
				return m.startJob("download "+name, "", []string{"model", "fetch", name, "--yes"}, reload)
			})
		}
	case "x":
		if p := v.selected(); p != nil && p.Present {
			name := p.Name
			return m.ask("Delete model weights?", "Delete the downloaded weights of "+name+" ("+p.File+")? Download them again with d.", true,
				func() tea.Cmd {
					return m.startJob("delete "+name, "", []string{"model", "remove", name, "--yes"}, reload)
				})
		}
	case "s", "enter":
		if m.info.ProviderModel != "" {
			return m.toast("using the cloud provider "+m.info.Provider+": there is no local server (p to change)", toastInfo)
		}
		if m.info.InferenceMode == "external" {
			return m.toast("inference.mode is external; nothing to start", toastInfo)
		}
		args := []string{"runtime", "start"}
		name := m.info.DefaultModel
		if p := v.selected(); p != nil && (key == "enter" || v.focus == 0) {
			name = p.Name
			args = append(args, "--model", name)
		}
		return m.startJob("start "+name, "", args, reload)
	case "S":
		return m.ask("Stop the inference server?", "Running tasks lose their model until it is started again (tasks repair the server automatically).", true,
			func() tea.Cmd { return m.startJob("stop runtime", "", []string{"runtime", "stop"}, reload) })
	case "m":
		if p := v.selected(); p != nil {
			name := p.Name
			be, ctx := m.be, m.ctx
			return func() tea.Msg {
				var buf syncBuffer
				err := be.Exec(ctx, []string{"model", "show", name}, &buf)
				return showTextMsg{title: "Model profile " + name, text: buf.String(), err: err}
			}
		}
	case "R":
		return v.load(m)
	}
	return nil
}

type showTextMsg struct {
	title, text string
	err         error
}

func (v *runtimeView) view(m *Model, w, h int) string {
	var b strings.Builder
	b.WriteString(" " + sTitle.Render("Inference runtime") + sFaint.Render("  llama.cpp llama-server · "+m.info.InferenceMode) + "\n\n")
	// Status card.
	var card []string
	switch {
	case v.st == nil && v.err == nil:
		card = append(card, m.spin.View()+sMuted.Render(" checking…"))
	case v.err != nil:
		card = append(card, sErr.Render("✘ "+v.err.Error()))
	default:
		st := v.st
		state := badge("STOPPED", cMuted)
		switch {
		case st.Healthy && st.Sleeping:
			state = badge("SLEEPING", cInfo)
		case st.Healthy:
			state = badge("SERVING", cOK)
		case st.Running:
			state = badge("STARTING", cWarn)
		}
		managed := "external"
		if st.Managed {
			managed = "managed"
		}
		card = append(card,
			state+"  "+sBold.Render(orDash(st.Profile))+sFaint.Render("  "+managed),
			"",
			kv("Endpoint", 12, orDash(st.Endpoint.BaseURL))+"   "+kv("PID", 6, fmt.Sprint(st.PID)),
			kv("Memory", 12, fmt.Sprintf("%d MiB RSS", st.RSSMiB))+"   "+kv("Context", 10, human(st.CtxSize)+" tokens"),
		)
		if st.Version != "" {
			card = append(card, kv("Build", 12, trunc(st.Version, w-20)))
		}
		if st.Detail != "" {
			card = append(card, kv("Note", 12, sWarn.Render(trunc(st.Detail, w-20))))
		}
	}
	b.WriteString(sPanel.Width(w - 4).Render(strings.Join(card, "\n")))
	b.WriteString("\n\n")
	// Models.
	title := sTitle.Render("Model profiles")
	if v.focus == 0 {
		title = sAccentB.Render("Model profiles")
	}
	b.WriteString(" " + title + "\n")
	widths := []int{1, 26, 8, 14, 0}
	b.WriteString(" " + sFaint.Render(row(w-2, widths, "", "PROFILE", "WEIGHTS", "LICENSE", "FILE")) + "\n")
	used := strings.Count(b.String(), "\n")
	modelRows := min(len(v.models), max(2, (h-used)/2-1))
	v.h = modelRows
	if v.modErr != nil {
		b.WriteString(" " + sErr.Render("✘ "+v.modErr.Error()) + "\n")
	}
	from, to := v.cur.window(len(v.models), v.h)
	for i := from; i < to; i++ {
		p := v.models[i]
		mark := " "
		if p.Default {
			mark = sAccent.Render("★")
		}
		present := sOK.Render("present")
		if !p.Present {
			present = sErr.Render("missing")
		}
		name := sText.Render(p.Name)
		if m.rt != nil && m.rt.Profile == p.Name && m.rt.Healthy {
			name += sOK.Render(" ●")
		}
		line := row(w-2, widths, mark, name, present, sMuted.Render(p.License), sFaint.Render(p.File))
		if i == v.cur.pos && v.focus == 0 {
			line = sSel.Render(fit(line, w-2))
		}
		b.WriteString(" " + line + "\n")
	}
	// Server log.
	b.WriteString("\n")
	title = sTitle.Render("Server log")
	if v.focus == 1 {
		title = sAccentB.Render("Server log")
	}
	b.WriteString(" " + title + sFaint.Render("  "+v.logPath) + "\n")
	used = strings.Count(b.String(), "\n")
	v.logH = max(1, h-used-1)
	if v.logErr != nil {
		b.WriteString(" " + sFaint.Render("no log yet ("+firstLine(v.logErr.Error())+")") + "\n")
	} else {
		vis, _, _ := v.scroll.window(v.log, v.logH)
		for _, l := range vis {
			b.WriteString(" " + sMuted.Render(trunc(l, w-2)) + "\n")
		}
	}
	return b.String()
}

func (v *runtimeView) hints(m *Model) []hint {
	return []hint{{"s", "start selected"}, {"u", "use"}, {"d", "download"}, {"x", "delete"}, {"p", "model & provider"}, {"S", "stop"}, {"m", "details"}, {"tab", "models/log"}}
}

func (v *runtimeView) commands(m *Model) []command {
	k := func(key string) func(m *Model) tea.Cmd { return func(m *Model) tea.Cmd { return v.action(m, key) } }
	cs := []command{
		{title: "Start the inference server (default model)", group: "Runtime", run: func(m *Model) tea.Cmd {
			return m.startJob("start "+m.info.DefaultModel, "", []string{"runtime", "start"}, nil)
		}},
		{title: "Stop the inference server", group: "Runtime", key: "S", run: k("S")},
	}
	if p := v.selected(); p != nil {
		cs = append(cs,
			command{title: "Start the server with " + p.Name, group: "Runtime", key: "s", run: k("s")},
			command{title: "Show profile " + p.Name, group: "Models", key: "m", run: k("m")})
	}
	return cs
}
