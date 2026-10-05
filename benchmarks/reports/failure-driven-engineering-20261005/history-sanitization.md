# Git history sanitization (executed 2026-10-05)

Executed before any push, on the unpushed range only (`origin/main..main`,
24 commits), with `git filter-branch` (tree and message filters). The
original history is kept on the local branch `backup/pre-sanitize-20261005`
(delete only after an approved push).

- Report and evidence files: the home prefix became `$HOME` (as the tip
  already had).
- `internal/frontier/packet.go` and `sanitize_test.go` in the older commits
  got the neutral example homes the tip already used: `/home/dev`,
  `/home/devon`, `/home/dev_backup`, `/home/dev.bak`. One commit message got
  the same mapping.
- Commit hashes cited in docs, reports and commit messages were remapped
  to the rewritten commits (table below). Hashes in run records refer to
  the same trees.
- Authorship, committer, dates, messages otherwise, and every DCO sign-off
  are unchanged.

Checks after the rewrite:

- no absolute home path in any commit, diff or message reachable from
  `main`;
- the tip tree equals the pre-rewrite tip except for the remapped hash
  references (14 files, hash tokens only);
- `make check` and the security suites pass on the new tip.

| Before rewrite | After rewrite | Commit |
|---|---|---|
| `a0e637e` | `1cc85e9` | Report the targeted engineering pass and the second independent validation |
| `784d6ce` | `f63dde0` | Freeze the second validation: six screened tasks, configs, environment |
| `a5fb204` | `5b3c54a` | Record the second validation's candidate manifest and derivability screening |
| `ed3a0a3` | `4b31742` | Calibrate the strategy governor on what it measures |
| `08c841f` | `d5a09d3` | Report strategy stops, ambiguity, contract and seed metrics in benchmark runs |
| `a57c79f` | `43f7cfd` | Bound unproductive strategies, read task contracts, rank retrieval seeds |
| `8b6506f` | `87bdbaa` | Report the failure-driven engineering pass |
| `f99f034` | `3c75f2e` | Format digest test |
| `e4dd766` | `036ac7d` | Lead retry packs with a failure digest |
| `cdc698a` | `f794870` | Use a neutral example home in the packet sanitizer; plan the history cleanup |
| `263a794` | `add3aab` | Failure-driven fixes: behavioural evidence, agent toolchain, reasoning budget |
| `cd9fc7e` | `c83f538` | Report the small real-world validation |
| `3b17646` | `65f805a` | Give acceptance checks the same dependency mounts as verification |
| `fd50c9f` | `f5ac462` | Undo verification side effects on the candidate |
| `138cdda` | `ee6f9ca` | Add verification configs for the post-fix reruns |
| `59bae6b` | `7bcc9c6` | Give tool caches a writable layer inside dependency mounts |
| `3b4cfce` | `b50ed59` | Sanitize all host paths in frontier packets; record blocked escalations |
| `5606798` | `6684073` | Record the remaining frozen validation results |
| `8a8c76a` | `e3e7e45` | Record the confirmed cause of the memory exhaustion |
| `d3a565a` | `b9e76f9` | Record the frozen vuejs result and memory-pressure evidence |
| `d8089b6` | `f2b4001` | Bound cross-service constant evaluation |
| `f623bba` | `bcc17e1` | Run JavaScript verification with the checkout's dependencies |
| `1ca41f1` | `e86a7aa` | Stop masking source code as secrets |
| `4e341a1` | `0217d96` | Add a small real-world validation harness and freeze its task set |

---

Original plan (kept below for the record). Status before execution: `main` is ahead of `origin/main` by every commit from
`0217d96` onwards; none of them has been pushed. Do not push until this is
done and publication preparation is approved.

## What contains an absolute home path

| Commits (unpushed) | Files | Content |
|---|---|---|
| `0217d96` … `65f805a` | `benchmarks/reports/small-real-world-validation-20261004/` screening JSON, `run-*.json`, two `evidence/*-attempt1-*.log` | Benchmark work paths in test output and `state_dir` fields. Rewritten to `$HOME` at the tip by `c83f538`, but present in the earlier commits' trees. |
| `b50ed59` onwards (still at HEAD) | `internal/frontier/packet.go` (doc comment), `internal/frontier/sanitize_test.go` | The real home directory used as an example home in the sanitizer's comment and tests. Not a leak of data, but it names the user. |

The source tree otherwise has no absolute home paths (`git grep "$HOME" HEAD`).

## Recommended strategy

1. At the tip, replace the example home in `packet.go` and
   `sanitize_test.go` with a neutral one (`/home/dev`, and `/home/devon` for
   the boundary case), and run the frontier tests.
2. Rewrite only the unpushed range, replacing the literal in every
   tree, with `git filter-repo` (preferred; not installed here) or
   `git filter-branch`:

   ```sh
   git branch backup/pre-sanitize           # keep the original
   export OLD="$HOME"                        # the prefix to remove
   git filter-branch --tree-filter \
     'grep -rlZ -- "$OLD" . 2>/dev/null | xargs -0 -r sed -i "s#$OLD#\$HOME#g"' \
     origin/main..HEAD
   ```

   Commit messages, authorship and DCO sign-offs are unchanged; commit
   hashes change (references to hashes in report text, e.g. "fixed in
   bcc17e1", must then be updated in a final commit, or kept with a
   mapping table).
3. Verify: `git log -p origin/main..HEAD | grep -c "$OLD"` returns 0;
   `make check` passes on the new tip; the tree at the new tip equals the
   old tip except for step 1.
4. Delete `backup/pre-sanitize` only after the push is approved and done.

Squashing the range into one commit would also work, but loses the
per-fix history this project relies on in its reports.
