---
name: batuta-routing
description: Automatic executor inventory, domain-by-complexity selection, immutable delivery routing, and bounded fresh-run fallback for the Batuta conductor.
---

# Batuta Routing

Batuta routes every approved task through a closed, evidence-backed pipeline:

`inventory → classify → select → apply → dispatch → reconcile`

No operator chooses the routine executor or model. The operator is involved
only when product requirements are ambiguous or an external credential or
capability is genuinely unavailable.

## SDD clarification

During SDD authorship, resolve a material product ambiguity with
`compozy__clarify`, one operator-language question at a time. For a closed
decision, offer two to four mutually exclusive choices, list the recommended
choice first with a concise impact, and accept free text. Wait until the card is
settled before opening another; never guess a default or delegate the decision
to a planner. Loop `ask` is reserved for a running Loop cell, not SDD, normal
explanations, or final spec approval.

## Evidence

Inventory evidence has exactly three resolution states:

`resolved | declared | unknown`

- `resolved` means the fixed probe or authoritative live catalog proved it.
- `declared` means configuration or instructions mention it but execution is
  not proven.
- `unknown` means Batuta could not safely establish it.

For a hard capability, declared and unknown are ineligible. Availability
unknown is ineligible; Batuta never treats uncertainty as a fallback.
Credentials, tokens, raw command output, raw configuration, environment values,
and task bodies never enter the public inventory or routing journal.

The supported executor IDs are `compozy`, `codex`, `opencode`, and
`cursor-agent`. Their configuration informs capability fit, but runtime
provider/model pairs must also exist exactly in the live Compozy catalog;
Compozy remains the execution authority.

## Taxonomy

Task `type` is its routing domain and must be one of:

`backend`, `frontend`, `mobile`, `data`, `infra`, `security`, `testing`,
`docs`, `general`, `fullstack`.

Complexity is exactly `low`, `medium`, `high`, or `critical`:

| Complexity | Minimum posture | Verification |
| --- | --- | --- |
| `low` | standard coding model | focused |
| `medium` | advanced coding model | focused and broad |
| `high` | frontier coding model | full, strict |
| `critical` | frontier model | full and independent |

Explicit valid task metadata is authoritative. LLM proposals must reference
every loaded task ID exactly once, preserve authored dependencies, and provide
confidence of at least `0.70`. Fullstack requires concrete indivisibility and
acceptance-criterion evidence. Invalid coverage or taxonomy requires reauthoring;
low confidence or malformed semantic evidence requires one fresh proposal.

## Selection

Every populated `domain × complexity` cell carries the exact task IDs it owns.
Fit candidates must belong to the inventoried and live-catalog universe. Reject
a candidate when its executor or catalog pair is unavailable, its credential
is missing, its model is hidden, deprecated, or below the complexity floor, or
a hard capability remains unresolved.

Rank eligible candidates deterministically by validated fit, resolved health,
model quality, compatible permission posture, cost, then stable
`executor_id/provider_id/model_id`. Provider and model IDs are copied verbatim
from the live catalog; never normalize provider-specific model prefixes.

Each cell stores one selected runtime and a floor-preserving fallback chain:
low has at most one fallback, medium two, high and critical three. The complete
result is an immutable routing generation containing task-set, inventory,
catalog, workspace, policy, budget, and canonical generation digests.

## Immutable routing generation

Batuta derives one rule per populated cell:

```json
{
  "match": {"type": "frontend", "complexity": "high"},
  "runtime": {"provider": "<live-provider>", "model": "<live-model>", "reasoning": "high"}
}
```

Matching precedence is `id > type + complexity > type > complexity`; later
equal-specificity rules merge per field. Matrix apply always reloads the task
set and inventory, recomputes the immutable routing generation, and accepts it
only when the fresh digest equals the expected digest.

The journal archives that generation together with the trusted worktree,
task-set snapshot, global deadline, token ceiling, and stable `delivery_id`.
It does not write Compozy Loop configuration, replace operator rules, or rely
on a workspace-wide configuration mutation. No plan-only call persists hidden
state.

## Dispatch and recovery

After `apply_matrix`, Batuta calls `start_delivery` with only the returned
stable `delivery_id`. The guarded tool loads the archived generation and starts
attempt 1 as a fresh Compozy parent run with typed ephemeral routing, worktree,
and budget overrides. The run receives the exact applied digest as required
input `routing_generation`, binding it to the archive even after inventory
refresh or extension restart.

On failure, Batuta first calls `reconcile_fallbacks` with the stable delivery
ID and exact terminal parent run ID. The guarded tool validates parent and child
ownership, direct task failures, usage, worktree continuity, and publication
state. If recoverable, `recover_delivery` chooses only the next candidate from
the immutable generation and starts a fresh Compozy parent run in the same
worktree. Completed tasks and literal commits carry forward; the next attempt
imports only incomplete tasks and applies their next exact runtime ephemerally.

Recovery stops at the lowest applicable boundary:

- remaining candidates in the task cell;
- the cell fallback limit;
- the delivery-wide limit of three fallbacks;
- the delivery-wide ceiling of four fresh parent runs;
- original token and wall-clock budget;
- pause or cancellation state.

The extension verifies prior recovery runtimes against the archive before
offering another candidate. Missing provenance, generation mismatch, changed
runtime evidence, ambiguous status, or exhausted budget blocks safely. Batuta
never accepts caller-authored task IDs, node IDs, item indexes, failure evidence,
runtime objects, rules, paths, or owners on the recovery surface.
