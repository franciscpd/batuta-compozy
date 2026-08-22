# Batuta Delivery Contract Robustness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give `batuta-deliver` a real wall-clock budget, define the conductor's partial-run redispatch audit, document `auto_commit=false` consequences, and scope the preference gate to delivery-path calls.

**Architecture:** The budget becomes a declared Loop input (`wall_clock_sec`, default 14400) referenced by the contract, riding the daemon's per-key input layering. Everything else is conductor-contract and docs text pinned by the existing bash+python contract tests and validated by the preference-gate e2e assertions.

**Tech Stack:** CompozyOS Loop YAML, markdown agent contracts, bash+python contract tests, python e2e event assertions.

**Spec:** `docs/internal/specs/2026-08-21-batuta-contract-robustness-design.md`

## Global Constraints

- Execute this plan AFTER `docs/internal/plans/2026-08-21-batuta-worktree-and-gated-publication.md`; both edit `loops/batuta-deliver/loop.yaml` and `agents/batuta/AGENT.md`, and this plan's docs reference publication routing.
- Never run contract tests from a checkout containing `.compozy/`; use the disposable detached-worktree recipe:
  `wt=$(mktemp -d /tmp/batuta-plan-wt.XXXXXX) && git worktree add --detach "$wt" HEAD && (cd "$wt" && tests/contract/<test>.sh); status=$?; git worktree remove --force "$wt"; exit $status`
- Contract tests need a running CompozyOS daemon; if unavailable, STOP and report.
- The conductor prohibition list ("What you never do") stays byte-identical.
- The preference-gate protocol mechanics (exact path `loops.inputs.batuta-deliver.auto_commit`, workspace scope, persist-and-reread on `config_path_not_found`, literal `false`) are unchanged — only WHEN the gate must run changes.

---

### Task 1: Budget as a Loop input

**Files:**
- Modify: `loops/batuta-deliver/loop.yaml`
- Test: `tests/contract/test_04_deliver_validate.sh`

**Interfaces:**
- Produces: declared input `wall_clock_sec` (integer, default 14400) referenced by `contract.budget.wall_clock_sec`; the input name is what the conductor's raised-budget procedure (Task 3) and docs (Task 4) cite.

- [ ] **Step 1: Failing asserts** in test_04's text-assert python block:

```python
assert "wall_clock_sec:" in text, "input wall_clock_sec ausente"
assert "14400" in text, "default de 4h ausente"
assert "wall_clock_sec: 0" not in text, "budget ilimitado permanece"
```

- [ ] **Step 2: Run from a detached worktree; expect FAIL.**

- [ ] **Step 3: Edit the YAML.** Add under `inputs:`:

```yaml
  wall_clock_sec:
    type: integer
    default: 14400
```

Change the contract budget to:

```yaml
  budget:
    tokens: 0
    wall_clock_sec: "{{ .inputs.wall_clock_sec }}"
    on_exceeded: halt
```

- [ ] **Step 4: Run; the daemon decides the grammar.** If `compozy loop validate` rejects a template in the numeric contract field, apply the spec's recorded fallback: revert the input, set the literal `wall_clock_sec: 14400`, delete the `"input wall_clock_sec ausente"` assert (keep the other two), and note "budget input template unsupported; fixed 14400 fallback" in the commit message. Expected: PASS either way.

- [ ] **Step 5: Commit** `feat: bound batuta-deliver with a 4h wall-clock budget`

---

### Task 2: Preference gate scoped to delivery-path calls

**Files:**
- Modify: `agents/batuta/AGENT.md` (the "Delivery preference gate" section)
- Test: `tests/e2e/assert_preference_gate.py`, `tests/e2e/test_assert_event_driven_return.py` pattern for offline runs

**Interfaces:**
- Produces: the delivery-path call list that Task 3's audit procedure and Task 4's docs repeat verbatim: `ext__spec_cycle__import_tasks` preflight, worktree creation, `batuta-deliver` dry-run, real dispatch.

- [ ] **Step 1: Rewrite the section's opening rules.** Replace the two paragraphs from "Open this gate before every other tool call in a new session" through "inspect catalogs, or call another tool." with:

```markdown
Open this gate before the session's first delivery-path tool call, and
repeat its read before every dispatch. The delivery-path calls are exactly:
the `ext__spec_cycle__import_tasks` preflight, delivery worktree creation,
a `batuta-deliver` dry-run, and a real dispatch. None of them may run
before the gate is open.

Purely conversational turns — questions, reports, status reads, and
spec-cycle requirement authoring (`cy-create-spec`, `cy-create-tasks`) with
their approval dialogues — need no config read: authored artifacts do not
depend on the commit preference.
```

Keep every following rule of the section (structured boolean, `config_path_not_found` question, persist-and-reread, literal `false`, error stop) unchanged, and update its closing sentence to: "Only after the gate opens may preflight, worktree creation, dry-runs, or dispatch begin."

- [ ] **Step 2: Align `tests/e2e/assert_preference_gate.py`.** Read its `validate()` ordering asserts: wherever it requires the `config_get` of `loops.inputs.batuta-deliver.auto_commit` to be the first tool call of the session, relax that to "precedes the first of `ext__spec_cycle__import_tasks`, `compozy__worktree_create`, or `compozy__loop_run`" while keeping the persist-and-reread sequence asserts intact. Add one negative assert: no delivery-path tool call appears before the gate's final reread.

- [ ] **Step 3: Offline-test the assert script** with synthetic event fixtures (mirror `test_assert_event_driven_return.py`): one passing session where a conversational turn precedes the gate, one failing session where `import_tasks` precedes the gate. Run `python3 -m pytest tests/e2e/ -q`; expect PASS.

- [ ] **Step 4: Commit** `feat: scope the preference gate to delivery-path calls`

---

### Task 3: Partial-run audit and exhausted-budget procedure

**Files:**
- Modify: `agents/batuta/AGENT.md` (Escalation section)
- Test: `tests/contract/test_05_return_validate.sh` (run to prove the dispatch pins survived)

**Interfaces:**
- Consumes: `wall_clock_sec` input name (Task 1), delivery-path list (Task 2).

- [ ] **Step 1: Add to the Escalation section**, after the repeated-failure bullet:

```markdown
- On EVERY non-success terminal where the implement child may have started
  — `failed`, `exhausted`, `stalled`, `canceled`, and `blocked` alike —
  audit before any redispatch: read `compozy__loop_nodes` for the implement
  child and report per task its terminal state and commit evidence (landed
  or not) on the delivery branch. State explicitly that a redispatch
  re-executes the full task set and may re-apply already-landed tasks, then
  decide with the operator: redispatch, amend the task set first, or stop.
- `exhausted` means the wall-clock budget halted the run — likelier a stuck
  run than a long one. After the audit above, a legitimately long delivery
  is redispatched with a raised `wall_clock_sec` run input; the dry-run
  reports the effective value and its origin, and you state it to the
  operator before submitting the real run with identical inputs. Human-gate
  residence never consumes this budget — the daemon suspends the wall clock
  during approval waits.
```

- [ ] **Step 2: Run test_05 from a detached worktree; expect PASS** (Escalation edits must not trip the dispatch-section watcher regexes — the added text contains no "poll"/"keep watching" phrasing near "successful real dispatch").

- [ ] **Step 3: Commit** `feat: partial-run audit and exhausted-budget redispatch procedure`

---

### Task 4: `auto_commit=false` consequences and public docs

**Files:**
- Modify: `agents/batuta/AGENT.md` (Delivery preference gate section), `docs/how-it-works.md`, `README.md`, `README.pt-BR.md`
- Test: `tests/contract/test_07_public_docs.sh`

**Interfaces:**
- Consumes: publication routing vocabulary from the companion plan's Task 7 docs.

- [ ] **Step 1: Failing doc pins** in `test_07_public_docs.sh`: add to the how-it-works list `'wall-clock budget'` and `'auto_commit=false'`.

- [ ] **Step 2: Run; expect FAIL.**

- [ ] **Step 3: Write the text.**
  - AGENT.md, preference-gate section, after the persist-and-reread rules:

```markdown
When the operator chooses or confirms `false`, state the consequences
before persisting: implement runs leave changes uncommitted in the delivery
worktree; review-and-fix reviews the working tree without commit boundaries
between tasks; publication routes on branch-vs-base commit evidence, so a
fresh `false` delivery skips the gate while commits inherited from a reused
worktree remain publishable; integrating uncommitted work is fully manual.
```

  - `docs/how-it-works.md`: extend section 1 (preference gate) with the delivery-path scoping and the `false` consequences paragraph; extend the escalation section with the any-terminal audit and the exhausted/raised-budget procedure; add one sentence on the 4-hour wall-clock budget default (`wall_clock_sec`, per-dispatch raise, gate residence exempt).
  - Both READMEs: one sentence noting deliveries carry a 4-hour wall-clock budget raised per dispatch when needed.

- [ ] **Step 4: Run test_07_public_docs.sh; expect PASS.**

- [ ] **Step 5: Run the full contract suite once from a detached worktree** (`tests/contract/run.sh`) as the plan's closing verification; expect all green. Report any red literally.

- [ ] **Step 6: Commit** `docs: budget, audit, and auto_commit=false contracts in reading order`
