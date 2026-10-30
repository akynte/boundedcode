package lab

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/akynte/boundedcode/internal/config"
)

// A code candidate is a patch against the base revision. It may change
// only how the agent works, never what it may do or how it is judged:
//   - files under PatchAllowDirs, and none of PatchDenyFiles (security
//     policy, permissions, containment, MCP trust, hooks, content
//     provenance, post-change checks, web access, background shells);
//   - in files that mix the two, no hunk inside a ProtectedFuncs function
//     (rules, policy, approval, path resolution, secret stores, the shell
//     tool, verification fallback);
//   - no existing test changed or deleted (new tests may be added): the
//     tests are part of what judges it;
//   - nothing else: not the benchmark graders or tasks (internal/benchmark,
//     benchmarks/), not verification (internal/verify, internal/orchestrator),
//     the sandbox (internal/execbox, internal/sandbox), policy, secrets,
//     configuration and its defaults, the laboratory, the module files or CI.
//
// The checks are on the patch text and the base revision's source, before
// anything is built. A patch that passes is still only a candidate: its
// binary must also pass the security tests (SecurityTestPackages) before
// it runs any task.

// PatchAllowDirs are the directories a patch may change.
var PatchAllowDirs = []string{"internal/direct/", "internal/retrieve/", "internal/prune/", "internal/routing/", "internal/contextplan/",
	"internal/repointel/", "internal/fileedit/", "internal/lang/", "internal/ecosystem/"}

// PatchDenyFiles are files under those directories a patch may not touch.
var PatchDenyFiles = []string{"internal/direct/guard.go", "internal/direct/permissions.go", "internal/direct/contain.go", "internal/direct/mcptrust.go",
	"internal/direct/hooks.go", "internal/direct/provenance.go", "internal/direct/check.go", "internal/direct/web.go", "internal/direct/shells.go",
	"internal/direct/subagent.go", "internal/direct/mcp.go", "internal/direct/capabilities.go"}

// ProtectedFuncs are functions (by file) a hunk may not fall inside.
var ProtectedFuncs = map[string][]string{
	"internal/direct/tools.go": {"toolAllowed", "permissionArg", "refuse", "prepareTool", "permissionSpecs", "allowedAll", "resolve", "checkReadable",
		"mutationConsent", "isSafeCommand", "shellArgv", "rootRel", "bashTool"},
	"internal/direct/direct.go": {"skipCalls", "mutating", "verifyUnavailable", "verifyFallback", "Error"},
}

// SecurityTestPackages are tested in a patched worktree before its binary
// is used: they hold the policy, sandbox, guard and grading tests.
var SecurityTestPackages = []string{"./internal/direct/...", "./internal/execbox/...", "./internal/policy/...", "./internal/benchmark/..."}

// EditBudget bounds one candidate: one isolated change at a time, so that
// what helped can be told apart (a bundled change that wins hides which
// part won, and which part regressed).
type EditBudget struct {
	Settings int `yaml:"settings,omitempty" json:"settings"` // settings an arm may change (default 2)
	Files    int `yaml:"files,omitempty" json:"files"`       // files a patch may touch (default 8)
	Lines    int `yaml:"lines,omitempty" json:"lines"`       // lines a patch may add and remove (default 400)
}

func (b EditBudget) withDefaults() EditBudget {
	if b.Settings == 0 {
		b.Settings = 2
	}
	if b.Files == 0 {
		b.Files = 8
	}
	if b.Lines == 0 {
		b.Lines = 400
	}
	return b
}

// PatchInfo is what the checks found in a patch.
type PatchInfo struct {
	Files  []string `json:"files"`
	Lines  int      `json:"lines"` // added and removed
	SHA256 string   `json:"sha256"`
}

type hunk struct {
	file         string
	oldStart     int
	oldLen       int
	added        []string
	deletedFile  bool
	createdFile  bool
	renamedFrom  string
	touchesLines bool
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+\d+(?:,\d+)? @@`)

// parsePatch reads a unified diff (git diff format).
func parsePatch(b []byte) ([]hunk, error) {
	var out []hunk
	var cur *hunk
	file, from := "", ""
	created, deleted := false, false
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		l := sc.Text()
		switch {
		case strings.HasPrefix(l, "diff --git "):
			file, from, created, deleted, cur = "", "", false, false, nil
		case strings.HasPrefix(l, "new file mode"):
			created = true
		case strings.HasPrefix(l, "deleted file mode"):
			deleted = true
		case strings.HasPrefix(l, "rename from "):
			from = strings.TrimPrefix(l, "rename from ")
		case strings.HasPrefix(l, "rename to "):
			file = strings.TrimPrefix(l, "rename to ")
			out = append(out, hunk{file: file, renamedFrom: from})
		case strings.HasPrefix(l, "--- "):
			if p := strings.TrimPrefix(l, "--- "); p != "/dev/null" {
				file = strings.TrimPrefix(p, "a/")
			}
		case strings.HasPrefix(l, "+++ "):
			if p := strings.TrimPrefix(l, "+++ "); p != "/dev/null" {
				file = strings.TrimPrefix(p, "b/")
			}
		case strings.HasPrefix(l, "@@"):
			m := hunkHeader.FindStringSubmatch(l)
			if m == nil {
				return nil, fmt.Errorf("bad hunk header %q", l)
			}
			start, _ := strconv.Atoi(m[1])
			n := 1
			if m[2] != "" {
				n, _ = strconv.Atoi(m[2])
			}
			out = append(out, hunk{file: file, oldStart: start, oldLen: n, createdFile: created, deletedFile: deleted})
			cur = &out[len(out)-1]
		case cur != nil && strings.HasPrefix(l, "+"):
			cur.added = append(cur.added, l[1:])
			cur.touchesLines = true
		case cur != nil && strings.HasPrefix(l, "-"):
			cur.touchesLines = true
		}
	}
	if file == "" && len(out) == 0 {
		return nil, fmt.Errorf("not a patch: no file changes")
	}
	return out, sc.Err()
}

// CheckPatch checks a patch against the rules above. baseDir is a checkout
// of the base revision (for protected functions and existing tests);
// leaks are strings the patch may not contain (task ids, hidden files,
// expected answers: a change that names the benchmark is tuned to it).
func CheckPatch(patch []byte, baseDir string, budget EditBudget, leaks []string) (PatchInfo, error) {
	budget = budget.withDefaults()
	if strings.Contains(string(patch), "GIT binary patch") || strings.Contains(string(patch), "Binary files ") {
		return PatchInfo{}, fmt.Errorf("the patch is refused: it changes a binary file, which cannot be checked")
	}
	hunks, err := parsePatch(patch)
	if err != nil {
		return PatchInfo{}, err
	}
	sum := sha256.Sum256(patch)
	info := PatchInfo{SHA256: hex.EncodeToString(sum[:])}
	var problems []string
	files := map[string]bool{}
	for _, h := range hunks {
		for _, f := range []string{h.file, h.renamedFrom} {
			if f == "" || files[f] {
				continue
			}
			files[f] = true
			info.Files = append(info.Files, f)
			if why := pathRefusal(f); why != "" {
				problems = append(problems, f+": "+why)
			}
			if strings.HasSuffix(f, "_test.go") && !h.createdFile {
				if _, err := os.Stat(filepath.Join(baseDir, filepath.FromSlash(f))); err == nil {
					problems = append(problems, f+": an existing test may not change (add a new test file instead)")
				}
			}
		}
		info.Lines += len(h.added)
		for _, a := range h.added {
			for _, leak := range leaks {
				if leak != "" && strings.Contains(a, leak) {
					problems = append(problems, fmt.Sprintf("%s: names %q from the benchmark (a change tuned to the tasks)", h.file, leak))
				}
			}
		}
	}
	// Removed lines count against the budget too.
	for _, l := range strings.Split(string(patch), "\n") {
		if strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---") {
			info.Lines++
		}
	}
	for file, funcs := range ProtectedFuncs {
		var hs []hunk
		for _, h := range hunks {
			if h.file == file && h.touchesLines {
				hs = append(hs, h)
			}
		}
		if len(hs) == 0 {
			continue
		}
		spans, err := funcSpans(filepath.Join(baseDir, filepath.FromSlash(file)), funcs)
		if err != nil {
			problems = append(problems, file+": "+err.Error())
			continue
		}
		for _, h := range hs {
			for name, sp := range spans {
				if h.oldStart <= sp[1] && h.oldStart+max(h.oldLen, 1)-1 >= sp[0] {
					problems = append(problems, fmt.Sprintf("%s: a change in %s (lines %d–%d), a protected function", file, name, sp[0], sp[1]))
				}
			}
		}
	}
	if len(info.Files) > budget.Files {
		problems = append(problems, fmt.Sprintf("%d files changed; the edit budget is %d", len(info.Files), budget.Files))
	}
	if info.Lines > budget.Lines {
		problems = append(problems, fmt.Sprintf("%d lines changed; the edit budget is %d", info.Lines, budget.Lines))
	}
	if len(problems) > 0 {
		return info, fmt.Errorf("the patch is refused:\n- %s", strings.Join(dedupe(problems), "\n- "))
	}
	return info, nil
}

// pathRefusal says why a patch may not touch f ("" when it may).
func pathRefusal(f string) string {
	f = filepath.ToSlash(filepath.Clean(f))
	if strings.HasPrefix(f, "../") || filepath.IsAbs(f) {
		return "outside the repository"
	}
	for _, d := range PatchDenyFiles {
		if f == d {
			return "security or verification code"
		}
	}
	for _, d := range PatchAllowDirs {
		if strings.HasPrefix(f, d) {
			return ""
		}
	}
	return "not under " + strings.Join(PatchAllowDirs, ", ")
}

// funcSpans returns the line spans of the named functions (methods
// included) in a Go file.
func funcSpans(path string, names []string) (map[string][2]int, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	out := map[string][2]int{}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || !contains(names, fd.Name.Name) {
			continue
		}
		start := fd.Pos()
		if fd.Doc != nil {
			start = fd.Doc.Pos()
		}
		out[fd.Name.Name+"@"+strconv.Itoa(fset.Position(fd.Pos()).Line)] = [2]int{fset.Position(start).Line, fset.Position(fd.End()).Line}
	}
	return out, nil
}

func dedupe(s []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, x := range s {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// Fingerprint hashes everything in a configuration except the experiment
// settings: two arms with equal fingerprints differ in tunables only, so
// their security policy, permissions, sandbox, credentials references,
// verification and models are the same.
func Fingerprint(c config.Config) string {
	// Clear exactly the fields ExperimentSettings can set.
	t := &c.Direct
	t.Retrieval, t.SeedPack, t.WorkingSetTokens, t.MaxToolOutput, t.ObservationBytes = "", nil, 0, 0, 0
	t.Prune, t.HistoryNotes, t.Compaction, t.ParallelTools, t.MCPToolSearch, t.Routing, t.MaxTurns = nil, nil, "", 0, "", "", 0
	c.Inference.ReasoningHistory = ""
	c.Budgets.MaxAttempts, c.Budgets.ContextPackTokens = 0, 0
	c.Escalation.Policy, c.Escalation.FailedAttempts = "", 0
	b, _ := json.Marshal(c)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
