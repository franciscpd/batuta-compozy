# Batuta delivery worktree and gated publication — design

Design approved in conversation on 2026-08-21. This change closes the two
largest gaps in the delivery flow: work landing as loose commits on the
operator's current branch, and the flow ending at local commits with no path
to a remote. Both close with CompozyOS primitives the daemon already owns —
managed worktrees, Loop environments, human gates, and worktree exit actions.

## Goal

Every delivery runs isolated in a dedicated managed worktree on its own
branch, and a clean review can continue — behind an explicit human gate —
into `push` plus a pull request, executed by the daemon-side worktree exit
actions, never by the conductor.

The conductor's prohibitions do not move: Batuta still never writes code,
commits, pushes, or approves its own runs.

## Worktree isolation

At dispatch, before the dry-run, Batuta creates one managed worktree per
delivery with the native mutating tool `compozy__worktree_create`:

- name `batuta-<slug>`, branch `batuta/<slug>`, base = the repository's
  default branch;
- creation is orchestration, not implementation — it stays inside the
  conductor contract;
- creation is asynchronous: Batuta continues only after a structured read
  shows the record `ready`. A `setup_state=failed` ready record is surfaced
  to the operator before dispatch.

`batuta-deliver` gains a required input `worktree_ref` and declares a Loop
default environment `{mode: worktree, worktree_ref: {{ .inputs.worktree_ref }}}`.
`run-loop` forwards the parent environment, so the bundled `implement-tasks`
and `review-and-fix` children execute inside the worktree without any change
to the spec-cycle extension.

Branch lifecycle: the branch is always preserved by the system.

- `done` + published → the PR is the integration path; cleanup follows the
  exit plan's cleanup evidence and remains the operator's action.
- `failed` / `blocked` / any other terminal → the worktree and branch stay
  for inspection; a redispatch for the same slug reuses the existing ready
  worktree by ref instead of creating a second one (`worktree_name_taken` on
  a blind create is treated as "reuse", confirmed by inspect).

## Publication gate and publish node

The `batuta-deliver` graph gains two nodes after `review`:

1. `publish_gate` — control `gate` with a `human` criterion. Reaching it
   parks the run `needs-approval`. Batuta surfaces run ID and gate ID to the
   operator through the existing escalation contract and waits; the daemon
   denies self-approval (`approval_self_denied`). `reject` ends the run
   `blocked` with all commits preserved on `batuta/<slug>`.
2. `publish` — a `goal` node bound to a new bundled executor agent,
   `batuta-publisher`, running in the delivery worktree environment. Its
   fixed objective: run `compozy worktree push <ref>`, then
   `compozy worktree pr <ref> --base <default branch>` with a title and body
   derived from the slug's spec artifacts, and report the resulting PR URL.
   Exit actions are CLI/HTTP/UDS surfaces, not native tools, which is why a
   goal-bound executor performs them rather than a direct action node.

Forge dependency is honest: PR creation requires a serving credentialed
`forge.provider` extension. Without one, `batuta-publisher` still pushes,
reports the browser compare URL from the exit plan, and the run completes
`done` with publication evidence stating "pushed, PR manual".

With `auto_commit=false` there is nothing committed to publish: a `branch`
control node routes the graph from `review` directly to done, skipping
`publish_gate` and `publish`.

## Conductor contract changes

`agents/batuta/AGENT.md`:

- Dispatch section: create-or-reuse the delivery worktree, verify `ready`
  with a structured read, pass `worktree_ref` to the dry-run and real run.
- Escalation section: `publish_gate` joins `needs-approval` handling — the
  operator decides publication; Batuta only surfaces IDs and waits.
- "What you never do" is unchanged. The publisher executor — not the
  conductor — performs push and PR, and only after a human approval the
  conductor cannot grant itself.

## Extension inventory

The extension grows from three resources to four: the `batuta` agent, the
`batuta-publisher` agent, the `batuta-routing` skill, and the
`batuta-deliver` Loop. Inventory contracts and docs update accordingly.

## Verification

- Loop validation: `compozy loop validate` passes on the extended
  definition (new input, environment default, branch node, gate, goal node).
- Static contract checks: AGENT.md names the worktree step, the reuse rule,
  and the publish-gate escalation; the prohibition list is byte-identical.
- E2E (public events, no private stores): a delivery with `auto_commit=true`
  shows implement/review executing in the worktree environment, a
  `needs-approval` park on `publish_gate`, an operator approval, a push, and
  PR-or-compare-URL evidence; a `reject` path ends `blocked` with the branch
  still present; an `auto_commit=false` delivery never reaches the gate.
- Redispatch smoke: a second dispatch for the same slug reuses the existing
  worktree and creates no duplicate.

## Non-goals

- No change to CompozyOS or the spec-cycle extension.
- No auto-merge, no branch deletion, no rebase/merge repair — behind or
  diverged branches remain explicit operator repairs.
- No forge-provider bundling or credential handling; provider setup stays an
  operator prerequisite like model authentication.
- No per-task worktrees (`per_run`); one worktree per delivery is the unit.
