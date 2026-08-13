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

## Usage

Create a session with the `batuta` agent in your project's workspace and
talk to it. On first contact, Batuta derives a concrete routing table from the
live provider-model catalog, confirms it with the operator, and stores it as
the `implement-tasks` runtime override. It separately stores the operator's
commit preference at `loops.inputs.batuta-deliver.auto_commit`.

Flow: PM phase in conversation (PRD → TechSpec → tasks via the `cy-*` skills)
→ direct read-only task import preflight → Loop dry-run (planning only) →
dispatch of `batuta-deliver(slug, origin_session_id, auto_commit)` → bundled
`implement-tasks` (one isolated cycle + one commit per task) →
`review-and-fix` (review rounds until clean) → exact terminal outcome.

The direct preflight must return a positive task count. Dry-run resolves inputs
and plans nodes but does not execute `import_tasks`, so it cannot detect a
missing task set.

Batuta supplies its current CompozyOS session ID as `origin_session_id`. The
composite Loop passes `auto_commit` explicitly to both children. All seven
native contract terminal effects queue one idempotent prompt to that same
conversation. There is no `batuta-watch` resource, background watcher, or
reporting agent.

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
