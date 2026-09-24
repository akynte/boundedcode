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
	// The original compaction-loop defect (docs/explanation/opencode-context.md)
	// had buffer=20000 and keep=15000: that satisfies buffer > retainedTail above,
	// yet the preflight threshold (physicalContext - buffer = 12,768) was already
	// below retainedTail, so the rebuilt request carried more than the trigger
	// before OpenCode's system prompt, tool schemas and new summary were even
	// added — a request that must recompact again as soon as it is built. The
	// buffer > retainedTail check alone cannot see this; only the margin can.
	threshold := result.PhysicalContext - result.CompactionBuffer
	margin := threshold - result.RetainedTail
	if margin < minCompactionSafetyMargin {
		return result, fmt.Errorf(
			"compaction margin %d tokens (threshold %d − retained tail %d) is below the %d-token "+
				"floor a rebuilt request's system prompt, AGENTS.md and tool schemas need; this is how "+
				"the original compact→compact→compact loop happened even though the buffer exceeds the "+
				"retained tail",
			margin, threshold, result.RetainedTail, minCompactionSafetyMargin)
	}
	return result, nil
}

// ValidateExternalRuntime verifies an OpenAI-compatible endpoint without
// requiring Prism's private /props extension. External servers do not expose a
// single-slot physical context contract, so the profile and generated OpenCode
// limits are the authority; the endpoint still has to answer before the editor
// is launched.
func ValidateExternalRuntime(ctx context.Context, repoRoot, baseURL string) (RuntimeBudget, error) {
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
	if result.AdvertisedContext <= 0 || result.OutputReserve <= 0 || result.OutputReserve >= result.AdvertisedContext {
		return result, fmt.Errorf("OpenCode model limits are invalid: context=%d output=%d", result.AdvertisedContext, result.OutputReserve)
	}
	if result.CompactionBuffer <= result.RetainedTail || result.AdvertisedContext-result.CompactionBuffer-result.RetainedTail < minCompactionSafetyMargin {
		return result, fmt.Errorf("OpenCode compaction policy does not leave the required safety margin")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return result, err
	}
	if u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1" {
		return result, fmt.Errorf("external runtime requires a local endpoint")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/models"
	if u.Path == "/models" {
		u.Path = "/v1/models"
	}
	u.RawQuery = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return result, err
	}
	resp, err := (&http.Client{Timeout: 4 * time.Second}).Do(req)
	if err != nil {
		return result, fmt.Errorf("reading external inference endpoint: %w", err)
	}
	_ = resp.Body.Close()
	return result, nil
}

// minCompactionSafetyMargin is the floor for threshold-minus-retained-tail.
//
// The measured post-adapter first-request total was 7,494 tokens
// (docs/explanation/opencode-context.md) for OpenCode's own system prompt,
// AGENTS.md, built-in and MCP tool schemas, and the BoundedCode task card,
// before any conversation content. 8,000 gives that observed cost headroom
// rather than passing at the exact edge of one measurement.
const minCompactionSafetyMargin = 8000
