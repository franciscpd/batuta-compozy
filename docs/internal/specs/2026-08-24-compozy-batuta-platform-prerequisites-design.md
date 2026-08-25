# Compozy prerequisites for autonomous Batuta delivery — design

Status: approved in conversation on 2026-08-24

## Context

Batuta has two approved functional changes:

1. scoped LLM-controlled publication that commits implementation and review
   fixes, then pushes and opens a verified pull request after one human gate;
2. executor inventory and routing by the matrix `domain × complexity`.

Current Compozy provides most required primitives, but six platform contracts
are insufficient for those designs:

- an SDK-built extension can describe supported hook events, but the generated
  manifest cannot preserve a matcher or `required: true`; and
- one task runtime rule may match `id`, `type`, or `complexity`, but validation
  requires exactly one selector, so it cannot express `frontend/high` as one
  durable rule;
- a code-first extension cannot override the SDK's historical minimum daemon
  version in its generated manifest;
- Loop config has a structured GET internally, but no CLI read surface or
  revisioned compare-and-swap replacement; and
- a settled parent cannot autonomously reopen one failed nested child item
  under the original lineage budget with an ephemeral runtime; and
- the spec-cycle implementer receives task complexity but has no closed,
  testable verification-depth contract keyed by that value.

These are generic Compozy capabilities. Batuta must not emulate these
contracts with unrestricted shell access, mutable user configuration, composite
task-type strings, or operator cleanup.

## Decision

Implement independent, compatibility-safe Compozy changes and release them
together as the Batuta platform floor:

1. code-first hook declarations preserve the security-relevant declaration
   fields needed for fail-closed extension hooks;
2. runtime rules accept a conjunctive `type + complexity` selector with an
   explicit specificity order;
3. code-first definitions can declare an extension-specific minimum daemon;
4. Loop config supports read-only revision snapshots and optional CAS writes;
5. nested recovery preserves lineage, carried work, and budgets;
6. spec-cycle execution applies a closed complexity verification policy.

The changes receive reviewed implementation plans and tests. Batuta raises its
operational minimum only after every contract exists in a released Compozy
prerelease.

The last four contracts were found during executable-plan review. They do not
change Batuta's product scope; they make the approved publication and routing
semantics executable without destructive reads, stale writes, fresh budgets,
or handwritten generated manifests.

## Extension-specific minimum daemon version

`ExtensionDefinition` accepts an optional string `min_compozy_version`. The SDK
trims surrounding whitespace before testing presence and validity. When the
normalized value is empty, it retains its historical default for compatibility.
When present, SDK describe preserves that normalized valid SemVer value into
`sdk.min_compozy_version`, and the generated manifest/install compatibility
check uses it. A non-empty invalid SemVer value fails validation.
This lets Batuta's generated immutable bundle carry its actual platform floor;
a shell guard that is absent from the published bundle is insufficient.

## Revisioned Loop configuration

A public Loop config read returns the stored override, effective config, and a
canonical monotonic `int64` `config_revision` without mutation. Revision `0`
means no stored override; clients otherwise treat the value as an opaque
concurrency token. The CLI exposes this as
an explicit read-only command; the current `loop configure --file {}` uses the
write/auth path and can create an empty row, so it is never documented as a
read even though omitted patch fields preserve an existing row.

Loop config mutation accepts an optional `expected_revision`. When present,
the store compares it atomically with the current stored revision and either
commits the validated patch or returns a typed conflict without mutation.
Legacy callers that omit it retain existing patch semantics.
The revision changes whenever the stored override changes and has one stable
sentinel for an absent override. It contains no configuration or credential
material.

## Same-lineage nested recovery

Compozy exposes a durable recovery operation for a settled parent delivery run
that owns a failed nested `run-loop` child item. Given the trusted workspace,
opaque parent run ID, an idempotency key, and a validated ephemeral exact-item
runtime, the daemon itself resolves the parent/child/node/item lineage. It
rejects caller-selected foreign nodes or task metadata, reopens only the failed
child item and transitive dependents, carries successful siblings, and resumes
the owning parent after the child settles.

Compozy also exposes the closed Loop `reattempt_strategy` value `halt`. When a
generation settles as failed under this strategy, the coordinator terminalizes
the run as `failed` without quarantine or another generation. Existing Loops
retain `failed_only` as the default, and `full_body` is unchanged. This gives
recovery an explicit, opt-in way to preserve a naturally failed item and its
successful siblings as a settled recoverable lineage; it is not an implicit
change to retry behavior.

The recovery generation inherits the original pinned definition, task set,
token accounting, wall-clock deadline, and iteration ceiling. The exact runtime
applies only to that recovery generation and never enters stored workspace
rules. Status exposes the recovery operation ID and resolved-runtime snapshot
needed for Batuta to reconcile its journal. Cancellation, conflict, exhausted
budgets, missing ownership, or an unsettled lineage fail with typed evidence
and never start a fresh full-budget delivery.

## Complexity-aware task verification

The spec-cycle importer already carries authored `complexity`. The
`implement-tasks` prompt exposes that exact value to `code_implementer`, and
`code_implementer` plus `cy-execute-task` apply this closed minimum policy:

| Complexity | Minimum verification depth | Review posture |
| --- | --- | --- |
| `low` | focused tests and formatting/lint for every changed surface | required self-review |
| `medium` | `low` plus the owning package/suite and applicable static analysis | self-review plus contract parity |
| `high` | `medium` plus applicable race/integration coverage and cross-surface regression checks | independent review required |
| `critical` | `high` plus the repository's affected system/gate | independent review plus final contract-parity review required |

Repository instructions and the approved task resolve concrete commands; the
agent cannot skip an applicable row merely because the task omitted a command.
An inapplicable check is reported with bounded evidence, not silently treated
as pass. Existing `review-and-fix` remains the independent review executor;
independence means its reviewer runs in a new isolated session with no
implementation-session history. Its prompt receives the exact complexity of
the named review target. A single-task caller supplies that task's complexity;
the Batuta parent names the whole slug as its delivery-review target and passes
the deterministic highest authored complexity returned by task import. This
policy does not invent a per-cell daemon retry field or a model tier.

## Code-first required hook declarations

### Public SDK contract

The existing code-first hook declaration surface is extended without removing
the legacy event-only form. A described hook may carry:

- `name`;
- `event`;
- optional `profile`;
- `mode`;
- `matcher`;
- `required`.

An event-only declaration remains valid and keeps its current defaults:
generated name from the event, sync mode for sync-eligible events, async mode
otherwise, empty matcher, and `required: false`.

Batuta declares one synchronous `tool.pre_call` hook with:

```text
name: batuta-publisher-guard
event: tool.pre_call
mode: sync
matcher.agent_name: batuta-publisher
required: true
```

The manifest builder must preserve those values exactly. It still derives the
subprocess executor command, arguments, and environment from the described
extension subprocess; callers cannot replace the executor through the
code-first declaration.

### Validation

Build-time validation rejects:

- an unknown hook event;
- an explicit mode not supported by the event;
- `required: true` on a non-synchronous declaration;
- matcher fields invalid for the selected event;
- duplicate hook identities within the same profile;
- an explicitly supplied name that normalizes to empty or violates the
  existing hook-name contract.

SDK normalization trims strings, deep-copies matcher values, sorts
deterministically, and does not mutate caller-owned declarations. The
initialization handshake continues to advertise only the deduplicated event
names supported by the runtime.

### Fail-closed runtime invariant

This change does not introduce a second hook execution path. Generated hooks
enter the existing normalized hook registry and pipeline. A required
synchronous hook error, timeout, malformed response, unavailable subprocess,
or explicit deny must prevent the intercepted tool call from executing.

Acceptance includes an integration test starting from an SDK describe payload,
building and loading the generated manifest, matching an agent-scoped
`tool.pre_call`, and proving a failing required subprocess hook blocks the tool
invocation. A manifest-shape assertion alone is insufficient.

## Conjunctive task runtime rules

### Selector grammar

`RuntimeMatch` keeps the existing fields `id`, `type`, and `complexity`.
Valid selector shapes become:

```text
id
type
complexity
type + complexity
```

`id` remains exclusive because it already names one concrete imported item.
Empty selectors, `id + type`, `id + complexity`, and all three fields together
remain invalid.

The conjunction matches only when both normalized values equal the imported
task metadata. It is not an OR expression.

### Specificity and field resolution

The specificity order is:

```text
id > type + complexity > type > complexity
```

Existing layer precedence and per-field merging do not change. Within one
specificity, a later matching rule continues to win per non-empty runtime
field. Existing single-selector rules therefore retain their current behavior.

For example:

```json
{
  "match": {"type": "frontend", "complexity": "high"},
  "runtime": {
    "provider": "cursor",
    "model": "exact-live-model-id",
    "reasoning": "high"
  }
}
```

beats separate `type: frontend` and `complexity: high` rules for the fields it
sets, while an exact `id` override still beats the matrix rule.

### Compatibility

No stored configuration migration is required: existing JSON, YAML, TOML, and
API payloads already use the same fields and remain valid. Public API and
native-tool schemas must describe the newly valid conjunction and the exact
precedence. CLI support for authoring JSON/TOML rules remains sufficient; no
new compact flag syntax is required for this delivery.

## Verification

The Compozy implementation is accepted only when:

1. Go and TypeScript SDK generated contracts contain the complete described
   hook declaration fields and code generation is clean.
2. Go and TypeScript normalization tests prove deterministic preservation and
   legacy event-only compatibility.
3. Manifest build tests prove matcher, mode, name, profile, and `required`
   survive code-first generation.
4. Runtime integration proves a required generated hook failure prevents the
   matched tool call and does not affect a non-matching agent.
5. Runtime-rule unit tests cover every valid and invalid selector shape,
   conjunction truth tables, the new specificity order, later-rule behavior,
   and per-field provenance.
6. API/native-tool and config round trips accept `type + complexity` without
   changing legacy single-selector output.
7. SDK describe/build/reload tests preserve an explicit minimum daemon and
   retain the historical default when the field is omitted.
8. CLI/API/store integration proves a read causes zero mutation, stale
   revisions conflict atomically, absent-config revisions are stable, and
   legacy unconditional PUT remains compatible.
9. Recovery integration proves one failed nested item and dependents rerun,
   successful siblings carry, parent continuation resumes, the ephemeral
   runtime appears in resolved provenance, original budgets remain monotonic,
   and replayed idempotency keys do not duplicate work.
10. Spec-cycle contract tests cover all four complexity rows, unknown/missing
    values, applicable/inapplicable evidence, and high/critical independent
    review posture.
11. `make codegen-check`, focused Go/TypeScript tests, `make gate`, and
   `make gate-full` pass with `TMPDIR` and `GOTMPDIR` unset as required by the
   repository test contract.

## Rollout

The changes land as separately reviewable commits in an isolated Compozy
worktree. They may share one release, but neither Batuta feature claims
compatibility until all required contracts are present in a published
prerelease and the Batuta generated manifest/runtime guard recognizes its exact
identity.

The dirty primary Compozy checkout is never used for implementation. Existing
unrelated edits there remain untouched.

## Rejected alternatives

- **Generate an unmatched, non-required publisher hook:** fails open and does
  not contain the publisher agent.
- **Install a mutable global hook as a Batuta bootstrap step:** makes security
  depend on user configuration and leaves cleanup/recovery state behind.
- **Encode `frontend-high` in task `type`:** destroys the approved independent
  domain and complexity axes.
- **Materialize only temporary task-ID rules:** task IDs can repeat across
  slugs and crash recovery would have to clean workspace-global routing state.
- **Let `type` choose provider while `complexity` chooses one global model:**
  model IDs and tiers are provider-specific, so the merged pair may be invalid
  or silently weaker than the required lane.
