package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureWith(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func historicalTask(t *testing.T, fixture string) Task {
	t.Helper()
	return Task{
		ID: "hist-1", Objective: "make the limit configurable", Fixture: fixture,
		Acceptance: Acceptance{Argv: []string{"go", "test", "./..."}, TimeoutSeconds: 60,
			Files: map[string]string{"hidden_test.go": "package x"}},
		Origin: TaskOrigin{
			BaseRevision: "aaaa1111", GoldRevision: "bbbb2222", Derived: true,
			Repository: "example/project", Date: "2026-01-02",
		},
	}
}

// Every route by which the fix can reach the solver.
func TestFixtureLeaksAreDetected(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"a patch file":          {"main.go": "package main", "fix.patch": "diff"},
		"a diff file":           {"main.go": "package main", "changes.diff": "diff"},
		"a file named gold":     {"main.go": "package main", "gold_answer.go": "x"},
		"a file named solution": {"main.go": "package main", "solution.md": "x"},
		"an annotation":         {"main.go": "package main", "t.annotation.yaml": "x"},
		"the hidden test":       {"main.go": "package main", "hidden_test.go": "x"},
	} {
		t.Run(name, func(t *testing.T) {
			task := historicalTask(t, fixtureWith(t, files))
			leaks := ScanFixtureForLeaks(t.TempDir(), task)
			if len(leaks) == 0 {
				t.Fatalf("no leak reported for %s", name)
			}
		})
	}
}

// Version-control history in the fixture is the most direct route there is:
// the model has a read tool and `git log` would describe the change.
func TestHistoryInsideTheFixtureIsALeak(t *testing.T) {
	dir := fixtureWith(t, map[string]string{"main.go": "package main"})
	if err := os.MkdirAll(filepath.Join(dir, ".git", "refs"), 0o750); err != nil {
		t.Fatal(err)
	}
	leaks := ScanFixtureForLeaks(t.TempDir(), historicalTask(t, dir))
	if len(leaks) == 0 {
		t.Fatal("a .git directory inside the fixture was not reported")
	}
	if !strings.Contains(strings.Join(leaks, " "), "history") {
		t.Fatalf("leaks = %v", leaks)
	}
}

func TestCleanFixtureHasNoLeaks(t *testing.T) {
	dir := fixtureWith(t, map[string]string{
		"main.go": "package main", "internal/limit.go": "package internal",
	})
	if leaks := ScanFixtureForLeaks(t.TempDir(), historicalTask(t, dir)); len(leaks) != 0 {
		t.Fatalf("a clean fixture reported %v", leaks)
	}
}

// A task with no fixture must not cause the whole working directory to be
// scanned — which it did, reporting the repository's own .git as a leak.
func TestTaskWithoutAFixtureScansNothing(t *testing.T) {
	if leaks := ScanFixtureForLeaks(t.TempDir(), Task{ID: "x"}); leaks != nil {
		t.Fatalf("scanning a task with no fixture found %v", leaks)
	}
}

// "We could not establish that the solver started where it was supposed to"
// is not a pass.
func TestUnpinnedBaseIsNotVerified(t *testing.T) {
	dir := fixtureWith(t, map[string]string{"main.go": "package main"})
	task := historicalTask(t, dir)
	res := CheckWorkspace(context.Background(), t.TempDir(), task, dir)
	if res.Verified {
		t.Fatal("a fixture with no recorded base digest was reported verified")
	}
	if !strings.Contains(strings.Join(res.Problems, " "), "base_digest") {
		t.Fatalf("the problem does not say how to fix it: %v", res.Problems)
	}
}

// With the digest recorded the check passes, and an edited fixture fails it.
func TestPinnedBaseDetectsAnEditedFixture(t *testing.T) {
	dir := fixtureWith(t, map[string]string{"main.go": "package main\n"})
	task := historicalTask(t, dir)
	task.Origin.Digest = FixtureDigest(dir)

	if res := CheckWorkspace(context.Background(), t.TempDir(), task, dir); !res.OK() {
		t.Fatalf("a pinned, matching fixture failed: %v", res.Problems)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main // edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res := CheckWorkspace(context.Background(), t.TempDir(), task, dir)
	if res.OK() {
		t.Fatal("an edited fixture still matched its declared base")
	}
	if !strings.Contains(strings.Join(res.Problems, " "), "does not match") {
		t.Fatalf("problems = %v", res.Problems)
	}
}

// A synthetic fixture is its own base: there is nothing to reconstruct it
// from and nothing it could silently differ from.
func TestSyntheticFixtureNeedsNoRevision(t *testing.T) {
	dir := fixtureWith(t, map[string]string{"main.go": "package main"})
	task := Task{ID: "syn-1", Fixture: dir, Origin: TaskOrigin{Synthetic: true}}
	if res := CheckWorkspace(context.Background(), t.TempDir(), task, dir); !res.OK() {
		t.Fatalf("a synthetic fixture failed: %v", res.Problems)
	}
}

// The digest must ignore what a copy changes and notice what an edit does.
func TestFixtureDigestIgnoresCopyingAndNoticesEdits(t *testing.T) {
	files := map[string]string{"a.go": "package a\n", "sub/b.go": "package b\n"}
	first := FixtureDigest(fixtureWith(t, files))
	second := FixtureDigest(fixtureWith(t, files))
	if first != second {
		t.Fatal("two identical fixtures digested differently; a copy would fail the check")
	}
	edited := FixtureDigest(fixtureWith(t, map[string]string{
		"a.go": "package a // changed\n", "sub/b.go": "package b\n",
	}))
	if edited == first {
		t.Fatal("an edited fixture digested the same")
	}
	// A file added is a different starting state too.
	added := FixtureDigest(fixtureWith(t, map[string]string{
		"a.go": "package a\n", "sub/b.go": "package b\n", "c.go": "package c\n",
	}))
	if added == first {
		t.Fatal("an added file did not change the digest")
	}
}

// The answer-name rule matches a word, not a substring. It refused a
// third-party tree for containing goldmark — a markdown renderer — which
// says nothing about whether the fixture holds the fix.
func TestAnswerNamesMatchWordsNotSubstrings(t *testing.T) {
	for name, want := range map[string]bool{
		"gold_answer.go":                true,
		"answer.gold":                   true,
		"gold":                          true,
		"the-solution-here.py":          true,
		"expected_fix.diff":             true,
		"goldmark_integration_test.go":  false,
		"golden_gate.go":                false,
		"marigold.txt":                  false,
		"solutions_manager_internal.go": false,
		"main.go":                       false,
	} {
		if got := suggestsAnswer(name); got != want {
			t.Errorf("suggestsAnswer(%q) = %v, want %v", name, got, want)
		}
	}
}
