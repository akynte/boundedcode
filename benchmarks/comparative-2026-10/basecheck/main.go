// Command basecheck runs BoundedCode's full verification gate (the
// repository's preset stages) on the untouched base of each frozen task, in
// the evaluation's sandbox and caches, and prints the stage results as
// JSON. It measures which checks already fail on the base offline, the
// input to each task's environment-only verification config
// (deviations.md D1). It reads no agent output and no reference patch.
//
// usage: go run ./benchmarks/comparative-2026-10/basecheck -eval DIR [-image IMG] [ID ...]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/akynte/boundedcode/internal/gitops"
	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/verify"
)

type taskSpec struct {
	ID      string            `json:"id"`
	Sources map[string]string `json:"sources"`
	Repos   []string          `json:"repos"`
}

type out struct {
	ID     string               `json:"id"`
	Repo   string               `json:"repo"`
	Error  string               `json:"error,omitempty"`
	Stages []verify.StageResult `json:"stages"`
}

func main() {
	eval := flag.String("eval", filepath.Join(os.Getenv("HOME"), ".cache", "bc-comparative"), "evaluation directory")
	image := flag.String("image", "boundedcode-openhands:local", "sandbox image")
	configs := flag.String("config", "", "directory of <id>.yaml verification configs to commit into each base as .boundedcode/verification.yaml")
	preset := flag.Bool("preset", false, "print each base's preset verification config (YAML) instead of running it")
	flag.Parse()
	ctx := context.Background()
	frozen := filepath.Join(*eval, "tasks", "frozen")
	ids := flag.Args()
	if len(ids) == 0 {
		ents, _ := os.ReadDir(frozen)
		for _, e := range ents {
			ids = append(ids, strings.TrimSuffix(e.Name(), ".yaml"))
		}
	}
	gomod, _ := exec.Command("go", "env", "GOMODCACHE").Output()
	e := &verify.Engine{
		Sandbox: &sandbox.Container{Engine: "docker", Image: *image, Network: "none", Memory: "8g", CPUs: "8",
			PIDs: 4096, UID: os.Getuid(), GID: os.Getgid()},
		CacheDir: filepath.Join(*eval, "basecheck", "build"), GoModCache: strings.TrimSpace(string(gomod)),
		Packages: sandbox.HostPackageCaches(),
	}
	var all []out
	for _, id := range ids {
		o := out{ID: id}
		func() {
			b, err := os.ReadFile(filepath.Join(frozen, id+".yaml"))
			if err != nil {
				o.Error = err.Error()
				return
			}
			var t taskSpec
			if err := json.Unmarshal(b, &t); err != nil {
				o.Error = err.Error()
				return
			}
			o.Repo = t.Repos[0]
			spec := t.Sources[o.Repo]
			at := strings.LastIndex(spec, "@")
			name := strings.NewReplacer("git:https://github.com/", "", "/", "__").Replace(spec[:at])
			src := filepath.Join(os.Getenv("BC_BENCH_SOURCES"), name+"@"+spec[at+1:at+13])
			// A fresh repository at the base, as the harness materializes it.
			dir := filepath.Join(*eval, "basecheck", id)
			_ = os.RemoveAll(dir)
			_ = os.MkdirAll(filepath.Dir(dir), 0o755)
			if out, err := exec.Command("cp", "-a", src, dir).CombinedOutput(); err != nil {
				o.Error = fmt.Sprintf("copy: %v %s", err, out)
				return
			}
			if *configs != "" {
				b, err := os.ReadFile(filepath.Join(*configs, id+".yaml"))
				if err != nil {
					o.Error = err.Error()
					return
				}
				_ = os.MkdirAll(filepath.Join(dir, ".boundedcode"), 0o755)
				if err := os.WriteFile(filepath.Join(dir, verify.ConfigPath), b, 0o644); err != nil {
					o.Error = err.Error()
					return
				}
			}
			for _, a := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=b", "-c", "user.email=b@b", "commit", "-qm", "base"}} {
				if _, err := gitops.Run(ctx, dir, a...); err != nil {
					o.Error = err.Error()
					return
				}
			}
			base, _ := gitops.Run(ctx, dir, "rev-parse", "HEAD")
			if *preset {
				cfg, err := verify.LoadConfig(ctx, dir, base)
				if err != nil {
					o.Error = err.Error()
					return
				}
				b, _ := yaml.Marshal(cfg)
				fmt.Printf("# %s\n%s---\n", id, b)
				return
			}
			res, err := e.Run(ctx, verify.RepoTarget{Name: o.Repo, Worktree: dir, Base: base, TaskID: "basecheck-" + id, Source: src}, verify.Full)
			if err != nil {
				o.Error = err.Error()
			}
			o.Stages = res.Stages
		}()
		for _, s := range o.Stages {
			fmt.Fprintf(os.Stderr, "%s %-14s %-8s %s\n", id, s.Name, s.Status, firstLine(s.Output))
		}
		if o.Error != "" {
			fmt.Fprintf(os.Stderr, "%s error: %s\n", id, o.Error)
		}
		all = append(all, o)
	}
	if *preset {
		return
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", " ")
	_ = enc.Encode(all)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 110 {
		s = s[:110]
	}
	return s
}
