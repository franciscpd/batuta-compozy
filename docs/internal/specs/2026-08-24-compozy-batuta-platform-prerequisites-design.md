# Compozy prerequisites for autonomous Batuta delivery — design

Status: approved in conversation on 2026-08-24

## Context

Batuta has two approved functional changes:

1. scoped LLM-controlled publication that commits implementation and review
   fixes, then pushes and opens a verified pull request after one human gate;
2. executor inventory and routing by the matrix `domain × complexity`.

Current Compozy provides almost all required primitives, but two platform
contracts are insufficient for those designs:

- an SDK-built extension can describe supported hook events, but the generated
  manifest cannot preserve a matcher or `required: true`; and
- one task runtime rule may match `id`, `type`, or `complexity`, but validation
  requires exactly one selector, so it cannot express `frontend/high` as one
  durable rule.

These are generic Compozy capabilities. Batuta must not emulate either
contract with unrestricted shell access, mutable user configuration, composite
task-type strings, or operator cleanup.

## Decision

Implement two independent, compatibility-safe Compozy changes and release
them together as the Batuta platform floor:

1. code-first hook declarations preserve the security-relevant declaration
   fields needed for fail-closed extension hooks;
2. runtime rules accept a conjunctive `type + complexity` selector with an
   explicit specificity order.

The changes receive separate implementation plans and tests. Batuta raises its
operational minimum only after both contracts exist in a released Compozy
prerelease.

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
name: batuta-publisher-tool-guard
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
7. `make codegen-check`, focused Go/TypeScript tests, `make gate`, and
   `make gate-full` pass with `TMPDIR` and `GOTMPDIR` unset as required by the
   repository test contract.

## Rollout

The two changes land as separately reviewable commits in an isolated Compozy
worktree. They may share one release, but neither Batuta feature claims
compatibility until both are present in a published prerelease and the Batuta
runtime guard recognizes its exact identity.

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
