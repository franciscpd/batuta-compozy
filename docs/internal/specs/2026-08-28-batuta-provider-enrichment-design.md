# Batuta provider-authoritative routing with CLI enrichment — design

Status: implemented and deterministically verified on 2026-08-28; live
provider execution remains blocked-verify

## Context

Batuta currently models Compozy, Codex, OpenCode, and Cursor Agent as peer
`executor_id` values. Its inventory adapters inspect those CLIs and routing
admits a provider/model pair only when an adapter reports matching model
evidence.

That coupling is stricter than the actual execution boundary. `batuta-task`
always executes through Compozy's `run-agent` action with a typed
`runtime.provider` and `runtime.model`. Compozy, rather than a Batuta adapter,
owns provider configuration, authentication, model availability, and process
launch. A provider that is live in Compozy can therefore be executable even
when Batuta has no dedicated CLI adapter.

The current model creates two undesirable outcomes:

- a live provider such as Claude or Agy can be excluded only because Batuta
  has no corresponding enum member or adapter; and
- a CLI's model listing can accidentally act as execution authority even when
  it disagrees with Compozy's effective catalog.

At the same time, direct CLI inspection remains useful. A bounded adapter can
prove MCPs, plugins, agents, skills, repository instructions, permission
posture, health, and version details that make Batuta's routing recommendation
more assertive than a provider/model list alone.

## Decision

Use a hybrid authority model:

```text
Compozy live catalog             optional CLI enrichers
provider/model candidate  <---  capabilities, health, version, diagnostics
          |
          v
 deterministic Batuta policy + validated semantic fit
          |
          v
 Compozy run-agent runtime {provider, model, reasoning, speed}
```

The live Compozy provider/model catalog is the sole authority for candidate
existence and exact runtime identifiers. Direct CLI adapters are optional
enrichers. They can improve or reduce confidence, satisfy proven capability
requirements, and affect ranking, but they cannot create a runtime candidate,
remove a live candidate merely by being absent, or override Compozy's
availability/authentication state.

This is a Batuta-only change. It requires no Compozy code, API change,
configuration mutation, database migration, or persisted-state migration.

## Runtime and evidence identities

The canonical routing identity is:

```text
provider_id + model_id
```

`executor_id` is retained on the current beta wire and routing-generation
structures for compatibility, but every newly generated runtime binding uses
`compozy`: Compozy is the execution owner. A new ordered
`enrichment_ids,omitempty` field records the optional evidence sources that contributed
to the decision, for example `codex`, `cursor-agent`, `opencode`, `claude`, or
`agy`.

Fit recommendations retain required legacy `executor_id` on the current beta
wire. New Batuta prompts emit `compozy`; the service accepts the existing
closed enum for replay/rolling compatibility but ignores it for admission,
uniqueness, and ranking. Semantic identity is keyed by exact
`provider_id + model_id`, not by an adapter. This prevents the same executable
runtime from appearing as several competing candidates merely because several
local CLIs can describe it.

Existing archived routing generations remain readable. They already contain
the exact provider/model runtime that recovery needs. Batuta does not rewrite
or reinterpret an in-flight generation; the new binding semantics apply only
when planning a new generation. The routing-generation schema remains version
1: `enrichment_ids` is additive and uses `omitempty`, so unmarshalling and
re-marshalling an old generation preserves its exact digest. The ownership
journal schema and decoder remain unchanged.

## Inventory contract

Keep the public `executor_inventory` tool and the existing redaction and
bounded-probe guarantees. The snapshot contains two roles:

1. the `compozy` record supplies the authoritative live provider/model catalog
   and its generation digest; and
2. zero or more CLI records supply enrichment evidence.

The snapshot schema remains version 1. `provider_bindings` is an additive
`omitempty` field, and snapshots are not persisted. Every CLI record can add
normalized typed provider bindings:

```go
type ProviderBinding struct {
    ProviderID string `json:"provider_id"`
    ModelID    string `json:"model_id,omitempty"`
}
```

A binding may contain:

- an exact provider ID explicitly owned by that CLI contract, such as
  `claude`, `agy`, `codex`, or `cursor`; or
- exact provider/model identifiers returned by a safely parsed CLI inventory,
  as OpenCode can do.

Provider bindings only associate enrichment with an existing Compozy catalog
entry. They are never promoted to live runtime identifiers on their own.
The public redactor explicitly permits only the three structural keys
`provider_bindings`, `provider_id`, and `model_id` for this projection; their
values still pass safe-identifier validation and secret-canary tests. All
other unknown/raw adapter keys continue to be dropped.

An adapter state is one of:

- `resolved`: the bounded probe produced safely parsed evidence;
- `declared`: a documented local source declares the value but the runtime did
  not resolve it; or
- `unknown`: the executable, command, field, or safe parser is unavailable.

Missing or failed optional adapters yield diagnostics and unknown enrichment.
They do not fail the whole inventory while the Compozy catalog remains valid.
If the Compozy catalog itself is missing, stale, malformed, or has no live
models, planning fails closed exactly as it does today.

The Compozy record also normalizes provider auth state without copying status
messages, commands, login metadata, or raw payload. `configured`,
`authenticated`, and `none` map to ready; `missing_cli`, `missing_credential`, `needs_login`, and
`permission_denied` are ineligible; `rate_limited`, `transient`, and `unknown`
remain explicit unknown/degraded evidence. Any unrecognized future state maps
to unknown/degraded rather than being guessed ready or missing. Adapter
credential state cannot override this authoritative provider state.

Unknown/degraded auth is distinct from unknown capability evidence: a live
model with unknown auth may remain a lower-confidence candidate because
Compozy reported it live, while unknown capability evidence never proves a
hard requirement.

## Enricher registry

Extend `ExecutorSnapshot` with
`ProviderBindings []ProviderBinding \`json:"provider_bindings,omitempty"\``.
The adapter interface itself does not need another lifecycle method: each
adapter normalizer emits its safe typed associations together with the rest of
its bounded evidence.

The association is constructor- and parser-owned, not caller input. An exact
model binding can only come from safely parsed output; a provider-only binding
can only come from the adapter's reviewed constructor. Probe ownership remains
closed through `ProbeSpec.Executor`; adding an adapter therefore still
requires code and fixture review, while adding a provider to Compozy does not
require a Batuta enum change.

The core adapter seam remains:

```go
type Adapter interface {
    ID() inventory.ExecutorID
    StaticSpecs() []inventory.ProbeSpec
    DynamicSpecs(map[inventory.ProbeID][]byte) ([]inventory.ProbeSpec, error)
    Normalize(map[inventory.ProbeID][]byte) inventory.ExecutorSnapshot
    Missing() inventory.ExecutorSnapshot
}
```

The initial registry is:

| Adapter | Provider association | Safe probes |
| --- | --- | --- |
| Compozy | execution authority for all live pairs | existing version, status, config, agent, provider, model, skill, toolset and tool surfaces |
| Codex | `codex`, plus exact safely parsed model IDs | existing version, doctor, MCP, plugin, marketplace and bundled-model surfaces |
| Cursor Agent | `cursor` | existing version, status, model and MCP surfaces |
| OpenCode | only exact safely parsed provider/model pairs | existing version, resolved config/path, agent, skill, MCP, auth and model surfaces |
| Claude Code | `claude` | `--version`, `plugin list --json` |
| Agy | no runtime binding; local CLI capability evidence only | `--version`, `agent`, `plugin list` |

Claude and Agy commands run only when their absolute executable is discovered.
They use the existing no-shell runner, workspace directory, global probe
budget, per-probe timeout, bounded stdout/stderr, safe argument allowlist,
redaction, record caps, and diagnostics. No adapter logs in, installs,
refreshes, executes an agent turn, changes configuration, or reads credentials.

Agy is the local CLI evidence source for the former Gemini CLI lineage, but it
does not rename Compozy's runtime or emit a provider/model binding. Agy 1.1.14
implements `agy models` as an authenticated network fetch, so that command is
deliberately excluded from automatic inventory. Gemini, Claude, and other
Agy-backed models still enter the candidate universe through Compozy's live
catalog. A custom Compozy provider that launches Agy also works generically;
Batuta does not guess that association.

The chosen Claude probe set intentionally excludes `claude mcp list`: Claude
Code documents that the command health-checks approved MCP servers, which can
spawn configured stdio processes or open network connections. Agy 1.1.14
documents bare `agy agent` as “List available agents”; the implementation must
lock that read-only shape with fixtures and stop if a supported CLI changes its
meaning.

## Candidate construction

`BuildCandidateBindings` performs these deterministic steps:

1. validate the inventory digest and exact Compozy catalog generation;
2. create exactly one candidate for each non-hidden, non-deprecated,
   live-available Compozy provider/model pair (`available_live` or the current
   compatible `available` wire value) whose authoritative provider auth state
   is not ineligible;
3. set its execution owner to `compozy`;
4. attach only matching resolved/declared adapter evidence as ordered
   enrichment IDs;
5. compute permission and capability scores from the authoritative Compozy
   evidence plus attached enrichments; and
6. canonicalize and deduplicate by `provider_id + model_id`.

An adapter's model list may prove a precise association or expose catalog skew,
but it cannot add a pair absent from Compozy. Conversely, a catalog pair with
no adapter still becomes one generic candidate.

## Eligibility and ranking

Hard capability requirements use the union of applicable Compozy evidence and
attached enrichment evidence. A requirement is satisfied only by `resolved`
evidence or by an exact successful bounded capability probe. `declared`
evidence can improve ranking but cannot satisfy a security-sensitive hard
requirement. `unknown` never becomes proof.

Adapter absence is not itself a hard requirement failure. A task may require a
capability already represented by the closed routing taxonomy, and attached
resolved enrichment can prove it. Claude plugin and Agy agent identifiers are
currently ranking/diagnostic evidence only; making either a hard requirement
would first require an explicit, versioned capability-kind policy change.

Model strength remains deterministic. The default policy continues to use
exact versioned provider/model tier entries for medium, high, and critical
complexity. A live model without a known tier receives the conservative `standard`
tier, making it eligible for low-complexity tasks but never silently promoting
it above a known floor. Adding Claude or Agy model-family tiers is a policy-data
change backed by exact catalog fixtures, not an adapter-side inference.

Stable ranking order is:

1. all hard requirements proven;
2. complexity model floor;
3. validated semantic-fit score;
4. resolved relevant enrichment count and health;
5. exact model tier;
6. permission posture;
7. cost score; and
8. exact provider/model lexical tie-break.

The selected and rejected routing evidence records the provider/model pair,
execution owner, ordered enrichment IDs, evidence digests, and rejection
codes. It never records raw CLI output.

## Claude and Agy support

Support has two independent levels:

- **generic execution support:** any Claude or Gemini/Agy-backed provider/model
  reported `available_live` by Compozy is a candidate without a dedicated
  adapter; and
- **enriched routing support:** when the matching CLI is installed, Batuta
  collects its bounded evidence and can make a more confident capability fit.

This distinction keeps Batuta aligned with future Compozy providers. New
providers work generically on day one, while focused enrichers can be added
when their CLI exposes stable, safe, decision-useful surfaces.

## Security and privacy

The existing secret boundary remains mandatory:

- executable paths are discovered by Batuta and converted to absolute paths;
- callers cannot submit a command, path, provider binding, environment value,
  or config location;
- probes never use a shell;
- all command shapes are constructor-owned and tested exactly;
- raw stdout/stderr is bounded and cannot enter errors, logs, journals, task
  prompts, or public tool output;
- tokens, headers, environment values, credential content, prompts, and task
  bodies are rejected by redaction canaries; and
- a malformed or version-skewed enricher degrades only its own evidence.

## Documentation and rollout

Update the Batuta agent, routing skill, English/Portuguese README, architecture,
how-it-works, verification guide, and contract assertions to explain:

- Compozy owns execution and provider/model truth;
- adapters are optional capability enrichers;
- generic live providers are not excluded;
- Claude and Agy have bounded enrichment support; and
- absence of proof for a named hard capability still fails closed.

Roll out in one Batuta preview after unit, race, secret-canary, routing,
contract, and isolated live-inventory tests pass. Do not claim a provider is
live in public QA unless an isolated Compozy catalog actually reports the
exact pair as `available_live`.

## Acceptance criteria

1. A synthetic `available_live` provider with no Batuta adapter produces one
   generic Compozy-owned candidate.
2. Removing Claude, Agy, Codex, Cursor, or OpenCode executables does not remove
   any live Compozy catalog pair.
3. Installing a matching enricher changes evidence/ranking only; it cannot
   alter candidate cardinality or exact runtime IDs.
4. A CLI-only model absent from Compozy is rejected.
5. Claude and Agy command shapes, malformed output, missing executable,
   version skew, output bounds, and secret canaries have deterministic tests.
6. Missing Compozy CLI/credentials/login or permission state rejects the
   affected provider even when an adapter claims it is healthy.
7. Provider/model fit and routing generations are deduplicated by exact pair.
8. Unknown model tiers are eligible only at the conservative standard floor.
9. Existing archived routing generations remain readable and recovery keeps
   their exact provider/model selection.
10. All changes are confined to Batuta; Compozy remains untouched.

## Rejected alternatives

- **One hard-coded executor enum per Compozy provider:** duplicates Compozy's
  provider registry and makes every new provider a Batuta release.
- **Remove adapters entirely:** loses useful local capability, health,
  instruction, MCP, plugin, and permission evidence.
- **Let a CLI model list create runtime candidates:** can select a model that
  Compozy cannot execute.
- **Require an adapter for every candidate:** repeats the current exclusion
  bug and prevents generic providers.
- **Infer advanced/frontier tier from a provider or adapter name:** silently
  weakens complexity floors when vendors change model catalogs.
- **Modify Compozy to expose Batuta-specific adapter data:** the current live
  catalog and run-agent runtime already define the necessary authority.
