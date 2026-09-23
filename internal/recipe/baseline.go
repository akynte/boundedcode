package recipe

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Verdict is what a candidate's result means beside the baseline's.
//
// Absolute verification asks "does everything pass". On a repository this
// project did not write, the answer is no before the solver touches
// anything: a linter whose rules postdate the tree, a test the project runs
// through its own harness, a submodule outside the build. Requiring every
// preset to pass makes those repositories unmeasurable, and the zero says
// nothing about the change. The question worth asking is whether the change
// made anything worse.
type Verdict string

const (
	// VerdictPass: it passed before and it passes now.
	VerdictPass Verdict = "pass"
	// VerdictRegression: the candidate introduced a failure, or worsened one.
	VerdictRegression Verdict = "regression"
	// VerdictImprovement: it was failing and now passes.
	VerdictImprovement Verdict = "improvement"
	// VerdictBaselinePreserved: it was failing, it still fails, and in no new
	// way. Not a pass, and not the candidate's fault.
	VerdictBaselinePreserved Verdict = "baseline_failure_preserved"
)

// BaselineEntry is what one preset did on the untouched tree, recorded with
// everything that would make the record inapplicable if it changed.
type BaselineEntry struct {
	Preset string `json:"preset_id"`
	// Image, Command and Env are the execution-relevant inputs. A baseline
	// measured under different ones describes a different experiment.
	Image   string `json:"runtime_image"`
	Command string `json:"command_digest"`
	Env     string `json:"environment_digest"`

	Status Status `json:"baseline_status"`
	// ExitCode is the coarsest comparison available, and the only one left
	// when a tool names no failures.
	ExitCode int `json:"exit_status"`
	// Failures is the normalized failure set, sorted.
	Failures []string `json:"normalized_failure_set"`
	// Normalizable is false when the tool's output could not be reduced to
	// comparable identities. The limitation is recorded rather than papered
	// over: without it, "the same bytes" would be mistaken for "the same
	// failures".
	Normalizable bool   `json:"normalizable"`
	Note         string `json:"note,omitempty"`
	// Artifact addresses the full output.
	Artifact string `json:"raw_log_reference,omitempty"`
}

// Baseline is the whole recorded baseline for one task.
type Baseline struct {
	TaskID        string                   `json:"task_id"`
	FixtureDigest string                   `json:"fixture_digest"`
	Entries       map[string]BaselineEntry `json:"entries"`
}

// Applies reports whether the recorded entry still describes this preset run
// under these inputs. Anything execution-relevant that changed invalidates it.
func (b *Baseline) Applies(p Preset, image, envDigest string) (BaselineEntry, bool) {
	if b == nil {
		return BaselineEntry{}, false
	}
	e, ok := b.Entries[p.Name]
	if !ok {
		return BaselineEntry{}, false
	}
	if e.Image != image || e.Command != CommandDigest(p) || e.Env != envDigest {
		return BaselineEntry{}, false
	}
	return e, true
}

// CommandDigest identifies a preset's command, so that editing the argv
// invalidates a baseline measured with the old one.
func CommandDigest(p Preset) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00", p.Name, p.Kind, p.Dir)
	for _, a := range p.Argv {
		fmt.Fprintf(h, "%s\x00", a)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// EnvDigest identifies the environment a preset ran under. Order does not
// matter; content does.
func EnvDigest(env []string) string {
	sorted := append([]string(nil), env...)
	sort.Strings(sorted)
	h := sha256.New()
	for _, kv := range sorted {
		fmt.Fprintf(h, "%s\x00", kv)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Unstable output that says nothing about whether the code is correct.
var (
	reDuration  = regexp.MustCompile(`\b\d+(\.\d+)?(ns|µs|ms|s|m)\b`)
	reTimestamp = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d+)?Z?\b`)
	reHexAddr   = regexp.MustCompile(`\b0x[0-9a-fA-F]+\b`)
	reTempPath  = regexp.MustCompile(`/tmp/[^\s:]+|/var/folders/[^\s:]+`)
	reLineCol   = regexp.MustCompile(`:\d+(:\d+)?\b`)
	reSpace     = regexp.MustCompile(`\s+`)
)

// normalizeMessage strips what changes between two runs of the same failure.
//
// Line and column go too. A candidate edits files, so a diagnostic that was
// already there moves; keeping its line would make every untouched
// pre-existing complaint look new the moment a line was inserted above it.
func normalizeMessage(root, s string) string {
	if root != "" {
		s = strings.ReplaceAll(s, root, "")
	}
	s = reTempPath.ReplaceAllString(s, "")
	s = reTimestamp.ReplaceAllString(s, "")
	s = reDuration.ReplaceAllString(s, "")
	s = reHexAddr.ReplaceAllString(s, "")
	s = reLineCol.ReplaceAllString(s, "")
	s = reSpace.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func normalizePath(root, p string) string {
	if p == "" {
		return ""
	}
	if root != "" && strings.HasPrefix(p, root) {
		if rel, err := filepath.Rel(root, p); err == nil {
			p = rel
		}
	}
	return strings.TrimPrefix(filepath.ToSlash(p), "./")
}

// NormalizeFailures reduces a result to comparable failure identities.
//
// It uses what the existing parsers already produced — named tests and
// structured findings — rather than the raw log, because the log carries
// paths, orderings and timings that differ between two runs of the same
// failure.
//
// The second return says whether the reduction can be trusted. A failing
// preset that yielded no identity at all cannot be compared, and saying so is
// the point: silently treating it as equivalent to the baseline would let a
// real regression through.
func NormalizeFailures(root string, r Result) ([]string, bool, string) {
	seen := map[string]bool{}
	add := func(id string) {
		if id != "" && !seen[id] {
			seen[id] = true
		}
	}
	for name, st := range r.Summary.Tests {
		if st == Fail || st == Error {
			add("test:" + name)
		}
	}
	for _, f := range r.Summary.Findings {
		if f.Test != "" {
			add("test:" + f.Test)
			continue
		}
		add("diag:" + normalizePath(root, f.File) + "|" + f.Rule + "|" + normalizeMessage(root, f.Message))
	}

	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)

	switch {
	case r.Status == Pass || r.Status == Skipped:
		return out, true, ""
	case len(out) > 0 && !r.Summary.Truncated:
		return out, true, ""
	case r.Summary.Truncated:
		return out, false, "the tool's findings were truncated, so its failure set is incomplete"
	default:
		return out, false, "the tool reported failure without naming a test or a diagnostic, " +
			"so two failures cannot be told apart"
	}
}

// PresetComparison is one preset judged against its baseline.
type PresetComparison struct {
	Preset      string   `json:"preset"`
	Verdict     Verdict  `json:"verdict"`
	NewFailures []string `json:"new_failures,omitempty"`
	Fixed       []string `json:"fixed,omitempty"`
	// Limitation records that the comparison could not be made on identities.
	// It never silently becomes equivalence.
	Limitation string `json:"limitation,omitempty"`
}

// Compare judges one candidate result against its baseline entry.
func Compare(root string, base BaselineEntry, cand Result) PresetComparison {
	out := PresetComparison{Preset: base.Preset}
	candFailed := cand.Status == Fail || cand.Status == Error
	baseFailed := base.Status == Fail || base.Status == Error

	switch {
	case !baseFailed && !candFailed:
		out.Verdict = VerdictPass
		return out
	case !baseFailed && candFailed:
		out.Verdict = VerdictRegression
		ids, ok, note := NormalizeFailures(root, cand)
		out.NewFailures = ids
		if !ok {
			out.Limitation = note
		}
		return out
	case baseFailed && !candFailed:
		out.Verdict = VerdictImprovement
		out.Fixed = base.Failures
		return out
	}

	// Both failed: the question is whether anything is new.
	ids, ok, note := NormalizeFailures(root, cand)
	if !ok || !base.Normalizable {
		out.Verdict = VerdictBaselinePreserved
		out.Limitation = "not compared on failure identities: "
		switch {
		case !base.Normalizable && base.Note != "":
			out.Limitation += "baseline: " + base.Note
		case note != "":
			out.Limitation += note
		default:
			out.Limitation += "the failure set could not be normalized"
		}
		if base.ExitCode != cand.ExitCode {
			out.Limitation += fmt.Sprintf("; exit status changed from %d to %d",
				base.ExitCode, cand.ExitCode)
		}
		return out
	}

	had := map[string]bool{}
	for _, id := range base.Failures {
		had[id] = true
	}
	now := map[string]bool{}
	for _, id := range ids {
		now[id] = true
		if !had[id] {
			out.NewFailures = append(out.NewFailures, id)
		}
	}
	for _, id := range base.Failures {
		if !now[id] {
			out.Fixed = append(out.Fixed, id)
		}
	}
	sort.Strings(out.NewFailures)
	sort.Strings(out.Fixed)
	if len(out.NewFailures) > 0 {
		out.Verdict = VerdictRegression
		return out
	}
	out.Verdict = VerdictBaselinePreserved
	return out
}

// CheckAgainstBaseline is the baseline-relative counterpart of CheckPresets.
//
// A candidate is rejected for what it broke, not for what was already broken.
// Everything else the absolute check enforced still holds: every declared
// preset must have produced a result for this candidate, because a missing
// result is not evidence of anything.
func CheckAgainstBaseline(root string, presets []Preset, results []Result,
	candidate string, base *Baseline, image, envDigest string) (bool, []string, []PresetComparison) {

	byName := map[string]Result{}
	for _, r := range results {
		if r.Candidate == candidate {
			byName[r.Recipe] = r
		}
	}
	var reasons []string
	var comparisons []PresetComparison
	if len(presets) == 0 {
		return false, []string{"no required verification presets"}, nil
	}
	for _, p := range presets {
		r, ok := byName[p.Name]
		if !ok {
			reasons = append(reasons, "required preset produced no result on this candidate: "+p.Name)
			continue
		}
		entry, applicable := base.Applies(p, image, envDigest)
		if !applicable {
			// No usable baseline: fall back to the absolute rule rather than
			// assume the failure was pre-existing.
			if r.Status != Pass && r.Status != Skipped {
				reasons = append(reasons,
					"no applicable baseline for "+p.Name+", and it did not pass: "+r.Summary.Headline)
			}
			comparisons = append(comparisons, PresetComparison{
				Preset: p.Name, Verdict: VerdictRegression,
				Limitation: "no applicable baseline; judged absolutely",
			})
			continue
		}
		cmp := Compare(root, entry, r)
		comparisons = append(comparisons, cmp)
		if cmp.Verdict == VerdictRegression {
			detail := p.Name + ": " + r.Summary.Headline
			if len(cmp.NewFailures) > 0 {
				detail = p.Name + " introduced " + fmt.Sprint(len(cmp.NewFailures)) +
					" new failure(s): " + strings.Join(clip(cmp.NewFailures, 3), "; ")
			}
			reasons = append(reasons, detail)
		}
	}
	return len(reasons) == 0, reasons, comparisons
}

func clip(in []string, n int) []string {
	if len(in) <= n {
		return in
	}
	return append(append([]string(nil), in[:n]...), "…")
}
