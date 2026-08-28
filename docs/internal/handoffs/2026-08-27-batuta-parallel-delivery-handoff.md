# Batuta parallel delivery — final handoff

Date: 2026-08-28

## Outcome

The Batuta implementation is complete and independently approved at the code
boundary. It now owns the full delivery graph: deterministic routing by domain
and complexity, up to four isolated task worktrees, typed task questions and
same-child continuation, canonical prefix integration and conflict
reexecution, one final review, automatic push/PR verification, and safe cleanup
or explicit retained evidence. Merge remains manual.

Release qualification is `blocked-verify`, not complete. The approved source
pin builds and validates the extension, but its binary identifies as beta.20
and timed out in Compozy workspace resolution before a public Loop run was
persisted. Batuta keeps the strict beta.21 runtime floor; no beta.20 exception
was shipped.

## Candidate identities

- Batuta branch: `worktree-batuta-delivery-hardening`
- Batuta implementation commit: `fc32c1f6a3df608519ab28edb86007eda5fc4612`
  (`fix: close parallel delivery release gates`)
- Batuta extension candidate: `batuta@0.1.0-beta.6`
- Site branch: `docs/batuta-parallel-delivery`
- Site commit: `b915be0f81d073cb30102ff3932fc4ccff7423f7`
- Compozy build/lint source: `382976d4b43274630a4b67445812fd4a0216dbcc`
- Built Compozy identity: `v0.3.0-beta.20-49-g382976d4b`, embedded commit `382976d4b`
- Built Compozy SHA-256: `128e34b28829df08341bed31cc02de3d28c3bf700d040c05591293033ecd0072`

No Batuta/site branch was pushed, no tag or release was created, and no PR was
opened or activated by this task. No Compozy source, configuration, shared
daemon, or production installation was changed.

## Packaged surface

The isolated candidate installed and exposed 13 live resources:

- agent: `batuta`
- Loops: `batuta-deliver`, `batuta-task`
- skill: `batuta-routing`
- tools: `delivery_budget_context`, `delivery_graph`, `executor_inventory`,
  `publication_plan`, `publication_verify`, `publish_worktree`, `routing_apply`,
  `routing_context`, and `routing_plan` under the `ext__batuta__` namespace

The package contains Go source/resources and the two Loops; SDD plans/specs are
not staged into the extension.

## Verification evidence

Fresh final Batuta gates:

```text
rtk go test ./... -count=1
PASS: 500 tests / 8 packages

rtk go test -race ./... -count=1
PASS: 500 tests / 8 packages

rtk go test -tags=integration ./... -count=1
PASS: 523 tests / 8 packages

rtk go vet ./...
PASS

PYTHONPYCACHEPREFIX=<owned scratch> rtk python3 -m unittest discover -s tests/e2e -p 'test_*.py'
PASS: 31 tests

rtk tests/contract/test_03_lifecycle_cleanup.sh
rtk tests/contract/test_06_parallel_delivery.sh
rtk tests/contract/test_07_workflow_contract.sh
rtk tests/contract/test_07_public_docs.sh
rtk tests/contract/test_07_preview_docs.sh
rtk git diff --check
PASS
```

The personal-site candidate passed `pnpm check`, `pnpm build`, and
`git diff --check` before commit `b915be0`.

The final deterministic scenario emitted:

- delivery: `sha256:7566a26f95a67697a8e4c08b2176db7e56b0e55689192cfdd671fb9ed530f2c2`
- question operation: `sha256:bf6b8ad8bdd7de2e03b00ce7de3b8fb60af39722a630502a7d1d59413ea2997a`
- reviewed HEAD: `6d2f45c94a4dfde33ad14b6d01ccbf74a309f55f`
- initial children: `run_started_task_01_1` through `run_started_task_04_1`
- retained diagnostic worktree ID: `wt_parallel_03`

The canonical fixture proves Cursor/Grok 4.6 routing for frontend, four writers,
typed ask/resume, sibling progress, conflict prefix integration, fresh retry
from the integrated HEAD, five Conventional implementation commits, one review,
one verified publication sequence, replay stability, and cleanup/retention. A
separate five-independent-task width scenario proves that only four eligible
tasks start and the fifth stays pending without a worktree or child run. All
disposable Git roots and worktrees were removed by teardown.

## Exact-pin runtime result

The final isolated lab used its own `COMPOZY_HOME`, UDS/port, workspace,
provider fixture, extension installation, and standalone detached Compozy
clone. `claude/base-model` was `available_live`; Batuta installed with the exact
inventory above; `batuta-deliver` passed public lint+compile.

The public Start then failed before a run existed:

```text
POST /api/workspaces/<isolated>/loops/batuta-deliver/run
context deadline exceeded (Client.Timeout exceeded while awaiting headers)
```

The daemon recorded `workspace.resolve.error` / `context_canceled`. There was
no `unknown_action_kind`, no `loop_runs` row, and no `routing_context` output.
All lab PIDs, sessions, extension state, workspaces, `/tmp` contract roots, and
owned scratch directories were removed.

## Release gate and next verification

The current CI/release source pin still builds a beta.20 runtime, so the strict
beta.21 guard intentionally prevents release qualification. Once Compozy
publishes an actual beta.21-or-later runtime containing the consumed graph/ask
contracts, update only the Batuta source pin after review and rerun the isolated
`tests/contract/run.sh`. Qualification requires the installed
`batuta-deliver` to persist a run and advance from `load_check=succeeded` to a
terminal `routing_context` result, followed by complete teardown.

Live external provider and forge evidence remains `blocked-verify`; the
deterministic provider/review/forge seams are not presented as production
external calls. Publication must remain stopped until this handoff is reviewed
and the beta.21+ runtime smoke passes.

## Presentation assets

- Current architecture/roadmap image:
  [batuta-next-roadmap.png](../../images/batuta-next-roadmap.png)
- UI-first demonstration runbook:
  [2026-08-25-batuta-next-demo-runbook.md](2026-08-25-batuta-next-demo-runbook.md)
- Detailed deterministic QA:
  [2026-08-27-batuta-parallel-delivery-local.md](../qa/2026-08-27-batuta-parallel-delivery-local.md)
