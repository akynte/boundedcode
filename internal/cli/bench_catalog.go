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

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/benchmark"
)

func newBenchCatalogCmd(app *App) *cobra.Command {
	var (
		check            bool
		mdOut, jsonOut   string
		image, modelName string
	)
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "List every benchmark task with its group, capability, languages, split and provenance, and check the suites' integrity",
		Long: `Reads the direct-mode suite (benchmarks/direct and the real held-out
tasks in benchmarks/retrieval/agent-tasks), the bounded suite
(benchmarks/tasks) and the verification corpus. It reports coverage by
language, group and capability, which tasks can run on this machine (and,
with --image, in the sandbox image), contamination against a model's
publication date, and integrity problems: missing metadata, sources not
pinned to an exact version or commit, hidden files the agent could read in
the fixture, a group without held-out tasks. --check exits 1 on a problem.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			here := func(t string) bool { _, err := exec.LookPath(t); return err == nil }
			var inImage func(string) bool
			if image != "" {
				have, err := imageTools(ctx, image)
				if err != nil {
					return err
				}
				inImage = func(t string) bool { return have[t] }
			}
			c, err := benchmark.BuildCatalog(ctx, []benchmark.CatalogSource{
				{Suite: "direct", Dir: "benchmarks/direct"}, {Suite: "direct", Dir: "benchmarks/retrieval/agent-tasks"},
				{Suite: "bounded", Dir: "benchmarks/tasks"}}, "benchmarks/fixtures", here, inImage)
			if err != nil {
				return err
			}
			published := ""
			if p, err := app.profile(modelName); err == nil {
				modelName, published = p.Name, p.Source.Published
			}
			var md bytes.Buffer
			benchmark.WriteCatalogMarkdown(&md, c, modelName, published)
			if mdOut != "" {
				if err := os.WriteFile(mdOut, md.Bytes(), 0o644); err != nil {
					return err
				}
			}
			if jsonOut != "" {
				b, _ := json.MarshalIndent(c, "", "  ")
				if err := os.WriteFile(jsonOut, append(b, '\n'), 0o644); err != nil {
					return err
				}
			}
			switch {
			case app.jsonOut:
				if err := app.printJSON(c); err != nil {
					return err
				}
			case mdOut == "" && jsonOut == "":
				app.printf("%s", md.String())
			default:
				app.printf("%d tasks, %d integrity problems\n", len(c.Entries), len(c.Problems))
			}
			if check && len(c.Problems) > 0 {
				return fmt.Errorf("%d integrity problems:\n- %s", len(c.Problems), strings.Join(c.Problems, "\n- "))
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&check, "check", false, "exit 1 when an integrity problem is found (for CI)")
	f.StringVar(&mdOut, "markdown", "", "write the coverage report here")
	f.StringVar(&jsonOut, "out", "", "write the catalog as JSON here")
	f.StringVar(&image, "image", "", "also check which tools the bounded and verification tasks need are in this sandbox image (docker)")
	f.StringVar(&modelName, "model", "", "model profile for the contamination view (default: the configured one)")
	return cmd
}

// imageTools lists which of the suites' tools image has.
func imageTools(ctx context.Context, image string) (map[string]bool, error) {
	tools := []string{"go", "node", "npm", "python3", "cargo", "rustc", "java", "javac", "mvn", "gradle", "cmake", "ninja", "ctest", "make", "php", "ruby", "rake", "dotnet", "sh", "bash", "grep", "git"}
	script := "for t in " + strings.Join(tools, " ") + "; do command -v $t >/dev/null 2>&1 && echo $t; done"
	out, err := exec.CommandContext(ctx, "docker", "run", "--rm", "--network", "none", "--entrypoint", "sh", image, "-c", script).Output()
	if err != nil {
		return nil, fmt.Errorf("probe %s: %w", image, err)
	}
	have := map[string]bool{}
	for _, t := range strings.Fields(string(out)) {
		have[t] = true
	}
	return have, nil
}

func newBenchScorecardCmd(app *App) *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "scorecard REPORT...",
		Short: "Combine trials (reports of one suite) into a machine-readable scorecard with uncertainty, per language, group and split",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			meta := map[string]benchmark.CatalogEntry{}
			if c, err := benchmark.BuildCatalog(cmd.Context(), []benchmark.CatalogSource{
				{Suite: "direct", Dir: "benchmarks/direct"}, {Suite: "direct", Dir: "benchmarks/retrieval/agent-tasks"},
				{Suite: "bounded", Dir: "benchmarks/tasks"}}, "benchmarks/fixtures", nil, nil); err == nil {
				for _, e := range c.Entries {
					meta[e.ID] = e
				}
			}
			sc, err := benchmark.BuildScorecard(args, meta)
			if err != nil {
				return err
			}
			var md bytes.Buffer
			benchmark.WriteScorecardMarkdown(&md, sc)
			if out != "" {
				b, _ := json.MarshalIndent(sc, "", "  ")
				if err := os.WriteFile(out, append(b, '\n'), 0o644); err != nil {
					return err
				}
				if err := os.WriteFile(strings.TrimSuffix(out, ".json")+".md", md.Bytes(), 0o644); err != nil {
					return err
				}
			}
			if app.jsonOut {
				return app.printJSON(sc)
			}
			app.printf("%s", md.String())
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "write the scorecard here (.json; .md beside it)")
	return cmd
}

func newBenchGateCmd(app *App) *cobra.Command {
	var base, cand, policy, dir string
	cmd := &cobra.Command{
		Use:   "gate",
		Short: "Compare a candidate scorecard with a baseline under a regression policy; exit 1 when a blocking rule regresses",
		Long: `With --baseline and --candidate, compares the two. With --dir, compares
each suite directory's candidate.json with its baseline.json (a suite
without a candidate passes): the layout CI gates on.

Rules allow for the suite's own noise (see benchmarks/gate-policy.yaml);
scorecards of other models, task sets or budgets are refused as not
comparable.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := benchmark.LoadGatePolicy(policy)
			if err != nil {
				return err
			}
			pairs := [][3]string{}
			switch {
			case dir != "":
				suites, err := os.ReadDir(dir)
				if err != nil {
					return err
				}
				for _, s := range suites {
					c := filepath.Join(dir, s.Name(), "candidate.json")
					if _, err := os.Stat(c); err == nil && s.IsDir() {
						pairs = append(pairs, [3]string{s.Name(), filepath.Join(dir, s.Name(), "baseline.json"), c})
					}
				}
				if len(pairs) == 0 {
					app.printf("no candidate scorecards under %s: nothing to gate\n", dir)
					return nil
				}
			case base != "" && cand != "":
				pairs = append(pairs, [3]string{"", base, cand})
			default:
				return fmt.Errorf("give --baseline and --candidate, or --dir")
			}
			failed := false
			for _, pr := range pairs {
				b, err := readScorecard(pr[1])
				if err != nil {
					return err
				}
				c, err := readScorecard(pr[2])
				if err != nil {
					return err
				}
				g := benchmark.Gate(b, c, p)
				if app.jsonOut {
					if err := app.printJSON(map[string]any{"suite": pr[0], "gate": g}); err != nil {
						return err
					}
				} else {
					if pr[0] != "" {
						app.printf("## %s\n\n", pr[0])
					}
					var md bytes.Buffer
					benchmark.WriteGateMarkdown(&md, g)
					app.printf("%s\n", md.String())
				}
				failed = failed || g.Failed
			}
			if failed {
				return benchmark.ErrGateFailed
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&base, "baseline", "", "baseline scorecard")
	f.StringVar(&cand, "candidate", "", "candidate scorecard")
	f.StringVar(&dir, "dir", "", "a directory of suites, each with baseline.json and maybe candidate.json")
	f.StringVar(&policy, "policy", "benchmarks/gate-policy.yaml", "regression policy")
	return cmd
}

func readScorecard(path string) (*benchmark.Scorecard, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sc benchmark.Scorecard
	if err := json.Unmarshal(b, &sc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if sc.Schema != benchmark.ScorecardSchema {
		return nil, fmt.Errorf("%s: scorecard schema %d is not %d", path, sc.Schema, benchmark.ScorecardSchema)
	}
	return &sc, nil
}
