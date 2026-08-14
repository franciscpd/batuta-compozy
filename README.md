# batuta-compozy

> 🇧🇷 [Versão em português](README.pt-BR.md)

Batuta as a resource-only CompozyOS extension: a conductor agent that
orchestrates the dev-cycle (the `cy-*` skills + bundled Loops) with
cost/complexity runtime routing. The conductor never writes code — it
classifies, decomposes, dispatches, and reports.

Current design: `docs/superpowers/specs/2026-08-12-batuta-reliability-design.md`.

## Prerequisites

1. A post-`0.3.0-beta.13` CompozyOS build containing fix `594d9fdf`, or the
   first later release (`0.3.0-beta.14`/stable expected), with the daemon
   running. The manifest keeps `0.3.0-beta.13` only as its grammar floor; the
   plain beta.13 tag is not operationally supported. Verify the runtime with:

   ```bash
   scripts/check-compozy-version.sh
   ```

   For beta.13 post-tag builds, the guard resolves `Version` and `Commit`
   against canonical full hashes for the known official descendants from
   `594d9fdf` through the current verified build. `Commit` must be the exact
   full hash or the official eight-character build abbreviation, and the
   describe hash must be an unambiguous prefix of that same commit. Arbitrary
   custom builds are rejected; base them on a later beta/stable release.
2. Bundled `dev-cycle` extension active (`compozy extension list`) — it
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

Flow: PM phase in conversation (PRD → TechSpec → tasks via the `cy-*` skills)
→ direct read-only task import preflight → Loop dry-run (planning only) →
dispatch of `batuta-deliver(slug, origin_session_id, auto_commit)` → bundled
`implement-tasks` (one isolated cycle + one commit per task) →
`review-and-fix` (review rounds until clean) → exact terminal outcome.

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
