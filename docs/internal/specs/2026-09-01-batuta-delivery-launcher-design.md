# Batuta delivery launcher — design

Status: approved by the operator on 2026-09-01

## Objective

Make `start_delivery` return a durable Compozy Loop run within its existing
bounded command deadline without changing Compozy source, configuration, or
runtime contracts.

The fix stays entirely inside Batuta. It introduces a lightweight launcher
Loop as the public `batuta-deliver` entrypoint and moves the existing delivery
graph behind that launcher as `batuta-deliver-core`.

## Incident and responsible mechanism

The failed delivery persisted attempt 1 as `planned`, but never persisted a
Compozy run ID. Read-only reproduction established that:

- listing recent `batuta-deliver` runs completes in about 0.3 seconds;
- resolving the current `batuta-deliver` definition takes about 18 seconds;
- `compozy loop run --dry-run` remains unresolved beyond 45 seconds and creates
  no run;
- Batuta and the calling tool boundary both use a 30-second deadline;
- the current definition contains many Batuta extension actions whose schemas
  and availability are resolved during Loop preparation.

Increasing Batuta's internal timeout alone cannot work because the caller
cancels at the same boundary. Releasing the delivery journal lock alone also
does not remove definition resolution from the active Batuta tool call. A
durable Batuta-only fix must make the Loop created by `start_delivery` cheap to
resolve and defer the extension-heavy graph until after that call returns.

## Non-negotiable decisions

- No Compozy repository, configuration, database, or runtime mutation is part
  of this change.
- `start_delivery` remains idempotent and returns one authoritative parent run
  ID; it does not return an untracked background operation.
- Compozy remains the durable supervisor. Batuta does not introduce a detached
  goroutine, private job queue, or fire-and-forget subprocess.
- The existing workspace journal remains the only Batuta delivery state.
- The current plan -> recent-run reconciliation -> start -> submitted state
  machine remains intact.
- The journal lock remains held across the bounded launcher start so concurrent
  Batuta instances cannot create duplicate parents.
- Existing submitted runs created from the legacy direct graph remain
  reconcilable.
- Merge remains manual and all existing delivery, review, publication, budget,
  and cleanup guarantees remain unchanged.

## Architecture

The public entrypoint becomes a small durable envelope:

```text
start_delivery tool call
  -> journal attempt planned
  -> compozy loop run batuta-deliver
       -> lightweight launcher run created
  -> journal attempt submitted with launcher run ID
  -> start_delivery returns and releases the journal lock
  -> launcher asynchronously starts batuta-deliver-core
       -> existing extension-heavy delivery graph
       -> task waves -> final review -> publication -> cleanup
  -> launcher reaches the child's terminal outcome
  -> launcher terminal hook prompts the origin session
  -> reconcile_fallbacks validates launcher and exact core child
```

`batuta-deliver` contains no open extension action kind. Its executable graph
contains one reserved `run-loop` action targeting `batuta-deliver-core` and the
minimum reserved control/transform nodes needed to expose a closed terminal
contract. This keeps Batuta schema/availability resolution out of the active
`start_delivery` extension call.

`batuta-deliver-core` is the current `batuta-deliver` graph under an internal
name. Its task-wave, review, publication, terminalization, and cleanup behavior
is preserved. It has no origin-session terminal hooks; the launcher owns those
hooks so the run ID presented back to Batuta and the operator remains the
journaled launcher ID.

## Inputs and budget propagation

The launcher receives every immutable identity already supplied to the current
delivery graph:

- `delivery_id`;
- `attempt`;
- `slug`;
- `origin_session_id`;
- `worktree_ref`;
- `routing_generation`;
- `absolute_deadline`;
- `token_ceiling`;
- `recovery_operation_id`.

Batuta also supplies `delivery_envelope_version: 1` as a fixed protocol marker.
The launcher forwards it unchanged to the core. This field is not operator
configurable: it distinguishes launcher-based runs from pinned legacy direct
runs without guessing from graph shape or missing outputs.

It also receives the Batuta-computed per-attempt values currently written only
to the CLI config file:

- `iteration_cap`;
- `budget_tokens`;
- `budget_wall_seconds`.

The existing secure config file still bounds the launcher. The launcher passes
the three explicit values to `batuta-deliver-core` through child-scoped
`config_overrides`, with `budget_on_exceeded: halt` and
`reattempt_strategy: halt`. The core receives the original immutable inputs
unchanged. No stored Loop configuration is written.

Batuta validates and includes the protocol marker and new scalar inputs in its
exact recent-run matching. A launcher run missing or contradicting any
identity, marker, or budget field cannot be adopted as the submitted launcher.

## Run identity and reconciliation

The delivery journal continues to store the public `batuta-deliver` launcher
run ID in `DeliveryAttempt.RunID`. Batuta does not trust a caller-supplied core
run ID.

For a launcher-based attempt, reconciliation:

1. reads the exact submitted launcher run;
2. validates workspace, loop name, terminal state, inputs, attempt, and
   recovery identity against the journal;
3. requires exactly one settled launcher output for the authored core node;
4. obtains exactly one non-empty child run ID from that output;
5. reads that child once and requires:
   - loop name `batuta-deliver-core`;
   - `parent_loop_run_id` equal to the launcher run ID;
   - the same workspace and immutable delivery inputs;
   - a terminal state compatible with the launcher's outcome;
6. applies the existing graph usage, fallback, publication, worktree, and
   terminal evidence logic to the validated core detail;
7. persists the same delivery attempt transitions and returns the launcher run
   ID in the public result.

Missing, duplicate, foreign, nonterminal, or contradictory core evidence fails
closed without starting a fallback.

For backward compatibility, a submitted `batuta-deliver` run without
`delivery_envelope_version` is treated as a legacy direct delivery run and
follows the current reconciliation path. Any present value other than `1` is
an unsupported protocol and fails closed. Pinned historical definitions remain
authoritative for legacy runs.

## Lifecycle and terminal behavior

The launcher owns `on_done`, `on_noop`, `on_blocked`, `on_failed`,
`on_exhausted`, `on_stalled`, and `on_canceled`. Each hook queues the existing
idempotent terminal message to `origin_session_id` and names the launcher run
ID.

The core owns no origin-session hook. It remains responsible for truthful graph
terminalization before its Loop outcome settles. The awaited launcher mirrors
that outcome and becomes the sole public notification boundary.

If the launcher cannot create the core child, it terminates through its normal
failed action path. Reconciliation reports missing core evidence as a closed
delivery conflict. It does not invent a child identity or start a second child.

## Cancellation and locking

The existing 30-second CLI command bound remains. The expected fix is a smaller
entrypoint, not a larger timeout that the caller cannot honor.

`start_delivery` continues to hold the workspace journal lock while it
reconciles recent launcher runs and creates at most one launcher. Because the
launcher definition has no Batuta extension action, that start cannot require
the same journal or the Batuta tool runtime before returning its durable run
ID. The lock is released before `batuta-deliver-core` executes
`routing_context` or any other Batuta action.

## Current planned attempt and rollout

The incident delivery is already `active` with attempt 1 in `planned` state and
no run ID. The rollout must not edit or delete that journal entry.

After the fixed extension generation is active, replaying `start_delivery` for
the same delivery ID reuses the existing deterministic attempt, finds no
matching launcher, starts exactly one launcher, and records it as submitted.
The normal recent-run reconciliation still covers a lost launcher response.

No automatic extension reload, delivery retry, publication, or release is part
of implementation verification. Those are separate operator-approved actions
after the code and tests pass.

## Code boundaries

Expected Batuta changes are limited to:

- replace `loops/batuta-deliver/loop.yaml` with the lightweight launcher;
- move the current graph to `loops/batuta-deliver-core/loop.yaml` and change
  only its internal name and terminal-hook ownership;
- extend the delivery start request/response validation for launcher budget
  inputs;
- add launcher-to-core validation at the existing reconciliation boundary;
- update Batuta contract tests and documentation that enumerate shipped Loops
  or authoritative run identity.

No unrelated routing, inventory, worktree, publication, provider, or model
selection refactor is authorized.

## Test strategy

Implementation follows test-first red/green cycles.

### Loop contract regression

A contract test must fail against the current definition and prove that:

- public `batuta-deliver` contains no `ext__*` action;
- it targets exactly `batuta-deliver-core` through one awaited `run-loop`;
- it forwards the protocol marker, every immutable identity, and exact budget
  override;
- only the launcher owns origin-session terminal hooks;
- the core retains the existing delivery graph nodes and does not gain a second
  publication or notification boundary.

The production change caught by this test is reintroducing an extension-heavy
public start path.

### Delivery client regression

Focused tests must prove exact argv, secure config behavior, new input
serialization, strict response matching, cancellation propagation, and
rejection of a launcher whose budget or identity differs.

The production change caught by these tests is starting or adopting a run that
does not represent the journaled attempt exactly.

### Reconciliation regression

Service tests must cover:

- one valid launcher and one exact core child;
- missing, duplicate, foreign, wrong-parent, wrong-loop, nonterminal, and
  contradictory child evidence;
- terminal success and each recoverable/non-recoverable core outcome;
- legacy direct-run compatibility;
- rejection of any unsupported present `delivery_envelope_version`;
- replay after a lost launcher response;
- concurrent service instances still producing one launcher start.

The production change caught by these tests is trusting an unproven child or
breaking idempotent/legacy delivery recovery.

### Verification gates

Run focused race tests for delivery client, recovery, graph, and contract
surfaces first. Then run the repository's nearest Batuta gate from a checkout
allowed by `CLAUDE.md`; never run `tests/contract/run.sh` from a checkout that
contains `.compozy/`.

## Acceptance criteria

- `start_delivery` creates and returns one durable launcher run within the
  existing command deadline in the supported live verification environment.
- The journal transitions `planned -> submitted` with that launcher run ID.
- The heavy graph begins only as the launcher's exact child after the tool call
  can release its journal lock.
- Reconciliation proves launcher/core lineage before consuming graph evidence.
- Legacy submitted runs remain reconcilable.
- No Compozy source or configuration changes exist in the diff.
- Focused race tests and the nearest repository gate pass.
- No live extension reload or delivery retry occurs without separate operator
  approval.

## Non-goals

- Optimizing Compozy Loop compilation, tool registry projection, or extension
  availability checks.
- Changing Compozy tool-call deadlines.
- Replacing the CLI boundary with a private HTTP/UDS client.
- Adding an extension-owned scheduler or background worker.
- Editing the incident journal by hand.
- Retrying the incident delivery during implementation.
- Publishing a Batuta release or opening a pull request.
