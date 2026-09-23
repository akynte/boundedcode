# GitHub presentation

Recommendations only. This documentation change does not modify GitHub settings,
publish a release or post on the maintainer's behalf.

## Repository description

Local software engineering on an 8 GB GPU: a Go coding agent with ternary Bonsai inference, repository intelligence, scoped tools and verification.

## Launch tagline

**Local engineering. 8 GB VRAM. Evidence before acceptance.**

Use the descriptive README subtitle for first-time visitors:
“A local software-engineering agent built around an 8 GB VRAM budget.”

## Topics

Suggested set (20):

```text
local-ai
coding-agent
software-engineering-agent
ai-coding-assistant
8gb-vram
low-vram
consumer-gpu
local-llm
self-hosted
llama-cpp
bonsai
golang
repository-intelligence
code-analysis
agentic-coding
verification
sandbox
developer-tools
cuda
local-first
```

Do not tag unsupported OS/GPU platforms or claim formal verification. “Offline”
needs the network-policy caveat; “Claude Code alternative” or “Codex alternative”
can suggest feature/performance parity the project has not measured.

## Primary search phrases

Use naturally in introductions, guides and release notes, not a keyword block:

- local AI coding agent for an 8 GB GPU
- local software-engineering agent
- low-VRAM coding agent with verification
- llama.cpp repository-level coding
- RTX 4060 local coding agent
- local coding agent on consumer hardware

These phrases reflect the product and terminology found in current local-agent
documentation/search results; no search-volume or ranking advantage was measured.
See [related work](../explanation/related-work.md).

## Social preview text

BoundedCode  
Local software engineering, built around 8 GB VRAM  
Repository intelligence → scoped edits → verification

Use a simple hardware/task-flow visual. Put “RTX 4060 Laptop reference” in a
small evidence caption; do not imply universal 8 GB compatibility. A generated
terminal screenshot is not evidence.

## Demo to capture

The strongest missing asset is a real coding run with continuous resource
measurements. Place a short clip immediately after the README introduction;
link it to the full transcript and result artifact. Suggested future paths:
`docs/assets/8gb-task-demo.webm`, `docs/assets/8gb-task-demo-poster.png`, and
a dated result under `docs/benchmarks/results/`. These media files do not yet exist.

1. Record the GPU, CPU, RAM, driver, kernel, code revision, model checksum, profile
   and runtime revision. Disclose all other GPU processes.
2. Show the Bonsai server starting; distinguish cold load from warm task latency.
3. Sample whole-device VRAM and model/process-tree RAM throughout. Keep raw samples,
   not just a terminal meter at the beginning and end.
4. Initialize a disposable repository at a public commit and state the acceptance
   criteria. Use a nontrivial, bounded bug fix with regression tests.
5. Show localization, plan/write grant, edits and the actual verification output.
6. If verification fails, show the evidence-driven retry. Do not fabricate a
   failure; if demonstrating recovery requires a seeded defect, label it.
7. Show fresh review, the approval gate and the final task-branch diff/commit.
8. Run independent acceptance checks and report failures as well as passes.
9. End with peak sampled resources, elapsed time, token totals and source links.

A 60–120 second edited overview is useful, but disclose time cuts and retain the
full recording. Do not label an accelerated sequence “real time.”

The existing `docs/demo.cast` and `scripts/record-demo.sh` are a deterministic
CLI walkthrough. They do not show a measured Bonsai repair/verification lifecycle
or establish an 8 GB end-to-end result.

## Launch evidence checklist

Before a stronger launch claim, publish:

- repeated end-to-end Bonsai tasks with independent acceptance tests;
- continuous task/load memory measurements and a realistic host-RAM minimum;
- a clean-machine install validation and pinned Bonsai container recipe;
- a passing CI record, with no hidden skip/failing baseline gates;
- a short demo and a good first issue in indexing, verification or onboarding.

A star invitation should give a reason to follow the work: reproducible progress
toward useful, local repository engineering on consumer GPUs—not a promise that
stars unlock features.
