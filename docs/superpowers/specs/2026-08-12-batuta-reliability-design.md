# Batuta reliability hardening — design

Design approved in conversation on 2026-08-12. This change hardens the existing
resource-only Batuta extension without adding a subprocess, SDK dependency, or
private daemon state.

## Goal

Make Batuta reliable as a long-running CompozyOS conductor:

- preserve the operator's `auto_commit` choice through the composite delivery Loop;
- return terminal delivery results to the original Batuta conversation;
- keep the watcher armed without an isolated reporting agent;
- reject missing task sets without reporting false success;
- verify behavior rather than only YAML shape;
- align documentation and compatibility metadata with the live architecture.

The existing invariants remain binding: Batuta does not edit code, every task is
routed to the cheapest capable lane, automatic commits are one task per commit,
verification is mandatory, no remote push occurs, and terminal states are reported
exactly.

## Context continuity

CompozyOS owns the original conversation as a durable session. A prompt sent to an
idle or stopped user session starts a normal turn under the same session ID. A prompt
sent while the session is busy uses `mode: queue` and is delivered after the current
turn. The watcher therefore does not need a second agent to summarize the result.

Long sessions remain safe under CompozyOS compaction. At context pressure, complete
prior turns are summarized into the workspace checkpoint, their sequence coverage is
recorded, and covered events are archived rather than deleted. The queued terminal
prompt becomes a normal new turn and may trigger this existing compaction lifecycle.
Durable project facts stay in memory or task artifacts; the watcher message contains
only the run identity and instruction to inspect daemon-owned status.

## Delivery-to-session contract

`batuta-deliver` gains one required string input: `origin_session_id`. The Batuta
agent must pass its current CompozyOS session ID on every dispatch. CLI, HTTP, UDS,
schedule, and native-tool callers must also provide a target session if they want to
use this Loop. Making the value explicit avoids inference from `started_by_ref`, whose
shape varies by ingress and does not guarantee a promptable user session.

The Loop continues to accept `slug` and `auto_commit`. `auto_commit` remains a parent
input and is passed explicitly to both child Loops. The workspace preference is stored
at `loops.inputs.batuta-deliver.auto_commit`; child defaults are not the Batuta control
plane because an explicit parent value overrides them.

The agent bootstrap checks three independent states:

1. routing rules for `implement-tasks`;
2. the `batuta-deliver.auto_commit` workspace input default;
3. a live `batuta-watch` run.

Existing routing rules no longer imply that preferences and watcher state are
configured. The critical-lane choice is applied to the stored `implement-tasks`
runtime rules before dispatch.

## Deterministic watcher

`batuta-watch` remains a resource-only `watch-events` Loop. It no longer runs an
isolated Batuta agent. Its graph is deterministic:

1. `watch-events` receives a durable batch of `loop.terminal` events filtered to
   `batuta-deliver`;
2. a fan-out processes every event in the batch;
3. `compozy__loop_status` reads the exact terminal run;
4. `compozy__session_prompt` queues one message to
   `run.inputs.origin_session_id`;
5. a collect node settles the batch and the watcher returns to dormant watching.

The contract uses `stop_when: "false"` with `iteration_cap: 0`. A successful batch
must start another generation and park on `watch-events`; it must not terminate
`done` after its first event.

Each queued prompt uses deterministic identities derived from the terminal run ID:

- `message_id`: `batuta-terminal-<run-id>`;
- `idempotency_key`: `batuta-terminal-<run-id>`.

Redelivery therefore returns the prior admission result instead of creating a second
operator turn. `mode: queue` preserves any active operator turn. The message tells the
original Batuta session to inspect the run, report the exact terminal, child run IDs,
commits, and blocker, then decide follow-up with the operator. It never authorizes
automatic redispatch, escalation writes, gate approval, code edits, or push.

Prompt admission result fields `status` and `delivery` are authoritative. A failed
prompt action remains visible as a watcher node failure; it is not converted to a
successful transform. This preserves the delivery failure signal for diagnosis.

## Missing task sets

The current `on_error.route` converts `taskSetNotFoundError` into a successful
transform, causing `batuta-deliver` to finish `done`. CompozyOS has no explicit
general-purpose action that emits a terminal `no-op`; inventing a failing tool or
synthetic agent turn would be a workaround.

The fix is fail-closed at the source:

- Batuta must perform the existing dry-run before submission and refuse the real run
  when the task set cannot resolve.
- `batuta-deliver` removes the error-to-success route and the redundant `no_tasks`
  branch. A direct invalid CLI/API submission retains the real import failure instead
  of becoming `done`.
- The operator-facing report names the exact resulting terminal. It never describes a
  missing task set as successful delivery.

This favors truthful failure over fake `no-op`. A native declarative no-op terminal can
replace it later if CompozyOS adds that public Loop surface.

## Compatibility

The manifest minimum becomes `0.3.0-beta.13`, the earliest explicitly supported line
for the Loop composition and `watch-events` behavior used here. The README states the
same floor. Validation must prove that the current daemon accepts this prerelease
constraint.

The extension remains local/unverified during this beta. No new permissions, secrets,
or Host API methods are introduced.

## Tests

Contract tests cover four layers:

1. Manifest validation and exact minimum version.
2. Routing and input propagation through a dry-run, using a collision-safe disposable
   task slug and never deleting a pre-existing fixture.
3. Delivery definition validation, including required `origin_session_id`, explicit
   child `auto_commit` propagation, and absence of the error-to-success route.
4. Watcher definition validation, including no `run-agent`, batch fan-out, native
   status read, queued session prompt, deterministic identities, collection, unbounded
   iteration, and `stop_when: "false"`.

Daemon-backed Loop validation requires a registered workspace because tool and agent
resolution are workspace-scoped. Tests resolve `BATUTA_TEST_WORKSPACE` first and emit a
clear prerequisite error when the repository is not registered; they do not silently
validate against an unrelated workspace.

The guided E2E smoke verifies live behavior:

- operator preference `auto_commit=false` reaches both child runs;
- one terminal event creates one queued/direct turn in the originating session;
- replay of the same event does not duplicate the turn;
- watcher returns to `watching` after delivery;
- missing task dispatch is refused before a real run, while direct invalid submission
  never returns `done`;
- no isolated watcher session or watcher model token spend occurs.

## Documentation

Both READMEs describe the composite `batuta-deliver` flow, the required original
session binding, dynamic provider discovery, watcher behavior, registered-workspace
test prerequisite, and all four published extension resources. The historical 2026-08-11
design and implementation plan are marked superseded where their two-Loop conversational
dispatch and resource inventory differ from the current architecture.

## Non-goals

- No external bridge notification.
- No subprocess extension or custom hook.
- No automatic re-dispatch from the watcher.
- No mutation of bundled `dev-cycle` Loops.
- No private database access.
- No attempt to replace CompozyOS session compaction or memory.
