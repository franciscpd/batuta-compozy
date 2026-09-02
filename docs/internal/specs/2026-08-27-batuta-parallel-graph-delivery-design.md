# Batuta parallel graph delivery and interactive SDD — design

Status: approved on 2026-08-27

## Objective

Make the next Batuta version deliver an approved SDD through dependency-safe
parallel task execution, isolated worktrees, deterministic integration, bounded
automatic recovery, one final integrated review, automatic publication, and
structured user clarification. Documentation, release visuals, and the existing
Portuguese and English personal-site article are release deliverables rather
than follow-up work.

The design stays inside Batuta. It consumes only public Compozy contracts
present in the installed release. Any missing Compozy capability is a blocker
to report to the operator; it does not authorize an issue, branch, edit,
commit, push, rebase, or pull request in the Compozy repository.

## Non-negotiable product decisions

- The Batuta agent owns repository research and SDD authorship. It never edits
  product code.
- The SDD task graph, task bodies, dependencies, domain, and complexity remain
  the execution authority.
- Automatic executor inventory and the immutable `domain x complexity` routing
  generation remain mandatory.
- At most four dependency-independent tasks execute concurrently.
- Every executing task owns a separate managed worktree.
- Each approved task contributes exactly one implementation commit to the
  delivery branch.
- A conflict does not become routine operator work. Batuta reexecutes the
  conflicting task from the latest integrated HEAD within the original budget.
- The complete `review-and-fix` journey runs once, after every task commit has
  been integrated. Task runners still perform focused verification and
  self-review before their own commit.
- Healthy review, push, pull-request opening, and exact remote-HEAD verification
  remain automatic. Merge remains manual.
- There is no routine human gate. Batuta asks only for a material product
  clarification or reports a genuine external blocker.
- No Batuta-specific Compozy migration, stored Loop configuration mutation, or
  new Compozy core contract is part of this design.

## Existing Compozy boundary

Batuta relies on these public Compozy capabilities:

- Loop `fan-out`, `collect`, `route`, `run-loop`, generations, and typed output
  references;
- managed worktree create, inspect, status, exit, and remove surfaces;
- per-action or per-run worktree environments;
- human request nodes with `kind: ask`;
- active-session `compozy__clarify` with one question, at most four choices,
  optional text, and one pending clarification per session;
- child-scoped, non-persistent `run-loop.params.config_overrides` for runtime
  and budget propagation;
- the bundled spec-cycle task import, implementation skills, reviewer, and
  fixer agents;
- worktree publication and forge surfaces already consumed by Batuta.

Using a public capability is not authorization to modify Compozy. Before any
future Compozy mutation, Batuta must stop and present the exact missing
contract, use case, blast radius, and proposed change for explicit approval.
The child `config_overrides` capability remains the external release dependency
already identified by the current migration-free Batuta design; this spec does
not authorize a new or expanded Compozy change.

## Architecture

The delivery keeps one integration worktree and creates disposable task
worktrees around it:

```text
approved SDD
  -> validated task DAG + immutable routing generation
  -> next ready wave (at most four tasks)
  -> one task worktree and one batuta-task child Loop per task
  -> collect verified task commits and clarification outcomes
  -> preflight and integrate commits in canonical order
  -> repeat until every task is integrated
  -> review-and-fix on the integration worktree
  -> publish -> PR -> exact remote-HEAD verification
```

`batuta-deliver` remains the only end-to-end delivery Loop and publication
owner. A new `batuta-task` Loop is a deterministic execution program, not a
planner or a new Batuta agent. It invokes an existing implementation agent with
the exact runtime selected by the pinned routing generation.

The pinned awaited `run-loop` result intentionally exposes only `{loop_run_id,
status}` to its parent. The parent declares that typed output and passes the
authoritative ID to
the Batuta extension, which rereads the completed child and derives exactly one
latest-generation inline `implementation` payload. The closed task completion
schema bounds that payload below the public 16 KiB inline-output limit; a
content-addressed, malformed, wrong-generation, wrong-node, or ambiguous
payload blocks. Batuta never resolves a private output blob. The legacy explicit
candidate input remains only as a closed compatibility form and must equal the
derived authoritative payload.

The Go extension adds the narrow coordination required above the public
worktree and Loop surfaces:

- plan and persist the next dependency-safe wave;
- create or reconcile Batuta-owned task worktrees;
- validate task results and their single implementation commits;
- preflight and apply deterministic integration;
- record questions, answers, attempts, conflicts, and cleanup evidence;
- resolve answered human-request identity from the persisted authoritative child
  run instead of trusting caller-authored request coordinates;
- derive the canonical `{task_id}` ask-context digest server-side, prove one
  answered human request plus one same-generation/item succeeded `ask_operator`
  cell with a valid content-addressed output reference, and resolve a same-child
  continuation from immutable run execution to its newest attempt;
- expose the next disposition to the Loop graph.

These operations extend the existing workspace-owned delivery journal. They do
not create a second database or write Compozy's internal store.

## Interactive SDD definition

Before SDD approval, the Batuta session uses `compozy__clarify` for each
material ambiguity. It must:

1. formulate one bounded question in the operator's language;
2. offer two to four mutually exclusive choices when the decision is closed;
3. put the recommended choice first and explain its impact concisely;
4. accept free text when the fixed choices do not preserve product intent;
5. wait for the selected result before applying it to the SDD;
6. ask the next ambiguity only after the prior clarification settles.

Batuta must not replace a meaningful ambiguity with a guessed default, a plain
chat wall of questions, or a delegated planning agent. Normal explanatory
conversation does not require a clarification card. The existing approvals of
the unified spec and task package remain product-definition boundaries, not
publication gates.

## Task graph and wave selection

The planner validates the complete authored task set before creating a task
worktree:

- every dependency names an existing task;
- the dependency graph is acyclic;
- every task has canonical domain and complexity metadata;
- every task belongs to exactly one routing cell;
- every selected runtime exists in the pinned live inventory and Compozy
  catalog;
- no task is already integrated under contradictory evidence.

A task is ready only when every transitive direct predecessor has an integrated
commit in the delivery journal and that commit is reachable from the current
integration HEAD. The next wave is the first four ready tasks in stable authored
order. Task ID breaks any remaining tie. A task never starts merely because an
executor slot is available.

The task ceiling remains 64, matching the existing bounded task fan-out. A DAG
with no ready task while incomplete tasks remain is blocked with dependency
evidence; Batuta does not infer or delete an edge.

## Worktree lifecycle and ownership

The existing `batuta-<slug>` managed worktree and `batuta/<slug>` branch remain
the integration and publication boundary. Each task attempt receives a
collision-safe managed worktree created from the exact integration HEAD
recorded for its wave. The journal stores the canonical Compozy worktree ID and
root returned by the public surface; names and paths supplied by an agent are
never trusted as identity.

A task worktree is reusable only when workspace, repository, base SHA, branch,
task ID, attempt, setup state, session bindings, and Git state all match the
journal. Otherwise the attempt blocks or receives a new collision-safe
worktree; Batuta never repurposes an unrelated checkout.

Worktree cleanup is evidence-gated:

- integrated task worktrees may be removed after the integrated commit and
  journal transition are both durable;
- superseded conflict worktrees may be removed only after the replacement is
  integrated or their diagnostic evidence is retained;
- blocked, ambiguous, or externally mutated worktrees remain available and
  are reported to the operator;
- cleanup replay is idempotent and never deletes the delivery worktree,
  operator branches, or foreign worktrees.

Parent terminalization is also evidence-gated. `cleanup: cleaned` is the only
successful `stop_when` path. When the graph journal proves a blocked task, an
exhausted budget/execution, or retained cleanup evidence, the closed Batuta
`terminalize` extension operation stores that exact disposition once and
returns a stable action error. The pinned Loop runtime then takes its normal
failed-action terminal path (with reattempts halted), so no later generation
can repeat review or publication; replay has no additional journal mutation.

## Per-task Loop

`batuta-task` receives immutable delivery, task, routing-generation, attempt,
runtime, worktree, budget, and base-HEAD identities. It reads the exact task
file and approved SDD, activates the existing task-execution and final-
verification skills, and runs in the assigned worktree.

The implementation action returns one of two structured outcomes:

- `completed`: focused verification and self-review passed, the task tracking
  evidence is complete, and exactly one implementation commit was created;
- `needs_input`: a product decision is required before a correct implementation
  can finish; no completion commit may exist.

An implementation agent may make technical decisions inside the approved task.
It must use `needs_input` only when alternative answers materially change
product behavior or when an external value cannot be derived safely. Routine
lane, runtime, test, commit, fallback, or publication choices are never
questions for the operator.

## In-delivery human requests

`needs_input` routes that fan-out cell to a native `ask` node. The request
includes the task ID, bounded redacted context, the concrete question, and the
expected answer shape. The request is human-only; the starter and its agent
descendants cannot answer their own delivery.

Only that cell parks. Other independent task cells continue, settle, and may be
integrated. The task worktree remains unchanged while waiting. A valid answer
is recorded in the delivery journal and supplied to the next execution of that
same task on the same owned worktree. Invalid, expired, canceled, duplicated,
or contradictory responses use Compozy's structured request result and never
become guessed implementation input.

The answer value is trusted through the typed dataflow of the executed Loop
definition, `ask_operator.output.answer -> record_answer`. The pinned public
status surface publishes a content-addressed output reference, not the blob
payload, so the Batuta extension validates child ownership, request provenance,
and the unique succeeded ask cell without pretending it can resolve or
byte-compare that blob. This is a Batuta-only authority ruling: `delivery_graph`
is an extension action, not a model-facing tool of the Batuta/code implementer;
a manual operator invocation has workspace authority and is trusted after those
structural checks.

A clarification consumes an execution turn but no work occurs while it is
parked. Each task therefore has at most four execution attempts across child
generations and replacement child runs, including clarification continuations,
runtime fallbacks, and conflict reexecution.

## Deterministic integration

Wave settlement accepts only journal-owned task results. For every completed
task it verifies:

- the worktree is the expected ready Compozy worktree;
- its recorded base equals the wave base;
- exactly one candidate implementation commit is ahead of that base;
- the candidate subject follows `<type>[optional scope][!]: <description>` from Conventional Commits;
- the commit, tree, task identity, and routing attempt match the result;
- no uncommitted product-code mutation remains;
- allowed task-tracking changes are bounded to the approved SDD directory;
- focused verification evidence is present.

Candidates are ordered by the SDD's topological order and then authored order.
Before touching the integration worktree, Batuta applies the ordered candidate
commits in a disposable Git worktree rooted at the expected integration HEAD.
This preflight discovers the maximal conflict-free prefix without changing the
delivery branch.

Batuta then reacquires the workspace journal and integration locks, compares
the actual HEAD and cleanliness with the expected snapshot, and applies only
that proven prefix. The integration operation records its starting HEAD,
accepted commit prefix, first conflicting task when present, final HEAD, and
request digest in the same owned transition. After a process crash, replay may
continue only from an exact reachable prefix; any foreign movement or ambiguous
prefix blocks safely. A prefix is therefore durable progress, never an
unjournaled partial wave.

If a candidate conflicts in preflight, the proven tasks before it become
integrated and the conflicting candidate is not applied. Tasks after the
conflict remain completed candidates but are not integrated until the conflict
is replaced. Batuta creates a new attempt for the conflicting task from the
latest integrated HEAD and reuses the next eligible exact runtime according to
the pinned fallback policy. The obsolete commit remains only on its task branch
until evidence-gated cleanup. A conflict never permits an automatic merge
heuristic, force push, history rewrite, or operator-owned branch mutation.

After each accepted prefix, Batuta copies only the corresponding task-local SDD
completion evidence back to the integration worktree and deterministically
recomputes shared task tracking from the journal. These tracking artifacts are
not additional product implementation commits and never override Git ignore
rules.

## Review and publication

Task-level execution includes focused tests and self-review but does not invoke
the bundled `review-and-fix` Loop. After the journal proves all approved tasks
integrated and the delivery worktree contains the exact integrated HEAD,
`batuta-deliver` invokes `review-and-fix` once on that combined worktree.

This final review owns cross-task defects and remediation. Its result must be
integrated into the reviewed HEAD before publication planning. The existing
publication planner then refreshes Compozy's worktree exit state, pushes at
most once per planned operation, opens or reuses the pull request, and verifies
the exact remote HEAD independently. Merge remains manual.

No task worktree is published independently. The delivery produces one pull
request per approved phase, not one pull request per task or lane.

## Journal and idempotency

The journal keeps the existing stable `delivery_id`, routing generation,
delivery worktree, origin session, task-set digest, and global budget. Parallel
delivery adds an append-only graph projection:

- ordered task nodes and dependency IDs;
- wave number, wave base HEAD, and ordered task IDs;
- per-task attempt, runtime, worktree, state, question and answer identities,
  candidate commit, verification evidence, conflict evidence, and integrated
  commit;
- integration operation ID, request digest, accepted prefix, and final HEAD;
- cleanup operation and retained diagnostic evidence.

Existing sequential journal records remain readable. Missing graph fields mean
legacy sequential delivery; Batuta does not rewrite an old record into a
parallel delivery. New fields are additive and every mutating operation remains
under the workspace process mutex and file lock with `0700` directory and
`0600` file permissions.

Every external action has a deterministic operation ID and canonical request
digest. Reuse with a different digest is a conflict. Replay returns the durable
result or reconciles exact public Compozy/Git evidence before authorizing one
new action.

## Budgets and stop conditions

The parallel design adds throughput, not unbounded work:

- maximum parallel task worktrees: `4`;
- maximum task count: `64`;
- maximum executions per task: `4`, including initial work, clarification
  continuation, runtime fallback, and conflict reexecution;
- maximum fresh parent delivery runs: `4`, preserving migration-free parent
  recovery;
- cumulative delivery token ceiling: `100_000_000`;
- cumulative active-work wall budget: `4h`;
- each pinned fallback candidate may be consumed at most once per task.

Active-work time excludes intervals durably parked on a human clarification.
Tokens, completed attempts, commits, and provider usage never reset while
parked. A resumed task receives only the remaining token and active-wall
allowance. Missing timestamps, overlapping pause intervals, future evidence,
or contradictory usage fail closed.

Batuta starts no new task, wave, fallback, conflict reexecution, review, or
publication operation after the applicable task, token, wall, fallback,
cancellation, or parent-run boundary is exhausted. It reports `blocked`,
`exhausted`, `canceled`, or `stalled` exactly and preserves every worktree,
commit, request, and operation ID needed for diagnosis.

## Failure semantics

- One task failure does not erase successful sibling commits or task evidence.
- An implementation or verification failure advances only that task's exact
  fallback chain.
- A user clarification keeps the cell live; it is not failure or approval.
- A dependency failure prevents dependent admission but does not rerun healthy
  independent tasks.
- A conflict reruns only the conflicting task from the newest integrated HEAD.
- Ambiguous Git, Compozy, journal, remote publication, or identity evidence
  blocks before another side effect.
- Review failure never rolls back integrated implementation commits.
- Publication begins only after one clean integrated review result.

## Documentation, article, and visual deliverables

The Batuta repository release change updates at least:

- README and installation/version requirements;
- architecture and how-it-works guides;
- routing and worktree behavior documentation;
- release notes and changelog;
- operator demo/runbook and QA scenario evidence;
- the architecture image used to explain Batuta inside Compozy.

The visual must show the Batuta agent authoring the SDD, automatic inventory and
routing, dependency-safe parallel worktrees, per-task clarification, canonical
integration, one final review, and automatic push/PR verification. It must not
show a routine human gate or a separate publisher agent.

The personal site at `/home/francisross/Projects/francisross` updates both
`batuta-compozy-journey.pt.mdx` and `batuta-compozy-journey.en.mdx`, their
shared diagrams, and any current Batuta documentation surface that contradicts
the released architecture. The article preserves the historical resource-only
journey while adding a dated evolution to the code-backed Go extension and the
parallel graph. It must not claim a release tag, checksum, public SDK, or real
forge proof before that evidence exists.

## Verification

### Contract and unit proof

- The task DAG rejects missing dependencies, cycles, duplicate tasks, and
  noncanonical metadata before worktree creation.
- Ready-wave selection is stable and never includes a task whose dependencies
  are not integrated.
- No wave exceeds four tasks or the remaining budget.
- Every task gets a distinct journal-owned worktree based on the recorded HEAD.
- SDD clarification uses one bounded `compozy__clarify` request at a time.
- A task `ask` parks only its addressed fan-out cell and resumes with the exact
  winning answer.
- Integration rejects multiple commits, dirty product code, foreign worktrees,
  base drift, or a changed integration HEAD.
- Conflict preflight identifies a deterministic clean prefix; settlement
  journals that exact prefix and schedules only the first conflicting task from
  the resulting newest HEAD.
- Replay before and after every worktree, integration, cleanup, Loop, push, and
  PR boundary is idempotent.

### Integrated proof

A deterministic local scenario contains at least five tasks across backend,
frontend, testing, and docs, with two dependency levels and one intentional
same-file conflict. It proves:

1. four eligible tasks can own distinct worktrees concurrently;
2. a structured question parks one task while independent siblings finish;
3. the selected answer resumes the same task identity and worktree;
4. commits integrate in canonical order;
5. the conflict produces no unjournaled partial integration and only that task
   reexecutes from the new HEAD;
6. dependent work starts only after its prerequisites integrate;
7. completed tasks, commits, and runtimes are never repeated;
8. one final `review-and-fix` runs on the combined worktree;
9. publication verifies the exact reviewed remote HEAD and one PR URL;
10. every temporary process and safely removable task worktree is torn down.

Its task fixture uses the exact `compozy.tasks/v2` `_tasks.md` YAML graph
manifest consumed by the pinned `spec-cycle` extension and Batuta artifact
loader. No parallel JSON manifest or alternative loader is introduced.

The Go race suite, E2E assertions, contract suite from a disposable detached
worktree, extension build/validate/install inventory, and documentation/site
tests must pass. Real-provider and real-forge release claims require separate
disposable smoke evidence.

## Rollout and compatibility

The new graph is the default for newly created deliveries. Existing terminal
or active legacy sequential deliveries retain their recorded semantics and are
never silently converted. If the installed Compozy version lacks any required
public contract, Batuta installation or delivery preflight reports the exact
missing capability and stops; it does not mutate Compozy or fall back to
parallel writers in one worktree.

## Non-goals

- Modifying Compozy without a separate explicit approval.
- Parallel SDD authorship or a child planning agent.
- Concurrent writers in one worktree.
- Heuristic or LLM-authored merge resolution.
- One PR per task or lane.
- Routine human approval of routing, commits, review, fallback, or publication.
- Automatic merge of the final pull request.
- Rewriting historical delivery journals or published Git history.
