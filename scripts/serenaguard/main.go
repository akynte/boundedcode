// Command serenaguard fails CI when the Serena pin drifts or changes without
// the license review recorded alongside it (docs/licensing/policy.md,
// ADR-0008). Serena v1.7.0 is the last MIT-licensed release; Serena v2/main
// is GPL-3.0-or-later. Every upgrade must be a deliberate change of all of:
//
//   - internal/repointel/serena constants (version, commit, LICENSE and wheel hashes)
//   - internal/config.SerenaVersion
//   - configs/serena/pyproject.toml and uv.lock (exact version and wheel hash)
//   - LICENSES/upstream/serena.txt
//   - the license matrix, THIRD_PARTY_NOTICES.md, upstream-components.md and the ADR
//   - a `bench intel` report measured with that version (compatibility evidence)
//
// It also rejects unpinned installs (git+…/serena without a ref, serena-agent
// without ==) and dependency updaters that could bump Serena automatically.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/repointel/serena"
)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	problems := check(root)
	if len(problems) > 0 {
		fmt.Fprintf(os.Stderr, "serena pin guard: %d problem(s). Serena upgrades are manual and need a license review "+
			"(docs/licensing/policy.md, ADR-0008):\n", len(problems))
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "  - "+p)
		}
		os.Exit(1)
	}
	fmt.Printf("serena pin guard: ok (Serena %s @ %.8s, %s)\n", serena.RequiredVersion, serena.PinnedCommit, serena.ExpectedLicense)
}

func check(root string) []string {
	var problems []string
	bad := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			bad("%s: %v", rel, err)
		}
		return string(b)
	}
	ver, commit, short := serena.RequiredVersion, serena.PinnedCommit, serena.PinnedCommit[:8]

	// 1. Pins agree.
	if config.SerenaVersion != ver {
		bad("internal/config.SerenaVersion %q != serena.RequiredVersion %q", config.SerenaVersion, ver)
	}
	if serena.ExpectedLicense != "MIT" {
		bad("serena.ExpectedLicense is %q: a non-MIT Serena needs an architectural and legal review before integration", serena.ExpectedLicense)
	}
	py := read("configs/serena/pyproject.toml")
	specs := regexp.MustCompile(`"serena-agent\s*([^"]*)"`).FindAllStringSubmatch(py, -1)
	if len(specs) != 1 || strings.TrimSpace(specs[0][1]) != "=="+ver {
		bad("configs/serena/pyproject.toml must depend on exactly \"serena-agent==%s\" (found %v)", ver, specs)
	}
	lock := read("configs/serena/uv.lock")
	if !regexp.MustCompile(`(?m)^name = "serena-agent"\nversion = "` + regexp.QuoteMeta(ver) + `"\nsource = \{ registry = "https://pypi.org/simple" \}`).MatchString(lock) {
		bad("configs/serena/uv.lock does not lock serena-agent %s from PyPI (run `uv lock` in configs/serena)", ver)
	}
	if !strings.Contains(lock, "serena_agent-"+ver+"-py3-none-any.whl\", hash = \"sha256:"+serena.WheelSHA256+"\"") {
		bad("configs/serena/uv.lock wheel hash != serena.WheelSHA256 (%.12s…)", serena.WheelSHA256)
	}
	if lic := read("LICENSES/upstream/serena.txt"); sha(lic) != serena.LicenseSHA256 {
		bad("LICENSES/upstream/serena.txt sha256 %.12s… != serena.LicenseSHA256 %.12s… (copy LICENSE from tag v%s)", sha(lic), serena.LicenseSHA256, ver)
	}

	// 2. The review is recorded in the documents.
	type doc struct {
		path  string
		needs []string
	}
	adrs, _ := filepath.Glob(filepath.Join(root, "docs/architecture/adr/*serena*.md"))
	docs := []doc{
		{"docs/licensing/upstream-license-matrix.md", []string{"Serena", "v" + ver, short, "MIT", serena.LicenseSHA256[:8]}},
		{"THIRD_PARTY_NOTICES.md", []string{"Serena", "v" + ver, short, "MIT"}},
		{"docs/architecture/upstream-components.md", []string{"Serena", "v" + ver, short}},
	}
	if len(adrs) == 0 {
		bad("no ADR for the Serena integration in docs/architecture/adr (*serena*.md)")
	}
	for _, a := range adrs {
		rel, _ := filepath.Rel(root, a)
		docs = append(docs, doc{rel, []string{"v" + ver, short}})
	}
	for _, d := range docs {
		if !lineWithAll(read(d.path), d.needs) {
			bad("%s has no line naming %s", d.path, strings.Join(d.needs, " + "))
		}
	}

	// 3. Compatibility evidence: an overlap study measured with this version.
	reports, _ := filepath.Glob(filepath.Join(root, "benchmarks/reports/*-intel-overlap.json"))
	evidence := false
	for _, r := range reports {
		var reps []struct {
			SerenaVersion string `json:"serena_version"`
			SerenaCommit  string `json:"serena_commit"`
		}
		b, _ := os.ReadFile(r)
		if json.Unmarshal(b, &reps) != nil || len(reps) == 0 {
			continue
		}
		all := true
		for _, x := range reps {
			all = all && x.SerenaVersion == ver && x.SerenaCommit == commit
		}
		evidence = evidence || all
	}
	if !evidence {
		bad("no benchmarks/reports/*-intel-overlap.json measured with Serena %s @ %s (run `boundedcode bench intel`)", ver, short)
	}

	// 4. No unpinned or floating installs anywhere in tracked files.
	floatingGit := regexp.MustCompile(`git\+https?://github\.com/oraios/serena(\.git)?(["'\s]|$)`)
	looseSpec := regexp.MustCompile(`serena-agent\s*(>=|~=|>|<|!=|\^|@\s*(latest|main))|serena-agent\s*$|serena-agent["'](\s|,|\])`)
	for _, f := range trackedFiles(root) {
		if !installFile(f) {
			continue // prose, code and lock files describe or pin; they do not install
		}
		b, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			continue
		}
		s := string(b)
		if floatingGit.MatchString(s) {
			bad("%s installs Serena from git without a pinned ref", f)
		}
		if looseSpec.MatchString(s) {
			bad("%s references serena-agent without an exact == pin", f)
		}
	}

	// 5. Automatic updaters must not touch Serena.
	for _, u := range []string{".github/dependabot.yml", ".github/dependabot.yaml", "renovate.json", "renovate.json5", ".github/renovate.json", ".renovaterc", ".renovaterc.json"} {
		b, err := os.ReadFile(filepath.Join(root, u))
		if err != nil {
			continue
		}
		s := string(b)
		if (strings.Contains(s, "configs/serena") || strings.Contains(s, "pip") || strings.Contains(s, "uv")) && !strings.Contains(s, "serena-agent") {
			bad("%s may update Python dependencies; add an ignore rule for serena-agent (upgrades need a license review)", u)
		}
	}
	return problems
}

// installFile reports whether a file can install Python packages.
func installFile(f string) bool {
	base := strings.ToLower(filepath.Base(f))
	switch {
	case base == "pyproject.toml", base == "setup.py", base == "setup.cfg", base == "pipfile",
		strings.HasPrefix(base, "requirements") && strings.HasSuffix(base, ".txt"),
		strings.HasPrefix(base, "dockerfile"), strings.HasSuffix(base, ".dockerfile"),
		strings.HasSuffix(base, ".sh"), strings.HasSuffix(base, ".yml"), strings.HasSuffix(base, ".yaml"):
		return true
	}
	return false
}

func lineWithAll(text string, needs []string) bool {
	for line := range strings.SplitSeq(text, "\n") {
		ok := true
		for _, n := range needs {
			ok = ok && strings.Contains(line, n)
		}
		if ok {
			return true
		}
	}
	return false
}

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func trackedFiles(root string) []string {
	out, err := exec.CommandContext(context.Background(), "git", "-C", root, "ls-files", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		return nil
	}
	return strings.Fields(string(out))
}
