# Compozy Batuta Activation Prerequisites Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: use
> `superpowers:subagent-driven-development` or `superpowers:executing-plans`,
> and apply `superpowers:test-driven-development` to every task.

**Goal:** Finish the four unreleased Compozy contracts that Batuta needs to
activate autonomous publication and `domain × complexity` routing without
mutable operator cleanup, stale configuration writes, fresh recovery budgets,
or handwritten manifests.

**Architecture:** Preserve existing compatibility paths and add narrow owners:
the Go SDK owns an optional extension minimum-version override; the Loop store
owns monotonic config revisions and atomic CAS; the Loop aggregate owns one
daemon-resolved nested recovery transaction spanning the failed child and its
awaiting parent; and the embedded spec-cycle bundle owns a closed complexity
verification policy. Recovery runtime state is generation-scoped, stored with
the recovered child cell, and never changes workspace Loop configuration.

**Tech stack:** Go 1.26, SQLite/sqlc/Atlas, Gin/OpenAPI/Cobra, Compozy Loop v1,
embedded spec-cycle YAML/Markdown resources.

**Spec:**
`docs/internal/specs/2026-08-24-compozy-batuta-platform-prerequisites-design.md`

## Global constraints

- Implement only in the isolated Compozy worktree
  `/home/francisross/Projects/opensource/_worktrees/batuta-platform-prerequisites`.
- Required code-first hooks and conjunctive runtime rules are already complete
  on this branch; do not reopen them except to fix a directly exposed
  regression.
- Preserve legacy SDK minimum-version defaults and unconditional Loop config
  patch behavior for callers that omit new fields.
- `config_revision` is a monotonic `int64`: `0` means no stored override and
  an existing row starts at `1`. It is an opaque concurrency token to clients,
  not a configuration digest.
- Nested recovery accepts only trusted workspace, parent delivery run ID,
  required request ID, and exact runtime. The daemon derives child run, node,
  item, task metadata, current budgets, and failure state. No public caller can
  supply those identities.
- Recovery reuses the same parent and child run IDs, creates one new generation
  in each, carries successful child siblings, reruns the selected failed child
  item and its transitive dependents, and rebinds the parent run-loop cell to
  the same child. It must not start a replacement full child run.
- Recovery preserves the original run snapshots and monotonically accumulated
  token, wall-clock, and iteration accounting. Exhausted limits fail before
  either run is reactivated.
- The exact runtime is stored only for the selected child
  `run_id + generation + node_id + item_index`, has precedence over all normal
  runtime layers, and is reported with provenance `recovery`. It is never
  written into `loop_config` or the immutable definition snapshot.
- Replay of a request ID with the same canonical request digest returns the
  original result. Reuse with different content fails. The operation,
  generation rows, recovery runtime, both run transitions, and coordinator
  reservations commit or roll back together.
- Leave `TMPDIR` and `GOTMPDIR` unset for Compozy Go tests and `make gate*`.
- Do not tag, push, publish, or claim the Batuta floor is released in this plan.

---

### Task 1: Extension-specific minimum Compozy version

**Files:**

- Modify: `sdk/go/types.go`
- Modify: `sdk/go/extension_describe.go`
- Modify: `sdk/go/extension_test.go`
- Modify: `internal/extension/build_test.go`
- Modify: `packages/site/content/docs/extensions/develop.mdx`

**Interface:** add `MinCompozyVersion string` to `ExtensionDefinition`. Empty
retains the SDK constant; non-empty is trimmed, validated by the existing
manifest path, and emitted as `sdk.min_compozy_version`.

- [ ] **Step 1: Write RED tests**

Cover the historical default, explicit valid prerelease preservation,
whitespace trimming, invalid SemVer rejection in build, and generated-manifest
round-trip.

```bash
rtk go test ./sdk/go ./internal/extension -run 'MinCompozyVersion|Describe|Build' -count=1
```

- [ ] **Step 2: Implement the smallest SDK override**

Do not add a second manifest field or change the handshake contract. Describe
chooses the trimmed definition value when present and otherwise uses the SDK
constant. The existing builder remains the single validation/serialization
owner.

- [ ] **Step 3: Verify and commit**

```bash
rtk go test -race ./sdk/go ./internal/extension -run 'MinCompozyVersion|Describe|Build' -count=1
rtk go vet ./sdk/go ./internal/extension
rtk make codegen-check
rtk git diff --check
rtk git add sdk/go internal/extension packages/site/content/docs/extensions/develop.mdx
rtk git commit -m "feat: allow extension daemon version floor"
```

---

### Task 2: Read-only Loop config snapshot and atomic CAS

**Files:**

- Modify: `internal/loop/service_types.go`
- Modify: `internal/loop/service.go`
- Modify: `internal/loop/service_test.go`
- Modify: `internal/api/contract/loops.go`
- Modify: `internal/api/core/loop_interfaces.go`
- Modify: `internal/api/core/loop_errors.go`
- Modify: `internal/api/core/loops_definitions.go`
- Modify: `internal/api/core/loops_test.go`
- Modify: `internal/daemon/loop_api_runs.go`
- Modify: `internal/daemon/loop_api_runs_test.go`
- Modify: `internal/daemon/native_loop_tools.go`
- Modify: `internal/daemon/native_loop_tools_support.go`
- Modify: `internal/daemon/native_loop_tools_test.go`
- Modify: `internal/tools/builtin/loops.go`
- Modify: `internal/cli/loop.go`
- Modify: `internal/cli/loop_client.go`
- Modify: `internal/cli/client_loops.go`
- Modify: `internal/cli/loop_test.go`
- Modify: `internal/store/globaldb/schema/definitions/50_loops.sql`
- Create: `internal/store/globaldb/schema/migrations/00090_loop_config_revision.sql`
- Modify: `internal/store/globaldb/schema/migrations/atlas.sum`
- Modify: `internal/store/globaldb/queries/loop_core.sql`
- Regenerate: `internal/store/globaldb/sqlcgen/*`
- Modify: `internal/store/globaldb/global_db_loop.go`
- Modify: `internal/store/globaldb/global_db_loop_config.go`
- Modify: `internal/store/globaldb/global_db_loop_test.go`
- Modify: `internal/store/globaldb/global_db_loop_schema_integration_test.go`
- Modify: OpenAPI/generated API artifacts selected by canonical codegen
- Modify: `internal/api/spec/loops.go`
- Modify: `packages/site/content/docs/loops/configure.mdx`

**Domain interfaces:**

```go
type ConfigSnapshot struct {
    Stored    *LoopConfig
    Effective EffectiveConfig
    Revision  int64
}

type LoopConfigRevisionStore interface {
    GetStoredLoopConfigSnapshot(context.Context, WorkspaceID, string) (StoredLoopConfigSnapshot, error)
    CompareAndSwapLoopConfig(context.Context, WorkspaceID, string, int64, LoopConfig) (StoredLoopConfigSnapshot, error)
}

var ErrConfigRevisionConflict = errors.New("loop: config revision conflict")

type ConfigRevisionConflictError struct {
    Expected int64
    Current  int64
}
```

`ConfigRevisionConflictError` implements `Error()` and `Unwrap()` to
`ErrConfigRevisionConflict`. The HTTP operation advertises a 409 response whose
safe body contains expected and current revisions; `respondLoopError` maps the
typed domain conflict to it.

Add an explicit narrow `LoopConfigRevisionStore` seam for stored-snapshot reads
and CAS; do not widen the coordinator-facing base `Store` interface. Production
globaldb implements both interfaces, while service paths return a typed missing
dependency if the revision seam is unavailable. Keep
`UpsertLoopConfig` as the legacy compatibility surface, implemented through
the same revision-incrementing transaction with no expected value. Add a service mutation
that accepts `expectedRevision *int64`; `nil` delegates to current patch
semantics, while a value uses the atomic CAS path.
Update only service fakes that exercise config snapshots/CAS; coordinator and
unrelated store fakes must remain source-compatible.

The public wire contract is:

```go
type LoopConfigResponse struct {
    Config          *LoopConfig         `json:"config"`
    EffectiveConfig LoopEffectiveConfig `json:"effective_config"`
    ConfigRevision  int64               `json:"config_revision"`
}

type PutLoopConfigRequest struct {
    Config           LoopConfig `json:"config"`
    ExpectedRevision *int64     `json:"expected_revision,omitempty"`
}

type LoopConfigRevisionConflictResponse struct {
    Error            string `json:"error"`
    ExpectedRevision int64  `json:"expected_revision"`
    CurrentRevision  int64  `json:"current_revision"`
}
```

- [ ] **Step 1: Write store and migration RED tests**

Cover no row=`0`, migrated row=`1`, first CAS insert from `0`, update from `N`
to `N+1`, stale writer conflict with current revision, unchanged patch keeping
the same revision, rollback on validation/storage failure, and legacy nil-CAS
patch compatibility, including equivalence with
`loopConfigPatchFlagsForStore`/`upsertLoopConfigWithExecutor` in
`global_db_loop_config.go`. Reject `expected_revision < 0` in contract conversion and
the service before store access. Prove an empty read causes no
row/event/revision change.

```bash
rtk go test ./internal/store/globaldb ./internal/loop -run 'LoopConfig.*Revision|ConfigSnapshot|CompareAndSwap' -count=1
```

- [ ] **Step 2: Implement the transactional owner**

The migration gives all existing rows revision `1`; the canonical definition
uses `revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)`. Inside one
immediate transaction, CAS loads current stored config/revision, rejects a
stale expected value before merging, applies the same fieldwise patch semantics
as the legacy path, validates the complete stored result, and writes the full
row with an increment only when canonical content changed. For expected `0`,
insert only if no row exists. Return the winning stored snapshot from that
transaction. Never emulate CAS with a service-level GET followed by PUT.
Regenerate and commit Atlas `atlas.sum` as well as sqlc output.

- [ ] **Step 3: Write API and CLI RED tests**

Cover JSON names and zero revision, HTTP 409 with safe expected/current
revision metadata, legacy PUT, and these exact commands:

```text
compozy loop config --workspace <ref> --name <loop> -o json
compozy loop configure --workspace <ref> --name <loop> --expected-revision <N> --file <path> -o json
```

`loop config` is read-only and has no file/set flags. Reject negative or
malformed expected revisions before network access.

Extend `compozy__loop_configure` through its descriptor, input schema, daemon
binding, and handler to accept the same optional non-negative
`expected_revision`. Omission remains legacy unconditional patch behavior.
This closes the native writer as well as HTTP/CLI; no public config writer may
silently discard the concurrency token when it is supplied.

- [ ] **Step 4: Implement API/CLI, regenerate, verify, commit**

```bash
rtk go test -race ./internal/loop ./internal/store/globaldb ./internal/api/core ./internal/daemon ./internal/cli -run 'LoopConfig|ConfigSnapshot|Revision' -count=1
rtk go vet ./internal/loop ./internal/store/globaldb ./internal/api/core ./internal/daemon ./internal/cli
rtk make codegen
rtk make codegen-check
rtk git diff --check
rtk git add internal packages/site/content/docs/loops/configure.mdx
rtk git commit -m "feat: add revisioned loop configuration"
```

---

### Task 3: Same-lineage nested child recovery with ephemeral runtime

**Files:**

- Modify: `internal/loop/timetravel_types.go`
- Modify: `internal/loop/service_types.go`
- Create: `internal/loop/service_nested_recovery.go`
- Create: `internal/loop/service_nested_recovery_test.go`
- Create: `internal/loop/nested_recovery_plan.go`
- Create: `internal/loop/nested_recovery_plan_test.go`
- Modify: `internal/loop/generation_intent.go`
- Modify: `internal/loop/generation_snapshot_test.go`
- Modify: `internal/loop/action_types.go`
- Modify: `internal/loop/action_runtime_helpers.go`
- Modify: `internal/loop/action_runagent.go`
- Modify: `internal/loop/runtime_types.go`
- Modify: `internal/loop/runtime_resolve.go`
- Modify: `internal/loop/runtime_resolve_test.go`
- Modify: `internal/loop/coordinator_action_input.go`
- Modify: `internal/loop/coordinator.go`
- Modify: `internal/loop/coordinator_options.go`
- Modify: `internal/loop/coordinator_outputs.go`
- Modify: `internal/loop/run_read_service.go`
- Modify: `internal/loop/coordinator_test.go`
- Modify: `internal/loop/effect_context_test.go`
- Modify: `internal/api/contract/loop_timetravel.go`
- Modify: `internal/api/contract/loop_runs.go`
- Modify: `internal/api/contract/loop_enums.go`
- Modify: `internal/api/contract/contract_test.go`
- Modify: `internal/api/core/loop_interfaces.go`
- Modify: `internal/api/core/loop_errors.go`
- Modify: `internal/api/core/loops_timetravel.go`
- Modify: `internal/api/core/loops_test.go`
- Modify: `internal/api/spec/loops.go`
- Modify: `internal/api/httpapi/loops_routes.go`
- Modify: `internal/api/udsapi/loops_routes.go`
- Modify: `internal/daemon/loop_api_timetravel.go`
- Modify: `internal/daemon/loop_api_contract.go`
- Modify: `internal/daemon/loop_api_payloads.go`
- Modify: `internal/daemon/task_runtime_boot_roles.go`
- Modify: `internal/daemon/task_runtime_boot_roles_test.go`
- Modify: `internal/daemon/loop_api_runs_test.go`
- Modify: `internal/daemon/native_loop_timetravel_tools.go`
- Modify: `internal/daemon/native_loop_tools_support.go`
- Modify: `internal/daemon/native_loop_tool_bindings.go`
- Modify: `internal/daemon/native_loop_tools_test.go`
- Modify: `internal/tools/builtin_loop_ids.go`
- Modify: `internal/tools/builtin/loop_timetravel.go`
- Modify: `internal/cli/loop_timetravel.go`
- Modify: `internal/cli/loop.go`
- Modify: `internal/cli/loop_client.go`
- Modify: `internal/cli/client_loop_timetravel.go`
- Modify: `internal/cli/loop_test.go`
- Modify: `internal/cli/helpers_test.go`
- Modify: `internal/store/globaldb/schema/definitions/50_loops.sql`
- Create: `internal/store/globaldb/schema/migrations/00091_nested_loop_recovery.sql`
- Modify: `internal/store/globaldb/schema/migrations/atlas.sum`
- Modify: `internal/store/globaldb/queries/loop_core.sql`
- Regenerate: `internal/store/globaldb/sqlcgen/*`
- Create: `internal/store/globaldb/global_db_loop_nested_recovery.go`
- Create: `internal/store/globaldb/global_db_loop_nested_recovery_test.go`
- Modify: `internal/store/globaldb/global_db_loop_schema_integration_test.go`
- Create: `internal/daemon/loop_nested_recovery_integration_test.go`
- Modify: OpenAPI/generated API artifacts
- Modify: `packages/site/content/docs/loops/time-travel.mdx`

**Public contract:**

```go
type RecoverNestedLoopRequest struct {
    RequestID string          `json:"request_id"`
    Runtime   LoopRuntimeSpec `json:"runtime"`
}

type RecoverNestedLoopResponse struct {
    OperationID     string              `json:"operation_id"`
    ParentRunID     string              `json:"parent_run_id"`
    ParentGeneration int64              `json:"parent_generation"`
    ChildRunID      string              `json:"child_run_id"`
    ChildGeneration int64               `json:"child_generation"`
    TaskID          string              `json:"task_id"`
    Runtime         LoopResolvedRuntime `json:"runtime"`
    Replayed        bool                `json:"replayed,omitempty"`
}

type LoopNestedRecoveryPayload struct {
    OperationID      string              `json:"operation_id"`
    ParentRunID      string              `json:"parent_run_id"`
    ParentGeneration int64               `json:"parent_generation"`
    ChildRunID       string              `json:"child_run_id"`
    ChildGeneration  int64               `json:"child_generation"`
    TaskID           string              `json:"task_id"`
    Runtime          LoopResolvedRuntime `json:"runtime"`
}
```

The route/native tool/CLI accepts a parent run ID and the request body above.
The runtime requires exact non-empty provider and model; reasoning/speed are
optional but validated. Request ID is required. Node ID, item index, child ID,
task metadata, budgets, config rules, and source generation are never public
inputs.

`LoopRunResponse` exposes an ordered `nested_recoveries` array for operations
where the requested run is the parent or child. This is the authoritative
status projection Batuta uses to reconcile operation ID, generations, task,
and resolved recovery runtime after restart.

- [ ] **Step 1: Write pure lineage and generation-planning RED tests**

Cover deterministic graph-order selection of the first recoverable failed
awaited child and the first failed child item, rejection of foreign/unsettled/
non-`run-loop`/non-`run-agent` lineage, and rejection when no recovery target
exists. Both parent and child must be terminal and recoverable, share the
trusted workspace and direct ownership lineage, and the parent's failed output
must still point to that exact child. Child planning must carry successful siblings, reset only the selected
cell plus transitive dependents, and preserve task ordering. Parent planning
must carry its prior outputs, put the owning run-loop cell back into
`awaiting_child` with the same child ID, and reset only its transitive
dependents; it must never execute the run-loop cell again.

```bash
rtk go test ./internal/loop -run 'NestedRecovery|RecoveryGeneration' -count=1
```

- [ ] **Step 2: Add the recovery runtime layer RED tests**

Add `RuntimeSourceRecovery`. A generation-cell lookup is performed while the
coordinator builds `ActionExecutionInput`; only an exact coordinate match adds
the recovery runtime. Resolution applies it after the normal run layer and
records per-field `recovery` provenance. Tests prove it beats matrix,
frontmatter, input, and per-run fields; does not leak to another item,
generation, run, or workspace; and survives daemon restart/readback.
Inject the workspace-scoped recovery-runtime reader explicitly through
`CoordinatorRunner`, a typed coordinator option, and
`task_runtime_boot_roles.go`; do not let action code reach around the
coordinator to a global store. A boot test must prove the production daemon
passes the real store-backed reader, not only that unit construction can do so.

- [ ] **Step 3: Write atomic store and budget-preflight RED tests**

Before production transaction code, cover successful two-run commit, every
individual write failure rolling back both runs and recovery state, concurrent
same/different request IDs, same-key replay, key reuse with a different digest,
full carried-output fidelity, terminal/ownership changes between service
planning and transaction, and iteration/token/deadline rejection before either
reactivation. Prove an accepted request produces exactly two next-generation
coordinator reservations and a replay produces none.

```bash
rtk go test ./internal/loop ./internal/store/globaldb -run 'NestedRecovery|RecoveryBudget|RecoveryTransaction' -count=1
```

- [ ] **Step 4: Implement one atomic two-run recovery transaction**

Add a `NestedRecoveryStore` operation that receives already validated parent
and child plans plus the exact coordinate/runtime. In one immediate transaction:

1. replay-check the required request ID and canonical digest;
2. reload and compare both current run generations, terminal recoverable
   statuses, trusted workspace, direct parent/child ownership, and the parent's
   exact child pointer;
3. run one shared budget preflight before reactivation: next generation versus
   each original iteration cap, accumulated tokens versus each token ceiling,
   and wall-clock deadline derived from each original `StartedAt`; also reject
   cancellation/pause conflicts without resetting counters;
4. insert `nested_recovery` generation intents for the existing child and
   parent run IDs;
5. insert child carried/pending outputs and the exact recovery runtime row;
6. insert parent carried/awaiting-child/pending outputs;
7. reactivate both runs and reserve each next coordinator using existing
   generation fencing;
8. append typed status/generation events and persist the operation.

Any failed write rolls back both runs and the override. The service validates
the exact runtime against the live workspace catalog and derives the target
task identity through the same namespace/item helper used by run-agent
resolution, avoiding a second parser. A replay returns the original resolved
target/runtime even after later generations exist.

The canonical schema adds `nested_recovery` to both the generation-origin and
time-travel-operation checks, and adds `loop_nested_recoveries` keyed by
`workspace_id + operation_id`. That row stores parent/child run IDs and new
generations, derived parent/child node and item coordinates, derived task ID,
validated exact runtime JSON, and creation time. It has a unique child
generation-cell constraint and foreign keys to both existing runs. The
coordinator lookup is workspace scoped and keyed only by
`child_run_id + child_generation + node_id + item_index`. Migration `00091`
rebuilds SQLite tables whose `CHECK` vocabulary changes and proves all prior
`rerun`/`fork` rows survive byte-for-byte. The required `request_id` and its
canonical request digest live in `loop_timetravel_ops`; preserve and recreate
the existing `uq_loop_timetravel_ops_idempotency` unique partial index during
the table rebuild, and make
`loop_nested_recoveries` reference the operation row. Replay lookup and insert
therefore share the same immediate transaction and cannot create duplicate
recovery rows.
For this operation, the generic row uses the parent as both source and result,
the old/new parent generations as source/result generations, and the derived
parent run-loop coordinate as `from_node + item_index`; the dedicated recovery
row owns all child coordinates and runtime evidence.

Do not reuse `insertTimeTravelOutputs`: its projection omits
`child_loop_run_id` and resolved runtime. The nested transaction gets a
dedicated full-output insertion helper that writes every durable
`loop_generation_outputs` column required by carried child evidence and the
parent's `awaiting_child` binding.

- [ ] **Step 5: Write API/native/CLI and integration RED tests, then implement**

Cover strict JSON, missing request ID, malformed runtime, wrong workspace,
foreign child, exhausted budget, concurrent recovery conflict, replay/key
reuse, cancellation propagation, and zero mutation on every rejection. The
CLI form is fixed and structured:

```text
compozy loop recover-nested --workspace <ref> --run <parent-id> --request-id <id> --runtime-file <internal-json> -o json
```

The native tool schema exposes the same closed inputs. Integration starts a
real parent `run-loop` over a fan-out child, settles one sibling successfully
and one unsuccessfully, recovers twice with two exact runtimes, and proves:

- parent and child run IDs never change;
- successful siblings carry and only failed cells/dependents rerun;
- stored Loop config and executed definition digests do not change;
- accumulated budget usage is monotonic and original ceilings remain;
- `runtime_applied` reports exact runtime with recovery provenance;
- parent continues after child settlement;
- terminal effects for both settlements carry distinct existing
  `effect.identity.generation` values and fire once each;
- replay starts no extra coordinator/task/session.
- status reads for both parent and child expose the same ordered recovery
  operation ID and resolved runtime snapshot.

- [ ] **Step 6: Verify and commit**

```bash
rtk go test -race ./internal/loop ./internal/store/globaldb ./internal/api/core ./internal/daemon ./internal/cli -run 'NestedRecovery|RecoveryRuntime|EffectContext' -count=1
rtk go vet ./internal/loop ./internal/store/globaldb ./internal/api/core ./internal/daemon ./internal/cli
rtk make codegen
rtk make codegen-check
rtk git diff --check
rtk git add internal packages/site/content/docs/loops/time-travel.mdx
rtk git commit -m "feat: recover nested loop items in lineage"
```

---

### Task 4: Closed complexity verification policy in spec-cycle

**Files:**

- Modify: `extensions/spec-cycle/import_tasks.go`
- Modify: `extensions/spec-cycle/import_tasks_parser.go`
- Modify: `extensions/spec-cycle/import_tasks_test.go`
- Modify: `extensions/spec-cycle/schemas.go`
- Modify: `extensions/spec-cycle/extension.json`
- Modify: `extensions/spec-cycle/loops/implement-tasks/loop.yaml`
- Modify: `extensions/spec-cycle/loops/review-and-fix/loop.yaml`
- Modify: `extensions/spec-cycle/agents/code_implementer/AGENT.md`
- Modify: `extensions/spec-cycle/agents/reviewer/AGENT.md`
- Modify: `extensions/spec-cycle/skills/cy-execute-task/SKILL.md`
- Create: `extensions/spec-cycle/skills/cy-execute-task/references/complexity-verification.md`
- Modify: `extensions/spec-cycle/embed_test.go`
- Modify: `packages/site/content/docs/loops/extensions.mdx`

**Policy:**

| Complexity | Required minimum evidence | Review posture |
| --- | --- | --- |
| `low` | focused tests for each changed surface plus applicable format/lint | explicit self-review |
| `medium` | `low` plus owning package/suite and applicable static analysis | self-review plus contract-parity check |
| `high` | `medium` plus applicable race/integration and cross-surface regression checks | independent review required |
| `critical` | `high` plus the repository's affected system/gate | independent review plus final contract-parity review required |

Repository instructions and authored validation choose concrete commands.
Inapplicable checks require bounded evidence. Missing/unknown complexity is a
contract error at task import and the JSON schema uses the closed enum;
review-only legacy callers that omit complexity default to `medium` for
compatibility.

`review-and-fix` reviews the identifier supplied as `task_name`. Its new
`complexity` input is the exact complexity of that target: a single-task caller
passes that task's value; the Batuta parent deliberately treats the whole slug
as one delivery review target and passes the deterministic highest complexity
returned by `load_check`. The review Loop never guesses or reloads a different
task set. Here, independent means a new isolated reviewer session with no
implementation-session history; this remains true even when a caller selects
another valid reviewer agent. It is not a claim about a provider/model tier.
The Loop input is declared exactly as `type: string`,
`enum: [low, medium, high, critical]`, `default: medium`; unknown values fail
normal Loop input validation before a reviewer session starts.

- [ ] **Step 1: Write RED contract tests**

Add table tests for all four rows, unknown/missing imported values,
applicable/inapplicable evidence language, and high/critical independent-review
requirements. Assert `implement-tasks` renders each item's exact complexity in
the implementer prompt. Extend importer output with deterministic
`highest_complexity` so a parent delivery can pass one validated value to
`review-and-fix`; assert `critical > high > medium > low` independent of task
order and compute it across every authored task file, including completed tasks
that are omitted from the executable `tasks` array. Assert review prompt and reviewer agent consume the input, with default
`medium` only for legacy direct review calls. An integration assertion proves
the review session ID differs from every implementation session ID; embedded
contract tests keep `session.isolated: true`.

Keep the embedded manifest and runtime descriptor identical:
`extension.json` requires `tasks`, `count`, and `highest_complexity`, uses the
four-value enum for every item complexity, and declares the same enum for
`highest_complexity`.

```bash
rtk go test ./extensions/spec-cycle -run 'Complexity|Implementer|ReviewAndFix|ImportTasks' -count=1
```

- [ ] **Step 2: Implement the embedded policy**

Keep the closed table in one reference file and make the agent/skill/Loop
prompts refer to its exact obligations. `cy-execute-task` adds the selected row
to its checklist, refuses completion without required evidence, and records
why a check is inapplicable. `code_implementer` repeats the floor so execution
does not depend on optional skill availability. `review-and-fix` and the
reviewer require independent review for high/critical; critical additionally
requires final contract-parity review after remediation. Do not add a
per-cell retry setting or select a model in this policy.

- [ ] **Step 3: Verify and commit**

```bash
rtk go test -race ./extensions/spec-cycle -count=1
rtk go vet ./extensions/spec-cycle
rtk git diff --check
rtk git add extensions/spec-cycle packages/site/content/docs/loops/extensions.mdx
rtk git commit -m "feat: enforce complexity verification policy"
```

---

### Task 5: Cross-contract verification, docs, and release readiness

**Files:**

- Modify: relevant public reference/configuration/Loop/extension docs
- Create: `.superpowers/sdd/2026-08-25-compozy-batuta-activation-prerequisites/*`
- Create: one final QA report under that SDD directory

- [ ] **Step 1: Run focused cross-contract verification**

```bash
rtk go test -race ./sdk/go ./internal/extension ./internal/loop ./internal/store/globaldb ./internal/api/core ./internal/daemon ./internal/cli ./extensions/spec-cycle -count=1
rtk go vet ./sdk/go ./internal/extension ./internal/loop ./internal/store/globaldb ./internal/api/core ./internal/daemon ./internal/cli ./extensions/spec-cycle
rtk make codegen-check
rtk git diff --check
```

- [ ] **Step 2: Run repository gates with required temp semantics**

Leave `TMPDIR` and `GOTMPDIR` unset:

```bash
rtk make gate
rtk make gate-full
```

Record pre-existing or external failures exactly; do not convert a branch-owned
failure into `blocked-verify`.

- [ ] **Step 3: Independent review and final commit**

Request a read-only review covering migration/CAS atomicity, two-run recovery
fencing and budgets, runtime-overlay isolation, API closure, generated drift,
and all four complexity rows. Address every branch-owned finding with
RED/GREEN evidence, rerun affected checks, and commit only after the review is
approved.

## Stop conditions

- Stop Task 2 before production edits if an atomic SQLite CAS cannot preserve
  legacy patch semantics; revise the store design, never fake it with GET+PUT.
- Stop Task 3 before production edits if the plan would start a replacement
  full child run, reset any budget counter/deadline, accept caller-selected
  lineage, or persist fallback in Loop config.
- Stop release readiness if either generated artifacts drift, a stale CAS
  mutates state, a rejected recovery changes either run, an override leaks
  outside its exact generation cell, a second settlement is deduplicated, or
  any complexity row is merely advisory.
- A clean implementation is still not a published platform floor. Tagging,
  SDK publication, and push require separate explicit authority.
