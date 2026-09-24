// Package oracle loads hidden acceptance checks: tests and commands the
// operator keeps outside the repository, which the supervisor applies to a
// candidate during verification and the coding model never sees.
//
// A repository's own tests are written, read and editable by the same model
// whose work they judge. A check the model cannot read cannot be satisfied by
// editing it, weakened by a patch, or special-cased by code that knows what it
// asserts. That is the whole point of keeping it here, and it is why nothing
// in this package's output may reach model context: only a check's ID and
// whether it passed.
//
// A suite is a directory of YAML files, each holding one or more checks:
//
//	checks:
//	  - id: refresh-token-single-use
//	    applies_to: ["services/auth/**"]
//	    must_not_change: ["services/auth/refresh_test.go"]
//	    files:
//	      services/auth/hidden_refresh_test.go: |
//	        package auth
//	        ...
//	    argv: ["go", "test", "-run", "TestHiddenRefresh", "./services/auth/"]
//	    timeout_seconds: 300
//	    canaries: ["bc-canary-4f1d93a2e7"]
//
// Files are written into the verification snapshot after the candidate is
// applied, then argv runs there, in the verification sandbox. A non-zero exit,
// a timeout, or a command that cannot run all mean the check did not pass.
package oracle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/akynte/boundedcode/internal/policy"
)

// DefaultTimeout bounds a check that does not name its own.
const DefaultTimeout = 10 * time.Minute

// Check is one hidden acceptance check.
type Check struct {
	// ID names the check in evidence and in the feedback the model receives.
	// It is the only part of a check the model is ever told.
	ID string `yaml:"id" json:"id"`
	// AppliesTo limits the check to changes touching these paths (exact
	// paths, directory prefixes, path.Match globs, or a /** suffix). Empty
	// means every change.
	AppliesTo []string `yaml:"applies_to,omitempty" json:"applies_to,omitempty"`
	// MustNotChange lists paths a candidate may not modify. A change that
	// passes by deleting the test it was meant to satisfy has not passed.
	MustNotChange []string `yaml:"must_not_change,omitempty" json:"must_not_change,omitempty"`
	// Files are written into the snapshot before argv runs, replacing any
	// candidate file at the same path.
	Files map[string]string `yaml:"files,omitempty" json:"files,omitempty"`
	// Argv decides the check. It runs with the snapshot as working directory.
	Argv []string `yaml:"argv" json:"argv"`
	// TimeoutSeconds bounds the run. Zero uses DefaultTimeout.
	TimeoutSeconds int `yaml:"timeout_seconds,omitempty" json:"timeout_seconds,omitempty"`
	// Canaries are strings that appear in this check's files and nowhere a
	// model should see: a comment with a random token is enough. Any prompt
	// about to be sent to a model that contains one is refused, and the task
	// stops. It is how a leak of the check is detected rather than assumed
	// not to happen.
	Canaries []string `yaml:"canaries,omitempty" json:"canaries,omitempty"`
}

// MinCanaryLength keeps canaries distinctive. A short canary would match
// ordinary code and stop tasks for leaks that did not happen.
const MinCanaryLength = 12

// Timeout is the check's bound.
func (c Check) Timeout() time.Duration {
	if c.TimeoutSeconds <= 0 {
		return DefaultTimeout
	}
	return time.Duration(c.TimeoutSeconds) * time.Second
}

// Applies reports whether the check is required for a change touching the
// given repository-relative paths.
func (c Check) Applies(changed []string) bool {
	if len(c.AppliesTo) == 0 {
		return true
	}
	for _, p := range changed {
		if policy.Covers(c.AppliesTo, p) {
			return true
		}
	}
	return false
}

// Tampered returns the changed paths the check protects.
func (c Check) Tampered(changed []string) []string {
	if len(c.MustNotChange) == 0 {
		return nil
	}
	var out []string
	for _, p := range changed {
		if policy.Covers(c.MustNotChange, p) {
			out = append(out, p)
		}
	}
	return out
}

// Suite is every check an operator has installed for a repository.
type Suite struct {
	// Dir is where the suite was loaded from.
	Dir string
	// Checks are sorted by ID.
	Checks []Check
	// Digest identifies the suite's exact content, so evidence can name the
	// oracle it was judged by without repeating it.
	Digest string
}

// Empty reports whether the suite holds no checks.
func (s *Suite) Empty() bool { return s == nil || len(s.Checks) == 0 }

// Canaries maps every canary in the suite to the check that declared it.
func (s *Suite) Canaries() map[string]string {
	if s.Empty() {
		return nil
	}
	out := map[string]string{}
	for _, c := range s.Checks {
		for _, canary := range c.Canaries {
			out[canary] = c.ID
		}
	}
	return out
}

type file struct {
	Checks []Check `yaml:"checks"`
}

// Load reads every *.yaml and *.yml file in dir. A missing directory is an
// error: an operator who named an oracle and got none would believe work was
// judged by checks that never ran.
func Load(dir string) (*Suite, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("oracle: %s must be an absolute path", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("oracle: %w", err)
	}
	var names []string
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !e.IsDir() && (ext == ".yaml" || ext == ".yml") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	suite := &Suite{Dir: dir}
	seen := map[string]string{}
	for _, name := range names {
		body, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // a file in the operator's oracle directory
		if err != nil {
			return nil, fmt.Errorf("oracle: %w", err)
		}
		var f file
		dec := yaml.NewDecoder(strings.NewReader(string(body)))
		dec.KnownFields(true)
		if err := dec.Decode(&f); err != nil {
			return nil, fmt.Errorf("oracle: %s: %w", name, err)
		}
		for _, c := range f.Checks {
			if err := validate(c); err != nil {
				return nil, fmt.Errorf("oracle: %s: %w", name, err)
			}
			if prev, dup := seen[c.ID]; dup {
				return nil, fmt.Errorf("oracle: check %q is defined in both %s and %s", c.ID, prev, name)
			}
			seen[c.ID] = name
			suite.Checks = append(suite.Checks, c)
		}
	}
	if len(suite.Checks) == 0 {
		return nil, fmt.Errorf("oracle: %s holds no checks", dir)
	}
	sort.Slice(suite.Checks, func(i, j int) bool { return suite.Checks[i].ID < suite.Checks[j].ID })

	canonical, err := json.Marshal(suite.Checks)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(canonical)
	suite.Digest = hex.EncodeToString(sum[:])
	return suite, nil
}

func validate(c Check) error {
	if strings.TrimSpace(c.ID) == "" {
		return fmt.Errorf("a check has no id")
	}
	if len(c.Argv) == 0 {
		return fmt.Errorf("check %q has no argv", c.ID)
	}
	if c.TimeoutSeconds < 0 {
		return fmt.Errorf("check %q has a negative timeout", c.ID)
	}
	for _, canary := range c.Canaries {
		if len(strings.TrimSpace(canary)) < MinCanaryLength {
			return fmt.Errorf("check %q: canary %q is shorter than %d characters and would match ordinary text",
				c.ID, canary, MinCanaryLength)
		}
	}
	for rel := range c.Files {
		clean := path.Clean(filepath.ToSlash(rel))
		if rel == "" || path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("check %q: file %q must be a relative path inside the repository", c.ID, rel)
		}
	}
	return nil
}

// Outside reports whether dir lies outside root, after resolving symlinks.
// An oracle inside the repository is readable by the model working on it,
// which would make it a visible test with extra steps.
func Outside(dir, root string) (bool, error) {
	d, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false, err
	}
	r, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(r, d)
	if err != nil {
		return false, err
	}
	return rel == ".." || strings.HasPrefix(rel, "../"), nil
}
