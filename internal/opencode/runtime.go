package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RuntimeBudget is read from the two production authorities: OpenCode's
// project config and the live Prism slot. Validation happens before a long
// supervised run, so a stale 32K/64K pairing cannot compact unpredictably.
type RuntimeBudget struct {
	Model             string `json:"model"`
	AdvertisedContext int    `json:"advertised_context"`
	PhysicalContext   int    `json:"physical_context"`
	OutputReserve     int    `json:"output_reserve"`
	CompactionBuffer  int    `json:"compaction_buffer"`
	RetainedTail      int    `json:"retained_tail"`
}

func ValidateRuntime(ctx context.Context, repoRoot, baseURL string) (RuntimeBudget, error) {
	var result RuntimeBudget
	body, err := os.ReadFile(filepath.Join(repoRoot, "opencode.json"))
	if err != nil {
		return result, err
	}
	var cfg struct {
		Model     string `json:"model"`
		Providers map[string]struct {
			Models map[string]struct {
				Limit struct {
					Context int `json:"context"`
					Output  int `json:"output"`
				} `json:"limit"`
			} `json:"models"`
		} `json:"providers"`
		Compaction struct {
			Buffer int `json:"buffer"`
			Keep   struct {
				Tokens int `json:"tokens"`
			} `json:"keep"`
		} `json:"compaction"`
	}
	if err := json.Unmarshal(body, &cfg); err != nil {
		return result, err
	}
	parts := strings.SplitN(cfg.Model, "/", 2)
	if len(parts) != 2 {
		return result, fmt.Errorf("OpenCode model %q is not provider/model", cfg.Model)
	}
	provider, ok := cfg.Providers[parts[0]]
	if !ok {
		return result, fmt.Errorf("OpenCode provider %q is missing", parts[0])
	}
	model, ok := provider.Models[parts[1]]
	if !ok {
		return result, fmt.Errorf("OpenCode model metadata for %q is missing", cfg.Model)
	}
	result.Model = cfg.Model
	result.AdvertisedContext = model.Limit.Context
	result.OutputReserve = model.Limit.Output
	result.CompactionBuffer = cfg.Compaction.Buffer
	result.RetainedTail = cfg.Compaction.Keep.Tokens
	u, err := url.Parse(baseURL)
	if err != nil {
		return result, err
	}
	if u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1" {
		return result, fmt.Errorf("runtime metadata requires a local endpoint")
	}
	u.Path = "/props"
	u.RawQuery = ""
	client := &http.Client{Timeout: 4 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return result, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return result, fmt.Errorf("reading local Prism context: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("Prism /props returned %s", resp.Status)
	}
	var props struct {
		DefaultGenerationSettings struct {
			NCTX int `json:"n_ctx"`
		} `json:"default_generation_settings"`
		ModelAlias string `json:"model_alias"`
		TotalSlots int    `json:"total_slots"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&props); err != nil {
		return result, err
	}
	result.PhysicalContext = props.DefaultGenerationSettings.NCTX
	if props.TotalSlots != 1 {
		return result, fmt.Errorf("runtime metadata requires one model slot; Prism reports %d", props.TotalSlots)
	}
	if props.ModelAlias != parts[1] {
		return result, fmt.Errorf("OpenCode model %q disagrees with Prism alias %q", parts[1], props.ModelAlias)
	}
	if result.PhysicalContext <= 0 || result.PhysicalContext != result.AdvertisedContext {
		return result, fmt.Errorf("OpenCode advertises %d tokens but Prism provides %d; synchronize both before supervised work", result.AdvertisedContext, result.PhysicalContext)
	}
	if result.OutputReserve <= 0 || result.OutputReserve >= result.PhysicalContext {
		return result, fmt.Errorf("invalid OpenCode output reserve %d", result.OutputReserve)
	}
	if result.CompactionBuffer <= result.RetainedTail {
		return result, fmt.Errorf("OpenCode compaction buffer %d is not larger than retained tail %d", result.CompactionBuffer, result.RetainedTail)
	}
	return result, nil
}
