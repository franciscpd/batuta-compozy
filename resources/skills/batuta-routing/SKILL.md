---
name: batuta-routing
description: Automatic executor inventory, domain-by-complexity selection, immutable delivery routing, and bounded fresh-run fallback for the Batuta conductor.
---

# Batuta Routing

Batuta routes every approved task through a closed, evidence-backed pipeline:

`inventory → classify → select → align → bootstrap → apply → dispatch → reconcile`

Batuta derives the executor/model proposal automatically. The operator confirms
the exact derived matrix before delivery mutation, and is otherwise involved
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

Compozy is the only provider/model execution authority. New fit candidates use
`executor_id: compozy` plus an exact `provider_id + model_id` pair from the live
Compozy catalog. The legacy closed executor IDs `codex`, `opencode`, and `cursor-agent`
remain accepted for archived request compatibility but are ignored by fit
identity. Never submit `enrichment_ids`; the extension derives them.

Claude Code and Agy are optional enrichers, not execution backends. A missing
enricher cannot exclude a live pair. Resolved enrichment may prove capability
fit, while declared or unknown evidence cannot prove a hard requirement.
Unknown provider auth is degraded, not absent; explicit `missing_cli`,
`missing_credential`, `needs_login`, and `permission_denied` states are
ineligible. Agy never rewrites a runtime ID and Batuta never calls `agy models`
automatically because it is an authenticated network fetch. Agy-backed models
remain generic live catalog pairs owned by Compozy.

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
`provider_id/model_id`. Provider and model IDs are copied verbatim
from the live catalog; never normalize provider-specific model prefixes.

Each cell stores one selected runtime and a floor-preserving fallback chain:
low has at most one fallback, medium two, high and critical three. The complete
result is an immutable routing generation containing task-set, inventory,
catalog, workspace, policy, budget, and canonical generation digests.

A successful `routing_plan` result is the only authority for a generation.
Copy its returned generation digest verbatim with the byte-equivalent request.
Never construct, hash, infer, or reuse a digest from inventory, rejected
output, another request, or another session. A `routing_fit_retryable` result
permits one corrected proposal containing only the remaining exact live
candidates. A second routing rejection is terminal: report its exact reason
codes and make zero `routing_apply` calls or delivery mutations.

## Operator alignment and repository bootstrap

After planning, call `routing_apply` operation `alignment_status` with the
byte-equivalent request and generation digest. Present one row per populated
cell with task IDs, exact provider/model/reasoning/tier, ordered fallbacks, and
a cost column. The generation has no authoritative monetary cost snapshot, so
the cost is always displayed as `unknown`; it is display-only and excluded
from the durable task/selected/fallback projection. Never infer it. Use a
typed `compozy__clarify` choice so the operator can approve the exact proposal
or request adjusted requirements. On approval, call `confirm_alignment`.

Confirmation is durable only for the identical selected and fallback
projection. Replay remains confirmed; a changed cell invalidates the record and
must be presented again. `apply_matrix` refuses an unconfirmed generation.
This alignment is not an implementation, review, or publication human gate.

At any Batuta tool boundary, `tool_backend_failed` with
`backend_unhealthy` permits one `compozy__tool_info` read for that exact tool.
Then stop and report the blocker with the tool ID, operation, reason codes, and
last successful routing state. Never call extension reload, install, remove,
validate, or logs, never run `doctor`, and never inspect daemon or extension
process environments. Runtime repair is outside routing and remains an
operator responsibility.

Before worktree creation, call operation `bootstrap_repository` with that
confirmed generation. It uses the trusted workspace root and returns
`already_initialized` for a valid existing HEAD. For a new repository it
respects `.gitignore`, rejects unignored sensitive material with
`blocked_sensitive_paths`, initializes `main`, and creates exactly one commit:
`chore: initialize workspace`. Batuta never runs Git directly. A blocked new
repository removes only the `.git` directory created by this operation and
does not change project files. An existing HEAD-less repository is accepted
only when its current branch is already `main`.

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
