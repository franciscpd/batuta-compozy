# Batuta beta.5 local acceptance — 2026-08-27

Status: `blocked-verify` for public release; deterministic local core is `pass`.

## Scope and trust

- Dependency: official `github.com/compozy/compozy/sdk/go v0.3.0-beta.21`.
- Runtime floor guard: `v0.3.0-beta.21` or a commit-matching compatible preview.
- Extension source: code-backed; generated manifest and binary are built before
  validation and installation.
- Trust: local-path with explicit `allow_unverified`; no release provenance
  claim.

No credentials, environment values, raw prompts, or executor configuration are
recorded here.

## Fresh deterministic evidence

- `go test -race ./... -count=1`: pass in six Go packages.
- `go vet ./...`: pass.
- `go test -tags=integration ./internal/extensionapp -run
  TestMigrationFreeDelivery -count=1 -v`: pass.
- Domain-routing and publication Python validators: 11 pass.
- Runtime guard, staged-source shape, generated-manifest validation, republish,
  and domain-lane contracts: pass.
- A clean detached HEAD containing only committed files passes the Go suite,
  vet, stage, generated-manifest, and domain-lane contracts.
- Generated extension inventory: one `batuta` agent, one `batuta-deliver` Loop,
  one `batuta-routing` skill, fixed subprocess, and eight hosted tools.

## Migration-free two-attempt evidence

The isolated integration creates a disposable Git workspace and bare remote.
It authors two canonical tasks and exercises the production planner, matrix
archive, delivery journal, fallback owner, publisher, and verifier:

| task | lane | attempt 1 | attempt 2 |
| --- | --- | --- | --- |
| `task_01` | `backend/low` | Codex / `gpt-5.6-luna`, completed and committed | carried by its completed artifact and commit |
| `task_02` | `frontend/medium` | Cursor / `grok-4.6[effort=high,fast=true]`, failed | Codex / `gpt-5.6-terra`, completed and committed |

Assertions:

- `apply_matrix` archives one immutable routing generation and stable
  `delivery_id`; it makes zero stored Loop-config calls;
- attempt 1 and attempt 2 have different parent `run_id` values and the same
  workspace, worktree, task snapshot, routing generation, token ceiling, and
  absolute deadline;
- attempt 2 contains only the incomplete frontend task and advances exactly
  that task to its configured fallback;
- start and recovery replay return the same run without another external start;
- a lost start response adopts exactly one matching recent run; multiple
  matches block without a third submission;
- publication pushes once to the disposable bare remote, opens one simulated PR
  receipt, and independently verifies that upstream HEAD equals reviewed HEAD;
- merge is never attempted.

## Stop-condition matrix

Fresh automated tests prove no new delivery submission for:

- canceled context before admission;
- absolute deadline reached;
- worktree or task drift;
- foreign run identity;
- exhausted fallback chain;
- exhausted token ceiling;
- terminal `blocked`, `exhausted`, `stalled`, or `canceled` run;
- review failure;
- failure after publication started.

The journal remains byte-equivalent where no state transition is allowed.
Deadline/token and terminal evidence may make the explicit durable
`exhausted`/`blocked` transition, but runner call counts remain unchanged.

## External scenarios

| scenario | status | evidence needed |
| --- | --- | --- |
| Official Compozy binary with child `run-loop` `config_overrides` | `blocked-verify` | merged contract and official binary release; local preview is not public provenance |
| Real executor failure then exact live fallback | `blocked-verify` | disposable authenticated providers with redacted `runtime_applied` events |
| Real forge publication | `blocked-verify` | disposable remote proving push receipt, HTTPS PR URL, and independent remote HEAD |

The deterministic bare-remote proof is release-relevant local evidence, but it
does not substitute for a real provider or forge persona. Public beta.5 must not
be promoted until all three external scenarios pass.
