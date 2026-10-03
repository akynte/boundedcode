package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/config"
)

func newInitCmd(app *App) *cobra.Command {
	var (
		force                                       bool
		modelsDir, serverBin, benchBin, externalURL string
		adapterDir                                  string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create the user configuration and state directories",
		Long: `Writes ~/.config/boundedcode/config.yaml (or $BOUNDEDCODE_HOME/config/config.yaml)
with explicit paths. Existing configuration is never overwritten without --force.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := app.Paths.Ensure(); err != nil {
				return err
			}
			path := app.configFile()
			if _, err := os.Stat(path); err == nil && !force {
				return fmt.Errorf("%s already exists (use --force to overwrite)", path)
			} else if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			cfg := config.Defaults()
			if modelsDir != "" {
				abs, err := filepath.Abs(modelsDir)
				if err != nil {
					return err
				}
				cfg.ModelsDir = abs
			} else {
				cfg.ModelsDir = filepath.Join(app.Paths.Data, "models")
			}
			if serverBin != "" {
				cfg.Inference.ServerBinary = absIfPath(serverBin)
			}
			if benchBin != "" {
				cfg.Inference.BenchBinary = absIfPath(benchBin)
			}
			if externalURL != "" {
				cfg.Inference.Mode = "external"
				cfg.Inference.ExternalURL = externalURL
			}
			if adapterDir != "" {
				cfg.Agent.AdapterDir = absIfPath(adapterDir)
			}
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			if _, err := app.Store(cmd.Context()); err != nil {
				return err
			}
			app.printf("wrote %s\nstate database: %s\nnext: %s doctor\n", path, app.Paths.StateDB(), cmd.Root().Name())
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&force, "force", false, "overwrite an existing config")
	f.StringVar(&modelsDir, "models-dir", "", "directory containing GGUF files")
	f.StringVar(&serverBin, "llama-server", "", "path to llama-server")
	f.StringVar(&benchBin, "llama-bench", "", "path to llama-bench")
	f.StringVar(&externalURL, "external-url", "", "use an already-running OpenAI-compatible server instead of managing one")
	f.StringVar(&adapterDir, "adapter-dir", "", "path to the OpenHands adapter project")
	return cmd
}

func absIfPath(p string) string {
	if filepath.Base(p) == p { // bare command name resolved via PATH
		return p
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}
