package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"os/user"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/eval"
)

// The localization annotation workflow.
//
// It shows a person the evidence and records their decision. It proposes no
// labels, defaults to nothing, and derives nothing from a gold diff: an
// annotation the tool suggested and a human accepted is the tool's opinion
// with a name on it, and a benchmark scored against that measures the tool
// against itself.

func newEvalRubricCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rubric",
		Short: "Print the localization annotation rubric",
		Long: "The labels must match the proposition the benchmark evaluates. Printing the\n" +
			"rubric is how an annotator, and a reader of the numbers afterwards, can see\n" +
			"exactly what a label was supposed to mean.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), eval.RubricText)
			return nil
		},
	}
}

func newEvalAnnotationsCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "annotations",
		Short: "Report localization annotation coverage",
		RunE: func(cmd *cobra.Command, _ []string) error {
			tasks, err := eval.LoadSet(dir)
			if err != nil {
				return err
			}
			st, err := eval.AnnotationCoverage(dir, tasks)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "rubric %s\n\n", eval.AnnotationRubricVersion)
			fmt.Fprintf(w, "%-10s %-8s %-10s %s\n", "SET", "TASKS", "ANNOTATED", "")
			for _, set := range eval.Sets() {
				fmt.Fprintf(w, "%-10s %-8d %-10d\n", set, st.TotalBySet[set], st.BySet[set])
			}
			fmt.Fprintf(w, "\n%d of %d task(s) carry usable localization ground truth.\n",
				st.Annotated, st.Total)
			if len(st.Unlabelled) > 0 {
				fmt.Fprintf(w, "\nunlabelled (%d): %s\n", len(st.Unlabelled),
					strings.Join(st.Unlabelled, ", "))
			}
			if len(st.StaleTasks) > 0 {
				fmt.Fprintf(w, "\nstale (%d): %s\n"+
					"These were written against an older rubric or an edited task. They are\n"+
					"ignored rather than reinterpreted: a rubric change must not silently\n"+
					"redefine a label somebody else wrote.\n",
					len(st.StaleTasks), strings.Join(st.StaleTasks, ", "))
			}
			if st.Annotated == 0 {
				fmt.Fprintln(w, "\nNo localization metric can be computed. `bcode eval annotate <task>`\n"+
					"walks one task; `bcode eval rubric` prints what the labels mean.")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "tasks", "evals/tasks", "the task set directory")
	return cmd
}

func newEvalAnnotateCmd() *cobra.Command {
	var (
		dir       string
		annotator string
		notes     string
	)
	cmd := &cobra.Command{
		Use:   "annotate <task-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Label one task's localization ground truth, by hand",
		Long: "annotate records one independent reading. It is blind by construction.\n\n" +
			"You will not be shown the fixing revision, any file list derived from it,\n" +
			"another annotator's labels, an adjudicated label, or a suggestion of any\n" +
			"kind. A reading made while looking at the patch is a reading of the patch,\n" +
			"and the rubric asks a different question: would an engineer need to READ\n" +
			"this to implement or verify the objective correctly. Unchanged interfaces,\n" +
			"consumers, configuration and behaviour-defining tests are all things a\n" +
			"careful engineer reads and none of them appear in a diff.\n\n" +
			"Gold evidence belongs in `bcode eval adjudicate`, after both readings are in.\n\n" +
			"Run `bcode eval rubric` first if you have not read it.",
		RunE: func(cmd *cobra.Command, args []string) error {
			tasks, err := eval.LoadSet(dir)
			if err != nil {
				return err
			}
			var target *eval.Task
			for i := range tasks {
				if tasks[i].ID == args[0] {
					target = &tasks[i]
					break
				}
			}
			if target == nil {
				return fmt.Errorf("no task %q in %s", args[0], dir)
			}
			if annotator == "" {
				annotator = defaultAnnotator()
			}
			if annotator == "" {
				return fmt.Errorf("pass --annotator: an unattributed label is one nobody can question")
			}
			ev, err := eval.GatherAnnotationEvidence(dir, *target, annotator, eval.ModeIndependent)
			if err != nil {
				return err
			}
			return runAnnotation(cmd, dir, ev, annotator, notes)
		},
	}
	cmd.Flags().StringVar(&dir, "tasks", "evals/tasks", "the task set directory")
	cmd.Flags().StringVar(&annotator, "annotator", "", "who is deciding; defaults to the git user")
	cmd.Flags().StringVar(&notes, "notes", "", "reasoning, for calls that were not obvious")
	return cmd
}

func runAnnotation(cmd *cobra.Command, dir string, ev eval.AnnotationEvidence,
	annotator, notes string) error {

	w := cmd.OutOrStdout()
	t := ev.Task

	fmt.Fprintf(w, "%s  [%s, %s]\n\n%s\n\n", t.ID, t.Membership(), t.Category,
		strings.Join(wrapText(t.Objective, 74), "\n"))
	if t.Notes != "" {
		fmt.Fprintf(w, "task notes: %s\n\n", strings.Join(wrapText(t.Notes, 74), "\n"))
	}
	if len(ev.Scope) > 0 {
		fmt.Fprintf(w, "write scope: %s\n", strings.Join(ev.Scope, ", "))
	}
	if len(ev.AcceptanceFiles) > 0 {
		fmt.Fprintf(w, "graded by:   %s\n", strings.Join(ev.AcceptanceFiles, ", "))
	}
	fmt.Fprintln(w)

	if ev.Existing != nil {
		fmt.Fprintf(w, "your earlier annotation, rubric %s", ev.Existing.RubricVersion)
		if ev.Stale {
			fmt.Fprintf(w, " — STALE: %s", ev.StaleReason)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "Independent reading. You are not shown the fixing revision, any file\n"+
		"list derived from it, or another annotator's labels.\n\n")
	fmt.Fprintf(w, "The question for each file:\n\n"+
		"    Would a careful engineer need to READ this to implement or verify the\n"+
		"    objective correctly?\n\n"+
		"  r = REQUIRED    u = USEFUL    n = NOT_REQUIRED\n"+
		"  ? = show the rubric    s = skip the rest    q = quit without saving\n\n"+
		"%d candidate file(s).\n\n", len(ev.Candidates))

	in := bufio.NewReader(cmd.InOrStdin())
	files := map[string]eval.Label{}
	for i, c := range ev.Candidates {
		marks := []string{}
		if c.InScope {
			marks = append(marks, "in write scope")
		}
		if c.IsTest {
			marks = append(marks, "test — a test that defines required behaviour is "+
				"REQUIRED reading even when the objective never mentions tests")
		}
		if c.Existing != "" {
			// This annotator's own earlier call, never anybody else's.
			marks = append(marks, "you previously said "+string(c.Existing))
		}
		fmt.Fprintf(w, "[%d/%d] %s  (%d lines)\n", i+1, len(ev.Candidates), c.Path, c.Lines)
		for _, m := range marks {
			fmt.Fprintf(w, "        %s\n", m)
		}
		label, action, err := askLabel(w, in)
		if err != nil {
			return err
		}
		if action == "quit" {
			fmt.Fprintln(w, "nothing saved.")
			return nil
		}
		if action == "skip" {
			fmt.Fprintf(w, "stopping after %d file(s).\n", len(files))
			break
		}
		files[c.Path] = label
	}

	if len(files) == 0 {
		fmt.Fprintln(w, "nothing labelled; not saving.")
		return nil
	}

	a := eval.Annotation{
		TaskID:        t.ID,
		RubricVersion: eval.AnnotationRubricVersion,
		Annotator:     annotator,
		AnnotatedAt:   time.Now().UTC(),
		TaskDigest:    eval.TaskDigestOf(t),
		Notes:         notes,
		Files:         files,
	}
	if problems := a.Validate(); len(problems) > 0 {
		fmt.Fprintf(w, "\nnot saved:\n")
		for _, p := range problems {
			fmt.Fprintf(w, "  - %s\n", p)
		}
		return fmt.Errorf("the annotation is incomplete")
	}
	// Saved under this annotator's own name, so a second reading cannot
	// overwrite the first and both survive for the agreement report.
	path, err := eval.SaveForAnnotator(dir, a)
	if err != nil {
		return err
	}
	required, useful := a.Required(), len(a.UsefulFiles())
	fmt.Fprintf(w, "\nwrote %s\n%d REQUIRED, %d USEFUL, %d NOT_REQUIRED\n",
		path, required, useful, len(files)-required-useful)
	return nil
}

// askLabel reads one decision. There is no default: pressing return does not
// label a file, because a label nobody chose is not ground truth.
func askLabel(w io.Writer, in *bufio.Reader) (eval.Label, string, error) {
	for {
		fmt.Fprint(w, "        r/u/n ? ")
		line, err := in.ReadString('\n')
		if err != nil && line == "" {
			return "", "quit", nil
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "r":
			return eval.LabelRequired, "", nil
		case "u":
			return eval.LabelUseful, "", nil
		case "n":
			return eval.LabelNotRequired, "", nil
		case "?":
			fmt.Fprintln(w, eval.RubricText)
		case "s":
			return "", "skip", nil
		case "q":
			return "", "quit", nil
		default:
			fmt.Fprintln(w, "        r = REQUIRED, u = USEFUL, n = NOT_REQUIRED, "+
				"? = rubric, s = skip, q = quit")
		}
	}
}

func defaultAnnotator() string {
	if out, err := gitConfigValue("user.email"); err == nil && out != "" {
		return out
	}
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return ""
}

func gitConfigValue(key string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "config", "--get", key).Output() //nolint:gosec // a fixed key
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func newEvalAdjudicateCmd() *cobra.Command {
	var (
		dir         string
		adjudicator string
		note        string
	)
	cmd := &cobra.Command{
		Use:   "adjudicate <task-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Resolve disagreements and gaps between independent annotations",
		Long: "adjudicate runs after both readings are in, and shows everything:\n" +
			"each annotator's label, the fixing revision, the lot. That is legitimate\n" +
			"here and not during an independent reading — the job now is to resolve a\n" +
			"difference between two opinions that have already been formed, rather than\n" +
			"to form one.\n\n" +
			"It resolves two kinds of thing. A disagreement is a file both annotators\n" +
			"labelled differently. A gap is a file only one of them considered: not a\n" +
			"disagreement, and not something that may quietly vanish from the ground\n" +
			"truth either. Held-out evaluation requires every file in the union to have\n" +
			"two labels or an adjudicated one.",
		RunE: func(cmd *cobra.Command, args []string) error {
			tasks, err := eval.LoadSet(dir)
			if err != nil {
				return err
			}
			var target *eval.Task
			for i := range tasks {
				if tasks[i].ID == args[0] {
					target = &tasks[i]
					break
				}
			}
			if target == nil {
				return fmt.Errorf("no task %q in %s", args[0], dir)
			}
			if adjudicator == "" {
				adjudicator = defaultAnnotator()
			}
			if adjudicator == "" {
				return fmt.Errorf("pass --adjudicator: a resolution nobody owns is not one")
			}

			annotations, err := eval.LoadIndependentAnnotations(dir, target.ID)
			if err != nil {
				return err
			}
			if len(annotations) < 2 {
				return fmt.Errorf("%s has %d independent annotation(s); adjudication needs "+
					"two. `bcode eval annotate %s --annotator <someone-else>`",
					target.ID, len(annotations), target.ID)
			}
			return runAdjudication(cmd, dir, *target, annotations, adjudicator, note)
		},
	}
	cmd.Flags().StringVar(&dir, "tasks", "evals/tasks", "the task set directory")
	cmd.Flags().StringVar(&adjudicator, "adjudicator", "", "who is deciding; defaults to the git user")
	cmd.Flags().StringVar(&note, "note", "", "why the calls went the way they did")
	return cmd
}

func runAdjudication(cmd *cobra.Command, dir string, t eval.Task,
	annotations []eval.Annotation, adjudicator, note string) error {

	w := cmd.OutOrStdout()
	a, b := annotations[0], annotations[1]
	open := eval.OpenItems(a, b)

	fmt.Fprintf(w, "%s  [%s]\n\n%s\n\n", t.ID, t.Membership(),
		strings.Join(wrapText(t.Objective, 74), "\n"))
	if t.Origin.GoldRevision != "" {
		fmt.Fprintf(w, "fixing revision: %s", t.Origin.GoldRevision)
		if t.Origin.Reference != "" {
			fmt.Fprintf(w, "  (%s)", t.Origin.Reference)
		}
		fmt.Fprintln(w, "\n\nVisible here and not during an independent reading. It is\n"+
			"evidence about what changed, which is a different question from what an\n"+
			"engineer had to read.")
	}
	fmt.Fprintf(w, "\n%s and %s\n", a.Annotator, b.Annotator)
	if len(open) == 0 {
		fmt.Fprintln(w, "\nNothing to resolve: the two readings agree on every file and "+
			"neither\nconsidered a file the other did not.")
		return nil
	}
	fmt.Fprintf(w, "\n%d item(s) to resolve.\n\n", len(open))

	in := bufio.NewReader(cmd.InOrStdin())
	final := map[string]eval.Label{}
	var resolved []eval.Disagreement
	for i, item := range open {
		kind := "DISAGREEMENT"
		if item.Gap {
			kind = "GAP"
		}
		fmt.Fprintf(w, "[%d/%d] %s  %s\n", i+1, len(open), kind, item.Path)
		for _, who := range []string{a.Annotator, b.Annotator} {
			if label, ok := item.Given[who]; ok {
				fmt.Fprintf(w, "        %-28s %s\n", who, label)
			} else {
				fmt.Fprintf(w, "        %-28s (did not consider it)\n", who)
			}
		}
		label, action, err := askLabel(w, in)
		if err != nil {
			return err
		}
		if action == "quit" {
			fmt.Fprintln(w, "nothing saved.")
			return nil
		}
		if action == "skip" {
			fmt.Fprintf(w, "stopping after %d item(s); the rest stay unresolved.\n", len(final))
			break
		}
		final[item.Path] = label
		resolved = append(resolved, eval.Disagreement{
			Path: item.Path, Given: item.Given, Final: label,
		})
	}
	if len(final) == 0 {
		fmt.Fprintln(w, "nothing resolved; not saving.")
		return nil
	}

	adj := eval.Adjudication{
		TaskID: t.ID, RubricVersion: eval.AnnotationRubricVersion,
		Annotators:    []string{a.Annotator, b.Annotator},
		Adjudicator:   adjudicator,
		AdjudicatedAt: time.Now().UTC().Format(time.RFC3339),
		Note:          note,
		Files:         final,
		Disagreements: resolved,
	}
	if problems := adj.Validate(); len(problems) > 0 {
		fmt.Fprintln(w, "\nnot saved:")
		for _, p := range problems {
			fmt.Fprintf(w, "  - %s\n", p)
		}
		return fmt.Errorf("the adjudication is incomplete")
	}
	path, err := eval.SaveAdjudication(dir, adj)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "\nwrote %s\n%d item(s) resolved, %d left open\n",
		path, len(final), len(open)-len(final))
	return nil
}
