package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Pre-fix snapshot integrity.
//
// A historical task has a base revision A and a fixing revision B. The solver
// executes against A and must not be able to reach B by any route: not the
// diff, not the later tree, not the annotations, and not a .git directory
// whose log says what the next commit did.
//
// The evaluator and the annotator may know B — that is the whole point of
// having it. The invariant is about which side of the worktree boundary it
// sits on, and it is checked rather than asserted, because "we were careful"
// is exactly the claim that fails silently.

// LeakKind names a way the answer can reach the solver.
type LeakKind string

const (
	// LeakGoldMaterial: a patch, a diff, a copy of the fixed file.
	LeakGoldMaterial LeakKind = "gold_material"
	// LeakAnnotation: an evaluator label inside the worktree.
	LeakAnnotation LeakKind = "annotation"
	// LeakHistory: version-control history that reveals the change.
	LeakHistory LeakKind = "history"
	// LeakAcceptance: the hidden test present while the task is being solved.
	LeakAcceptance LeakKind = "acceptance"
)

// ScanFixtureForLeaks reports gold or evaluator material inside the fixture
// the solver is handed.
//
// It scans the directory on disk rather than trusting the task definition,
// because the failure this catches is somebody putting a file somewhere, not
// somebody declaring it.
func ScanFixtureForLeaks(taskDir string, t Task) []string {
	var found []string
	if strings.TrimSpace(t.Fixture) == "" {
		// Without a declared fixture, FixturePath resolves to the task
		// directory — or, for a task built in memory, to the working
		// directory. Walking either would scan somebody's whole repository
		// and report its .git as a leak. A task with no fixture has nothing
		// to scan and says so.
		return nil
	}
	fixture := t.FixturePath()
	if info, err := os.Stat(fixture); err != nil || !info.IsDir() {
		return []string{fmt.Sprintf("%s (the fixture directory is missing)", t.Fixture)}
	}
	note := func(kind LeakKind, path, why string) {
		found = append(found, fmt.Sprintf("%s (%s: %s)", path, kind, why))
	}

	// The acceptance files must not be in the fixture. They are written into
	// the worktree copy after the run, and a solution that could read them
	// would be graded on a test it had seen.
	for name := range t.Acceptance.Files {
		if _, err := os.Stat(filepath.Join(fixture, name)); err == nil {
			note(LeakAcceptance, name, "the hidden acceptance file is in the starting state")
		}
	}

	err := filepath.WalkDir(fixture, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr // an unreadable entry is not a finding
		}
		rel, relErr := filepath.Rel(fixture, path)
		if relErr != nil {
			return nil //nolint:nilerr // outside the fixture, not our concern
		}
		rel = filepath.ToSlash(rel)
		name := strings.ToLower(d.Name())

		if d.IsDir() {
			switch name {
			case ".git", ".hg", ".svn":
				// A checkout's own history is the most direct route to the
				// fix there is: `git log` in the worktree would show it, and
				// the model has a read tool.
				note(LeakHistory, rel, "version-control history is inside the fixture")
				return filepath.SkipDir
			case "node_modules", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		switch {
		case strings.Contains(name, ".annotation."):
			note(LeakAnnotation, rel, "an evaluator label is inside the fixture")
		case strings.HasSuffix(name, ".patch"), strings.HasSuffix(name, ".diff"):
			note(LeakGoldMaterial, rel, "a patch file is inside the fixture")
		case suggestsAnswer(name):
			note(LeakGoldMaterial, rel, "the name suggests the answer")
		}
		return nil
	})
	if err != nil {
		found = append(found, fmt.Sprintf("the fixture could not be scanned: %v", err))
	}

	// And the annotation directory must not be inside it.
	if fixtureRelative(fixture, filepath.Join(taskDir, AnnotationDir)) {
		found = append(found, fmt.Sprintf("%s (%s: the annotation directory is inside the fixture)",
			AnnotationDir, LeakAnnotation))
	}
	sort.Strings(found)
	return found
}

// WorkspaceIntegrity is the check applied to a prepared execution workspace.
type WorkspaceIntegrity struct {
	TaskID string `json:"task_id"`
	// BaseDeclared is what the task says the starting state is;
	// BaseObserved is what the workspace actually contains. Equal, or the
	// solver is not working on the problem the task describes.
	BaseDeclared string `json:"base_declared,omitempty"`
	BaseObserved string `json:"base_observed,omitempty"`
	// Verified says the two were compared and matched. False with no
	// Problems means it could not be established, which for a benchmark is
	// the same as failing.
	Verified bool     `json:"verified"`
	Problems []string `json:"problems,omitempty"`
}

// OK reports an integrity check that found nothing wrong.
func (w WorkspaceIntegrity) OK() bool { return w.Verified && len(w.Problems) == 0 }

// CheckWorkspace verifies that a prepared workspace is the declared base
// state and holds no gold material.
//
// A task with no declared base revision cannot be verified. That is reported
// as unverified rather than passed: for a benchmark, "we could not establish
// that the solver started where it was supposed to" is not a pass.
func CheckWorkspace(ctx context.Context, taskDir string, t Task, workspace string) WorkspaceIntegrity {
	w := WorkspaceIntegrity{TaskID: t.ID, BaseDeclared: t.Origin.BaseRevision}

	for _, leak := range ScanFixtureForLeaks(taskDir, t) {
		w.Problems = append(w.Problems, "fixture: "+leak)
	}

	// The gold revision must not be reachable from the workspace. The
	// strongest form of that is that the workspace is not a checkout of the
	// project at all: the harness copies a fixture rather than cloning.
	if info, err := os.Stat(filepath.Join(workspace, ".git")); err == nil && info.IsDir() {
		// The harness does init a repository in the task copy, so the
		// presence of .git is expected. What must not be there is the
		// project's real history.
		if reachable(ctx, workspace, t.Origin.GoldRevision) {
			w.Problems = append(w.Problems,
				"the gold revision is reachable from the execution workspace; `git show` "+
					"would hand the model the answer")
		}
		if n := commitCount(ctx, workspace); n > 1 {
			w.Problems = append(w.Problems, fmt.Sprintf(
				"the execution workspace has %d commits; the harness creates one base "+
					"commit, so this history came from somewhere else and may describe "+
					"the change", n))
		}
	}

	switch t.Origin.BaseRevision {
	case "":
		if !t.Origin.Synthetic {
			w.Problems = append(w.Problems,
				"no base revision is declared, so the starting state cannot be shown to be "+
					"the pre-fix one")
		} else {
			// A synthetic fixture is its own base: there is nothing to
			// reconstruct it from and nothing it could silently differ from.
			w.Verified = true
			w.BaseObserved = "synthetic"
		}
	default:
		w.BaseObserved = FixtureDigest(t.FixturePath())
		// The declared revision is an identifier in another repository, which
		// this harness cannot resolve. What it can do is pin the content: if
		// the task records the digest of the base state, a fixture edited
		// afterwards stops matching.
		if t.Origin.BaseDigest() == "" {
			w.Problems = append(w.Problems,
				"the task declares a base revision but records no content digest for it, so "+
					"an edited fixture would go unnoticed. Add `base_digest` from "+
					"`bcode eval integrity --print-digest`.")
		} else if t.Origin.BaseDigest() != w.BaseObserved {
			w.Problems = append(w.Problems, fmt.Sprintf(
				"the fixture does not match the declared base state (%s, expected %s)",
				shortRev(w.BaseObserved), shortRev(t.Origin.BaseDigest())))
		} else {
			w.Verified = true
		}
	}
	return w
}

// reachable reports whether a revision exists in a checkout.
func reachable(ctx context.Context, dir, rev string) bool {
	if rev == "" {
		return false
	}
	cmd := exec.CommandContext(ctx, "git", "cat-file", "-e", rev+"^{commit}") //nolint:gosec // a recorded revision
	cmd.Dir = dir
	return cmd.Run() == nil
}

func commitCount(ctx context.Context, dir string) int {
	cmd := exec.CommandContext(ctx, "git", "rev-list", "--count", "HEAD") //nolint:gosec // fixed arguments
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &n); err != nil {
		return 0
	}
	return n
}

// FixtureDigest is the content identity of a starting state.
//
// Every regular file's path and bytes, in sorted order. It deliberately
// ignores modification times and permissions: those differ between a fresh
// clone and a copy, and a digest that changed because a file was touched
// would be a check nobody could keep green.
func FixtureDigest(dir string) string {
	h := sha256.New()
	var paths []string
	contents := map[string][]byte{}

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".hg", ".svn", "node_modules", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil //nolint:nilerr // outside the fixture
		}
		body, err := os.ReadFile(path) //nolint:gosec // inside the fixture
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		paths = append(paths, rel)
		contents[rel] = body
		return nil
	})
	if err != nil {
		return ""
	}
	sort.Strings(paths)
	for _, p := range paths {
		fmt.Fprintf(h, "%d:%s", len(p), p)
		fmt.Fprintf(h, "%d:", len(contents[p]))
		h.Write(contents[p])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// answerWords are the name fragments that suggest a file holds the fix.
var answerWords = []string{"gold", "solution", "expected_fix"}

// suggestsAnswer reports whether a filename names the answer.
//
// The word has to stand on its own. Matching "gold" anywhere in the name
// refused a third-party tree for containing goldmark, a markdown renderer,
// which is a fact about English rather than about the fixture. A separator
// or the end of the name on both sides is what distinguishes gold_answer.go
// and answer.gold from goldmark and golden-gate.
func suggestsAnswer(name string) bool {
	boundary := func(b byte) bool {
		return b == '.' || b == '_' || b == '-' || b == '/' || b == ' '
	}
	for _, w := range answerWords {
		for i := 0; ; {
			j := strings.Index(name[i:], w)
			if j < 0 {
				break
			}
			start, end := i+j, i+j+len(w)
			beforeOK := start == 0 || boundary(name[start-1])
			afterOK := end == len(name) || boundary(name[end])
			if beforeOK && afterOK {
				return true
			}
			i = start + 1
		}
	}
	return false
}
