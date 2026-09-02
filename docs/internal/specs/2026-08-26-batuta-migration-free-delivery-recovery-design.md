# Batuta migration-free delivery recovery — design

Status: approved on 2026-08-26

## Objective

Keep Batuta's automatic executor inventory, domain-by-complexity routing,
bounded fallback, review, publication, and verification loop without requiring
Batuta-specific Compozy database migrations. Compozy executes each Loop run;
Batuta owns delivery continuity across runs in its existing workspace journal.

## Decisions

- A stable Batuta `delivery_id` identifies the end-to-end delivery.
- Each recovery starts a new `batuta-deliver` run with a monotonically
  increasing `attempt`; Compozy `run_id` is intentionally not preserved.
- Every attempt reuses the same workspace, worktree, slug, task-set digest,
  origin session, routing generation, and absolute delivery budget.
- Completed task artifacts and commits remain in the worktree. A successor
  imports and executes only tasks that are still incomplete.
- Runtime rules are per-run data. Batuta never writes the user's stored Loop
  configuration to install or recover a routing matrix.
- Recovery is automatic on a recoverable failure. A human is contacted only
  for a genuine blocked or exhausted terminal state. Merge remains manual.
- Required hooks, revisioned Loop configuration, and same-lineage nested
  recovery are not Batuta prerequisites.

## Compozy boundary

The only new generic Compozy contract requested by this design is
`run-loop.params.config_overrides`. It accepts the same closed public
`LoopConfig` fields as a direct per-run override, rejects unknown fields during
Loop lint, preserves JSON-valued fields such as `enabled_checks_json`, and
applies the result only to the child run. It never persists the override.

Batuta consumes the upstream implementations of conjunctive runtime rules,
`reattempt_strategy: halt`, and idempotent Goal session cleanup. It does not
submit competing implementations for behavior owned upstream.

The final Compozy change must not modify schema definitions, generated SQL,
Atlas state, or any file under `internal/store/globaldb/schema/migrations`.
The Batuta release must use an officially published Compozy daemon and Go SDK
that contain every consumed contract; a local `replace`, pseudo-version, or
fork-only SDK is not release evidence.

## Compozy issue and pull-request gate

Before opening the Compozy pull request:

1. Create a dedicated Compozy `core/feature` issue describing child per-run
   overrides, examples, validation, ephemeral scope, and the Batuta use case.
2. Fetch upstream and update the fork's `main` to the exact upstream `main`.
3. Confirm that `main` contains the merged conjunctive routing change and the
   upstream `halt` and Goal-cleanup implementations from Pedro.
4. Create a clean feature branch from that updated `main`; do not reuse the
   experimental prerequisite branch.
5. Carry only the child `config_overrides` contract, its focused tests,
   generated catalog changes required by that contract, and documentation.
6. Require an empty migration diff and reference the feature issue from the
   pull request.

If an upstream implementation overlaps any local hunk, upstream owns the
behavior. Rebase, remove the duplicate, and retest instead of preserving the
local variant.

## Delivery identity and journal

Batuta persists one immutable delivery header and ordered attempt records in
its existing workspace-owned routing journal under the default
`$XDG_CACHE_HOME/batuta/routing/v1/<sha256(workspace_id)>.json` owner. The
workspace ID remains hashed in the filename; the file and directory retain
permissions `0600` and `0700`.

The `delivery_id` is the canonical `sha256:<64 lowercase hex>` digest of the
workspace ID, canonical worktree ID, slug, task-set digest, routing-generation
digest, origin session ID, and initial worktree fingerprint. Replaying
`apply_matrix` for the same immutable request therefore returns the same
delivery, while a changed task set or worktree state creates a different
identity. The delivery header contains:

- `delivery_id`, workspace ID, canonical worktree ID and root;
- slug, task-set digest, routing-generation digest, and origin session ID;
- initial creation time, absolute deadline, attempt ceiling, and token ceiling;
- the initial worktree state fingerprint.

Each attempt contains:

- positive attempt number and deterministic operation ID;
- canonical request digest;
- selected exact-ID fallback rules for incomplete tasks;
- state `planned`, `submitted`, or `terminal`;
- optional Compozy run ID and every discovered child run ID;
- start/terminal timestamps, terminal status, token usage, and worktree state
  fingerprint;
- safe blocker evidence when the attempt cannot continue.

The operation ID is derived from workspace ID, delivery ID, attempt number,
routing-generation digest, ordered incomplete task IDs, and their exact
runtime selections. Reuse with a different request digest is a conflict.

Journal writes remain under the existing workspace process mutex and file
lock. Records are immutable after terminal settlement except for the explicit
`planned -> submitted -> terminal` transition and safe evidence fields owned
by that transition.

The attempt owner holds that workspace file lock across loading the intent,
the bounded recent-run reconciliation, at most one external `loop run` call,
and recording the returned run ID. Concurrent callers therefore cannot both
observe `planned` with no run and start successors. A process crash releases
the lock; the next caller reconciles durable Compozy runs before deciding
whether a start is still absent.

## Ephemeral matrix application

`routing_plan` continues to collect live provider/catalog evidence, classify
tasks, select primary and fallback runtimes, and produce an immutable routing
generation.

`apply_matrix` no longer calls `loop config`, `loop configure`, or any config
CAS surface. It atomically archives the routing generation and delivery header
in Batuta's journal and returns the delivery identity plus budget. Read-back
means reloading the archived generation and comparing its digest; it never
means reading Compozy's stored Loop configuration.

The Batuta agent then calls the guarded `routing_apply` operation
`start_delivery`. That operation creates the deterministic attempt-1 journal
intent, reconciles any already-created matching run, starts at most one
`batuta-deliver` run, records its run ID, and returns it. Initial start and
recovery therefore use the same idempotent attempt owner; the agent never
invokes raw `loop run` for a Batuta delivery.

The guarded attempt owner starts `batuta-deliver` with these additional typed
inputs:

- `delivery_id`: opaque non-empty string, at most 128 bytes;
- `attempt`: integer from `1` through `4`;
- `absolute_deadline`: UTC RFC3339 timestamp;
- `token_ceiling`: integer fixed to `500_000_000` for this beta (CompozyOS sums the
  context each provider turn reports, so one medium task spends several million);
- `recovery_operation_id`: lowercase `sha256:<64 hex>` string, empty only on
  the initial attempt.

The Loop DSL has no arbitrary array input type, so runtime rules never cross a
string input or require a new Compozy input contract. Before implementation,
the parent calls a read-only `ext__batuta__routing_context` node with
`delivery_id`, `attempt`, slug, and routing-generation digest. The tool reloads
the workspace journal, verifies the exact tuple and task-set digest, and
returns ordered `runtime_rules`, `remaining_tokens`, and
`remaining_wall_seconds`.

The parent passes the direct typed reference
`{{ .nodes.routing_context.output.runtime_rules }}` to the `implement-tasks`
child through `params.config_overrides.runtime_rules`; the two numeric outputs
likewise populate its per-run token and wall limits. Direct node-output
references preserve the array and number values.

A recovery attempt advances the fallback only for a task with an observed
terminal failed execution output. An incomplete task that was never admitted
because its dependency failed retains its previous effective runtime. Exact-ID
rules for the failed tasks win over the original domain-and-complexity rules.
Review runtime selection remains independent.

## Attempt execution

The initial attempt uses the selected primary runtimes. `batuta-deliver` keeps
literal `auto_commit: true`, waits for `implement-tasks`, runs
`review-and-fix`, plans publication, performs the push and PR mutation when
publishable, and independently verifies the exact remote HEAD.

After a successful implementation child and before review, a read-only
`ext__batuta__delivery_budget_context` node reads that exact child status and
recomputes the remaining wall and token allowance without mutating the
journal. The review child receives those remaining values through its own
`config_overrides`. Both child identities and their terminal usage are recorded
during attempt settlement or the next reconciliation. This prevents the
implementation and review children from each receiving the full delivery
allowance independently.

Every attempt uses the same worktree. A terminal attempt records the exact
HEAD plus a digest of the porcelain and diff state. Recovery requires that the
current worktree fingerprint still equals the recorded terminal fingerprint.
This permits a failed executor's unchanged partial edits to be continued, but
rejects unrelated mutation between attempts.

The task loader derives the ordered incomplete set from the authored task
files. A task whose durable status is completed is not dispatched again.
Successful task commits therefore carry across attempts without Compozy run
lineage support.

## Recovery and idempotency

The terminal effect wakes the Batuta origin session with the completed run ID.
Batuta reads that exact run once, then calls the guarded `recover_delivery`
operation.

For a recoverable failure the tool:

1. loads the pinned delivery and routing generation;
2. verifies workspace, worktree fingerprint, task-set digest, terminal status,
   and accumulated budget;
3. derives the ordered incomplete tasks and their next unused fallbacks;
4. records a deterministic `planned` attempt before external mutation;
5. reconciles an existing successor or starts one new `batuta-deliver` run;
6. records its run ID as `submitted` and returns it to the Batuta agent.

A terminal outcome is recoverable only when the parent failed because its
`implement-tasks` child contains at least one exact failed task execution and
no publication mutation started. Review failure, configuration/authoring
failure, blocked publication, exhausted/canceled/stalled outcomes, and an
ambiguous remote effect stop without a successor.

Compozy `loop run` has no caller idempotency key. Therefore every successor
includes `delivery_id`, `attempt`, routing digest, and operation ID in its
inputs. When replaying a `planned` operation without a recorded run ID, Batuta
lists at most the 200 newest `batuta-deliver` runs and reads candidates durably
created no earlier than one minute before the journal's `planned` timestamp; a
non-zero start time must satisfy the same bound. Exactly
one matching input tuple is adopted. More than one match is `blocked` as
ambiguous. No match authorizes one start. A replay with a recorded run returns
that same run without starting another.

The Batuta agent ends the current turn after an accepted successor start. The
successor's own terminal effect resumes the same reconciliation loop. No shell
sleep, status polling loop, approval gate, or operator routing is introduced.

## Global budget and stop conditions

Budget belongs to `delivery_id` and never resets with a new Compozy run.

- Maximum attempts: `4`, including the initial attempt.
- Absolute wall-clock deadline: `4h` after the initial delivery creation.
- Cumulative token ceiling: `500_000_000` tokens across unique implementation
  and review child runs recorded by the delivery.
- A task may consume each declared fallback candidate at most once.

Before starting a successor, Batuta reads the recorded run set, accounts each
unique child run once, and computes remaining attempts, wall time, tokens, and
fallbacks. Missing or contradictory accounting fails closed.

The token value is an admission ceiling at child and attempt boundaries. A
provider turn already in flight may report usage that crosses the remaining
value; after that durable report Batuta admits no new child or successor. It
does not claim provider-side mid-turn token preemption.

Batuta starts no successor when:

- attempt, wall-clock, token, or fallback capacity is exhausted;
- no exact eligible runtime remains for any incomplete task;
- task-set digest, workspace identity, worktree identity, or worktree state
  changed unexpectedly;
- the routing generation is missing, foreign, or changed;
- the previous terminal outcome is not recoverable;
- reconciliation finds more than one matching successor;
- a remote publication mutation has an unresolved outcome.

The result is reported truthfully as `blocked` or `exhausted`; existing commits,
journal evidence, remote operation IDs, and worktree state remain intact.

## Migration removal

The migration-free implementation is built from a clean upstream Compozy
`main`, not by deleting SQL files from the experimental branch while retaining
their consumers.

Batuta removes every use of:

- revisioned Loop config reads and writes;
- `expected_revision` and configuration ownership CAS;
- `recover-nested` CLI/native/API surfaces;
- same-lineage recovery operation IDs and nested recovery status projections.

The experimental `00090_schema.sql` and `00091_nested_loop_recovery.sql`
commits are excluded from the clean Compozy branch. No downgrade migration is
needed because those migrations were never released as the Batuta platform
floor.

## Verification

### Compozy

- RED/GREEN proof that a parent can pass iteration, wall, budget, environment,
  runtime rules, and JSON-valued enabled checks to one child run.
- Lint rejects misspelled or unsupported override fields before a run starts.
- Direct-run behavior and child environment inheritance remain unchanged when
  `config_overrides` is absent.
- Focused Loop race tests, native catalog tests, `go vet`, code generation,
  and generated-diff checks pass.
- `git diff upstream/main -- internal/store/globaldb/schema/migrations` is
  empty.

### Batuta

- Applying a matrix never invokes `loop config` or `loop configure`.
- The read-only routing-context node materializes only the pinned attempt's
  rules, and those rules reach `implement-tasks` as typed per-run overrides.
- A controlled initial failure creates attempt 2 in the same worktree with a
  different Compozy run ID and the exact fallback runtime.
- Completed tasks and commits are not repeated; only incomplete tasks run.
- Review and publication continue in the successor attempt.
- Replay before and after successor submission returns the same run.
- A simulated ambiguous CLI outcome is reconciled by exact inputs.
- Attempt, deadline, token, fallback, drift, and ambiguity boundaries create no
  additional run.
- The full Go race suite, E2E assertions, staging contracts, and an isolated
  local Compozy integration pass.

Public release acceptance additionally requires an official Compozy/SDK pair,
a clean install from the built immutable generation, and a disposable real
remote smoke proving push, PR URL, and exact remote HEAD verification.

## Non-goals

- Preserving one Compozy run lineage across fallbacks.
- Batuta-specific Compozy persistence or migrations.
- Required hooks.
- Routine human approval.
- Automatic merge.
- Parallel task worktrees, deterministic commit integration, graph engineering,
  or interactive clarification. Those remain later increments.
