package main

// `bcode judgment consultations` — the denominator behind every site-level rate.
//
// `bcode judgment calibrate` reports skill over paired findings, which is a
// numerator. Without the number of times a site was consulted at all, a site
// that found two real problems in three hundred consultations and one that
// found two in two read identically. This prints the denominator beside the
// findings so the difference is visible.

import (
	"fmt"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/ledger"
)

func newJudgmentConsultationsCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "consultations",
		Short: "Report how often each judgment site was consulted, and what came back",
		Long: "Every registered site's consultations, including the ones that found\n" +
			"nothing. A site with no row was never reached — that is the one thing\n" +
			"an absence means here, and it is what makes the rest of the table a\n" +
			"rate rather than a count.\n\n" +
			"This reads records only. It promotes nothing and changes nothing.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			_, root, st, err := openWorkspace(ctx)
			if err != nil {
				return err
			}
			defer closeRoot(cmd, root)

			totals, err := ledger.NewConsultationStore(st).Totals(ctx)
			if err != nil {
				return err
			}
			if asJSON {
				return emitJSON(totals)
			}
			printConsultations(cmd, totals)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}

func printConsultations(cmd *cobra.Command, totals map[string]*ledger.SiteTotals) {
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SITE\tREACHED\tASKED\tSKIPPED\tCLEAN\tFOUND\tFINDINGS\tQUESTIONS\tp50 ms")

	var names []string
	for _, s := range judgment.KnownSites() {
		names = append(names, s.Name)
	}
	sort.Strings(names)
	var reached, asked int
	for _, name := range names {
		t := totals[name]
		if t == nil {
			fmt.Fprintf(w, "%s\t0\t0\t0\t0\t0\t0\t0\t-\t(never reached)\n", name)
			continue
		}
		reached += t.Reached
		asked += t.Requested
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\n",
			name, t.Reached, t.Requested, t.Reached-t.Requested,
			t.Reached-t.WithFinding, t.WithFinding, t.Findings, t.Questions,
			medianMS(t.LatencyMS))
	}
	_ = w.Flush()

	fmt.Fprintf(cmd.OutOrStdout(),
		"\n%d consultation(s) across %d site(s); %d sent a request.\n",
		reached, len(totals), asked)
	fmt.Fprintln(cmd.OutOrStdout(),
		"CLEAN is a consultation that returned no finding. It is evidence, not an\n"+
			"absence: a site is only worth authority if FOUND is meaningful against\n"+
			"REACHED. A site with no row was never reached in these records.")

	// Skip reasons matter on their own: a site that is reached but always
	// declines before asking is not being evaluated at all.
	for _, name := range names {
		t := totals[name]
		if t == nil || len(t.BySkip) == 0 {
			continue
		}
		reasons := make([]string, 0, len(t.BySkip))
		for r := range t.BySkip {
			reasons = append(reasons, r)
		}
		sort.Strings(reasons)
		fmt.Fprintf(cmd.OutOrStdout(), "\n%s skipped:\n", name)
		for _, r := range reasons {
			fmt.Fprintf(cmd.OutOrStdout(), "  %-28s %d\n", r, t.BySkip[r])
		}
	}
}

func medianMS(v []int64) int64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]int64(nil), v...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}
