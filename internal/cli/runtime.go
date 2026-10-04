package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/inference/llamacpp"
	"github.com/akynte/boundedcode/internal/model"
)

// llamaManager builds the managed llama.cpp runtime from config.
func (a *App) llamaManager() *llamacpp.Manager {
	c := a.Config.Inference
	return &llamacpp.Manager{
		Binary: c.ServerBinary, Host: c.Host, Port: c.Port, ModelsDir: a.Config.ModelsDir,
		StateDir: a.Paths.Runtime, StartupTimeout: c.StartupTimeout.D(), IdleSleep: c.IdleSleep.D(), Log: a.Log,
	}
}

// inferenceRuntime returns the configured runtime. External mode returns nil.
func (a *App) inferenceRuntime() inference.Runtime {
	if a.Config.Inference.Mode == "external" {
		return nil
	}
	return a.llamaManager()
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
			var st inference.Status
			if rt := app.inferenceRuntime(); rt != nil {
				var err error
				if st, err = rt.Status(cmd.Context()); err != nil {
					return err
				}
			} else {
				c := inference.NewClient(app.Config.Inference.ExternalURL, 3*time.Second)
				st.Endpoint.BaseURL = app.Config.Inference.ExternalURL
				st.Healthy, _ = c.Healthy(cmd.Context())
				st.Running = st.Healthy
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
			p := filepath.Join(app.llamaManager().StateDir, "llama-server.log")
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
			if len(lines) > 60 {
				lines = lines[len(lines)-60:]
			}
			app.printf("%s\n%s\n", p, strings.Join(lines, "\n"))
			return nil
		},
	}
	cmd.AddCommand(start, stop, status, logs)
	return cmd
}

func newModelCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "model", Short: "Inspect model profiles"}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List model profiles and whether their weights are present",
		RunE: func(*cobra.Command, []string) error {
			type row struct {
				Name    string `json:"name"`
				File    string `json:"file"`
				Present bool   `json:"present"`
				License string `json:"license"`
				Default bool   `json:"default"`
			}
			var rows []row
			for _, n := range app.Models.Names() {
				p := app.Models[n]
				path := p.ResolveFile(app.Config.ModelsDir)
				_, err := os.Stat(path)
				rows = append(rows, row{n, path, err == nil, p.Source.License, n == app.Config.DefaultModel})
			}
			if app.jsonOut {
				return app.printJSON(rows)
			}
			for _, r := range rows {
				mark := " "
				if r.Default {
					mark = "*"
				}
				app.printf("%s %-24s present=%-5v license=%-12s %s\n", mark, r.Name, r.Present, r.License, r.File)
			}
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
			argv := app.llamaManager().ServerArgs(p, p.ResolveFile(app.Config.ModelsDir))
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
