package main

import "testing"

func TestMissingFromNotices(t *testing.T) {
	mods := []mod{
		{path: "golang.org/x/sys", version: "v0.48.0"},
		{path: "golang.org/x/term", version: "v0.46.0"},
		{path: "github.com/remyoudompheng/bigfft", version: "v0.0.0-20230129092748-24d4a6f8daec"},
	}
	text := "| golang.org/x/sys | v0.48.0 | BSD-3-Clause |\n" +
		"| golang.org/x/termite | v0.46.0 | MIT |\n" + // a longer path does not count
		"| golang.org/x/sys/unix | v0.46.0 |\n" + // nor does a package path
		"| github.com/remyoudompheng/bigfft | v0.0.0-20230129 | BSD-3-Clause |\n"
	got := missingFromNotices(text, mods)
	if len(got) != 1 || got[0].path != "golang.org/x/term" {
		t.Fatalf("missing = %v, want only golang.org/x/term", got)
	}
	if got := missingFromNotices(text+"| golang.org/x/term | v0.46.0 | BSD-3-Clause |\n", mods); len(got) != 0 {
		t.Fatalf("missing = %v, want none", got)
	}
	if got := missingFromNotices("| golang.org/x/sys | v0.47.0 |\n", mods[:1]); len(got) != 1 {
		t.Fatal("a stale version must count as missing")
	}
}
