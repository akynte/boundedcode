# ADR-0009: Frontier escalation through the Codex CLI subscription

Status: accepted (2026-10-04)

## Context
ADR-0001 commits to local models for most token volume and a frontier model
only for high-value cases, within a budget of at most one ~USD 20/month
subscription. Local Qwen3.6 runs can fail on design-heavy or
correctness-critical tasks (concurrency, idempotency, cross-service
contracts) even when they pass our verification. Three questions needed an
answer: when to escalate, how to reach a frontier model without metered API
spending, and what the frontier side may see.

The Codex CLI (Apache-2.0, user-installed) can run non-interactively
(`codex exec`) with ChatGPT-plan sign-in, so a ChatGPT subscription can be
used without an API key. `codex exec --sandbox read-only` restricts writes,
not reads: commands run by Codex's own agent can still read the host
filesystem. The Phase 7 live run showed this, and also showed packets
carrying absolute host paths from codebase-memory-mcp snippets
([report](../../../benchmarks/reports/phase7-frontier/README.md)).

## Decision
1. **Triggers are deterministic** (`internal/frontier/policy.go`,
   `frontier.Evaluate`). No model decides whether to escalate.
   * **Z1, architectural risk**, evaluated before the first attempt and
     again after each failed attempt until it has fired once. The possible
     reasons are keyword hits in the request (and goal)
     (`escalation.architectural_risk`), changes spanning more than one
     repository, and changed contract files (`*.proto`, OpenAPI/Swagger,
     `*.sql`, `migrations/`, `*.avsc`, GraphQL schemas). Z1 needs at least
     two reasons, so keywords alone never fire it. Before the first attempt
     nothing has changed yet, so there Z1 means risk keywords in a
     multi-repository task. At most once per task.
   * **Z2, repeated local failure**, after a failed verification: the
     runtime stuck detector fired, the same failure signature repeated
     across two attempts, `escalation.failed_attempts` (default 3)
     consecutive failures, or `escalation.rejected_strategies` (default 2)
     rejected strategies with at least two consecutive failures.
   * **Z3, high-risk pre-merge review**, once per task after verification
     passes: changed paths match `escalation.high_risk_review` (auth,
     crypto, payment, ledger, Terraform, Kubernetes, …), and/or a
     cross-service contract was changed without its counterpart in another
     repository. The local agent gets one more round to apply the review. If
     no attempts are left, the task is blocked with the advice kept for
     `task resume`, rather than completed with the advice dropped.
   * **Z4, explicit user request** (`boundedcode frontier review TASK`).
   Z1–Z3 stop firing once the task's `budgets.max_escalations` (default 2;
   0 means no limit) is used up. Z4 is not limited by that budget.
2. **Frontier advice returns to the local workflow.** The provider answers
   a question; the local agent implements and our verification decides. The
   frontier never edits repositories. A declined, deferred or failed
   escalation leaves the task running locally
   (`internal/orchestrator/escalation.go`).
3. **Subscription only, through the Codex CLI** (`frontier.Codex`). Before
   each call it runs `codex login status` and refuses when the auth mode
   mentions an API key. `OPENAI_API_KEY` and `CODEX_API_KEY` are removed
   from the environment of `codex exec`, so it cannot fall back to metered
   API auth. The `manual` provider writes the packet to disk instead; the
   user pastes it into any chat and stores the reply with `frontier answer
   TASK FILE`.
4. **Codex sees only the packet.** It runs as `codex exec --sandbox
   read-only --skip-git-repo-check --ephemeral -C <empty dir> -o <file> -`,
   with the packet on stdin. With `frontier.contain: true` (default) it runs
   in a named container (`sandbox.engine`, the agent image; removed on
   cancellation) with all capabilities dropped, `no-new-privileges`, the caller's uid/gid, a tmpfs
   home, the host `codex` binary mounted read-only, the Codex credential
   directory (`$CODEX_HOME`, default `~/.codex`) mounted read-write for token
   refresh, and only the per-task frontier scratch directory. The container
   keeps network access, because Codex must reach OpenAI. `contain` requires
   `sandbox.kind: docker`; turning it off is an explicit config choice.
5. **Approval is required by default** (`frontier.require_approval: true`).
   Each Z1–Z3 escalation shows the trigger, the reason, the packet path and
   its token estimate and asks before sending. Without a terminal the
   escalation is declined unless `--approve-frontier` was passed. A Z4
   request counts as approval.
6. **Packet composition and size.** The packet is a context pack built by
   `contextplan.Build` with `frontier.max_packet_tokens` (default 24,000) as
   its budget: task (request, goal, acceptance criteria, decisions),
   latest verification failures, strategies already tried, current diff,
   impact, cross-service contracts, relevant code and ADRs; the local-agent
   rules section is left out. It is prefixed with a fixed instruction and
   ends with a trigger-specific question (`frontier.DefaultQuestion`). Host
   paths are rewritten to workspace-relative names, the text is redacted
   (`telemetry.Redact`), and a packet that still contains the home
   directory is not sent (`frontier.CheckPacket`).
7. **No hard-coded quota.** We do not encode a subscription's message
   limits; they change and differ by plan. Usage is bounded by the
   per-task escalation budget, approval and the trigger rules. A provider
   error (including a quota refusal) is recorded and the task continues
   locally.
8. **Telemetry.** Every escalation is a row in the `escalations` table
   (trigger, reason, provider, model, status `proposed`/`declined`/`sent`/
   `answered`/`failed`, packet path and token estimate, response path,
   outcome, the task's final status, and fingerprints of the task diff when
   advice was requested and after the next attempt, so whether the advice
   changed the code is measured) plus `frontier.triggered`,
   `frontier.blocked`, `frontier.failed` and `frontier.answered` events. Packets and answers
   are written with mode 0600 under the task's `frontier/` directory. The
   outcome (`helped` when the task later completes) is correlational; the
   benchmark harness measures effect by running tasks with frontier
   disabled. `boundedcode frontier status` shows the configuration, the
   Codex login state and the history.

Escalation is off by default (`frontier.enabled: false`).

## Consequences
* A ChatGPT subscription is enough; there is no API-key path to misuse.
  Users of other frontier products can use the `manual` provider.
* We depend only on documented `codex exec` flags and `codex login status`
  output. A change in that output (for example the "API key" wording) could
  weaken the refusal check; the environment stripping still applies.
* Containment needs a container engine and the agent image. Inside the
  container Codex still has network access and the credentials; only the
  packet and an empty directory are visible (see
  [sandbox.md](../../design/sandbox.md), residual risks).
* Packets leave the machine. Redaction and the home-directory check are
  best effort; approval, which shows the packet path before sending, is the
  primary control.
* Escalations add wall-clock time (Phase 7: 642 s local-only versus 1,649 s
  with two escalations on the same task).

## Evidence
* [Phase 7 live run](../../../benchmarks/reports/phase7-frontier/README.md)
  (2026-10-03, Codex CLI 0.156.1, ChatGPT Plus sign-in, no API key): on
  `cross-service-idempotency`, Z1 (1,139-token packet) and Z3 (5,463-token
  packet) were sent. The Z3 pre-merge review found the race the local agent
  left in, and the stricter concurrent hidden check then passed; it fails
  local-only. One task: it shows the mechanism works, not a success rate.
* The same run found the two leaks this ADR's packet rewriting, home check
  and containment fix. The containerized Codex was verified to report
  "Logged in using ChatGPT" with no host repositories or `$HOME` visible.
* Unit tests: `internal/frontier/frontier_test.go`; live test
  `internal/frontier/codex_live_test.go` (opt-in: `BC_TEST_CODEX_LIVE=1` and `BC_TEST_DOCKER_IMAGE`).
