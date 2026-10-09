# README review for first-time visitors (2026-10-09)

This review covers the README restructure that follows the
[readiness audit](readiness-audit.md). It is written from the point of view
of an experienced engineer who opens the repository for the first time.

## Proposed GitHub metadata

These are not applied yet. Changing the repository settings is the
maintainer's decision.

**Description** (under GitHub's 350-character limit):

> Control plane for AI coding agents: runs OpenHands in a network-less
> sandbox and reports a change as verified only when a test it adds fails on
> the original code and passes with the change. Local llama.cpp model by
> default, or a cloud API. Public alpha.

**Topics:** `coding-agent`, `llm`, `local-llm`, `llama-cpp`, `openhands`,
`software-testing`, `sandbox`, `developer-tools`, `golang`, `mcp`

**Changes from the current settings:**
- **Dropped:** `ai`, which is too generic.
- **Dropped, now `local-llm`:** `local-ai`.
- **Dropped:** `software-engineering`, which is too broad to help discovery.
- **Added:** `software-testing` and `sandbox`, the two properties the README
  leads with.

The current description leads with "bounded context, deterministic
verification…, resumable tasks and selective frontier escalation". That
names four features with equal weight. The proposed description leads with
the one property that is distinctive and checkable.

## What a first-time visitor now understands, and where

| Question | Where it is answered | Before |
|---|---|---|
| What is this, in one sentence? | Title line and first paragraph | The tagline "Bounded context. Bounded cost. Unbounded codebases." did not say what the tool does |
| What problem does it address? | "The problem: green tests are weak evidence", with two concrete cases (the demo's passing `go test`; the first validation's false passes) | Two bullets in "Why BoundedCode" |
| What exactly does "verified" mean? | "What BoundedCode does", step 4, plus a two-row result table and an explicit "not formal verification" sentence | Spread over the principles table, the Verification section and a footnote |
| Can I see it work? | "Demo": the GIF plus the actual terminal output as text, explained step by step | GIF only, with a caption |
| How do I try it, and will it run on my machine? | "Quick start": install commands, what you need, a platform status table | Installers, then a long manual install inline |
| Does it work? | "Results": the three-stage table, then the caveats as bullets directly under it | Results, then caveats in several paragraphs and a collapsed section |
| How is it built, and what is theirs vs. upstream? | "How it works": a simplified diagram and the implements/integrates tables | A dense diagram with 20+ nodes before any explanation |
| How is it different from other coding agents? | "How it differs from a general-purpose coding agent": what it adds around the agent loop, with no claim of better code generation | Not addressed directly |
| What are the risks and limits? | Security table, "Known limitations", the verification guide's "Known limits" | Present but long |

## Consolidation

**Moved out of the README:**
- **Full task-flow diagram, evidence rules and verification limits:** to
  the new [verification guide](../usage/verification.md).
- **Validation detail:** "Why 5/6", "not an improvement curve", the
  methodology caveats and the memory figures, to the new
  [evaluation overview](../benchmarks/README.md). The README keeps the
  results table and every qualification in short form.
- **Manual install:** already covered in
  [getting started](../usage/getting-started.md), now linked.
- **The "Smaller limitations" section:** the offline-dependency requirement
  moved into the Quick start requirements; "terminal only" became
  limitation 9.

**Merged:**
- The Models, Cloud models and Frontier sections are now one section.
- The two separate verification-state tables are now one.

**Added:** a [documentation index](../README.md). The README's "Docs" link
previously opened a bare directory listing.

**Removed from the README:**
- Four of the seven badges: secret-scan, DCO, Go version and the status
  badge. The status is stated in the alpha notice.
- The tagline.

**Preserved:**
- All benchmark numbers and qualifications.
- Every failure report and its links.
- The "How this was built" disclosure.
- The upstream attribution tables.
- The README anchors linked from release notes (`#quick-start`,
  `#verification`).

## Checks against the constraints

- **No formal-verification or correctness claims.** `TASK_VERIFIED` is
  described as evidence that a test captures a behaviour change. The README
  says it is not formal verification and does not guarantee correctness.
- **No improvement curve.** The README states that 0/8 and 5/6 come from
  differently selected sets and do not measure improvement.
- **Unvalidated configurations are marked:**
  - Linux x86-64 is the only validated platform.
  - macOS, Windows, Linux arm64, cloud providers, other models and frontier
    escalation are marked experimental, unrun or unproven.
- **No claims about competing tools.** The comparison section describes
  only what BoundedCode adds. It cites BoundedCode's own negative baseline
  result.

## Validation performed

- **Links:** every relative link and heading anchor in the README, the docs
  index, the verification guide, the evaluation overview, getting started,
  SECURITY.md, the sandbox design and the audit was checked with a script
  that follows GitHub's heading-slug rules. No problems were found. A planted
  bad anchor and a planted missing file were both detected.
- **Tables:** every Markdown table in the new and rewritten files has a
  consistent column count.
- **Facts:** every number in the README was checked against the reports.
  The demo transcript was copied from the GIF's frames.

**Not rendered:**
- The Mermaid diagrams. The README diagram uses only constructs already
  present in the previously rendered diagram.
- The GitHub alert boxes, which use GitHub's documented syntax.
