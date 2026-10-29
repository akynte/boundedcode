package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/benchmark"
	"github.com/akynte/boundedcode/internal/model"
	"github.com/akynte/boundedcode/internal/routing"
)

// routeCandidates are the local models model routing may choose from, with
// this machine's measurements: direct-suite results recorded by `bench
// direct` and load times recorded by `bench models`.
func (a *App) routeCandidates(ctx context.Context) ([]routing.Candidate, error) {
	s, err := a.Store(ctx)
	if err != nil {
		return nil, err
	}
	var reps []*benchmark.DirectReport
	rows, err := s.DB.QueryContext(ctx, `SELECT result FROM benchmark_runs WHERE kind = 'direct' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var res string
		if err := rows.Scan(&res); err != nil {
			rows.Close()
			return nil, err
		}
		var r benchmark.DirectReport
		if json.Unmarshal([]byte(res), &r) == nil {
			reps = append(reps, &r)
		}
	}
	rows.Close()
	// The latest measured load time of each profile, over every probe run.
	load := map[string]float64{}
	prows, err := s.DB.QueryContext(ctx, `SELECT result FROM benchmark_runs WHERE kind = 'models' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	for prows.Next() {
		var res string
		if err := prows.Scan(&res); err != nil {
			prows.Close()
			return nil, err
		}
		var pr benchmark.ProbeReport
		if json.Unmarshal([]byte(res), &pr) == nil {
			for _, p := range pr.Profiles {
				if p.LoadSeconds > 0 {
					load[p.Profile] = p.LoadSeconds
				}
			}
		}
	}
	prows.Close()
	perf := benchmark.ModelPerf(reps)
	snap := a.hardware(ctx)
	var out []routing.Candidate
	for _, name := range a.Models.Names() {
		mp, ok := perf[name]
		if !ok {
			continue
		}
		p, _ := a.Models.Get(name)
		out = append(out, routing.Candidate{Profile: name, Fast: model.FitFor(p, snap).Fast(), LoadSeconds: load[name], Perf: mp})
	}
	return out, nil
}

func newModelRouteCmd(app *App) *cobra.Command {
	var current string
	var tasks int
	cmd := &cobra.Command{
		Use:   "route",
		Short: "Show which local model the experimental model routing would choose for chat and code requests, from this machine's measurements",
		Long: `Model routing compares the local models measured on this machine (bench
direct for results, bench models for load times) and keeps the current
model unless another is better beyond chance on the kind of request, or as
reliable and faster by enough to repay its load time twice over the
expected requests. A model without measurements is never chosen. This
command shows the decision; it does not switch the server.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if current == "" {
				current = app.Config.DefaultModel
			}
			cands, err := app.routeCandidates(cmd.Context())
			if err != nil {
				return err
			}
			type row struct {
				Mode   string         `json:"mode"`
				Choice routing.Choice `json:"choice"`
			}
			var rows []row
			for _, mode := range []string{"chat", "code"} {
				rows = append(rows, row{mode, routing.Choose(current, cands, mode, routing.DefaultHysteresis.Dwell, tasks, routing.DefaultHysteresis)})
			}
			if app.jsonOut {
				return app.printJSON(map[string]any{"candidates": cands, "decisions": rows})
			}
			if len(cands) == 0 {
				app.printf("no local model has direct-suite measurements on this machine (`bench direct -m NAME` records them)\n")
				return nil
			}
			app.printf("%-20s %-5s %-7s %-22s %-22s\n", "model", "fast", "load s", "chat (passed, s/task)", "code (passed, s/task)")
			for _, c := range cands {
				cell := func(k string) string {
					p, ok := c.Perf[k]
					if !ok {
						return "–"
					}
					return fmt.Sprintf("%d/%d, %.0f", p.Passed, p.Tasks, p.Mean)
				}
				load := "–"
				if c.LoadSeconds > 0 {
					load = fmt.Sprintf("%.1f", c.LoadSeconds)
				}
				app.printf("%-20s %-5v %-7s %-22s %-22s\n", c.Profile, c.Fast, load, cell("chat"), cell("code"))
			}
			for _, r := range rows {
				verb := "keep"
				if r.Choice.Switch {
					verb = "switch to"
				}
				app.printf("%s requests: %s %s (%s)\n", r.Mode, verb, r.Choice.Profile, strings.TrimSpace(r.Choice.Reason))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&current, "current", "", "the model loaded now (default: default_model)")
	cmd.Flags().IntVar(&tasks, "tasks", 5, "requests of the kind expected before one of another kind")
	return cmd
}
