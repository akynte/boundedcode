package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/eval"
)

func newEvalRuntimeCmd() *cobra.Command {
	var dir string
	var only []string
	var asJSON bool
	var prepare bool
	cmd := &cobra.Command{
		Use:   "runtime",
		Short: "Prove each task's verification environment is usable before a batch",
		Long: "runtime checks the environment a task's own verification will run in.\n\n" +
			"The official grader answers a different question — does the gold patch\n" +
			"resolve this instance — and a task can pass that while this system cannot\n" +
			"verify it at all, because the checks belong to the upstream project and\n" +
			"need its interpreter, its compiler and its installed packages. A task that\n" +
			"names a pinned runtime is checked inside it: the image is present, it is\n" +
			"named by digest rather than by a tag, and every preset the repository\n" +
			"declares can actually start there.\n\n" +
			"Nothing here reads the acceptance key. The presets come from the\n" +
			"repository, and what the grader would say is a separate step.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			tasks, err := eval.LoadSet(dir)
			if err != nil {
				return err
			}
			want := map[string]bool{}
			for _, id := range only {
				want[id] = true
			}
			work, cleanup, err := eval.NewRuntimeWorkDir("bc-runtime-")
			if err != nil {
				return err
			}
			defer cleanup()

			w := cmd.OutOrStdout()
			var reports []eval.RuntimeReport
			var bad int
			for _, t := range tasks {
				if len(want) > 0 && !want[t.ID] {
					continue
				}
				if prepare {
					ref, err := eval.PrepareRuntime(cmd.Context(), t, work)
					if err != nil {
						fmt.Fprintf(w, "[FAIL] %-28s %v\n", t.ID, err)
						bad++
						continue
					}
					if ref != "" && ref != t.Origin.RuntimeImage {
						fmt.Fprintf(w, "[prep] %-28s %s\n", t.ID, ref)
						t.Origin.RuntimePreparedImage = ref
					}
				}
				rep := eval.CheckRuntime(cmd.Context(), t, work)
				reports = append(reports, rep)
				if rep.Usable() {
					if !asJSON {
						fmt.Fprintf(w, "[ok  ] %-28s %s\n", t.ID, rep.Image)
					}
					continue
				}
				bad++
				if !asJSON {
					fmt.Fprintf(w, "[FAIL] %-28s %s\n", t.ID, rep.Image)
					for _, b := range rep.Blockers() {
						fmt.Fprintf(w, "       %s\n", b)
					}
				}
			}
			if asJSON {
				enc := json.NewEncoder(w)
				enc.SetIndent("", " ")
				if err := enc.Encode(reports); err != nil {
					return err
				}
			}
			if bad > 0 {
				return fmt.Errorf("%d task(s) have no usable verification runtime", bad)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "tasks", "evals/tasks", "the task set directory")
	cmd.Flags().StringSliceVar(&only, "task", nil, "check only these task ids")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	cmd.Flags().BoolVar(&prepare, "prepare", false,
		"derive a runtime with the repository's dependencies already fetched, so the measured run needs no network")
	return cmd
}
