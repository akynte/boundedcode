package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/benchmark"
	"github.com/akynte/boundedcode/internal/lab"
)

func newLabCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lab",
		Short: "Harness optimization laboratory: analyse runs, test hypotheses in isolation, write proposals for review",
		Long: `The laboratory studies BoundedCode's own benchmark runs to find what goes
wrong (failure patterns in the tool calls), turns that into hypotheses, and
tests candidate changes in controlled experiments: each arm runs in its own
isolated home (and, for a code change, its own worktree and binary), against
a fixed baseline and simpler alternatives, decided on held-out tasks across
languages. The result is a proposal. Nothing changes until a person approves
and promotes it, and no experiment can touch security policy, permissions,
the sandbox, credentials or verification.`,
	}
	cmd.AddCommand(newLabAnalyzeCmd(app), newLabHypothesizeCmd(app), newLabRunCmd(app), newLabEvaluateCmd(app), newLabWorktreeCmd(app),
		newLabApproveCmd(app), newLabPromoteCmd(app))
	return cmd
}

func dash(s string) string {
	if s == "" {
		return "–"
	}
	return s
}

// labDir is where experiment runs are kept: under the data directory, apart
// from production state and from any project.
func (a *App) labDir() string { return filepath.Join(a.Paths.Data, "lab") }

// catalogMeta is task metadata for reports that predate it.
func catalogMeta(ctx context.Context) map[string]benchmark.CatalogEntry {
	meta := map[string]benchmark.CatalogEntry{}
	if c, err := benchmark.BuildCatalog(ctx, []benchmark.CatalogSource{
		{Suite: "direct", Dir: "benchmarks/direct"}, {Suite: "direct", Dir: "benchmarks/retrieval/agent-tasks"},
		{Suite: "bounded", Dir: "benchmarks/tasks"}}, "benchmarks/fixtures", nil, nil); err == nil {
		for _, e := range c.Entries {
			meta[e.ID] = e
		}
	}
	return meta
}

func newLabHypothesizeCmd(app *App) *cobra.Command {
	var out, drafts string
	var trials int
	cmd := &cobra.Command{
		Use:   "hypothesize ANALYSIS.json",
		Short: "Turn an analysis's recurring patterns into testable hypotheses and draft experiments",
		Long: `Each hypothesis comes from the laboratory's catalog of interventions: a
configuration change (only tunables: retrieval, context, tool orchestration,
routing, turn and attempt budgets, escalation policy) or a code change to
write as a patch, with the simpler alternative it must beat and the metric it
is decided on. With --drafts, an experiment file is written per hypothesis
for review before anything runs.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			var a lab.Analysis
			if err := json.Unmarshal(b, &a); err != nil {
				return err
			}
			hs := lab.Hypothesize(&a)
			var md bytes.Buffer
			lab.WriteHypothesesMarkdown(&md, hs)
			if out != "" {
				jb, _ := json.MarshalIndent(hs, "", "  ")
				if err := os.WriteFile(out, append(jb, '\n'), 0o644); err != nil {
					return err
				}
				if err := os.WriteFile(strings.TrimSuffix(out, ".json")+".md", md.Bytes(), 0o644); err != nil {
					return err
				}
			}
			if drafts != "" {
				if err := os.MkdirAll(drafts, 0o755); err != nil {
					return err
				}
				for _, h := range hs {
					p := filepath.Join(drafts, h.ID+".yaml")
					if _, err := os.Stat(p); err == nil {
						continue // never overwrite an experiment someone may have edited
					}
					if err := os.WriteFile(p, lab.DraftExperiment(h, trials), 0o644); err != nil {
						return err
					}
				}
			}
			if app.jsonOut {
				return app.printJSON(hs)
			}
			app.printf("%s", md.String())
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "write the hypotheses here (.json; .md beside it)")
	cmd.Flags().StringVar(&drafts, "drafts", "", "write a draft experiment per hypothesis into this directory (existing files are kept)")
	cmd.Flags().IntVar(&trials, "trials", 2, "trials per arm in the drafts")
	return cmd
}

func newLabRunCmd(app *App) *cobra.Command {
	var keep bool
	cmd := &cobra.Command{
		Use:   "run EXPERIMENT.yaml",
		Short: "Run an experiment in isolation, decide it on held-out tasks and write proposals",
		Long: `Builds every arm from a fresh worktree of HEAD (a code arm with its patch,
after the patch checks and the security tests), gives each arm its own
BOUNDEDCODE_HOME with the user's configuration and the arm's settings and no
credentials, runs the trials interleaved, pins every report, decides the
experiment against the baseline and the simpler alternatives, and writes a
proposal for an arm that is accepted. Nothing outside the laboratory
directory changes. Runs are kept under the data directory's lab/.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := lab.LoadExperiment(args[0])
			if err != nil {
				return err
			}
			policy, err := benchmark.LoadGatePolicy(policyPath(e))
			if err != nil {
				return err
			}
			repo, err := os.Getwd()
			if err != nil {
				return err
			}
			r := &lab.Runner{Repo: repo, LabDir: app.labDir(), UserConfigDir: app.Paths.Config, ModelsDir: app.modelsDir(),
				KeepWorktrees: keep, Progress: func(s string) { fmt.Fprintf(app.Err, "%s %s\n", time.Now().Format("15:04:05"), s) }}
			dir, rec, reports, err := r.Run(ctx, e, args[0])
			if err != nil {
				if dir != "" {
					fmt.Fprintf(app.Err, "experiment directory: %s\n", dir)
				}
				return err
			}
			return decideAndPropose(app, ctx, e, dir, rec, reports, policy)
		},
	}
	cmd.Flags().BoolVar(&keep, "keep-worktrees", false, "keep the worktrees the arms were built in")
	return cmd
}

func policyPath(e *lab.Experiment) string {
	if e.Policy != "" {
		return e.Policy
	}
	return "benchmarks/gate-policy.yaml"
}

// decideAndPropose decides an experiment, writes the decision and
// proposals into dir and pins them in the provenance record.
func decideAndPropose(app *App, ctx context.Context, e *lab.Experiment, dir string, rec *lab.Record, reports map[string][]string, policy *benchmark.GatePolicy) error {
	d, err := lab.Decide(e, reports, catalogMeta(ctx), policy)
	if err != nil {
		return err
	}
	dpath := filepath.Join(dir, "decision.json")
	jb, _ := json.MarshalIndent(d, "", "  ")
	if err := os.WriteFile(dpath, append(jb, '\n'), 0o644); err != nil {
		return err
	}
	var md bytes.Buffer
	lab.WriteDecisionMarkdown(&md, d)
	if err := os.WriteFile(filepath.Join(dir, "decision.md"), md.Bytes(), 0o644); err != nil {
		return err
	}
	evidence, err := lab.EvidenceFor(dir, rec, reports)
	if err != nil {
		return err
	}
	props, err := lab.Proposals(dir, e, d, rec, evidence)
	if err != nil {
		return err
	}
	if rec != nil {
		if err := lab.PinOutputs(dir, rec, dpath, props); err != nil {
			return err
		}
	}
	app.printf("%s\nexperiment directory: %s\n", md.String(), dir)
	for _, p := range props {
		app.printf("proposal for review: %s\n", p)
	}
	return nil
}

func newLabEvaluateCmd(app *App) *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "evaluate (RUN-DIR | EXPERIMENT.yaml)",
		Short: "Decide again from recorded runs: check a run's decision reproduces, or decide a retrospective experiment",
		Long: `Given an experiment run's directory, re-reads its provenance, checks that every
report is still what its run produced, decides again and compares the result
with the recorded decision byte for byte. With --out, it instead writes the new
decision there, leaving the recorded one as it is: for re-reading a run after
the decision rules changed. A re-decision makes no proposals. Given an
experiment file whose arms list reports (a retrospective experiment), decides
it from those reports.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if st, err := os.Stat(args[0]); err == nil && st.IsDir() {
				return reevaluate(app, ctx, args[0], out)
			}
			e, err := lab.LoadExperiment(args[0])
			if err != nil {
				return err
			}
			if !e.Retrospective() {
				return fmt.Errorf("%s has arms to run: use lab run, or give a run directory", args[0])
			}
			policy, err := benchmark.LoadGatePolicy(policyPath(e))
			if err != nil {
				return err
			}
			reports := map[string][]string{}
			for _, a := range e.Arms {
				ps, err := expandReports(a.Reports)
				if err != nil {
					return fmt.Errorf("arm %s: %w", a.Name, err)
				}
				reports[a.Name] = ps
			}
			if out == "" {
				out = filepath.Join(app.labDir(), e.ID, "retrospective")
			}
			if err := os.MkdirAll(out, 0o700); err != nil {
				return err
			}
			return decideAndPropose(app, ctx, e, out, nil, reports, policy)
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "directory for a retrospective decision (default: the laboratory directory), or for a run's new decision")
	return cmd
}

func reevaluate(app *App, ctx context.Context, dir, out string) error {
	rec, err := lab.LoadRecord(filepath.Join(dir, "provenance.json"))
	if err != nil {
		return err
	}
	if err := lab.VerifyRuns(rec, dir); err != nil {
		return err
	}
	e, err := lab.LoadExperiment(filepath.Join(dir, "experiment.yaml"))
	if err != nil {
		return err
	}
	policy, err := benchmark.LoadGatePolicy(policyPath(e))
	if err != nil {
		return err
	}
	d, err := lab.Decide(e, lab.ReportsOf(rec, dir), catalogMeta(ctx), policy)
	if err != nil {
		return err
	}
	got, _ := json.MarshalIndent(d, "", "  ")
	if out != "" {
		if err := os.MkdirAll(out, 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(out, "decision.json"), append(got, '\n'), 0o644); err != nil {
			return err
		}
		var md bytes.Buffer
		lab.WriteDecisionMarkdown(&md, d)
		if err := os.WriteFile(filepath.Join(out, "decision.md"), md.Bytes(), 0o644); err != nil {
			return err
		}
		app.printf("%s\n%d reports verified by hash; decided again into %s (no proposals: run the experiment again to propose)\n", md.String(), len(rec.Runs), out)
		return nil
	}
	want, err := os.ReadFile(filepath.Join(dir, "decision.json"))
	if err != nil {
		return err
	}
	if !bytes.Equal(append(got, '\n'), want) {
		return fmt.Errorf("the decision does not reproduce: deciding again from the same reports gives a different result (compare with %s)", filepath.Join(dir, "decision.json"))
	}
	app.printf("reproduced: %d reports verified by hash, the decision is identical\n", len(rec.Runs))
	return nil
}

func newLabWorktreeCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "worktree NAME",
		Short: "Make an isolated worktree of HEAD under the laboratory directory, to write a code candidate in",
		Long: `Write the change there (by hand, or with an agent working in that directory),
then save it as a patch with git -C DIR diff > NAME.patch and name the
patch in an experiment arm. The patch is checked against the laboratory's
rules when the experiment runs; the worktree itself is never used to build an
arm.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := filepath.Join(app.labDir(), "worktrees", args[0])
			if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
				return err
			}
			c := exec.CommandContext(cmd.Context(), "git", "worktree", "add", "--detach", dir, "HEAD")
			if b, err := c.CombinedOutput(); err != nil {
				return fmt.Errorf("git worktree add: %w: %s", err, strings.TrimSpace(string(b)))
			}
			app.printf("%s\nwhen done: git -C %s diff > %s.patch; git worktree remove %s\n", dir, dir, args[0], dir)
			return nil
		},
	}
}

func newLabApproveCmd(app *App) *cobra.Command {
	var reviewer, note string
	cmd := &cobra.Command{
		Use:   "approve PROPOSAL.json",
		Short: "Record your approval of a proposal, after reviewing it",
		Long: `Checks the proposal (an accepted or workload-specific verdict, an allowed
change, evidence unchanged since the decision) and records the reviewer's
approval of its exact content. Approval changes nothing by itself; promote
does, and only for a configuration change.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := lab.Approve(args[0], reviewer, note)
			if err != nil {
				return err
			}
			app.printf("approved: %s\n", out)
			return nil
		},
	}
	cmd.Flags().StringVar(&reviewer, "reviewer", "", "who reviewed it (required)")
	cmd.Flags().StringVar(&note, "note", "", "a note kept with the approval")
	return cmd
}

func newLabPromoteCmd(app *App) *cobra.Command {
	var write bool
	cmd := &cobra.Command{
		Use:   "promote PROPOSAL.json",
		Short: "Show, or with --write make, the configuration change an approved proposal describes",
		Long: `Only an approved proposal whose content is unchanged since its approval is
promoted. A configuration change for every workload sets its keys in your
configuration; a workload-specific one adds a direct.workloads entry for its
languages. The file is backed up first and must load as a valid
configuration. A code change is never applied: land its patch through
review.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pr, err := lab.Promote(args[0], filepath.Join(app.Paths.Config, "config.yaml"), write)
			if err != nil {
				return err
			}
			switch {
			case pr.Written:
				app.printf("promoted: %s in %s (backup: %s)\n", pr.Change, pr.Config, dash(pr.Backup))
			case pr.Config != "":
				app.printf("would %s in %s; run again with --write to make the change\n", pr.Change, pr.Config)
			default:
				app.printf("%s\n", pr.Change)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&write, "write", false, "write the change to your configuration")
	return cmd
}

func newLabAnalyzeCmd(app *App) *cobra.Command {
	var (
		sessions, taskDirs []string
		out                string
		heldout            bool
		examples           int
	)
	cmd := &cobra.Command{
		Use:   "analyze REPORT...",
		Short: "Find recurring failure patterns in benchmark runs and their tool calls",
		Long: `Reads bench direct and bench tasks reports and, for direct mode, each run's
session transcript (found by session id under --sessions), and runs
deterministic detectors for: incorrect context retrieval, repeated tool
calls, weak verification, tool misuse, excessive context, escalation that
did not help, unnecessary retries, ineffective compaction, and exhausted
turn budgets. Held-out runs are left out unless --include-heldout, so the
held-out tasks stay independent of the hypotheses.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reports, err := expandReports(args)
			if err != nil {
				return err
			}
			tasks, err := lab.TaskIndex(taskDirs)
			if err != nil {
				return err
			}
			if len(sessions) == 0 {
				sessions = []string{filepath.Join(app.Paths.Cache, "bench-direct")}
			}
			runs, err := lab.Load(reports, sessions, tasks)
			if err != nil {
				return err
			}
			a, err := lab.Analyze(runs, reports, heldout)
			if err != nil {
				return err
			}
			var md bytes.Buffer
			lab.WriteAnalysisMarkdown(&md, a, examples)
			if out != "" {
				b, _ := json.MarshalIndent(a, "", "  ")
				if err := os.WriteFile(out, append(b, '\n'), 0o644); err != nil {
					return err
				}
				if err := os.WriteFile(strings.TrimSuffix(out, ".json")+".md", md.Bytes(), 0o644); err != nil {
					return err
				}
			}
			if app.jsonOut {
				b, _ := json.MarshalIndent(a, "", "  ")
				app.printf("%s\n", b)
				return nil
			}
			app.printf("%s", md.String())
			return nil
		},
	}
	f := cmd.Flags()
	f.StringSliceVar(&sessions, "sessions", nil, "directories holding bench direct work dirs with session transcripts (default: the cache's bench-direct)")
	f.StringSliceVar(&taskDirs, "task-dirs", []string{"benchmarks/direct", "benchmarks/tasks", "benchmarks/retrieval/agent-tasks"}, "task definitions, for turn limits and the files a task is about")
	f.StringVar(&out, "out", "", "write the analysis here (.json; .md beside it)")
	f.BoolVar(&heldout, "include-heldout", false, "analyse held-out runs too (they then no longer validate hypotheses drawn from this analysis independently)")
	f.IntVar(&examples, "examples", 5, "examples per pattern in the text report")
	return cmd
}

// expandReports expands glob patterns into report files (.json).
func expandReports(args []string) ([]string, error) {
	var out []string
	for _, a := range args {
		m, err := filepath.Glob(a)
		if err != nil {
			return nil, err
		}
		if len(m) == 0 {
			return nil, fmt.Errorf("%s: no such report", a)
		}
		for _, p := range m {
			if strings.HasSuffix(p, ".json") {
				out = append(out, p)
			}
		}
	}
	return out, nil
}
