# Parallel delivery — deterministic local QA

Date: 2026-08-28

## Scope

The deterministic `parallel-demo` fixture is the canonical
`compozy.tasks/v2` `_tasks.md` graph: four independent initial tasks and a
fifth task dependent on all four. The integration-tag Go harness exercises
production Batuta routing, matrix/journal, delivery-graph, integration,
publication planner/publisher/verifier, cleanup, and real disposable Git
worktrees/commits. Its only deterministic seams are child-run/provider data,
review completion, and forge PR response. Python consumes Go-emitted evidence;
it does not author lifecycle events.

The scenario proves four physical initial worktrees and child starts, a
Cursor/Grok frontend route, typed ask/answer continuation on the same child and
worktree, sibling Git progress while waiting, a prefix conflict with execution
3 retry from the accepted base, dependent task admission, five correlated
implementation commits plus five separately identified tracking commits, one
review, exact reviewed-head push/PR/verification, and cleanup that physically
removes every eligible worktree while retaining only a deliberately dirty
diagnostic worktree.

Replays are checked at prepare/create, question, answer, candidate, conflict
settlement, retry allocation, dependent prepare, publication, and cleanup. The
assertions compare journal, Git refs, worktree inventory, run-read counters,
and mutation counters where the operation owns them.

## Identity and reproducibility

The tested Batuta implementation commit is
`fc32c1f6a3df608519ab28edb86007eda5fc4612`
(`fix: close parallel delivery release gates`) on branch
`worktree-batuta-delivery-hardening`. The extension identity is
`batuta@0.1.0-beta.6`.

The final `go test -v` scenario emitted:
`scenario_id=parallel-demo`,
`delivery_id=sha256:7566a26f95a67697a8e4c08b2176db7e56b0e55689192cfdd671fb9ed530f2c2`,
`question_operation_id=sha256:bf6b8ad8bdd7de2e03b00ce7de3b8fb60af39722a630502a7d1d59413ea2997a`,
`reviewed_head=6d2f45c94a4dfde33ad14b6d01ccbf74a309f55f`, and retained
`wt_parallel_03`. Its disposable retained path was
`/tmp/TestParallelDeliveryHarnessIntegratesRealGitPrefixAndCreatesCumu1652904410/002/batuta-parallel-demo-task-03-a1-4f21048f`
and was removed by test teardown.
The four initial child starts were `run_started_task_01_1`,
`run_started_task_02_1`, `run_started_task_03_1`, and
`run_started_task_04_1`. The Go harness derives the version from the production
descriptor; Python only validates the emitted evidence. All disposable Git
repositories, worktrees, and task-owned directories were removed.

## Stop boundaries

- Dedicated five-independent-task width graph: four active worktrees/children;
  repeated prepare plus the child-start reconciliation preserve the same four
  IDs/counters and leave the eligible fifth task pending.
- 65 real authored canonical artifacts: `NewDeliveryGraph` rejects before
  delivery-side effects.
- Four physical task attempts via three typed answer continuations: the fourth
  attempt rejects a new question with `routing.ErrInvalidDeliveryTransition`
  before a liveness query, preserving the full journal/ref/worktree/run/counter
  snapshot and proving a fifth attempt cannot allocate.
- Four distinct terminal legacy parent attempts using four eligible frontier
  runtimes: fifth `Recover` terminalizes exhausted and replay starts nothing.
- Token ceiling and active-wall expiry are separate delivery-attempt tests.
- Parent `canceled` and public `stalled`/no-progress terminal statuses reconcile
  as blocked and replay without a recovery start. The pinned public surface
  provides no separate no-progress reason metadata; that authority remains the
  Compozy runtime/configuration.
- The five-task production service opens one pause only after the question is
  waiting, three independent siblings have become candidates, and the dependent
  task remains pending. A seven-minute answer interval is excluded exactly,
  candidate/answer replay does not mutate the journal, and a second question
  reopens one new interval.
- A real untracked parent Git file is rejected by `prepare_wave` with unchanged
  delivery boundaries.

## Commands and current results

```text
rtk go test -tags=integration ./...
PASS: 523 tests in 8 packages.

rtk go test ./... -count=1
PASS: 500 tests in 8 packages.

rtk go test -race ./... -count=1
PASS: 500 tests in 8 packages.

PYTHONPYCACHEPREFIX=<owned /tmp cache> python3 -m unittest discover -s tests/e2e -p 'test_*.py' -v
PASS: 31 tests; cache removed.

rtk tests/contract/test_06_parallel_delivery.sh
PASS.

rtk go vet ./...; rtk git diff --check
PASS.
```

The branch compatibility regression in
`TestMigrationFreeDeliveryUsesFreshRunFallbackAndVerifiesPublicationIntegration`
was fixed: a graph becomes parent-settlement authority only after a graph wave
or task transition is durable. A matrix-preallocated, untouched graph retains
the validated legacy parent-child settlement path. This does not weaken Task 7
graph authority after graph execution starts.

## Exact pin

Compozy source `382976d4b43274630a4b67445812fd4a0216dbcc` built as
`v0.3.0-beta.20-49-g382976d4b` (`Commit=382976d4b`), binary SHA-256
`128e34b28829df08341bed31cc02de3d28c3bf700d040c05591293033ecd0072`.
In a fully isolated home/daemon/workspace, `claude/base-model` was
`available_live`, Batuta installed with all 13 live resources, and
`batuta-deliver` passed lint+compile. Runtime Start then failed before run
persistence: the UDS `POST .../loops/batuta-deliver/run` reached the internal
client deadline after about 64 seconds, and the daemon logged
`workspace.resolve.error` with `context_canceled`. There was no
`unknown_action_kind`, no run row, and no `routing_context` dispatch.

Therefore the exact pin is a **build/lint baseline only**. Runtime/release QA is
`blocked-verify` until an actual Compozy beta.21-or-later binary passes the same
public smoke. The strict Batuta guard has no beta.20 exception. Every isolated
daemon, extension, workspace, PID, scratch directory, and temporary contract
root was removed; the shared Batuta and Compozy checkouts/configuration were not
mutated.
