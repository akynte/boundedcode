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
