package benchmark

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestTrailStudyScoresCommits(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git")
	}
	dir, git, write := testRepo(t)
	write("p/a.go", "package p\n\nfunc ParseAlpha() int { return 0 }\n")
	write("p/b.go", "package p\n\nfunc Beta() int { return 0 }\n")
	write("p/c.go", "package p\n\nfunc Gamma() int { return 0 }\n")
	git("add", "-A")
	git("commit", "-qm", "initial")
	for i, v := range []string{"1", "2", "3"} {
		write("p/a.go", "package p\n\nfunc ParseAlpha() int { return "+v+" }\n")
		write("p/b.go", "package p\n\nfunc Beta() int { return "+v+" }\n")
		if i == 0 {
			write("p/a_test.go", "package p\n") // tests are not counted
		}
		git("add", "-A")
		git("commit", "-qm", "tweak "+v)
	}
	write("p/c.go", "package p\n\nfunc Gamma() int { return 1 }\n")
	git("add", "-A")
	git("commit", "-qm", "gamma")
	write("p/a.go", "package p\n\nfunc ParseAlpha() int { return 9 }\n")
	write("p/b.go", "package p\n\nfunc Beta() int { return 9 }\n")
	write("p/d.go", "package p\n")
	git("add", "-A")
	git("commit", "-qm", "p: speed up ParseAlpha")

	rep, err := (&TrailStudy{Repo: dir, Commits: 10}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Commits) != 5 || rep.Skipped != 1 {
		t.Fatalf("commits %d skipped %d", len(rep.Commits), rep.Skipped)
	}
	last := rep.Commits[0]
	if last.Changed != 2 || last.Added != 1 {
		t.Fatalf("changed %d added %d", last.Changed, last.Added)
	}
	if last.Named != (Hits{1, 1}) || last.WithCo != (Hits{3, 2}) || last.Hot != (Hits{3, 2}) {
		t.Fatalf("request time: %+v %+v %+v", last.Named, last.WithCo, last.Hot)
	}
	if e := last.EditRule[1]; e.Fired != 2 || e.Suggested != 2 || e.Right != 2 || e.Examples[0] != "p/a.go -> p/b.go" {
		t.Fatalf("edit time: %+v", e)
	}
	if e := last.EditRule[2]; e.Fired != 0 { // support 4 < 5
		t.Fatalf("strict rule: %+v", e)
	}
	s := rep.Summary
	if s.MultiFile != 4 || s.Edit[0].Coverage != 0.75 || s.Edit[1].Coverage != 0.5 || s.Edit[0].Precision != 1 || s.Edit[0].Nag != 0 {
		t.Fatalf("summary: %+v", s)
	}
	var md bytes.Buffer
	WriteTrailMarkdown(&md, []*TrailReport{rep})
	if !strings.Contains(md.String(), "| 3 | 0.5 | 4 | 100% | 50% (of 4) | 0% |") {
		t.Fatalf("markdown:\n%s", md.String())
	}
}
