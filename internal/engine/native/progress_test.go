package native

import "testing"

// The same question answered the same way is not information, however many
// times it is asked.
func TestIdenticalCallsAreCountedAsRepeats(t *testing.T) {
	p := newProgress([]string{"read_file", "search_code", "edit_file"}, nil)
	for i := 1; i <= 3; i++ {
		v, times := p.observe("read_file", `{"path":"a.go"}`, "package a")
		if i == 1 && v != learned {
			t.Fatalf("the first call was not counted as new")
		}
		if i > 1 && v != repeated {
			t.Errorf("call %d was not counted as a repeat", i)
		}
		if times != i {
			t.Errorf("call %d reported %d occurrences", i, times)
		}
	}
	// The third identical call is a correction, not an ending. Terminating
	// here threw away every piece of evidence the loop had already found.
	if stuck, why := p.stuck(); stuck {
		t.Fatalf("the attempt ended before the model was told to stop: %s", why)
	}
	recover, note := p.recoverable("read_file", `{"path":"a.go"}`)
	if !recover {
		t.Fatal("the repeat limit produced no correction")
	}
	for _, want := range []string{"read_file", "[supervisor]", "Do not repeat it", "search_code"} {
		if !contains(note, want) {
			t.Errorf("the correction does not contain %q: %s", want, note)
		}
	}
	// Repeating it after being told ends the attempt, and the reason says so.
	p.observe("read_file", `{"path":"a.go"}`, "package a")
	stuck, why := p.stuck()
	if !stuck {
		t.Fatal("repeating a corrected call did not end the loop")
	}
	if !contains(why, "read_file") || !contains(why, "explicitly") {
		t.Errorf("the reason must say the correction was ignored, got %q", why)
	}
}

// Arguments that differ only in key order or spacing are the same call. A
// model that reformats its JSON is not making progress.
func TestArgumentOrderDoesNotDisguiseARepeat(t *testing.T) {
	p := newProgress([]string{"read_file", "search_code", "edit_file"}, nil)
	p.observe("edit_file", `{"path":"a.go","old":"x","new":"y"}`, "ok")
	v, times := p.observe("edit_file", `{"new":"y","old":"x","path":"a.go"}`, "ok")
	if v != repeated || times != 2 {
		t.Errorf("reordered arguments were treated as a new call (v=%v times=%d)", v, times)
	}
}

// The same call answered differently is real information: the file changed, or
// the build now fails for another reason. Counting it as a repeat would punish
// a model for checking its own work.
func TestTheSameCallWithANewAnswerIsProgress(t *testing.T) {
	p := newProgress([]string{"read_file", "search_code", "edit_file"}, nil)
	p.observe("run_verification", `{}`, "1 failure")
	v, _ := p.observe("run_verification", `{}`, "passing")
	if v != learned {
		t.Error("a changed answer was not counted as progress")
	}
	if stuck, _ := p.stuck(); stuck {
		t.Error("a loop that is learning was stopped")
	}
}

// A stale step is survivable; three in a row is a strategy that is not working.
func TestConsecutiveStaleStepsEndTheLoop(t *testing.T) {
	p := newProgress([]string{"read_file", "search_code", "edit_file"}, nil)
	p.observe("list_files", `{"path":"."}`, "a.go")
	for range staleLimit - 1 {
		p.endOfStep(false)
		if stuck, _ := p.stuck(); stuck {
			t.Fatal("the loop was stopped before the stale limit")
		}
	}
	p.endOfStep(false)
	stuck, why := p.stuck()
	if !stuck {
		t.Fatal("consecutive stale steps did not end the loop")
	}
	if !contains(why, "learned nothing new") {
		t.Errorf("the reason should say what happened, got %q", why)
	}
}

// One good step clears the stale run: a model that reads two files it has
// already seen and then edits something is working, not stuck.
func TestProgressResetsTheStaleCount(t *testing.T) {
	p := newProgress([]string{"read_file", "search_code", "edit_file"}, nil)
	p.endOfStep(false)
	p.endOfStep(false)
	p.endOfStep(true)
	p.endOfStep(false)
	if stuck, why := p.stuck(); stuck {
		t.Errorf("a loop that made progress was stopped: %s", why)
	}
}

// The note is the supervisor speaking and has to be actionable, not a counter.
func TestTheRepeatNoteTellsTheModelWhatToDo(t *testing.T) {
	note := repeatNote("search_code", 2)
	for _, want := range []string{"search_code", "[supervisor]", "already have"} {
		if !contains(note, want) {
			t.Errorf("the note does not contain %q: %s", want, note)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

// Canonically-equal arguments must reach the guard, and be counted as the
// serialization difference they are rather than as a model looping.
func TestCanonicalEquivalenceIsCountedSeparately(t *testing.T) {
	p := newProgress([]string{"search_code"}, nil)
	p.observe("search_code", `{"query":"foo"}`, "no matches")
	p.observe("search_code", `{"query": "foo"}`, "no matches")
	exact, canonical, _, _ := p.Counters()
	if exact != 1 {
		t.Errorf("exact repetitions = %d, want 1", exact)
	}
	if canonical == 0 {
		t.Error("a reformatted argument list was not recorded as canonically equivalent")
	}
}

// A different, useful call must not be blocked by the guard.
func TestDifferentCallsAreNotBlocked(t *testing.T) {
	p := newProgress([]string{"read_file"}, nil)
	for _, path := range []string{"a.py", "b.py", "c.py", "d.py"} {
		if v, _ := p.observe("read_file", `{"path":"`+path+`"}`, "contents of "+path); v != learned {
			t.Errorf("reading %s was counted as a repeat", path)
		}
		p.endOfStep(true)
	}
	if stuck, why := p.stuck(); stuck {
		t.Errorf("a loop making progress was stopped: %s", why)
	}
	if recover, _ := p.recoverable("read_file", `{"path":"a.py"}`); recover {
		t.Error("a call made once triggered a repetition correction")
	}
}

// After the correction the model may do something else and carry on.
func TestAnAlternativeActionContinuesAfterRecovery(t *testing.T) {
	p := newProgress([]string{"read_file", "search_code"}, nil)
	for range repeatLimit {
		p.observe("search_code", `{"query":"def _get_prefetch"}`, "3 matches")
	}
	recover, _ := p.recoverable("search_code", `{"query":"def _get_prefetch"}`)
	if !recover {
		t.Fatal("the repeat limit produced no correction")
	}
	// Something different, and the attempt is alive.
	if v, _ := p.observe("read_file", `{"path":"related_descriptors.py"}`, "class Prefetch"); v != learned {
		t.Error("the alternative action was not counted as progress")
	}
	p.endOfStep(true)
	if stuck, why := p.stuck(); stuck {
		t.Fatalf("the attempt ended after a successful recovery: %s", why)
	}
	_, _, recoveries, terminated := p.Counters()
	if recoveries != 1 || terminated != 0 {
		t.Errorf("counters wrong: recoveries=%d terminated=%d", recoveries, terminated)
	}
}

// The correction offers the tools this engine has, not a fixed list.
func TestRecoveryNoteNamesTheEnginesOwnTools(t *testing.T) {
	p := newProgress([]string{"grep_repo", "apply_patch"}, nil)
	note := p.recoveryNote("grep_repo", 3)
	for _, want := range []string{"grep_repo", "apply_patch"} {
		if !contains(note, want) {
			t.Errorf("the correction does not offer %q: %s", want, note)
		}
	}
	if contains(note, "read_file") {
		t.Error("the correction offers a tool this engine does not have")
	}
}
