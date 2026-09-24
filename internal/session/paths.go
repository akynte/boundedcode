package session

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// MakeOpenCodeSessionRoot creates a private, collision-resistant directory for
// one editor session. Persistent OpenCode data and state live beside it; only
// the returned session directory is removed when the editor exits.
func MakeOpenCodeSessionRoot(openCodeDir string) (string, error) {
	if openCodeDir == "" {
		return "", fmt.Errorf("session: OpenCode directory is empty")
	}
	sessions := filepath.Join(openCodeDir, "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		return "", err
	}
	name := fmt.Sprintf("session-%d-%d", os.Getpid(), time.Now().UnixNano())
	root := filepath.Join(sessions, name)
	if err := os.Mkdir(root, 0o700); err != nil {
		return "", err
	}
	return root, nil
}

// EnsureOpenCodeSessionDirs creates the private paths used by one editor
// process. The caller supplies paths already derived from the workspace store;
// keeping the operation here gives the session boundary one audited owner.
func EnsureOpenCodeSessionDirs(dirs ...string) error {
	for _, dir := range dirs {
		if dir == "" {
			return fmt.Errorf("session: OpenCode session directory is empty")
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// RemoveOpenCodeSessionRoot removes only the ephemeral directory returned by
// MakeOpenCodeSessionRoot. It is intentionally idempotent for deferred cleanup.
func RemoveOpenCodeSessionRoot(root string) error {
	if root == "" {
		return nil
	}
	return os.RemoveAll(root)
}
