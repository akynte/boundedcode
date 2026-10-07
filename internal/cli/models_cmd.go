package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/akynte/boundedcode/internal/buildinfo"
	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/hw"
	"github.com/akynte/boundedcode/internal/model"
	"github.com/akynte/boundedcode/internal/secrets"
)

// hwCache keeps one hardware probe per process for a minute: it runs
// nvidia-smi, and the interface asks for model rows on every refresh.
var hwCache struct {
	sync.Mutex
	at   time.Time
	snap hw.Snapshot
}

// hardware returns the machine's hardware (cached).
func (a *App) hardware(ctx context.Context) hw.Snapshot {
	hwCache.Lock()
	defer hwCache.Unlock()
	if hwCache.at.IsZero() || time.Since(hwCache.at) > time.Minute {
		hwCache.snap, hwCache.at = hw.Probe(ctx), time.Now()
	}
	return hwCache.snap
}

// hfTokenName is the credential-store name of a Hugging Face token.
const hfTokenName = "huggingface"

// modelsDir is where weights are downloaded.
func (a *App) modelsDir() string {
	if a.Config.ModelsDir != "" {
		return a.Config.ModelsDir
	}
	return filepath.Join(a.Paths.Data, "models")
}

// fetchModel downloads a profile's weights with progress on a.Out.
func (a *App) fetchModel(ctx context.Context, p model.Profile) (string, error) {
	token, _, _ := a.secretStore().Get(hfTokenName)
	last := -1
	d := &model.Downloader{Token: token, Progress: func(done, total int64) {
		if total <= 0 {
			return
		}
		pct := int(done * 100 / total)
		if pct/5 != last/5 || done == total {
			last = pct
			a.printf("  %3d%%  %.2f / %.2f GB\n", pct, float64(done)/1e9, float64(total)/1e9)
		}
	}}
	path, err := d.Fetch(ctx, p, a.modelsDir())
	if errors.Is(err, model.ErrGated) {
		return "", fmt.Errorf("%w; then run `%s model token set`", err, buildinfo.Command())
	}
	return path, err
}

// licenseNotice is what a user accepts before a download.
func licenseNotice(p model.Profile) string {
	n := fmt.Sprintf("%s is licensed %s", p.DisplayName, p.Source.License)
	if p.Source.LicenseURL != "" {
		n += " (" + p.Source.LicenseURL + ")"
	}
	n += "."
	if p.Source.LicenseNotice != "" {
		n += " " + p.Source.LicenseNotice
	}
	return n
}

func formatGB(b int64) string {
	if b <= 0 {
		return "?"
	}
	return fmt.Sprintf("%.1f GB", float64(b)/1e9)
}

func newModelCatalogCmds(app *App) []*cobra.Command {
	recommend := &cobra.Command{Use: "recommend", Short: "Show this machine's hardware and the model that suits it", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			snap := app.hardware(cmd.Context())
			rec := model.Recommend(app.Models, app.Config.DefaultModel, snap)
			if app.jsonOut {
				return app.printJSON(map[string]any{"hardware": snap, "recommendation": rec})
			}
			app.printf("machine: %s\n", describeHardware(snap))
			if rec.Best.Profile == "" {
				app.printf("recommendation: none: %s\n", rec.Reason)
				return nil
			}
			app.printf("recommendation: %s: %s\n  %s\n", rec.Best.Profile, rec.Reason, rec.Best.Detail)
			if rec.Best.Profile != app.Config.DefaultModel {
				app.printf("to use it: %s model use %s\n", buildinfo.Command(), rec.Best.Profile)
			}
			return nil
		}}

	var yes bool
	fetch := &cobra.Command{Use: "fetch NAME", Short: "Download a model's weights (pinned revision, sha256-verified, resumable)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := app.Models.Get(args[0])
			if err != nil {
				return err
			}
			if p.Status == model.StatusReview {
				return fmt.Errorf("%s's license is under review; it is not offered for download", p.Name)
			}
			app.printf("%s\n", licenseNotice(p))
			if !yes && !confirm(app, fmt.Sprintf("Download %s (%s) into %s? [y/N] ", p.DisplayName, formatGB(p.Source.SizeBytes), app.modelsDir())) {
				return errors.New("download declined")
			}
			path, err := app.fetchModel(cmd.Context(), p)
			if err != nil {
				return err
			}
			app.printf("downloaded and verified: %s\n", path)
			return nil
		}}
	fetch.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask (you have read the license)")

	use := &cobra.Command{Use: "use NAME", Short: "Make a model the default and the local provider the active one", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := app.Models.Get(args[0])
			if err != nil {
				return err
			}
			if err := app.updateConfig(func(c *config.Config) {
				c.DefaultModel = p.Name
				c.Inference.Provider = "local"
			}); err != nil {
				return err
			}
			app.printf("default model: %s\n", p.Name)
			if _, err := os.Stat(p.ResolveFile(app.modelsDir())); err != nil {
				app.printf("its weights are not downloaded yet: %s model fetch %s\n", buildinfo.Command(), p.Name)
			}
			if f := model.FitFor(p, app.hardware(cmd.Context())); !f.Fast() {
				app.printf("note: %s (%s)\n", f.Detail, f.Level)
			}
			return nil
		}}

	var removeYes bool
	remove := &cobra.Command{Use: "remove NAME", Short: "Delete a model's downloaded weights", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			p, err := app.Models.Get(args[0])
			if err != nil {
				return err
			}
			path := p.ResolveFile(app.modelsDir())
			fi, err := os.Stat(path)
			if err != nil {
				return fmt.Errorf("%s: no downloaded weights at %s", p.Name, path)
			}
			if !removeYes && !confirm(app, fmt.Sprintf("Delete %s (%s)? [y/N] ", path, formatGB(fi.Size()))) {
				return errors.New("not deleted")
			}
			if err := os.Remove(path); err != nil {
				return err
			}
			_ = os.Remove(path + ".part")
			app.printf("deleted %s\n", path)
			if p.Name == app.Config.DefaultModel && !app.Config.Inference.IsCloud() {
				app.printf("note: it is the default model; choose another with `%s model use`\n", buildinfo.Command())
			}
			return nil
		}}

	remove.Flags().BoolVarP(&removeYes, "yes", "y", false, "do not ask")

	token := &cobra.Command{Use: "token", Short: "Store or remove a Hugging Face token (for gated model repositories)"}
	token.AddCommand(&cobra.Command{Use: "set", Short: "Store a Hugging Face token (read without echo, or from stdin)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			v, err := readSecret(cmd.InOrStdin(), app.Err, "Hugging Face token: ")
			if err != nil {
				return err
			}
			src, err := app.secretStore().Set(hfTokenName, v)
			if err != nil {
				return err
			}
			app.printf("stored the token in the %s\n", describeSource(src, app))
			return nil
		}}, &cobra.Command{Use: "delete", Short: "Remove the stored Hugging Face token", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if err := app.secretStore().Delete(hfTokenName); err != nil {
				return err
			}
			app.printf("removed the Hugging Face token\n")
			if os.Getenv(secrets.EnvVar(hfTokenName)) != "" {
				app.printf("note: %s is still set in the environment\n", secrets.EnvVar(hfTokenName))
			}
			return nil
		}})
	return []*cobra.Command{recommend, fetch, use, remove, token}
}

// describeHardware is one line about the machine.
func describeHardware(s hw.Snapshot) string {
	parts := []string{fmt.Sprintf("%s/%s", s.OS, s.Arch)}
	if s.CPUModel != "" {
		parts = append(parts, strings.TrimSpace(s.CPUModel))
	}
	parts = append(parts, fmt.Sprintf("%.0f GB RAM", float64(s.MemTotalMiB)/1024))
	switch s.Accelerator.Kind {
	case hw.AccelCUDA:
		parts = append(parts, fmt.Sprintf("%s (%.0f GB, CUDA)", s.Accelerator.Name, float64(s.Accelerator.MemoryMiB)/1024))
	case hw.AccelMetal:
		est := ""
		if s.Accelerator.Estimated {
			est = " estimated"
		}
		parts = append(parts, fmt.Sprintf("Apple GPU, about %.0f GB usable%s (Metal, unified memory)", float64(s.Accelerator.MemoryMiB)/1024, est))
	default:
		parts = append(parts, "no supported GPU (CPU only)")
	}
	return strings.Join(parts, ", ")
}
