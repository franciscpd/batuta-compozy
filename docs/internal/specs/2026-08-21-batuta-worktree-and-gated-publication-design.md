# Batuta delivery worktree and gated publication — design

Design approved in conversation on 2026-08-21; revised the same day after
external review. This change closes the two largest gaps in the delivery
flow: work landing as loose commits on the operator's current branch, and
the flow ending at local commits with no path to a remote. Both close with
CompozyOS primitives the daemon already owns — managed worktrees, Loop
environments, human gates, node effects, and worktree exit actions.

## Goal

Every delivery runs isolated in a dedicated managed worktree on its own
branch, and a clean review can continue — behind an explicit human gate —
into `push` plus a pull request, executed through the daemon's worktree exit
actions, never by the conductor.

The conductor's prohibitions do not move: Batuta still never writes code,
commits, pushes, or approves its own runs.

## Worktree isolation

At dispatch, after the preference gate and before the dry-run, Batuta
creates one managed worktree per delivery with the native mutating tool
`compozy__worktree_create`:

- name `batuta-<slug>`, branch `batuta/<slug>`, base = the repository's
  default branch;
- creation is orchestration, not implementation — it stays inside the
  conductor contract;
- creation is asynchronous. Batuta waits bounded (structured reads against
  the record, no shell sleeps; the worktree stream is the observable) and
  continues only on `ready` with `setup_state` healthy. Every other outcome
  stops the dispatch before dry-run with the exact structured evidence:
  a rejected create call, an unreadable inspect, a record still `pending`
  past the setup timeout, a `failed` record, or `setup_state=failed`. The
  typed worktree error vocabulary (`worktree_name_taken`,
  `worktree_path_exists`, `branch_held_by_worktree`, `base_ref_not_found`,
  …) is reported literally; Batuta never retries destructively and never
  removes a worktree to make room.

`batuta-deliver` gains a required input `worktree_ref` and declares a Loop
default environment `{mode: worktree, worktree_ref: {{ .inputs.worktree_ref }}}`.
`run-loop` forwards the parent environment, so the bundled `implement-tasks`
and `review-and-fix` children execute inside the worktree without any change
to the spec-cycle extension.

### Task artifact transport

The conductor's `ext__spec_cycle__import_tasks` preflight runs in the main
workspace, not the delivery worktree; a fresh managed worktree does not
inherit `.compozy/tasks/<slug>/task_*.md` on its own, so a preflight
`count > 0` does not prove the Loop's `load_check` node — which runs inside
the worktree — will find anything. The daemon already owns the transport
primitive: the workspace's `[worktrees]` bootstrap-copy configuration
(`worktrees.copy_list`) carries ignored/untracked paths into every new
managed worktree. Before creating the delivery worktree, Batuta reads
`worktrees.copy_list` with `compozy__config_get` and continues only when it
covers `.compozy/tasks`; otherwise it stops and reports the structured
evidence plus the one-time operator remedy (`compozy config set
worktrees.copy_list '[".compozy/tasks"]' --scope workspace`) instead of
dispatching into an artifact-less worktree. A live probe against a
throwaway workspace confirmed the fix: with `worktrees.copy_list` set to
`[".compozy/tasks"]` at workspace scope, a freshly created managed worktree
contained the seeded `.compozy/tasks/<slug>/task_*.md` file at its root.

### Reuse on redispatch

A redispatch for the same slug reuses the existing worktree only when a
structured inspect confirms ALL of: same repository, name `batuta-<slug>`,
branch `batuta/<slug>`, state `ready`, and no active bound session or
running exit operation. Any mismatch — different branch or base, dirty or
diverged state the operator has not seen, an open or merged PR already
recorded for the branch — is presented to the operator, who chooses reuse,
a fresh name, or repair. `worktree_name_taken` on a blind create is never
silently treated as reuse; it triggers exactly this inspect-and-confirm
path.

### Branch lifecycle

The branch is always preserved by the system.

- `done` + published → the PR is the integration path; cleanup follows the
  exit plan's cleanup evidence and remains the operator's action.
- `failed` / `blocked` / `exhausted` / any other terminal → the worktree
  and branch stay for inspection; redispatch follows the reuse rules above.
- A dry-run rejection or failed real submission has no Loop terminal. The
  worktree is preserved by default; Batuta reports the worktree ref
  together with the exact structured dispatch failure so the operator never
  faces an unexplained managed worktree, and the next attempt reuses it via
  the same inspect-and-confirm path.

## Publication graph

The `batuta-deliver` graph changes after `review`. Complete post-review
topology:

- `publish_check` — control `branch`. Predicate: publication proceeds only
  when the delivery has publishable evidence — commits exist on
  `batuta/<slug>` ahead of the base branch (branch-vs-base evidence, not
  the `auto_commit` boolean alone, so commits inherited from a reused
  worktree are never silently dropped). No publishable commits → route
  directly to the run's success sink; the run completes `done` with
  publication evidence "nothing to publish".
- `publish_gate` — control `gate` with a `human` criterion, entered only on
  the publishable route. Reaching it parks the run `needs-approval`. The
  daemon denies self-approval (`approval_self_denied`); only the operator
  decides. `reject` ends the run `blocked` with all commits preserved on
  `batuta/<slug>`.
- `publish` — a `goal` node bound to the new bundled executor agent
  `batuta-publisher`, running in the delivery worktree environment,
  entered only on gate approval.

Edges: `review → publish_check`; `publish_check → publish_gate`
(publishable) or `→ done` (nothing to publish); `publish_gate → publish`;
`publish → done`.

**Known limitation — `ahead_of_base` staleness.** `publish_check`'s
predicate reads `status.ahead_of_base` from the `compozy__worktree_inspect`
node output, which is populated from the worktree record and can go stale
without an explicit refresh; this daemon version exposes no refresh
parameter on that node surface. Consequently a stale zero routes an
otherwise-publishable delivery to "nothing to publish" and the run
completes `done` without ever reaching the gate. No code-level mitigation
exists for this yet; the live smoke must observe a non-zero
`ahead_of_base` after the implement child commits and before the gate is
reached, to confirm the value is fresh in practice for the exercised path.

### Surfacing the gate to the operator

`needs-approval` is a live pause, not a terminal, so the terminal effect
cannot announce it. The gate is surfaced without a watcher:

- Primary: `publish_gate` declares a node effect on its pause trigger that
  queues one idempotent `compozy__session_prompt` to
  `{{ .inputs.origin_session_id }}` (identity derived from run ID and gate
  ID, mirroring the terminal effect). The prompt tells Batuta to read
  `compozy__loop_status`, report the parked gate with run and gate IDs, and
  wait for the operator — never approve.
- Fallback (verified at implementation time): if the effect grammar does
  not fire node effects for approval waits, the dispatch acknowledgement
  already carries `web_url` and states that a clean review will park the
  run `needs-approval` for the operator's decision; an explicit progress
  read shows the parked gate. No polling in either mode.

Gate residence does not burn the delivery budget: the daemon suspends node
clocks and the run wall-clock work budget during approval waits.

### Approval is bound to a clean worktree

The implemented property is narrower than "approval is bound to the exact
state the reviewer saw": `batuta-publisher` verifies a clean working tree
(`git status --porcelain` empty) in the worktree and records the `HEAD` SHA
as publication evidence. It does not compare that `HEAD` against
review-round evidence — the review child (`review-and-fix`, owned by the
`spec-cycle` extension) does not currently expose a recorded HEAD/commit
SHA in its output shape, and no plumbing exists in this design to carry one
forward to the publish node. A dirty working tree is still a hard failure:
the publisher aborts without pushing and reports it — the run ends `failed`
on that node with the branch untouched.

This is a deliberate, bounded gap, not an oversight: the worktree is
daemon-managed, and between the review round completing and the operator
deciding on the gate, no bound session mutates it in normal operation. Any
drift would have to be operator-initiated (a manual commit or edit in the
managed worktree between review and approval), and that drift is visible in
the branch history the operator is approving — the gate's human review of
the branch state, not an automated SHA comparison, is what catches it today.
Future upgrade: once `spec-cycle`'s review child exposes a recorded
review-HEAD in its evidence, thread it through to the publish node so the
publisher can compare `HEAD` against it and fail closed on drift, instead of
relying solely on the operator's own inspection.

### The publisher and its capability surface

`batuta-publisher` is a bundled executor agent (a resource, like any
spec-cycle executor). Its agent definition sets `permissions: approve-reads`
— the daemon's coarse session-approval enum
(`deny-all`/`approve-reads`/`approve-all`) is the only permission
construct the agent-frontmatter schema exposes; there is no
frontmatter-level mechanism to allowlist individual shell command
patterns for an agent's underlying coding-tool session in this daemon
version. `deny-all` was tried first and rejected by a live probe: with
`deny-all`, the provider auto-rejects (`reject-once`) every shell/CLI step
the publisher issues before it runs — including the read-only
`git rev-parse HEAD` and `compozy worktree exit` — which blocks the
publish node entirely rather than merely narrowing its blast radius.
`approve-reads` lets those steps proceed (subject to approval) while still
keeping the agent from silently crossing into mutations the operator did
not authorize; the narrow scope of what it actually runs — the four
`compozy worktree` exit verbs (`exit`, `push`, `pr`, `exit-cancel`) plus
two read-only git commands — is stated and enforced in the agent's prompt
body ("you never edit files, never commit, never approve gates, and never
touch a branch other than the delivery worktree you were bound to"), the
same behavioral mechanism `batuta`, `reviewer`, and `review_fixer` already
rely on. The effective
controls that actually bound this agent's blast radius are structural,
not permission-schema-based: the human gate that must approve before the
publisher ever runs, and the daemon's own exit-action safety (durable,
idempotent `op_id`s; clean-HEAD/dirty-tree checks; re-read-then-reconcile
recovery) described below.

Fixed objective, in order:

1. `compozy worktree exit <ref> -o json` — the daemon-computed exit plan is
   the source of truth for blocked reasons, forge vocabulary, and
   `pr_prefill` (treated as untrusted plain text).
2. `compozy worktree push <ref> -o json` — sets upstream when needed.
3. `compozy worktree pr <ref> --title … --body … --base <default branch>
   -o json` — title and body derived from the slug's spec artifacts.
4. Report the PR URL as publication evidence.

### Publication failure and reconciliation

Every exit action returns a durable `op_id` and continues after disconnect;
the publisher's recovery rule is always "re-read the exit plan, then
reconcile", never blind retry:

- Push rejected (non-fast-forward, permission, protected branch) → report
  the structured refusal; node fails; branch untouched locally.
- Ambiguous push outcome (connection lost mid-action) → re-read the exit
  plan; it reflects whether the remote ref advanced. Re-running push after
  a successful remote update is a no-op upstream set.
- PR creation failure after a successful push → the push stands; the daemon
  returns an existing open PR instead of duplicating one, so a retry is
  safe. If the forge remains unavailable (`forge_unavailable` /
  `forge_error`), the publisher reports "pushed, PR manual" with the exit
  plan's browser compare URL, and the run completes `done` with that
  evidence — the honest forge dependency: PR creation requires a serving
  credentialed `forge.provider` extension, which remains an operator
  prerequisite like model authentication.
- Step-level evidence (`op_id`s, structured results) is preserved in the
  node output for the terminal report.

## Contract update

`batuta-deliver`'s `goal` and `definition_of_done` change to cover
publication. Definition of done: the implement child ended `done`, the
review child ended `done` or `no-op`, and either the `publish_check` route
was "nothing to publish", or the gate was approved and `publish` ended
`done` with push evidence plus a PR URL or an explicit push-only compare
URL. A rejected gate is `blocked`; a publish-node failure is `failed`.

## Conductor contract changes

`agents/batuta/AGENT.md`:

- Dispatch section: create-or-reuse the delivery worktree per the rules
  above, verify `ready` with a structured read, pass `worktree_ref` to the
  dry-run and real run, and state the parked-gate expectation in the
  dispatch acknowledgement.
- Escalation section: the parked `publish_gate` joins `needs-approval`
  handling — Batuta reports run and gate IDs and waits; the operator
  decides publication.
- "What you never do" is unchanged. The publisher executor — not the
  conductor — performs push and PR, and only after a human approval the
  conductor cannot grant itself.

## Extension inventory

The extension grows from three resources to four: the `batuta` agent, the
`batuta-publisher` agent, the `batuta-routing` skill, and the
`batuta-deliver` Loop. Inventory contracts and docs update accordingly.

## Verification

- Loop validation: `compozy loop validate` passes on the extended
  definition (new input, environment default, branch node, gate, goal
  node, pause-trigger effect). If validation rejects the pause-trigger
  effect, the fallback surfacing mode is implemented and the primary is
  dropped from the definition — this choice is recorded in the plan.
- Static contract checks: AGENT.md names the worktree step, the
  inspect-and-confirm reuse rule, and the publish-gate escalation; the
  prohibition list is byte-identical; the publisher agent's permission
  surface names only the worktree exit verbs.
- E2E (public events, no private stores): a delivery with commits shows
  implement/review executing in the worktree environment, a
  `needs-approval` park on `publish_gate`, an operator approval, a push,
  and PR-or-compare-URL evidence bound to the recorded HEAD SHA; a
  `reject` path ends `blocked` with the branch still present; a delivery
  with no publishable commits never reaches the gate; a dirty-worktree
  publish aborts without pushing.
- Redispatch smoke: a second dispatch for the same slug passes the
  inspect-and-confirm reuse path and creates no duplicate worktree; a
  mismatching inspect stops for operator choice.

## Non-goals

- No change to CompozyOS or the spec-cycle extension.
- No auto-merge, no branch deletion, no rebase/merge repair — behind or
  diverged branches remain explicit operator repairs.
- No forge-provider bundling or credential handling.
- No per-task worktrees (`per_run`); one worktree per delivery is the unit.
- No gate deadline or auto-timeout: a parked gate is operator-owned, does
  not consume budget, and waits until decided.
