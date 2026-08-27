# Batuta architecture

## Purpose and boundaries

Batuta is an independent community project for CompozyOS. It conducts the
engineering lifecycle but does not implement feature code itself.

The Batuta session has the full tool scope inside its authenticated workspace.
It researches the repository and authors the complete SDD with the bundled
`spec-cycle` skills. Product-code implementation and review remain owned by the
executor Loops.

## Components

Batuta packages one `batuta` agent, one `batuta-routing` skill, one
`batuta-deliver` Loop, and deterministic extension tools for inventory,
routing, publication, and verification. The bundled `spec-cycle` extension
supplies `cy-create-spec`, `cy-create-tasks`,
`ext__spec_cycle__import_tasks`, `implement-tasks`, and `review-and-fix`.

## Data flow

```text
Operator
  -> Batuta session
  -> spec-cycle SDD and task artifacts
  -> executor inventory
  -> domain x complexity routing plan
  -> immutable matrix archive and delivery_id
  -> delivery worktree
  -> batuta-deliver attempt 1 (fresh Compozy run)
     -> implement-tasks: one commit per task
     -> on recoverable failure: settle exact evidence
  -> batuta-deliver attempt N (fresh run, same worktree, failed tasks only)
     -> review-and-fix: review the complete phase after implementation succeeds
     -> publication plan
     -> ext__batuta__publish_worktree
     -> independent publication verification
  -> compozy__session_prompt terminal return
  -> original Batuta conversation
```

## Task, phase, and PR boundaries

A task is one implementation item and produces one commit. One execution of
`batuta-deliver` is a delivery phase and produces at most one pull request. The
approved slug is one phase by default; explicit multi-phase graph engineering
is a later extension. This gives one PR per delivery phase without publishing
each task independently.

## Resource and authority boundaries

Batuta may read and write SDD artifacts anywhere inside the trusted workspace.
It must not implement feature code. `implement-tasks` and `review-and-fix`
perform product mutations in the managed worktree.

Publication has no human gate and no publisher agent. The Loop passes the exact
reviewed HEAD from the read-only publication plan directly to
`ext__batuta__publish_worktree`, then passes that structured result to an
independent verifier. Push and PR creation are automatic; merge remains manual.

Routing configuration is not stored in Compozy. Batuta's guarded matrix tool
archives one immutable generation and a stable `delivery_id` in its local
journal. Every attempt receives typed ephemeral child overrides. A recovery
starts a new parent run with a new `run_id`, keeps the same delivery, worktree,
task snapshot, and routing generation, and advances only exact failed tasks to
their next candidate. No Batuta-specific Compozy migration or config CAS is
required.

## Session lifecycle

Terminal effects return idempotently to the origin conversation. Batuta reads
the exact run, reconciles it into the delivery journal, starts at most one
eligible fresh-run recovery, or reports the authoritative result. Attempt cap,
token ceiling, absolute deadline, fallback exhaustion, drift, cancellation,
stalled execution, review failure, and ambiguous mutation evidence stop before
another submission. There is no reporting agent or poller.

## Trust and compatibility

For CompozyOS concepts and compatibility, use the
[official documentation](https://www.compozy.com/docs/) and
[official repository](https://github.com/compozy/compozy). The Go dependency is
the upstream SDK `v0.3.0-beta.21`. Public promotion additionally requires an
official Compozy binary carrying child `run-loop` `config_overrides`; until
then, the branch is locally integrated against a compatible preview.
