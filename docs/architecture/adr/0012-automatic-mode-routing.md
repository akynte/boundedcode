# ADR-0012: Automatic routing between chat, code and verification

Status: accepted (2026-10-24). Amends ADR-0011: the user no longer chooses
between direct and bounded mode; a router does.

## Context
ADR-0011 added direct mode beside the task runner and left the choice to
the user (`/task`, `/mode`). The maintainer asked for the switch to be
fully automatic: three modes, **chat**, **code** and **verification**,
selected from the user's intent, the task's requirements and the execution
context, with no manual step.

## Decision
1. **Three modes, one router.** Every message is classified before it runs
   (`direct.Route`): *chat* answers from the code without changing files;
   *code* changes the checkout directly; *verify* runs the request as a
   bounded task (worktree, sandbox, deterministic checks, behavioural
   evidence).
2. **Deterministic rules first, the model only when unsure.** The rules
   read the request: question forms and run/check verbs route to chat;
   change verbs to code; a change that mentions a risk keyword (the
   configuration's escalation lists: payments, authentication, migrations,
   contracts, …), asks for tests or proof, spans several services, names a
   risky path (`direct.RiskyPath`) or names more than `direct.max_files`
   files routes to verify. When the rules are unsure ("how do I add a
   flag?") the model is asked for a one-word classification through the
   same metered gateway; a failed or odd answer keeps the rules' choice.
3. **Switches during a turn.** A chat turn whose answer needs a file change
   becomes a code turn at the first edit (the user asked for an answer;
   the model found a change is needed; nothing is lost by allowing it). A
   code turn that is about to change a risky path (authentication, payments,
   migrations, schema, contracts, infrastructure, protected repository
   configuration) stops *before* the edit and the request is handed off
   to a verified task; earlier edits of that turn are listed and kept.
4. **Execution context.** When a bounded task cannot run here (not a git
   repository with a commit, set-up incomplete, no sandbox image), verify
   downgrades to code with the reason shown, and risky paths are then
   editable; nothing is silently refused.
5. **Overrides stay available but are not needed.** `/mode chat|code|verify`
   and `--mode` force a mode; `/task`, `/code` and `/chat` force one
   message. The default is `direct.mode: auto`.

## Consequences
- The transcript shows the decision for every message (`◇ code · a
  request for a change`) and every switch, so routing mistakes are visible
  and cheap to correct with an override.
- Routing is a heuristic; its false negatives (a risky change routed to
  code) are bounded by the path check at edit time, and its false positives
  cost a slower, verified run rather than a wrong one.
- The risk vocabulary is shared with frontier escalation
  (`escalation.architectural_risk`, `escalation.high_risk_review`).

## Amendment (2026-10-28): the fallback fails closed

Originally, when a verified task could not run, a verify-routed request was
made directly with a note. A request that deserved verification therefore
lost it silently. Now the request is still answered in code mode, but its
first change, and each risky path, is decided by `direct.verify_fallback`:
`ask` (the default; refused when no one can be asked), `refuse`, or `allow`
(audited). See [the security policy](../../usage/security-policy.md).

## Evidence
`internal/direct/router_test.go` (rule table, risky paths, model fallback,
chat→code switch, code→verify hand-off before writing, AllowRisky);
`internal/direct/security_test.go` (a verification downgrade fails closed:
`direct.verify_fallback` ask, refuse, allow; risky paths without
verification);
`internal/cli` (`TestChatRoutesToVerification`: verify route, forced mode,
fallback without a sandbox); `internal/tui` (auto routing to code and
verify, mid-turn hand-off creating the task, forced modes).
