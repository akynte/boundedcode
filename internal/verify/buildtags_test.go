package verify

import (
	"context"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/akynte/boundedcode/internal/sandbox"
)

func TestCustomTagsNeeded(t *testing.T) {
	dir := t.TempDir()
	cases := map[string][]string{
		"//go:build stringlabels\n\npackage x\n":                       {"stringlabels"},
		"//go:build !stringlabels && !dedupelabels\n\npackage x\n":     nil,
		"// Copyright\n\n//go:build integration || e2e\n\npackage x\n": {"e2e", "integration"},
		"//go:build linux\n\npackage x\n":                              nil,
		"//go:build windows\n\npackage x\n":                            nil,
		"package x\n\n//go:build late is not a constraint\n":           nil,
		"//go:build go1.21 && custom\n\npackage x\n":                   {"custom"},
	}
	for src, want := range cases {
		p := filepath.Join(dir, "f.go")
		writeFiles(t, dir, map[string]string{"f.go": src})
		got := customTagsNeeded(p)
		if len(got) > 1 {
			if got[0] > got[1] {
				got[0], got[1] = got[1], got[0]
			}
		}
		if !reflect.DeepEqual(got, want) && (len(got) != 0 || len(want) != 0) {
			t.Errorf("%q: got %v want %v", src, got, want)
		}
	}
}

// TestBuildTagVariantsAreCompiled is the regression test for the Prometheus
// failure analysis: edits to tag-selected implementations did not compile
// under their tag and no check noticed.
func TestBuildTagVariantsAreCompiled(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil || testing.Short() {
		t.Skip("go toolchain required")
	}
	repo, base := commitRepo(t, map[string]string{
		"go.mod":       "module example.com/l\n\ngo 1.22\n",
		"labels.go":    "//go:build !stringlabels\n\npackage l\n\nfunc Name() string { return \"slice\" }\n",
		"labels_sl.go": "//go:build stringlabels\n\npackage l\n\nfunc Name() string { return \"string\" }\n",
	})
	e := &Engine{Sandbox: sandbox.None{}, CacheDir: t.TempDir(), Gitleaks: fakeGitleaks(t)}
	writeFiles(t, repo, map[string]string{"labels_sl.go": "//go:build stringlabels\n\npackage l\n\nfunc Name() string { var b []byte = \"x\"; return b }\n"})
	res, err := e.Run(context.Background(), RepoTarget{Name: "l", Worktree: repo, Base: base, TaskID: "t"}, Targeted)
	if err != nil {
		t.Fatal(err)
	}
	var st *StageResult
	for i := range res.Stages {
		if res.Stages[i].Name == "go-build-tags" {
			st = &res.Stages[i]
		}
	}
	if st == nil || st.Status != "fail" || res.Passed {
		t.Fatalf("broken tagged variant not caught: %+v", res.Stages)
	}
	writeFiles(t, repo, map[string]string{"labels_sl.go": "//go:build stringlabels\n\npackage l\n\nfunc Name() string { return \"string2\" }\n"})
	res, _ = e.Run(context.Background(), RepoTarget{Name: "l", Worktree: repo, Base: base, TaskID: "t"}, Targeted)
	for _, s := range res.Stages {
		if s.Name == "go-build-tags" && s.Status != "pass" {
			t.Fatalf("valid tagged variant failed: %+v", s)
		}
	}
}
