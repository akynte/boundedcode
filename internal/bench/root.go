package bench

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/akynte/boundedcode/internal/store"
)

// ValidateOutputLocation rejects an output tree before any schedule or lock
// file is created when it would fall inside one of the suite's source roots.
func ValidateOutputLocation(output string, s Suite) error {
	if output == "" {
		return errors.New("bench: output root is empty")
	}
	for _, task := range s.Tasks {
		source := task.ResolvedRepository()
		if source == "" || strings.Contains(source, "://") {
			continue
		}
		info, err := os.Stat(source)
		if err != nil || !info.IsDir() {
			continue
		}
		inside, err := pathWithin(comparablePath(source), comparablePath(output))
		if err != nil {
			return fmt.Errorf("bench: validate output root: %w", err)
		}
		if inside {
			return fmt.Errorf("bench: output root %s is inside source repository %s", output, source)
		}
	}
	return nil
}

// OpenPrivateRoot creates an isolated data root for a non-persistent benchmark
// process and returns a cleanup function. The root is intentionally outside
// the operator's normal data directory: smoke runs must not require an
// initialized workspace and must not leave results in a user's shared store.
// All filesystem ownership is kept behind the benchmark package boundary so
// the CLI does not bypass the storescope storage policy.
func OpenPrivateRoot() (*store.Root, func() error, error) {
	dir, err := os.MkdirTemp("", "boundedcode-bench-")
	if err != nil {
		return nil, nil, fmt.Errorf("bench: create private data root: %w", err)
	}
	root, err := store.OpenRoot(dir)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, nil, fmt.Errorf("bench: open private data root: %w", err)
	}

	var once sync.Once
	var cleanupErr error
	cleanup := func() error {
		once.Do(func() {
			cleanupErr = root.CloseAll()
			if err := os.RemoveAll(dir); err != nil {
				cleanupErr = errors.Join(cleanupErr, err)
			}
		})
		return cleanupErr
	}
	return root, cleanup, nil
}
