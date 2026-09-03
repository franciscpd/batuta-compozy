# Smoke E2E — parallel Batuta delivery

This UI-first smoke route demonstrates a small disposable project. It is an
operator walkthrough, not a claim that an external provider or forge is being
exercised by a deterministic fixture.

## Preconditions

- Use an isolated, compatible CompozyOS daemon and project workspace.
- Enable `spec-cycle` and `batuta`; diagnose setup with `compozy extension
  validate`, `status`, and `inventory` only.
- Supply an authenticated provider/model catalog and a small Git repository with
  an `origin` only when demonstrating real publication.
- The tested development baseline is SDK `v0.3.0-beta.21` with CompozyOS
  built from source commit `34208e9990622ee62e9a5cf114386273ae6abfa0`
  (`v0.3.0-beta.22`).

## Route through the Compozy UI

1. Create a small workspace/project and open a `batuta` session. Ask Batuta to
   author an SDD for five tasks: four independent changes and one dependent
   change. Do not create task files manually.
2. Select a single interactive SDD clarification card when product intent is
   materially ambiguous. Confirm it is distinct from a task-level `ask`.
3. Approve the SDD and task graph. In the timeline, inspect
   `executor_inventory`, `routing_plan`, and `routing_apply`; verify each task
   has one canonical domain × complexity lane and the immutable `delivery_id`.
4. Start delivery through `start_delivery`. The graph has max-four dependency-safe parallelism and must create at most four isolated task worktrees
   for independent eligible tasks; the fifth remains pending. View the routing
   graph and child-run IDs in the UI rather than asking an operator to choose
   executors.
5. While one sibling progresses, park one `batuta-task` with a material typed
   question. Answer it in the UI. Confirm the same child run and same task
   worktree resume with the stored answer; do not turn it into an SDD card or a
   new task run.
6. Inspect each completed task's one Conventional Commit and its task worktree.
   The graph integrates verified commits into the canonical integration worktree.
   If a deterministic prefix conflict is shown, inspect the newly allocated
   execution/base/worktree for canonical conflict reexecution.
7. After the graph is integrated, inspect the one final review (`review-and-fix`) child, the
   exact reviewed HEAD, the publication plan, one push/PR operation, and
   independent publication verification. Merge remains manual.
8. Inspect cleanup. Eligible task worktrees disappear; a named diagnostic
   worktree may be retained only with terminal blocked evidence.

## Required evidence

- The SDD and `_tasks.md`/`task_NN.md` files came from `cy-create-spec` and
  `cy-create-tasks`; Batuta did not write feature code.
- Interactive SDD cards and in-delivery `ask` have separate identities and
  audiences.
- Four distinct eligible child starts occur and no fifth concurrent start does.
- Task and integration worktrees are distinct; no worktree has concurrent
  writers.
- Every implementation has one commit per task (a Conventional Commit); integration ancestry has
  one task commit per task plus a separately identified review commit if present.
- Replays of prepare/start, question/answer, candidate, settlement/retry,
  publication/verification, and cleanup create no duplicate journal entries,
  child runs, worktrees, commits, pushes, or PRs.
- Stops for capacity, physical attempt cap, fresh parent cap, token ceiling,
  active wall clock, cancellation/stall/no-progress, human pause, exhausted
  fallback, or ambiguity preserve journal/Git/worktree/run evidence and do not
  start another generation or review.

## Negative checks

- A fifth independent task is not started until capacity frees.
- A task after four physical executions is rejected before a liveness or
  worktree mutation; replays of historical answers/candidates stay truthful.
- A fourth terminal fresh-parent attempt prevents a fifth Start/Recover.
- A retained cleanup cannot be reported as `cleaned` and cannot repeat review,
  publication, or worktree removal.
- A missing forge provider, remote, or credential is a durable blocker; a
  compare URL is never evidence of a published PR.
