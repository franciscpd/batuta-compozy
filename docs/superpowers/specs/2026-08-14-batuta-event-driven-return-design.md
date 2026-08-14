# Batuta event-driven delivery return — design

Design approved in conversation on 2026-08-14. This change tightens the
resource-only Batuta agent contract so an accepted delivery runs independently
and returns through CompozyOS terminal effects instead of keeping the conductor
turn alive with polling.

## Goal

After Batuta submits a real `batuta-deliver` run, it acknowledges the durable
dispatch and ends the current turn. CompozyOS owns execution and wakes the
originating session through the Loop's existing idempotent terminal effect.

Progress remains available on demand. An explicit operator progress request may
perform one structured status read in that separate turn, but it must not start
an internal wait or polling cycle.

## Observed failure

The 2026-08-14 visual smoke session `sess-22de2cc93e324ddc` submitted
`looprun-c3275372773ef7c4`, then remained in one provider turn while its child
Loops executed. The durable event stream records 18 shell `sleep` calls and 20
`compozy__loop_status` calls in that turn. The delivery eventually succeeded,
but Batuta consumed a model session to reproduce scheduling that the daemon
already owns.

This is an agent-contract failure, not a missing Loop capability.
`batuta-deliver` already carries `origin_session_id` and declares one
idempotent `compozy__session_prompt` effect for every terminal outcome.

## Dispatch boundary

The successful real `compozy__loop_run` call is a hard turn boundary:

1. Batuta reads the structured result and retains the returned `run_id` and
   optional `web_url` for its dispatch acknowledgement.
2. Batuta reports that the run was accepted and that CompozyOS will return the
   terminal result to the same conversation.
3. Batuta ends the turn without another tool call.

No tool may follow the accepted real dispatch in that turn. In particular,
Batuta must not call `sleep`, a shell-based wait, `compozy__loop_status`,
`compozy__loop_runs`, `compozy__loop_nodes`, session wait/prompt tools, or an
equivalent polling surface.

Dry-runs and failed submissions are not accepted real dispatches. Batuta may
continue the current turn to report or correct their structured errors without
pretending that asynchronous work started.

## Terminal return

The existing terminal effect queues one prompt to the exact
`origin_session_id`. Its `message_id` and `idempotency_key` are derived from the
delivery run ID, so effect replay cannot create another logical prompt.

When that terminal prompt begins a new Batuta turn:

1. The first operational tool call is `compozy__loop_status` for the exact run
   ID carried by the prompt.
2. Batuta reads the parent terminal, child run IDs and terminals, commits, and
   blocker evidence from daemon-owned state.
3. Batuta reports terminal values literally and discusses any redispatch or
   escalation with the operator.

The terminal prompt is a doorbell, not proof of success. Batuta never reports
from the prompt text alone and never rounds a non-`done` terminal up to
success.

## Explicit progress requests

An operator message asking for progress creates a distinct turn and authorizes
one `compozy__loop_status` read for the named or current delivery run. Batuta
reports that snapshot and ends the turn. A live result does not authorize a
sleep, repeated read, or wait-until-terminal loop.

If an operator message races the terminal effect, daemon queue ordering remains
authoritative. Each turn handles its own input once; neither path creates a
watcher or a second reporting agent.

## Return failures

Terminal effects are durable, at-least-once deliveries with stable identities.
If the session-prompt effect fails, its result remains visible on the Loop run.
Batuta does not compensate with polling. The operator can inspect the run or
ask for progress, and Batuta then follows the explicit-progress contract.

Duplicate delivery replays return the prior prompt admission and do not create
another report turn. Batuta performs no automatic redispatch, routing mutation,
gate approval, code edit, commit, or push from a terminal return.

## Changes

- Strengthen `agents/batuta/AGENT.md` with the accepted-dispatch hard boundary,
  terminal-turn first-read rule, and bounded explicit-progress rule.
- Add a public-event E2E validator that checks the ordering without reading
  private stores or logs.
- Update both READMEs and `tests/e2e/SMOKE.md` with the observable contract.
- Keep `loops/batuta-deliver/loop.yaml`, `extension.toml`, and the exact
  three-resource inventory unchanged.

## Verification

Static contract checks ensure the authored agent contract names all three
boundaries and continues to forbid a watcher or reporting agent.

The E2E validator reads `compozy session events <session> -o json` and verifies:

- one successful real `batuta-deliver` dispatch is identifiable;
- no tool call follows that dispatch in the same turn;
- no shell sleep or equivalent active wait occurs while the delivery is live;
- the terminal return occurs in a later turn;
- that turn's first operational tool call is `compozy__loop_status` for the
  exact dispatched run ID;
- replay of the terminal identity does not create another logical return;
- an explicit progress turn performs at most one status read and no wait.

The isolated live smoke republishes only the candidate Batuta package, uses a
fresh workspace and session, captures the event stream, and tears down every
lab process. Existing package, publication, compatibility, config-preference,
missing-task, Loop-validation, and inventory contracts remain green.

## Non-goals

- No change to CompozyOS or bundled `dev-cycle` Loops.
- No provider/model failover implementation.
- No fallback watcher, automation trigger, reporting agent, subprocess, or
  hook.
- No automatic status refresh for the UI.
- No change to model pricing or routing candidates.
- No session-lineage or session-archive change.

Ordered provider/model fallback is a separate future CompozyOS feature. It
must be generic to sessions and Loop workers, reuse the daemon's pre-acceptance
fallback safety boundary, persist typed audit evidence, and begin with an
upstream feature issue before Batuta consumes it.
