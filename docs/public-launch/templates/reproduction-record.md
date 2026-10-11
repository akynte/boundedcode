# Reproduction record

Post this as a comment on the issue, whether or not the failure
reproduced ([feedback process, step 2](../feedback-process.md#2-reproduce)).
Copy everything below the line. Write "not applicable" rather than deleting
a field.

---

**Reproduction record**

- **Issue:** #N, reported by @handle on YYYY-MM-DD
- **Reported symptom:** one sentence, in the reporter's terms
- **Reported environment:** version, platform, provider and model, as given

**Attempt**

- **Date:** YYYY-MM-DD
- **BoundedCode:** `boundedcode version` output, or the commit
- **Environment used:** machine or container image (with digest), OS,
  container engine, provider and model. Say how it differs from the
  reporter's.
- **Starting state:** clean install, existing config, or the reporter's
  repository and commit
- **Commands, in order:**

  ```
  exact commands
  ```

- **Observed:** the output that matters, verbatim (trim, do not paraphrase)

**Outcome:** one of
- **Reproduced.** Same symptom. Root cause, if known: …
- **Reproduced differently.** Symptom: … How it differs: …
- **Not reproduced.** What is still needed from the reporter: …

**Classification:** class (feedback process, step 3) · priority `p0`–`p3`
· reason for the priority in one line

**Next step:** fix (and where the regression test goes), a question to the
reporter, or close with the reason
