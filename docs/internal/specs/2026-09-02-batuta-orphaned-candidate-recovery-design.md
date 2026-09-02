# Batuta orphaned candidate recovery — design

Status: draft, follow-up recorded on 2026-09-02

## Objective

Stop re-implementing a task whose child run already completed with a
verified commit whenever the parent Loop fails between `run_task` and
`record_candidate`. Recovery must reuse the existing task worktree and
commit as the next attempt instead of blocking the delivery and forcing a
brand-new delivery that redoes the work.

The fix stays entirely inside Batuta: journal reconciliation, the recovery
envelope, and `prepare_wave`. No Compozy source or runtime contract changes.

## Incident and responsible mechanism

On 2026-09-02 the same task (`task_01` of `city-temperature-demo`) was
implemented four times. Each time the `batuta-task` child reached `done` with
a passing verification and a local commit, and each time the parent
`batuta-deliver-core` run failed in the node right after it:

- attempt A: the child closed as `exhausted` against the old 1M token ceiling
  after the work was committed;
- attempts B and C: `record_candidate` failed (dead run-loop output template,
  then an unreproducible agent-computed verification digest).

In every case the journal was left with the task attempt in state `running`,
no `child_run_id`, no candidate, and a healthy task worktree holding the
commit. `reconcile_fallbacks` then reasoned only from journal task states:

- `graphFallbackTaskIDs` found no `blocked` task, so `failedTaskIDs` was empty;
- the parent status was `failed` with no failed task, so the delivery became
  `blocked` with `non_recoverable_graph_failure`;
- `recover_delivery` refused, and the conductor started a new delivery whose
  `prepare_wave` created a fresh worktree from the base commit.

The responsible mechanism is that reconciliation treats "parent failed while a
task is still `running`" as unknowable, while the durable evidence to settle it
(the terminal child run, its recorded `implementation` output, and the task
worktree) is fully available through public Compozy surfaces.

## Design

### Reconciliation settles running tasks from durable evidence

When `Reconcile` observes a terminal settlement parent with graph authority and
a task attempt still in `running`, it resolves the attempt exactly the way
`record_candidate` now does:

1. list recent `batuta-task` runs and select the single terminal run whose
   inputs match the attempt (`delivery_id`, `wave`, `task_id`, run execution,
   `worktree_ref`, `base_sha`, `routing_generation`, `runtime`);
2. if that run is `done`/`no-op` and carries a valid `implementation` output,
   record the candidate through the existing `record_candidate` path (canonical
   verification, derived digest, worktree evidence validation) so the attempt
   becomes `candidate` with the same journal transitions a live run produces;
3. if the run is any other terminal status, record the failure through the
   existing `record_failure` path with blocker
   `task_terminal_<status>_reconciled`;
4. if zero or several runs match, leave the attempt untouched and keep today's
   blocked outcome with a new blocker `task_liveness_unknown` so the operator
   sees why.

Only after this settlement does reconciliation compute `failedTaskIDs`,
budgets, and the delivery state. A delivery whose tasks are all `candidate`,
`integrated`, or `blocked`-with-fallback is recoverable.

### Recovery resumes from the settled graph

`recover_delivery` already builds the next attempt from the journal graph. With
the settlement above:

- candidate attempts are kept; `prepare_wave` on the next attempt sees the wave
  as `wave_ready` with no task to run and proceeds to `settle_wave`, which
  integrates the recorded commit;
- blocked attempts with a remaining fallback get the usual re-execution;
- the task worktree is reused, never recreated, because the attempt still
  references it.

### Public contract

`reconcile_fallbacks` output gains `settled_tasks`: a list of
`{task_id, execution, outcome: candidate|failed|unknown, child_run_id}` so the
conductor can report what was salvaged. No input changes.

## Boundaries

- Reconciliation never inspects the worktree by itself; candidate validation
  stays in `record_candidate` through `integration.CandidateRequest`.
- No commit, integration, or publication happens during reconciliation; only
  journal transitions that a live run would have produced.
- A task whose child run is still live is never settled; the existing
  `in_progress` result stands.

## Tests

- Reconcile with parent `failed`, task `running`, child `done` with a valid
  `implementation` output: attempt becomes `candidate`, delivery stays
  `active`, `recover_delivery` produces attempt 2 whose `prepare_wave` reuses
  the worktree and reaches `settle_wave` without running the task.
- Same with child `exhausted`: attempt becomes `blocked` with the reconciled
  blocker and the fallback re-execution is scheduled.
- Zero or two matching child runs: attempt untouched, blocker
  `task_liveness_unknown`.
- `settled_tasks` is present and exact in every case.

## Out of scope

- Reusing a worktree across deliveries (a new `delivery_id`). Recovery is
  scoped to the same delivery, as today.
- Changing Compozy's `run-loop` output contract; Batuta keeps resolving child
  runs by inputs.
