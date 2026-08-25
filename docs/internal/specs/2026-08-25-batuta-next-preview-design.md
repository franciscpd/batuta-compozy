# Batuta Next local preview — design

Status: approved in conversation on 2026-08-25

## Objective

Prepare an honest local preview of the next Batuta version for a presentation
on 2026-08-25 at 19:00. The preview must demonstrate new Batuta-owned routing
behavior on the released Compozy `0.3.0-beta.20` without depending on the large
experimental Compozy prerequisite branch.

## Decision

The preview is Batuta-only. It adds routing rules on the two existing task
metadata axes:

```text
lane = type + complexity
```

The closed preview domains are `backend`, `frontend`, `mobile`, `data`,
`infra`, `security`, `testing`, `docs`, `general`, and `fullstack`.
Complexity remains `low`, `medium`, `high`, or `critical`.

Batuta derives exact provider/model identifiers from the live Compozy catalog,
then applies ordered rules using the selector shapes already accepted by the
released daemon. The preview must never claim a provider is executable merely
because a static model exists: provider presence and live catalog evidence are
shown separately.

## Demo slice

The presentation must have durable evidence for:

1. Batuta installed and healthy on released Compozy `0.3.0-beta.20`.
2. Codex, OpenCode, and Cursor Agent discovered from the local provider
   surfaces, with redacted authentication posture.
3. At least two authored tasks with different canonical `type` and/or
   `complexity` values selecting distinct configured runtime lanes.
4. Real task execution, focused verification, and one local commit per
   executed task.
5. Explicit iteration and wall-clock limits in the demonstrated configuration.
6. A `batuta-deliver` dry-run showing implementation, review, worktree
   inspection, the human publication gate, and the publisher node.
7. A 16:9 roadmap image and a short Portuguese presenter script that clearly
   distinguish shipped preview behavior from the next increments.

## Safety and stop conditions

- No Compozy source change, schema migration, generated contract, or daemon
  rebuild is part of this preview.
- No concurrent agent may write or commit in the same worktree. Parallel task
  delivery is not enabled until Batuta owns a separate worktree per task and a
  deterministic integration order.
- No automatic fallback may silently reset budgets. The later design uses new
  runs with Batuta-owned journal entries, an absolute delivery deadline, and
  remaining-token accounting.
- No real push or pull request is attempted unless existing local GitHub
  authentication and a disposable remote make it safe and reproducible.
- Stop implementation when the demo slice is green. Only then may remaining
  time be used for an additional increment.

## Deferred increments

1. Create one isolated worktree per independent task.
2. Execute conflict-free task nodes concurrently with bounded lane/global
   concurrency.
3. Persist a Batuta-owned routing and recovery journal.
4. Retry only incomplete tasks through fresh runs with ephemeral runtime
   overrides and inherited absolute budgets.
5. Integrate task commits in deterministic dependency order.
6. Run review/remediation on the integrated head.
7. Push the exact verified head and open a pull request autonomously.

These increments require no Batuta-specific database migration in Compozy.
Any small generic Compozy API seam discovered later must be proposed and
reviewed independently; it is not a prerequisite for this preview.

## Verification

- Run the full Batuta contract suite in a detached disposable worktree without
  a `.compozy` marker.
- Validate the installed extension inventory and both Loop definitions.
- Capture dry-run output proving the selected lane matrix and delivery graph.
- Execute the disposable task set and verify exact commits and test commands.
- Confirm the Batuta and Compozy source worktrees remain clean.
- Label every unimplemented roadmap item explicitly in the visual and script.

