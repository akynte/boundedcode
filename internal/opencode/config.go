package opencode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/akynte/boundedcode/internal/opencode/plugin"
)

// ServerName is the key BoundedCode registers itself under.
const ServerName = "boundedcode"

// legacyPluginPath is what every setup before this one wrote: a path relative
// to the repository OpenCode opened. It happened to resolve only when that
// repository was a checkout of BoundedCode's own source — the one case this
// project dogfoods itself in — and silently failed to load (OpenCode logs a
// WARN, not an error a developer would see) in every other project, which is
// the only place `bcode opencode` is actually meant to run. RegisterContextPolicy
// now replaces it with InstallPlugin's absolute, portable path.
const legacyPluginPath = "./internal/opencode/plugin"

// InstallPlugin unpacks the embedded OpenCode context-hook adapter into
// stateDir and returns its directory. A source-relative path only resolves
// when OpenCode happens to be opened at this repository's own root; an
// absolute path resolves from any project, which is the whole point of an
// installed tool. Re-extracting is cheap and idempotent: content is compared
// before writing, so `bcode opencode setup` can call this on every run without
// dirtying the workspace state directory's mtimes for no reason.
func InstallPlugin(stateDir string) (dir string, changed bool, err error) {
	dir = filepath.Join(stateDir, "opencode-plugin")
	entries, err := plugin.FS.ReadDir(".")
	if err != nil {
		return "", false, fmt.Errorf("reading embedded OpenCode plugin: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return dir, false, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		body, err := plugin.FS.ReadFile(e.Name())
		if err != nil {
			return dir, false, fmt.Errorf("reading embedded %s: %w", e.Name(), err)
		}
		target := filepath.Join(dir, e.Name())
		if existing, err := os.ReadFile(target); err == nil && bytes.Equal(existing, body) {
			continue
		}
		if err := os.WriteFile(target, body, 0o644); err != nil { //nolint:gosec // the plugin's own source, not a secret
			return dir, false, fmt.Errorf("writing %s: %w", target, err)
		}
		changed = true
	}
	return dir, changed, nil
}

// RegisterContextPolicy installs the OpenCode 2 adapter and a compaction
// policy that fits the reference 32K Bonsai slot. OpenCode 2.0.15 otherwise
// retains 15K tokens after compacting while its 20K buffer triggers at 12.8K.
//
// pluginDir is the absolute path InstallPlugin returned; the caller extracts
// once and passes it in, rather than this function reaching into the
// filesystem on its own, so a test can register a policy against a plugin
// path it fully controls.
func RegisterContextPolicy(repoRoot, pluginDir string) (string, bool, error) {
	return mergeConfig(repoRoot, func(doc map[string]any) bool {
		changed := false
		existing, _ := doc["plugins"].([]any)
		var plugins []any
		found := false
		for _, item := range existing {
			if item == legacyPluginPath {
				// Drop the broken path outright rather than keeping both: a
				// project that somehow has a real file at that relative
				// location is not a case this adapter needs to support, and
				// keeping it would register the adapter twice.
				changed = true
				continue
			}
			if item == pluginDir {
				found = true
			}
			plugins = append(plugins, item)
		}
		if !found {
			plugins = append(plugins, pluginDir)
			changed = true
		}
		if changed {
			doc["plugins"] = plugins
		}
		model, _ := doc["model"].(string)
		if strings.HasPrefix(model, ProviderName+"/") && strings.Contains(strings.ToLower(model), "bonsai") {
			want := map[string]any{
				"auto": true, "buffer": 12000,
				"keep": map[string]any{"tokens": 4000},
			}
			if !equalJSON(doc["compaction"], want) {
				doc["compaction"] = want
				changed = true
			}
		}
		return changed
	})
}

// verifyTimeoutMillis bounds one MCP request. Twenty minutes is the task budget
// a verification runs under, so a client that gives up earlier would abandon a
// call the supervisor is still honouring.
const verifyTimeoutMillis = 20 * 60 * 1000

// RegisterMCP adds BoundedCode to the repository's opencode.json, leaving
// every other setting alone.
//
// Merging rather than writing: opencode.json is the developer's file and may
// already carry a model choice, other MCP servers, or permissions. Replacing it
// to add one key would be the kind of helpfulness that loses someone's
// configuration.
func RegisterMCP(repoRoot string, command []string) (path string, changed bool, err error) {
	// A .jsonc is refused by mergeConfig, but this one route can say what to
	// paste instead of only what went wrong.
	if jsonc := filepath.Join(repoRoot, "opencode.jsonc"); exists(jsonc) {
		return jsonc, false, fmt.Errorf(
			"%s already exists and may contain comments this cannot preserve. Add by hand:\n"+
				"  \"mcp\": { \"servers\": { %q: { \"type\": \"local\", \"command\": %s, \"codemode\": false } } }",
			jsonc, ServerName, mustJSON(command))
	}
	return mergeConfig(repoRoot, func(doc map[string]any) bool {
		mcpConfig, _ := doc["mcp"].(map[string]any)
		if mcpConfig == nil {
			mcpConfig = map[string]any{}
		}
		servers, _ := mcpConfig["servers"].(map[string]any)
		if servers == nil {
			servers = map[string]any{}
		}
		// OpenCode 1.x stored server names directly under "mcp". Move those
		// entries into the 2.x "mcp.servers" map while preserving MCP-wide
		// settings such as its default timeout.
		for name, value := range mcpConfig {
			server, ok := value.(map[string]any)
			if name == "servers" || !ok {
				continue
			}
			if serverType, _ := server["type"].(string); serverType == "local" || serverType == "remote" {
				if _, alreadyMoved := servers[name]; !alreadyMoved {
					servers[name] = value
				}
				delete(mcpConfig, name)
			}
		}
		want := map[string]any{
			"type":     "local",
			"command":  toAny(command),
			"disabled": false,
			// These tools are clearer and more reliable when called directly.
			// Code Mode's execute tool is JavaScript for orchestrating other
			// tools, not a general-purpose interpreter for their inputs.
			"codemode": false,
			// OpenCode's default MCP request timeout is short. bc_verify runs
			// this repository's build, vet, test and format checks in a
			// sandbox, which is minutes on anything real.
			"timeout": map[string]any{"request": verifyTimeoutMillis},
		}
		if equalJSON(servers[ServerName], want) {
			return false
		}
		servers[ServerName] = want
		mcpConfig["servers"] = servers
		doc["mcp"] = mcpConfig
		return true
	})
}

// mergeConfig applies one edit to the repository's opencode.json, writing only
// when the edit changed something. apply reports whether it did.
//
// The file is written as .json rather than .jsonc because this marshals it, and
// marshalling a document that permitted comments would silently delete them.
// OpenCode reads both; if a .jsonc already exists this reports that rather than
// creating a second file that shadows it.
func mergeConfig(repoRoot string, apply func(doc map[string]any) bool) (path string, changed bool, err error) {
	if jsonc := filepath.Join(repoRoot, "opencode.jsonc"); exists(jsonc) {
		return jsonc, false, fmt.Errorf(
			"%s already exists and may contain comments this cannot preserve. "+
				"Edit it by hand, or rename it to opencode.json", jsonc)
	}
	path = filepath.Join(repoRoot, "opencode.json")

	doc := map[string]any{}
	body, err := os.ReadFile(path) //nolint:gosec // a path derived from the workspace root
	switch {
	case err == nil:
		if err := json.Unmarshal(body, &doc); err != nil {
			return path, false, fmt.Errorf("%s is not valid JSON: %w", path, err)
		}
	case !os.IsNotExist(err):
		return path, false, err
	default:
		doc["$schema"] = "https://opencode.ai/config.json"
	}

	if !apply(doc) {
		return path, false, nil
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return path, false, err
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil { //nolint:gosec // a committed editor config
		return path, false, err
	}
	return path, true, nil
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func equalJSON(a, b any) bool {
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// ProviderName is the key the local model is registered under.
const ProviderName = "boundedcode-local"

// RegisterModel points the editor at the same local endpoint the supervisor
// uses, and selects it.
//
// Without this a developer who has followed every instruction lands in an
// editor with nothing to talk to: the tools are registered, the agent is
// defined, and the first message asks them to sign in to a cloud provider. The
// endpoint is already known — it is the one the supervisor was configured with
// — so asking the developer to transcribe it into a second file is asking them
// to get it wrong.
//
// It is skipped when a model is already chosen. A developer who has set one has
// made a decision, and quietly replacing it with a local endpoint would be the
// kind of helpfulness that loses someone's configuration.
func RegisterModel(repoRoot, baseURL, model string) (path string, changed bool, err error) {
	if baseURL == "" || model == "" {
		return filepath.Join(repoRoot, "opencode.json"), false, nil
	}
	return mergeConfig(repoRoot, func(doc map[string]any) bool {
		// A model this function set before is ours to keep current: an operator
		// who points the supervisor at a different endpoint and re-runs setup
		// means the editor to follow. A model chosen any other way is a
		// decision, and replacing it would be the kind of helpfulness that
		// loses somebody's configuration.
		if existing, ok := doc["model"].(string); ok {
			existing = strings.TrimSpace(existing)
			if existing != "" && !strings.HasPrefix(existing, ProviderName+"/") {
				return false
			}
		}
		providers, _ := doc["providers"].(map[string]any)
		if providers == nil {
			providers = map[string]any{}
		}
		// The OpenAI-compatible adapter, because that is the boundary the
		// supervisor already speaks: anything serving that API works here
		// without this file knowing which engine it is.
		// The id is a short alias, not the model string the supervisor sends.
		// That string is a filesystem path for a local GGUF, and opencode.json
		// is a committed file: a path belongs in the operator's own config, not
		// in a repository other people clone. Servers on this boundary select
		// by what they loaded rather than by this field — a single-model
		// llama-server answers to any name — so the alias costs nothing.
		id := shortModelName(model)
		modelConfig := map[string]any{"name": id}
		// The reference Bonsai setup serves a text-only model with a 32K
		// context. These are known facts about that artifact, not safe defaults
		// to impose on every OpenAI-compatible endpoint.
		if strings.Contains(strings.ToLower(id), "bonsai") {
			modelConfig["capabilities"] = map[string]any{
				"tools":  true,
				"input":  []any{"text"},
				"output": []any{"text"},
			}
			modelConfig["limit"] = map[string]any{"context": 32768, "output": 8192}
		}
		want := map[string]any{
			"name":     "Local (via boundedcode)",
			"package":  "@opencode/ai/providers/openai-compatible",
			"settings": map[string]any{"baseURL": strings.TrimSuffix(baseURL, "/") + "/v1"},
			"models":   map[string]any{id: modelConfig},
		}
		changed := false
		if !equalJSON(providers[ProviderName], want) || doc["model"] != ProviderName+"/"+id {
			providers[ProviderName] = want
			doc["providers"] = providers
			doc["model"] = ProviderName + "/" + id
			changed = true
		}
		// Remove only our generated V1 entry. Other V1 provider entries may
		// still be used by the developer and remain intact for OpenCode's
		// compatibility layer. This runs even when the V2 entry above was
		// already current, so a stale V1 duplicate left by an older setup
		// (or a hand-edited config) still gets cleaned up on the next run.
		if legacy, ok := doc["provider"].(map[string]any); ok {
			if _, has := legacy[ProviderName]; has {
				delete(legacy, ProviderName)
				if len(legacy) == 0 {
					delete(doc, "provider")
				} else {
					doc["provider"] = legacy
				}
				changed = true
			}
		}
		return changed
	})
}

// shortModelName renders a file path as something readable in a model picker.
func shortModelName(model string) string {
	base := filepath.Base(model)
	return strings.TrimSuffix(base, ".gguf")
}
