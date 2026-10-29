package policy

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// DataPolicyFile is where a repository says where its content may be sent,
// relative to its root. It is under .boundedcode, which a task's change can
// never touch (protectedPaths), and it is read from the user's checkout,
// not from an agent's worktree.
const DataPolicyFile = ".boundedcode/data-policy.yaml"

// DataPolicy is a repository's data-handling rule.
type DataPolicy struct {
	// ExternalModels is "allow" (the default when the file is absent) or
	// "deny": no content of the repository goes to a model outside this
	// machine, neither a cloud provider in direct mode nor a frontier
	// escalation.
	ExternalModels string `yaml:"external_models"`
	// Path is the file the policy was read from ("" when absent).
	Path string `yaml:"-"`
}

// Data policy values.
const (
	ExternalAllow = "allow"
	ExternalDeny  = "deny"
)

// LoadDataPolicy reads root's data policy. A missing file allows; a file
// that does not parse, or names an unknown value, is an error, and the
// callers treat an error as deny (fail closed).
func LoadDataPolicy(root string) (DataPolicy, error) {
	p := filepath.Join(root, filepath.FromSlash(DataPolicyFile))
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return DataPolicy{ExternalModels: ExternalAllow}, nil
	}
	if err != nil {
		return DataPolicy{ExternalModels: ExternalDeny, Path: p}, err
	}
	var d DataPolicy
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&d); err != nil {
		return DataPolicy{ExternalModels: ExternalDeny, Path: p}, fmt.Errorf("%s: %w", p, err)
	}
	d.Path = p
	switch d.ExternalModels {
	case "":
		d.ExternalModels = ExternalAllow
	case ExternalAllow, ExternalDeny:
	default:
		return DataPolicy{ExternalModels: ExternalDeny, Path: p}, fmt.Errorf("%s: external_models: %q is not allow or deny", p, d.ExternalModels)
	}
	return d, nil
}

// ExternalDenied reports whether any of the repositories at roots denies
// external models, and why. An unreadable policy denies.
func ExternalDenied(roots ...string) (bool, string) {
	for _, r := range roots {
		d, err := LoadDataPolicy(r)
		switch {
		case err != nil:
			return true, fmt.Sprintf("%s could not be read (%v), so external models are refused", DataPolicyFile, err)
		case d.ExternalModels == ExternalDeny:
			return true, fmt.Sprintf("%s denies external models (%s)", filepath.Base(r), d.Path)
		}
	}
	return false, ""
}
