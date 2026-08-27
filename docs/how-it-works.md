# How Batuta works

Batuta is the autonomous engineering conductor for one trusted CompozyOS
workspace. Its complete operating contract lives in
[`agents/batuta/AGENT.md`](../agents/batuta/AGENT.md); the routing contract lives
in
[`resources/skills/batuta-routing/SKILL.md`](../resources/skills/batuta-routing/SKILL.md).

## 1. Product intent and SDD

The Batuta session has full workspace scope so it can inspect the repository,
clarify product intent, and write the complete SDD. It runs `cy-create-spec`
and `cy-create-tasks`, producing the approved spec, user stories, developer
experience, tests, optional UI/UX document, task index, and individual task
files under `.compozy/tasks/<slug>/`.

Every task carries a canonical domain and complexity. Batuta may author and
revise these planning artifacts, but it never implements feature code. Product
changes remain the responsibility of the implementation and review Loops.

## 2. Automatic inventory and routing

For every approved slug, Batuta calls
`ext__batuta__executor_inventory`. The result is a redacted snapshot of the
executors configured in Compozy, Codex, Cursor, OpenCode, and supported local
agents. Credentials and raw provider configuration never enter the prompt or
the routing matrix.

Batuta classifies every task using the closed `domain × complexity` vocabulary
and submits complete proposals to `ext__batuta__routing_plan`. A plan must cover
every task exactly once and use only capabilities present in the inventory and
live Compozy catalog. For example, `backend/low` and `frontend/medium` can be
routed to different executors and models.

Batuta then calls `ext__batuta__routing_apply` with `apply_matrix`. The
extension recollects the inventory, validates the immutable generation digest,
pins the trusted worktree and canonical task snapshot, and archives one stable
`delivery_id`. It makes no stored Loop-config call, so operator-authored rules
remain untouched. The archived generation pins the exact fallback chain used
by every attempt.

## 3. Worktree and dispatch

Batuta creates or reuses a clean managed worktree on branch `batuta/<slug>`.
It checks that `.compozy/tasks` is copied into the worktree, then calls the
read-only `ext__spec_cycle__import_tasks` preflight and requires at least one
task.

The session calls `routing_apply` with `start_delivery`. Batuta derives the
request from its journal and submits `batuta-deliver` with the slug,
`origin_session_id`, ready worktree reference, routing generation, stable
delivery ID, attempt number, token ceiling, and absolute deadline. Typed
ephemeral `config_overrides` carry exact runtime rules and remaining budgets to
the child Loops. The absolute deadline is the delivery's original wall-clock budget,
not a fresh allowance per attempt. A successful submission is a turn boundary: the daemon owns
that attempt and returns to the original session on a terminal effect.

Each `batuta-deliver` attempt runs writers sequentially in the same worktree:

1. `implement-tasks` executes the approved items with automatic commits — one commit per task.
2. `review-and-fix` reviews the complete delivery phase only after the current
   implementation set succeeds and commits valid remediations.
3. The publication planner freezes the reviewed HEAD and determines whether
   publication is possible.
4. `ext__batuta__publish_worktree` pushes that exact HEAD and opens or reuses
   the pull request.
5. `publication_verify` independently confirms the remote HEAD and PR identity.

There is no publication agent, routine approval, or manual healthy-path action.
One stable delivery may contain multiple fresh parent run IDs, but publication
occurs once for the completed phase, giving one PR per delivery phase. The
merge remains manual.

## 4. Return and bounded recovery

Every terminal delivery effect queues one idempotent prompt to the
`origin_session_id`. Batuta first reads the exact parent through
`compozy__loop_status`, then asks `ext__batuta__routing_apply` to reconcile the
pinned fallback chain.

When one failed item has an eligible next runtime, Batuta first settles the
exact parent and child evidence in its journal. It then starts a fresh parent
run with a new `run_id` in the same worktree. Completed task files and commits
carry forward; only incomplete tasks are submitted, and only observed failed
tasks advance to the next exact fallback. The stable `delivery_id`, task
snapshot, worktree identity, routing generation, token ceiling, and absolute
deadline do not change.

`start_delivery` and `recover_delivery` are replay-safe. A lost response adopts
exactly one matching recent run; two matches are ambiguous and block without a
third submission. Attempt cap 4, deadline, token ceiling, exhausted fallback,
task/worktree/routing drift, a foreign run, review failure,
publication-started failure, cancellation, stalled execution, and ambiguous
remote effects all stop before another mutation.

When reconciliation is complete, Batuta reports exact child outcomes, commits,
reviewed HEAD, publication operation IDs, and the verified PR URL. Push and PR
opening are automatic; merge remains manual.

## Safety boundaries

- Batuta writes SDD artifacts but never feature code or product tests.
- Never run concurrent writers in one worktree.
- Routing generations and delivery attempts go only through Batuta's guarded
  journal tools; stored Compozy Loop configuration is never mutated.
- Publication acts only on the reviewed HEAD and is verified independently.
- External blockers return to the operator with durable evidence; healthy-path
  routing, commits, fallback, push, and PR creation do not require approval.

## Future increments

Interactive clarification may later park a session on a durable user question
when product intent is materially ambiguous. Parallel task worktrees,
deterministic commit integration, and graph engineering with lane-level
concurrency are also deferred. None of these future increments introduces a
routine delivery or publication gate.
