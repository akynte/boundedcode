package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/eval"
	"github.com/akynte/boundedcode/internal/judgebench"
	"github.com/akynte/boundedcode/internal/oracle"
)

func newEvalJudgesCmd() *cobra.Command {
	var tasksDir, corpusDir, oraclesDir string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "judges",
		Short: "Measure how often each acceptance judge accepts a change that does not solve its task",
		Long: "judges puts a corpus of candidate changes before two judges and scores each\n" +
			"against the task's hidden acceptance command:\n\n" +
			"  ci     the repository's visible build, vet, test and format checks\n" +
			"  bcode  the completion contract on an indexed repository: the same\n" +
			"         checks in a verification snapshot, plus the tests that reach\n" +
			"         the change\n\n" +
			"A candidate is either an edit script in <corpus>/*.yaml or a unified diff at\n" +
			"<corpus>/<task-id>/<name>.patch, which is how a patch from another agent is\n" +
			"judged. Its label comes from the hidden command, never from its stated intent.\n" +
			"No hidden oracle is given to either judge: the task's hidden tests are the\n" +
			"labels, and judging with them would be circular.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			tasks, err := eval.LoadSet(tasksDir)
			if err != nil {
				return err
			}
			candidates, err := judgebench.LoadCorpus(corpusDir)
			if err != nil {
				return err
			}
			work, err := os.MkdirTemp("", "bcode-judges-")
			if err != nil {
				return err
			}
			defer func() { _ = os.RemoveAll(work) }()
			judges := []judgebench.Judge{judgebench.CI{WorkDir: work}, judgebench.Bcode{WorkDir: work}}
			names := []string{"ci", "bcode"}
			var suites map[string]*oracle.Suite
			if oraclesDir != "" {
				if suites, err = judgebench.LoadOracles(oraclesDir); err != nil {
					return err
				}
				judges = append(judges, judgebench.Bcode{WorkDir: work, Oracles: suites})
				names = append(names, "bcode+oracle")
			}
			rows, err := judgebench.Run(cmd.Context(), tasks, candidates, judges, judgebench.Options{
				WorkDir: work,
				Logf:    func(f string, a ...any) { fmt.Fprintf(cmd.ErrOrStderr(), f+"\n", a...) },
			})
			if err != nil {
				return err
			}
			constructed, patches := judgebench.Split(rows)
			tallies := map[string][]judgebench.Tally{
				"constructed": judgebench.Tallies(constructed, names),
				"patches":     judgebench.Tallies(patches, names),
			}
			// An oracle that passes on the unfixed code cannot tell a done task
			// from an untouched one, so each is checked before its verdicts
			// are read.
			catches := map[string]bool{}
			for _, t := range tasks {
				if suite, ok := suites[t.ID]; ok {
					if catches[t.ID], err = judgebench.OracleCatchesBase(cmd.Context(), work, t, suite); err != nil {
						return err
					}
				}
			}
			if asJSON {
				return emitJSON(map[string]any{"rows": rows, "tallies": tallies, "oracle_fails_on_unfixed_code": catches})
			}
			printJudges(cmd, rows, tallies, names)
			if len(catches) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "\nOracle fails on the unfixed code (it must, to mean anything):")
				for _, t := range tasks {
					if c, ok := catches[t.ID]; ok {
						fmt.Fprintf(cmd.OutOrStdout(), "  %-32s %v\n", t.ID, c)
					}
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&tasksDir, "tasks", "evals/tasks", "task set directory")
	cmd.Flags().StringVar(&corpusDir, "corpus", "evals/judges", "candidate corpus directory")
	cmd.Flags().StringVar(&oraclesDir, "oracles", "",
		"directory of per-task hidden acceptance suites, <dir>/<task-id>/*.yaml; adds the bcode+oracle judge")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}

func printJudges(cmd *cobra.Command, rows []judgebench.Row, tallies map[string][]judgebench.Tally, names []string) {
	w := cmd.OutOrStdout()
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprint(tw, "TASK\tCANDIDATE\tSOLVED")
	for _, n := range names {
		fmt.Fprintf(tw, "\t%s", n)
	}
	fmt.Fprintln(tw, "\tINTENT")
	for _, r := range rows {
		solved := "no"
		if r.Solved {
			solved = "yes"
		}
		if r.TruthErr != "" {
			solved = "?"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s", r.Candidate.TaskID, r.Candidate.Name, solved)
		for _, n := range names {
			v := r.Verdicts[n]
			mark := "reject"
			switch {
			case v.Err != "":
				mark = "error"
			case v.Accepted && !r.Solved:
				mark = "ACCEPT (false)"
			case v.Accepted:
				mark = "accept"
			case r.Solved:
				mark = "REJECT (false)"
			}
			fmt.Fprintf(tw, "\t%s", mark)
		}
		fmt.Fprintf(tw, "\t%s\n", r.Candidate.Intent)
	}
	_ = tw.Flush()

	for _, group := range []struct{ key, title string }{
		{"constructed", "Hand-written candidates (does each mechanism work?)"},
		{"patches", "Agent patches (how often does real work fail this way?)"},
	} {
		if len(tallies[group.key]) == 0 || tallies[group.key][0].CandidatesSeen == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s\n", group.title)
		tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "JUDGE\tFALSE ACCEPTS\tFALSE REJECTS\tERRORS")
		for _, t := range tallies[group.key] {
			fmt.Fprintf(tw, "%s\t%d of %d unsolved\t%d of %d solved\t%d\n",
				t.Judge, t.FalseAccepts, t.Unsolved, t.FalseRejects, t.Solved, t.Errors)
		}
		_ = tw.Flush()
	}
}
