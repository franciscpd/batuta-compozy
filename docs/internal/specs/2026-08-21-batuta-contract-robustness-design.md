# Batuta delivery contract robustness — design

Design approved in conversation on 2026-08-21; revised the same day after
external review. Four smaller gaps in the delivery contract close together:
an unlimited runtime budget, undefined redispatch semantics after a partial
failure, underspecified `auto_commit=false` behavior, and preference-gate
friction on conversational turns.

## Budget backstop

`batuta-deliver` gains a declared input `wall_clock_sec` (integer, default
`14400` — 4 hours) referenced by the contract budget, with
`on_exceeded: halt`. `tokens` stays `0` (unlimited) — a correct token
ceiling varies too much per delivery to fix a default.

Input resolution is the daemon's per-key layering (`run`, `workspace`,
`global`, then `definition`), so a legitimately long delivery raises the
budget per dispatch as a run input. The dry-run reports the effective value
and its origin; Batuta states that value to the operator at dry-run time and
submits the real run with identical inputs, per the existing dispatch rule.

Grammar verification gates this design: if `compozy loop validate` rejects
an input reference in the contract budget field, the fallback is a fixed
`wall_clock_sec: 14400` in the definition, and the raised-budget path is
dropped — recorded in the plan, with the rest of this design unchanged.

With `halt`, crossing the budget ends the run on the `exhausted` terminal —
no synthetic gate, no silent continuation — and the terminal effect returns
it to the origin session like any other outcome. Recovery is a
conversation: Batuta reports the exhaustion and, after the partial-progress
audit below, a legitimately long delivery is redispatched with a raised
budget. The `escalate` policy (which parks on the daemon's synthetic
`budget` gate for a one-shot operator continuation) was considered and
rejected: a 4-hour crossing is more likely a stuck run than a long one, and
a parked gate keeps it alive silently.

Human-gate residence does not consume this budget: the daemon suspends node
clocks and the run wall-clock work budget during approval waits, so a
delivery parked on the publication gate (companion design, same date)
cannot exhaust while waiting for the operator.

## Redispatch after a partial run

`implement-tasks` belongs to the bundled spec-cycle extension; Batuta does
not change it and does not gain skip-if-committed behavior. What this
design fixes is the conductor's contract, which today says nothing.

On EVERY non-success terminal where the `implement` node may have started —
`failed`, `exhausted`, `stalled`, `canceled`, and `blocked` alike — before
any redispatch:

1. Batuta reads `compozy__loop_nodes` for the implement child and reports
   per task: terminal state and commit evidence (landed / not landed) on
   the delivery branch.
2. Batuta states explicitly that a redispatch re-executes the full task set
   and may re-apply already-landed tasks, and decides with the operator
   whether to redispatch, amend the task set first, or stop.
3. Worktree isolation (companion design) bounds the blast radius:
   re-execution happens on `batuta/<slug>`, never on the operator's branch,
   and the reuse of that worktree follows the companion design's
   inspect-and-confirm rules.

## `auto_commit=false` semantics

Documented, not restricted:

- implement runs leave changes uncommitted in the delivery worktree;
- `review-and-fix` reviews the working tree without commit boundaries
  between tasks;
- publication (companion design) routes on branch-vs-base commit evidence,
  so a fresh `false` delivery has nothing to publish and skips the gate —
  but commits inherited from a reused worktree remain publishable and are
  never silently dropped by the boolean;
- integration of uncommitted work is fully manual: the operator commits
  from the worktree.

Batuta states these consequences in the preference-gate conversation
whenever the operator chooses or confirms `false`, before persisting it.

## Preference gate scoped to delivery

The delivery preference gate moves from "first tool call of every session"
to "before the first delivery-path tool call". Delivery-path calls are
exactly: the `ext__spec_cycle__import_tasks` preflight, worktree creation,
`batuta-deliver` dry-run, and real dispatch.

- Purely conversational turns — questions, reports, status reads, and
  spec-cycle requirement authoring (`cy-create-spec`, `cy-create-tasks`)
  with their approval dialogues — make no mandatory config read: authored
  artifacts do not depend on the commit preference.
- The gate — `compozy__config_get` for exactly
  `loops.inputs.batuta-deliver.auto_commit`, workspace-bound, with the
  persist-and-reread protocol on `config_path_not_found` — must complete
  before the first delivery-path call of the session.
- The reread before every dispatch is unchanged.
- Every other rule of the gate (structured boolean only, literal `false`,
  stop on other errors) is unchanged.

## Changes

- `loops/batuta-deliver/loop.yaml`: the `wall_clock_sec` input and the
  contract budget reference.
- `agents/batuta/AGENT.md`: gate scoping with the delivery-path list, the
  any-partial-terminal audit procedure, `auto_commit=false` consequences,
  and the exhausted-budget raised-input redispatch procedure.
- `docs/how-it-works.md`, both READMEs: the same contracts in reading
  order.

Follow-up, outside this delivery's acceptance criteria: file the
spec-cycle skip-if-committed feature request upstream.

## Verification

- Loop validation passes with the input-referenced budget (or the recorded
  fixed-value fallback).
- Static contract checks: AGENT.md orders the gate before the enumerated
  delivery-path calls (not before conversation or spec authoring), names
  the partial-run audit for every non-success terminal, and the
  `auto_commit=false` consequences.
- E2E: a conversational turn performs no config read; a dispatch turn shows
  the gate read before `import_tasks`; a forced budget crossing ends the
  run `exhausted`, and the following turn shows the audit read before any
  redispatch is offered.

## Non-goals

- No change to spec-cycle Loops (skip-if-committed stays upstream).
- No token-budget default.
- No new configuration keys beyond the declared Loop input's standard
  `loops.inputs.batuta-deliver.wall_clock_sec` surface.
- No weakening of the gate's persist-and-reread protocol.
- No synthetic-gate (`escalate`) budget continuation.
