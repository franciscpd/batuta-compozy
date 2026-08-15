# batuta-compozy

> 🇧🇷 [Versão em português](README.pt-BR.md)

Batuta as a resource-only CompozyOS extension: a conductor agent that
orchestrates the spec-cycle (the `cy-*` skills + bundled Loops) with
cost/complexity runtime routing. The conductor never writes code — it
classifies, decomposes, dispatches, and reports.

Current design: `docs/superpowers/specs/2026-08-15-batuta-spec-cycle-migration-design.md`.

## Prerequisites

1. CompozyOS `v0.3.0-beta.16-9-ga35eda6d` at the verified full commit
   `a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c`, or a later release that
   contains it, with the daemon running. The manifest keeps `0.3.0-beta.13`
   only as its grammar floor. Verify the runtime with:

   ```bash
   scripts/check-compozy-version.sh
   ```

   The guard accepts the exact verified build and supported later releases;
   arbitrary custom histories are rejected.
2. Bundled `spec-cycle` 0.4.0 extension active (`compozy extension list`) — it
   publishes the `cy-*` skills and the `implement-tasks` / `review-and-fix`
   Loops.
3. **Provider authentication** (an operator surface, once and global — outside
   the extension's scope). Derive concrete provider/model IDs from the live
   catalog; never copy a lane assignment from documentation:

   ```bash
   compozy provider models list
   ```

4. Register this repository once before running daemon-backed contract tests:

   ```bash
   compozy workspace add "$PWD"
   ```

## Installation (local/dev)

```bash
scripts/republish.sh
```

The workflow validates compatibility before changing the installed extension,
stages only declared resources, then installs, enables, and checks the exact
inventory: `batuta`, `batuta-routing`, and `batuta-deliver`.

Publication retains a content-addressed source under
`${XDG_DATA_HOME}/batuta-compozy/packages` or
`~/.local/share/batuta-compozy/packages`. Set `BATUTA_PACKAGE_ROOT` to override
that root. Its files are read-only, and its exact tree and bytes are verified
before reuse. The live extension provenance continues to reference this
existing minimal package. A stable per-user lock at
`~/.compozy/locks/batuta-republish.lock`, independent of package location,
serializes package creation, verification, validation, installation, enabling,
and final inventory verification. The package is reverified under that lock
immediately before any installed extension is removed or replaced.

## Preview installation: v0.1.0-beta.2

The reviewed preview is published from `franciscpd/batuta-compozy` as exactly
`batuta-compozy_0.1.0-beta.2.tar.gz` and `SHA256SUMS`. Download and verify
both assets before extracting them into a new directory:

```bash
preview_dir=$(mktemp -d)
gh release download v0.1.0-beta.2 --repo franciscpd/batuta-compozy --dir "$preview_dir"
(cd "$preview_dir" && sha256sum --check SHA256SUMS)
extracted_directory=$(mktemp -d)
tar -xzf "$preview_dir/batuta-compozy_0.1.0-beta.2.tar.gz" -C "$extracted_directory"
compozy extension validate "$extracted_directory" -o json
```

This is a preview trust boundary: only after explicitly accepting the
unverified preview source, install that validated extracted directory:

```bash
compozy extension install "$extracted_directory" --allow-unverified --yes
```

To remove this preview or roll back before installing another validated
package, run:

```bash
compozy extension remove batuta --global
```

Verified Batuta behavior is resource-only orchestration with one `batuta`
agent, one `batuta-routing` skill, and one `batuta-deliver` Loop. Two upstream
CompozyOS limitations remain: executor sessions are not visually nested and
remain active/idle after normal terminal completion. Neither limitation is
fixed by this preview.

## Usage

Create a session with the `batuta` agent in your project's workspace and
talk to it. On first contact, Batuta resolves the operator's commit preference
at `loops.inputs.batuta-deliver.auto_commit`. After that gate opens, it derives
a concrete routing table from the live provider-model catalog, confirms it
with the operator, and stores it as the `implement-tasks` runtime override.

As the hard initial gate of every new session, Batuta reads only that exact key
in the current workspace before discovery, routing, PM, preflight, dry-runs, or
Loop inspection. `config_path_not_found` makes it ask the operator, write the
boolean at workspace scope, and immediately confirm it with a structured
reread. Any other config error stops unchanged; global defaults, child Loop
defaults, definition defaults, and dry-runs never substitute for the stored
preference. Batuta repeats the read before every dispatch.

Flow: requirements and unified spec via `cy-create-spec` → operator approval
of `_spec.md`, `_user_stories.md`, `_dx.md`, `_tests.md`, and `_uiux.md` only
for Web-bearing work → tasks via `cy-create-tasks` → direct read-only task
preflight via `ext__spec_cycle__import_tasks` → Loop dry-run (planning only) →
dispatch of
`batuta-deliver(slug, origin_session_id, auto_commit)` → bundled
`implement-tasks` → `review-and-fix` → exact terminal outcome.

A simple request may use a short grill but may not skip `cy-create-spec` or
task creation.

Executable requirements such as dependency names and versions, commands,
paths, flags, and constraints remain literal throughout PM artifacts, tasks,
and execution prompts.

The direct preflight must return a positive task count. Dry-run resolves inputs
and plans nodes but does not execute `import_tasks`, so it cannot detect a
missing task set.

Batuta supplies its current CompozyOS session ID as `origin_session_id`. The
composite Loop passes `auto_commit` explicitly to both children. All seven
native contract terminal effects queue one idempotent prompt to that same
conversation. When dispatch is accepted, its tool result returns `run_id` and
an optional `web_url`, and Batuta ends that turn. CompozyOS's existing
idempotent terminal effect starts the later reporting turn; in that turn,
Batuta verifies the exact run before reporting. An explicit progress request
takes one status snapshot and does not poll. There is no `batuta-watch`
resource, background watcher, or reporting agent.

## Routing

Lane semantics live in `resources/skills/batuta-routing/SKILL.md`
(`low`/`medium`/`high`/`critical`). Provider/model selections always come from
`compozy provider models list`; the catalog is authoritative for installed
providers, model IDs, and costs. To change the stored workspace override, ask
Batuta in conversation. Routing stays auditable per generation in
`resolved_runtime`.

## Tests

```bash
# Requires the repository registration from Prerequisites.
tests/contract/run.sh
```

Guided E2E smoke: `tests/e2e/SMOKE.md` (PT-BR).
