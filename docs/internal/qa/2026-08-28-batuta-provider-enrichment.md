# Provider routing enrichment — local QA

Date: 2026-08-28

Status: deterministic Batuta behavior `pass`; live Claude/Agy execution
`blocked-verify`.

## Scope and authority

Compozy's redacted live catalog is the only provider/model execution authority.
Batuta creates one candidate for every eligible live pair with execution owner
`compozy`. Codex, OpenCode, Cursor Agent, Claude Code, and Agy are optional
evidence enrichers; their absence does not remove a catalog pair. Caller input
cannot supply provider bindings or enrichment IDs.

The implementation is split across these commits on
`worktree-batuta-delivery-hardening`:

- `e3a54496afeea6700270be5fe67c1141baad8855` — make the Compozy catalog authoritative;
- `8552c98e1aae491d0acd54476298947f66a3ffe1` — convert existing CLI probes into optional enrichers;
- `eac718f9e9a8319da4126f00d3d0905daf695dd1` — add bounded Claude Code and Agy inventory;
- `08d08d1c0eee2fd7f75d4044574a51b90d07c265` — close output evidence and legacy replay compatibility.

No Compozy source, configuration, database, migration, extension installation,
or daemon state was changed for this work.

## Fixture-backed evidence

Go fixtures cover all six inventory records, missing executables, malformed and
schema-skewed output, unknown fields, duplicate identifiers, secret canaries,
record-budget exhaustion, stable catalog generation, optional-enricher loss,
generic live providers, hard-capability proof, canonical enrichment ordering,
closed schemas, and byte-equivalent legacy generation replay.

Contract fixtures cover the four explicit ineligible provider-auth states
(`missing_cli`, `missing_credential`, `needs_login`, `permission_denied`), live
availability, degraded unknown auth, Compozy-owned fit identity, and the absence
of caller-authored enrichment evidence. E2E evidence requires a new generation
to record `executor_id: compozy`, canonical enrichment IDs, and no raw adapter
payload.

## Local Agy observation

The installed Agy CLI reported version `1.1.14`. After the operator authenticated
in Agy, a bounded manual `agy models` observation exited successfully and listed
14 remote model IDs. The command visibly performs “Fetching available models”,
so it is an authenticated network request and is deliberately excluded from the
Batuta adapter. Batuta probes only `agy --version`, `agy agent`, and
`agy plugin list`; provider/model candidates still come from Compozy.

This observation proves the command's network behavior, not runtime support or
provider availability. No token, credential, environment value, or raw auth
state is recorded here.

## Live qualification still required

| Scenario | Status | Required evidence |
| --- | --- | --- |
| Claude live execution | `blocked-verify` | exact `available_live` Compozy pair plus child runtime provenance |
| Agy-backed live execution | `blocked-verify` | exact `available_live` Compozy pair plus child runtime provenance |
| Enricher missing during live delivery | `blocked-verify` | isolated daemon run proving the catalog pair remains selectable |

Fixture-backed behavior must not be presented as proof that a provider is
configured on another machine. Live qualification must use an isolated
workspace and redact runtime evidence.

## Fresh deterministic gates

```text
rtk go test ./... -count=1
PASS: 560 tests in 8 packages.

rtk go test -race ./internal/inventory/... ./internal/routing ./internal/extensionapp -count=1
PASS: 328 tests in 4 packages.

rtk go vet ./...
PASS.

PYTHONDONTWRITEBYTECODE=1 rtk python3 -m unittest discover -s tests/e2e -p 'test_*.py'
PASS: 32 tests.

rtk bash tests/contract/test_02_domain_lane_surface.sh
rtk bash tests/contract/test_02_routing_pair_selection.sh
rtk bash tests/contract/test_07_public_docs.sh
rtk bash -n tests/contract/*.sh scripts/*.sh
rtk git diff --check
PASS.
```

`test_02_routing_dryrun.sh` and `test_04_deliver_validate.sh` were not run
against the shared Compozy installation: they read or mutate live workspace and
extension state. They remain part of the isolated runtime qualification above,
not deterministic evidence from this change.
