# Batuta parallel graph delivery — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILLS: Use `superpowers:test-driven-development` for every behavior change, `lean-build` to stop at the approved boundary, `compozy` for public Compozy contract work, `superpowers:receiving-code-review` for every review round, and `superpowers:verification-before-completion` before any completion claim. Use `imagegen` only for the approved architecture visual. Do not modify Compozy without the operator's explicit approval.

**Goal:** Ship the next Batuta beta with interactive SDD clarification, immutable executor routing, dependency-safe execution of at most four tasks in parallel, one managed worktree per task, deterministic commit integration with conflict reexecution, one final integrated review, and automatic push/PR verification; update Batuta documentation, its architecture visual, and the Portuguese and English personal-site article in the same release program.

**Architecture:** `batuta-deliver` remains the sole end-to-end and publication owner. It asks one narrow Go extension service for the next durable graph transition, fans out at most four ready task descriptors, and awaits one new `batuta-task` child Loop per task. Each child runs the existing `code_implementer` in its assigned managed worktree and records either one verified candidate commit or a bounded human clarification. The extension journals graph state beside the existing routing generation, validates candidates, preflights them in a disposable Git worktree, applies only the canonical conflict-free prefix to the integration worktree, and schedules only the first conflict for reexecution from the new HEAD. After every task is integrated, `review-and-fix` runs once on the combined worktree, then the current publication tools push, open/reuse one PR, and verify the exact remote HEAD.

**Tech stack:** Go 1.26.4, Compozy Go extension SDK `v0.3.0-beta.21` or the explicitly pinned compatible successor, Compozy Loop YAML, JSON journal with `flock`, managed worktree CLI JSON boundaries, Git, Python E2E assertions, shell contract tests, Astro 7 for the personal site.

**Approved design:** [2026-08-27-batuta-parallel-graph-delivery-design.md](../specs/2026-08-27-batuta-parallel-graph-delivery-design.md)

**Execution order:** Tasks 1–9 are sequential Batuta ownership boundaries. Task 10 is the separate personal-site repository change and starts only after Task 9 documents proven behavior. Task 11 is the final integrated gate across both candidates.

## Global constraints

- Do not edit, commit, branch, push, rebase, open an issue, or open a PR in `/home/francisross/Projects/opensource/compozy` without stopping and receiving explicit operator approval. Read-only contract inspection is allowed.
- Before Batuta production code depends on a Compozy behavior, prove it against the exact pinned Compozy source/binary. If `run-loop.params.config_overrides`, `ask`, fan-out isolation, managed worktrees, or typed child output is missing or differs, stop with the exact missing contract and impact.
- Do not add a Batuta database, Compozy migration, stored Loop configuration mutation, or a second planner agent.
- Keep the existing integration worktree and one-PR-per-phase publication boundary. Never publish task worktrees.
- Maximum ready tasks per wave and active task worktrees: `4`; maximum authored tasks: `64`; maximum executions per task: `4`; maximum fresh parent runs: `4`; cumulative tokens: `1_000_000`; cumulative active work: `4h`.
- A human clarification pauses only its task cell. Persisted human-wait intervals do not consume Batuta's active-wall accounting, but tokens and execution counts never reset.
- A task candidate is exactly one Conventional Commit ahead of its recorded base. Validate `<type>[optional scope][!]: <description>` before integration. Do not squash, rewrite, force-push, auto-merge conflicts, or mutate operator-owned branches.
- `review-and-fix` runs once after all task candidates are integrated. Task cells perform only focused verification and self-review.
- Every external side effect uses a deterministic operation ID and request digest. A replay with a different digest is a conflict.
- Do not use sleeps or polling in shell code. Product reconciliation uses bounded structured reads; test waits use the repository's event/status helpers.
- Keep build scratch rules from `AGENTS.md`: prefix commands with `rtk`; use a unique `/home/francisross/tmp-builds` directory for heavy scratch; never export `TMPDIR` globally.
- Commit no SDD implementation artifacts from Compozy. This plan and its approved spec live only in Batuta. QA evidence may be committed in Batuta.
- Do not claim a public tag, checksum, forge result, or released Compozy compatibility before that evidence exists.

---

## Task 1: Freeze the external Compozy capability boundary

**Files:**

- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `.github/workflows/ci.yml`
- Modify: `.github/workflows/release.yml`
- Modify: `tests/contract/test_00_runtime_guard.sh`
- Modify: `tests/contract/test_07_workflow_contract.sh`
- Create: `tests/contract/test_02_parallel_compozy_surface.sh`

### Step 1: Record the exact accepted source and binary identity

Read the current Batuta CI pin, the local compatible Compozy branch, and `origin/main` without changing Compozy:

```bash
rtk rg -n 'COMPOZY_COMMIT|github.com/compozy/compozy' .github go.mod
rtk git -C /home/francisross/Projects/opensource/compozy status --short --branch
rtk git -C /home/francisross/Projects/opensource/compozy log -1 --format='%H %s' origin/main
```

Select only a Compozy commit already approved/released by the operator. Update the SDK module and CI source pin together. Do not silently substitute a local-only branch.

### Step 2: Write the RED public-surface contract

`test_02_parallel_compozy_surface.sh` must inspect the pinned source, not a developer home path embedded in committed code. Pass the checked-out CI source root as an argument and assert all of:

- `dsl.RunLoopParams` exposes child-scoped `config_overrides`;
- `RunLoopActionExecutor` forwards those overrides to the child start input;
- Loop DSL includes `fan-out`, `collect`, `route`, `ask`, and `run-loop`;
- `ask` accepts a typed `expect` schema and creates a human request;
- the public request identity exposes exact run, generation, node, and fan-out item coordinates, and the answer result reports the winning durable request;
- Loop run status/list exposes the exact `batuta-task` inputs and child output needed to bind one running/completed child to delivery, wave, task, and execution without caller-authored identity;
- managed worktree CLI exposes create, inspect/status, and non-forced remove;
- the installed binary reports the exact source abbreviation expected by CI.

Run it first against an intentionally incompatible fixture or the old pin and require a focused non-zero result naming the missing symbol. Then run it against the approved pin.

```bash
rtk bash tests/contract/test_02_parallel_compozy_surface.sh /home/francisross/Projects/opensource/compozy
```

Expected RED: the old pin lacks at least child `config_overrides` propagation. Expected GREEN: every required public surface is found under the newly approved pin.

### Step 3: Keep the runtime guard exact

Extend `test_00_runtime_guard.sh` and workflow tests so source commit, built binary commit, SDK requirement, and extension minimum version are mutually consistent. Tests must reject prefix/substring matches and developer-local absolute paths.

### Step 4: Run focused gates

```bash
rtk bash -n tests/contract/test_02_parallel_compozy_surface.sh tests/contract/test_00_runtime_guard.sh tests/contract/test_07_workflow_contract.sh
rtk bash tests/contract/test_07_workflow_contract.sh
rtk go mod verify
rtk git diff --check
```

### Step 5: Review and commit

Request an independent read-only review of the boundary. If approved:

```bash
rtk git add go.mod go.sum .github/workflows/ci.yml .github/workflows/release.yml tests/contract
rtk git commit -m "build: pin parallel Compozy contract"
```

Stop immediately if satisfying the contract would require a new Compozy change.

---

## Task 2: Add the durable task graph and active-budget domain

**Files:**

- Create: `internal/routing/graph.go`
- Create: `internal/routing/graph_test.go`
- Modify: `internal/routing/delivery.go`
- Modify: `internal/routing/delivery_test.go`
- Modify: `internal/routing/ownership.go`
- Modify: `internal/routing/ownership_test.go`
- Modify: `internal/routing/artifacts.go`
- Modify: `internal/routing/artifacts_test.go`

### Step 1: Write RED DAG and wave tests

Define tests before production symbols for:

- no more than 64 task nodes;
- duplicate IDs, missing dependency IDs, self-dependencies, and cycles rejected;
- canonical domain/complexity required for every task;
- already completed authored tasks represented but not admitted;
- ready tasks require every dependency state `integrated` and dependency commit reachable from the current integration HEAD;
- stable order is topological order, then authored index, then task ID;
- wave size clamps to `min(4, remaining slots, ready tasks)`;
- incomplete graph with no ready task returns `dependency_blocked`, not an empty success.

Use pure domain fixtures. Supply reachability as deterministic input evidence; no Git, Compozy, clock, or journal I/O belongs in these tests.

```go
const (
    MaxParallelTasks = 4
    MaxDeliveryTasks = 64
    MaxTaskExecutions = 4
)

type GraphTaskState string

const (
    GraphTaskPending      GraphTaskState = "pending"
    GraphTaskPreparing    GraphTaskState = "preparing"
    GraphTaskRunning      GraphTaskState = "running"
    GraphTaskWaitingInput GraphTaskState = "waiting_input"
    GraphTaskCandidate    GraphTaskState = "candidate"
    GraphTaskIntegrated   GraphTaskState = "integrated"
    GraphTaskBlocked      GraphTaskState = "blocked"
)
```

Run the RED:

```bash
rtk go test ./internal/routing -run 'DeliveryGraph|ReadyWave|DAG' -count=1
```

Expected RED: graph types and planner do not exist.

### Step 2: Implement the minimal graph projection

Add closed JSON-tagged types:

```go
type DeliveryGraph struct {
    Tasks        []GraphTask             `json:"tasks"`
    Waves        []DeliveryWave           `json:"waves"`
    Integrations []IntegrationOperation   `json:"integrations"`
    Pauses       []HumanPause             `json:"pauses"`
}

type GraphTask struct {
    TaskID              string             `json:"task_id"`
    AuthoredIndex       int                `json:"authored_index"`
    Dependencies        []string           `json:"dependencies"`
    Domain              Domain             `json:"domain"`
    Complexity          Complexity         `json:"complexity"`
    State               GraphTaskState     `json:"state"`
    Attempts            []GraphTaskAttempt `json:"attempts"`
    IntegratedCommitSHA string             `json:"integrated_commit_sha,omitempty"`
    BlockerCode         string             `json:"blocker_code,omitempty"`
}

type GraphTaskAttempt struct {
    Execution          int             `json:"execution"`
    Runtime            RuntimeValue    `json:"runtime"`
    State              GraphTaskState  `json:"state"`
    BaseHeadSHA        string          `json:"base_head_sha"`
    WorktreeID         string          `json:"worktree_id,omitempty"`
    WorktreeRoot       string          `json:"worktree_root,omitempty"`
    ChildRunID         string          `json:"child_run_id,omitempty"`
    CandidateCommitSHA string          `json:"candidate_commit_sha,omitempty"`
    VerificationDigest string          `json:"verification_digest,omitempty"`
    Question           *TaskQuestion   `json:"question,omitempty"`
    Conflict           *ConflictProof  `json:"conflict,omitempty"`
    BlockerCode        string          `json:"blocker_code,omitempty"`
}
```

Keep question text/context bounded and redacted. Model wave base, task order, attempt/runtime, obsolete candidate/conflict evidence, and integration prefix explicitly. Attempts are append-only; a fallback or conflict reexecution creates a new attempt and never overwrites the old worktree, runtime, commit, question, or diagnostic evidence. Constructors clone slices/maps and validate every transition; callers never mutate stored slices by alias.

### Step 3: Extend `DeliveryRecord` additively

Add only:

```go
Graph *DeliveryGraph `json:"graph,omitempty"`
```

`nil` means the existing sequential delivery. Validation must preserve and replay legacy records without conversion. A new delivery constructs a graph once from the immutable task snapshot and routing generation. Tests must byte-round-trip a legacy schema-v2 fixture, accept it unchanged, and reject attempts to attach a graph after a legacy delivery has started.

Do not bump the routing journal schema merely for an optional additive field unless strict compatibility tests prove a bump is necessary. If a bump is necessary, stop and document why before implementing it.

### Step 4: Add active-work accounting

Model pause intervals as append-only identities:

```go
type HumanPause struct {
    TaskID    string     `json:"task_id"`
    Execution int        `json:"execution"`
    RequestID string     `json:"request_identity"`
    StartedAt time.Time  `json:"started_at"`
    EndedAt   *time.Time `json:"ended_at,omitempty"`
}
```

Write RED tests for open pause, closed pause, duplicate replay, overlapping intervals, future timestamps, missing start/end, cancellation, and resume. `RemainingActiveWall(now)` subtracts only valid durably parked intervals; token and execution counters never change. Contradictory timing fails closed.

### Step 5: Prove journal transitions and race safety

Add transition tests covering:

- pending → running → waiting_input → running → candidate → integrated;
- pending/running → blocked;
- conflict reexecution increments execution and resets only attempt-local fields;
- integrated is terminal and immutable;
- duplicate operation replay returns the durable graph exactly;
- cross-store writers cannot admit more than four running tasks.

```bash
rtk go test ./internal/routing -run 'DeliveryGraph|ReadyWave|ActiveWall|LegacyGraph' -count=1
rtk go test -race ./internal/routing -run 'DeliveryGraph|ReadyWave|ActiveWall|Journal' -count=1
rtk go vet ./internal/routing
rtk git diff --check
```

### Step 6: Review and commit

```bash
rtk git add internal/routing
rtk git commit -m "feat: persist parallel delivery graphs"
```

---

## Task 3: Add a bounded managed task-worktree boundary

**Files:**

- Create: `internal/worktreeops/types.go`
- Create: `internal/worktreeops/client.go`
- Create: `internal/worktreeops/client_test.go`
- Modify: `internal/extensionapp/app.go`
- Modify: `internal/extensionapp/app_test.go`

### Step 1: Define RED JSON and command-boundary tests

Introduce a narrow interface owned by Batuta rather than expanding publication concepts:

```go
type Client interface {
    Create(context.Context, publication.TrustedScope, CreateRequest) (Worktree, error)
    Inspect(context.Context, publication.TrustedScope, string) (Worktree, error)
    Remove(context.Context, publication.TrustedScope, string) (Worktree, error)
}

type CreateRequest struct {
    Name    string `json:"name"`
    Branch  string `json:"branch"`
    BaseSHA string `json:"base_sha"`
}
```

The returned `Worktree` must include canonical daemon-owned ID, root, workspace ID, repository identity, branch, base ref/SHA, lifecycle state, and setup result. RED tests cover exact argv, working directory, JSON shape, additive unknown upstream fields, duplicate/trailing JSON rejection, malformed IDs/SHA/path, wrong workspace, path outside trusted repository, stdout/stderr limits, timeout, and manual cancellation.

Exact CLI templates:

```text
compozy worktree create <name> --workspace <workspace_id> --branch <branch> --base <sha> -o json
compozy worktree inspect <worktree_id> --workspace <workspace_id> -o json
compozy worktree remove <worktree_id> --workspace <workspace_id> --force -o json
```

`--force` is permitted only after Batuta proves the exact journal-owned temporary task worktree satisfies the cleanup gate. It is forbidden for the delivery worktree, foreign worktrees, dirty/ambiguous state, or any caller-supplied path. This is automatic cleanup of Batuta-owned disposable state, not a routine operator gate.

### Step 2: Implement collision-safe deterministic names

Derive names and branches from immutable identities, never agent text:

```text
name   = batuta-<slug>-<task-id>-a<execution>-<digest8>
branch = batuta/task/<delivery12>/<task-id>/a<execution>-<digest8>
```

Sanitize only through a single tested canonicalizer; preserve task identity separately. Operation ID covers workspace, delivery, wave, task, execution, base SHA, name, and branch.

### Step 3: Implement create/reconcile without sleeping

`Create` performs one bounded command. If the response is pending, graph orchestration records `preparing`; the next idempotent graph transition uses `Inspect`. Do not loop or sleep in the extension call. Reuse is allowed only when every trusted field equals the journal. A same-name foreign or drifted worktree blocks.

### Step 4: Implement evidence-gated removal

Removal requires the caller to prove that the task is integrated or superseded with retained diagnostics, the worktree ID matches the journal, the root is not the integration root, and Git evidence is clean/reachable. The client itself performs only the public remove command; orchestration owns admissibility.

### Step 5: Wire dependency injection and verify

Add `worktreeops.Client` to the application service composition without registering a tool yet. `newWithServices` tests use fakes; `New` uses the same fixed absolute Compozy executable and bounded `publication.ExecRunner`.

```bash
rtk go test ./internal/worktreeops ./internal/extensionapp -run 'Worktree|ApplicationWiring' -count=1
rtk go test -race ./internal/worktreeops ./internal/extensionapp -run 'Worktree|ApplicationWiring' -count=1
rtk go vet ./internal/worktreeops ./internal/extensionapp
rtk git diff --check
```

### Step 6: Review and commit

```bash
rtk git add internal/worktreeops internal/extensionapp/app.go internal/extensionapp/app_test.go
rtk git commit -m "feat: own task worktree lifecycle"
```

---

## Task 4: Add deterministic candidate validation and integration

**Files:**

- Create: `internal/integration/types.go`
- Create: `internal/integration/git.go`
- Create: `internal/integration/git_test.go`
- Create: `internal/integration/service.go`
- Create: `internal/integration/service_test.go`
- Modify: `internal/publication/command.go`
- Modify: `internal/publication/command_test.go`

### Step 1: Write RED candidate validation tests

Define a narrow Git seam:

```go
type Git interface {
    Candidate(context.Context, CandidateRequest) (CandidateEvidence, error)
    Preflight(context.Context, PreflightRequest) (PreflightResult, error)
    Apply(context.Context, ApplyRequest) (ApplyResult, error)
    Reconcile(context.Context, ReconcileRequest) (ApplyResult, error)
}
```

Candidate tests require:

- expected worktree/root/base/branch and clean product state;
- `git rev-list --reverse --ancestry-path <base>..HEAD` returns exactly one commit;
- the commit subject follows Conventional Commits and remains unchanged during integration;
- commit/tree SHA and task identity are valid;
- task-local tracking changes stay under `.compozy/tasks/<slug>/`;
- no ignored/untracked product mutation is silently accepted;
- verification evidence is non-empty, bounded, canonical JSON, and digest-matched.

Reject zero or multiple commits, merge commits, base drift, detached/foreign branch, dirty code, malformed SHA, symlink escape, missing verification, and cancellation. Pin exact argv and directory in tests.

The one candidate implementation commit may contain product changes plus its own task-local SDD tracking. Any residual uncommitted change is allowed only under those same task-local paths. Shared memory/manifest changes are rejected from the task candidate and rebuilt centrally after integration.

### Step 2: Write RED preflight-prefix tests

Given candidates in canonical order, preflight must create a disposable detached Git worktree at the expected integration HEAD, cherry-pick in order, and return:

```go
type PreflightResult struct {
    OperationID         string   `json:"operation_id"`
    RequestDigest       string   `json:"request_digest"`
    StartingHeadSHA     string   `json:"starting_head_sha"`
    AcceptedTaskIDs     []string `json:"accepted_task_ids"`
    AcceptedCommitSHAs  []string `json:"accepted_commit_shas"`
    FirstConflictTaskID string   `json:"first_conflict_task_id,omitempty"`
    ResultingHeadSHA    string   `json:"resulting_head_sha"`
}
```

Test all-clean, conflict-first, conflict-middle, conflict-last, cancellation, temp cleanup, and bounded diagnostics. A conflict returns evidence, not raw stderr. Create scratch below a secure Batuta cache root such as `os.UserCacheDir()/batuta/integration`, never below the trusted repository or an agent-supplied path. The production implementation may invoke `git cherry-pick --abort` only inside its own disposable worktree and must remove only the exact temp path it created.

### Step 3: Implement actual apply with compare-before-mutate

Before the real integration worktree changes, reacquire the graph journal lock and verify expected HEAD, cleanliness, and candidate prefix. Apply only the preflight-proven prefix. Do not use `git reset --hard`, `git checkout --`, force options, or automatic conflict resolution.

Journal the operation intent before the first cherry-pick and the exact resulting prefix after each accepted commit. After a prefix is durable, copy only its validated task-local SDD evidence into the integration worktree and recompute shared tracking from the journal. Respect Git ignore rules. If those tracking files are tracked and changed, create one deterministic `chore: sync Batuta task tracking` metadata commit for the accepted prefix and journal its SHA separately from task implementation commits; if there is no tracked diff, create no commit. If a command outcome is ambiguous, return for `Reconcile`; do not run a second mutation.

### Step 4: Implement crash reconciliation

Reconciliation accepts only these exact states:

- HEAD equals starting HEAD: no candidate applied, safe to retry the same operation once;
- HEAD equals a reachable prefix recorded by the operation: persist that prefix and continue from the next candidate;
- HEAD equals the full expected prefix: replay success;
- any foreign commit, reorder, dirty state, or unreachable prefix: block.

Add fault injection after intent persistence and after each candidate apply. Prove replay never duplicates a commit.

Add parallel tracking tests proving two task-local updates cannot overwrite one another, shared tracking is derived from integrated journal state rather than last-writer content, the optional metadata commit is deterministic/idempotent, and ignored files never become force-added.

### Step 5: Keep command output bounded during execution

If the shared runner cannot prove streaming-time stdout/stderr bounds, make the narrowest compatible change in `internal/publication/command.go` and add regression tests there. Preserve all current publication behavior and error redaction.

### Step 6: Verify and commit

```bash
rtk go test ./internal/integration ./internal/publication -run 'Candidate|Preflight|Apply|Reconcile|Command' -count=1
rtk go test -race ./internal/integration ./internal/publication -run 'Candidate|Preflight|Apply|Reconcile|Command' -count=1
rtk go vet ./internal/integration ./internal/publication
rtk git diff --check
```

```bash
rtk git add internal/integration internal/publication/command.go internal/publication/command_test.go
rtk git commit -m "feat: integrate task commits deterministically"
```

---

## Task 5: Expose one closed delivery-graph coordination tool

**Files:**

- Create: `internal/extensionapp/delivery_graph.go`
- Create: `internal/extensionapp/delivery_graph_test.go`
- Create: `internal/extensionapp/delivery_graph_service.go`
- Create: `internal/extensionapp/delivery_graph_service_test.go`
- Modify: `internal/extensionapp/app.go`
- Modify: `internal/extensionapp/app_test.go`
- Modify: `internal/extensionapp/routing_context.go`
- Modify: `internal/extensionapp/routing_context_test.go`
- Modify: `internal/routing/delivery.go`
- Modify: `internal/routing/delivery_test.go`
- Modify: `tests/contract/test_01_validate.sh`

### Step 1: Define the RED closed union

Register one additional tool, `ext__batuta__delivery_graph`, classified mutating because some operations cause worktree/Git side effects. Its input is a JSON Schema `oneOf` with no extra properties. Operations are:

```go
const (
    GraphOpPrepareWave     GraphOperation = "prepare_wave"
    GraphOpTaskContext     GraphOperation = "task_context"
    GraphOpRecordQuestion GraphOperation = "record_question"
    GraphOpRecordAnswer   GraphOperation = "record_answer"
    GraphOpRecordCandidate GraphOperation = "record_candidate"
    GraphOpRecordFailure  GraphOperation = "record_failure"
    GraphOpSettleWave     GraphOperation = "settle_wave"
    GraphOpCleanup        GraphOperation = "cleanup"
)
```

Every operation requires `delivery_id`; task operations additionally require exact `wave`, `task_id`, and `execution`. Candidate and failure operations require `child_run_id`; candidate additionally requires base/commit SHA and verification evidence. Question accepts at most four choices and bounded context. Answer requires the deterministic question operation ID and the Compozy human-request result identity.

The output always contains `operation` and one disposition from:

```text
preparing | wave_ready | task_ready | waiting_input | candidate_recorded |
wave_integrated | reexecute_conflict | all_integrated | cleaned | blocked | exhausted
```

RED tests assert exact schema closure, required fields, maximums, and that every handler rejects a missing daemon-trusted workspace before any service call. The extension descriptor count becomes `9`, with exact sorted IDs updated in Go and shell contracts.

### Step 2: Implement `prepare_wave`

Under the workspace journal lock:

1. load the exact delivery/generation/graph;
2. reconcile any earlier preparing/running worktree operation;
3. compute remaining token/active-wall/task capacity;
4. select the stable next ready wave;
5. persist wave intent with one immutable base HEAD;
6. create at most four task worktrees, one public call per task operation;
7. on replay inspect, never duplicate;
8. return `preparing` until every worktree is daemon-ready, then `wave_ready` with immutable task descriptors.

Because no extension call polls or sleeps, `batuta-deliver` advances generations while a wave is `preparing`; its existing no-progress/parent caps remain authoritative.

### Step 3: Implement `task_context`, question, answer, and terminal failure

`task_context` is side-effect-free even though it shares a mutating descriptor. It returns only the exact task file, runtime, answer history, remaining task/global budget, worktree ID/root, base SHA, and current disposition.

`record_question` changes only running → waiting_input and opens one pause interval. Reject a second open question, more than four choices, secrets/paths outside bounded context, or a fourth execution without completion.

Because an action template does not expose its own Loop run ID, `record_question` resolves exactly one live `batuta-task` run by daemon-trusted workspace plus immutable inputs (`delivery_id`, wave, task ID, execution), then persists that authoritative child run ID. Zero or multiple matches block before opening a pause.

`record_answer` rereads that exact child and validates the public request composite identity (`loop_run_id`, generation, node, item index), answered state, and the schema-valid answer passed directly from the `ask` node. It closes exactly one pause, stores the answer, increments execution only when a new implementation turn begins, and returns replay for duplicates. Do not invent a random request ID that the public Compozy contract does not expose.

`record_failure` is designed for `batuta-task` terminal effects. It validates child run ownership/status, records usage once, advances only the failed task's immutable fallback chain, and blocks when no candidate or budget remains.

### Step 4: Implement candidate settlement and conflict reexecution

`record_candidate` is called by the parent fan-out branch after awaited `run-loop` success. It requires the authoritative child run ID returned by `run-loop`, rereads that exact `batuta-task` status/output, proves its immutable inputs and `completed` outcome, calls the Task 4 Git validator, and persists exactly one candidate. The child never claims its own run identity. `settle_wave`:

- orders journal candidates canonically;
- preflights and applies the maximal conflict-free prefix;
- marks only accepted tasks integrated;
- preserves later candidates;
- when conflict exists, increments only the first conflicting task execution, selects its next eligible runtime, and returns `reexecute_conflict` from the latest integrated HEAD;
- returns `all_integrated` only when every authored task is integrated/reachable.

Do not call final review or publication from this service.

### Step 5: Implement idempotent cleanup

`cleanup` removes only graph-owned task worktrees whose commit/evidence state satisfies Task 3. It records each cleanup operation/result. Blocked, dirty, ambiguous, or foreign worktrees are retained and reported. A replay issues zero duplicate removes.

### Step 6: Extend remaining-budget context

Make `delivery_budget_context` use cumulative graph token usage and active-wall accounting. It must reject new wave/review/publication admission after any applicable ceiling, while an open human pause itself is not an error.

### Step 7: Verify and commit

```bash
rtk go test ./internal/extensionapp ./internal/routing ./internal/worktreeops ./internal/integration -run 'DeliveryGraph|PrepareWave|TaskContext|Question|Answer|Candidate|Settle|Cleanup|Budget' -count=1
rtk go test -race ./internal/extensionapp ./internal/routing ./internal/worktreeops ./internal/integration -run 'DeliveryGraph|PrepareWave|TaskContext|Question|Answer|Candidate|Settle|Cleanup|Budget' -count=1
rtk go vet ./internal/extensionapp ./internal/routing ./internal/worktreeops ./internal/integration
rtk bash tests/contract/test_01_validate.sh
rtk git diff --check
```

```bash
rtk git add internal/extensionapp internal/routing tests/contract/test_01_validate.sh
rtk git commit -m "feat: coordinate parallel delivery waves"
```

---

## Task 6: Add `batuta-task` and interactive clarification paths

**Files:**

- Create: `loops/batuta-task/loop.yaml`
- Modify: `main_test.go`
- Modify: `agents/batuta/AGENT.md`
- Modify: `resources/skills/batuta-routing/SKILL.md`
- Modify: `internal/extensionapp/app_test.go`
- Modify: `tests/contract/test_01_stage.sh`
- Modify: `tests/contract/test_02_spec_cycle_surface.sh`
- Create: `tests/e2e/assert_interactive_delivery.py`
- Create: `tests/e2e/test_assert_interactive_delivery.py`

### Step 1: Write RED Loop structure tests

Add typed YAML tests in `main_test.go` that require:

- loop name `batuta-task`, `concurrency: queue`, max four generations/executions;
- immutable inputs for delivery, wave, task, execution, routing generation, runtime, worktree, base SHA, and budget;
- a first `ext__batuta__delivery_graph task_context` node;
- `run-agent` using `code_implementer`, exact runtime, isolated session, and the inherited assigned worktree environment;
- implementation output schema with exactly `completed` or `needs_input`;
- `needs_input` route through `record_question` → native `ask` → `record_answer`;
- `completed` becomes the child Loop's typed terminal output; candidate recording belongs to the parent branch that has the public child run ID;
- `stop_when` only after one typed `completed` output;
- terminal effects record task failure with child run/generation identity;
- no `review-and-fix`, publication, raw worktree, or routing-selection calls.

Run RED before creating the YAML:

```bash
rtk go test . -run 'BatutaTask|InteractiveTask' -count=1
```

### Step 2: Implement the task Loop

The implementer prompt must:

- read the exact approved task and SDD files;
- use `cy-workflow-memory`, `cy-execute-task`, and `cy-final-verify`;
- begin without an approval question;
- make technical decisions inside approved scope;
- use `needs_input` only for a material product decision/external value;
- return one question, two to four concise choices when closed, recommended choice first, and bounded context;
- perform focused verification/self-review;
- create exactly one local implementation commit and never push;
- never invoke the full `review-and-fix` Loop.

Use a typed `ask` expectation:

```yaml
expect:
  type: object
  additionalProperties: false
  required: [answer]
  properties:
    answer:
      type: string
      minLength: 1
      maxLength: 4096
```

Set `responders.agents: deny` explicitly so the starter and its agent descendants cannot answer their own task question.

Render proposed choices into the human prompt, but allow the typed free-text answer. The next generation obtains the journaled answer from `task_context` and resumes the same task/worktree.

### Step 3: Update SDD interaction in the Batuta agent

Change `AGENT.md` and the routing skill so material SDD ambiguity uses `compozy__clarify` one question at a time:

- operator language;
- two to four mutually exclusive choices;
- recommended first with concise impact;
- free text accepted;
- next question only after settlement;
- no guessed default and no delegated planner.

Do not use `ask` during SDD authorship; `ask` is only for a running Loop cell. Do not make normal explanation or final spec approval into clarification cards.

### Step 4: Add pure E2E assertion fixtures

`assert_interactive_delivery.py` consumes a saved event fixture and proves:

- one clarification card pending at a time during SDD;
- one task human request parks while sibling task events continue;
- the winning answer is associated with the same task/worktree and next execution;
- invalid/expired/canceled answer is never converted to guessed input.

### Step 5: Verify and commit

```bash
rtk go test . ./internal/extensionapp -run 'BatutaTask|InteractiveTask|Describe' -count=1
rtk python3 -m unittest tests.e2e.test_assert_interactive_delivery -v
rtk bash tests/contract/test_01_stage.sh
rtk bash tests/contract/test_02_spec_cycle_surface.sh
rtk git diff --check
```

```bash
rtk git add loops/batuta-task agents/batuta resources/skills/batuta-routing main_test.go internal/extensionapp/app_test.go tests
rtk git commit -m "feat: add interactive task loop"
```

---

## Task 7: Refactor `batuta-deliver` into the parallel graph engineer

**Files:**

- Modify: `loops/batuta-deliver/loop.yaml`
- Modify: `main_test.go`
- Modify: `internal/extensionapp/delivery_client.go`
- Modify: `internal/extensionapp/delivery_client_test.go`
- Modify: `internal/extensionapp/routing_recovery.go`
- Modify: `internal/extensionapp/routing_recovery_test.go`
- Modify: `internal/extensionapp/routing_context.go`
- Modify: `internal/extensionapp/routing_context_test.go`
- Modify: `tests/e2e/assert_event_driven_return.py`
- Modify: `tests/e2e/test_assert_event_driven_return.py`

### Step 1: Write RED parent graph assertions

Replace the current linear-implementation assertions with a typed structure test requiring this order:

```text
prepare_wave
  -> route(preparing | wave_ready | all_integrated | blocked/exhausted)
  -> fan-out(max_parallel=4, max_fan_out=4)
  -> run-loop batuta-task with per-child config_overrides/environment
  -> record_candidate with run-loop child ID inside the same fan-out cell
  -> collect without canceling healthy siblings
  -> settle_wave
  -> next generation while incomplete
  -> delivery_budget_context
  -> review-and-fix exactly once
  -> publication_plan -> publish/open PR -> publication_verify
  -> cleanup
```

Require `load_check` and routing generation validation before the first wave. Forbid direct `implement-tasks`. Require one final `review-and-fix` node outside fan-out and no human gate.

### Step 2: Implement a generation-driven parent Loop

`prepare_wave` returns immutable task descriptors used as the fan-out collection. Set `batch_size: 1`, `max_parallel: 4`, and `max_fan_out: 4`. Each `run-loop batuta-task` receives exact task identities and child `config_overrides`:

```yaml
config_overrides:
  iteration_cap: 4
  budget_tokens: "{{ .item.remaining_tokens }}"
  budget_wall_sec: "{{ .item.remaining_active_wall_seconds }}"
  budget_on_exceeded: halt
  reattempt_strategy: halt
  runtime_rules:
    - match:
        id: "{{ .item.task_id }}"
      runtime: "{{ .item.runtime }}"
  environment:
    mode: worktree
    worktree_ref: "{{ .item.worktree_id }}"
```

The parent `stop_when` becomes graph-complete, not first-wave-complete. Configure enough parent generations for at most 64 tasks while keeping fresh parent starts capped at four in the Batuta journal. Do not confuse Compozy generation count with the fresh-parent recovery ceiling.

If the released child-override syntax differs, stop and report the contract mismatch; do not add a Compozy workaround.

### Step 3: Preserve parent fallback and event-driven return

Update `deliveryLoopCLIClient` validation/config so a graph parent can use the bounded generation cap required by 64 tasks while the delivery record still limits fresh parent runs to four. Parent terminal reconciliation must preserve graph-integrated tasks, candidates, open human requests, and task execution counts.

Keep the origin-session terminal effect idempotent for all seven terminal states. The Batuta agent reads exact status and calls guarded reconcile/recover; it never polls.

### Step 4: Run one final review only

The graph routes to final review only on `all_integrated`. The review receives the integration worktree, remaining global tokens/active wall, `iteration_cap: 4`, `reattempt_strategy: halt`, and `auto_commit: true`. After review, reread publication plan and publish/verify the exact reviewed HEAD. Cleanup happens only after durable publication verification or truthful nothing-to-publish; blocked publication retains diagnostic worktrees.

### Step 5: Verify and commit

```bash
rtk go test . ./internal/extensionapp -run 'Delivery|Graph|Parent|Fallback|EventDriven' -count=1
rtk go test -race ./internal/extensionapp ./internal/routing -run 'Delivery|Graph|Fallback|Budget' -count=1
rtk python3 -m unittest tests.e2e.test_assert_event_driven_return -v
rtk go vet . ./internal/extensionapp ./internal/routing
rtk git diff --check
```

```bash
rtk git add loops/batuta-deliver main_test.go internal/extensionapp tests/e2e
rtk git commit -m "feat: run dependency-safe task waves"
```

---

## Task 8: Prove the complete deterministic local scenario

**Files:**

- Create: `tests/fixtures/parallel-delivery/.compozy/tasks/parallel-demo/_manifest.json`
- Create: `tests/fixtures/parallel-delivery/.compozy/tasks/parallel-demo/task_01.md`
- Create: `tests/fixtures/parallel-delivery/.compozy/tasks/parallel-demo/task_02.md`
- Create: `tests/fixtures/parallel-delivery/.compozy/tasks/parallel-demo/task_03.md`
- Create: `tests/fixtures/parallel-delivery/.compozy/tasks/parallel-demo/task_04.md`
- Create: `tests/fixtures/parallel-delivery/.compozy/tasks/parallel-demo/task_05.md`
- Create: `internal/extensionapp/parallel_delivery_integration_test.go`
- Create: `tests/e2e/assert_parallel_delivery.py`
- Create: `tests/e2e/test_assert_parallel_delivery.py`
- Modify: `tests/contract/run.sh`
- Create: `tests/contract/test_06_parallel_delivery.sh`
- Create: `docs/internal/qa/2026-08-27-batuta-parallel-delivery-local.md`

### Step 1: Build the five-task fixture

Use at least:

- backend task;
- frontend task routed to the configured Cursor executor/runtime fixture;
- testing task;
- docs task;
- one dependent fullstack/general task.

Use two dependency levels. Make four tasks initially ready. One task must request a material typed clarification. Two independent tasks must intentionally edit the same fixture line so canonical integration produces one conflict. The dependent task must remain inadmissible until its prerequisites integrate.

### Step 2: Write the RED integration assertion

Before wiring the real service, require a deterministic trace proving:

1. four distinct managed task worktree IDs share one recorded base;
2. four child runs start without a fifth concurrent task;
3. one child reaches a human request while at least one sibling completes;
4. the answer resumes the same task/worktree;
5. canonical prefix integrates and one conflict is retained without partial ambiguity;
6. only the conflicting task reexecutes from the resulting newest HEAD;
7. the dependent task starts only after prerequisite commits are reachable;
8. each task contributes exactly one implementation commit;
9. final review starts once after all integrations;
10. publication plan/push/PR/verify use one exact reviewed HEAD;
11. replay at every side-effect boundary produces zero duplicate worktrees, child starts, commits, pushes, PRs, or removes;
12. safely removable task worktrees and temporary processes are gone; retained blockers are explicitly listed.

### Step 3: Run the real pinned Compozy harness

Use an isolated `COMPOZY_HOME`, workspace, port, and disposable Git repository. Do not use personal production configuration. Start the pinned approved Compozy binary, install the staged Batuta extension, register the fixture workspace, and drive the Loop through public surfaces. Inject the human response through the public human-request surface.

No real provider or forge claim is required here: deterministic local executor and forge fixtures are acceptable if the trace distinguishes them from live proof.

### Step 4: Test stop conditions

Add table cases for:

- fifth concurrent worktree denied;
- 65th task rejected before side effects;
- fourth failed task execution permits no fifth;
- fourth fresh parent run permits no fifth;
- token ceiling, active-wall ceiling, cancel, stalled, and no-progress boundaries;
- open human pause excludes only its exact interval;
- ambiguous worktree/Git/journal evidence blocks without cleanup.

Every rejected case compares the journal, repository refs, worktree inventory, child runs, and external mutation counts before/after.

### Step 5: Record honest QA evidence

Create the dated, version-neutral parallel-delivery QA file instead of rewriting beta-5 evidence. Record exact source commit, binary identity, extension version, commands, scenario IDs, operation IDs, retained evidence, and teardown. Use `blocked-verify` for any live-provider/forge claim not actually exercised.

### Step 6: Run integration gates and commit

```bash
rtk go test -tags=integration ./internal/extensionapp -run 'ParallelDelivery' -count=1 -v
rtk python3 -m unittest discover -s tests/e2e -p 'test_*.py' -v
rtk tests/contract/test_06_parallel_delivery.sh
rtk git diff --check
```

```bash
rtk git add tests internal/extensionapp/parallel_delivery_integration_test.go docs/internal/qa
rtk git commit -m "test: prove parallel Batuta delivery"
```

---

## Task 9: Update Batuta release docs and architecture visual

**Files:**

- Modify: `README.md`
- Modify: `docs/architecture.md`
- Modify: `docs/how-it-works.md`
- Modify: `docs/verify.md`
- Modify: `docs/images/batuta-next-roadmap.png`
- Create: `docs/releases/0.1.0-beta.6.md` only if `beta.6` is the next unused remote version
- Modify: `CHANGELOG.md` if present; otherwise do not invent one
- Modify: `docs/internal/handoffs/2026-08-25-batuta-next-demo-runbook.md`
- Modify: `tests/e2e/SMOKE.md`
- Modify: `tests/contract/test_07_preview_docs.sh`
- Modify: `tests/contract/test_07_public_docs.sh`
- Modify: `tests/contract/test_07_workflow_contract.sh`
- Modify: `internal/extensionapp/app.go`
- Modify: `internal/extensionapp/app_test.go`

### Step 1: Establish the release number from evidence

Inspect remote tags/releases before editing version strings. Use `0.1.0-beta.6` only if unused; otherwise choose the next beta and apply it atomically across extension definition, tests, notes, workflows, and docs. Do not create a tag or GitHub release in this task.

### Step 2: Generate the approved architecture image

Use the `imagegen` skill to replace the roadmap image with a presentation-ready visual showing:

```text
Batuta agent -> interactive SDD -> inventory -> domain/complexity matrix
             -> dependency graph -> 4 isolated task worktrees
             -> task ask/resume -> canonical integration
             -> one final review -> push + one PR + exact verification
```

The image must not show a routine human gate, a separate publisher agent, one PR per task, or Batuta editing implementation code. Inspect the generated image before accepting it and keep alt text/captions accurate.

### Step 3: Update public docs and demo route

Document:

- Go extension and exact resource/tool inventory;
- interactive SDD cards versus in-delivery `ask`;
- automatic executor inventory and domain/complexity selection;
- max-four dependency-safe parallelism;
- task versus integration worktrees;
- canonical conflict reexecution;
- one final review and automatic publication;
- stop conditions and retained evidence;
- merge remains manual;
- explicit Compozy minimum compatible version/commit.

Update the UI-first demo runbook: create a small workspace/project, ask Batuta to author SDD/tasks, select a clarification card, show routing and parallel worktrees in the Compozy UI, answer one parked task request, show canonical integration/final review, and inspect publication evidence. CLI may appear only in setup/diagnostics, not as the primary presentation path.

### Step 4: Update release inventory contracts

Contracts must expect:

- agent `batuta`;
- loops `batuta-deliver` and `batuta-task`;
- skill `batuta-routing`;
- exactly nine Batuta tools including `ext__batuta__delivery_graph`;
- staged Go sources and both loop files;
- no plan/spec/QA files inside the extension package.

### Step 5: Verify and commit

```bash
rtk bash -n tests/contract/*.sh scripts/*.sh
rtk tests/contract/test_07_public_docs.sh
rtk tests/contract/test_07_preview_docs.sh
rtk tests/contract/test_07_workflow_contract.sh
rtk go test . ./internal/extensionapp -run 'Describe|Version|Delivery' -count=1
rtk git diff --check
```

```bash
rtk git add README.md docs tests/contract internal/extensionapp/app.go internal/extensionapp/app_test.go
rtk git commit -m "docs: explain parallel Batuta delivery"
```

---

## Task 10: Update the personal-site article and Batuta docs

**Repository:** `/home/francisross/Projects/francisross`

**Files:**

- Modify: `src/content/blog/batuta-compozy-journey.pt.mdx`
- Modify: `src/content/blog/batuta-compozy-journey.en.mdx`
- Modify: `src/lib/diagrams/compozyos.ts`
- Modify: `src/lib/batuta-docs.ts`
- Modify only if required by the shared content contract: `src/pages/opensource/batuta.astro`
- Modify only if required by the shared content contract: `src/pages/en/opensource/batuta.astro`

### Step 1: Create an isolated site worktree

Read `/home/francisross/Projects/francisross/AGENTS.md`, verify the repository is clean/current, and create a dedicated worktree/branch. Do not edit the site `main` checkout directly.

### Step 2: Preserve the historical article and add a dated evolution

Keep the original resource-only beta narrative explicitly historical. Add a dated section explaining the evolution to:

- a code-backed Go extension;
- immutable executor inventory/routing generation;
- domain and complexity lanes;
- parallel task worktrees and graph waves;
- clarification cards and parked task questions;
- deterministic integration and conflict reexecution;
- one final review and automatic PR verification.

Update title/excerpt only if they remain truthful in both languages. Keep PT/EN claims semantically paired. Do not rewrite old beta.2 evidence as if it described the new release.

### Step 3: Replace stale diagrams and docs copy

Update `batutaFlow` or add a new shared diagram function for the current architecture. Remove the old human `auto_commit` gate and sequential `implement-tasks` picture from the current-state diagram while keeping a historical caption where relevant.

Update `batuta-docs.ts`, which currently describes the old Claude Code/WORK.md product, to the Compozy extension architecture. Keep the existing pages consuming one shared bilingual content contract; avoid duplicating markup.

### Step 4: Add truthfulness tests through build/check

Search and remove current-state contradictions such as “resource-only”, “exactly three resources”, “no code”, old `auto_commit` gate, `/batuta` Claude Code installation, and one sequential implementation Loop. Historical passages may keep those strings only when clearly dated and scoped.

### Step 5: Verify and commit separately

Use a unique build scratch directory only if Astro/sharp creates heavy temp data:

```bash
rtk npm run check
rtk npm run build
rtk git diff --check
```

Review PT/EN parity and diagram rendering before commit:

```bash
rtk git add src/content/blog/batuta-compozy-journey.pt.mdx src/content/blog/batuta-compozy-journey.en.mdx src/lib/diagrams/compozyos.ts src/lib/batuta-docs.ts src/pages
rtk git commit -m "docs: update the Batuta Compozy journey"
```

Keep the site commit and Batuta commit in separate repositories/PRs.

---

## Task 11: Run final gates and prepare release handoff

**Files:**

- Modify with fresh evidence only: `docs/internal/qa/2026-08-27-batuta-parallel-delivery-local.md`
- Create: `docs/internal/handoffs/2026-08-27-batuta-parallel-delivery-handoff.md`

### Step 1: Run fresh Batuta unit/race/vet gates

```bash
rtk go test ./... -count=1
rtk go test -race ./... -count=1
rtk go vet ./...
rtk python3 -m unittest discover -s tests/e2e -p 'test_*.py' -v
rtk bash -n scripts/*.sh tests/contract/*.sh
rtk git diff --check
```

### Step 2: Run staging and disposable contract suite

Create a detached disposable worktree from the candidate commit, use an isolated `COMPOZY_HOME`, build the exact approved Compozy pin, stage/validate/install Batuta, and run:

```bash
rtk tests/contract/run.sh
```

Capture exact source/binary/extension identities and guaranteed teardown. Do not use the operator's production Compozy configuration.

### Step 3: Re-run the complete parallel scenario

Run the Task 8 integration once from the final candidate. Verify no survivor processes/worktrees, no duplicate journal operations, one final review, one publication operation sequence, and one exact PR URL in the fixture.

### Step 4: Run personal-site gates on its candidate commit

```bash
rtk npm run check
rtk npm run build
rtk git diff --check
```

### Step 5: Independent final review

Request a read-only whole-branch review against the approved spec, with special attention to:

- worktree identity and deletion safety;
- graph transition/idempotency completeness;
- conflict prefix reconciliation;
- active-wall accounting around human requests;
- runtime/fallback provenance;
- one-commit-per-task and one-final-review invariants;
- exact remote publication verification;
- Compozy boundary compliance;
- documentation truthfulness and PT/EN parity.

Fix only validated findings with focused RED/GREEN proof and rerun affected gates plus the final core gate.

### Step 6: Write the handoff

The handoff must state:

- Batuta and site candidate commits/branches;
- exact compatible Compozy source and binary identity;
- installed extension version/resource/tool inventory;
- verification commands and results;
- deterministic scenario operation IDs and teardown evidence;
- any live-provider/live-forge evidence still `blocked-verify`;
- release/tag/PR actions not yet performed;
- explicit reminder that merge remains manual and no Compozy changes were made by this plan.

### Step 7: Stop before publication

Do not tag, release, push a Batuta/site branch, or open/activate PRs until the operator reviews the final handoff and explicitly authorizes those external mutations.

---

## Acceptance checklist

- [ ] Exact approved Compozy pin proves every consumed public contract; no Compozy repository mutation occurred.
- [ ] Batuta SDD uses one bounded `compozy__clarify` card at a time for material ambiguities.
- [ ] Complete authored DAG is validated before any task worktree side effect.
- [ ] At most four dependency-ready tasks run concurrently, each in a distinct managed worktree.
- [ ] Every task uses the immutable routed runtime and contributes exactly one verified implementation commit.
- [ ] One task `ask` parks without preventing independent sibling progress and resumes with the exact answer.
- [ ] Deterministic preflight integrates only a canonical conflict-free prefix.
- [ ] Only the first conflicting task reexecutes from the newest integrated HEAD within its original caps.
- [ ] Journal replay is idempotent across worktree, child Loop, integration, cleanup, push, and PR boundaries.
- [ ] Tokens, execution counts, and parent-run caps never reset; only valid parked intervals are excluded from active-wall use.
- [ ] `review-and-fix` runs once on the fully integrated worktree.
- [ ] Publication pushes/reuses one branch, opens/reuses one PR, and independently verifies the exact reviewed remote HEAD.
- [ ] Safe task worktrees are removed; blocked/ambiguous evidence is retained.
- [ ] Batuta release docs, UI-first demo runbook, architecture image, QA, and release inventory are current.
- [ ] Portuguese and English site article/docs preserve history while explaining the current architecture.
- [ ] Unit, race, vet, E2E, contract, integration, staging/install, and Astro check/build gates pass from final candidates.
