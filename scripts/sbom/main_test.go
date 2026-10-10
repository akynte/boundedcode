package main

import (
	"testing"
	"time"
)

func TestCreationTimeFromSourceDateEpoch(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "1759937252")
	if got, want := creationTime(), time.Unix(1759937252, 0).UTC(); !got.Equal(want) {
		t.Fatalf("creationTime() = %v, want %v", got, want)
	}
	t.Setenv("SOURCE_DATE_EPOCH", "")
	if got := creationTime(); time.Since(got) > time.Minute {
		t.Fatalf("creationTime() without SOURCE_DATE_EPOCH = %v, want about now", got)
	}
}
