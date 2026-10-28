package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/direct"
)

// Streamed text shows while the model is still answering and is not
// duplicated once the whole text arrives.
func TestDirectRunShowsStreamedText(t *testing.T) {
	m := newModel(context.Background(), &fakeBackend{}, Options{Dir: t.TempDir()})
	r := &directRun{}
	r.apply(direct.Event{Kind: direct.EventTextDelta, Text: "Hello "})
	r.apply(direct.Event{Kind: direct.EventTextDelta, Text: "world"})
	joined := strings.Join(r.lines(m, 80), "\n")
	if !strings.Contains(joined, "Hello world") || strings.Contains(joined, "thinking") {
		t.Fatalf("while streaming:\n%s", joined)
	}
	r.apply(direct.Event{Kind: direct.EventText, Text: "Hello world"})
	joined = strings.Join(r.lines(m, 80), "\n")
	if strings.Count(joined, "Hello world") != 1 {
		t.Fatalf("after the whole text:\n%s", joined)
	}
}

// A stream that broke off is shown again from the start, and a long
// prompt's progress shows until the answer begins.
func TestStreamResetAndPromptProgress(t *testing.T) {
	r := &directRun{}
	r.apply(direct.Event{Kind: direct.EventPromptProgress, Args: map[string]any{"total": 9000, "cached": 8000, "processed": 300}})
	if !strings.Contains(r.progress, "300 of 1.0k") {
		t.Fatalf("progress %q", r.progress)
	}
	r.apply(direct.Event{Kind: direct.EventTextDelta, Text: "Part"})
	if r.progress != "" {
		t.Fatalf("progress kept after text: %q", r.progress)
	}
	r.apply(direct.Event{Kind: direct.EventStreamReset})
	r.apply(direct.Event{Kind: direct.EventTextDelta, Text: "Whole"})
	if r.partial != "Whole" {
		t.Fatalf("partial %q", r.partial)
	}
	if promptProgress(map[string]any{"total": 100, "cached": 100, "processed": 0}) != "" {
		t.Fatal("a fully cached prompt shows progress")
	}
}
