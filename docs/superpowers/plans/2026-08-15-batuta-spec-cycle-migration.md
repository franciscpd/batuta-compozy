# Batuta Spec-Cycle Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Batuta operate against the canonical `spec-cycle` 0.4.0 skills and extension tool surface shipped by the current CompozyOS build.

**Architecture:** Keep Batuta resource-only and fail closed. Replace the retired PM and task-import contract at the authored agent and Loop boundaries, prove the replacement through the live skill/tool catalog plus deterministic failure behavior, and retain the exact runtime identity in the existing compatibility guard.

**Tech Stack:** Bash contract tests, Python assertions embedded in Bash, CompozyOS CLI/native tool catalog, Compozy Loop YAML, Markdown agent instructions, TOML extension manifest.

## Global Constraints

- The source of truth is `docs/superpowers/specs/2026-08-15-batuta-spec-cycle-migration-design.md`.
- Use `cy-create-spec` for every delivery; a simple request may shorten the grill but may not skip it.
- Required spec artifacts are `_spec.md`, `_user_stories.md`, `_dx.md`, and `_tests.md`; `_uiux.md` exists only for Web-bearing work.
- Use the exact task-import ToolID `ext__spec_cycle__import_tasks` and never fall back to `ext__dev_cycle__import_tasks`.
- Preserve executable requirements byte-for-byte, including the literal `todo 1.0.0`.
- Trust current CompozyOS only as the exact pair `a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c` and `v0.3.0-beta.16-9-ga35eda6d`.
- Preserve the three-resource package inventory: agent `batuta`, skill `batuta-routing`, Loop `batuta-deliver`.
- Preserve preference gating, routing confirmation, one-parent dispatch, event-driven terminal return, non-coding conductor behavior, and the no-push boundary.
- Do not remove the operator's stale `dev-cycle` installation in this workstream.
- Do not change CompozyOS source, session lineage/finalization, provider failover, prices, routing policy, or Web EventSource behavior.
- Keep the pre-existing untracked `.compozy/` out of every diff, package, and commit.
- Use unscoped English conventional commits; do not push.

## File Map

- `scripts/check-compozy-version.sh`: exact trusted CompozyOS version/commit pairs.
- `tests/contract/test_00_runtime_guard.sh`: adversarial acceptance and rejection coverage for the compatibility guard.
- `tests/contract/test_02_spec_cycle_surface.sh`: live bundled-skill/tool discovery plus Batuta's authored unified-PM contract.
- `agents/batuta/AGENT.md`: operator conversation, unified spec handoff, and exact task preflight instructions.
- `loops/batuta-deliver/loop.yaml`: daemon-owned import action before the two existing child Loops.
- `tests/contract/test_04_deliver_validate.sh`: compiled Loop and exact import-action contract.
- `tests/contract/test_06_missing_tasks.sh`: real missing-task failure through the current extension handler.
- `extension.toml`: active product description only; resource inventory and version grammar floor stay unchanged.
- `README.md` and `README.pt-BR.md`: current prerequisite and unified operator flow.
- `tests/e2e/SMOKE.md`: guided visual acceptance for unified artifacts and exact task import.

---

### Task 1: Trust the exact current CompozyOS build

**Files:**
- Modify: `tests/contract/test_00_runtime_guard.sh:56-75`
- Modify: `scripts/check-compozy-version.sh:34-40`

**Interfaces:**
- Consumes: `scripts/check-compozy-version.sh --version VERSION --commit COMMIT`.
- Produces: exact acceptance of the current post-beta.16 build without widening release or descendant matching.

- [ ] **Step 1: Add the failing current-build contract**

Append the current identity cases after the existing `c88b3e52` cases:

```bash
CURRENT_SPEC_CYCLE_COMMIT=a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c

expect_accept \
  "v0.3.0-beta.16-9-ga35eda6d" \
  "$CURRENT_SPEC_CYCLE_COMMIT"
expect_accept "v0.3.0-beta.16-9-ga35eda6d" "a35eda6d"
expect_reject "v0.3.0-beta.16-9-ga35eda6d" "a35eda6"
expect_reject \
  "v0.3.0-beta.16-8-ga35eda6d" \
  "$CURRENT_SPEC_CYCLE_COMMIT"
expect_reject \
  "v0.3.0-beta.16-9-ga35eda6d" \
  "a35eda6d3a2ec47995c19a14a5a01d4f9452cf1d"
```

- [ ] **Step 2: Run the guard contract and verify RED**

Run:

```bash
tests/contract/test_00_runtime_guard.sh
```

Expected: nonzero at the first `v0.3.0-beta.16-9-ga35eda6d` acceptance because `a35eda6d...` is absent from `trusted_post_tag_builds`.

- [ ] **Step 3: Add only the exact trusted mapping**

Add this entry to `trusted_post_tag_builds`:

```python
    "a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c":
        "v0.3.0-beta.16-9-ga35eda6d",
```

Do not change `resolve_trusted_post_tag`, the accepted hash lengths, release-floor logic, or arbitrary-descendant behavior.

- [ ] **Step 4: Run focused compatibility checks and verify GREEN**

Run:

```bash
tests/contract/test_00_runtime_guard.sh
tests/contract/test_00_version_neutral_cwd.sh
scripts/check-compozy-version.sh
```

Expected: all three commands pass; the final command identifies the active binary as an exact trusted test build and does not create repository-local `.compozy` state.

- [ ] **Step 5: Commit the compatibility boundary**

```bash
git add -- scripts/check-compozy-version.sh \
  tests/contract/test_00_runtime_guard.sh
git diff --cached --check
git commit -m "fix: trust current spec-cycle runtime"
```

Expected: two files committed; `git status --short` shows only the preserved `?? .compozy/`.

---

### Task 2: Migrate Batuta's executable contract to spec-cycle

**Files:**
- Create: `tests/contract/test_02_spec_cycle_surface.sh`
- Modify: `agents/batuta/AGENT.md:87-124`
- Modify: `loops/batuta-deliver/loop.yaml:77-86`
- Modify: `tests/contract/test_04_deliver_validate.sh:38-46`
- Modify: `tests/contract/test_06_missing_tasks.sh:39-41`
- Modify: `extension.toml:4`

**Interfaces:**
- Consumes: live skills `cy-create-spec` and `cy-create-tasks`; live tool `ext__spec_cycle__import_tasks`; `.compozy/tasks/<slug>/task_*.md`.
- Produces: one approved unified spec corpus, authored task files, a positive direct task-import preflight, and the unchanged `batuta-deliver` child-Loop chain.

- [ ] **Step 1: Add the live spec-cycle surface contract**

Create `tests/contract/test_02_spec_cycle_surface.sh` with this complete content:

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
source tests/contract/lib.sh
REPO_ROOT=$PWD
REPO_WORKSPACE_PREEXISTED=false
if workspace_marker_present "$REPO_ROOT"; then
  REPO_WORKSPACE_PREEXISTED=true
fi
cleanup() {
  local original_status=$?
  trap - EXIT
  if [[ $REPO_WORKSPACE_PREEXISTED == false ]] && \
    ! cleanup_generated_workspace_marker "$REPO_ROOT"; then
    exit 1
  fi
  exit "$original_status"
}
trap cleanup EXIT
WS=$(require_test_workspace)

for skill in cy-create-spec cy-create-tasks; do
  compozy skill view "$skill" --workspace "$WS" -o json | \
    python3 -c '
import json, sys
expected = sys.argv[1]
data = json.load(sys.stdin)
assert data["name"] == expected, data
assert data["source"] == "bundled", data
assert data["content"].strip(), data
' "$skill"
done

compozy tool info ext__spec_cycle__import_tasks \
  --workspace "$WS" -o json | python3 -c '
import json, sys
tool = json.load(sys.stdin)["tool"]
descriptor = tool["descriptor"]
availability = tool["availability"]
decision = tool["decision"]
assert descriptor["tool_id"] == "ext__spec_cycle__import_tasks", descriptor
assert descriptor["backend"]["extension_id"] == "spec-cycle", descriptor
assert descriptor["backend"]["handler"] == "import_tasks", descriptor
assert descriptor["read_only"] is True, descriptor
assert availability["available"] is True, availability
assert availability["executable"] is True, availability
assert decision["callable"] is True, decision
'

python3 - <<'PY'
from pathlib import Path

agent = Path("agents/batuta/AGENT.md").read_text()
required = (
    "`cy-create-spec`",
    "`_spec.md`",
    "`_user_stories.md`",
    "`_dx.md`",
    "`_tests.md`",
    "`_uiux.md`",
    "`cy-create-tasks`",
    "`ext__spec_cycle__import_tasks`",
)
for value in required:
    assert value in agent, f"missing current PM contract: {value}"
for retired in (
    "`cy-create-prd`",
    "`cy-create-techspec`",
    "`ext__dev_cycle__import_tasks`",
    "skip PRD/TechSpec",
):
    assert retired not in agent, f"retired PM contract remains active: {retired}"
assert "short grill" in agent, "simple requests must shorten, not skip, the grill"
assert "only when the request changes a Web surface" in agent, (
    "_uiux.md must remain conditional"
)
print("OK: Batuta authors the unified spec-cycle PM and preflight contract")
PY
```

Make it executable:

```bash
chmod +x tests/contract/test_02_spec_cycle_surface.sh
```

- [ ] **Step 2: Run the new contract and verify RED at the authored boundary**

Run:

```bash
tests/contract/test_02_spec_cycle_surface.sh
```

Expected: live `cy-create-spec`, `cy-create-tasks`, and `ext__spec_cycle__import_tasks` checks pass, then the authored assertion fails because `AGENT.md` still names `cy-create-prd`/`cy-create-techspec` and the retired ToolID.

- [ ] **Step 3: Record the existing real task-import failure before changing resources**

Run:

```bash
tests/contract/test_06_missing_tasks.sh
```

Expected: nonzero because the retired `ext__dev_cycle__import_tasks` tool is unavailable; preserve the structured availability error in the execution notes.

- [ ] **Step 4: Replace the Phase PM instructions with the unified contract**

Replace the four bullets at `agents/batuta/AGENT.md:97-104` with:

```markdown
- Use `cy-create-spec` for every delivery. A simple, unambiguous request may
  use a short grill, but never skip the grill or unified spec.
- Require operator approval of `_spec.md`, `_user_stories.md`, `_dx.md`, and
  `_tests.md`; require `_uiux.md` only when the request changes a Web surface.
- After spec approval, use `cy-create-tasks` for `_tasks.md` + `task_NN.md`.
  It writes `type` and `complexity` frontmatter per task — that frontmatter is
  what routing matches on, so review the assignments with the operator during
  the interactive approval step.
- Never recreate the retired PRD/TechSpec split. Tasks remain the unit of
  dispatch, commit, and routing.
```

At the direct preflight, replace only the ToolID:

```markdown
2. Call the read-only `ext__spec_cycle__import_tasks` tool directly with
```

Keep literal-requirement preservation, routing, dispatch, return, escalation, and conductor prohibitions byte-for-byte unless wrapping changes are required by formatting.

- [ ] **Step 5: Replace the Loop import action and strengthen its owning test**

Change the `load_check` action in `loops/batuta-deliver/loop.yaml` to:

```yaml
    - id: load_check
      class: action
      kind: ext__spec_cycle__import_tasks
      params:
        pattern: ".compozy/tasks/{{ .inputs.slug }}/task_*.md"
      produces:
        tasks: array
        count: integer
```

Add these assertions to the existing Python block in `tests/contract/test_04_deliver_validate.sh`:

```python
assert "kind: ext__spec_cycle__import_tasks" in text, (
    "spec-cycle import action absent"
)
assert "ext__dev_cycle__import_tasks" not in text, (
    "retired dev-cycle import action remains"
)
```

- [ ] **Step 6: Move the real missing-task contract to the current handler**

In `tests/contract/test_06_missing_tasks.sh`, change the invocation to:

```bash
if compozy tool invoke ext__spec_cycle__import_tasks \
  --workspace "$WS" --input "{\"pattern\":\"$pattern\"}" -o json \
```

Keep the existing assertions for `tool_invalid_input`,
`dependency_missing`, the exact pattern, and `Create the matching task set`.

- [ ] **Step 7: Update the active manifest description without changing inventory**

Set `extension.toml` description to:

```toml
description = "Batuta, the conductor: routes CompozyOS spec-cycle work to the cheapest capable executor and never writes code itself."
```

Do not change `name`, `version`, `min_compozy_version`, or any resource path.

- [ ] **Step 8: Run focused GREEN verification**

Run:

```bash
bash -n tests/contract/test_02_spec_cycle_surface.sh \
  tests/contract/test_04_deliver_validate.sh \
  tests/contract/test_06_missing_tasks.sh
tests/contract/test_02_spec_cycle_surface.sh
tests/contract/test_04_deliver_validate.sh
tests/contract/test_06_missing_tasks.sh
tests/contract/test_01_validate.sh
tests/contract/test_01_stage.sh
tests/contract/test_01_package.sh
```

Expected: all commands pass; Loop validation compiles the current action, the missing-task call reaches the current handler and fails for the missing dependency rather than extension availability, and package inventory remains exactly three resources.

- [ ] **Step 9: Commit the executable migration**

```bash
git add -- agents/batuta/AGENT.md extension.toml \
  loops/batuta-deliver/loop.yaml \
  tests/contract/test_02_spec_cycle_surface.sh \
  tests/contract/test_04_deliver_validate.sh \
  tests/contract/test_06_missing_tasks.sh
git diff --cached --check
git commit -m "fix: migrate Batuta to spec-cycle"
```

Expected: six files committed; `.compozy/` is not staged.

---

### Task 3: Document the unified operator flow and visual contract

**Files:**
- Modify: `README.md:5-31,83-95`
- Modify: `README.pt-BR.md:5-31,85-98`
- Modify: `tests/e2e/SMOKE.md:15-24,78-82`

**Interfaces:**
- Consumes: the executable contract from Task 2.
- Produces: matching English/Portuguese operator guidance and an exact visual artifact checklist.

- [ ] **Step 1: Update the English active documentation**

In `README.md`:

- change “dev-cycle” to “spec-cycle” in the introduction;
- point “Current design” to `docs/superpowers/specs/2026-08-15-batuta-spec-cycle-migration-design.md`;
- name bundled `spec-cycle` 0.4.0 as the prerequisite;
- replace the old flow sentence with:

```markdown
Flow: requirements and unified spec via `cy-create-spec` → operator approval
of `_spec.md`, `_user_stories.md`, `_dx.md`, `_tests.md`, and `_uiux.md` only
for Web-bearing work → tasks via `cy-create-tasks` → direct read-only task
import preflight → Loop dry-run (planning only) → dispatch of
`batuta-deliver(slug, origin_session_id, auto_commit)` → bundled
`implement-tasks` → `review-and-fix` → exact terminal outcome.
```

Add one sentence after the flow: a simple request may use a short grill but may not skip `cy-create-spec` or task creation.

- [ ] **Step 2: Mirror the contract in Portuguese**

In `README.pt-BR.md`, make the same structural changes and use:

```markdown
Fluxo: requisitos e spec unificada via `cy-create-spec` → aprovação pelo
operador de `_spec.md`, `_user_stories.md`, `_dx.md`, `_tests.md` e de
`_uiux.md` somente quando houver mudança Web → tasks via `cy-create-tasks` →
preflight direto e somente leitura da importação de tasks → dry-run do Loop
(apenas planejamento) → despacho de
`batuta-deliver(slug, origin_session_id, auto_commit)` →
`implement-tasks` bundled → `review-and-fix` → resultado terminal exato.
```

Add the equivalent sentence: um pedido simples pode ter um grill curto, mas não pode pular `cy-create-spec` nem a criação de tasks.

- [ ] **Step 3: Update the guided visual PM and literal-artifact checks**

Replace Smoke step 4 with:

```markdown
4. **Fase PM**: o Batuta conduz `cy-create-spec`, inclusive para uma feature
   pequena. Aceite: o grill pode ser curto, mas não é pulado; o operador
   aprova `_spec.md`, `_user_stories.md`, `_dx.md` e `_tests.md`, com
   `_uiux.md` somente se houver mudança Web. Depois o Batuta conduz
   `cy-create-tasks`; `.compozy/tasks/<slug>/` contém `_tasks.md` +
   `task_NN.md`, cada task com `complexity`, e o breakdown é apresentado para
   aprovação em conversa.
```

Replace the literal `todo 1.0.0` artifact list with:

```markdown
- Peça uma feature que exija literalmente `todo 1.0.0`. Confirme que
  `_spec.md`, `_user_stories.md`, `_dx.md`, `_tests.md`, `_tasks.md`, cada
  `task_NN.md` e os prompts de execução aplicáveis preservam `todo 1.0.0` sem
  upgrade, normalização ou paráfrase. Para mudança Web, inclua `_uiux.md` na
  mesma verificação.
```

In dispatch step 5, name `ext__spec_cycle__import_tasks` explicitly.

- [ ] **Step 4: Check active documentation consistency**

Run:

```bash
rg -n "dev-cycle|cy-create-prd|cy-create-techspec|ext__dev_cycle" \
  README.md README.pt-BR.md agents/batuta/AGENT.md \
  loops/batuta-deliver/loop.yaml tests/e2e/SMOKE.md extension.toml
rg -n "cy-create-spec|cy-create-tasks|ext__spec_cycle__import_tasks" \
  README.md README.pt-BR.md agents/batuta/AGENT.md \
  loops/batuta-deliver/loop.yaml tests/e2e/SMOKE.md extension.toml
git diff --check
```

Expected: the first command returns no matches; the second finds the unified PM flow and exact preflight across the active surfaces; diff check is silent. Historical specs/plans are intentionally outside these commands.

- [ ] **Step 5: Commit the active documentation**

```bash
git add -- README.md README.pt-BR.md tests/e2e/SMOKE.md
git diff --cached --check
git commit -m "docs: document Batuta spec-cycle flow"
```

Expected: three files committed and no historical document rewritten.

---

### Task 4: Run aggregate contracts and review the complete migration

**Files:**
- Verify all files changed since `e9ea1db273f2e3f698921513358066ceaeeef8b1`.
- Do not create a file solely to record a passing command.

**Interfaces:**
- Consumes: Tasks 1–3 commits.
- Produces: a clean, reviewable Batuta candidate package with automated evidence.

- [ ] **Step 1: Run syntax and Python validator tests**

```bash
bash -n scripts/*.sh tests/contract/*.sh
python3 -m unittest discover -s tests/e2e -p 'test_*.py' -v
```

Expected: Bash syntax passes and every event-driven validator unit test passes.

- [ ] **Step 2: Run the complete contract suite against the current daemon**

```bash
if [[ -e .compozy || -L .compozy ]]; then
  BATUTA_STATE_HOLD=$(mktemp -d /tmp/batuta-contract-state.XXXXXX)
  mv -- .compozy "$BATUTA_STATE_HOLD/repo-compozy"
  restore_batuta_state() {
    if [[ -e .compozy || -L .compozy ]]; then
      printf 'contract suite left unexpected .compozy state\n' >&2
      return 1
    fi
    mv -- "$BATUTA_STATE_HOLD/repo-compozy" .compozy
    rmdir -- "$BATUTA_STATE_HOLD"
  }
  trap restore_batuta_state EXIT
  tests/contract/run.sh
  restore_batuta_state
  trap - EXIT
else
  tests/contract/run.sh
fi
```

Expected: every contract passes, the runner prints `todos os testes de contrato passaram`, removes any workspace registration it created, and leaves no newly generated repository `.compozy` state. The preserved pre-existing state returns byte-for-byte to `.compozy/`; it is never deleted or committed.

- [ ] **Step 3: Rebuild and inspect the retained package**

```bash
PACKAGE_ROOT=$(mktemp -d /tmp/batuta-spec-cycle-package.XXXXXX)
BATUTA_PACKAGE_ROOT="$PACKAGE_ROOT" scripts/package-extension.sh
PACKAGE_DIR=$(BATUTA_PACKAGE_ROOT="$PACKAGE_ROOT" scripts/package-extension.sh)
find "$PACKAGE_DIR" -type f -print | LC_ALL=C sort
compozy extension validate "$PACKAGE_DIR" -o json
```

Expected: the same content digest is returned twice, validation has no error-severity issue, and the package contains only `extension.toml` plus the agent, routing skill, and delivery Loop. Remove the explicit `/tmp/batuta-spec-cycle-package.*` directory after inspection.

```bash
case "$PACKAGE_ROOT" in
  /tmp/batuta-spec-cycle-package.*)
    chmod -R u+w -- "$PACKAGE_ROOT"
    rm -rf -- "$PACKAGE_ROOT"
    ;;
  *)
    printf 'refusing unexpected package cleanup path: %s\n' \
      "$PACKAGE_ROOT" >&2
    exit 1
    ;;
esac
```

- [ ] **Step 4: Run deslop and inspect the branch diff**

Use the `deslop` skill against:

```bash
git diff e9ea1db273f2e3f698921513358066ceaeeef8b1..HEAD
```

Then run:

```bash
git diff --check e9ea1db273f2e3f698921513358066ceaeeef8b1..HEAD
git status --short
git log --oneline e9ea1db273f2e3f698921513358066ceaeeef8b1..HEAD
```

Expected: no compatibility shim, duplicated retired terminology, vague instruction, weakened guard, unrelated change, or unfinished marker; three implementation commits follow unscoped English conventions; only the preserved `?? .compozy/` remains outside Git.

- [ ] **Step 5: Correct only evidence-backed defects**

For each real defect found in Steps 1–4, add or strengthen its owning test first, reproduce RED, make the smallest production/documentation correction, rerun the focused test and complete suite, then create one unscoped `fix:`, `test:`, or `docs:` commit. Do not create an empty verification commit and do not amend reviewed history merely to conceal a correction.

---

### Task 5: Prove isolated behavior and prepare the operator's visual smoke

**Files:**
- Runtime evidence only under a fresh lab's `qa-artifacts/qa/`.
- Candidate package sourced from the clean Batuta branch.
- Operator workspace remains unchanged until the explicit republication step.

**Interfaces:**
- Consumes: active CompozyOS binary SHA-256 `c0a9b6586cdc257ca0dd2953d05c8c409a0d5c3819f773ca163e76d303628ea4`, candidate Batuta package, bundled `spec-cycle` 0.4.0.
- Produces: isolated proof, clean teardown, then a controlled live candidate for the user's visual test.

- [ ] **Step 1: Re-identify the exact CompozyOS binary before QA**

```bash
/home/franciscpd/.local/bin/compozy version -o json
sha256sum /home/franciscpd/.local/bin/compozy
```

Expected: version `v0.3.0-beta.16-9-ga35eda6d`, commit `a35eda6d`, and SHA-256 `c0a9b6586cdc257ca0dd2953d05c8c409a0d5c3819f773ca163e76d303628ea4`. Stop if any field differs and update the compatibility design rather than silently trusting another build.

- [ ] **Step 2: Bootstrap a fresh targeted lab**

From `/home/franciscpd/Projects/compozy`, run:

```bash
python3 .agents/skills/eng/eng-qa-bootstrap/scripts/bootstrap-qa-env.py \
  --scenario "batuta-spec-cycle-migration" \
  --repo-root . \
  --profile targeted \
  --required-surface runtime \
  --required-surface provider \
  --required-surface cli
```

Record the emitted `BOOTSTRAP_MANIFEST`, source only its `bootstrap.env`, use the exact binary from Step 1, and register every long-lived daemon/provider PID under the manifest's `qa-artifacts/qa/pids/` directory.

- [ ] **Step 3: Publish the candidate only inside the isolated home**

Under the manifest-derived environment, run the candidate's normal package/validation/install/enable flow and capture:

```bash
scripts/check-compozy-version.sh
PACKAGE_DIR=$(scripts/package-extension.sh)
compozy extension validate "$PACKAGE_DIR" -o json
compozy extension install "$PACKAGE_DIR" --allow-unverified --yes -o json
compozy extension enable batuta -o json
compozy extension inventory batuta -o json
compozy extension list -o json
```

Expected: Batuta is active/healthy with exactly three live resources; `spec-cycle` 0.4.0 is active/healthy; candidate publication does not touch the operator's default `COMPOZY_HOME`.

- [ ] **Step 4: Prove the unified surface through public commands**

In the lab workspace, capture:

```bash
compozy skill view cy-create-spec --workspace "$WORKSPACE_ID" -o json
compozy skill view cy-create-tasks --workspace "$WORKSPACE_ID" -o json
compozy tool info ext__spec_cycle__import_tasks \
  --workspace "$WORKSPACE_ID" -o json
```

Expected: both skills resolve from `bundled`; the tool is owned by `spec-cycle`, read-only, available, executable, and callable. Confirm the public agent/tool catalog does not require or select `dev-cycle` for Batuta's task preflight.

- [ ] **Step 5: Exercise the smallest real provider PM journey**

Create and register a fresh no-remote fixture under the lab root:

```bash
FIXTURE="$WORKSPACE_PATH/batuta-spec-cycle-fixture"
mkdir -p -- "$FIXTURE"
git -C "$FIXTURE" init
git -C "$FIXTURE" config user.name "Batuta QA"
git -C "$FIXTURE" config user.email "batuta-qa@example.invalid"
```

Use `apply_patch` to create `$FIXTURE/README.md` with this content:

```markdown
# Version CLI fixture

This no-remote fixture is intentionally small. It needs a version subcommand,
tests, and the exact dependency requirement `todo 1.0.0`.
```

Then register it and create the session through public CLI surfaces:

```bash
git -C "$FIXTURE" add -- README.md
git -C "$FIXTURE" commit -m "build: create visual fixture"
WORKSPACE_JSON=$(compozy workspace add "$FIXTURE" \
  --name "batuta-spec-cycle-fixture-$SCENARIO_SLUG" \
  --default-agent batuta -o json)
WORKSPACE_ID=$(python3 -c \
  'import json,sys; print(json.load(sys.stdin)["id"])' \
  <<<"$WORKSPACE_JSON")
SESSION_JSON=$(compozy session new --workspace "$WORKSPACE_ID" \
  --agent batuta --name "Batuta spec-cycle QA" -o json)
SESSION_ID=$(python3 -c \
  'import json,sys; print(json.load(sys.stdin)["id"])' \
  <<<"$SESSION_JSON")
```

Prompt that exact session:

```text
Crie uma feature mínima de linha de comando que preserve literalmente todo 1.0.0. Não escreva código ainda; conduza a especificação e pare após criar e apresentar as tasks para minha aprovação.
```

```bash
compozy session prompt "$SESSION_ID" \
  "Crie uma feature mínima de linha de comando que preserve literalmente todo 1.0.0. Não escreva código ainda; conduza a especificação e pare após criar e apresentar as tasks para minha aprovação." \
  -o json
```

Choose `auto_commit=false` if the preference is absent. Capture public session events and the created artifact tree.

Expected: the preference gate completes first; Batuta invokes `cy-create-spec`, conducts the required grill, creates `_spec.md`, `_user_stories.md`, `_dx.md`, and `_tests.md` without `_uiux.md` for this non-Web request, waits for spec approval, invokes `cy-create-tasks`, creates `_tasks.md` and at least one `task_NN.md`, preserves `todo 1.0.0` literally everywhere applicable, and writes no implementation code.

- [ ] **Step 6: Prove task preflight, dry-run, and candidate Loop wiring**

After approving the authored tasks, require Batuta to continue only through preflight and dry-run. Capture the tool events and Loop result.

Expected: direct `ext__spec_cycle__import_tasks` returns `count > 0`; no `ext__dev_cycle__import_tasks` call occurs; `batuta-deliver` dry-run resolves `load_check` to the current action and plans `implement-tasks` followed by `review-and-fix`; no real run or code edit occurs at this checkpoint.

- [ ] **Step 7: Audit and tear down on every result**

Append the real actions to `journey-log.jsonl`, run the manifest's strict audit, and execute its exact teardown command on PASS, FAIL, BLOCKED, or abort:

```bash
python3 "$AUDIT_COMMAND" --qa-output-path "$QA_OUTPUT_PATH" --strict
eval "$TEARDOWN_COMMAND"
```

Expected: the audit contains no blocker for runtime/provider/CLI surfaces and `teardown.json` records `"clean": true` with no survivors. Do not continue to the operator environment without clean teardown.

- [ ] **Step 8: Preserve the current live Batuta source before republication**

In the operator environment, capture these read-only records under a timestamped directory in `/home/franciscpd/.local/state/batuta-compozy/rollouts/`:

```bash
compozy extension list -o json
compozy extension inventory batuta -o json
compozy status -o json
```

Resolve and record the currently installed Batuta source path and package digest from the extension detail/list output. Verify the retained directory exists and is readable before replacing the live package; that exact source is the rollback target.

- [ ] **Step 9: Republish Batuta and hand the clean visual prompt to the operator**

Run from the candidate worktree:

```bash
scripts/republish.sh
compozy extension inventory batuta -o json
compozy extension list -o json
```

Expected: Batuta and `spec-cycle` are active/healthy and the Batuta inventory is exactly three live resources. Do not remove the stale `dev-cycle` installation.

Reset only the visual fixture workspace's authored `.compozy/tasks/<slug>/` artifacts and workspace-scoped `loops.inputs.batuta-deliver.auto_commit` value, then give the operator this first prompt:

```text
Quero adicionar um subcomando de versão a este projeto usando literalmente todo 1.0.0. Conduza todo o planejamento comigo, apresente os artefatos para aprovação e não implemente nada antes de eu aprovar as tasks.
```

Expected visual sequence: exact preference gate; `cy-create-spec` grill; unified artifacts; explicit spec approval; `cy-create-tasks`; task approval; current spec-cycle import preflight. Stop and preserve evidence at the first divergence.

- [ ] **Step 10: Roll back only if the candidate fails**

If the candidate fails the visual contract, drain/stop Batuta activity, remove only the failed Batuta installation, reinstall the exact retained source recorded in Step 8 with `--allow-unverified --yes`, enable it, and verify the same exact three-resource inventory. Do not change the CompozyOS binary or remove `spec-cycle`/`dev-cycle` as part of Batuta rollback.

- [ ] **Step 11: Report the migration evidence and deferred work**

Report:

- all Batuta commit IDs and exact changed-file count since `e9ea1db`;
- active CompozyOS version, full commit, and checksum;
- candidate package digest and exact live inventory;
- runtime-guard RED/GREEN and complete contract-suite result;
- isolated session/workspace IDs, unified artifact list, literal requirement proof, direct ToolID, and dry-run result;
- manifest, strict audit, and clean teardown paths;
- visual verdict and rollback status;
- confirmation that no push occurred and `.compozy/` was excluded from commits;
- the deferred investigations: run-agent child-session lineage, deterministic run-owned session finalization, provider/model fallback with typed causes, and price-catalog freshness.

---

## Final Self-Review Checklist

- [ ] Every design requirement maps to one task and one automated or guided proof.
- [ ] Live catalog assertions accompany the authored-agent checks.
- [ ] The retired `dev-cycle` handler is absent from every active Batuta surface.
- [ ] Historical design/plan documents retain their original terminology.
- [ ] Simple requests shorten the `cy-create-spec` grill but never skip it.
- [ ] `_uiux.md` remains conditional on Web-bearing work.
- [ ] The exact current version/commit pair is accepted without widening hash or descendant rules.
- [ ] Missing tasks fail through the current handler with `tool_invalid_input` and `dependency_missing`.
- [ ] The Loop still contains one import action and exactly two ordered `run-loop` children.
- [ ] Package inventory remains exactly three resources.
- [ ] Preference, routing, literal-requirement, event-return, no-code, no-push, and no-self-approval boundaries remain intact.
- [ ] `.compozy/` is absent from every commit and retained package.
- [ ] Isolated teardown is `clean: true` before global republication.
- [ ] Visual smoke stops at the first divergence and rollback uses the exact prior Batuta package.
- [ ] Session nesting/finalization, provider fallback, prices, and EventSource behavior remain outside this migration.
