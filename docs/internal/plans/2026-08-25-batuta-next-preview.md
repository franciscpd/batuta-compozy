# Batuta Next Preview Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Demonstrate Batuta routing real tasks by canonical domain and complexity on a minimal migration-free Compozy matrix build, with bounded execution, verification, atomic commits, and an honest roadmap visual.

**Architecture:** Keep Batuta resource-only. Extend the routing skill and conductor contract so the live provider/model catalog feeds ordered conjunctive `type + complexity` rules. Build Compozy from `upstream/main` plus only the reviewed migration-free matrix stack; use a disposable workspace for live evidence.

**Tech Stack:** Markdown agent/skill resources, Compozy Loop v1 YAML/runtime rules, Bash/Python contract tests, isolated local Compozy daemon, Git.

**Spec:** `docs/internal/specs/2026-08-25-batuta-next-preview-design.md`

## Global Constraints

- Use `upstream/main` plus only commits `e7b55569c`, `39cf835ad`,
  `e3cc76d29`, `b21e0d33c`, `9d9360ec6`, and `caca649c6`.
- Add no database migration, config-CAS, nested recovery, or dependency.
- Canonical domains are `backend`, `frontend`, `mobile`, `data`, `infra`, `security`, `testing`, `docs`, `general`, and `fullstack`.
- Complexity is exactly `low`, `medium`, `high`, or `critical`.
- Never run concurrent writers or committers in one worktree.
- Never claim automatic fallback, parallel integration, push, or PR creation as implemented without live evidence.
- Run `tests/contract/run.sh` only in a detached disposable worktree with no `.compozy` marker.

---

### Task 0: Minimal migration-free Compozy matrix build

**Files:**
- No new source edits; compose an isolated branch from reviewed commits.

**Interfaces:**
- Consumes: clean `upstream/main` and the six reviewed matrix commits.
- Produces: one local Compozy binary accepting conjunctive runtime selectors.

- [ ] **Step 1: Create an isolated Compozy worktree**

Create branch `feat/batuta-matrix-preview` from current `upstream/main` under
`/home/francisross/Projects/opensource/_worktrees/batuta-matrix-preview`.

- [ ] **Step 2: Apply only the matrix stack**

Cherry-pick, in order:

```text
e7b55569c
39cf835ad
e3cc76d29
b21e0d33c
9d9360ec6
caca649c6
```

Confirm the resulting diff contains no `internal/store/migrations` file and no
`00090` or `00091` migration.

- [ ] **Step 3: Run focused verification and build**

Run the matrix validation/resolution, API contract, daemon integration, native
schema, and config tests named by the commit stack. Build `./cmd/compozy` with
compiler scratch under a unique `/home/francisross/tmp-builds` directory.

- [ ] **Step 4: Re-run the Task 1 RED against this binary**

Run the detached Batuta contract suite with `COMPOZY_BIN`/`PATH` selecting the
minimal binary. Expected: the conjunctive routing test reaches GREEN while the
same test remains RED on the released binary.

---

### Task 1: Domain × complexity routing contract

**Files:**
- Modify: `resources/skills/batuta-routing/SKILL.md`
- Modify: `tests/contract/test_02_routing_dryrun.sh`

**Interfaces:**
- Consumes: live `compozy provider list` and `compozy provider models list` projections.
- Produces: ordered `runtime_rules[]` entries whose `match` is `{type, complexity}` and whose runtime uses exact live provider/model IDs.

- [ ] **Step 1: Make the routing contract test require conjunctive lanes**

Replace the four `--runtime complexity=...` arguments with a temporary JSON
config file containing these cells, substituting the live selected
`PROVIDER`/`MODEL` values:

```json
{
  "runtime_rules": [
    {"match":{"type":"backend","complexity":"low"},"runtime":{"provider":"$PROVIDER","model":"$MODEL"}},
    {"match":{"type":"frontend","complexity":"medium"},"runtime":{"provider":"$PROVIDER","model":"$MODEL"}},
    {"match":{"type":"infra","complexity":"high"},"runtime":{"provider":"$PROVIDER","model":"$MODEL"}},
    {"match":{"type":"security","complexity":"critical"},"runtime":{"provider":"$PROVIDER","model":"$MODEL"}}
  ]
}
```

Pass it through `compozy loop run --config-file "$RULES_FILE" --dry-run` and
assert the exact four `(type, complexity)` keys survive under
`effective_config.run_runtime_rules`.

- [ ] **Step 2: Run the test and preserve an honest RED**

Run: `tests/contract/test_02_routing_dryrun.sh`

Expected: FAIL because the current test/skill contract is complexity-only or
because the released daemon rejects the conjunction. If the daemon rejects
the conjunction, preserve that RED and run the same test against the minimal
matrix build; do not encode a composite type or task-ID workaround.

- [ ] **Step 3: Update the routing skill**

Define `lane = type × complexity`, the closed domain table, and specificity:

```text
id > type + complexity > type > complexity
```

Require canonical task `type`, exact live model IDs, provider availability
evidence, deterministic fallbacks to single-axis rules, and no raw credential
or config values. Replace the dated complexity-only example with a compact
matrix example using `backend/low`, `frontend/medium`, `infra/high`, and
`security/critical`.

- [ ] **Step 4: Run focused GREEN verification**

Run:

```bash
tests/contract/test_02_routing_pair_selection.sh
tests/contract/test_02_routing_dryrun.sh
git diff --check
```

Expected: all commands exit `0`; dry-run reports four conjunctive cells.

- [ ] **Step 5: Commit**

```bash
git add resources/skills/batuta-routing/SKILL.md tests/contract/test_02_routing_dryrun.sh
git commit -m "feat: route batuta by domain and complexity"
```

---

### Task 2: Conductor inventory and bounded dispatch contract

**Files:**
- Modify: `agents/batuta/AGENT.md`
- Modify: `docs/how-it-works.md`
- Modify: `tests/e2e/SMOKE.md`
- Modify: `tests/contract/test_07_public_docs.sh`

**Interfaces:**
- Consumes: the Task 1 routing skill and daemon provider/model projections.
- Produces: Batuta bootstrap instructions that validate canonical task types, build redacted executor evidence, store the lane matrix, and refuse unbounded delivery.

- [ ] **Step 1: Add failing authored-contract assertions**

Require the published Batuta agent to contain all of:

```text
domain × complexity
backend
frontend
compozy__provider_models_list
resolved_runtime
budget_wall_sec
```

Require it to state that provider presence, model catalog membership, and
credential state are separate evidence, and that secrets/raw config never
enter routing artifacts.

- [ ] **Step 2: Run the focused test and verify RED**

Run: `tests/contract/test_07_public_docs.sh`

Expected: FAIL on the missing domain-matrix and redacted-inventory language.

- [ ] **Step 3: Update the Batuta conductor**

Change principle 2 to domain-and-complexity routing. During bootstrap, validate
every imported task `type` against the closed vocabulary, obtain redacted
provider health and exact model IDs from daemon surfaces, and apply ordered
matrix cells to `implement-tasks`. Preserve the existing delivery preference
gate, worktree safety, one-item/one-commit rule, terminal reporting, and
publication gate.

- [ ] **Step 4: Align public operational documentation**

Update `docs/how-it-works.md` and `tests/e2e/SMOKE.md` to show two task types,
two complexities, distinct resolved runtimes, sequential commit proof, and
the explicit prohibition on concurrent writers in one worktree.

- [ ] **Step 5: Run focused GREEN verification**

Run:

```bash
tests/contract/test_07_public_docs.sh
tests/contract/test_04_deliver_validate.sh
git diff --check
```

Expected: all commands exit `0`.

- [ ] **Step 6: Commit**

```bash
git add agents/batuta/AGENT.md docs/how-it-works.md tests/e2e/SMOKE.md tests/contract
git commit -m "feat: inventory batuta execution lanes"
```

---

### Task 3: Live two-lane evidence and presentation package

**Files:**
- Create: `docs/internal/handoffs/2026-08-25-batuta-next-demo-runbook.md`
- Create: `docs/images/batuta-next-roadmap.png`

**Interfaces:**
- Consumes: Task 1 matrix contract, Task 2 conductor contract, staged Batuta extension, minimal migration-free Compozy Next binary.
- Produces: reproducible local evidence with run IDs/commit SHAs plus one 16:9 roadmap visual and Portuguese presenter sequence.

- [ ] **Step 1: Stage and install the updated extension in an isolated lab**

Create unique directories under `/home/francisross/tmp-builds` for
`COMPOZY_HOME`, staged package, and disposable Git workspace. Configure a
unique HTTP port, start the daemon, install the local stage with explicit
unverified consent, enable Batuta, and confirm `state: active` plus
`health: healthy` through structured reads.

- [ ] **Step 2: Author two disjoint disposable tasks**

Create one `backend/low` task and one `frontend/medium` task. Each must touch
different files, carry a deterministic local test command, and remain ordered
in `_tasks.md` so only one writer commits at a time.

- [ ] **Step 3: Configure and dry-run the matrix**

Choose two live Codex models (prefer `gpt-5.6-luna` and `gpt-5.6-terra`) and
store exact `type + complexity` rules. Configure nonzero wall limits and a
finite iteration cap. Dry-run `implement-tasks` and `batuta-deliver`; capture
the matrix and complete delivery graph.

- [ ] **Step 4: Execute and verify**

Run the two-task `implement-tasks` delivery with `auto_commit=true`. From
durable status, record each cell's `resolved_runtime`, terminal outcome, and
session ID. Verify each authored test and prove exactly two new local commits,
one for each task, with no push.

- [ ] **Step 5: Generate the roadmap image**

Generate a Portuguese 16:9 slide with two bands:

```text
AGORA — catálogo vivo → domínio × complexidade → execução verificada → commit por tarefa
PRÓXIMO — worktree por tarefa → paralelismo seguro → journal/fallback → integração → PR autônomo
```

Use a dark orchestration-console visual language, high contrast, large text,
and no logos or claims that the deferred band is already delivered.

- [ ] **Step 6: Write the runbook and presenter script**

Record exact lab paths, daemon port, commands, run IDs, commit SHAs, expected
outputs, UI URLs, a 5–7 minute Portuguese narrative, and explicit limitations.

- [ ] **Step 7: Run final verification**

Run the full contract suite in a detached disposable worktree without a
`.compozy` marker. Re-run the two authored demo tests, confirm both source
worktrees are clean, and confirm the demo daemon/UI remain healthy.

- [ ] **Step 8: Commit presentation assets**

```bash
git add docs/internal/handoffs/2026-08-25-batuta-next-demo-runbook.md docs/images/batuta-next-roadmap.png
git commit -m "docs: prepare batuta next demo"
```

## Stop condition

Stop when Tasks 1–3 are green and the demo lab is ready. Only if time remains,
write and separately approve a new design for one deferred increment; do not
silently expand this plan.

## Post-demo follow-up: Compozy branch review

After the preview is ready, review the experimental Compozy branch against
`upstream/main` without editing it. Use `caveman-review` and report findings as
`file:Lline: severity: problem. fix.` Focus on the two migrations, API/schema
surface, compatibility, generated churn, stop/budget semantics, and which
commits remain useful under the Batuta-owned fresh-run design. This review does
not block or broaden Tasks 1–3.
