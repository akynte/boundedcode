package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/llm"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect and initialise configuration",
	}
	cmd.AddCommand(newConfigInitCmd(), newConfigReferenceCmd(), newConfigShowCmd(), newConfigProfilesCmd())
	return cmd
}

// newConfigReferenceCmd configures the shipped Bonsai reference as one
// operation, then verifies that the external inference server is reachable.
func newConfigReferenceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reference",
		Short: "Configure and check the local Bonsai reference setup",
		Long: "Selects the Bonsai 2 27B profile, points inference at the local " +
			"llama-server on 127.0.0.1:8080, and routes every role to it. It replaces " +
			"providers.yaml with the single-provider reference configuration. Start " +
			"the server from Step 5 before running this command.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := openRoot()
			if err != nil {
				return err
			}
			defer closeRoot(cmd, root)

			cfg, err := loadConfig(root)
			if err != nil {
				return err
			}
			const baseURL = "http://127.0.0.1:8080"
			cfg.Profile = "bonsai-2-27b-8gb-cuda"
			cfg.Inference.Mode = config.ModeExternal
			cfg.Inference.BaseURL = baseURL
			cfg.Inference.Port = 8080
			cfg.Egress.Enabled = false

			dir := root.Layout().ConfigDir()
			if err := config.Save(dir, cfg); err != nil {
				return err
			}
			providers := llm.DefaultProvidersFile(baseURL, "boundedcode-bonsai")
			providers.Providers[0].TimeoutSeconds = 900
			providers.Roles = map[string]string{}
			if err := llm.SaveProvidersFile(dir, providers); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "configured %s and %s\n",
				config.Path(dir), filepath.Join(dir, "providers.yaml"))

			router, err := llm.NewRouter(providers)
			if err != nil {
				return err
			}
			defer router.Close()
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			for name, status := range router.Health(ctx) {
				fmt.Fprintf(cmd.OutOrStdout(), "%-20s %s\n", name, status)
				if status != "ok" {
					return fmt.Errorf("local inference is unreachable; confirm llama-server is running at %s", baseURL)
				}
			}
			return nil
		},
	}
}

func newConfigInitCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write default bcode.yaml and providers.yaml into the data directory",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := openRoot()
			if err != nil {
				return err
			}
			defer closeRoot(cmd, root)
			dir := root.Layout().ConfigDir()

			if _, err := os.Stat(config.Path(dir)); err == nil && !force {
				return fmt.Errorf("%s already exists; pass --force to overwrite", config.Path(dir))
			}
			cfg := config.Default()
			if err := config.Save(dir, cfg); err != nil {
				return err
			}
			providers := llm.DefaultProvidersFile(
				fmt.Sprintf("http://127.0.0.1:%d", cfg.Inference.Port), "local")
			if err := llm.SaveProvidersFile(dir, providers); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\nwrote %s\n",
				config.Path(dir), filepath.Join(dir, "providers.yaml"))
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing files")
	return cmd
}

func newConfigShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print the effective configuration and role routing",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := openRoot()
			if err != nil {
				return err
			}
			defer closeRoot(cmd, root)
			cfg, err := loadConfig(root)
			if err != nil {
				return err
			}
			out := map[string]any{"config": cfg}

			if f, err := llm.LoadProvidersFile(root.Layout().ConfigDir()); err == nil {
				if r, err := llm.NewRouter(f); err == nil {
					out["providers"] = r.Names()
					out["routing"] = r.Routing()
					r.Close()
				} else {
					out["providers_error"] = err.Error()
				}
			}
			if p := loadProfile(root, cfg); p != nil {
				out["profile"] = p
			} else {
				out["profile"] = "none loaded; conservative fallback values are in force"
			}
			return emitJSON(out)
		},
	}
	return cmd
}

func newConfigProfilesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "profiles",
		Short: "List available hardware profiles",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := openRoot()
			if err != nil {
				return err
			}
			defer closeRoot(cmd, root)
			cfg, _ := loadConfig(root)

			dir := profileDir(root)
			names, err := config.ListProfiles(dir)
			if err != nil {
				return err
			}
			for _, n := range names {
				marker := " "
				if n == cfg.Profile {
					marker = "*"
				}
				origin := "shipped"
				if config.OnDisk(dir, n) {
					origin = "yours"
				}
				desc := ""
				if p, err := config.LoadProfile(dir, n); err == nil {
					desc = p.Description
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %-32s %-8s %s\n", marker, n, origin, desc)
			}
			fmt.Fprintln(cmd.OutOrStdout(),
				"\n* is the active profile. Shipped profiles are starting points, not measurements:")
			fmt.Fprintln(cmd.OutOrStdout(),
				"  run `bcode models bench --write` to measure this machine.")
			return nil
		},
	}
}
