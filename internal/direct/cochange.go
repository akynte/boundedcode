package direct

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/akynte/boundedcode/internal/ecosystem"
	"github.com/akynte/boundedcode/internal/lease"
)

// After a file change in a git repository, the files the repository's
// history usually changes together with it are named in the tool result:
// a generated file and its source, the two halves of an encoder and
// decoder, go.mod and go.sum, a document and its changelog. A small model
// finishes the file it was asked about and misses the companion. The
// rule is an association rule over the last commits (Zimmermann et al.,
// 2004); its thresholds come from the trail study
// (docs/benchmarks/trail-study-2026-10.md), where on held-out commits
// 70-81% of the files named were changed by the same commit.

const (
	coChangeHistory    = 1000 // commits read
	coChangeMaxFiles   = 30   // larger commits (mass renames, vendoring) are not evidence
	coChangeSupport    = 5    // commits that changed both files, at least
	coChangeConfidence = 0.7  // share of the file's commits that changed the other, at least
	coChangeNamed      = 3    // files named per change
	coChangeTimeout    = 5 * time.Second
)

// coChange is the session's co-change index, read from git on the first
// change.
type coChange struct {
	mu      sync.Mutex
	commits [][]string       // files per commit, newest first
	of      map[string][]int // file -> indexes into commits
	changed map[string]bool  // files changed in this session
	named   map[string]bool  // files already named in a note
}

// coChangeIndex creates the index on first use (not under r.mu, which a
// turn holds while it runs tools).
func (r *Runner) coChangeIndex() *coChange {
	r.coOnce.Do(func() {
		r.co = &coChange{changed: map[string]bool{}, named: map[string]bool{}}
		r.co.load(r.Opts.Root)
	})
	return r.co
}

// load reads the history; outside a git repository the index stays empty.
func (co *coChange) load(root string) {
	ctx, cancel := context.WithTimeout(context.Background(), coChangeTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, "git", "log", "--no-merges", "--no-renames", "--relative", "--format=%x00", "--name-only",
		"-n", strconv.Itoa(coChangeHistory), "HEAD")
	c.Dir = root
	c.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := c.Output()
	if err != nil {
		return
	}
	co.commits, co.of = parseCoChangeLog(string(out))
}

func parseCoChangeLog(raw string) ([][]string, map[string][]int) {
	var commits [][]string
	of := map[string][]int{}
	for _, chunk := range strings.Split(raw, "\x00") {
		var files []string
		for _, p := range strings.Split(chunk, "\n") {
			if p = strings.TrimSpace(p); p != "" && coChangeCounted(p) {
				files = append(files, p)
			}
		}
		if len(files) == 0 || len(files) > coChangeMaxFiles {
			continue
		}
		for _, f := range files {
			of[f] = append(of[f], len(commits))
		}
		commits = append(commits, files)
	}
	return commits, of
}

// coChangeCounted: the study scored project files outside tests.
func coChangeCounted(p string) bool {
	return !lease.Skipped(p) && !ecosystem.IsTest(p)
}

// coChangeNote returns the note for a change to rel (slash-separated,
// relative to the root), or "".
func (r *Runner) coChangeNote(rel string) string {
	co := r.coChangeIndex()
	co.mu.Lock()
	defer co.mu.Unlock()
	co.changed[rel] = true
	if !coChangeCounted(rel) || len(co.of[rel]) == 0 {
		return ""
	}
	n := len(co.of[rel])
	together := map[string]int{}
	for _, i := range co.of[rel] {
		for _, f := range co.commits[i] {
			if f != rel {
				together[f]++
			}
		}
	}
	var cand []string
	for f, k := range together {
		if k < coChangeSupport || float64(k) < coChangeConfidence*float64(n) || co.changed[f] || co.named[f] {
			continue
		}
		if _, err := os.Stat(filepath.Join(r.Opts.Root, filepath.FromSlash(f))); err != nil {
			continue
		}
		cand = append(cand, f)
	}
	if len(cand) == 0 {
		return ""
	}
	sort.Slice(cand, func(i, j int) bool {
		if together[cand[i]] != together[cand[j]] {
			return together[cand[i]] > together[cand[j]]
		}
		return cand[i] < cand[j]
	})
	cand = cand[:min(len(cand), coChangeNamed)]
	var b strings.Builder
	fmt.Fprintf(&b, "history note: in this repository, commits that changed %s usually changed these too; check whether this change needs them:", rel)
	for _, f := range cand {
		co.named[f] = true
		fmt.Fprintf(&b, "\n- %s (%d of %d commits)", f, together[f], n)
	}
	return b.String()
}
