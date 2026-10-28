package contextplan

import (
	"context"
	"os/exec"
	"strings"

	"github.com/akynte/boundedcode/internal/retrieve"
)

// Retrieval seeds (the strings from a request the planner searches for)
// are ranked by internal/retrieve; see retrieve.Seeds.

// Seed is one thing to look up.
type Seed = retrieve.Seed

// Seeds ranks the retrieval seeds of a request.
func Seeds(request string) []Seed { return retrieve.Seeds(request) }

const (
	maxSeedFileMatches  = 25 // a sample name matching more files is too common to help
	frequencyCheckLimit = 10 // names checked against the repository per pack
)

// fileMatches counts files containing name as a whole word under root,
// stopping at limit+1 (ripgrep; -1 if it cannot run).
func fileMatches(ctx context.Context, root, name string, limit int) int {
	rg, err := exec.LookPath(rgBinary)
	if err != nil {
		return -1
	}
	cmd := exec.CommandContext(ctx, rg, "-l", "-w", "--fixed-strings", "--max-filesize", "1M",
		"-g", "!vendor", "-g", "!node_modules", "--", lastSegment(name), ".")
	cmd.Dir = root
	out, _ := cmd.Output() // exit 1 means no match
	n := strings.Count(string(out), "\n")
	if n > limit {
		return limit + 1
	}
	return n
}
