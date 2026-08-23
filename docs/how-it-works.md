# How Batuta works

This is the operational contract of the `batuta` agent. The source of truth
is [`agents/batuta/AGENT.md`](../agents/batuta/AGENT.md) and
[`resources/skills/batuta-routing/SKILL.md`](../resources/skills/batuta-routing/SKILL.md);
this page explains the same rules in reading order.

## 1. Delivery preference gate

The first tool call of every new session is `compozy__config_get` for exactly
`loops.inputs.batuta-deliver.auto_commit` in the current workspace. Both
`true` and `false` open the gate. On `config_path_not_found`, Batuta asks
whether automatic commits should be enabled, persists your boolean answer at
workspace scope, rereads it, and only then continues. Any other config error
stops the session with the exact structured error. Global defaults, child
Loop defaults, the `batuta-deliver` definition default, and dry-runs never
substitute for the stored preference. Batuta rereads the key before every
dispatch.

## 2. Bootstrap and routing

After the gate opens, Batuta reads the stored `implement-tasks` runtime rules.
When absent, it derives them: the `batuta-routing` skill gives lane semantics
(`low`, `medium`, `high`, `critical`) and the JSON shape; the live catalog
from `compozy__provider_models_list` (with costs) is the only source of
provider and model IDs. Batuta shows the derived table with costs, waits for
your confirmation, then stores it with `compozy__loop_configure`
(`name: implement-tasks`, field `runtime_rules`). Reconfiguration later is a
conversation request that re-applies the override. Routing is auditable per
generation in `resolved_runtime`.

Provider authentication is yours to do once, outside the extension.

## 3. Requirements and tasks (spec-cycle)

Batuta runs `cy-create-spec` for every delivery. A simple request may use a
short grill but never skips the spec. You approve `_spec.md`,
`_user_stories.md`, `_dx.md`, and `_tests.md`; `_uiux.md` only when a Web
surface changes. Then `cy-create-tasks` writes `_tasks.md` plus `task_NN.md`
under `.compozy/tasks/<slug>/`, each with `type` and `complexity`
frontmatter — the fields routing matches on. Executable requirements
(package names and versions, commands, paths, flags) stay literal from the
conversation to the execution prompts.

## 4. Preflight, dry-run, dispatch

Before dispatch Batuta calls the read-only `ext__spec_cycle__import_tasks`
with `pattern=.compozy/tasks/<slug>/task_*.md` and continues only when it
returns `count > 0`. A Loop dry-run plans nodes but does not execute the
import, so it cannot prove tasks exist. Then it dry-runs `batuta-deliver` with
`slug`, `origin_session_id` (its own session), and `auto_commit` (the
verified workspace boolean), shows the plan, and submits the real run with
the same inputs. `batuta-deliver` chains the bundled `implement-tasks` and
`review-and-fix` Loops inside the daemon; Batuta never dispatches them
separately and never sends per-run runtime rules.

A successful dispatch ends the turn: Batuta reports `run_id` (and `web_url`
when available) and stops.

## 5. Event-driven return

Every terminal effect of `batuta-deliver` (`done`, `no-op`, `blocked`,
`failed`, `canceled`, `exhausted`, `stalled`) queues one idempotent prompt
back to the `origin_session_id`. In that turn Batuta's first operational call
is `compozy__loop_status` for the exact delivery run; it then reports the
literal parent and child outcomes, commits, and blockers. There is no
watcher, poller, or reporting agent. An explicit progress request takes one
`compozy__loop_status` snapshot and ends the turn.

## 6. Publication gate

`batuta-deliver` runs the whole cycle in a delivery worktree named
`batuta-<slug>`, on branch `batuta/<slug>`, created or reused by Batuta at
dispatch with the native worktree tools; the chained child Loops inherit
that worktree environment, so `implement-tasks` and `review-and-fix` commit
inside it, never on the base branch.

After `review-and-fix` ends clean, the Loop inspects the worktree and
branches on the evidence: when `ahead_of_base` is `0`, there is nothing to
publish and the run completes `done` without ever reaching the gate. When
there are commits ahead of base, the run parks on a human `publish_gate` —
reported to you as `needs-approval` with the run and gate IDs, exactly like
any other human gate (see Escalation below). Rejecting the gate ends the run
`blocked` with the branch and its commits preserved, untouched, for you to
inspect or resume.

Approving the gate hands off to the `batuta-publisher` agent, and only to
it: Batuta itself never pushes or publishes. The publisher checks the
worktree is clean and records `HEAD`, reads the exit plan, pushes the
branch, and opens a PR against the repository default branch. When no forge
provider serves the repository, it reports "pushed, PR manual" together
with the exit plan's compare URL instead of a PR URL — that is a successful
outcome, not a failure.

Time spent parked on the publication gate does not consume the delivery
budget: the daemon suspends the run's wall-clock work budget while it waits
on the operator's decision.

## 7. Escalation

Retry, quarantine, and failure classes belong to the daemon. A task that
fails repeatedly in its lane gets a surgical `id` rule one lane up in the
stored `implement-tasks` override, a redispatch, and the rule removed after
it lands. `needs-approval` gates are surfaced to you with run and gate IDs —
Batuta cannot approve runs it started. Ambiguity mid-run comes back to the
conversation.

## What Batuta never does

Write, edit, or commit code; fork or mutate the bundled Loop definitions;
push to any remote; approve its own runs; report a terminal state other than
the daemon's exact one.
