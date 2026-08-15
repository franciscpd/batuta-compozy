# Batuta spec-cycle migration — design

Design approved in conversation on 2026-08-15. This change migrates Batuta's
active delivery contract from the removed `dev-cycle` 0.3.x surface to the
bundled `spec-cycle` 0.4.0 surface shipped by current CompozyOS.

## Problem

Current Batuta resources still require `cy-create-prd`,
`cy-create-techspec`, and `ext__dev_cycle__import_tasks`. Current CompozyOS
publishes `cy-create-spec`, `cy-create-tasks`, and
`ext__spec_cycle__import_tasks` instead. The old extension remains installed in
the operator environment but is unhealthy, and Batuta's task preflight is
therefore registered but unavailable.

This is a package compatibility failure. The CompozyOS extension tool ID is
derived from the extension name and handler, so renaming `dev-cycle` to
`spec-cycle` necessarily changed the exact tool ID. The current Loop DSL has no
semantic alias or capability indirection that Batuta can use instead.

## Goal

Batuta uses the canonical `spec-cycle` 0.4.0 product and tool contracts while
preserving its existing role as a non-coding conductor:

- requirements are clarified in the originating Batuta session;
- one unified spec corpus is approved before task generation;
- tasks remain the unit of routing, dispatch, implementation, and review;
- executable requirements remain byte-for-byte literal;
- the existing preference gate, routing confirmation, asynchronous dispatch,
  terminal return, and no-push boundaries remain unchanged.

## Unified PM contract

Batuta invokes `cy-create-spec` for every delivery, including small and
unambiguous requests. A simple request may use a short grill, but must not skip
the grill or the unified spec.

The approved corpus is:

- `_spec.md`, containing the product and technical parts;
- `_user_stories.md`, containing the canonical behavior catalog;
- `_dx.md`, containing the developer-facing surface contract;
- `_tests.md`, containing the canonical test contract;
- `_uiux.md` only when the request changes a Web surface.

After the corpus is approved, Batuta invokes `cy-create-tasks`. It reviews task
type and complexity assignments with the operator because those authored
values drive routing. Batuta does not recreate the removed PRD/TechSpec split
and does not manufacture compatibility files named `_prd.md` or
`_techspec.md`.

The feature slug remains the stable workspace artifact key under
`.compozy/tasks/<slug>/` and the input that connects spec artifacts, imported
tasks, and the delivery Loop. It is not treated as a CompozyOS session, task
run, or Loop run ID; those runtime objects continue to use their own opaque
identities.

## Delivery contract

The direct task preflight and the `batuta-deliver` graph both use the exact
current tool ID `ext__spec_cycle__import_tasks` with the existing
`.compozy/tasks/<slug>/task_*.md` pattern.

Batuta fails closed when `spec-cycle` is missing, inactive, unhealthy, or the
task set is empty. It reports the structured error and does not fall back to
the stale `dev-cycle` extension. Dry-run remains insufficient evidence that
tasks exist because it plans the graph without executing `import_tasks`.

The child Loops remain `implement-tasks` followed by `review-and-fix`.
`batuta-deliver` continues to submit one durable parent run and return through
its existing terminal session-prompt effect.

## Compatibility guard

Batuta adds the exact trusted current CompozyOS identity:

- commit `a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c`;
- version `v0.3.0-beta.16-9-ga35eda6d`.

The guard continues to require an exact known commit/version pair. Short
prefixes, altered commit counts, arbitrary descendants, and lookalike version
strings remain rejected. Existing trusted identities remain accepted so the
package's compatibility history does not change implicitly.

## Active surfaces

The implementation updates only current, executable Batuta surfaces:

- `agents/batuta/AGENT.md` for the unified PM sequence and direct task
  preflight;
- `loops/batuta-deliver/loop.yaml` for the current import action kind;
- `README.md` and `README.pt-BR.md` for prerequisites and the operator flow;
- `extension.toml` where its active description names the retired product;
- runtime-guard, package, Loop, and missing-task contracts affected by those
  changes.

Executed historical design and plan documents retain the product names and
commands that were true for their original work. They are not mechanically
rewritten as if the current migration had already existed.

## Alternatives

### Hard-cut Batuta to spec-cycle — selected

Use the one product and exact tool IDs shipped by current CompozyOS. This keeps
the authored agent, Loop graph, package documentation, and runtime behavior on
one observable contract.

### Support dev-cycle and spec-cycle simultaneously — rejected

Loop action kinds are exact tool IDs and cannot select a handler dynamically.
A dual contract would either keep a known-dead dependency or require two Loop
resources and runtime branching that exists only to preserve a removed
surface.

### Discover a semantic import capability dynamically — deferred upstream

CompozyOS currently exposes handler ToolIDs, not a stable capability alias for
extension-provided actions. Adding such indirection is a separate platform
feature and is not required to restore Batuta compatibility now.

## Verification

Development follows red/green tests against the active current daemon:

1. Extend the runtime-guard contract with the exact accepted current identity
   and adversarial prefix, count, hash, and version cases; demonstrate RED
   before changing the allowlist.
2. Demonstrate the existing missing-task contract fails through the retired
   `dev-cycle` ToolID, then change it and the production resources to
   `spec-cycle` and require the existing deterministic
   `tool_invalid_input`/`dependency_missing` outcome.
3. Run syntax, authored-agent boundary, Loop validation, staging, package-lock,
   package, inventory, and complete Batuta contract suites.
4. Stage and publish the candidate package only in a fresh isolated QA lab
   using the current CompozyOS binary. Verify `spec-cycle` is healthy, Batuta
   exposes exactly its three resources, the unified PM artifacts exist, task
   import succeeds, dry-run resolves the graph, real delivery returns, and
   teardown records `clean: true` with no survivors.
5. Perform the operator's visual smoke before republishing Batuta in the user
   environment.

No test may gain a compatibility shim, skipped assertion, source-text-only
substitute for runtime behavior, or fallback to `dev-cycle`.

## Rollout

After verification, republish only the Batuta extension package and confirm it
is active and healthy with exactly one agent, one routing skill, and one Loop.
The already-promoted CompozyOS binary does not change for this migration.

If isolated or visual verification fails, do not republish. Preserve the exact
structured failure and reopen the red/green cycle at its owning boundary.

## Non-goals

- No CompozyOS source change or new extension alias mechanism.
- No removal of the operator's stale `dev-cycle` installation.
- No session-lineage, child-session grouping, or run-owned session cleanup
  change.
- No provider/model fallback, quota escalation, price catalog, or routing
  redesign.
- No change to EventSource lifetime or terminal-effect delivery.
- No direct coding, approval, commit, or push by the Batuta conductor.

Session nesting and deterministic run-owned session finalization are the next
generic CompozyOS investigations after this migration is verified. Provider
fallback, quota/auth/config classification, pricing freshness, and routing
policy remain later Batuta/platform work and must not be mixed into this
compatibility change.
