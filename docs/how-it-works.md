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

`ext__batuta__executor_inventory` creates a redacted snapshot of Compozy's live
catalog plus bounded optional evidence from Codex, Cursor, OpenCode, Claude
Code, and Agy. Compozy is the only provider/model execution authority. Batuta
uses `ext__batuta__routing_plan` and `ext__batuta__routing_apply` to cover every
task exactly once by domain × complexity. New fit requests use
`executor_id: compozy` and an exact live provider/model pair. Enricher absence
never removes that pair; resolved evidence can improve fit or prove a hard
capability. For example, `backend/low` and `frontend/medium` may select different
live pairs while retaining the same Compozy execution owner. Agy model listing
is network-backed and is not probed automatically.

Provider/model order and optional reasoning overrides belong to the current
delivery's fit request. Batuta does not ship a domain preference for a CLI,
provider, or model family. A live model missing from the known quality hints
remains eligible as unclassified and is shown that way for operator review.

Before mutation, Batuta renders the exact derived matrix: tasks, selected live
provider/model/reasoning/tier, ordered fallbacks, and a cost column. Because the
generation has no authoritative monetary cost snapshot, cost is displayed as
`unknown` and is not part of the durable task/selected/fallback projection. The
operator approves that proposal or requests an adjustment. Confirmation is
durable for the identical projection and is invalidated by any changed cell.
This is routing transparency, not a later implementation or publication gate.

`alignment_status` revalidates the semantic catalog once and archives the exact
candidate generation. Volatile refresh timestamps do not change its identity.
`confirm_alignment` confirms that archive without planning again; a genuinely
changed model, availability, task, or fit is rejected by the final apply
preflight and requires a new generation to be shown to the operator.
The journal keeps a deterministic bounded set of up to eight unconfirmed
candidates, so interleaved operator sessions remain independently confirmable
without allowing abandoned proposals to grow storage indefinitely.

The confirmed preflight also makes a new project deliverable without a manual
`git init`. The guarded `bootstrap_repository` operation uses the trusted
workspace root, respects `.gitignore`, blocks unignored sensitive paths, and
creates branch `main` with one `chore: initialize workspace` commit. Existing
repositories with a valid HEAD are left untouched; an existing HEAD-less
repository must already use `main`.

`apply_matrix` promotes the archived immutable generation with the canonical
task snapshot, integration worktree, and stable `delivery_id`. Stored Compozy Loop configuration is never mutated.

## 3. Dependency-safe task waves

`start_delivery` starts `batuta-deliver` with the stable delivery identities,
original token ceiling, absolute deadline, and effective per-run configuration
overrides, then returns its durable public launcher run ID; the journal stores
the launcher ID. That launcher creates exactly one internal
`batuta-deliver-core` child, which owns the existing dependency-safe graph.
The guarded tooling validates that core child internally; neither an operator
nor the Batuta agent supplies or reconciles its ID. The budget literals authored
in all three Loop definitions document intent; Compozy does not derive effective
enforcement from those literals.
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
human gate; merge remains manual. A repository with no remote is not an
error: the plan reports `local_only`, push and PR are skipped, the commits
stay on the delivery branch, and the terminal report quotes the manual merge
command. Batuta never merges.

The launcher terminal effect queues a message for `origin_session_id`. Batuta
reads the exact launcher run with `compozy__loop_status`, calls
`reconcile_fallbacks`, and starts at most one eligible fresh launcher attempt
through `recover_delivery`. A fresh launcher does not duplicate the previous
core graph usage or review.

## 5. Replay and stop conditions

Every graph mutation has a durable operation identity. Replaying prepare,
child start, question, answer, candidate, integration, retry, review,
publication, verification, or cleanup returns the truthful current result
without duplicate child runs, commits, pushes, PRs, or worktree mutations.

Capacity (four worktrees), four fresh launcher runs, four physical task
executions, token ceiling, active wall-clock deadline, terminal
canceled/stalled state, no-progress, an open human pause, exhausted fallback,
and ambiguous evidence all stop before a new mutation. A cleaned worktree is
the only successful cleanup disposition. A retained diagnostic worktree records
terminal blocked state and stable evidence; it cannot launch another generation,
review, or publication.

## Compatibility

Batuta depends directly on the official Compozy Go SDK `v0.3.0-beta.21`. CI
builds CompozyOS from source commit `34208e9990622ee62e9a5cf114386273ae6abfa0`,
the `v0.3.0-beta.22` release, and runs the contract suite against that daemon.
The minimum runtime is a released beta.21-or-later binary; the version guard
rejects older builds. Check the installed daemon's public version and
extension validation before production use.
