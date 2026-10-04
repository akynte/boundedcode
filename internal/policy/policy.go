// Package policy holds deterministic safety rules. The LLM cannot override
// them: they are evaluated in Go, outside the agent, before actions run.
package policy

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// secretGlobs match workspace-relative paths the agent must not read.
// Matching is on the slash-separated path and on its base name.
var secretGlobs = []string{
	".env", ".env.*", "*.env", ".envrc",
	"secrets", "secrets/*", ".secrets", "secret.*", "secrets.*",
	"credentials", "credentials.*", "*credentials*.json",
	"*.pem", "*.key", "*.p12", "*.pfx", "*.jks", "*.keystore",
	"id_rsa", "id_rsa.*", "id_dsa", "id_ecdsa", "id_ed25519", "id_ed25519.*",
	".ssh", ".aws", ".azure", ".gcloud", ".kube", "kubeconfig", "kubeconfig.*", "*.kubeconfig",
	".netrc", ".npmrc", ".pypirc", ".docker", ".git-credentials",
	"terraform.tfstate", "terraform.tfstate.*", "*.tfvars", ".terraform",
	"service-account*.json", "*-sa.json",
}

// sourceExts are source-code files. They are code, not secret stores, so a
// secret-sounding name (credentials.go, kubeconfig.go, a "credentials"
// package) does not make them secret; literal secrets inside code are the
// secret scanner's job. Under a strongSecretDir (secrets/, .ssh/, .aws/, ...)
// they stay secret.
var sourceExts = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true,
	".py": true, ".rb": true, ".java": true, ".kt": true, ".scala": true, ".rs": true, ".c": true,
	".h": true, ".cc": true, ".cpp": true, ".hpp": true, ".cs": true, ".swift": true, ".php": true,
	".vue": true, ".svelte": true,
}

// strongSecretDir reports whether a directory name declares secret content
// outright: "secrets" and the secret dot directories (.ssh, .aws, ...). Their
// contents stay masked whole, source code included. Other secret-sounding
// directory names ("credentials") are also common package names, so source
// code in them stays visible.
func strongSecretDir(name string) bool {
	if name == "secrets" || name == ".secrets" {
		return true
	}
	if !strings.HasPrefix(name, ".") {
		return false
	}
	for _, g := range secretGlobs {
		if g == name {
			return true
		}
	}
	return false
}

// isSourceFile reports whether base names a source file (not a dotfile).
func isSourceFile(base string) bool {
	return !strings.HasPrefix(base, ".") && sourceExts[path.Ext(base)]
}

// allowedExceptions are conventional non-secret templates.
var allowedExceptions = []string{".env.example", ".env.sample", ".env.template", "*.example.tfvars"}

// IsSecretPath reports whether a workspace-relative path matches the secret
// denylist.
func IsSecretPath(rel string) bool {
	rel = filepath.ToSlash(filepath.Clean(rel))
	base := path.Base(rel)
	for _, a := range allowedExceptions {
		if ok, _ := path.Match(a, base); ok {
			return false
		}
	}
	parts := strings.Split(rel, "/")
	if isSourceFile(base) {
		for _, p := range parts[:len(parts)-1] {
			if strongSecretDir(p) {
				return true // e.g. .aws/helper.py, secrets/prod.go
			}
		}
		return false
	}
	for _, g := range secretGlobs {
		if ok, _ := path.Match(g, base); ok {
			return true
		}
		// A secret directory anywhere in the path.
		for _, p := range parts[:len(parts)-1] {
			if ok, _ := path.Match(g, p); ok && !strings.Contains(g, ".") || p == g {
				return true
			}
		}
	}
	return false
}

// ErrTooManySecrets is returned by FindSecretPaths when a tree has more
// secret paths than can be masked. Callers must refuse to run rather than
// expose the unmasked remainder.
var ErrTooManySecrets = errors.New("policy: too many secret paths to mask")

// MaxSecretMasks bounds FindSecretPaths for sandbox masking.
const MaxSecretMasks = 500

// FindSecretPaths walks root and returns workspace-relative secret paths
// (directories are reported once and not descended). Used to mask them in the
// sandbox. The walk skips .git and dependency directories (node_modules,
// vendor, .venv): they hold third-party code, and test fixtures there (keys,
// certificates) would otherwise exhaust the mask budget. If more than limit
// paths match, it returns the first limit paths and ErrTooManySecrets.
func FindSecretPaths(root string, limit int) ([]string, error) {
	var out []string
	tooMany := false
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // unreadable entries are skipped
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			return nil
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "vendor" || d.Name() == ".venv") {
			return filepath.SkipDir
		}
		if IsSecretPath(rel) {
			// A secret-named directory holding source code (a Go package
			// named "credentials") is walked file by file: its code stays
			// visible, its other files are masked individually.
			if d.IsDir() && !strongSecretDir(d.Name()) && !strings.HasPrefix(d.Name(), ".") && containsSource(p) {
				return nil
			}
			if len(out) >= limit {
				tooMany = true
				return filepath.SkipAll
			}
			out = append(out, rel)
			if d.IsDir() {
				return filepath.SkipDir
			}
		}
		return nil
	})
	if err == nil && tooMany {
		err = fmt.Errorf("%w: more than %d under %s", ErrTooManySecrets, limit, root)
	}
	return out, err
}

// containsSource reports whether the tree under dir holds a source file. It
// looks at no more than 5000 entries and skips dot directories; an
// unreadable or very large tree counts as not containing source, so it is
// masked whole (fail closed).
func containsSource(dir string) bool {
	seen, found := 0, false
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return filepath.SkipAll
		}
		if seen++; seen > 5000 {
			return filepath.SkipAll
		}
		if d.IsDir() && p != dir && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if d.Type().IsRegular() && isSourceFile(d.Name()) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// protectedPaths are workspace-relative globs a task's change must never
// touch, whatever the repository's verification config says: they control
// how the change itself is verified, reviewed or built in CI.
var protectedPaths = []string{
	".boundedcode", ".boundedcode/*",
	".github/workflows", ".github/workflows/*",
	"CODEOWNERS", ".github/CODEOWNERS", "docs/CODEOWNERS",
	".gitlab-ci.yml", ".gitmodules",
}

// IsProtectedPath reports whether a workspace-relative path is protected
// (see protectedPaths). Nested paths under a protected directory match.
func IsProtectedPath(rel string) bool {
	rel = filepath.ToSlash(filepath.Clean(rel))
	for _, g := range protectedPaths {
		if ok, _ := path.Match(g, rel); ok {
			return true
		}
		if !strings.ContainsAny(g, "*?[") && strings.HasPrefix(rel, g+"/") {
			return true
		}
	}
	return false
}

// commandRule denies a command line matching re.
type commandRule struct {
	re     *regexp.Regexp
	reason string
}

var deniedCommands = []commandRule{
	{regexp.MustCompile(`\bgit\s+push\b`), "pushing is never automatic"},
	{regexp.MustCompile(`\bgit\s+(reset\s+--hard|clean\s+-[a-z]*f)`), "destructive git operation"},
	{regexp.MustCompile(`\bgit\s+(merge|rebase)\b.*\b(main|master|release|production|prod)\b`), "merging into a protected branch"},
	{regexp.MustCompile(`\bgit\s+branch\s+-D\s+(main|master)\b`), "deleting a protected branch"},
	{regexp.MustCompile(`\bterraform\s+(apply|destroy|import|state\s+(rm|mv|push))\b`), "terraform mutation of real infrastructure"},
	{regexp.MustCompile(`\btofu\s+(apply|destroy)\b`), "opentofu mutation of real infrastructure"},
	{regexp.MustCompile(`\bpulumi\s+(up|destroy)\b`), "pulumi mutation of real infrastructure"},
	{regexp.MustCompile(`\bkubectl\s+(apply|delete|create|replace|patch|scale|rollout|drain|cordon|edit|exec)\b`), "kubectl mutation"},
	{regexp.MustCompile(`\bhelm\s+(install|upgrade|uninstall|rollback|delete)\b`), "helm release mutation"},
	{regexp.MustCompile(`\b(aws|gcloud|az)\s+\S+\s+(create|delete|put|update|terminate|deploy)`), "cloud CLI mutation"},
	{regexp.MustCompile(`\brm\s+-[a-z]*r[a-z]*f?\s+(/|~|\$HOME)(\s|$)`), "recursive delete of root or home"},
	{regexp.MustCompile(`\b(npm|pnpm|yarn)\s+publish\b|\bgoreleaser\b|\bgh\s+release\b|\bdocker\s+push\b|\btwine\s+upload\b`), "publishing artifacts"},
	{regexp.MustCompile(`(curl|wget)[^|]*\|\s*(sh|bash)\b`), "piping remote scripts into a shell"},
}

// CheckCommand returns an error if argv matches a denied pattern. The raw
// command line is checked, and so is a normalized form of every simple
// command in it (including `sh -c` bodies) with environment assignments,
// wrapper commands and global flags removed, so `git -C . push`,
// `kubectl --context=prod apply` or `terraform -chdir=x apply` are caught.
func CheckCommand(argv []string) error {
	line := strings.Join(argv, " ")
	candidates := append([]string{line}, normalizedCommands(line)...)
	if len(argv) >= 3 && strings.HasPrefix(argv[1], "-") && strings.Contains(argv[1], "c") {
		candidates = append(candidates, normalizedCommands(argv[2])...) // sh -c BODY, unsplit
	}
	for _, c := range candidates {
		for _, r := range deniedCommands {
			if r.re.MatchString(c) {
				return fmt.Errorf("policy: command denied (%s): %q", r.reason, truncate(line, 200))
			}
		}
		if recursiveDeleteOfRoot(c) {
			return fmt.Errorf("policy: command denied (recursive delete of root or home): %q", truncate(line, 200))
		}
	}
	return nil
}

// globalFlagsWithValue lists, per tool, global flags that take a separate
// value argument and come before the subcommand.
var globalFlagsWithValue = map[string]map[string]bool{
	"git":       {"-C": true, "-c": true, "--git-dir": true, "--work-tree": true, "--namespace": true, "--exec-path": true},
	"kubectl":   {"--context": true, "-n": true, "--namespace": true, "--kubeconfig": true, "--cluster": true, "--user": true, "-s": true, "--server": true, "--as": true},
	"helm":      {"--kube-context": true, "-n": true, "--namespace": true, "--kubeconfig": true},
	"terraform": {},
	"tofu":      {},
	"pulumi":    {"-C": true, "--cwd": true, "-s": true, "--stack": true},
}

// wrappers are commands that run their arguments as another command.
var wrappers = map[string]bool{"env": true, "sudo": true, "nohup": true, "time": true, "nice": true, "xargs": true, "exec": true, "command": true, "timeout": true}

// normalizedCommands splits a command line into simple commands and returns
// each as "tool subcommand args..." with the noise removed.
func normalizedCommands(line string) []string {
	var out []string
	for _, simple := range commandSeparators.Split(line, -1) {
		f := shellFields(simple)
		// Drop leading env assignments and wrapper commands (with their flags).
		for len(f) > 0 {
			switch {
			case strings.Contains(f[0], "=") && !strings.HasPrefix(f[0], "-"):
				f = f[1:]
			case wrappers[path.Base(f[0])]:
				f = f[1:]
				for len(f) > 0 && (strings.HasPrefix(f[0], "-") || isDurationArg(f[0])) {
					f = f[1:]
				}
			default:
				goto done
			}
		}
	done:
		if len(f) == 0 {
			continue
		}
		tool := path.Base(f[0])
		if (tool == "sh" || tool == "bash" || tool == "zsh" || tool == "dash") && len(f) >= 3 && strings.HasPrefix(f[1], "-") && strings.Contains(f[1], "c") {
			out = append(out, normalizedCommands(strings.Join(f[2:], " "))...)
			continue
		}
		rest := f[1:]
		if flags, ok := globalFlagsWithValue[tool]; ok {
			for len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
				if flags[rest[0]] && len(rest) > 1 {
					rest = rest[2:]
				} else {
					rest = rest[1:]
				}
			}
		}
		out = append(out, strings.Join(append([]string{tool}, rest...), " "))
	}
	return out
}

var (
	commandSeparators = regexp.MustCompile(`&&|\|\||[;|\n&()` + "`" + `]|\$\(`)
	durationArg       = regexp.MustCompile(`^[0-9.]+[smhd]?$`)
)

func isDurationArg(s string) bool { return durationArg.MatchString(s) }

// shellFields splits on whitespace and strips simple quotes.
func shellFields(s string) []string {
	var out []string
	for _, f := range strings.Fields(s) {
		if f = strings.Trim(f, `"'`); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// recursiveDeleteOfRoot reports `rm` with a recursive flag (in any form)
// targeting /, /*, ~, ~/ or $HOME.
func recursiveDeleteOfRoot(c string) bool {
	f := shellFields(c)
	if len(f) == 0 || path.Base(f[0]) != "rm" {
		return false
	}
	recursive, target := false, false
	for _, a := range f[1:] {
		switch {
		case a == "--recursive" || strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.ContainsAny(a, "rR"):
			recursive = true
		case a == "/" || a == "/*" || a == "~" || a == "~/" || a == "~/*" || a == "$HOME" || a == "$HOME/" || a == "${HOME}" || a == "$HOME/*":
			target = true
		}
	}
	return recursive && target
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
