// Package setup contains the deterministic pieces behind `bcode setup`.
//
// The terminal UI lives in cmd/bcode so it can use Cobra's input and output
// streams. Everything that changes state lives here instead: discovery,
// idempotent configuration, model download, credential storage, and validation.
// Keeping those operations out of the UI makes a setup run reproducible in a
// test and, more importantly, makes it possible for `bcode opencode` to apply
// the same safety checks without ever asking the user a second set of
// questions.
package setup

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/llm"
	"github.com/akynte/boundedcode/internal/store"
)

// MarkerFile records that the interactive setup reached a successful,
// validated state. It is deliberately separate from bcode.yaml: a user may
// keep an old configuration and re-run setup, while this file answers the
// narrower question "has the guided setup been completed?".
const MarkerFile = "setup.json"

// CredentialFile is an owner-only environment fragment used for the hosted
// decision-plane credential. The value is never written to YAML or printed by
// the setup UI.
const CredentialFile = "jev.env"

// DefaultModelURL is the reference GGUF offered by the setup TUI. A caller can
// override it with --model-url, which is useful for a mirror or a private
// artifact store without creating a second installation path.
const DefaultModelURL = "https://huggingface.co/prism-ml/Ternary-Bonsai-2-27B-gguf/resolve/6ed5e12b/Ternary-Bonsai-2-27B-PTQ1_0.gguf"

// DefaultModelName is the file name used when the model is downloaded.
const DefaultModelName = "Ternary-Bonsai-2-27B-PTQ1_0.gguf"

// DefaultModelSHA256 is the checksum of the pinned reference artifact. A
// caller downloading from a different mirror can replace it explicitly.
const DefaultModelSHA256 = "53107f530aa52eb00912263ab1ee29bd199261c87cd7b4ad4ca1318c1fe33ee3"

// DefaultInferencePort is deliberately separate from the supervisor API port.
// The session launcher owns the API address dynamically, while this port is
// the stable loopback model endpoint registered with OpenCode.
const DefaultInferencePort = 8080

// Check is one dependency or readiness result shown by the TUI.
type Check struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Detail   string `json:"detail"`
	Fix      string `json:"fix,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// Options controls a non-interactive setup run. Interactive callers normally
// leave the zero values alone and use the command's prompts.
type Options struct {
	DataDir        string
	NonInteractive bool
	AssumeYes      bool
	DownloadModel  bool
	SkipJudgment   bool
	ModelURL       string
	ModelSHA256    string
	RuntimeBinary  string
	// InstallRuntime authorizes the pinned Prism build when no local
	// llama-server is available. The interactive command layer still asks for
	// confirmation; non-interactive callers can use it as the explicit yes.
	InstallRuntime   bool
	ModelPath        string
	ExternalURL      string
	ProviderModel    string
	Profile          string
	APIKeyEnv        string
	APIKey           string
	Force            bool
	SkipRuntimeCheck bool
	JSON             bool
}

// Choice is the complete, reviewable result of the setup conversation. It is
// also the input to Apply, so a non-interactive invocation and a person moving
// through the TUI take exactly the same state-changing path.
type Choice struct {
	DataDir           string
	Profile           string
	Mode              config.InferenceMode
	RuntimeBinary     string
	ModelPath         string
	ModelName         string
	BaseURL           string
	ProviderModel     string
	JudgmentEnabled   bool
	JudgmentAPIKeyEnv string
	Force             bool
}

// Report describes what Apply changed and what remains true after it returns.
type Report struct {
	DataDir       string   `json:"data_dir"`
	ConfigPath    string   `json:"config_path"`
	ProvidersPath string   `json:"providers_path"`
	JudgmentPath  string   `json:"judgment_path,omitempty"`
	MarkerPath    string   `json:"marker_path"`
	Profile       string   `json:"profile,omitempty"`
	Mode          string   `json:"mode,omitempty"`
	Runtime       string   `json:"runtime,omitempty"`
	Model         string   `json:"model,omitempty"`
	ProviderModel string   `json:"provider_model,omitempty"`
	Judgment      string   `json:"judgment,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
	Checks        []Check  `json:"checks,omitempty"`
}

// Marker is the small durable record written after successful setup.
type Marker struct {
	Version     int       `json:"version"`
	CompletedAt time.Time `json:"completed_at"`
	Profile     string    `json:"profile,omitempty"`
	Mode        string    `json:"mode,omitempty"`
	Model       string    `json:"model,omitempty"`
}

// DefaultDataDir returns the same location used by the host CLI and avoids
// writing an installation into the checkout. A container already sets BC_DATA,
// so the normal container path remains /data.
func DefaultDataDir() string {
	if value := strings.TrimSpace(os.Getenv(store.EnvDataDir)); value != "" {
		return value
	}
	return store.DefaultDataDirPath()
}

// DataDir resolves the effective installation root for a command invocation.
func DataDir(explicit string) string {
	candidate := strings.TrimSpace(explicit)
	if candidate == "" {
		candidate = DefaultDataDir()
	}
	if absolute, err := filepath.Abs(candidate); err == nil {
		return absolute
	}
	return candidate
}

// MarkerPath is the completed-setup record under a data directory.
func MarkerPath(dataDir string) string {
	return filepath.Join(dataDir, "config", MarkerFile)
}

// Completed reports whether the guided setup has completed successfully.
func Completed(dataDir string) bool {
	body, err := os.ReadFile(MarkerPath(DataDir(dataDir)))
	if err != nil {
		return false
	}
	var marker Marker
	return yaml.Unmarshal(body, &marker) == nil && marker.Version > 0
}

// Detect checks the tools that affect setup and session startup. It never
// mutates the host. Missing optional tools are warnings; missing required
// tools are failures the TUI can explain and offer to fix where safe.
func Detect(ctx context.Context) []Check {
	checks := make([]Check, 0, 8)
	addPath := func(name, command, fix string, required bool) {
		path, err := exec.LookPath(command)
		if err != nil {
			checks = append(checks, Check{Name: name, Status: "missing", Detail: fix, Required: required})
			return
		}
		checks = append(checks, Check{Name: name, Status: "ok", Detail: path})
	}

	addPath("Git", "git", "Install Git before setting up a project workspace.", true)
	addPath("ripgrep", "rg", "Install ripgrep for fast repository search; setup can continue without it.", false)
	addPath("C compiler", "cc", "A C compiler is needed only when building BoundedCode from source.", false)
	addPath("CMake", "cmake", "Required only when the TUI builds the pinned Prism runtime.", false)
	addPath("Ninja", "ninja", "Required only when the TUI builds the pinned Prism runtime.", false)
	addPath("CUDA toolkit", "nvcc", "Required only when the TUI builds the pinned Prism runtime.", false)
	addPath("bubblewrap", "bwrap", "Optional: install bubblewrap to enable the strongest process sandbox.", false)
	addPath("NVIDIA driver", "nvidia-smi", "Required for the local CUDA model and automatic Prism build; use an external endpoint on a CPU-only host.", false)
	addPath("OpenCode", "opencode", "Install OpenCode 2 before starting a coding session.", true)

	checks = append(checks, Check{
		Name: "platform", Status: "ok",
		Detail: runtime.GOOS + "/" + runtime.GOARCH,
	})
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		checks[len(checks)-1].Status = "warning"
		checks[len(checks)-1].Fix = "The reference CUDA profile is tested on Linux x86-64; other platforms need a measured profile."
	}
	if runtime.GOOS == "linux" {
		if body, err := os.ReadFile("/proc/meminfo"); err == nil {
			for _, line := range strings.Split(string(body), "\n") {
				if strings.HasPrefix(line, "MemTotal:") {
					fields := strings.Fields(line)
					if len(fields) >= 2 {
						checks[len(checks)-1].Detail += fmt.Sprintf(", RAM %s", fields[1]+" kB")
					}
					break
				}
			}
		}
	}
	// A cancelled context is still a cancelled dependency check. Do not turn
	// Ctrl-C into a misleading list of missing tools.
	select {
	case <-ctx.Done():
		return append(checks, Check{Name: "dependency scan", Status: "cancelled", Detail: ctx.Err().Error()})
	default:
	}
	return checks
}

// DiscoverRuntime returns the first usable llama-server executable, or "".
func DiscoverRuntime(dataDir string) string {
	candidates := []string{
		strings.TrimSpace(os.Getenv("BC_LLAMA_SERVER")),
		strings.TrimSpace(os.Getenv("BC_PRISM_DIR")),
		filepath.Join(DataDir(dataDir), "runtime", "bin", "llama-server"),
		filepath.Join(DataDir(dataDir), "runtime", "build", "bin", "llama-server"),
		filepath.Join(DataDir(dataDir), "runtime", "build", "llama-server"),
		filepath.Join(DataDir(dataDir), "runtime", "llama-server"),
		"/opt/bcode/llama/llama-server",
		"/usr/local/bin/llama-server",
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return absoluteRuntimePath(candidate)
		}
	}
	if prism := strings.TrimSpace(os.Getenv("BC_PRISM_DIR")); prism != "" {
		for _, candidate := range []string{
			filepath.Join(prism, "build", "bin", "llama-server"),
			filepath.Join(prism, "build", "llama-server"),
			filepath.Join(prism, "llama-server"),
		} {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
				return absoluteRuntimePath(candidate)
			}
		}
	}
	if path, err := exec.LookPath("llama-server"); err == nil {
		return absoluteRuntimePath(path)
	}
	return ""
}

func absoluteRuntimePath(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		return absolute
	}
	return path
}

// DiscoverModels lists GGUF files in the installation's model directory. It
// returns paths sorted for a stable TUI and a stable non-interactive report.
func DiscoverModels(dataDir string) []string {
	dir := filepath.Join(DataDir(dataDir), "models")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".gguf") {
			continue
		}
		out = append(out, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(out)
	return out
}

// DefaultChoice discovers what is already present and proposes a complete
// local setup. It never downloads anything and never changes configuration.
func DefaultChoice(dataDir string) Choice {
	dataDir = DataDir(dataDir)
	choice := Choice{
		DataDir:           dataDir,
		Profile:           "bonsai-2-27b-8gb-cuda",
		Mode:              config.ModeEmbedded,
		RuntimeBinary:     DiscoverRuntime(dataDir),
		ModelName:         DefaultModelName,
		BaseURL:           fmt.Sprintf("http://127.0.0.1:%d", DefaultInferencePort),
		ProviderModel:     strings.TrimSuffix(DefaultModelName, ".gguf"),
		JudgmentAPIKeyEnv: "TYPESAFE_API_KEY",
	}
	models := DiscoverModels(dataDir)
	if len(models) > 0 {
		choice.ModelPath = models[0]
		choice.ModelName = filepath.Base(models[0])
		choice.ProviderModel = strings.TrimSuffix(choice.ModelName, ".gguf")
	}
	if choice.RuntimeBinary == "" {
		choice.Mode = config.ModeNone
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		choice.Profile = "external-inference"
	}
	return choice
}

// Apply writes the selected setup atomically enough for an interrupted run to
// leave either the old complete files or the new complete files. Individual
// files are still separate operations, so a failure returns the path and the
// caller can re-run setup; no partial file is presented as valid.
func Apply(ctx context.Context, dataDir string, choice Choice) (Report, error) {
	dataDir = DataDir(dataDir)
	layout, err := store.NewLayout(dataDir)
	if err != nil {
		return Report{}, err
	}
	configDir := layout.ConfigDir()
	if err := os.MkdirAll(configDir, 0o750); err != nil {
		return Report{}, err
	}
	release, err := acquireLock(configDir)
	if err != nil {
		return Report{}, err
	}
	defer release()

	cfg, err := config.Load(configDir)
	if err != nil {
		return Report{}, err
	}
	if choice.Profile != "" {
		cfg.Profile = choice.Profile
	}
	if choice.Mode != "" {
		cfg.Inference.Mode = choice.Mode
	}
	if choice.RuntimeBinary != "" {
		cfg.Inference.Binary = choice.RuntimeBinary
	}
	if choice.ModelPath != "" {
		cfg.Inference.Model = choice.ModelPath
	}
	if choice.BaseURL != "" {
		cfg.Inference.BaseURL = choice.BaseURL
	}
	if cfg.Inference.Mode == config.ModeEmbedded {
		if cfg.Inference.Port == 0 {
			cfg.Inference.Port = DefaultInferencePort
		}
		// BaseURL belongs to external mode. A prior external setup must not
		// leave the generated local provider pointing at the old endpoint when
		// the operator switches back to the embedded runtime.
		cfg.Inference.BaseURL = ""
		if cfg.Inference.Binary == "" {
			return Report{}, fmt.Errorf("setup: embedded inference needs a llama-server executable; rerun setup and choose a runtime")
		}
		if cfg.Inference.Model == "" {
			return Report{}, fmt.Errorf("setup: embedded inference needs a GGUF model; rerun setup and choose or download a model")
		}
		if err := validateEmbeddedFiles(cfg, dataDir); err != nil {
			return Report{}, err
		}
		// The alias is the stable name OpenCode and providers.yaml use. Do
		// not append it repeatedly when setup is re-run.
		alias := providerAlias(cfg.Inference.Model)
		cfg.Inference.Args = withArg(cfg.Inference.Args, "--alias", alias)
	} else if cfg.Inference.Mode == config.ModeExternal {
		if strings.TrimSpace(cfg.Inference.BaseURL) == "" {
			return Report{}, fmt.Errorf("setup: external inference needs a loopback base URL")
		}
		if err := validateLoopbackURL(cfg.Inference.BaseURL); err != nil {
			return Report{}, err
		}
	}

	if err := cfg.Validate(); err != nil {
		return Report{}, err
	}
	if err := config.Save(configDir, cfg); err != nil {
		return Report{}, err
	}

	providers, err := llm.LoadProvidersFile(configDir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) && !choice.Force {
			return Report{}, fmt.Errorf("setup: existing providers.yaml is invalid; fix it or rerun with --force: %w", err)
		}
		providers = llm.ProvidersFile{}
	}
	if len(providers.Providers) == 0 {
		base := providerBaseURL(cfg, choice)
		model := choice.ProviderModel
		if model == "" {
			model = providerAlias(cfg.Inference.Model)
		}
		providers = llm.DefaultProvidersFile(base, model)
		providers.Providers[0].TimeoutSeconds = 900
		if err := llm.SaveProvidersFile(configDir, providers); err != nil {
			return Report{}, err
		}
	}
	// Re-running setup after changing a model must keep the provider and the
	// supervisor on the same route. Only the generated/local provider is
	// rewritten; a named custom provider remains the operator's choice.
	if choice.ProviderModel != "" && (cfg.Inference.Mode == config.ModeEmbedded || cfg.Inference.Mode == config.ModeExternal) {
		base := providerBaseURL(cfg, choice)
		changed := false
		for i := range providers.Providers {
			if providers.Providers[i].Name != "local" {
				continue
			}
			providers.Providers[i].BaseURL = base
			providers.Providers[i].Model = choice.ProviderModel
			providers.Providers[i].TimeoutSeconds = 900
			changed = true
			break
		}
		if changed {
			if err := llm.SaveProvidersFile(configDir, providers); err != nil {
				return Report{}, err
			}
		}
	}

	jPath := filepath.Join(configDir, judgment.ConfigFile)
	if choice.JudgmentEnabled {
		jc := judgment.DefaultConfig()
		jc.Enabled = true
		jc.Model = judgment.RecommendedModel
		jc.APIKeyEnv = firstNonEmpty(choice.JudgmentAPIKeyEnv, "TYPESAFE_API_KEY")
		if err := writeYAMLAtomic(jPath, jc, 0o600); err != nil {
			return Report{}, err
		}
	}

	report := Report{
		DataDir: dataDir, ConfigPath: config.Path(configDir),
		ProvidersPath: filepath.Join(configDir, "providers.yaml"),
		Profile:       cfg.Profile, Mode: string(cfg.Inference.Mode),
		Model: cfg.Inference.Model, ProviderModel: choice.ProviderModel,
	}
	if cfg.Inference.Mode == config.ModeEmbedded {
		report.Runtime = cfg.Inference.Binary
	}
	if report.ProviderModel == "" && len(providers.Providers) > 0 {
		report.ProviderModel = providers.Providers[0].Model
	}
	if choice.JudgmentEnabled {
		report.JudgmentPath = jPath
		report.Judgment = "configured (credential is stored outside YAML)"
	} else if existing, _ := judgment.Load(configDir); existing.Enabled {
		report.Judgment = "configured (credential is stored outside YAML)"
	}
	report.MarkerPath = MarkerPath(dataDir)
	checks, err := Validate(ctx, dataDir)
	report.Checks = checks
	if err != nil {
		return report, err
	}
	marker := Marker{
		Version: 1, CompletedAt: time.Now().UTC(), Profile: cfg.Profile,
		Mode: string(cfg.Inference.Mode), Model: cfg.Inference.Model,
	}
	if err := writeYAMLAtomic(MarkerPath(dataDir), marker, 0o640); err != nil {
		return report, err
	}
	return report, nil
}
func validateEmbeddedFiles(cfg config.Config, dataDir string) error {
	if info, err := os.Stat(cfg.Inference.Binary); err != nil {
		return fmt.Errorf("setup: runtime %s is not available: %w", cfg.Inference.Binary, err)
	} else if info.IsDir() || info.Mode()&0o111 == 0 {
		return fmt.Errorf("setup: runtime %s is not executable", cfg.Inference.Binary)
	}
	model := cfg.Inference.Model
	if !filepath.IsAbs(model) {
		// A relative model is resolved by the data directory's models folder
		// by config.LlamaArgs. Validate that exact path here.
		dataDir = DataDir(dataDir)
		model = filepath.Join(dataDir, "models", model)
	}
	info, err := os.Stat(model)
	if err != nil {
		return fmt.Errorf("setup: model %s is not available: %w", model, err)
	}
	if info.IsDir() || info.Size() == 0 {
		return fmt.Errorf("setup: model %s is empty", model)
	}
	return nil
}

func validateLoopbackURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Hostname() == "" {
		return fmt.Errorf("setup: external inference URL %q is not an http URL", raw)
	}
	ip := net.ParseIP(u.Hostname())
	if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("setup: guided OpenCode sessions require a loopback inference endpoint, got %q", u.Hostname())
	}
	return nil
}

func providerBaseURL(cfg config.Config, choice Choice) string {
	if cfg.Inference.Mode == config.ModeEmbedded {
		return fmt.Sprintf("http://127.0.0.1:%d", cfg.Inference.Port)
	}
	if strings.TrimSpace(choice.BaseURL) != "" {
		return choice.BaseURL
	}
	return cfg.Inference.BaseURL
}

func providerAlias(model string) string {
	base := filepath.Base(strings.TrimSpace(model))
	return strings.TrimSuffix(base, ".gguf")
}

func withArg(args []string, flag, value string) []string {
	out := make([]string, 0, len(args)+2)
	for i := 0; i < len(args); i++ {
		if args[i] == flag {
			i++ // replace the old value
			continue
		}
		out = append(out, args[i])
	}
	return append(out, flag, value)
}

func writeYAMLAtomic(path string, value any, mode os.FileMode) error {
	body, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func acquireLock(configDir string) (func(), error) {
	path := filepath.Join(configDir, ".setup.lock")
	for attempt := 0; attempt < 2; attempt++ {
		err := os.Mkdir(path, 0o700)
		if err == nil {
			_ = os.WriteFile(filepath.Join(path, "owner"), []byte(fmt.Sprintf("%d\\n", os.Getpid())), 0o600)
			return func() { _ = os.RemoveAll(path) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		// A setup process killed during a write must not make setup
		// permanently impossible. A lock older than an hour is treated as
		// abandoned; the owner file is informational, not an authority.
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > time.Hour {
			_ = os.RemoveAll(path)
			continue
		}
		return nil, fmt.Errorf("another BoundedCode setup is already running (lock %s)", path)
	}
	return nil, fmt.Errorf("could not acquire setup lock %s", path)
}

// StoreCredential writes an environment fragment without printing or putting
// the secret in a configuration file that users routinely share.
func StoreCredential(dataDir, envName, value string) error {
	if strings.TrimSpace(envName) == "" || strings.TrimSpace(value) == "" {
		return errors.New("setup: a credential needs an environment name and value")
	}
	path := filepath.Join(DataDir(dataDir), "config", CredentialFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	body := envName + "=" + value + "\n"
	tmp, err := os.CreateTemp(filepath.Dir(path), CredentialFile+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := io.WriteString(tmp, body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// LoadCredential reads the owner-only credential fragment. An absent file is
// not an error; the caller decides whether the feature requires one.
func LoadCredential(dataDir string) (string, error) {
	path := filepath.Join(DataDir(dataDir), "config", CredentialFile)
	f, err := os.Open(path) //nolint:gosec // path is derived from the data root
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(key) == "TYPESAFE_API_KEY" {
			return strings.Trim(strings.TrimSpace(value), "\""), nil
		}
	}
	return "", scanner.Err()
}

// DownloadModel streams a model into the installation atomically. The
// progress callback is suitable for a TUI and is deliberately not used for
// logging in non-interactive callers.
func DownloadModel(ctx context.Context, dataDir, url, name, expectedSHA string, progress func(downloaded, total int64)) (string, error) {
	if strings.TrimSpace(url) == "" {
		return "", errors.New("setup: model URL is empty")
	}
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(url)
	}
	if name != filepath.Base(name) || name == "." || name == ".." || strings.ContainsAny(name, `/\\`) {
		return "", fmt.Errorf("setup: invalid model file name %q", name)
	}
	dir := filepath.Join(DataDir(dataDir), "models")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, name)
	if info, err := os.Stat(dst); err == nil && info.Size() > 0 {
		if expectedSHA == "" {
			return dst, nil
		}
		file, openErr := os.Open(dst) //nolint:gosec // dst is derived from the data root
		if openErr != nil {
			return "", openErr
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		_ = file.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if got := hex.EncodeToString(hash.Sum(nil)); !strings.EqualFold(got, expectedSHA) {
			return "", fmt.Errorf("setup: existing model %s has SHA-256 %s, expected %s; remove it or pass the matching checksum", dst, got, expectedSHA)
		}
		return dst, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("setup: download model: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("setup: download model: %s returned %s", url, resp.Status)
	}
	tmp, err := os.CreateTemp(dir, name+".*.part")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	hash := sha256.New()
	var downloaded int64
	buffer := make([]byte, 128*1024)
	for {
		n, readErr := resp.Body.Read(buffer)
		if n > 0 {
			if _, err := tmp.Write(buffer[:n]); err != nil {
				_ = tmp.Close()
				return "", err
			}
			_, _ = hash.Write(buffer[:n])
			downloaded += int64(n)
			if progress != nil {
				progress(downloaded, resp.ContentLength)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = tmp.Close()
			return "", readErr
		}
	}
	if expectedSHA != "" && !strings.EqualFold(expectedSHA, hex.EncodeToString(hash.Sum(nil))) {
		_ = tmp.Close()
		return "", fmt.Errorf("setup: model checksum mismatch: got %s, want %s", hex.EncodeToString(hash.Sum(nil)), expectedSHA)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Chmod(0o640); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// Validate checks the files and configuration a completed setup needs. It is
// intentionally offline: a live model or judgment request belongs to session
// startup or an explicitly requested smoke test, not to a setup that may be
// running on a machine with no network.
func Validate(ctx context.Context, dataDir string) ([]Check, error) {
	dataDir = DataDir(dataDir)
	var checks []Check
	fail := func(name, detail string) {
		checks = append(checks, Check{Name: name, Status: "fail", Detail: detail, Required: true})
	}
	ok := func(name, detail string) {
		checks = append(checks, Check{Name: name, Status: "ok", Detail: detail})
	}

	layout, err := store.NewLayout(dataDir)
	if err != nil {
		fail("data directory", err.Error())
		return checks, err
	}
	cfg, err := config.Load(layout.ConfigDir())
	if err != nil {
		fail("bcode.yaml", err.Error())
		return checks, err
	}
	ok("bcode.yaml", layout.ConfigDir())
	if free, diskErr := availableBytes(dataDir); diskErr != nil {
		checks = append(checks, Check{Name: "free disk", Status: "warning", Detail: "could not determine free space: " + diskErr.Error()})
	} else if free > 0 {
		need := uint64(8) << 30
		if cfg.Inference.Mode != config.ModeEmbedded {
			need = 2 << 30
		}
		if free < need {
			fail("free disk", fmt.Sprintf("%d GB available; setup needs at least %d GB for the runtime and model", free>>30, need>>30))
		} else {
			ok("free disk", fmt.Sprintf("%d GB available", free>>30))
		}
	}
	switch cfg.Inference.Mode {
	case config.ModeEmbedded:
		if err := validateEmbeddedFiles(cfg, dataDir); err != nil {
			fail("local runtime and model", err.Error())
		} else {
			ok("local runtime and model", cfg.Inference.Binary+" + "+cfg.Inference.Model)
		}
	case config.ModeExternal:
		if cfg.Inference.BaseURL == "" {
			fail("external inference", "base_url is empty")
		} else if err := validateLoopbackURL(cfg.Inference.BaseURL); err != nil {
			fail("external inference", err.Error())
		} else {
			ok("external inference", cfg.Inference.BaseURL)
		}
	case config.ModeNone, "":
		fail("inference", "mode is none; setup did not select a model")
	default:
		fail("inference", "unknown mode "+string(cfg.Inference.Mode))
	}
	if _, err := llm.LoadProvidersFile(layout.ConfigDir()); err != nil {
		fail("providers.yaml", err.Error())
	} else {
		ok("providers.yaml", "model routing is configured")
	}
	if _, err := config.LoadProfile(filepath.Join(layout.ConfigDir(), "profiles"), cfg.Profile); err != nil {
		// An absent profile is allowed; an invalid one is not.
		if cfg.Profile != "" {
			fail("hardware profile", err.Error())
		}
	} else {
		ok("hardware profile", cfg.Profile)
	}
	if jc, err := judgment.Load(layout.ConfigDir()); err != nil {
		fail("judgment.yaml", err.Error())
	} else if jc.Enabled {
		ok("judgment decision plane", jc.Model+" (credential checked at first use)")
	} else {
		checks = append(checks, Check{Name: "judgment decision plane", Status: "warning", Detail: "not enabled; task execution will stop at the required decision-plane check", Fix: "Re-run `bcode setup` and provide TYPESAFE_API_KEY, or configure judgment.yaml explicitly."})
	}
	for _, c := range checks {
		if c.Status == "fail" {
			return checks, fmt.Errorf("setup validation failed: %s: %s", c.Name, c.Detail)
		}
	}
	select {
	case <-ctx.Done():
		return checks, ctx.Err()
	default:
	}
	return checks, nil
}

// CredentialEnvironment adds the owner-only credential fragment to an
// environment slice without replacing an explicitly exported value.
func CredentialEnvironment(env []string, dataDir string) []string {
	if value := os.Getenv("TYPESAFE_API_KEY"); value != "" {
		return env
	}
	value, err := LoadCredential(dataDir)
	if err != nil || value == "" {
		return env
	}
	return append(env, "TYPESAFE_API_KEY="+value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// ParsePositiveInt is a small exported helper for command flags and tests.
func ParsePositiveInt(value string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("setup: %q is not a positive integer", value)
	}
	return n, nil
}
