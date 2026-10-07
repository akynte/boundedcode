package cli

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/stats"
)

func newStatsCmd(app *App) *cobra.Command {
	var since time.Duration
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Metrics from the task ledger (local-only rate, escalation rate, tokens and estimated cost per provider, tasks/hour)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			s, err := app.Store(ctx)
			if err != nil {
				return err
			}
			from := ""
			if since > 0 {
				from = time.Now().UTC().Add(-since).Format(time.RFC3339Nano)
			}
			sum, err := app.computeStats(ctx, s.DB, from)
			if err != nil {
				return err
			}
			if app.jsonOut {
				return app.printJSON(sum)
			}
			app.printf("tasks %d %v\n", sum.Tasks, sum.ByStatus)
			app.printf("completed %d (%d task-verified, %d checks green but unverified), local-only %d (%.0f%%); attempts per completed task %.2f\n",
				sum.Completed, sum.CompletedVerified, sum.Completed-sum.CompletedVerified, sum.CompletedLocalOnly, 100*sum.LocalOnlyRate, sum.AttemptsPerCompleted)
			app.printf("escalation rate %.0f%% of tasks (%d sent, %d declined, %d blocked by packet sanitization)\n", 100*sum.EscalationRate, sum.EscalationsSent, sum.EscalationsDeclined, sum.EscalationsBlocked)
			if sum.CompletedCloud > 0 {
				app.printf("completed with a cloud model: %d\n", sum.CompletedCloud)
			}
			app.printf("tokens: processed %d (generated %d, cached prompt %d); frontier packets %d = %.2f%% of tokens\n",
				sum.LocalTokens, sum.GeneratedTokens, sum.CachedPromptTokens, sum.FrontierPacketTok, 100*sum.FrontierTokenShare)
			for _, name := range slices.Sorted(maps.Keys(sum.ByProvider)) {
				u := sum.ByProvider[name]
				line := fmt.Sprintf("  %-18s %d calls (%d failed), prompt %d (cached %d), output %d", name, u.Calls, u.Failed, u.Prompt, u.Cached, u.Completion)
				if u.CostUSD > 0 {
					line += fmt.Sprintf(", estimated $%.2f", u.CostUSD)
				}
				app.printf("%s\n", line)
			}
			app.printf("wall %.2f h, verified tasks/hour %.2f; condensations %d, resumes %d, context resets %d; verification runs %d (%d failed)\n",
				sum.WallHours, sum.VerifiedTasksPerHour, sum.Condensations, sum.SessionsResumed, sum.ContextResets, sum.VerificationRuns, sum.FailedVerificationRuns)
			if sum.Tasks == 0 {
				app.printf("(no tasks recorded; benchmark runs keep their own state, see benchmarks/reports)\n")
			}
			return nil
		},
	}
	cmd.Flags().DurationVar(&since, "since", 0, "only tasks created within this duration (e.g. 720h)")
	return cmd
}

// computeStats aggregates the ledger and estimates cloud cost from the
// configured prices.
func (a *App) computeStats(ctx context.Context, db *sql.DB, from string) (stats.Summary, error) {
	sum, err := stats.Compute(ctx, db, from)
	if err != nil {
		return sum, err
	}
	prices := map[string]stats.Price{}
	for name, p := range a.Config.Inference.Providers {
		prices[name] = stats.Price{Input: p.InputPrice, CachedInput: p.CachedInputPrice, Output: p.OutputPrice}
	}
	sum.ApplyPrices(prices)
	return sum, nil
}
