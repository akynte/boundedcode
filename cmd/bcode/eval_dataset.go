package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/eval"
)

// Dataset-construction commands: eligibility, integrity, the split audit,
// annotator agreement, information parity and the readiness state.
//
// None of them run a model. They are what a person uses while building a
// benchmark, and they are deliberately separate from `bcode eval run` so that
// checking a dataset never costs GPU time.

func newEvalAdmitCmd() *cobra.Command {
	var dir, only string
	cmd := &cobra.Command{
		Use:   "admit",
		Short: "Report why each task is eligible or ineligible for the benchmark",
		Long: "admit applies the task-admission protocol and says, per rule, what the\n" +
			"evidence is.\n\n" +
			"A benchmark of engineering assistance is only as good as its claim to hold\n" +
			"engineering problems. Tasks written to be solvable measure whether the\n" +
			"system can do what their author imagined; the numbers look the same either\n" +
			"way. Some rules this cannot decide — whether an objective really came from\n" +
			"real history is a judgment — and those are reported as needing review\n" +
			"rather than passed, because an automated verdict on an unverified claim is\n" +
			"worse than no verdict.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			tasks, err := eval.LoadSet(dir)
			if err != nil {
				return err
			}
			admissions := eval.AdmitAll(dir, tasks)
			w := cmd.OutOrStdout()
			for _, a := range admissions {
				if only != "" && a.TaskID != only {
					continue
				}
				status := "ELIGIBLE"
				if !a.Eligible() {
					status = "INELIGIBLE"
				}
				fmt.Fprintf(w, "\n%s  [%s]  %s\n", a.TaskID, a.Set, status)
				for _, f := range a.Findings {
					mark := map[eval.AdmissionVerdict]string{
						eval.AdmissionPass: "ok  ", eval.AdmissionFail: "FAIL",
						eval.AdmissionReview: "?   ",
					}[f.Verdict]
					fmt.Fprintf(w, "  [%s] %-32s %s\n", mark, f.Rule, f.Detail)
					if f.Evidence != "" {
						fmt.Fprintf(w, "       %-32s %s\n", "", f.Evidence)
					}
				}
			}
			s := eval.Summarise(admissions)
			fmt.Fprintf(w, "\n%d eligible, %d ineligible, %d needing review, of %d.\n",
				s.Eligible, s.Ineligible, s.NeedReview, s.Total)
			if len(s.ByRule) > 0 {
				rules := make([]string, 0, len(s.ByRule))
				for rule, n := range s.ByRule {
					rules = append(rules, fmt.Sprintf("%s=%d", rule, n))
				}
				sort.Strings(rules)
				fmt.Fprintf(w, "failures: %s\n", strings.Join(rules, " "))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "tasks", "evals/tasks", "the task set directory")
	cmd.Flags().StringVar(&only, "task", "", "report one task")
	return cmd
}

func newEvalIntegrityCmd() *cobra.Command {
	var dir string
	var printDigest bool
	cmd := &cobra.Command{
		Use:   "integrity",
		Short: "Verify each task starts from its declared pre-fix state with no gold material",
		Long: "integrity checks that the fixture the solver gets is the declared base state\n" +
			"and holds nothing that reveals the fix: no patch, no annotation, no version\n" +
			"control history whose log describes the change.\n\n" +
			"The evaluator and the annotator may know the fixing revision. The invariant\n" +
			"is which side of the worktree boundary it sits on, and it is checked rather\n" +
			"than asserted.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			tasks, err := eval.LoadSet(dir)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			var bad int
			for _, t := range tasks {
				if printDigest {
					fmt.Fprintf(w, "%-30s base_digest: %s\n", t.ID, eval.FixtureDigest(t.FixturePath()))
					continue
				}
				res := eval.CheckWorkspace(context.Background(), dir, t, t.FixturePath())
				if res.OK() {
					fmt.Fprintf(w, "[ok  ] %-30s %s\n", t.ID, shortDigest(res.BaseObserved))
					continue
				}
				bad++
				fmt.Fprintf(w, "[FAIL] %-30s\n", t.ID)
				for _, p := range res.Problems {
					fmt.Fprintf(w, "       %s\n", p)
				}
			}
			if printDigest {
				fmt.Fprintln(w, "\nRecord these under origin.base_digest so an edited fixture "+
					"stops matching\nits declared base instead of quietly measuring a different "+
					"starting state.")
				return nil
			}
			if bad > 0 {
				return fmt.Errorf("%d task(s) failed the integrity check", bad)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "tasks", "evals/tasks", "the task set directory")
	cmd.Flags().BoolVar(&printDigest, "print-digest", false,
		"print each fixture's content digest, to record as origin.base_digest")
	return cmd
}

func newEvalAuditCmd() *cobra.Command {
	var dir string
	var showProtocol bool
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Audit the dev/held-out split for imbalance and related task families",
		Long: "audit reports what a person should look at before freezing a split.\n\n" +
			"Two tasks from the same subsystem, or two variations of one bug, are not\n" +
			"independent: tuning on one is partly tuning on the other, and a held-out\n" +
			"number containing its dev twin is not held out. This surfaces candidates.\n" +
			"It never reassigns or deletes one — whether two tasks are the same problem\n" +
			"is a judgment about the work.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			if showProtocol {
				fmt.Fprintln(w, eval.SplitProtocol)
				return nil
			}
			tasks, err := eval.LoadSet(dir)
			if err != nil {
				return err
			}
			tasks, _, err = eval.ApplyAnnotations(dir, tasks)
			if err != nil {
				return err
			}
			a := eval.AuditSplit(tasks)
			fmt.Fprintf(w, "split protocol %s — dev=%d heldout=%d\n\n",
				a.ProtocolVersion, a.DevCount, a.HeldoutCount)

			for _, b := range a.Balance {
				fmt.Fprintf(w, "%s\n", b.Facet)
				values := map[string]bool{}
				for v := range b.Dev {
					values[v] = true
				}
				for v := range b.Heldout {
					values[v] = true
				}
				keys := make([]string, 0, len(values))
				for v := range values {
					keys = append(keys, v)
				}
				sort.Strings(keys)
				for _, v := range keys {
					fmt.Fprintf(w, "  %-24s dev=%-4d heldout=%d\n", v, b.Dev[v], b.Heldout[v])
				}
				for _, s := range b.Skewed {
					fmt.Fprintf(w, "  skew: %s\n", s)
				}
				fmt.Fprintln(w)
			}
			if len(a.SplitFamilies) > 0 {
				fmt.Fprintf(w, "DECLARED FAMILIES SPLIT ACROSS THE SETS:\n")
				for _, f := range a.SplitFamilies {
					fmt.Fprintf(w, "  %s\n", f)
				}
				fmt.Fprintf(w, "Somebody said these are the same problem. A family on "+
					"both sides leaks.\n\n")
			}
			if len(a.CrossSetPairs) > 0 {
				fmt.Fprintf(w, "POSSIBLY RELATED, ON OPPOSITE SIDES (%d):\n", len(a.CrossSetPairs))
				for _, p := range a.CrossSetPairs {
					fmt.Fprintf(w, "  %.2f  %s ↔ %s  (%s)\n", p.Score, p.A, p.B,
						strings.Join(p.Shared, ", "))
				}
				fmt.Fprintln(w, "\nFor review. Nothing is moved automatically: the threshold "+
					"is low on\npurpose, so a false positive costs ten seconds and a missed "+
					"pair costs the\nheld-out set its independence.")
			}
			if len(a.SyntheticHeldout) > 0 {
				fmt.Fprintf(w, "\nSYNTHETIC TASKS IN HELD-OUT: %s\n"+
					"A held-out number quoted from a task written for the benchmark\n"+
					"describes the benchmark's author.\n", strings.Join(a.SyntheticHeldout, ", "))
			}
			if len(a.Undeclared) > 0 {
				fmt.Fprintf(w, "\nNo traits recorded (%d): %s\n"+
					"The split cannot be balanced on facets nobody wrote down.\n",
					len(a.Undeclared), strings.Join(a.Undeclared, ", "))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "tasks", "evals/tasks", "the task set directory")
	cmd.Flags().BoolVar(&showProtocol, "protocol", false, "print the assignment protocol")
	return cmd
}

func newEvalReliabilityCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "reliability",
		Short: "Report agreement between independent annotators",
		Long: "One person's labels are one person's reading. For a held-out number\n" +
			"somebody will quote, a systematically generous annotator produces a recall\n" +
			"figure wrong in one direction for every task, and nothing in the pipeline\n" +
			"can detect it.\n\n" +
			"Held-out labels are written twice, independently — the annotate command\n" +
			"will not show one annotator another's — and the disagreements adjudicated.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			tasks, err := eval.LoadSet(dir)
			if err != nil {
				return err
			}
			r, err := eval.Reliability(dir, tasks)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "rubric %s\n\n", r.RubricVersion)
			for _, t := range r.Tasks {
				fmt.Fprintf(w, "%-30s %s vs %s: %d/%d agreed (%.0f%%), %d disagreed\n",
					t.TaskID, t.Annotators[0], t.Annotators[1], t.Agreed, t.Compared,
					t.RawAgreement*100, t.Disagreed)
				for _, d := range t.Disputes {
					fmt.Fprintf(w, "    %s\n", d)
				}
				if t.OnlyOneRated > 0 {
					fmt.Fprintf(w, "    %d file(s) only one annotator considered — a gap, "+
						"not a disagreement\n", t.OnlyOneRated)
				}
			}
			if r.Compared == 0 {
				fmt.Fprintln(w, "No task has two independent readings.")
			} else {
				fmt.Fprintf(w, "\npooled: %d/%d agreed (%.1f%%), %d disagreement(s), "+
					"%d task(s) need adjudication\n",
					r.Agreed, r.Compared, r.RawAgreement*100, r.Disagreed, r.NeedAdjudication)
				k, ok, caveat := r.PooledKappa()
				if ok {
					fmt.Fprintf(w, "Cohen's kappa %.3f\n", k)
				} else {
					fmt.Fprintf(w, "Cohen's kappa %.3f — not interpretable: %s\n", k, caveat)
				}
			}
			if len(r.SingleAnnotated) > 0 {
				fmt.Fprintf(w, "\nsingle-annotated (%d): %s\n"+
					"Proportionate for dev; not for a published held-out number.\n",
					len(r.SingleAnnotated), strings.Join(r.SingleAnnotated, ", "))
			}
			if len(r.Unadjudicated) > 0 {
				fmt.Fprintf(w, "\nunadjudicated disagreements: %s\n",
					strings.Join(r.Unadjudicated, ", "))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "tasks", "evals/tasks", "the task set directory")
	return cmd
}

func newEvalParityCmd() *cobra.Command {
	var armList []string
	var redact string
	cmd := &cobra.Command{
		Use:   "parity",
		Short: "Show what evidence each reranking arm receives, and which claims it licenses",
		Long: "If one arm receives signatures and another receives only paths, then\n" +
			"\"X is a better reranker\" is not a conclusion the experiment supports. It\n" +
			"supports a statement about two systems as configured, which is a different\n" +
			"and weaker claim.\n\n" +
			"This prints the table and says, per pair, which claim is available.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			arms := make([]eval.Arm, 0, len(armList))
			for _, name := range armList {
				a, err := eval.ArmByName(name)
				if err != nil {
					return err
				}
				arms = append(arms, a)
			}
			fmt.Fprint(cmd.OutOrStdout(), eval.DescribeEvidence(arms, redact).Format())
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&armList, "arms",
		[]string{"supervised", "supervised-rerank-local", "supervised-rerank"},
		"arms to compare")
	cmd.Flags().StringVar(&redact, "redact", "strict",
		"the judgment redaction mode the judged arm would run under")
	return cmd
}

func newEvalReadinessCmd() *cobra.Command {
	var dir string
	var frozen bool
	cmd := &cobra.Command{
		Use:   "readiness",
		Short: "Report which benchmark stage this dataset has reached",
		RunE: func(cmd *cobra.Command, _ []string) error {
			tasks, err := eval.LoadSet(dir)
			if err != nil {
				return err
			}
			tasks, _, err = eval.ApplyAnnotations(dir, tasks)
			if err != nil {
				return err
			}
			coverage, err := eval.AnnotationCoverage(dir, tasks)
			if err != nil {
				return err
			}
			reliability, err := eval.Reliability(dir, tasks)
			if err != nil {
				return err
			}
			// Smoke is not attempted here: readiness is a dataset question
			// and must work with no key and no network. It is reported as
			// unproven, which is what it is until `bcode judgment smoke` says
			// otherwise.
			r := eval.Assess(eval.ReadinessInput{
				Tasks: tasks, Coverage: coverage, Reliability: reliability,
				Admission:    eval.Summarise(eval.AdmitAll(dir, tasks)),
				SmokeOK:      false,
				TuningFrozen: frozen,
			})
			fmt.Fprint(cmd.OutOrStdout(), r.Format())
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "tasks", "evals/tasks", "the task set directory")
	cmd.Flags().BoolVar(&frozen, "tuning-frozen", false,
		"declare the dev-fitted parameters final, which held-out evaluation requires")
	return cmd
}

func shortDigest(s string) string {
	if len(s) > 16 {
		return s[:16]
	}
	return s
}
