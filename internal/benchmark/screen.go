package benchmark

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/sandbox"
)

// ScreenResult says whether a task is valid in this environment: its
// acceptance checks fail on the base and pass with the reference solution.
type ScreenResult struct {
	ID           string  `json:"id"`
	BaseFails    bool    `json:"base_fails"`
	GoldPasses   bool    `json:"gold_passes"`
	Valid        bool    `json:"valid"`
	BaseOutput   string  `json:"base_output,omitempty"`
	GoldOutput   string  `json:"gold_output,omitempty"`
	Error        string  `json:"error,omitempty"`
	WallSeconds  float64 `json:"wall_seconds"`
	SourceTokens int     `json:"source_tokens"`
}

// Screen materializes each task and runs its acceptance checks twice: on
// the untouched base, then with goldDir/<id>.patch applied. Reference
// patches are read only here, never by a task run.
func Screen(ctx context.Context, sb sandbox.Sandbox, fixturesDir, workRoot, goldDir string, tasks []TaskSpec, progress func(string)) []ScreenResult {
	var out []ScreenResult
	for _, spec := range tasks {
		start := time.Now()
		r := ScreenResult{ID: spec.ID}
		func() {
			fixture := ""
			if spec.Fixture != "" {
				fixture = filepath.Join(fixturesDir, spec.Fixture)
			}
			repos, err := materialize(ctx, fixture, spec.Sources, filepath.Join(workRoot, fmt.Sprintf("screen-%s-%d", spec.ID, time.Now().UnixNano()), "repos"), spec.Setup)
			if err != nil {
				r.Error = "materialize: " + err.Error()
				return
			}
			for _, name := range spec.Repos {
				r.SourceTokens += RepoSourceTokens(ctx, repos[name])
			}
			run := func() (bool, string, error) {
				for _, h := range spec.Hidden {
					dir := repos[h.Repo]
					if h.Patch != "" {
						if err := applyHiddenPatch(ctx, dir, "HEAD", h.Patch); err != nil {
							return false, "", err
						}
						continue
					}
					p := filepath.Join(dir, h.Path)
					_ = os.MkdirAll(filepath.Dir(p), 0o755)
					if err := os.WriteFile(p, []byte(h.Content), 0o644); err != nil {
						return false, "", err
					}
				}
				ok, outs := true, ""
				for _, c := range spec.Checks {
					cmd, err := sb.Command(ctx, checkSpec(ctx, spec, repos[c.Repo], c.Run))
					if err != nil {
						return false, "", err
					}
					b, err := cmd.CombinedOutput()
					if err != nil {
						ok = false
					}
					outs += tailStr(string(b), 1500)
				}
				return ok, outs, nil
			}
			pass, o, err := run()
			if err != nil {
				r.Error = "base: " + err.Error()
				return
			}
			r.BaseFails, r.BaseOutput = !pass, o
			gold, err := os.ReadFile(filepath.Join(goldDir, spec.ID+".patch"))
			if err != nil {
				r.Error = err.Error()
				return
			}
			for _, name := range spec.Repos {
				if _, err := gitops.Run(ctx, repos[name], "checkout", "-q", "--", "."); err != nil {
					r.Error = err.Error()
					return
				}
				if _, err := gitops.Run(ctx, repos[name], "clean", "-qfd"); err != nil {
					r.Error = err.Error()
					return
				}
			}
			if len(spec.Repos) != 1 {
				r.Error = "screening supports single-repository tasks"
				return
			}
			pf := filepath.Join(workRoot, spec.ID+".gold.patch")
			if err := os.WriteFile(pf, gold, 0o600); err != nil {
				r.Error = err.Error()
				return
			}
			defer os.Remove(pf)
			if _, err := gitops.Run(ctx, repos[spec.Repos[0]], "apply", "--whitespace=nowarn", pf); err != nil {
				r.Error = "gold: " + err.Error()
				return
			}
			pass, o, err = run()
			if err != nil {
				r.Error = "gold run: " + err.Error()
				return
			}
			r.GoldPasses, r.GoldOutput = pass, o
		}()
		r.Valid = r.Error == "" && r.BaseFails && r.GoldPasses
		r.WallSeconds = time.Since(start).Seconds()
		if progress != nil {
			progress(fmt.Sprintf("screen %s: valid=%v base_fails=%v gold_passes=%v source_tokens=%d %s", r.ID, r.Valid, r.BaseFails, r.GoldPasses, r.SourceTokens, r.Error))
		}
		out = append(out, r)
	}
	return out
}

// sourceExts are the files counted as repository source.
var sourceExts = map[string]bool{".go": true, ".js": true, ".mjs": true, ".cjs": true, ".ts": true, ".tsx": true,
	".jsx": true, ".vue": true, ".tf": true, ".proto": true, ".py": true, ".sh": true}

// RepoSourceTokens estimates the source tokens of a repository's tracked
// files (same estimator as context packs; dependency directories and
// untracked files excluded).
func RepoSourceTokens(ctx context.Context, repo string) int {
	files, err := gitops.Run(ctx, repo, "ls-files", "-z")
	if err != nil {
		return 0
	}
	total := 0
	for _, f := range strings.Split(files, "\x00") {
		if !sourceExts[filepath.Ext(f)] {
			continue
		}
		if st, err := os.Stat(filepath.Join(repo, f)); err == nil && st.Mode().IsRegular() {
			total += (int(st.Size())*10 + 31) / 32
		}
	}
	return total
}
