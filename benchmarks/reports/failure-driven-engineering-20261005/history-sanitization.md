# Git history sanitization plan (not executed)

Status on 2026-10-05: `main` is ahead of `origin/main` by every commit from
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
