package benchmark

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akynte/boundedcode/internal/gitops"
)

func TestGitHistorySourceStopsAtTheCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git")
	}
	t.Setenv("BC_BENCH_SOURCES", t.TempDir())
	ctx := context.Background()
	upstream, git, write := testRepo(t)
	for _, v := range []string{"one", "two", "three"} {
		write("f.txt", v+"\n")
		git("add", "-A")
		git("commit", "-qm", v)
	}
	commit, err := gitops.Run(ctx, upstream, "rev-parse", "HEAD~1")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 { // the second materialization reuses the cache
		repos, err := materialize(ctx, "", map[string]string{"r": "githistory:" + upstream + "@" + commit}, filepath.Join(t.TempDir(), "repos"), nil)
		if err != nil {
			t.Fatal(err)
		}
		dir := repos["r"]
		log, err := gitops.Run(ctx, dir, "log", "--format=%s", "--all")
		if err != nil {
			t.Fatal(err)
		}
		if log != "base\ntwo\none" {
			t.Fatalf("history: %q", log)
		}
		if b, _ := os.ReadFile(filepath.Join(dir, "f.txt")); string(b) != "two\n" {
			t.Fatalf("content %q", b)
		}
		if remotes, _ := gitops.Run(ctx, dir, "remote"); strings.TrimSpace(remotes) != "" {
			t.Fatalf("remotes: %q", remotes)
		}
	}
}
