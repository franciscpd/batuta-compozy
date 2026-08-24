# Batuta executor inventory and domain lanes — design

Status: approved in conversation on 2026-08-24; implementation requires the
Compozy conjunctive-runtime-rule prerequisite

## Context

Batuta currently routes imported tasks only by `complexity`. Its routing skill
derives a provider/model table from the live Compozy model catalog and stores
workspace rules for `low`, `medium`, `high`, and `critical`.

That model misses two requirements:

- different tasks need different executor capabilities, such as backend,
  frontend, mobile, data, or infrastructure tooling; and
- Batuta should understand the effective local configuration of Compozy,
  Codex, OpenCode, and Cursor Agent instead of treating every installed ACP
  provider as interchangeable.

The operator must not become the routing engine. Batuta owns classification,
decomposition, selection, fallback, persistence, and audit evidence. The only
remaining human delivery decision is the publication gate defined by the
scoped-publication spec.

## Decision

Route every executable task on two orthogonal axes:

```text
lane = domain × complexity
```

The domain describes the work and required capabilities. Complexity describes
the minimum model strength, reasoning, and risk posture. A lane is a capability
profile, not a permanent binding to a vendor or executable.

Batuta builds a redacted executor inventory, classifies and decomposes the
task set, filters executors by hard capability requirements, ranks the eligible
set, and stores one concrete Compozy runtime rule per matrix cell. Compozy's
resolved runtime and field provenance remain the execution authority.

## Canonical task taxonomy

The initial closed domain vocabulary is:

| Domain | Intent |
| --- | --- |
| `backend` | services, APIs, server-side application logic |
| `frontend` | browser UI, design systems, client state and accessibility |
| `mobile` | native or cross-platform mobile application work |
| `data` | schemas, queries, pipelines, analytics and data migrations |
| `infra` | CI/CD, containers, cloud, deployment and repository automation |
| `security` | authentication, authorization, secrets and threat-sensitive changes |
| `testing` | test infrastructure, fixtures, quality tooling and verification harnesses |
| `docs` | product, API, architecture and maintainer documentation |
| `general` | contained work with no stronger domain signal |
| `fullstack` | reserved fallback for a genuinely indivisible cross-domain task |

Complexity remains `low`, `medium`, `high`, or `critical`. Neither axis is
encoded into the other; values such as `frontend-high` are invalid.

## Classification and decomposition

Classification is hybrid and structured:

1. a valid domain or complexity explicitly authored in the approved task/spec
   wins;
2. Batuta evaluates the task body, acceptance criteria, affected paths,
   repository instructions, dependencies, and neighboring artifacts;
3. the Batuta LLM emits a closed-schema classification with domain,
   complexity, confidence, capability requirements, and bounded evidence;
4. deterministic validation rejects unknown values and contradictory or
   missing fields before routing.

Cross-domain work is decomposed into dependency-linked tasks whenever each
piece can be implemented and verified independently. For example, an API
change and its browser consumer become `backend` and `frontend` tasks. The
`fullstack` fallback is used only when splitting would break atomic behavior or
produce unverifiable intermediate work.

This supersedes the older spec-cycle rule that required the operator to review
every type and complexity assignment. Batuta may explain the classification,
but it does not pause for an operational routing decision.

## Redacted executor inventory

Batuta adds one read-only extension capability that returns a normalized
snapshot for the trusted workspace. It accepts no home path, config path,
command, or credential value from the caller.

Each adapter reports:

```text
executor id and version
availability and health
agents/profiles visible to that executor
provider and exact model identifiers
tools, MCP servers, plugins, skills, and repository instruction sources
permission, approval, sandbox, and writable-root posture
domain capability evidence
configuration origins and precedence
resolution state: resolved | declared | unknown
diagnostics without secret values
```

Secret values, auth tokens, headers, environment values, credential file
contents, and raw unredacted config are never returned, logged, stored in task
artifacts, or sent to an LLM. Credential information is reduced to
`configured`, `missing`, or `unknown` only when the underlying tool exposes
that state safely.

Adapter commands come from a closed executable/argument allowlist. They run
without a shell, with bounded time and output, against the trusted workspace.
Raw stdout and stderr are parsed and redacted before they can enter structured
errors or logs. File-backed adapters read only documented configuration and
instruction locations and project-local overrides inside the trusted
workspace; a caller cannot supply another path.

### Compozy adapter

The running daemon is authoritative. The adapter consumes redacted effective
configuration, installed agent definitions, provider auth state, the live
provider/model catalog, tool descriptors, skills, and runtime availability.
Static files are provenance only when the daemon has not resolved an
equivalent value.

### OpenCode adapter

The adapter prefers OpenCode's resolved configuration and agent-debug
surfaces, then its agent/model/MCP/skill inventories. Only allowlisted fields
are normalized. An absent resolved field is `unknown`; Batuta does not infer a
permission or model from a filename alone.

### Codex adapter

The adapter maps the active Codex version, selected configuration profile,
layered `config.toml` fields, project and user `AGENTS.md`, MCP/plugin
inventory, model/provider declaration, approval policy, sandbox mode, writable
roots, and skills. Where Codex exposes no safe effective-config command, the
state is marked `declared` with exact file provenance rather than presented as
resolved.

### Cursor Agent adapter

The adapter maps the Cursor Agent CLI version and health, available models,
MCP and plugin inventory, workspace rules/instructions, CLI configuration,
sandbox/approval posture, and writable roots. Editor-only settings that do not
govern Cursor Agent execution are labeled separately and never promoted to an
effective agent capability.

## Capability profiles

Each domain defines hard requirements and preference signals. Hard
requirements may include language/toolchain availability, browser or mobile
tooling, database access, infrastructure CLIs, MCPs, repository instructions,
sandbox write access, or test commands. Complexity sets minimum model tier,
reasoning effort, verification depth, retry budget, and review posture.

The inventory does not claim that an executor is capable merely because it is
installed. A candidate is eligible only when every hard requirement is
`resolved` or is proven by a successful bounded probe. `declared` evidence may
rank candidates but cannot satisfy a security-sensitive hard requirement.

The LLM owns semantic judgment: task classification, decomposition, and a
structured capability-fit recommendation among proven candidates.
Deterministic code owns the closed vocabulary, secret boundary, hard
constraints, model-floor policy, catalog membership, stable tie-breakers, and
the final rule schema. The LLM cannot invent an executable, provider, model,
permission, health result, configuration origin, or command.

## Selection and persistence

For each populated matrix cell Batuta:

1. filters unavailable executors and unmet hard requirements;
2. rejects models below the complexity floor;
3. evaluates the LLM's structured fit recommendation against Batuta's
   versioned model-tier and domain-capability policy;
4. ranks remaining candidates by validated capability fit, health, expected
   quality, configured permissions, and then cost;
5. uses deterministic stable tie-breakers;
6. records the selected provider, exact live model ID, reasoning, evidence,
   rejected candidates, and fallback order;
7. stores a `type + complexity` runtime rule for `implement-tasks` through the
   Compozy configuration API.

The live Compozy catalog is the only authority for provider/model identifiers
used in runtime rules. External CLI inventories inform eligibility and
capabilities; they do not invent a Compozy provider or model binding.

The stored matrix is refreshed automatically when an executable version,
effective configuration digest, provider catalog generation, or relevant
workspace instruction digest changes. A dispatch snapshots and reports the
resolved routing generation so a mid-run configuration change cannot silently
rewrite an executing task.

## Automatic fallback

Binding failures, unavailable models, quota/auth errors, and repeated
execution failures are classified from structured runtime evidence. Batuta
selects the next eligible candidate, writes a surgical exact-task override,
and redispatches within the existing iteration, token, and wall-clock budgets.
It never downgrades the task's complexity floor.

Exact-task overrides are Batuta-owned transient state. Batuta records their
owner delivery run, removes them after the replacement child run snapshots its
configuration, and reconciles stale owned overrides before the next Batuta
dispatch. It never removes an operator-authored rule.

If no candidate satisfies the lane, Batuta attempts valid decomposition and
the `general`/`fullstack` fallbacks where semantically sound. It blocks only
when no executor can meet a hard requirement or an external prerequisite such
as credentials is genuinely unavailable. The blocker is reported precisely;
the operator is not handed a sequence of routing or cleanup commands.

## Integration with the delivery loop

Before creating the delivery run, Batuta:

1. validates the task set and classification;
2. refreshes or reuses a proven inventory snapshot;
3. materializes the required matrix cells and fallback chains;
4. verifies the stored routing generation through a read-back;
5. dispatches `batuta-deliver` with the delivery worktree.

`batuta-deliver` always passes literal `auto_commit: true` to
`implement-tasks` and `review-and-fix`. Lane selection never changes commit
ownership. Each implemented task and every review remediation is committed by
the Batuta-controlled loop; the publication phase later verifies a clean exact
HEAD, pushes it, and opens the pull request.

The Compozy `code_implementer` agent remains the task-level execution
contract. Runtime rules choose the ACP provider, model, reasoning, and speed
per task. Codex/OpenCode/Cursor repository instructions that their runtime
applies remain visible in the inventory, but Batuta does not pretend that a
foreign named agent definition replaced the Compozy agent unless Compozy
actually reports that binding.

## Verification

Implementation acceptance requires:

1. adapter fixtures for Compozy, OpenCode, Codex, and Cursor Agent covering
   installed, missing, malformed, partially configured, and version-skewed
   states;
2. secret-canary tests proving tokens, headers, credential values, environment
   secrets, and unapproved raw config never cross the inventory boundary;
3. classification tests for every domain and complexity, invalid vocabulary,
   explicit-metadata precedence, and low-confidence structured output;
4. decomposition tests for backend/frontend dependencies and the indivisible
   `fullstack` fallback;
5. deterministic routing tests for hard constraints, model floors, cost
   ranking, stable ties, unavailable providers, and provider-specific model
   IDs;
6. Compozy integration tests proving `type + complexity` rules select distinct
   providers/models and exact-ID fallback retains precedence;
7. recovery tests proving Batuta-owned overrides are reconciled without
   deleting operator-authored rules;
8. an isolated end-to-end smoke with at least two domains and two complexity
   levels proving resolved runtime provenance, one commit per task, committed
   review fixes, clean HEAD, push, PR creation, and exact-HEAD verification;
9. all existing Batuta contract and E2E suites remain green after removing the
   routing-confirmation and configurable-`auto_commit` operator paths.

## Compatibility and rollout

The feature requires the first Compozy prerelease that supports conjunctive
`type + complexity` runtime matches. Batuta's runtime guard must recognize the
exact released identity before the new matrix is activated.

Inventory rollout begins read-only and records snapshots without changing
routing. Matrix selection is then enabled in an isolated QA workspace, followed
by fallback/recovery and the complete delivery smoke. No global user config is
rewritten, and Batuta never publishes raw local configuration as PR evidence.

## Rejected alternatives

- **Domain-only lanes:** loses the risk and quality floor carried by
  complexity.
- **Complexity-only lanes:** treats incompatible executor capabilities as
  interchangeable.
- **Hard-code frontend to Cursor and backend to Codex:** becomes stale and
  ignores the machine's effective configuration.
- **Ask the operator to classify or select every route:** violates autonomous
  loop ownership.
- **Use an LLM's narrative as the inventory:** configuration and availability
  claims require deterministic evidence and provenance.
- **Read every config file and send it to the LLM:** leaks secrets and confuses
  declared values with effective runtime state.
- **Route directly to foreign named agents:** Compozy's task loop owns the
  execution agent contract; only bindings proven by Compozy may be claimed.
