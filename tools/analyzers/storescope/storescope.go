// Package storescope implements the custom go/analysis analyzer required by
// design v3 §2.3:
//
//	"A linter rule (custom go/analysis analyzer) forbids `sql.Open` or file
//	 writes outside `internal/store` and `internal/artifacts`."
//
// The point is that workspace isolation is enforced by the type system and the
// build, not by discipline. If any other package can open a database or write
// a file, the guarantee that all state lives under
// `$BC_DATA/workspaces/<workspace_id>/` becomes a convention someone will
// eventually break.
//
// Exemptions are named here, in code, each with the reason it is not
// workspace state. Adding one is a reviewable change.
package storescope

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Analyzer is the go/analysis entry point.
var Analyzer = &analysis.Analyzer{
	Name: "storescope",
	Doc: "enforces design v3 §2.3: sql.Open and file writes are confined to " +
		"internal/store and internal/artifacts, so every byte of workspace state " +
		"is written through an API that is already scoped to one workspace",
	Run: run,
}

// writeOwners may write files and open databases. §2.3 names these two.
var writeOwners = []string{
	"internal/store",
	"internal/artifacts",
}

// exemptions are packages allowed to write files for a stated reason that is
// not workspace state. Each entry must say why; an unexplained entry defeats
// the rule.
var exemptions = map[string]string{
	// Writes `.bc/workspace.yaml` inside the user's repository. That file is
	// the identity pin (§2.1), not data-directory state.
	"internal/workspace": "writes the repository's identity pin file",
	// Writes bcode.yaml, providers.yaml and profiles under /data/config. Operator
	// configuration is not per-workspace state and is shared by design (§5.2).
	"internal/config": "writes operator configuration under /data/config",
	// Writes providers.yaml through the same operator-configuration path.
	"internal/llm": "writes operator provider configuration",
	// Owns the one-time installation/configuration state under BC_DATA:
	// generated operator config, credentials, downloaded models, and the
	// optional pinned runtime checkout/build. None of these paths is workspace
	// state; they are installation inputs shared by later sessions.
	"internal/setup": "writes installation data and the optional runtime build outside workspaces",
	// Owns the ephemeral OpenCode session directory and supervisor log. The
	// path is derived from the workspace store and is removed on every exit;
	// persistent OpenCode data/state remain separate.
	"internal/session": "creates and removes private per-session runtime state",
	// Owns the verifier signing key under the data root's key directory. The
	// key is deliberately outside every workspace and is created before a
	// workspace store exists, so it cannot be routed through a workspace API.
	"internal/attest": "owns the data-root verifier signing key",
	// Evaluation fixtures and judge workspaces are scratch data outside any
	// workspace and are removed after each candidate run.
	"internal/judgebench": "owns evaluation scratch fixtures and judge workspaces",
	// Manages git checkouts of the user's own code. Every path it touches is
	// inside a worktree whose location internal/store chose, and a checkout is
	// source being edited rather than workspace state — which is what this
	// rule exists to keep scoped.
	"internal/worktree": "manipulates git checkouts under a store-chosen root",
	// Creates and removes scratch copies of evaluation fixtures, outside any
	// workspace and deleted after each run. Not workspace state. The one place
	// it writes a caller-supplied path — acceptance files, whose names come
	// from a task file — goes through internal/worktree's confinement.
	//
	// It also writes the published benchmark artifacts, summary.json and
	// RESULTS.md, into a directory the operator names. Those are the opposite
	// of workspace state: they are meant to leave the workspace, live in
	// version control and be read by people. Routing them through the store
	// would put them in $BC_DATA, where nobody would find them and no commit
	// would carry them. They are written here rather than in cmd/bcode so the
	// exemption stays on one package with one justification, instead of on
	// the whole CLI.
	"internal/eval": "evaluation scratch directories and published benchmark artifacts",
	// Benchmark workspaces, frozen manifests, and result/artifact trees are
	// operator-selected reproducibility data, not BoundedCode workspace state.
	// They live outside the data-directory workspace namespace and are kept
	// with the benchmark run so a result can be audited without a live store.
	"internal/bench": "benchmark scratch workspaces, manifests, and published run artifacts",
	// Writes AGENTS.md and opencode.json into the user's repository. Both are
	// files the developer commits and edits: AGENTS.md is what OpenCode reads
	// into a session, and opencode.json is their editor configuration. Neither
	// is data-directory state, and confining them to the store would put them
	// somewhere the editor does not look — the same reasoning as the identity
	// pin and the memory notes. Both writes are to paths derived from a
	// workspace root, never from a caller-supplied string.
	"internal/opencode": "writes the repository's AGENTS.md/editor configuration and private per-session capability state",
	// Writes notes under `.bc/memory/` inside the user's repository. §2.2
	// places them there on purpose so they travel with the repository, which
	// makes them repository files rather than data-directory state — the same
	// reasoning as the identity pin above. Confining them to the store would
	// put them in $BC_DATA, where they would not travel and a clone would lose
	// them.
	"internal/memory": "writes the repository's own memory notes",
	// The generate check of §10.1 snapshots a repository's declared generator
	// outputs, runs the generator, and restores the worktree exactly as it was
	// found. Those writes are inside the worktree being verified — source
	// being edited, the same reasoning as internal/worktree above — and the
	// restore is what keeps the check from invalidating every other result
	// (§7.2). Nothing here touches the data directory.
	"internal/recipe": "restores a worktree's generator outputs after a generate check",
	// The Standard Evidence Suite's scratch and published artifacts, on the
	// same reasoning as internal/eval above: cloned benchmark task
	// directories, generated compose files, grading scratch and the run
	// reports. Every one is outside any workspace — they live under the
	// suite's own run root or a directory the operator names — and the
	// reports are meant to leave, be committed and be read by people.
	// Routing them through the store would put them in $BC_DATA where no
	// commit would carry them.
	"internal/evidence": "evidence-suite scratch directories and published run artifacts",
	// The Judgment Validation Campaign's datasets, manifests, policy file and
	// annotations, under evals/judgment/ or a path the operator names. Same
	// category again: campaign evidence is version-controlled and read by a
	// person deciding a promotion, which is the opposite of workspace state.
	"internal/judgeval": "judgment campaign datasets, manifests and policy",
	// Writes pyrightconfig.json into the repository being indexed and removes
	// it afterwards, and a SCIP output file in the system temp directory.
	// The config is what makes the compiler-backed indexer run at all — the
	// same shape as the TypeScript sidecar's tsconfig discovery — and it is
	// a file in the user's own checkout, not data-directory state. The path
	// is derived from the repository root, never from a caller-supplied
	// string.
	"internal/analyzers": "writes the indexer's own config into the repository being indexed",
	// The analyzer's own tests write fixtures.
	"tools/analyzers": "analyzer test fixtures",
}

// forbiddenFuncs are the file-writing and database-opening entry points.
// Reads are deliberately absent: reading a path the caller was given is not
// what breaks isolation; writing state outside the scoped API is.
var forbiddenFuncs = map[string]string{
	"database/sql.Open":   "open the workspace's databases through store.OpenWorkspace",
	"database/sql.OpenDB": "open the workspace's databases through store.OpenWorkspace",
	"os.Create":           "write through store.BlobDir, artifacts.Store, or another store API",
	"os.CreateTemp":       "write through store.BlobDir, artifacts.Store, or another store API",
	"os.WriteFile":        "write through store.BlobDir, artifacts.Store, or another store API",
	"os.OpenFile":         "write through store.BlobDir, artifacts.Store, or another store API",
	"os.Mkdir":            "let the store create directories under the workspace",
	"os.MkdirAll":         "let the store create directories under the workspace",
	"os.MkdirTemp":        "use store.Store.TmpDir, which is wiped on task end",
	"os.Remove":           "remove through a store API so the workspace boundary is respected",
	"os.RemoveAll":        "remove through a store API so the workspace boundary is respected",
	"os.Rename":           "rename through a store API so the workspace boundary is respected",
	"os.Symlink":          "the store does not create symlinks; state must be addressable by path",
	"os.Chmod":            "permissions on workspace state are the store's concern",
}

func run(pass *analysis.Pass) (any, error) {
	pkg := normalize(pass.Pkg.Path())
	if allowed(pkg) {
		return nil, nil
	}

	for _, file := range pass.Files {
		// Test files exercise the store from outside; a test that sets up a
		// fixture directory is not a product code path.
		if isTestFile(pass, file) {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn := calleeName(pass.TypesInfo, call)
			if fn == "" {
				return true
			}
			fix, forbidden := forbiddenFuncs[fn]
			if !forbidden {
				return true
			}
			pass.Reportf(call.Lparen, "%s is confined to %s by design v3 §2.3; %s",
				fn, strings.Join(writeOwners, " and "), fix)
			return true
		})
	}
	return nil, nil
}

// allowed reports whether a package may write.
func allowed(pkg string) bool {
	for _, owner := range writeOwners {
		if pkg == owner || strings.HasPrefix(pkg, owner+"/") {
			return true
		}
	}
	for prefix := range exemptions {
		if pkg == prefix || strings.HasPrefix(pkg, prefix+"/") {
			return true
		}
	}
	return false
}

// normalize strips the module path so the rule is stated in repository terms
// and keeps working if the module is ever renamed.
func normalize(path string) string {
	if i := strings.Index(path, "/internal/"); i >= 0 {
		return path[i+1:]
	}
	if i := strings.Index(path, "/tools/"); i >= 0 {
		return path[i+1:]
	}
	if i := strings.Index(path, "/cmd/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func isTestFile(pass *analysis.Pass, file *ast.File) bool {
	name := pass.Fset.Position(file.Pos()).Filename
	return strings.HasSuffix(name, "_test.go")
}

// calleeName resolves a call to "pkgpath.Func", or "" when it is not a
// package-level function call.
func calleeName(info *types.Info, call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	pkgName, ok := info.Uses[ident].(*types.PkgName)
	if !ok {
		return ""
	}
	return pkgName.Imported().Path() + "." + sel.Sel.Name
}
