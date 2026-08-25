---
name: batuta-routing
description: Domain-by-complexity routing contract for the batuta conductor. Read at bootstrap, validated against the live provider catalog, then stored as the per-workspace loop configuration; the stored workspace override is authoritative afterwards.
---

# Batuta Routing Table

Batuta's core opinion: route every task to the cheapest executor that can
handle its domain and risk. A lane is the conjunction `type × complexity`.
The canonical task domains are `backend`, `frontend`, `mobile`, `data`,
`infra`, `security`, `testing`, `docs`, `general`, and `fullstack`.
Complexity is exactly `low`, `medium`, `high`, or `critical`. Reject or
reauthor a task with a noncanonical `type`; do not silently map it to another
domain.

## Complexity semantics inside every domain

| Lane       | Intent                                | Selection rule                                            |
| ---------- | ------------------------------------- | --------------------------------------------------------- |
| `low`      | Contained change, well-trodden paths  | Cheapest coding-capable model in the catalog              |
| `medium`   | New interfaces, moderate coordination | Mid-tier coding model; raise reasoning before raising cost |
| `high`     | New subsystem, heavy reasoning        | Strong coding model, premium tier acceptable              |
| `critical` | Cross-cutting, high regression risk   | The operator's most trusted frontier model                |

## How batuta derives the concrete table (never copy an example)

1. Read provider presence and health, then call
   `compozy__provider_models_list` for exact model IDs and costs. These are
   separate evidence: a provider may be present while authentication is not
   usable, and a catalog model may be unavailable to the current account.
   Never copy credentials, tokens, or raw provider configuration into routing
   artifacts.
2. Map each lane's selection rule onto the catalog using the cost fields
   (`input_per_million` / `output_per_million`) as evidence.
3. Build the useful domain cells first, then add single-axis `type` or
   `complexity` fallback rules only when their behavior is intentional.
4. Model enablement is account-side and may be invisible to the daemon —
   present the redacted evidence and derived table to the operator for
   confirmation before storing it.

### Example only — model IDs must come from the current live catalog

An installation might route `backend/low` to a fast Codex model,
`frontend/medium` to a stronger Cursor model, `infra/high` to a high-reasoning
OpenCode model, and `security/critical` to its most trusted frontier model.
Those names are roles, not identifiers: derive exact provider/model IDs from
the live catalog on every installation.

## Canonical rule shape

This is the exact JSON SHAPE batuta writes with `compozy__loop_configure`
(stored per-workspace override for `implement-tasks`) after deriving the
values from the catalog — the model/provider strings below are illustrative
and MUST be replaced by live IDs. The stored override is what `run-loop`
children resolve at execution — batuta never
sends per-run rules on dispatch, because per-run rules freeze into the run
and are not inherited by `run-loop` children anyway. Rule matching
precedence inside the stored layer:
`id > type + complexity > type > complexity`.

```json runtime_rules
[
  {"match": {"type": "backend",  "complexity": "low"},      "runtime": {"provider": "codex",    "model": "gpt-5.6-luna"}},
  {"match": {"type": "frontend", "complexity": "medium"},   "runtime": {"provider": "cursor",   "model": "catalog-model-id"}},
  {"match": {"type": "infra",    "complexity": "high"},     "runtime": {"provider": "opencode", "model": "opencode/catalog-model-id", "reasoning": "high"}},
  {"match": {"type": "security", "complexity": "critical"}, "runtime": {"provider": "codex",    "model": "gpt-5.6-sol"}}
]
```

## Provider quirks

- Some providers multiplex upstreams and require the model field to carry a
  prefix — e.g. `opencode` only binds `opencode/kimi-k2.5`, never bare
  `kimi-k2.5`. The catalog's exact `model_id` is authoritative; copy it
  verbatim into the rule.
- A model can exist in the catalog and still be disabled for the operator's
  account at the provider (invisible to the daemon). When a lane fails its
  bind with zero tokens, ask the operator what their account enables.

## Escalation and reclassification

- Repeated failure in a lane: write a surgical `id` rule one lane up into
  the STORED override (`compozy__loop_configure` on `implement-tasks`, e.g.
  `{"match":{"id":"task_NN"},"runtime":{...}}` prepended to the rules), then
  re-dispatch `batuta-deliver`. `id` beats `complexity`; remove the rule
  after the task lands. The `id` rule overrides both conjunctive and
  single-axis rules.
- Operator reclassification in conversation ("use luna for this one")
  becomes the same stored `id` rule before the next dispatch.
- The daemon persists `resolved_runtime` with per-field provenance on every
  generation — routing decisions are auditable via `compozy__loop_status`,
  never narrated.
