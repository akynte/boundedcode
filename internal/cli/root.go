package cli

import (
	"context"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/akynte/boundedcode/internal/buildinfo"
)

// Execute runs the CLI and returns the process exit code.
func Execute(ctx context.Context, args []string) int {
	app := &App{Out: os.Stdout, Err: os.Stderr}
	// Like other coding agents, running the program with no arguments in a
	// terminal opens the interactive interface for the current directory.
	if len(args) == 0 && term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) {
		args = []string{"tui"}
	}
	root := newRoot(app)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	app.close()
	if err != nil {
		root.PrintErrln("error:", err)
		return 1
	}
	return 0
}

func newRoot(app *App) *cobra.Command {
	var verbose bool
	root := &cobra.Command{
		Use:           buildinfo.Command(),
		Short:         buildinfo.ProductName + ": local-first control plane for AI-assisted software engineering",
		Version:       buildinfo.Version + " " + buildinfo.Commit(),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			app.Log = newLogger(app.Err, verbose)
			return app.load()
		},
	}
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "debug logging to stderr")
	root.PersistentFlags().BoolVar(&app.jsonOut, "json", false, "machine-readable JSON output where supported")
	root.AddCommand(
		newVersionCmd(app),
		newInitCmd(app),
		newDoctorCmd(app),
		newRuntimeCmd(app),
		newModelCmd(app),
		newBenchCmd(app),
		newWorkspaceCmd(app),
		newIndexCmd(app),
		newIntelCmd(app),
		newTaskCmd(app),
		newVerifyCmd(app),
		newFrontierCmd(app),
		newSandboxCmd(app),
		newSerenaCmd(app),
		newStatsCmd(app),
		newTUICmd(app),
		newSetupCmd(app),
		newProviderCmd(app),
	)
	return root
}

func newVersionCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(*cobra.Command, []string) error {
			if app.jsonOut {
				return app.printJSON(map[string]string{"name": buildinfo.Name, "version": buildinfo.Version, "commit": buildinfo.Commit()})
			}
			app.printf("%s %s %s\n", buildinfo.Name, buildinfo.Version, buildinfo.Commit())
			return nil
		},
	}
}
