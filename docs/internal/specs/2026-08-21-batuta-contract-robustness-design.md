# Batuta delivery contract robustness — design

Design approved in conversation on 2026-08-21; revised the same day after
external review. Four smaller gaps in the delivery contract close together:
an unlimited runtime budget, undefined redispatch semantics after a partial
failure, underspecified `auto_commit=false` behavior, and preference-gate
friction on conversational turns.

## Budget backstop

`batuta-deliver`'s contract carries `budget.wall_clock_sec: 14400` (4
hours) with `on_exceeded: halt`. `tokens` stays `0` (unlimited) — a correct
token ceiling varies too much per delivery to fix a default.

**Grammar verification gated this design as originally written, and it
failed:** `compozy loop validate` rejects a template in the contract budget
field, so the input-referenced design (`wall_clock_sec` as a declared
Loop input, resolved through the daemon's per-key `run`/`workspace`/
`global`/`definition` layering) was dropped before implementation. The
fallback taken — a fixed `wall_clock_sec: 14400` literal in the definition
— is what shipped.

**Daemon limitation proved after the fallback shipped:** the fixed literal
is not itself an enforcement mechanism. A `compozy loop run --dry-run`
probe against a disposable workspace (see
`.superpowers/sdd/2026-08-21-batuta-contract-robustness/
budget-verification-report.md` for verbatim commands and output) showed:

- With the shipped `wall_clock_sec: 14400` literal and no stored config,
  the dry-run's `materialized_contract.budget.wall_clock_sec` reports
  `14400`, but the daemon's actually-enforced
  `effective_config.budget_wall_sec` reports `0` (unbounded) — the two are
  independent fields and the daemon does not derive the second from the
  first.
- Changing the definition's literal to `999` moved
  `materialized_contract.budget.wall_clock_sec` to `999` but left
  `effective_config.budget_wall_sec` at `0` — confirming the literal has no
  causal effect on enforcement.
- A per-run override (`compozy loop run --config-file` with
  `{"budget_wall_sec": N}`, i.e. `compozy__loop_run`'s
  `config_overrides.budget_wall_sec`) sets `effective_config.budget_wall_sec`
  to `N`.
- A stored workspace-scope override (`compozy loop configure --set
  budget_wall_sec=N`, i.e. `compozy__loop_configure`) also sets
  `effective_config.budget_wall_sec` to `N`, and persists across dry-runs
  that carry no per-run override.
- Precedence confirmed by conflicting values: per-run `config_overrides`
  (777) beat a stored override (14400) beat the daemon default (0).
- The dry-run response carries no origin/provenance field alongside
  `effective_config` — nothing in the structured output names which layer
  produced the resolved number.
- `compozy__loop_configure` called with an empty `config: {}` object is a
  safe, idempotent read: it returns the current stored `config` and
  `effective_config` without mutating the stored value (verified: a stored
  `budget_wall_sec: 14400` survived an empty-`config` call unchanged).

Given that, the 4-hour backstop is made real through the layer the daemon
actually honors: Batuta's Bootstrap phase provisions a stored
workspace-scope override — `compozy__loop_configure` (`name:
batuta-deliver`, `config: {budget_wall_sec: 14400, budget_on_exceeded:
halt}`) — once per workspace, verified with the empty-`config` read
pattern above. Dispatch re-verifies `effective_config.budget_wall_sec` is
nonzero on every dry-run before submitting the real run, and stops with the
exact structured evidence instead of dispatching an unbounded delivery when
it is `0`. The definition's `contract.budget.wall_clock_sec: 14400` literal
is kept as declared intent — it documents the same number the Bootstrap
override applies and remains what `materialized_contract` reports — but
`agents/batuta/AGENT.md` is explicit that the literal alone enforces
nothing.

The backstop is therefore Batuta's, not the Loop's: a `batuta-deliver`
submission that bypasses Batuta's own dispatch (cli/http/uds/native_tool/
schedule starts) begins at `effective_config.budget_wall_sec: 0` —
unbounded — in any workspace where Bootstrap has not yet provisioned the
override, and stays unbounded until it has.

With `halt`, crossing the effective budget ends the run on the `exhausted`
terminal — no synthetic gate, no silent continuation — and the terminal
effect returns it to the origin session like any other outcome. Recovery is
a conversation: Batuta reports the exhaustion and, after the
partial-progress audit below, a legitimately long delivery is redispatched
with `config_overrides.budget_wall_sec` raised for that one call (never by
editing the bundled Loop definition, and never by changing the stored
workspace override, which stays at the 4-hour default). The `escalate`
policy (which parks on the daemon's synthetic `budget` gate for a one-shot
operator continuation) was considered and rejected: a 4-hour crossing is
more likely a stuck run than a long one, and a parked gate keeps it alive
silently.

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

1. Batuta reads `compozy__loop_status` for the parent delivery run to get
   the implement child's `child_loop_run_id`, then reads
   `compozy__loop_status` for that child run: its per-generation `outputs`
   is the per-task roster, with `item_index` mapping positionally to the
   authored `task_NN.md` set. `compozy__loop_nodes` cannot supply this — its
   `state` filter only returns items currently `waiting`, `quarantined`,
   `attention`, or `retrying`, never a full per-task terminal listing.
   Under `auto_commit=true`, an item's success status means that task
   landed as one commit on the delivery branch, confirmed with
   `compozy__worktree_inspect`; under `auto_commit=false`, there is no
   commit boundary, so Batuta reports `compozy__worktree_inspect`'s
   working-tree state instead.
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
- publication (companion design) still parks on the human gate regardless
  of `auto_commit` — the branch check that would otherwise skip the gate on
  "nothing to publish" is unconditionally routed to the gate instead (the
  daemon cannot prove an `ahead_of_base` reading is fresh), so a fresh
  `false` delivery with nothing on the branch reaches the gate the same as
  any other and only reports "nothing to publish" after the operator
  approves it; commits inherited from a reused worktree remain publishable
  and are never silently dropped by the boolean;
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

- `loops/batuta-deliver/loop.yaml`: the fixed `contract.budget.wall_clock_sec:
  14400` literal (the input-referenced form was rejected by grammar
  verification) plus a comment documenting that the literal is declared
  intent, not the enforcement mechanism.
- `agents/batuta/AGENT.md`: gate scoping with the delivery-path list, the
  any-partial-terminal audit procedure, `auto_commit=false` consequences,
  a Bootstrap step that provisions and verifies the stored
  `compozy__loop_configure` budget override, a Dispatch check that stops
  before submitting an unbounded real run, and the exhausted-budget
  redispatch procedure naming the exact `config: {}` verification read.
- `docs/how-it-works.md`, both READMEs: the same contracts in reading
  order.

Follow-up, outside this delivery's acceptance criteria: file the
spec-cycle skip-if-committed feature request upstream.

## Verification

- Loop validation passes with the fixed-value budget literal.
- Static contract checks: `tests/contract/test_07_public_docs.sh` pins the
  literal AGENT.md strings that carry the budget-enforcement mechanism so
  the whole Bootstrap-provisioning-plus-dispatch-verification chain cannot
  be silently deleted with a green suite — it asserts the exact
  delivery-path call list (`The delivery-path calls are exactly: the
  ext__spec_cycle__import_tasks preflight, delivery worktree creation, a
  batuta-deliver dry-run, and a real dispatch.`), both
  `effective_config.budget_wall_sec` and `config_overrides.budget_wall_sec`
  appearing in AGENT.md, the Bootstrap provisioning value
  (`budget_wall_sec: 14400, budget_on_exceeded: halt`), and the audit's
  non-success terminal list (`` `failed`, `exhausted`, `stalled`,
  `canceled`, and `blocked` alike``). It does not re-run the dry-run probe
  itself — that evidence is recorded once in
  `.superpowers/sdd/2026-08-21-batuta-contract-robustness/
  budget-verification-report.md` — but it does pin that the AGENT.md text
  describing the mechanism the probe validated stays present.
- E2E: a conversational turn performs no config read; a dispatch turn shows
  the gate read before `import_tasks`; a forced budget crossing ends the
  run `exhausted`, and the following turn shows the audit read before any
  redispatch is offered.

## Non-goals

- No change to spec-cycle Loops (skip-if-committed stays upstream).
- No token-budget default.
- No new configuration keys beyond the daemon's existing
  `compozy__loop_configure` `budget_wall_sec` field on `batuta-deliver`.
- No weakening of the gate's persist-and-reread protocol.
- No synthetic-gate (`escalate`) budget continuation.
