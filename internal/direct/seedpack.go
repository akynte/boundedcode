package direct

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/contextplan"
)

// Before the first model call of a turn, the names the request mentions
// (identifiers, paths, quoted errors: contextplan.Seeds ranks them) are
// looked up in the project and the places found travel with the user
// message. A small model otherwise spends its first turns on grep and
// read calls at 35 tokens per second each; a deterministic lookup costs
// milliseconds and the model starts at the right file.

const (
	seedPackSeeds    = 6    // seeds looked up per message
	seedPackHits     = 6    // places listed per seed
	seedPackMaxFiles = 25   // a seed found in more files than this is too common to help
	seedPackTokens   = 1200 // size cap of the block
	seedPackTimeout  = 3 * time.Second
)

// seedPack returns the "likely relevant places" block for a request, or
// "" when nothing useful was found.
func (r *Runner) seedPack(ctx context.Context, request string) string {
	if r.Opts.NoSeedPack {
		return ""
	}
	seeds := contextplan.Seeds(request)
	if len(seeds) == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, seedPackTimeout)
	defer cancel()
	root, err := filepath.EvalSymlinks(r.Opts.Root)
	if err != nil {
		root = r.Opts.Root
	}
	var b strings.Builder
	b.WriteString("Likely relevant places, found by searching the project for the names in the request (verify before relying on them):\n")
	found, looked := 0, 0
	for _, s := range seeds {
		if looked == seedPackSeeds || ctx.Err() != nil {
			break
		}
		if len(s.Text) < 3 {
			continue
		}
		looked++
		var line string
		if s.Kind == "path" || looksLikeFile(s.Text) {
			line = r.seedPath(root, s.Text)
		} else {
			line = r.seedMatches(ctx, root, s.Text)
		}
		if line == "" {
			continue
		}
		found++
		b.WriteString(line + "\n")
		if contextplan.EstimateTokens(b.String()) > seedPackTokens {
			break
		}
	}
	if found == 0 {
		return ""
	}
	return strings.TrimRight(b.String(), "\n")
}

func looksLikeFile(s string) bool {
	return strings.Contains(s, "/") || regexp.MustCompile(`\.[a-z]{1,5}$`).MatchString(s)
}

// seedPath reports a path the request names, with its size and, for a
// long file, the first entries of its outline.
func (r *Runner) seedPath(root, p string) string {
	abs := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(p, "./")))
	st, err := os.Stat(abs)
	if err != nil || r.checkReadable(abs) != nil {
		// Try the base name anywhere in the project.
		base := filepath.Base(p)
		var hits []string
		_ = r.walk(root, func(file, rel string, _ os.DirEntry) error {
			if filepath.Base(file) == base {
				hits = append(hits, rel)
				if len(hits) == seedPackHits {
					return errStop
				}
			}
			return nil
		})
		if len(hits) == 0 {
			return ""
		}
		return fmt.Sprintf("- %s: %s", p, strings.Join(hits, ", "))
	}
	rel := r.rootRel(abs)
	if st.IsDir() {
		return fmt.Sprintf("- %s: a directory", rel)
	}
	return fmt.Sprintf("- %s: %d bytes", rel, st.Size())
}

// seedMatches lists where a name occurs, definitions first.
func (r *Runner) seedMatches(ctx context.Context, root, name string) string {
	pattern := regexp.QuoteMeta(name)
	if regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(name) {
		pattern = `\b` + pattern + `\b`
	}
	out, err := r.goGrep(ctx, root, pattern, "", false, "content", 0)
	if err != nil || out == "(no matches)" || strings.Contains(out, "too many to show") {
		return ""
	}
	type hit struct {
		rel  string
		line int
		text string
		def  bool
	}
	var hits []hit
	files := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		rel, rest, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		ln, text, ok := strings.Cut(rest, ":")
		if !ok {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(ln, "%d", &n); err != nil {
			continue
		}
		files[rel] = true
		hits = append(hits, hit{rel: rel, line: n, text: strings.TrimSpace(text), def: definitionLine.MatchString(text) || memberLine.MatchString(text)})
	}
	if len(hits) == 0 || len(files) > seedPackMaxFiles {
		return ""
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].def && !hits[j].def })
	var parts []string
	for i, h := range hits {
		if i == seedPackHits {
			parts = append(parts, fmt.Sprintf("… %d more", len(hits)-i))
			break
		}
		text := h.text
		if len(text) > 90 {
			text = text[:90] + "…"
		}
		if h.def {
			parts = append(parts, fmt.Sprintf("%s:%d (%s)", h.rel, h.line, text))
		} else {
			parts = append(parts, fmt.Sprintf("%s:%d", h.rel, h.line))
		}
	}
	return fmt.Sprintf("- %s: %s", name, strings.Join(parts, ", "))
}

// SeedPackFor is the block a new session's first message would carry for
// request in root, with no session state: the retrieval benchmark's
// baseline.
func SeedPackFor(ctx context.Context, root, request string) string {
	r := &Runner{Opts: Options{Root: root}}
	return r.seedPack(ctx, request)
}
