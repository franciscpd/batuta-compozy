# Provider routing enrichment — local QA

Date: 2026-08-28

Status: deterministic Batuta behavior `pass`; live Cursor/Grok inventory and
routing `pass`; live Claude/Agy execution `blocked-verify`.

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

No shared Compozy source, configuration, database, migration, extension
installation, or daemon state was changed for this work. The live qualification
below used an ephemeral isolated Compozy home, daemon, workspace, and Batuta
installation that were removed after the run.

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

## Live Cursor/Grok qualification

An isolated Compozy `0.3.0-beta.21.preview.738edb54f` daemon loaded Batuta from
commit `cd9196d3082cb0646304aeb51fc1c853ec36e249`. The live Cursor catalog exposed
`cursor/grok-4.6[effort=high,fast=true]` as `available_live`, and that exact pair
executed two read-only Batuta turns in session `sess-0efd6def2190fd03`:

1. `executor_inventory` completed with inventory digest
   `sha256:52a2e6532c566fed2ded07388ca8d3d3309628cb5a9ffd01e4f7ca09e8f7dfcc`
   and catalog generation
   `sha256:6e14462bbfc16fdd74c3c3ca1a4571183867f85ec3520f279f6e1bbf26a9bb9e`.
   The model correctly treated Agy as optional evidence enrichment rather than
   as provider/model execution authority.
2. The same runtime read the five `parallel-demo` task artifacts and called
   `routing_plan`. It produced generation digest
   `sha256:52355e6582676b7a5690a662f23d7fa666599c4bb2205ffdb762a684cda79b65`
   for task-set digest
   `d623fdf53eebed22c75dcdd185aef190ab5cefc80776c751a407404cab02e047`.

The generated rules selected the exact Grok 4.6 identifier for
`frontend/medium` with medium reasoning and `fullstack/high` with high
reasoning. Backend, docs, and testing low-complexity cells selected
`codex/gpt-5.6-sol` with low reasoning. The session ended `done/healthy`, with
no pending interaction, Loop run, worktree, `routing_apply`, or publication.
The provider did not expose token usage through the Compozy usage projection,
so this run makes no token-count claim.

## Live qualification still required

| Scenario | Status | Required evidence |
| --- | --- | --- |
| Cursor/Grok live inventory and routing | `pass` | exact `available_live` pair, prompt runtime provenance, inventory and routing digests |
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
