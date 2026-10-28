package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/ecosystem"
	"github.com/akynte/boundedcode/internal/lang"
)

func newCapabilitiesCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "capabilities [dir]",
		Short: "Show the languages, projects and tools BoundedCode finds in a repository",
		Long: "Lists every language in the repository with the check run after each edit, the formatter, linter and language server, " +
			"and every project (nested ones too) with its build and test commands: available, missing tool, or unsupported. " +
			"Every text file gets generic support (read, edit, search, symbol search, shell, git) whatever its language.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// The directory given, or the repository around the working
			// directory.
			root := ""
			if len(args) == 1 {
				abs, err := filepath.Abs(args[0])
				if err != nil {
					return err
				}
				root = abs
			} else {
				wd, err := os.Getwd()
				if err != nil {
					return err
				}
				if root, err = projectRoot(wd); err != nil {
					root = wd
				}
			}
			if st, err := os.Stat(root); err != nil || !st.IsDir() {
				return fmt.Errorf("%s is not a directory", root)
			}
			rep := ecosystem.Capabilities(root, &lang.Finder{Exclude: root}, ecosystem.Options{MaxProjects: 40})
			if app.jsonOut {
				return app.printJSON(rep)
			}
			app.printf("%s\n\n%s", root, rep.String())
			return nil
		},
	}
}
