package judgebench

import "os"

// NewScratchWorkDir owns the benchmark scratch directory lifecycle in the
// evaluation package. The CLI does not need to become a second filesystem
// authority just to create a temporary judge workspace.
func NewScratchWorkDir() (string, func(), error) {
	dir, err := os.MkdirTemp("", "bcode-judges-")
	if err != nil {
		return "", nil, err
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}
