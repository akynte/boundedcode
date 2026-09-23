package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// armDir is the on-disk directory name for one arm — matches what
// evals/evidence-v1/tooling/materialize_task.py already writes under
// .boundedcode-runs/evidence-v1/agent-workspaces/<task>/<armDir>, so
// Prepare reads the same, already-verified sanitized workspaces that
// tooling produced rather than a second copy.
func armDir(a Arm) string {
	if a == ArmControl {
		return "control"
	}
	return "jev-assisted"
}

// Prepare populates WorkspacePath and SourceFingerprint on a RunPlan.
//
// For SWE-Bench Pro Verified: locates the already-sanitized, already
// anti-leakage-verified workspace materialize_task.py produced, re-checks
// the sanitized-git invariant immediately before use (design instruction
// §9's "immediately before generator invocation verify again"), and
// fingerprints it. It does not re-run the sanitizer itself — that
// mechanism already exists, is tested, and is invoked by
// evals/evidence-v1/tooling/materialize_task.py; Prepare's job is to
// consume its output, not duplicate it.
//
// For SWE Atlas: not yet implemented. The harbor-native materialization
// path (a `harbor` task instantiation, not a docker-cp-and-sanitize flow)
// has not been built; Prepare says so explicitly rather than approximating
// it.
func Prepare(ctx context.Context, runRoot string, p RunPlan) (RunPlan, error) {
	switch p.Benchmark {
	case BenchmarkSWEBenchProVerified:
		return prepareSWEBenchPro(ctx, runRoot, p)
	case BenchmarkSWEAtlasTestWriting, BenchmarkSWEAtlasRefactoring:
		return prepareSWEAtlas(ctx, runRoot, p)
	default:
		return p, fmt.Errorf("evidence: unknown benchmark %q for task %s", p.Benchmark, p.TaskID)
	}
}

// prepareSWEAtlas materializes a task's workspace directly from its pinned
// image. Unlike SWE-Bench Pro Verified, no sanitization step is needed:
// every selected SWE Atlas image was confirmed, by direct probe against
// the real pulled images, to already be a single-commit repository with no
// other branches, tags, or reflog leakage — Scale AI strips history at
// image-build time. This function reuses the exact same materialization
// primitive materialize_task.py uses for SWE-Bench Pro Verified (start a
// disposable container, `docker cp` the repo root out, verify, stop the
// container) without the sanitize_git_repo step, since there is nothing to
// sanitize.
func prepareSWEAtlas(ctx context.Context, runRoot string, p RunPlan) (RunPlan, error) {
	dst := filepath.Join(runRoot, "agent-workspaces", p.TaskID, armDir(p.Arm))
	if _, err := os.Stat(filepath.Join(dst, ".git")); err == nil {
		// Already materialized by an earlier Prepare call (or a previous
		// process) — re-verify and reuse it rather than re-pulling and
		// re-copying multiple gigabytes on every invocation.
		if err := verifySanitizedInvariant(ctx, dst); err != nil {
			return p, fmt.Errorf("evidence: %s: anti-leakage re-check failed on the existing "+
				"materialized workspace: %w", p.TaskID, err)
		}
		fp, err := Fingerprint(dst)
		if err != nil {
			return p, err
		}
		p.WorkspacePath, p.SourceFingerprint = dst, fp
		return p, nil
	}

	if err := os.RemoveAll(dst); err != nil {
		return p, err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return p, err
	}

	cid, err := startEntrypointOverriddenContainer(ctx, p.ImageRef)
	if err != nil {
		return p, fmt.Errorf("evidence: starting a container from %s: %w", p.ImageRef, err)
	}
	defer func() {
		stopCtx, cancel := cleanupContext(ctx)
		defer cancel()
		_ = exec.CommandContext(stopCtx, "docker", "stop", "-t", "2", cid).Run()
	}()

	// G204: the executable is the literal "docker", never a variable. cid is
	// a container id the daemon generated moments ago; p.ImageDir comes from
	// the frozen suite manifest, which is committed and hash-pinned, not from
	// the task, the model or the network; dst is built from the run root.
	// CommandContext passes argv directly to execve, so no element is ever
	// interpreted by a shell.
	cp := exec.CommandContext(ctx, "docker", "cp", cid+":"+p.ImageDir, dst) //nolint:gosec // G204: fixed executable, operator-pinned arguments, no shell
	if out, err := cp.CombinedOutput(); err != nil {
		return p, fmt.Errorf("evidence: docker cp from %s: %w\n%s", p.ImageRef, err, out)
	}

	if err := verifySanitizedInvariant(ctx, dst); err != nil {
		return p, fmt.Errorf("evidence: %s: SWE Atlas image was not the expected single-commit "+
			"state (this benchmark family is not supposed to need sanitization — if this fires, "+
			"treat it as a real anti-leakage finding, not a bug to silence): %w", p.TaskID, err)
	}

	fp, err := Fingerprint(dst)
	if err != nil {
		return p, fmt.Errorf("evidence: fingerprinting %s: %w", dst, err)
	}
	p.WorkspacePath, p.SourceFingerprint = dst, fp
	return p, nil
}

// startEntrypointOverriddenContainer starts a disposable, long-lived
// container from ref with /bin/bash as its entrypoint and a sleep as its
// command — the exact pattern this session confirmed is required for
// every SWE-Bench Pro Verified and SWE Atlas image, all of which set
// ENTRYPOINT ["/bin/bash"] with no CMD; without the override, `docker run
// image sleep 600` execs `/bin/bash sleep 600`, which bash treats as a
// script named "sleep" to source rather than a command to run, and the
// container exits immediately.
func startEntrypointOverriddenContainer(ctx context.Context, ref string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "run", "-d", "--rm", "--entrypoint", "/bin/bash", ref, "-c", "sleep 600")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func prepareSWEBenchPro(ctx context.Context, runRoot string, p RunPlan) (RunPlan, error) {
	dir := filepath.Join(runRoot, "agent-workspaces", p.TaskID, armDir(p.Arm))
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return p, fmt.Errorf("evidence: sanitized workspace not materialized at %s: %w — run "+
			"evals/evidence-v1/tooling/materialize_task.py first (network + Docker, no "+
			"credential required)", dir, err)
	}

	if err := verifySanitizedInvariant(ctx, dir); err != nil {
		return p, fmt.Errorf("evidence: %s: anti-leakage re-check failed immediately before use: %w",
			p.TaskID, err)
	}

	fp, err := Fingerprint(dir)
	if err != nil {
		return p, fmt.Errorf("evidence: fingerprinting %s: %w", dir, err)
	}

	p.WorkspacePath = dir
	p.SourceFingerprint = fp
	return p, nil
}

// verifySanitizedInvariant re-checks, directly against the actual git
// state, that a workspace is still exactly the sanitized single commit —
// design instruction §9. A Go-native reimplementation of the same checks
// evals/evidence-v1/tooling/sanitize_workspace.py's verify_sanitized
// performs, kept intentionally narrow (this package does not need the
// fuller probe's fsck/object-alternate checks at every single Prepare
// call — those already ran once, thoroughly, when the workspace was
// materialized; this is the fast, cheap re-check immediately before use).
func verifySanitizedInvariant(ctx context.Context, dir string) error {
	run := func(args ...string) (string, error) {
		// G204: the executable is the literal "git". Every args value at the
		// call sites below is a fixed string constant, and dir is the prepared
		// workspace under the run root. Nothing from the task, the model or
		// the network reaches this, and argv goes straight to execve with no
		// shell.
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // G204: fixed executable and literal arguments, no shell
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
	log, err := run("log", "--all", "--oneline")
	if err != nil {
		return fmt.Errorf("git log --all: %w", err)
	}
	lines := strings.Split(log, "\n")
	if log == "" {
		return fmt.Errorf("no reachable commits")
	}
	if len(lines) != 1 {
		return fmt.Errorf("expected exactly 1 reachable commit, found %d: %v", len(lines), lines)
	}
	remotes, err := run("remote")
	if err != nil {
		return fmt.Errorf("git remote: %w", err)
	}
	if remotes != "" {
		return fmt.Errorf("unexpected remote(s): %s", remotes)
	}
	tags, err := run("tag")
	if err != nil {
		return fmt.Errorf("git tag: %w", err)
	}
	if tags != "" {
		return fmt.Errorf("unexpected tag(s): %s", tags)
	}
	return nil
}

// Fingerprint hashes a workspace's content deterministically (sorted
// relative path, mode, content), excluding .git — the same shape
// evals/evidence-v1/tooling/sanitize_workspace.py's fingerprint_tree uses,
// reimplemented here so this package does not need a Python interpreter at
// run time. Two independently prepared copies of the same base state must
// fingerprint identically — the control/treatment equality check design
// instruction §23 asks for.
func Fingerprint(root string) (string, error) {
	h := sha256.New()
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	for _, rel := range paths {
		full := filepath.Join(root, rel)
		info, err := os.Lstat(full)
		if err != nil {
			return "", err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(full)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(h, "L %s -> %s\n", filepath.ToSlash(rel), target)
		case info.IsDir():
			fmt.Fprintf(h, "D %s\n", filepath.ToSlash(rel))
		default:
			body, err := os.ReadFile(full)
			if err != nil {
				return "", err
			}
			exe := "-"
			if info.Mode()&0o100 != 0 {
				exe = "x"
			}
			fmt.Fprintf(h, "F %s %s ", filepath.ToSlash(rel), exe)
			sum := sha256.Sum256(body)
			h.Write(sum[:])
			h.Write([]byte("\n"))
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
