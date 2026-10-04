package serena

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/akynte/boundedcode/internal/config"
)

// ReadOnlyTools is the complete tool set an instance exposes (Stage 1 of the
// editing policy in ADR-0008). Shell, file, editing, memory, onboarding and
// project-switching tools are absent, so even a compromised client could not
// use Serena to write files or run commands.
var ReadOnlyTools = []string{
	"find_symbol", "find_referencing_symbols", "find_implementations",
	"get_symbols_overview", "get_current_config",
}

// editTools are Serena's symbol-level editing tools. They are not enabled in
// the product (Stage 1 is read-only); the Stage 2 experiment in
// stage2_test.go measures them on fixtures.
var editTools = []string{"replace_symbol_body", "insert_after_symbol", "insert_before_symbol", "rename_symbol"}

// serenaLanguages maps workspace.DetectLanguages names to Serena language
// server ids. JavaScript is served by Serena's TypeScript server.
var serenaLanguages = map[string]string{
	"go": "go", "typescript": "typescript", "javascript": "typescript",
	"python": "python", "rust": "rust",
}

// SupportedLanguages returns the Serena language ids for detected languages.
func SupportedLanguages(detected []string) []string {
	set := map[string]bool{}
	for _, l := range detected {
		if s, ok := serenaLanguages[l]; ok {
			set[s] = true
		}
	}
	out := make([]string, 0, len(set))
	for l := range set {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// Project is a checkout to serve.
type Project struct {
	Root      string   // absolute path of the checkout (task worktree)
	Languages []string // Serena language ids
	// Ignored are repo-relative paths Serena must not read (the sandbox's
	// secret masks), in gitignore syntax.
	Ignored []string
}

// instanceKey is stable for a root, so a restarted control plane reuses the
// same instance home (and Serena's symbol cache) for the same worktree.
func instanceKey(root string) string {
	s := sha256.Sum256([]byte(root))
	return hex.EncodeToString(s[:8]) + "-" + sanitize(filepath.Base(root))
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// layout is an instance's private SERENA_HOME.
type layout struct {
	Home        string // SERENA_HOME
	ProjectDir  string // Serena's per-project data folder (project.yml, caches)
	ContextFile string
	ProjectName string
}

// prepareHome writes an instance's configuration. Everything Serena would
// otherwise write into the repository (.serena/ with project.yml, pickled
// symbol caches, memories) lives here instead, outside the worktree, because
// the agent can write to the worktree and Serena trusts these files:
//   - project.yml can name an activation_command that Serena runs;
//   - the caches are Python pickles.
//
// A repository-supplied .serena/ is never read: Serena prefers the configured
// project folder when it exists, and no project path is trusted.
func prepareHome(root, languageServers string, p Project, editing bool) (layout, error) {
	key := instanceKey(p.Root)
	l := layout{Home: filepath.Join(root, "instances", key), ProjectName: "bc-" + key}
	l.ProjectDir = filepath.Join(l.Home, "project")
	l.ContextFile = filepath.Join(l.Home, "boundedcode-context.yml")
	if err := os.MkdirAll(l.ProjectDir, 0o700); err != nil {
		return l, err
	}
	// Language servers that Serena downloads (TypeScript) are shared across
	// instances and installed ahead of time by `serena setup`.
	shared := languageServers
	if shared == "" {
		shared = filepath.Join(root, "language_servers")
	}
	if err := os.MkdirAll(shared, 0o700); err != nil {
		return l, err
	}
	link := filepath.Join(l.Home, "language_servers")
	if target, err := os.Readlink(link); err != nil || target != shared {
		_ = os.RemoveAll(link)
		if err := os.Symlink(shared, link); err != nil {
			return l, err
		}
	}
	global := map[string]any{
		"language_backend":               "LSP",
		"gui_log_window":                 false,
		"web_dashboard":                  false, // the dashboard fetches news from the internet and listens on a port
		"web_dashboard_open_on_launch":   false,
		"log_level":                      20,
		"trace_lsp_communication":        false,
		"tool_timeout":                   240,
		"excluded_tools":                 []string{},
		"included_optional_tools":        []string{},
		"fixed_tools":                    []string{},
		"base_modes":                     []string{},
		"default_modes":                  []string{},
		"default_max_tool_answer_chars":  150000,
		"token_count_estimator":          "CHAR_COUNT",
		"symbol_info_budget":             10,
		"project_serena_folder_location": l.ProjectDir,
		"trusted_project_path_patterns":  []string{},
		"ignored_paths":                  []string{},
		"projects":                       []string{},
	}
	ctxDef := map[string]any{
		"description":                "boundedcode: read-only symbol navigation driven by the control plane",
		"prompt":                     nil,
		"excluded_tools":             []string{},
		"included_optional_tools":    []string{},
		"fixed_tools":                toolSet(editing),
		"tool_description_overrides": map[string]string{},
		// single_project would hide get_current_config, which we need to
		// verify the active project. activate_project is excluded anyway
		// because it is not in fixed_tools.
		"single_project": false,
	}
	ignored := append([]string{".git/", "node_modules/", "vendor/"}, p.Ignored...)
	proj := map[string]any{
		"project_name":                    l.ProjectName,
		"language_servers":                p.Languages,
		"encoding":                        "utf-8",
		"activation_command":              nil,
		"ignore_all_files_in_gitignore":   true,
		"ls_specific_settings":            map[string]any{},
		"ls_workspace_folders":            []string{"."},
		"ls_additional_workspace_folders": []string{},
		"ignored_paths":                   ignored,
		"read_only":                       !editing,
		"excluded_tools":                  []string{},
		"included_optional_tools":         []string{},
		"fixed_tools":                     []string{},
		"initial_prompt":                  "",
		"read_only_memory_patterns":       []string{},
		"ignored_memory_patterns":         []string{},
	}
	for path, v := range map[string]any{
		filepath.Join(l.Home, "serena_config.yml"): global,
		l.ContextFile: ctxDef,
		filepath.Join(l.ProjectDir, "project.yml"):       proj,
		filepath.Join(l.ProjectDir, "project.local.yml"): map[string]any{},
	} {
		b, err := yaml.Marshal(v)
		if err != nil {
			return l, err
		}
		if err := config.WriteFileAtomic(path, b, 0o600); err != nil {
			return l, fmt.Errorf("write %s: %w", path, err)
		}
	}
	return l, nil
}

func toolSet(editing bool) []string {
	if !editing {
		return ReadOnlyTools
	}
	return append(append([]string(nil), ReadOnlyTools...), editTools...)
}

// removeHome deletes an instance home (symbol caches are rebuildable).
func removeHome(root, projectRoot string) error {
	dir := filepath.Join(root, "instances", instanceKey(projectRoot))
	if !strings.HasPrefix(dir, filepath.Join(root, "instances")+string(filepath.Separator)) {
		return errors.New("serena: refusing to remove a path outside the instances directory")
	}
	return os.RemoveAll(dir)
}
