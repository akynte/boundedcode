package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/buildinfo"
	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/inference"
	"github.com/akynte/boundedcode/internal/secrets"
)

// Default endpoints of the cloud providers (the OpenAI-style base URLs end
// in the API version, as in the providers' own SDKs).
var defaultBaseURL = map[string]string{
	inference.ProviderOpenAI: "https://api.openai.com/v1",
	inference.ProviderGemini: inference.DefaultGeminiURL,
	// Anthropic: the SDK's production endpoint ("" here).
}

// secretStore is the credential store for provider API keys.
func (a *App) secretStore() *secrets.Store { return secrets.New(a.Paths.Config) }

// cloudKey returns the selected provider's API key, or an error naming how
// to set one.
func (a *App) cloudKey(provider string) (string, error) {
	key, _, err := a.secretStore().Get(provider)
	if errors.Is(err, secrets.ErrNotFound) {
		return "", fmt.Errorf("no API key for %s: add one with `%s` (Settings in the interface), `%s provider key set %s`, or the %s environment variable",
			provider, buildinfo.Command(), buildinfo.Command(), provider, secrets.EnvVar(provider))
	}
	return key, err
}

// upstreamFor builds the Upstream of a cloud provider. replay may be nil
// (host-side calls without a task); requestTimeout bounds one attempt.
func upstreamFor(provider string, p config.ProviderConfig, key string, replay inference.ReplayStore, requestTimeout time.Duration) (inference.Upstream, error) {
	base := p.BaseURL
	if base == "" {
		base = defaultBaseURL[provider]
	}
	switch provider {
	case inference.ProviderAnthropic:
		return &inference.AnthropicUpstream{APIKey: key, BaseURL: p.BaseURL, Model: p.Model, Effort: anthropicEffort(p.Effort),
			Timeout: requestTimeout, Replay: replay}, nil
	case inference.ProviderGemini:
		return &inference.GeminiUpstream{APIKey: key, BaseURL: base, Model: p.Model, Effort: geminiEffort(p.Effort),
			Timeout: requestTimeout, Replay: replay, Retry: inference.CloudRetry}, nil
	case inference.ProviderOpenAI, inference.ProviderOpenAICompatible:
		if base == "" {
			return nil, fmt.Errorf("inference.providers.%s.base_url is required", provider)
		}
		c := inference.NewClient(base, requestTimeout)
		if key != "" {
			c.Header = http.Header{"Authorization": {"Bearer " + key}}
		}
		return &openAIEffortUpstream{OpenAIUpstream: &inference.OpenAIUpstream{Client: c, Name: provider, Path: "/chat/completions",
			Cloud: true, Retry: inference.CloudRetry}, effort: p.Effort}, nil
	}
	return nil, fmt.Errorf("unknown provider %q", provider)
}

// openAIEffortUpstream adds reasoning_effort to OpenAI requests when set.
type openAIEffortUpstream struct {
	*inference.OpenAIUpstream
	effort string
}

func (u *openAIEffortUpstream) Complete(ctx context.Context, body map[string]any) (int, []byte, error) {
	if u.effort != "" {
		if _, set := body["reasoning_effort"]; !set {
			body["reasoning_effort"] = u.effort
		}
	}
	return u.OpenAIUpstream.Complete(ctx, body)
}

// anthropicEffort maps the shared effort scale to Anthropic's levels.
func anthropicEffort(e string) string {
	if e == "minimal" {
		return "low"
	}
	return e
}

// geminiEffort maps the shared effort scale to Gemini thinking levels.
func geminiEffort(e string) string {
	switch e {
	case "xhigh", "max":
		return "high"
	}
	return e
}

// limiter is an upstream that can report a model's limits.
type limiter interface {
	Limits(ctx context.Context) (inference.ModelLimits, error)
}

// lister is an upstream that can list models.
type lister interface {
	Models(ctx context.Context) ([]inference.ModelLimits, error)
}

// cloudContext returns the context size the agent works with: the model's
// window (configured, or reported by the provider) capped by
// context_limit. It also verifies the key where the provider can report
// limits.
func cloudContext(ctx context.Context, provider string, p config.ProviderConfig, up inference.Upstream) (int, error) {
	window := p.ContextWindow
	if l, ok := up.(limiter); ok {
		lim, err := l.Limits(ctx)
		if errors.Is(err, inference.ErrAuth) {
			return 0, fmt.Errorf("%w; replace the key with `%s provider key set %s` (or in Settings)", err, buildinfo.Command(), provider)
		}
		if err != nil && window == 0 {
			return 0, fmt.Errorf("%s: cannot read the limits of model %q: %w", provider, p.Model, err)
		}
		if window == 0 {
			window = lim.ContextWindow
		}
	}
	if window <= 0 {
		return 0, fmt.Errorf("%s does not report context windows: set inference.providers.%s.context_window (in Settings or `%s provider set %s --context-window N`)",
			provider, provider, buildinfo.Command(), provider)
	}
	limit := p.ContextLimit
	if limit <= 0 {
		limit = config.DefaultContextLimit
	}
	return min(window, limit), nil
}

// cloudEndpoint is the provider side of a task run in cloud mode.
type cloudEndpoint struct {
	provider string
	model    string
	ctxSize  int
	// upstream builds the per-task upstream (its replay file lives in the
	// task directory).
	upstream func(taskDir string) inference.Upstream
}

// prepareCloud resolves the selected cloud provider for a run: its key, its
// model (modelOverride, from --model, wins) and its context size. It fails
// before any work when the key is missing or rejected.
func (a *App) prepareCloud(ctx context.Context, modelOverride string) (*cloudEndpoint, error) {
	ic := a.Config.Inference
	p := ic.Cloud()
	if modelOverride != "" {
		p.Model = modelOverride
	}
	key, err := a.cloudKey(ic.Provider)
	if err != nil {
		return nil, err
	}
	timeout := ic.RequestTimeout.D()
	probe, err := upstreamFor(ic.Provider, p, key, nil, timeout)
	if err != nil {
		return nil, err
	}
	pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	size, err := cloudContext(pctx, ic.Provider, p, probe)
	if err != nil {
		return nil, err
	}
	return &cloudEndpoint{provider: ic.Provider, model: p.Model, ctxSize: size,
		upstream: func(taskDir string) inference.Upstream {
			var replay inference.ReplayStore = &inference.MemoryReplay{}
			if taskDir != "" {
				replay = &inference.FileReplay{Path: filepath.Join(taskDir, "provider-replay.jsonl")}
			}
			up, _ := upstreamFor(ic.Provider, p, key, replay, timeout) // validated above
			return up
		}}, nil
}

// providerLabel is a short description of the selected provider and model.
func (a *App) providerLabel() string {
	ic := a.Config.Inference
	if !ic.IsCloud() {
		return "local " + a.Config.DefaultModel
	}
	return ic.Provider + " " + ic.Cloud().Model
}

// normalizeProvider accepts common spellings of provider names.
func normalizeProvider(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "claude":
		s = inference.ProviderAnthropic
	case "google":
		s = inference.ProviderGemini
	case "compatible", "openai_compatible":
		s = inference.ProviderOpenAICompatible
	}
	for _, p := range inference.Providers {
		if s == p {
			return s, nil
		}
	}
	return "", fmt.Errorf("unknown provider %q (one of %s)", s, strings.Join(inference.Providers, ", "))
}
