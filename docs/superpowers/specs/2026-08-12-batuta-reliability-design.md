# Batuta reliability hardening — design

Design approved in conversation on 2026-08-12. This change hardens the existing
resource-only Batuta extension without adding a subprocess, SDK dependency, or
private daemon state.

## Goal

Make Batuta reliable as a long-running CompozyOS conductor:

- preserve the operator's `auto_commit` choice through the composite delivery Loop;
- return terminal delivery results to the original Batuta conversation;
- return every terminal through native effects without a watcher or reporting agent;
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
turn. The delivery Loop therefore does not need a second agent to summarize the result.

Long sessions remain safe under CompozyOS compaction. At context pressure, complete
prior turns are summarized into the workspace checkpoint, their sequence coverage is
recorded, and covered events are archived rather than deleted. The queued terminal
prompt becomes a normal new turn and may trigger this existing compaction lifecycle.
Durable project facts stay in memory or task artifacts; the terminal-effect message
contains only the run identity, terminal trigger, and instruction to inspect
daemon-owned status.

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

The agent bootstrap checks two independent states:

1. routing rules for `implement-tasks`;
2. the `batuta-deliver.auto_commit` workspace input default.

Existing routing rules no longer imply that the delivery preference is configured.
The critical-lane choice is applied to the stored `implement-tasks` runtime rules
before dispatch. Bootstrap does not start or monitor a background Loop.

## Root-cause amendment — 2026-08-12: native terminal effects

The initially approved watcher graph attempted to read
`nodes.read_deliver.output.run.inputs.origin_session_id` and
`nodes.read_deliver.output.run.status` after `compozy__loop_status`. Live descriptor
validation showed that `run_id` is accepted as input, but the declared output schema
exposes only `run.id`, `run.best_generation`, and `run.best_score`. The Loop compiler
correctly rejected both undeclared paths as `unresolvable_path`. Runtime history
containing those fields does not make them legal template paths, and a dynamic
accessor would only hide the contract mismatch.

The root-cause fix removes `batuta-watch` entirely. `batuta-deliver` declares all
seven contract terminal effects: `on_done`, `on_noop`, `on_blocked`, `on_failed`,
`on_exhausted`, `on_stalled`, and `on_canceled`. Each effect calls
`compozy__session_prompt` with:

- `session_id`: `{{ .inputs.origin_session_id }}`;
- `mode`: `queue`;
- `message_id`: `batuta-terminal-{{ .effect.identity.loop_run_id }}`;
- `idempotency_key`: `batuta-terminal-{{ .effect.identity.loop_run_id }}`.

The message identifies `effect.identity.loop_run_id` and
`effect.identity.trigger`, then tells the original Batuta session to inspect the run,
report the exact terminal, child run IDs, commits, and blocker, and decide follow-up
with the operator. It never authorizes automatic redispatch, escalation writes, gate
approval, code edits, or push.

Terminal effect intents are persisted with the Loop lifecycle transition and relayed
as tool calls with stable delivery IDs. The run-derived prompt identities make replay
idempotent: redelivery returns the prior admission instead of creating another turn.
Prompt admission fields such as `status` and `delivery`, plus durable effect results,
remain the failure signal; there is no reporting agent or transform that can round a
failed return up to success.

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
for the Loop composition and terminal-effect behavior used here. The README states
the same floor. Validation must prove that the current daemon accepts this prerelease
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
4. Terminal-return validation through the live `compozy__session_prompt` descriptor
   and candidate compilation of `batuta-deliver`.

The pre-publication `compozy loop validate` response exposes validation status and
diagnostics, not a compiled definition. Exact seven-hook coverage and template
bindings receive review, while session admission and replay deduplication remain
guided E2E behaviors. Contract tests do not add an undeclared YAML parser or replace
those behaviors with source-text assertions.

Daemon-backed Loop validation requires a registered workspace because tool and agent
resolution are workspace-scoped. Tests resolve `BATUTA_TEST_WORKSPACE` first and emit a
clear prerequisite error when the repository is not registered; they do not silently
validate against an unrelated workspace.

The guided E2E smoke verifies live behavior:

- operator preference `auto_commit=false` reaches both child runs;
- one delivery terminal creates one queued/direct turn in the originating session;
- replay of the same terminal effect identity does not duplicate the turn;
- missing task dispatch is refused before a real run, while direct invalid submission
  never returns `done`;
- the extension has no watcher or reporting-agent runtime, and the terminal return
  spends no reporting-agent model tokens.

## Documentation

Both READMEs describe the composite `batuta-deliver` flow, the required original
session binding, dynamic provider discovery, native terminal effects,
registered-workspace test prerequisite, and the three published resources: `batuta`,
`batuta-routing`, and `batuta-deliver`. The historical 2026-08-11 design and
implementation plan are marked superseded where their two-Loop conversational dispatch
and resource inventory differ from the current architecture.

## Non-goals

- No external bridge notification.
- No subprocess extension or custom hook.
- No automatic re-dispatch from the terminal return prompt.
- No mutation of bundled `dev-cycle` Loops.
- No private database access.
- No attempt to replace CompozyOS session compaction or memory.
