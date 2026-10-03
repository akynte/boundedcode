// Package policy holds deterministic safety rules. The LLM cannot override
// them: they are evaluated in Go, outside the agent, before actions run.
package policy

import (
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

// FindSecretPaths walks root and returns workspace-relative secret paths
// (directories are reported once and not descended). Used to mask them in the
// sandbox. The walk skips .git and dependency directories.
func FindSecretPaths(root string, limit int) ([]string, error) {
	var out []string
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
			out = append(out, rel)
			if len(out) >= limit {
				return filepath.SkipAll
			}
			if d.IsDir() {
				return filepath.SkipDir
			}
		}
		return nil
	})
	return out, err
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

// CheckCommand returns an error if argv (joined) matches a denied pattern.
func CheckCommand(argv []string) error {
	line := strings.Join(argv, " ")
	for _, r := range deniedCommands {
		if r.re.MatchString(line) {
			return fmt.Errorf("policy: command denied (%s): %q", r.reason, truncate(line, 200))
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
