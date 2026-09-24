package opencode

import (
	"fmt"
	"os"
	"path/filepath"
)

// PrepareBCodeBinary creates the private per-session bcode shim. The path is
// derived from the workspace's OpenCode state directory; the caller cannot
// choose an arbitrary write target. Keeping this filesystem boundary here also
// lets the command package remain a launcher rather than a second state owner.
func PrepareBCodeBinary(stateDir, executable string) (string, error) {
	binDir := filepath.Join(stateDir, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		return "", err
	}
	link := filepath.Join(binDir, "bcode")
	if info, err := os.Lstat(link); err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return "", fmt.Errorf("refusing to replace non-symlink %s", link)
		}
		if err := os.Remove(link); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Symlink(executable, link); err != nil {
		return "", err
	}
	return binDir, nil
}

// WriteBrokerCapability persists the loopback capability where the confined
// plugin can find it. It is session state under the workspace-owned state
// directory, not repository content.
func WriteBrokerCapability(stateDir, endpoint string) (string, error) {
	path := filepath.Join(stateDir, ".bcode-broker")
	if err := os.WriteFile(path, []byte(endpoint), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// RemoveBrokerCapability removes the short-lived capability file.
func RemoveBrokerCapability(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
