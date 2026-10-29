package direct

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/akynte/boundedcode/internal/prune"
)

// Observations: what a tool returns is fitted for the model before it
// enters the history. Command output with something to sort (errors,
// failing tests, stack traces, progress, downloads, passing tests,
// repeated lines) is pruned to the observation budget by internal/prune,
// which keeps the failures, their locations and assertions, the frames in
// the project and the outcome, and names what it dropped. Other output
// over the cap is cut in the middle as before. Either way the original is
// kept with the session and recall shows it, by line range or by pattern:
// nothing the model was not shown is lost.

const (
	// defaultObservationLocal and defaultObservationCloud are the budgets
	// command output is pruned to.
	defaultObservationLocal = 8 << 10
	defaultObservationCloud = 24 << 10
	// maxStoredOutput caps an original kept for recall.
	maxStoredOutput = 8 << 20
	// recallLines is how many lines of an original recall shows at once.
	recallLines = 300
)

// FullOutput describes an original output the model was shown a shorter
// version of.
type FullOutput struct {
	Bytes int `json:"bytes"`
	Lines int `json:"lines"`
	// Pruned: shortened by internal/prune; otherwise cut in the middle.
	Pruned bool `json:"pruned,omitempty"`
	// Stored: the original is kept (next to the session file, or in
	// memory for a session without one).
	Stored bool `json:"stored"`
}

func (r *Runner) observationBytes() int {
	if r.Opts.ObservationBytes > 0 {
		return r.Opts.ObservationBytes
	}
	if r.localProvider() {
		return defaultObservationLocal
	}
	return defaultObservationCloud
}

// observe fits a tool result for the model and keeps the original when
// the model is shown less.
func (r *Runner) observe(tool, id, out string) string {
	command := tool == "bash" || tool == "bash_output"
	limit := r.maxToolOutput()
	var shown string
	full := &FullOutput{Bytes: len(out), Lines: strings.Count(out, "\n") + 1}
	switch {
	case command && !r.Opts.NoPrune && prune.Signal(out):
		res := prune.Prune(out, prune.Options{Budget: min(r.observationBytes(), limit), Project: r.inProject})
		if !res.Pruned {
			return res.Text
		}
		shown, full.Pruned = res.Text, true
	case len(out) > limit:
		shown = clipTo(out, limit)
	default:
		return out
	}
	if r.Session != nil && id != "" {
		if err := r.Session.SaveOutput(id, out); err == nil {
			full.Stored = true
			shown += fmt.Sprintf("\n[the whole output (%d lines) is kept: recall id %q with offset and limit, or a pattern, shows any part of it]", full.Lines, id)
		}
	}
	if r.fulls == nil {
		r.fulls = map[string]*FullOutput{}
	}
	r.fulls[id] = full
	return shown
}

// inProject reports whether a file a stack frame names is the project's:
// inside the root and not in a dependency directory.
func (r *Runner) inProject(file string) bool {
	f := filepath.FromSlash(strings.TrimPrefix(file, "file://"))
	if !filepath.IsAbs(f) {
		f = filepath.Join(r.cwd(), f)
	}
	rel, err := filepath.Rel(r.Opts.Root, f)
	if err != nil || strings.HasPrefix(rel, "..") {
		return false
	}
	for _, d := range []string{"node_modules", "vendor", ".venv", "venv", "site-packages", "target", "build", "dist"} {
		if rel == d || strings.HasPrefix(rel, d+string(filepath.Separator)) || strings.Contains(rel, string(filepath.Separator)+d+string(filepath.Separator)) {
			return false
		}
	}
	_, err = os.Stat(f)
	return err == nil
}

// SaveOutput keeps a tool result's original output for recall: next to
// the session file (<session>.outputs/<id>.txt), or in memory.
func (s *Session) SaveOutput(id, text string) error {
	if len(text) > maxStoredOutput {
		text = clipTo(text, maxStoredOutput)
	}
	if s.Path == "" {
		if s.outputs == nil {
			s.outputs = map[string]string{}
		}
		s.outputs[id] = text
		return nil
	}
	dir := strings.TrimSuffix(s.Path, ".jsonl") + ".outputs"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, outputName(id)), []byte(text), 0o600)
}

// Output returns an original kept by SaveOutput.
func (s *Session) Output(id string) (string, bool) {
	if s.Path == "" {
		t, ok := s.outputs[id]
		return t, ok
	}
	b, err := os.ReadFile(filepath.Join(strings.TrimSuffix(s.Path, ".jsonl")+".outputs", outputName(id)))
	return string(b), err == nil
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,80}$`)

func outputName(id string) string {
	if safeID.MatchString(id) && !strings.Contains(id, "..") {
		return id + ".txt"
	}
	h := sha256.Sum256([]byte(id))
	return hex.EncodeToString(h[:12]) + ".txt"
}

// window shows part of an original: the lines matching pattern with two
// lines around each, or limit lines from offset (1-based).
func window(text, pattern string, offset, limit int) (string, error) {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if limit <= 0 {
		limit = recallLines
	}
	if pattern != "" {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return "", fmt.Errorf("pattern: %w", err)
		}
		show := make([]bool, len(lines))
		hits := 0
		for i, l := range lines {
			if re.MatchString(l) {
				hits++
				for j := max(0, i-2); j <= min(len(lines)-1, i+2); j++ {
					show[j] = true
				}
			}
		}
		if hits == 0 {
			return fmt.Sprintf("[no line of the %d matches %q]", len(lines), pattern), nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "[%d of %d lines match %q; each with two lines around it]\n", hits, len(lines), pattern)
		shown, last := 0, -2
		for i, l := range lines {
			if !show[i] {
				continue
			}
			if shown >= limit {
				fmt.Fprintf(&b, "… [more matches after line %d: narrow the pattern or give an offset]\n", i+1)
				break
			}
			if i > last+1 {
				b.WriteString("--\n")
			}
			fmt.Fprintf(&b, "%d: %s\n", i+1, l)
			last = i
			shown++
		}
		return strings.TrimRight(b.String(), "\n"), nil
	}
	if offset <= 0 {
		offset = 1
	}
	if offset > len(lines) {
		return fmt.Sprintf("[the output has %d lines]", len(lines)), nil
	}
	end := min(len(lines), offset-1+limit)
	head := fmt.Sprintf("[lines %d–%d of %d]\n", offset, end, len(lines))
	return head + strings.Join(lines[offset-1:end], "\n"), nil
}
