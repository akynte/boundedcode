package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/buildinfo"
	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/inference/llamacpp"
	"github.com/akynte/boundedcode/internal/model"
)

// llamaManager builds the managed llama.cpp runtime from config.
func (a *App) llamaManager() *llamacpp.Manager {
	c := a.Config.Inference
	return &llamacpp.Manager{
		Binary: c.ServerBinary, Host: c.Host, Port: c.Port, ModelsDir: a.modelsDir(),
		StateDir: a.Paths.Runtime, StartupTimeout: c.StartupTimeout.D(), IdleSleep: c.IdleSleep.D(), Log: a.Log,
	}
}

// inferenceRuntime returns the configured runtime. External mode returns nil.
func (a *App) inferenceRuntime() inference.Runtime {
	if a.Config.Inference.Mode == "external" || a.Config.Inference.IsCloud() {
		return nil
	}
	return a.llamaManager()
}

// externalProbeTimeout bounds the reachability check of an external server.
const externalProbeTimeout = 5 * time.Second

// checkExternalInference fails fast when the external inference server does
// not answer, instead of a task blocking later on transport errors.
func (a *App) checkExternalInference(ctx context.Context) error {
	url := a.Config.Inference.ExternalURL
	if err := inference.NewClient(url, externalProbeTimeout).Reachable(ctx); err != nil {
		return fmt.Errorf("external inference server %s is not reachable: %w; start it, or correct inference.external_url in %s "+
			"(to have %s run llama.cpp instead, set inference.mode: managed and run `%s setup --only inference`)",
			url, err, a.configFile(), buildinfo.ProductName, buildinfo.Command())
	}
	return nil
}

func (a *App) profile(name string) (model.Profile, error) {
	if name == "" {
		name = a.Config.DefaultModel
	}
	return a.Models.Get(name)
}

func newRuntimeCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "runtime", Short: "Manage the local inference server (llama.cpp)"}
	var modelName string
	start := &cobra.Command{
		Use:   "start",
		Short: "Start (or reuse) the inference server for a model profile",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := app.inferenceRuntime()
			if rt == nil {
				return fmt.Errorf("inference.mode is external (%s); nothing to start", app.Config.Inference.ExternalURL)
			}
			p, err := app.profile(modelName)
			if err != nil {
				return err
			}
			t0 := time.Now()
			ep, err := rt.Ensure(cmd.Context(), p)
			if err != nil {
				return err
			}
			rec, _ := app.Recorder(cmd.Context())
			rec.Emit(cmd.Context(), "", "runtime.started", map[string]any{"profile": p.Name, "endpoint": ep.BaseURL, "seconds": time.Since(t0).Seconds()})
			app.printf("%s ready at %s (model %q) in %s\n", rt.Name(), ep.BaseURL, ep.Model, time.Since(t0).Round(time.Millisecond))
			return nil
		},
	}
	start.Flags().StringVarP(&modelName, "model", "m", "", "model profile (default: config default_model)")
	stop := &cobra.Command{
		Use:   "stop",
		Short: "Stop the managed inference server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := app.inferenceRuntime()
			if rt == nil {
				return nil
			}
			if err := rt.Stop(cmd.Context()); err != nil {
				return err
			}
			rec, _ := app.Recorder(cmd.Context())
			rec.Emit(cmd.Context(), "", "runtime.stopped", nil)
			app.printf("stopped\n")
			return nil
		},
	}
	status := &cobra.Command{
		Use:   "status",
		Short: "Show inference server status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := app.runtimeStatus(cmd.Context())
			if err != nil {
				return err
			}
			if app.jsonOut {
				return app.printJSON(st)
			}
			app.printf("running: %v  healthy: %v  managed: %v  sleeping: %v\n", st.Running, st.Healthy, st.Managed, st.Sleeping)
			app.printf("endpoint: %s  profile: %s  pid: %d  rss: %d MiB  ctx: %d\n", st.Endpoint.BaseURL, st.Profile, st.PID, st.RSSMiB, st.CtxSize)
			if st.Version != "" {
				app.printf("build: %s\n", st.Version)
			}
			if st.Detail != "" {
				app.printf("note: %s\n", st.Detail)
			}
			return nil
		},
	}
	logs := &cobra.Command{
		Use:   "logs",
		Short: "Print the managed server log path and tail",
		RunE: func(*cobra.Command, []string) error {
			p, lines, err := app.runtimeLog(60)
			if err != nil {
				return err
			}
			app.printf("%s\n%s\n", p, strings.Join(lines, "\n"))
			return nil
		},
	}
	cmd.AddCommand(start, stop, status, logs)
	return cmd
}

func newModelCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "model", Short: "Choose, download and inspect local models"}
	cmd.AddCommand(newModelCatalogCmds(app)...)
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List model profiles and whether their weights are present",
		RunE: func(*cobra.Command, []string) error {
			rows := app.modelRows()
			if app.jsonOut {
				return app.printJSON(rows)
			}
			for _, r := range rows {
				mark := " "
				switch {
				case r.Default:
					mark = "*"
				case r.Recommended:
					mark = "+"
				}
				present := "not downloaded"
				if r.Present {
					present = "downloaded"
				}
				app.printf("%s %-20s %-12s %-8s %-9s %-14s %s\n", mark, r.Name, r.Status, formatGB(r.SizeBytes), r.Fit, present, r.License)
			}
			app.printf("(* default, + recommended for this machine; `%s model recommend` explains)\n", buildinfo.Command())
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show NAME",
		Short: "Show a profile and the llama-server arguments it produces",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			p, err := app.Models.Get(args[0])
			if err != nil {
				return err
			}
			c := app.Config.Inference
			argv := app.llamaManager().ServerArgs(p, p.ResolveFile(app.modelsDir()))
			if app.jsonOut {
				return app.printJSON(map[string]any{"profile": p, "server_args": argv})
			}
			app.printf("%s (%s) from %s\nsource: %s/%s license=%s\nargs: %s %s\n", p.Name, p.DisplayName, p.Origin,
				p.Source.Repo, p.Source.File, p.Source.License, c.ServerBinary, strings.Join(argv, " "))
			return nil
		},
	})
	return cmd
}

// runtimeStatus reports the managed server, or probes the external one.
func (a *App) runtimeStatus(ctx context.Context) (inference.Status, error) {
	if a.Config.Inference.IsCloud() {
		// No local server; the provider is reached per request.
		ic := a.Config.Inference
		ok, src := a.secretStore().Status(ic.Provider)
		st := inference.Status{Running: ok, Healthy: ok, Endpoint: inference.Endpoint{BaseURL: ic.Provider, Model: ic.Cloud().Model},
			Profile: ic.Cloud().Model, Detail: "cloud provider " + ic.Provider}
		if ok {
			st.Detail += " (key from the " + src + ")"
		} else {
			st.Detail += " (no API key)"
		}
		return st, nil
	}
	if rt := a.inferenceRuntime(); rt != nil {
		return rt.Status(ctx)
	}
	var st inference.Status
	c := inference.NewClient(a.Config.Inference.ExternalURL, 3*time.Second)
	st.Endpoint.BaseURL = a.Config.Inference.ExternalURL
	st.Healthy, _ = c.Healthy(ctx)
	st.Running = st.Healthy
	return st, nil
}

// runtimeLog returns the managed server log path and its last n lines.
func (a *App) runtimeLog(n int) (string, []string, error) {
	p := filepath.Join(a.llamaManager().StateDir, "llama-server.log")
	b, err := os.ReadFile(p)
	if err != nil {
		return p, nil, err
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return p, lines, nil
}

// modelRow is a model profile, whether its weights are present, and how it
// fits this machine.
type modelRow struct {
	Name        string `json:"name"`
	Display     string `json:"display_name"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"`
	File        string `json:"file"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
	Present     bool   `json:"present"`
	License     string `json:"license"`
	Default     bool   `json:"default"`
	Fit         string `json:"fit"`
	FitDetail   string `json:"fit_detail"`
	Recommended bool   `json:"recommended"`
}

func (a *App) modelRows() []modelRow {
	snap := a.hardware(context.Background())
	rec := model.Recommend(a.Models, a.Config.DefaultModel, snap)
	fits := map[string]model.Fit{}
	for _, f := range rec.Fits {
		fits[f.Profile] = f
	}
	var rows []modelRow
	for _, n := range a.Models.Names() {
		p := a.Models[n]
		path := p.ResolveFile(a.modelsDir())
		_, err := os.Stat(path)
		status := p.Status
		if status == "" {
			status = model.StatusExperimental
		}
		rows = append(rows, modelRow{Name: n, Display: p.DisplayName, Description: p.Description, Status: status, File: path,
			SizeBytes: p.Source.SizeBytes, Present: err == nil, License: p.Source.License, Default: n == a.Config.DefaultModel,
			Fit: fits[n].Level, FitDetail: fits[n].Detail, Recommended: n == rec.Best.Profile})
	}
	return rows
}
