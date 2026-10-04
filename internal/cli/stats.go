package cli

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/stats"
)

func newStatsCmd(app *App) *cobra.Command {
	var since time.Duration
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Local-first metrics from the task ledger (local-only rate, escalation rate, tokens, tasks/hour)",
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
			sum, err := stats.Compute(ctx, s.DB, from)
			if err != nil {
				return err
			}
			if app.jsonOut {
				return app.printJSON(sum)
			}
			app.printf("tasks %d %v\n", sum.Tasks, sum.ByStatus)
			app.printf("completed %d, local-only %d (%.0f%%); attempts per completed task %.2f\n",
				sum.Completed, sum.CompletedLocalOnly, 100*sum.LocalOnlyRate, sum.AttemptsPerCompleted)
			app.printf("escalation rate %.0f%% of tasks (%d sent, %d declined, %d blocked by packet sanitization)\n", 100*sum.EscalationRate, sum.EscalationsSent, sum.EscalationsDeclined, sum.EscalationsBlocked)
			app.printf("tokens: local processed %d (generated %d, cached prompt %d); frontier packets %d = %.2f%% of tokens\n",
				sum.LocalTokens, sum.GeneratedTokens, sum.CachedPromptTokens, sum.FrontierPacketTok, 100*sum.FrontierTokenShare)
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
