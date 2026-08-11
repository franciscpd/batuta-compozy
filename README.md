# batuta-compozy

> 🇧🇷 [Versão em português](README.pt-BR.md)

Batuta as a resource-only CompozyOS extension: a conductor agent that
orchestrates the dev-cycle (the `cy-*` skills + bundled Loops) with
cost/complexity runtime routing. The conductor never writes code — it
classifies, decomposes, dispatches, and reports.

Full design: `docs/superpowers/specs/2026-08-11-batuta-compozy-design.md` (PT-BR).

## Prerequisites

1. CompozyOS >= 0.3.0 (pre-releases included; manifest floor: 0.2.0) with the daemon running (`compozy status`).
2. Bundled `dev-cycle` extension active (`compozy extension list`) — it
   publishes the `cy-*` skills and the `implement-tasks` / `review-and-fix`
   Loops.
3. **Provider authentication for the routing lanes** (operator surface, once,
   global — outside the extension's scope):

   ```bash
   compozy provider auth login opencode   # low/medium/high lanes
   compozy provider auth login claude     # critical lane
   ```

   Check with `compozy provider models list`.

## Installation (local/dev)

```bash
compozy extension install ~/Projects/batuta-compozy --allow-unverified --yes
compozy extension enable batuta
compozy extension inventory batuta -o json   # must list the batuta agent and the batuta-routing skill
```

## Usage

Create a session with the `batuta` agent in your project's workspace and
talk to it. On first contact batuta configures itself: it applies the
default table from the `batuta-routing` skill as the stored override for the
`implement-tasks` Loop and asks only the preferences that matter
(auto-commit, the `critical` lane).

Flow: PM phase in conversation (PRD → TechSpec → tasks via the `cy-*`
skills) → dispatch of `implement-tasks` (one isolated cycle + one commit per
task) → `review-and-fix` (review rounds until clean) → exact terminal
outcome.

## Routing

Default table in `resources/skills/batuta-routing/SKILL.md`
(lanes `low`/`medium`/`high`/`critical`). To change it in a workspace, ask
batuta in conversation — it rewrites the override with `loop configure`. The
routing decision stays auditable per generation in `resolved_runtime`.

## Tests

```bash
tests/contract/run.sh
```

Guided E2E smoke: `tests/e2e/SMOKE.md` (PT-BR).
