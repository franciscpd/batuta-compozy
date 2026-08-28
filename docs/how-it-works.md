# How Batuta works

Batuta conducts one trusted CompozyOS workspace. Its executable operating
contract is [`agents/batuta/AGENT.md`](../agents/batuta/AGENT.md), and routing
rules live in [`resources/skills/batuta-routing/SKILL.md`](../resources/skills/batuta-routing/SKILL.md).

## 1. Product intent and interactive SDD

The Batuta session uses its full workspace scope to research, run
`cy-create-spec`, and write the complete SDD. It uses interactive SDD
clarification cards only when material product intent is ambiguous. After
approval, `cy-create-tasks` produces canonical `_tasks.md` and `task_NN.md`
files under `.compozy/tasks/<slug>/`; every task has a closed domain and
complexity.

Batuta owns SDD artifacts, not feature code. `batuta-task` owns one task
implementation attempt in an isolated task worktree. If it needs a material
product choice or unavailable external value, its typed `ask` parks that child;
the durable answer resumes the exact same child, execution, and worktree. It
does not use an SDD card as an in-delivery answer channel.

## 2. Automatic inventory and immutable routing

`ext__batuta__executor_inventory` creates a redacted snapshot of Compozy,
Codex, Cursor, OpenCode, and supported local-agent capabilities. Batuta then
uses `ext__batuta__routing_plan` and `ext__batuta__routing_apply` to cover every
task exactly once by domain × complexity. Exact live bindings are required; for
example, `backend/low` and `frontend/medium` can select different runtimes, and
Cursor/Grok 4.6 is eligible only when that exact ACP binding is in inventory.

`apply_matrix` archives an immutable generation, canonical task snapshot,
integration worktree, and stable `delivery_id`. Stored Compozy Loop configuration is never mutated.

## 3. Dependency-safe task waves

`start_delivery` starts `batuta-deliver` with the stable delivery identities,
original token ceiling, absolute deadline, and effective per-run configuration
overrides. The budget literals authored in the two Loop definitions document
intent; Compozy does not derive effective enforcement from those literals.
Direct manual, CLI, HTTP, UDS, native-tool, or scheduled starts outside the
guarded Batuta operation are unsupported and may be unbounded.
`ext__batuta__delivery_graph` prepares only eligible nodes in the task graph. It has max-four dependency-safe parallelism:
at most four independent task worktrees may be active, and no two
writers share a worktree.

Each `batuta-task` is bounded to four physical executions. Its run-agent returns
either a completed, bounded inline implementation payload (one commit and
verification) or `needs_input`. `record_candidate` derives the completed child
payload from the exact child run and checks task, execution, commit, and
verification evidence before integration.

The graph settles a wave through deterministic canonical integration into the
integration worktree. An ordinary success unblocks dependent tasks. A prefix
conflict allocates canonical conflict reexecution: a fresh immutable execution,
new base SHA, and a new task worktree. It never reuses an old execution as if it
were current.

## 4. Final review, publication, and terminal return

After all graph tasks are integrated, one final review runs through `review-and-fix` in the
integration worktree. `publication_plan` freezes the reviewed HEAD;
`ext__batuta__publish_worktree` pushes exactly that HEAD and opens or reuses one
PR; `publication_verify` checks the remote result. Healthy publication has no
human gate; merge remains manual.

The terminal effect queues a message for `origin_session_id`. Batuta reads the
exact parent with `compozy__loop_status`, calls `reconcile_fallbacks`, and starts
at most one eligible fresh parent attempt through `recover_delivery`. A fresh
parent does not duplicate the previous parent usage or review.

## 5. Replay and stop conditions

Every graph mutation has a durable operation identity. Replaying prepare,
child start, question, answer, candidate, integration, retry, review,
publication, verification, or cleanup returns the truthful current result
without duplicate child runs, commits, pushes, PRs, or worktree mutations.

Capacity (four worktrees), four physical task executions, four fresh parents,
token ceiling, active wall-clock deadline, terminal canceled/stalled state,
no-progress, an open human pause, exhausted fallback, and ambiguous evidence
all stop before a new mutation. A cleaned worktree is the only successful
cleanup disposition. A retained diagnostic worktree records terminal blocked
state and stable evidence; it cannot launch another generation, review, or
publication.

## Compatibility

Batuta depends directly on the official Compozy Go SDK `v0.3.0-beta.21` and
uses source commit `382976d4b43274630a4b67445812fd4a0216dbcc` only as its tested
build/lint baseline. That commit's binary reports beta.20 and failed runtime
Start qualification, so the minimum runtime remains a released beta.21-or-
later binary. Check the installed daemon's public version and extension
validation before production use.
