# Batuta architecture

## Purpose and boundaries

Batuta is an independent community project for CompozyOS. The `batuta` session
has full trusted-workspace authority to research and write the SDD; it never
implements feature code. Implementation, commits, review, integration, and
publication are owned by bounded executor Loops and deterministic extension
tools.

## Resource inventory

The Go extension exposes exactly these executable resources:

- agent: `batuta`;
- skill: `batuta-routing`;
- Loops: `batuta-deliver` and `batuta-task`;
- nine hosted Batuta tools: `ext__batuta__delivery_budget_context`,
  `ext__batuta__delivery_graph`, `ext__batuta__executor_inventory`,
  `ext__batuta__publication_plan`, `ext__batuta__publication_verify`,
  `ext__batuta__publish_worktree`, `ext__batuta__routing_apply`,
  `ext__batuta__routing_context`, and `ext__batuta__routing_plan`.

The bundled `spec-cycle` supplies SDD authoring and canonical task import. It
does not authorize Batuta to edit implementation files.

## Data flow

```text
Operator
  -> Batuta session
  -> interactive SDD clarification cards, spec-cycle SDD, and task artifacts
  -> automatic executor inventory and domain x complexity routing generation
  -> integration worktree and stable delivery_id
  -> batuta-deliver parent run
     -> dependency-safe wave (at most four isolated task worktrees)
        -> batuta-task run-agent implementation
        -> optional typed ask -> durable answer -> same child/worktree resume
        -> one implementation commit and completed evidence
     -> deterministic canonical integration
        -> conflict: new immutable execution and task worktree
     -> one review-and-fix child
     -> exact-HEAD publication plan, push/PR, and independent verification
  -> compozy__session_prompt terminal return
  -> original Batuta conversation
```

## Task, integration, review, and PR boundaries

One approved task produces one Conventional Commit in its task worktree. The
graph integrates it into the canonical integration worktree only after the
recorded candidate is verified. Tasks that do not depend on one another may run
in parallel, but the graph has max-four dependency-safe parallelism and never
runs concurrent writers in one worktree.

An integration conflict is not merged by guesswork: the journal allocates a
canonical conflict reexecution with a fresh immutable execution, base SHA, and
task worktree. Once every task is integrated, `review-and-fix` runs once for the
delivery. Publication plans and publishes the exact reviewed HEAD, opens or
reuses one PR, and verifies it independently; merge remains manual.

## Clarification paths

Interactive SDD clarification cards belong to the parent Batuta conversation:
they settle material product intent before task approval. The in-delivery `ask`
belongs only to a parked `batuta-task` child. `record_question` persists the
child identity and canonical prompt/context/expect; `record_answer` accepts the
typed answer from that exact `ask_operator` cell and resumes the same task
attempt. An answer never becomes a cross-task instruction or a new worktree.

## Stop conditions and retained evidence

The journal prevents another generation/review/publication when capacity,
physical-attempt cap, fresh-parent cap, token ceiling, active wall-clock,
fallback exhaustion, cancellation, no-progress/stall, open human pause,
ambiguous worktree/Git/journal evidence, or a terminal publication state stops
the delivery. Safe cleanup is the sole successful terminal path. If cleanup
must retain a diagnostic worktree, the retained diagnostic worktree has stable
blocked evidence rather than claiming success or retrying it.

All create, question/answer, candidate, settlement, retry, review, publication,
verification, and cleanup operations are journaled and replay-safe.

## Trust and compatibility

For CompozyOS concepts, use the [official documentation](https://www.compozy.com/docs/)
and [official repository](https://github.com/compozy/compozy). The direct Go
dependency is upstream `v0.3.0-beta.21`; Batuta's tested minimum compatible
source baseline is commit `382976d4b43274630a4b67445812fd4a0216dbcc`.

That pin is a development compatibility statement, not a claim that every
public Compozy binary already supports every Batuta graph action. Stored
Compozy Loop configuration is never mutated: routing and graph state live in
Batuta's immutable journal.
