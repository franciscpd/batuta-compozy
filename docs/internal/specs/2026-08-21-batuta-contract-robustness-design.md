# Batuta delivery contract robustness — design

Design approved in conversation on 2026-08-21. Four smaller gaps in the
delivery contract close together: an unlimited runtime budget, undefined
redispatch semantics after a partial failure, underspecified
`auto_commit=false` behavior, and preference-gate friction on conversational
turns.

## Budget backstop

`loops/batuta-deliver/loop.yaml` contract budget changes from the unlimited
`{tokens: 0, wall_clock_sec: 0}` to:

- `wall_clock_sec: 14400` (4 hours), `on_exceeded: halt`;
- `tokens` stays `0` (unlimited) — a correct token ceiling varies too much
  per delivery to fix a default.

With `halt`, crossing the budget ends the run on the `exhausted` terminal —
no synthetic gate, no silent continuation — and the terminal effect returns
it to the origin session like any other outcome. Recovery is a conversation:
Batuta reports the exhaustion, and a legitimately long delivery is
redispatched with a raised per-run budget stated to the operator at dry-run
time. The `escalate` policy (which parks on the daemon's synthetic `budget`
gate for a one-shot operator continuation) was considered and rejected: a
4-hour crossing is more likely a stuck run than a long one, and a parked
gate keeps it alive silently.

## Redispatch after partial failure

`implement-tasks` belongs to the bundled spec-cycle extension; Batuta does
not change it and does not gain skip-if-committed behavior. What this design
fixes is the conductor's contract, which today says nothing:

1. On a `failed` deliver whose `implement` child stopped partway, Batuta
   reads `compozy__loop_nodes` for the child, and reports per task: terminal
   state and commit evidence (landed / not landed).
2. Batuta states explicitly that a redispatch re-executes the full task set
   and may re-apply already-landed tasks, and decides with the operator
   whether to redispatch, amend the task set first, or stop.
3. Worktree isolation (companion design, same date) bounds the blast radius:
   re-execution happens on `batuta/<slug>`, never on the operator's branch.

True skip-if-committed is recorded as an upstream spec-cycle feature
request, not implemented here.

## `auto_commit=false` semantics

Documented, not restricted:

- implement runs leave changes uncommitted in the delivery worktree;
- `review-and-fix` reviews the working tree without commit boundaries
  between tasks;
- publication (companion design) is skipped — there is nothing to push;
- integration is fully manual: the operator commits from the worktree.

Batuta states these consequences in the preference-gate conversation
whenever the operator chooses or confirms `false`, before persisting it.

## Preference gate scoped to delivery

The delivery preference gate moves from "first tool call of every session"
to "before the first delivery-path tool call":

- purely conversational turns (questions, reports, PM dialogue) make no
  mandatory config read;
- the gate — `compozy__config_get` for exactly
  `loops.inputs.batuta-deliver.auto_commit`, workspace-bound, with the
  persist-and-reread protocol on `config_path_not_found` — must complete
  before any preflight (`ext__spec_cycle__import_tasks`), dry-run, worktree
  creation, or dispatch;
- the reread before every dispatch is unchanged;
- every other rule of the gate (structured boolean only, literal `false`,
  stop on other errors) is unchanged.

## Changes

- `loops/batuta-deliver/loop.yaml`: budget values only.
- `agents/batuta/AGENT.md`: gate scoping, partial-failure redispatch
  procedure, `auto_commit=false` consequences, exhausted-budget
  redispatch procedure.
- `docs/how-it-works.md`, both READMEs: the same contracts in reading order.
- Upstream: file the spec-cycle skip-if-committed feature request.

## Verification

- Loop validation passes with the new budget block.
- Static contract checks: AGENT.md orders the gate before preflight (not
  before conversation), names the redispatch procedure and the
  `auto_commit=false` consequences.
- E2E: a conversational turn performs no config read; a dispatch turn shows
  the gate read before `import_tasks`; a forced budget crossing ends the run
  `exhausted` and Batuta reports it literally, offering a raised-budget
  redispatch instead of approving or polling anything.

## Non-goals

- No change to spec-cycle Loops (skip-if-committed stays upstream).
- No token-budget default.
- No new configuration keys.
- No weakening of the gate's persist-and-reread protocol.
