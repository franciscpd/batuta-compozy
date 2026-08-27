# Batuta migration-free delivery continuity — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILLS: Use `superpowers:test-driven-development` for each task, `lean-build` to stop at the approved boundary, `superpowers:receiving-code-review` for every review round, and `superpowers:verification-before-completion` before completion. Keep the existing dirty feature worktree intact and commit only when the user explicitly authorizes it.

**Goal:** Complete the Batuta beta with automatic inventory, domain/complexity classification, exact executor selection, ephemeral runtime matrices, bounded fresh-run fallback, review, commit, push, PR opening, and exact remote verification without Batuta-specific Compozy migrations or routine operator gates.

**Architecture:** Batuta owns one durable `delivery_id` and ordered attempts in its workspace routing journal. Every attempt is a new `batuta-deliver` Compozy run, but all attempts share the exact worktree, task snapshot, routing generation, origin session, and absolute budget. Read-only extension tools materialize typed runtime/budget context for child Loops. A guarded mutating tool owns start/reconciliation under the journal file lock, scans recent Compozy runs after ambiguous outcomes, and starts at most one successor. Compozy remains the execution engine; Batuta never writes stored Loop config and never uses same-lineage recovery.

**Tech stack:** Go 1.25, Compozy Go extension SDK, JSON journal with `flock`, Compozy CLI JSON boundary, Loop YAML, Git worktree evidence, Python/shell contract tests.

**Approved design:** [2026-08-26-batuta-migration-free-delivery-recovery-design.md](../specs/2026-08-26-batuta-migration-free-delivery-recovery-design.md)

**Prerequisite plan:** [2026-08-26-compozy-run-loop-config-overrides.md](2026-08-26-compozy-run-loop-config-overrides.md)

**Supersession:** This plan replaces every stored-config/CAS and same-lineage fallback task in the 2026-08-25 activation and executor-routing plans. Their inventory, classification, selection, and publication work remains valid; their migration-backed recovery path does not.

## Global constraints

- Do not restore required hooks, revisioned Loop config, `recover-nested`, same-lineage recovery, or any Compozy database migration.
- Do not call `compozy loop config` or `compozy loop configure` in production code.
- Do not let the Batuta agent invoke raw `loop run` for a managed delivery; `ext__batuta__routing_apply` is the only mutating start/recovery owner.
- Do not add a routine human gate. Stop only on a truthful `blocked`, `exhausted`, `canceled`, or `stalled` condition. Merge remains manual.
- Maximum four attempts including the initial run; absolute deadline is four hours; cumulative admission ceiling is 1,000,000 tokens across unique implementation/review children.
- At most one external `loop run` may occur while one workspace journal lock is held. Never poll or sleep in a tool call.
- Start/recovery uses a fixed absolute Compozy executable and bounded stdout/stderr through the existing command runner.
- All caller-controlled IDs/paths are validated before command execution. Do not include temp paths, secrets, raw CLI stderr, or provider credentials in public errors.
- Preserve every existing publication safety invariant: fresh plan, exact reviewed HEAD, one mutation per operation, ambiguous-result reconciliation, and independent remote verification.
- Runtime selection advances only for exact tasks that actually executed and failed. Pending or dependency-skipped tasks retain their prior runtime.
- `routing_context` and `delivery_budget_context` are read-only; neither may write the journal or start work.
- All shell commands in this plan use `rtk`. Use a unique directory under `/home/francisross/tmp-builds` for heavy build scratch; do not export `TMPDIR` globally.

---

## Task 1: Replace config ownership with a delivery journal domain

**Files:**

- Modify: `internal/routing/ownership.go`
- Modify: `internal/routing/ownership_test.go`
- Modify: `internal/routing/matrix.go`
- Modify: `internal/routing/matrix_test.go`
- Create: `internal/routing/delivery.go`
- Create: `internal/routing/delivery_test.go`
- Delete after replacement tests are GREEN: `internal/routing/config.go`
- Delete after replacement tests are GREEN: `internal/routing/config_test.go`

### Step 1: Define the RED journal fixtures

Add tests for a schema-v2 journal containing:

```go
type DeliveryState string

const (
    DeliveryStateActive    DeliveryState = "active"
    DeliveryStateDone      DeliveryState = "done"
    DeliveryStateBlocked   DeliveryState = "blocked"
    DeliveryStateExhausted DeliveryState = "exhausted"
)

type AttemptState string

const (
    AttemptPlanned   AttemptState = "planned"
    AttemptSubmitted AttemptState = "submitted"
    AttemptTerminal  AttemptState = "terminal"
)
```

Model exact JSON fields:

- `DeliveryRecord`: `delivery_id`, workspace/worktree identity and canonical root, slug, task-set digest, routing-generation digest, origin session, created time, absolute deadline, attempt/token ceilings, initial worktree fingerprint, state, ordered attempts;
- `DeliveryAttempt`: attempt number, operation ID, request digest, exact runtime rules for the attempt, state, optional run ID, child run IDs, planned/started/terminal times, terminal status, token usage, worktree fingerprint, publication-mutation flag, blocker code;
- `WorktreeFingerprint`: HEAD SHA plus deterministic digests of porcelain and diff;
- `RoutingJournal`: schema version, current generation, generations, and deliveries keyed by `delivery_id`.

Test strict decode, `0700` directory, `0600` journal/lock, atomic rename, immutable header fields, ordered contiguous attempts, allowed state transitions only, duplicate operation replay, operation/request digest mismatch conflict, and 16 MiB input bound.

### Step 2: Define automatic v1 read upgrade

The beta journal is local cache, not a Compozy migration. Add a fixture for the current v1 JSON and require the loader to:

- preserve valid routing generations and `current_generation`;
- discard `owned_rules` and run-ID `delivery_bindings` because the new design never owns stored config and cannot safely invent delivery headers;
- initialize an empty `deliveries` map and write schema v2 only on the next explicit mutation;
- never call Compozy or mutate stored Loop config during upgrade;
- fail closed on malformed or internally inconsistent v1 data.

This automatic read upgrade avoids operator cleanup while refusing unsafe recovery of an old beta run.

### Step 3: Add a journal transaction seam under one lock

Expose one method that keeps the existing in-process mutex and `flock` for the entire callback while allowing an intent to become durable before an external mutation:

```go
type JournalTx struct {
    Journal *RoutingJournal
    persist func() error
}

func (tx *JournalTx) Persist() error

func (s *OwnershipStore) WithLockedJournal(
    workspaceID string,
    fn func(*JournalTx) error,
) error
```

The store loads and validates once, the callback mutates a private journal, and every `Persist` validates, writes, fsyncs, renames, and fsyncs the parent directory without releasing either lock. A callback that never calls `Persist` is read-only; an error after an earlier `Persist` does not pretend that durable phase was rolled back. Keep raw `load`/`save` unexported. Add tests with two store instances targeting the same root to prove cross-process-style serialization and with a callback that persists `planned`, returns an injected error, and leaves exactly that durable state.

### Step 4: Make matrix application archive-only

Replace `LoopConfigBoundary` and `MatrixManager.Config` with:

```go
type MatrixManager struct {
    Store *OwnershipStore
}

type MatrixApplyResult struct {
    DeliveryID       string    `json:"delivery_id"`
    GenerationDigest string    `json:"generation_digest"`
    CreatedAt        time.Time `json:"created_at"`
    AbsoluteDeadline time.Time `json:"absolute_deadline"`
    AttemptCeiling   int       `json:"attempt_ceiling"`
    TokenCeiling     int64     `json:"token_ceiling"`
    RuleCount        int       `json:"rule_count"`
}
```

Change `Apply` to accept the trusted workspace/worktree identity, slug, origin session, task/worktree snapshots, and generation. Derive `delivery_id` as `sha256:<64 lowercase hex>` over those canonical immutable fields, derive `absolute_deadline` once as creation time plus four hours, and replay an existing identical header instead of creating a second delivery. Verify all generation/header digests, write both with one `WithLockedJournal`/`Persist`, reload and compare the generation/delivery digests, and return the fixed budget. It must not accept a Loop name or Compozy client because it performs no Compozy call.

Add a command-runner spy at the extension wiring test and assert zero invocations containing `loop config` or `loop configure`.

### Step 5: Run RED/GREEN and remove config production code

```bash
rtk go test ./internal/routing -run 'Journal|Ownership|Delivery|Matrix' -count=1
rtk go test -race ./internal/routing -run 'Journal|Ownership|Delivery|Matrix' -count=1
```

Only after the archive-only tests are GREEN, delete `config.go` and `config_test.go`, then prove there are no consumers:

```bash
rtk rg -n 'LoopConfigClient|LoopConfigBoundary|ConfigRevision|MergeRuntimeRules|loop configure|loop config' internal --glob '*.go'
```

Expected: no Batuta production hit.

### Step 6: Commit when authorized

```bash
rtk git add internal/routing
rtk git commit -m "feat: persist migration-free deliveries"
```

---

## Task 2: Add authoritative worktree and task snapshot evidence

**Files:**

- Modify: `internal/publication/git.go`
- Modify: `internal/publication/git_test.go`
- Modify: `internal/routing/artifacts.go`
- Modify: `internal/routing/artifacts_test.go`
- Modify: `internal/routing/artifacts_manifest.go`
- Modify: `internal/routing/artifacts_test.go`
- Modify: `internal/routing/delivery.go`
- Modify: `internal/routing/delivery_test.go`

### Step 1: Add RED worktree fingerprint tests

Extend the existing fixed Git boundary with:

```go
type WorktreeState struct {
    HeadSHA         string `json:"head_sha"`
    PorcelainSHA256 string `json:"porcelain_sha256"`
    ContentSHA256   string `json:"content_sha256"`
}
```

Add `GitClient.WorktreeState(ctx, absolutePath)` using exact commands and directory:

```text
git rev-parse HEAD
git status --porcelain=v1 -z --untracked-files=all
git diff --binary --no-ext-diff HEAD
git ls-files --others --exclude-standard -z
```

Hash raw stdout bytes, not line-normalized text. `ContentSHA256` covers the tracked binary diff plus every untracked entry in NUL-sorted relative-path order. Stream regular-file bytes, hash a symlink's link target without following it, and reject special files or paths escaping the canonical worktree. This catches edits to an already-untracked file even when porcelain text is unchanged. Test exact argv/directory, malformed SHA rejection, context cancellation, bounded command output, tracked/untracked/symlink changes, escape rejection, and stable identical state.

### Step 2: Extend the task manifest without filtering completed tasks

The artifact loader already preserves authored status. Add deterministic helpers that return:

- complete ordered task snapshot across every authored task file, including `completed`;
- task-set digest over canonical task identity, dependencies, domain/type, complexity, and authored status;
- ordered incomplete task IDs;
- map from `implement-tasks` fan-out item index to task ID based on the exact imported order.

Reject duplicate IDs, unsupported statuses, task-file drift, or a manifest whose dependencies do not match the pinned generation.

### Step 3: Prove carry-forward semantics

Tests must create two tasks, mark task 1 completed between attempts, and assert:

- task 1 remains in the immutable task snapshot/generation;
- it is absent from the successor incomplete list;
- task 2 retains the same item identity mapping;
- only a task with an observed failed output is eligible to advance fallback;
- a pending/dependency-skipped task retains its prior runtime.

### Step 4: Run verification and commit when authorized

```bash
rtk go test -race ./internal/publication ./internal/routing -run 'Git|WorktreeState|Artifact|Manifest|Carry|Incomplete' -count=1
rtk go vet ./internal/publication ./internal/routing
```

```bash
rtk git add internal/publication/git.go internal/publication/git_test.go internal/routing
rtk git commit -m "feat: pin delivery worktree evidence"
```

---

## Task 3: Replace same-lineage recovery with a bounded Compozy run client

**Files:**

- Replace: `internal/extensionapp/routing_recovery.go`
- Replace: `internal/extensionapp/routing_recovery_test.go`
- Create: `internal/extensionapp/delivery_client.go`
- Create: `internal/extensionapp/delivery_client_test.go`
- Modify: `internal/extensionapp/routing_runtime.go`
- Modify: `internal/extensionapp/routing_tools.go`
- Modify: `internal/extensionapp/routing_tools_test.go`

### Step 1: Define closed internal wire types and RED decoders

Mirror only the Compozy JSON fields Batuta consumes:

```go
type deliveryRun struct {
    ID            string         `json:"id"`
    WorkspaceID   string         `json:"workspace_id"`
    LoopName      string         `json:"loop_name"`
    Status        string         `json:"status"`
    CreatedAt     time.Time      `json:"created_at"`
    StartedAt     time.Time      `json:"started_at"`
    TokensUsed    int64          `json:"tokens_used"`
    Inputs        map[string]any `json:"inputs"`
}

type deliveryRunDetail struct {
    Run         deliveryRun          `json:"run"`
    Generations []deliveryGeneration `json:"generations"`
}
```

Use `json.Decoder.DisallowUnknownFields` only on Batuta's own journal/tool inputs. For upstream Compozy responses, accept additive unknown fields but require and validate every consumed field. Reject duplicate/trailing JSON, wrong workspace, wrong loop name, invalid timestamps, negative tokens, invalid status, malformed inputs, and more than the requested list limit.

### Step 2: Implement the fixed CLI boundary

Replace `RecoverNested` with:

```go
type deliveryClient interface {
    Status(context.Context, string, string) (deliveryRunDetail, error)
    Recent(context.Context, string, int) ([]deliveryRun, error)
    Start(context.Context, string, deliveryStartRequest) (deliveryRun, error)
}
```

Exact command templates use the already validated scalar variables `workspace_id`, `run_id`, `delivery_id`, `attempt`, `slug`, `origin_session_id`, `worktree_ref`, `routing_generation`, `absolute_deadline`, `recovery_operation_id`, and `config_path`:

```text
compozy loop status --workspace "$workspace_id" --run-id "$run_id" -o json
compozy loop runs --workspace "$workspace_id" --loop batuta-deliver --limit 200 -o json
compozy loop run --workspace "$workspace_id" --name batuta-deliver --no-prompt \
  --input "delivery_id=$delivery_id" \
  --input "attempt=$attempt" \
  --input "slug=$slug" \
  --input "origin_session_id=$origin_session_id" \
  --input "worktree_ref=$worktree_ref" \
  --input "routing_generation=$routing_generation" \
  --input "absolute_deadline=$absolute_deadline" \
  --input token_ceiling=1000000 \
  --input "recovery_operation_id=$recovery_operation_id" \
  --config-file "$config_path" -o json
```

The config file contains only the remaining parent `iteration_cap`, `budget_tokens`, `budget_wall_sec`, `budget_on_exceeded: halt`, and `reattempt_strategy: halt`. Create it with `0600`, sync/close before execution, and remove it on every return. Preserve no temp path in public errors.

Bound list/status/start stdout to 2 MiB and stderr to 64 KiB. Apply a fixed 30-second command deadline capped by any earlier caller deadline so a journal lock cannot be held indefinitely. Validate the executable is absolute and every scalar is canonical before creating the temp file or invoking the runner.

### Step 3: Test exact argv and ambiguous process outcomes

Cover:

- exact command, flag order, directory, limits, and secure config file contents;
- successful start returns a `batuta-deliver` run whose inputs exactly match the request;
- nonzero exit with a valid JSON run response is not assumed success; caller reconciles;
- canceled context returns `context.Canceled`;
- timeout returns the context error;
- malformed/oversized output is safe and bounded;
- no `recover-nested`, `loop config`, or `loop configure` argument is ever emitted.

### Step 4: Run focused tests and commit when authorized

```bash
rtk go test -race ./internal/extensionapp -run 'DeliveryClient|RoutingRecovery|RoutingApply' -count=1
rtk go vet ./internal/extensionapp
```

```bash
rtk git add internal/extensionapp/delivery_client.go internal/extensionapp/delivery_client_test.go internal/extensionapp/routing_recovery.go internal/extensionapp/routing_recovery_test.go internal/extensionapp/routing_runtime.go internal/extensionapp/routing_tools.go internal/extensionapp/routing_tools_test.go
rtk git commit -m "feat: start bounded delivery attempts"
```

---

## Task 4: Implement the idempotent attempt owner and fallback policy

**Files:**

- Modify: `internal/routing/delivery.go`
- Modify: `internal/routing/delivery_test.go`
- Modify: `internal/extensionapp/routing_recovery.go`
- Modify: `internal/extensionapp/routing_recovery_test.go`
- Modify: `internal/extensionapp/routing_runtime.go`
- Modify: `internal/extensionapp/routing_runtime_test.go` if present; otherwise keep engine cases in `routing_tools_test.go`

### Step 1: Add deterministic identity helpers with RED vectors

Implement and test exact lowercase SHA-256 identities:

- `delivery_id`: canonical digest of workspace ID, canonical worktree ID, slug, task-set digest, routing digest, origin session, and initial worktree fingerprint, generated once by `apply_matrix` and replayed for an identical request;
- operation ID input: workspace ID, delivery ID, attempt, routing digest, ordered incomplete task IDs, ordered exact runtime selections;
- request digest: canonical start request including worktree/task/budget inputs;
- output format: `sha256:<64 lowercase hex>`.

Test stable golden vectors, order sensitivity where required, map-order independence, and conflict when the same operation ID has a different request digest.

### Step 2: Add budget accounting REDs

Create one pure planner that consumes a validated delivery, pinned generation, task manifest, exact run details, current time, and worktree state. It returns either a successor intent or a closed terminal reason.

Cover all stop conditions with zero client calls and byte-identical journal state:

- attempt 4 already used;
- current time at/after absolute deadline;
- unique child token sum at/above 1,000,000;
- duplicate child IDs counted once;
- missing/contradictory child accounting;
- fallback candidate already consumed;
- no candidate remains;
- task-set/routing/workspace/worktree drift;
- prior result not implementation-recoverable;
- review/publication failure;
- publication mutation started or outcome ambiguous;
- canceled/stalled/exhausted parent.

Remaining wall seconds is `floor(deadline-now)` and must be positive. Remaining tokens is ceiling minus the sum of unique implementation/review child runs. An in-flight turn may cross the ceiling, but no later work is admitted.

### Step 3: Derive only exact failed task fallbacks

From the parent detail:

1. find the exact `implement` child output;
2. load that child detail and its terminal generation outputs;
3. map only `status: failed` run-agent item indexes to pinned task IDs;
4. exclude completed task files from successor admission;
5. advance each failed task by one unused candidate;
6. retain the prior effective runtime for pending/dependency-skipped incomplete tasks;
7. construct exact-ID rules before domain/complexity rules so specificity wins.

Any missing child, item-index mismatch, duplicate item, nonterminal status, or foreign runtime fails closed.

### Step 4: Own plan → reconcile → start → submit under one lock

Implement `start_delivery` and `recover_delivery` through the same locked state machine:

1. enter `WithLockedJournal` and load/validate the delivery;
2. create a deterministic `planned` attempt and call `Persist` before any external command, or replay the existing planned/submitted attempt;
3. while the same lock remains held, call `Recent(..., 200)` and filter candidates:
   - loop name exactly `batuta-deliver`;
   - durable `created_at` no earlier than `planned_at - 1 minute`, and any non-zero `started_at` satisfies the same bound;
   - exact `delivery_id`, attempt, slug, worktree, routing digest, deadline, token ceiling, and operation ID inputs;
4. if one candidate exists, adopt it;
5. if more than one exists, transition delivery to blocked with `ambiguous_successor` and start nothing;
6. if none exists, call `Start` once while still holding the workspace file lock;
7. record returned/adopted run ID, transition to `submitted`, and call `Persist` again before releasing the lock.

On a process crash after the external call and before save, the next call repeats the recent-run reconciliation. On a replay with a submitted run, return the same run without list/start.

### Step 5: Settle terminal attempts truthfully

`reconcile_fallbacks` reads the exact submitted run once, records unique child IDs/tokens/worktree fingerprint, and transitions only a terminal run to `terminal`. It returns:

```go
type RoutingReconcileResult struct {
    DeliveryID        string `json:"delivery_id"`
    Attempt           int    `json:"attempt"`
    DeliveryRunID     string `json:"delivery_run_id"`
    State             string `json:"state"`
    Recoverable       bool   `json:"recoverable"`
    AttemptsUsed      int    `json:"attempts_used"`
    AttemptsLimit     int    `json:"attempts_limit"`
    TokensUsed        int64  `json:"tokens_used"`
    TokensLimit       int64  `json:"tokens_limit"`
    RemainingWallSec  int    `json:"remaining_wall_sec"`
    BlockerCode       string `json:"blocker_code,omitempty"`
}
```

`recover_delivery` requires both `delivery_id` and the exact terminal `delivery_run_id`; it never selects a delivery from a run ID alone.

### Step 6: Run the race and replay matrix

```bash
rtk go test -race ./internal/routing ./internal/extensionapp -run 'Delivery|Attempt|Fallback|Recovery|Reconcile|Budget|Ambiguous|Replay' -count=1
```

Include two concurrent service instances sharing the same journal root. Assert one `Start` call, one attempt record, one run ID, and identical replay results.

### Step 7: Commit when authorized

```bash
rtk git add internal/routing/delivery.go internal/routing/delivery_test.go internal/extensionapp/routing_recovery.go internal/extensionapp/routing_recovery_test.go internal/extensionapp/routing_runtime.go internal/extensionapp/routing_tools.go internal/extensionapp/routing_tools_test.go
rtk git commit -m "feat: reconcile delivery fallbacks across runs"
```

---

## Task 5: Expose closed mutating and read-only extension tools

**Files:**

- Modify: `internal/extensionapp/app.go`
- Modify: `internal/extensionapp/app_test.go`
- Modify: `internal/extensionapp/routing_tools.go`
- Modify: `internal/extensionapp/routing_tools_test.go`
- Create: `internal/extensionapp/routing_context.go`
- Create: `internal/extensionapp/routing_context_test.go`

### Step 1: Replace the `routing_apply` operation schema

Use a closed `oneOf` with four operations:

- `apply_matrix`: requires `routing_plan`, expected generation digest, slug, origin session ID, and worktree ref; resolves that worktree through the existing trusted `WorktreeClient`, captures its canonical root/fingerprint, and returns the archived matrix plus deterministic `delivery_id` and absolute budget;
- `start_delivery`: requires only `delivery_id`;
- `reconcile_fallbacks`: requires `delivery_id` and `delivery_run_id`;
- `recover_delivery`: requires `delivery_id` and `delivery_run_id`.

Reject mixed arms, unknown properties, malformed IDs, wrong digest format, and extra run IDs before calling the service. Remove `config_revision`, nested child recovery result, and same-lineage operation fields from outputs.

### Step 2: Add `ext__batuta__routing_context` as read-only

Input schema:

```json
{
  "delivery_id": "opaque <=128 bytes",
  "attempt": "integer 1..4",
  "slug": "canonical slug",
  "routing_generation": "sha256 digest"
}
```

Output schema:

```json
{
  "runtime_rules": [],
  "remaining_tokens": 1,
  "remaining_wall_seconds": 1
}
```

Implementation reloads the journal, verifies the exact delivery/attempt/slug/generation/workspace tuple and current task-set digest, recomputes remaining budget from already settled unique children, and returns a deep copy of ordered rules. It performs no journal save and no external mutation.

### Step 3: Add `ext__batuta__delivery_budget_context` as read-only

Input requires `delivery_id`, `attempt`, and exact `implementation_run_id`. It calls status once for that child, proves it belongs to the submitted parent attempt, requires terminal success, adds its usage to an in-memory unique accounting set, and returns remaining tokens/wall for review. It does not settle or save the attempt; final reconciliation remains the only journal writer for child accounting.

Reject foreign/nonterminal/failed implementation children, duplicate/contradictory identity, missing usage, and exhausted budget.

### Step 4: Register all eight tools and prove metadata

Update `app.go` and `app_test.go` so descriptors are exactly:

- existing publication plan/publish/verify;
- existing inventory and routing plan;
- mutating `routing_apply`;
- read-only `routing_context`;
- read-only `delivery_budget_context`.

Assert exact tool IDs, handlers, risk classes, `ReadOnly` values, closed schemas, and no obsolete `recover-nested` tool exposure. Update the expected tool count to eight.

### Step 5: Run focused tests and commit when authorized

```bash
rtk go test -race ./internal/extensionapp -run 'App|RoutingApply|RoutingContext|DeliveryBudgetContext|Schema' -count=1
rtk go vet ./internal/extensionapp
```

```bash
rtk git add internal/extensionapp
rtk git commit -m "feat: expose migration-free delivery controls"
```

---

## Task 6: Rewire `batuta-deliver` to typed ephemeral context

**Files:**

- Modify: `loops/batuta-deliver/loop.yaml`
- Modify: `agents/batuta/AGENT.md`
- Modify: `resources/skills/batuta-routing/SKILL.md`
- Modify: `tests/contract/test_04_deliver_validate.sh`
- Modify: `tests/contract/test_07_workflow_contract.sh`
- Modify: `tests/e2e/assert_event_driven_return.py`
- Modify: `tests/e2e/test_assert_event_driven_return.py` if present

### Step 1: Add the closed delivery inputs

Add required inputs to `batuta-deliver`:

- `delivery_id` string;
- `attempt` number constrained by the tool owner to 1..4;
- `absolute_deadline` string;
- `token_ceiling` number fixed to 1,000,000 by the tool owner;
- `recovery_operation_id` string, empty only for attempt 1.

Keep slug, origin session, worktree ref, and routing generation. The Loop is not a public arbitrary starter; the agent contract requires the guarded tool.

### Step 2: Add `routing_context` before implementation

Graph sequence:

```text
slug_input
  → load_check
  → routing_context
  → implement
  → delivery_budget_context
  → review
  → publication_plan
  → route/publish/verify
```

Pass direct typed references:

```yaml
runtime_rules: "{{ .nodes.routing_context.output.runtime_rules }}"
budget_tokens: "{{ .nodes.routing_context.output.remaining_tokens }}"
budget_wall_sec: "{{ .nodes.routing_context.output.remaining_wall_seconds }}"
```

Keep literal `iteration_cap: 4`, `budget_on_exceeded: halt`, `reattempt_strategy: halt`, worktree environment, and `auto_commit: true`.

### Step 3: Budget review from the exact implementation child

Pass `{{ .nodes.implement.output.child_loop_run_id }}` to `delivery_budget_context`, then pass its typed remaining numbers to `review-and-fix.config_overrides`. Review keeps its independent runtime selection and the same worktree.

Publication remains after successful review only. Do not add a human approval node.

### Step 4: Rewrite the terminal effect message

The queued origin prompt must include the stable delivery ID and exact terminal parent run ID. It instructs the Batuta agent to:

1. call `routing_apply/reconcile_fallbacks` with both IDs;
2. call `routing_apply/recover_delivery` once only if `recoverable: true`;
3. end the turn after a successor is accepted;
4. otherwise report the authoritative terminal state and publication evidence.

Use an idempotency key containing delivery ID, run ID, generation, and trigger. No polling, sleep, raw `loop run`, or operator routing.

### Step 5: Update the agent and routing skill

Replace all stored-config/CAS/same-lineage language with:

```text
routing_plan → apply_matrix → start_delivery
terminal effect → reconcile_fallbacks → optional recover_delivery
```

Make explicit that the Batuta agent creates SDD/tasks and orchestrates but does not implement feature code; implementation/review remain child Loops. Push and PR opening remain automatic after review, while merge remains manual.

### Step 6: Validate the Loop and contracts

```bash
rtk bash tests/contract/test_04_deliver_validate.sh
rtk bash tests/contract/test_07_workflow_contract.sh
rtk go test ./internal/extensionapp -run 'Loop|Descriptor|Routing' -count=1
```

Contract assertions must reject:

- `config_revision`, `expected_revision`, `recover-nested`, `loop config`, `loop configure`;
- a human gate or `batuta-publisher` agent;
- a raw string-encoded runtime-rules array;
- missing budget-context sequencing.

### Step 7: Commit when authorized

```bash
rtk git add loops/batuta-deliver/loop.yaml agents/batuta/AGENT.md resources/skills/batuta-routing/SKILL.md tests/contract tests/e2e
rtk git commit -m "feat: route delivery attempts ephemerally"
```

---

## Task 7: Update extension wiring, packaging, and public contracts

**Files:**

- Modify: `internal/extensionapp/app.go`
- Modify: `internal/extensionapp/app_test.go`
- Modify: `main_test.go`
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `scripts/check-compozy-version.sh`
- Modify: `scripts/stage-extension.sh`
- Modify: `scripts/republish.sh`
- Modify: `tests/contract/test_00_runtime_guard.sh`
- Modify: `tests/contract/test_01_stage.sh`
- Modify: `tests/contract/test_01_republish.sh`
- Modify: `tests/contract/test_01_validate.sh`
- Modify: `tests/contract/test_02_domain_lane_surface.sh`

### Step 1: Remove obsolete wiring

In `app.go`:

- stop constructing `routing.LoopConfigClient`;
- construct `MatrixManager` with only the journal store;
- construct the new delivery CLI client once with the fixed absolute Compozy executable and existing bounded runner;
- inject that client into attempt start, reconcile, routing context, and delivery budget context;
- retain publication and live inventory services unchanged.

Add wiring tests that fail if any production command contains `loop config`, `loop configure`, or `recover-nested`.

### Step 2: Pin only official released dependencies

Do not use a local `replace`, pseudo-version, or experimental fork SDK. Once the generic Compozy PR is merged and an official compatible SDK/daemon release exists:

- update `go.mod` to that tag;
- update `scripts/check-compozy-version.sh` to the exact supported daemon floor;
- regenerate `go.sum` with normal module tooling;
- prove staging rejects an older daemon and accepts the exact floor.

If the official pair is not yet released, keep the beta marked local/integration-only and do not claim public readiness.

### Step 3: Update package contract expectations

Tests must assert:

- one `batuta` agent, no `batuta-publisher` agent;
- eight exact tools and correct risk metadata;
- `batuta-deliver` includes the new inputs/nodes;
- no required-hook manifest contract;
- no stored-config or nested-recovery prerequisite;
- executor inventory and domain/complexity routing remain present;
- staged extension installs and reloads from an immutable generation.

### Step 4: Run focused package gates

```bash
rtk go test -race ./internal/extensionapp ./internal/routing ./internal/publication -count=1
rtk go test ./... -count=1
rtk go vet ./...
rtk bash tests/contract/test_00_runtime_guard.sh
rtk bash tests/contract/test_01_validate.sh
rtk bash tests/contract/test_01_stage.sh
rtk bash tests/contract/test_01_republish.sh
rtk bash tests/contract/test_02_domain_lane_surface.sh
rtk git diff --check
```

### Step 5: Commit when authorized

```bash
rtk git add internal/extensionapp main_test.go go.mod go.sum scripts tests/contract
rtk git commit -m "build: require migration-free delivery runtime"
```

---

## Task 8: Add a natural two-attempt integration and stop-condition matrix

**Files:**

- Create: `internal/extensionapp/delivery_integration_test.go`
- Modify: `tests/e2e/assert_domain_routing.py`
- Modify: `tests/e2e/test_assert_domain_routing.py`
- Modify: `tests/e2e/assert_publication_flow.py`
- Modify: `tests/e2e/test_assert_publication_flow.py`
- Create or modify the isolated lab harness under `tests/e2e/` following existing conventions

### Step 1: Build a deterministic local fixture

Use an isolated workspace/home/daemon port and a disposable Git repository/worktree. Author at least two tasks:

- backend task succeeds on the primary runtime and reaches completed with one commit;
- frontend task fails on its primary runtime and succeeds on its configured fallback;
- review succeeds;
- publication uses a disposable local/bare remote for deterministic push evidence; real PR URL remains a separate release smoke.

The fixture must expose different exact provider/model identities in `runtime_applied` evidence. Do not fake the routing result inside the assertion.

### Step 2: Prove the complete fresh-run lifecycle

Assert:

- inventory maps Compozy, Codex, OpenCode, and Cursor Agent configuration;
- authored tasks are classified by canonical domain and complexity;
- the frontend cell selects Cursor Agent with the configured Grok model when that exact live pair is available;
- `apply_matrix` performs no stored config call;
- attempt 1 starts one parent run and one implementation child;
- task 1 completes and its commit remains;
- task 2 failure settles the parent as recoverable;
- recovery creates attempt 2 with a different parent run ID and the same delivery/worktree/task/routing identity;
- attempt 2 runs only task 2 with the exact next fallback;
- review, publication plan, push, and exact remote HEAD verification finish;
- both parent run IDs and every child ID are present once in the journal;
- final terminal state is done and merge was not attempted.

### Step 3: Prove replay and ambiguity

Run the start/recover call twice before and after submission. Expect the same run ID and one external start. Simulate a lost CLI response and seed exactly one matching recent run; expect adoption. Seed two matching runs; expect blocked ambiguity and no third run.

### Step 4: Prove every stop boundary has zero mutation

Table-drive attempt ceiling, deadline, token ceiling, exhausted fallback, task drift, worktree drift, routing drift, foreign run, review failure, publication-started failure, canceled, stalled, and ambiguous remote effect. Snapshot journal bytes and runner call counts before/after; both must remain unchanged except the explicit blocked evidence transition allowed by the design.

### Step 5: Run integration with isolated heavy scratch

```bash
build_tmp=$(mktemp -d -p /home/francisross/tmp-builds batuta-delivery.XXXXXX)
TMPDIR="$build_tmp" rtk go test -tags=integration ./internal/extensionapp -run TestMigrationFreeDelivery -count=1 -v
TMPDIR="$build_tmp" rtk python -m unittest tests.e2e.test_assert_domain_routing tests.e2e.test_assert_publication_flow
```

After the commands finish, validate that the path begins with `/home/francisross/tmp-builds/batuta-delivery.` and remove only that run's directory.

### Step 6: Commit when authorized

```bash
rtk git add internal/extensionapp/delivery_integration_test.go tests/e2e
rtk git commit -m "test: prove fresh-run delivery fallback"
```

---

## Task 9: Align docs, QA, and release truth

**Files:**

- Modify: `README.md`
- Modify: `README.pt-BR.md`
- Modify: `docs/architecture.md`
- Modify: `docs/how-it-works.md`
- Modify: `docs/verify.md`
- Modify: `docs/releases/0.1.0-beta.5.md`
- Modify: canonical files under `docs/internal/qa/`
- Modify: `docs/internal/specs/2026-08-25-batuta-next-preview-design.md`
- Modify: relevant contract tests under `tests/contract/`
- Preserve: future note for interactive questions and graph engineering; do not implement them here

### Step 1: Replace obsolete architecture claims

Document the actual flow:

```text
SDD/task authoring
  → live executor inventory
  → domain + complexity classification
  → immutable routing generation
  → attempt 1 (new Compozy run)
  → exact failed-task fallback
  → attempt N (new Compozy run, same worktree)
  → review
  → push + PR
  → exact remote verification
```

Explain stable Batuta `delivery_id` versus changing Compozy `run_id`, global stop budgets, automatic recovery, no routine gate, manual merge, and no Batuta-specific Compozy migrations.

### Step 2: Reset QA evidence honestly

Create/update canonical scenarios for:

- typed child overrides;
- matrix archive without config mutation;
- fresh-run fallback and carry-forward;
- replay/ambiguous outcome;
- stop budgets;
- publication and exact remote verification;
- clean install with official Compozy/SDK;
- real remote PR smoke.

Mark deterministic local automation pass only when fresh logs exist. Keep official-release and real-provider/real-forge scenarios `blocked-verify` until those external prerequisites are genuinely available.

### Step 3: Update future-increment notes

Record but do not implement:

- interactive clarification requests parked for user input;
- parallel task execution/worktrees;
- deterministic commit integration;
- graph engineering and lane-level concurrency.

### Step 4: Run public-document checks

```bash
rtk bash tests/contract/test_07_public_docs.sh
rtk bash tests/contract/test_07_preview_docs.sh
rtk bash tests/contract/test_07_workflow_contract.sh
rtk git diff --check
```

### Step 5: Commit when authorized

```bash
rtk git add README.md README.pt-BR.md docs tests/contract
rtk git commit -m "docs: explain migration-free delivery recovery"
```

---

## Task 10: Independent final review and release gate

**Files:**

- Read: full branch diff and both approved 2026-08-26 documents
- Modify only to resolve validated review findings
- Write: final implementation/review reports in the existing ignored SDD report location

### Step 1: Audit scope removal before testing

```bash
rtk rg -n 'recover-nested|expected_revision|config_revision|loop configure|loop config|OwnedRules|LoopConfigClient|required hook|human gate|batuta-publisher' . --glob '!docs/internal/plans/2026-08-25-*' --glob '!docs/internal/specs/2026-08-25-*'
rtk git diff --check
rtk git status --short
```

Every production hit must be removed or justified as historical documentation. Confirm the current dirty worktree changes are all intentional; do not stage unrelated user edits.

### Step 2: Request an independent review

The reviewer must trace, end to end:

- apply matrix → archive/header;
- initial start → one run;
- terminal settlement → exact children/tokens/worktree;
- failed-task selection → next fallback only;
- recovery replay/lost-response/ambiguity;
- typed routing and budget contexts into both child Loops;
- review/publication continuation;
- all stop conditions;
- removal of config CAS/nested recovery/migrations/hooks/human gate;
- extension schema and packaging truth.

Resolve Critical/Important findings before proceeding. Rerun the smallest RED/GREEN suite after each fix.

### Step 3: Run fresh full gates

```bash
build_tmp=$(mktemp -d -p /home/francisross/tmp-builds batuta-gate.XXXXXX)
TMPDIR="$build_tmp" rtk go test -race ./... -count=1
TMPDIR="$build_tmp" rtk go vet ./...
TMPDIR="$build_tmp" rtk make gate
rtk git diff --check
```

If Batuta's `make gate` invokes Compozy filesystem tests requiring `/tmp`, rerun that gate with `TMPDIR`/`GOTMPDIR` unset and retain compiler scratch only in a variable proven not to affect `testing.TempDir`.

Then run the Task 8 isolated integration and all contract/E2E suites. Remove only the two unique scratch directories created by this task after validating their prefixes.

### Step 4: Verify against a locally built compatible Compozy

From the clean Compozy feature worktree, build with unique scratch and install into an isolated Compozy home. Stage/install the Batuta extension, validate all eight tools and the Loop, then run the deterministic two-attempt lab. Do not overwrite the user's production Compozy configuration during this proof.

### Step 5: Apply the public release gate

Public beta release is allowed only after:

- the Compozy issue/PR is merged;
- fork/main is synchronized;
- an official Compozy daemon and Go SDK release contains the contract;
- Batuta has no `replace` or pseudo-version;
- clean installation succeeds;
- disposable real-provider and real-forge smoke proves push, PR URL, and exact remote HEAD.

Until then, report the branch as locally integrated, not publicly released.

### Step 6: Commit/push/release only with explicit user authorization

Do not commit, push, tag, publish, open a Batuta PR, or mutate a real remote as part of this plan unless the user explicitly authorizes that action in the execution session.
