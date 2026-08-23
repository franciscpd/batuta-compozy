# Handoff — batuta delivery hardening (worktree + gated publication, contract robustness)

Branch: `worktree-batuta-delivery-hardening` (worktree
`.claude/worktrees/batuta-delivery-hardening`), 22 commits, `f2f5f8b7..fb7308d0`.
Extension version bumped to `0.1.0-beta.4`.

## What was built

Two designs, each with a spec and an implementation plan under
`docs/internal/specs/` and `docs/internal/plans/` (dated 2026-08-21):

1. **Delivery worktree + gated publication.** Every delivery runs in a managed
   worktree (`batuta-<slug>`, branch `batuta/<slug>`) whose environment the
   `run-loop` children inherit. After review the graph inspects the worktree,
   parks on a human gate, and only then runs a `publish` goal node bound to a
   new bundled agent, `batuta-publisher`, which runs the `compozy worktree`
   exit verbs (clean-tree + HEAD check, exit plan, push, PR). Task artifacts
   reach the worktree through the daemon's `worktrees.copy_list` bootstrap
   copy, which the conductor verifies before dispatch.
2. **Contract robustness.** A wall-clock backstop that actually binds, the
   preference gate scoped to delivery-path calls, a partial-run audit before
   any redispatch, and documented `auto_commit=false` consequences.

The extension grew from three resources to four (`batuta`, `batuta-publisher`,
`batuta-routing`, `batuta-deliver`), and `batuta-deliver` gained a REQUIRED
input `worktree_ref` — a breaking change for direct (non-conductor)
submissions, recorded in `docs/releases/0.1.0-beta.4.md`.

## Findings that changed the design mid-flight (all proven live, not assumed)

- **`permissions.shell.allow` does not exist.** The original spec invented it.
  The daemon's agent `permissions` is a bare enum (`deny-all` |
  `approve-reads` | `approve-all`). The publisher's command scope is enforced
  by its prompt body, not by a schema.
- **`deny-all` auto-rejected the publisher's own CLI steps.** Fell back to
  `approve-reads` per the recorded ladder. See the open question below.
- **A managed worktree does not contain `.compozy/tasks/<slug>`.** Without a
  fix, the conductor's preflight would pass in the main workspace and the
  Loop's `load_check` would then kill every delivery inside the worktree.
  Resolved with `worktrees.copy_list` (proven live), verified by the conductor
  before dispatch, and fail-closed when absent.
- **The Loop definition's `contract.budget.wall_clock_sec` enforces nothing.**
  It moves only `materialized_contract`; the daemon enforces
  `effective_config.budget_wall_sec` (per-run `config_overrides` > stored
  `compozy__loop_configure` workspace override > default `0`). The shipped
  literal was inert, so deliveries were running unbounded. The backstop now
  goes through the governing layer: Bootstrap provisions it, Dispatch verifies
  it is nonzero before a real submission. A direct submission outside the
  conductor path is still unbounded until that workspace override exists —
  stated plainly in the docs rather than papered over.
- **`ahead_of_base` can be stale and nothing can prove freshness.**
  `compozy__worktree_inspect` exposes no refresh parameter and the branch CEL
  cannot express freshness (`now()` undeclared). A stale `0` would have
  skipped the human gate silently and completed the run `done`. `publish_check`
  is now fail-closed (`condition: "true"`): EVERY delivery parks on the gate,
  and the nothing-to-publish case is established by the publisher from the exit
  plan after approval. The branch node remains as the seam where an
  evidence-based route returns if the daemon ever exposes freshness.
- **`compozy__loop_nodes` cannot supply per-task commit evidence** (its roster
  returns waiting/quarantined/attention/retrying cells only). The audit
  procedure was rewritten around a `compozy__loop_status` parent-to-child chain
  plus `compozy__worktree_inspect`, with an explicit `auto_commit=false`
  variant.

## Verification state

- Full contract suite green from a disposable detached worktree, except
  `test_00_runtime_guard.sh`, which fails locally because the installed daemon
  is a dev build (`v0.3.0-beta.19-3-gdf7e80e4-dirty`) whose version string the
  guard's regex does not recognize. It reproduces at the baseline commit and is
  environmental, not a branch defect.
- `python3 -m pytest tests/e2e/ -q` gives 32 passed.
- Reviews performed: per-task reviews on every task of both plans, a
  whole-branch review per plan, and an external Codex review of the whole
  branch. Their findings were applied in two fix waves plus a repair commit;
  the repair commit exists because an earlier wave left the contract suite red
  (a hermetic test's fake inventory was stale) and a report misdiagnosed that
  as daemon flakiness.

## THE OPEN QUESTION — this is what to analyze

**Can a Loop `goal` node run the `batuta-publisher`'s CLI steps unattended
under `permissions: approve-reads`?** Unanswered. It blocks merge.

What is known: an interactive probe under `approve-reads` required a manual
`compozy session approve` before each CLI step; `deny-all` auto-rejected them
outright. Whether a daemon-driven goal node behaves the same was never
observed.

The attempt to settle it was **inconclusive for a reason worth investigating on
its own**. A throwaway Loop (`gate-probe`: one goal node bound to
`batuta-publisher`, `environment: {mode: worktree, ...}`, lab repo whose
`origin` was a local bare repo so nothing could leave the machine) was
submitted for real and then sat for over twenty minutes in this state:

- `status: "running"` with `completion_state: "complete"`
- `compozy loop events <run> --view all` returned **zero events**
- zero node cells; `progress.steps_total: 0`; `tokens_used: 0`
- `last_progress_at` identical to `started_at`
- **no lines mentioning the run anywhere in the daemon log**
- the local bare repo still held only `main` — nothing was pushed

So the goal node never executed: `approve-reads` was never exercised. That is a
scheduling failure in the daemon or in the lab setup, NOT evidence about the
permission model — the distinction matters, and no verdict should be inferred
from the stall.

Two earlier `gate-probe` runs were canceled before maturing. Every lab artifact
was cleaned up afterwards (run canceled, worktree removed, workspace removed,
`/tmp` lab directory deleted), so the run is no longer inspectable — a fresh
reproduction is needed.

### What would close it

Either (a) an explanation for why a submitted run never schedules its only
node — the run record above is the evidence, and a fresh reproduction should be
cheap; or (b) the operator-driven live smoke in `tests/e2e/SMOKE.md`, which
requires a human conversing with the `batuta` agent and approving the spec
artifacts and the gate.

If the publish node does stall on approvals, the fix is to revisit the
publication design — not to raise the publisher to `approve-all`, which crosses
workspaces silently.

## Deliberately parked (with reasoning, not forgotten)

- The `publish` goal's `worktree_clean` judge runs
  `test -z "$(git -C . status --porcelain)"`, which is unanchored (`git -C .`
  is just `git`) and is a post-condition, not a pre-push guard. The real
  pre-push guard is the publisher's prompt. Anchor it deterministically if the
  graph ever exposes the worktree path to judge context.
- The publication-evidence assertion in `tests/e2e/assert_publication_gate.py`
  is forward-looking: today's success payload carries no node output, so it
  passes only against an enriched events export. Documented in-code.
- `test_04`'s budget assert pins YAML prose, not daemon behavior; live coverage
  lives in `SMOKE.md`.

## Where the working record lives

Per-task briefs, implementer reports, probe transcripts and the decision ledger
are under `.superpowers/sdd/` in the worktree (git-ignored, still on disk):
`2026-08-21-batuta-worktree-and-gated-publication/` and
`2026-08-21-batuta-contract-robustness/`. `progress.md` in each is the ledger,
including every ruling made on the user's behalf.
