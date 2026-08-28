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

The tested Batuta base was `2b26c29b9e58aa506a3c8a3f099846a326a31ad4`.
The candidate worktree identity was
`sha256:d75f7c277fc148ccfa3cfaa65bfd5d3b858eddf8f333d52e9b1b24cac92f2d5a`,
computed from the binary implementation diff plus sorted content hashes of
untracked implementation files, excluding this mutable QA evidence document;
it deliberately is not a self-referential future commit SHA. The extension
identity is `batuta@0.1.0-beta.5`.

The documented `go test -json` verification run emitted this concrete canonical
scenario evidence: `scenario_id=parallel-demo`,
`delivery_id=sha256:e81a2047955bdc63ddd842f6c861a22e0ca700dbf9cf45a7a9f9ff3f4419d3c7`,
`question_operation_id=sha256:1615c612ab0639b1e137799a7a330e6195b9b8033ba6dd266f2f55c1c5cd6656`,
`reviewed_head=f1c62ad18d8521a2c00c96c0fad4897e9ea9c516`, and retained
`wt_parallel_03` at
`/tmp/TestParallelDeliveryHarnessIntegratesRealGitPrefixAndCreatesCumu2569618141/002/batuta-parallel-demo-task-03-a1-ab837f0f`.
The four initial child starts were `run_started_task_01_1`,
`run_started_task_02_1`, `run_started_task_03_1`, and
`run_started_task_04_1`. The Go harness derives the extension version from the
production extension descriptor (`batuta@0.1.0-beta.5`), and Python checks its
format rather than echoing a second hard-coded version. The disposable
repository/worktrees and all task-owned test directories are removed by
`testing.T` teardown.

The implementation identity uses digest framing v2: header
`batuta-task8-implementation-digest-v2\0`; then `D\0`, an unsigned 64-bit
big-endian byte length, and the exact `git diff --binary --no-ext-diff HEAD`
payload; then one `F\0`, 64-bit path length, UTF-8 path bytes, 64-bit content
length, and raw content bytes for each untracked implementation file in byte
sorted path order. The QA document itself is excluded so recording the digest
does not change it. Reproduce it exactly with:

```text
rtk zsh -c 'python3 - <<'\''PY'\''
import hashlib, pathlib, subprocess
exclude = "docs/internal/qa/2026-08-27-batuta-parallel-delivery-local.md"
diff = subprocess.check_output(["git", "diff", "--binary", "--no-ext-diff", "HEAD", "--", ".", f":(exclude){exclude}"])
files = sorted(path for path in subprocess.check_output(["git", "ls-files", "--others", "--exclude-standard"], text=True).splitlines() if path != exclude)
h = hashlib.sha256(); h.update(b"batuta-task8-implementation-digest-v2\0")
h.update(b"D\0"); h.update(len(diff).to_bytes(8, "big")); h.update(diff)
for path in files:
    name, body = path.encode(), pathlib.Path(path).read_bytes()
    h.update(b"F\0"); h.update(len(name).to_bytes(8, "big")); h.update(name)
    h.update(len(body).to_bytes(8, "big")); h.update(body)
print("sha256:" + h.hexdigest())
PY'
```

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
- A one-task production graph opens an exact seven-minute human pause; answer
  continuation excludes that interval from active wall time and replays with no
  journal or run-read mutation. The five-task DAG cannot globally pause while
  its dependent task is pending.
- A real untracked parent Git file is rejected by `prepare_wave` with unchanged
  delivery boundaries.

## Commands and current results

```text
rtk go test -tags=integration ./internal/extensionapp -run '<Task8 focused set>' -count=1 -v
PASS: 49 focused integration tests.

rtk go test -race -tags=integration ./internal/extensionapp -run '<Task8 focused set>' -count=1 -v
PASS: 49 focused integration tests, including compatibility boundaries.

rtk go test -tags=integration ./...
PASS: 521 tests in 8 packages; 2 intentional skips.

rtk go test ./...
PASS: 499 tests in 8 packages.

PYTHONPYCACHEPREFIX=<owned /tmp cache> python3 -m unittest discover -s tests/e2e -p 'test_*.py' -v
PASS: 31 tests; cache removed.

rtk tests/contract/test_06_parallel_delivery.sh
PASS.

rtk go vet ./internal/extensionapp; rtk git diff --check
PASS.
```

The branch compatibility regression in
`TestMigrationFreeDeliveryUsesFreshRunFallbackAndVerifiesPublicationIntegration`
was fixed: a graph becomes parent-settlement authority only after a graph wave
or task transition is durable. A matrix-preallocated, untouched graph retains
the validated legacy parent-child settlement path. This does not weaken Task 7
graph authority after graph execution starts.

## Exact pin

Compozy `382976d4b43274630a4b67445812fd4a0216dbcc` was archived and built in
an owned scratch directory. Binary SHA-256:
`a3923ecc240c4abb73f8c1756a05418e91395ae226bfe44d4aa46ff59a5f4f7c`.
An isolated `COMPOZY_HOME`, workspace, port 39128, dev-linked staged Batuta
extension, and bounded installed `loop run --name batuta-deliver` were used.
The direct run stopped in four seconds with
`loop definition has 13 lint error(s): unknown_action_kind`; this is not a
successful public-loop claim. All task-owned processes and scratch were torn
down. No Compozy source/configuration, shared daemon, live provider, or live
forge was changed.
