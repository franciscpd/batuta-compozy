# Batuta Reliability Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Batuta preserve delivery preferences, return terminal results to the originating conversation, keep its watcher dormant and reusable, reject missing task sets truthfully, and verify those contracts.

**Architecture:** Keep the extension resource-only. Pass the originating CompozyOS session ID explicitly through `batuta-deliver`; replace the watcher reporting agent with deterministic native Loop actions that read terminal status and queue one idempotent prompt to that session. Keep routing in the stored `implement-tasks` runtime rules, but move the commit preference to the composite Loop that owns child inputs.

**Tech Stack:** CompozyOS `0.3.0-beta.13+`, resource-only extension TOML, Compozy Loop YAML, AGENT.md, Bash contract tests, Python 3 JSON assertions.

## Global Constraints

- Keep the extension resource-only: no subprocess, SDK, Host API, hook, or private database access.
- Never mutate the bundled `implement-tasks` or `review-and-fix` definitions.
- Batuta never edits code, pushes, approves its own gates, or reports a non-success terminal as success.
- Store routing rules on `implement-tasks`; store `auto_commit` on `batuta-deliver`.
- Queue terminal prompts to the explicit originating session with deterministic message identity.
- Treat missing task sets as invalid delivery, never `done`.
- Require a registered current-repository workspace for daemon-backed contract tests.
- Use `apply_patch` for repository edits and preserve unrelated worktree changes.

---

### Task 1: Test workspace contract and compatibility floor

**Files:**
- Create: `tests/contract/lib.sh`
- Modify: `tests/contract/test_01_validate.sh`
- Modify: `tests/contract/test_02_routing_dryrun.sh`
- Modify: `tests/contract/test_04_deliver_validate.sh`
- Modify: `tests/contract/test_05_watch_validate.sh`
- Modify: `extension.toml`

**Interfaces:**
- Produces: `require_test_workspace`, which prints the registered current repository path or exits with a precise setup error.
- Produces: manifest floor `0.3.0-beta.13`.
- Consumes: `BATUTA_TEST_WORKSPACE`; when set, it must resolve to the current repository, not another workspace.

- [ ] **Step 1: Add the failing manifest-floor assertion**

Extend `test_01_validate.sh` after parsing the validation response:

```python
manifest = d.get("manifest") or {}
assert manifest.get("min_compozy_version") == "0.3.0-beta.13", manifest
```

- [ ] **Step 2: Run the focused test and verify RED**

Run: `tests/contract/test_01_validate.sh`

Expected: FAIL because current manifest reports `0.2.0`.

- [ ] **Step 3: Add registered-workspace helper**

Create `tests/contract/lib.sh`:

```bash
#!/usr/bin/env bash

require_test_workspace() {
  local repo_root candidate workspaces_json resolved
  repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
  candidate=${BATUTA_TEST_WORKSPACE:-$repo_root}
  workspaces_json=$(mktemp)

  if ! compozy workspace list -o json > "$workspaces_json"; then
    rm -f -- "$workspaces_json"
    return 1
  fi

  if ! resolved=$(python3 - "$repo_root" "$candidate" "$workspaces_json" <<'PY'
import json
import os
import sys

repo_root = os.path.realpath(sys.argv[1])
candidate = sys.argv[2]
rows = json.load(open(sys.argv[3]))

for row in rows:
    root = os.path.realpath(row["root_dir"])
    if candidate in (row["id"], row.get("name"), row["root_dir"], root):
        if root != repo_root:
            raise SystemExit(
                f"BATUTA_TEST_WORKSPACE resolves to {root}, expected {repo_root}"
            )
        print(row["id"])
        break
else:
    raise SystemExit(
        f"workspace not registered: {repo_root}; run `compozy workspace add {repo_root}`"
    )
PY
  ); then
    rm -f -- "$workspaces_json"
    return 1
  fi

  rm -f -- "$workspaces_json"
  printf '%s\n' "$resolved"
}
```

- [ ] **Step 4: Make daemon-backed tests use the helper**

At the top of tests 02, 04, and 05, after changing to the repository root:

```bash
source tests/contract/lib.sh
WS=$(require_test_workspace)
```

Pass `--workspace "$WS"` to every Loop command.

- [ ] **Step 5: Make the routing fixture collision-safe**

Replace `_routing_probe` with a directory made inside `.compozy/tasks`:

```bash
mkdir -p .compozy/tasks
PROBE_DIR=$(mktemp -d .compozy/tasks/_routing_probe_XXXXXX)
PROBE_SLUG=$(basename "$PROBE_DIR")
TMP_OUT=$(mktemp)
cleanup() {
  rm -rf -- "$PROBE_DIR"
  rm -f -- "$TMP_OUT"
}
trap cleanup EXIT
```

Write the fixture under `PROBE_DIR`, set the manifest workflow to
`$PROBE_SLUG`, and invoke dry-run with `slug=$PROBE_SLUG`. Never remove the
whole `.compozy` directory or a fixed user-owned slug.

- [ ] **Step 6: Raise the manifest floor**

Change `extension.toml` to:

```toml
min_compozy_version = "0.3.0-beta.13"
```

Replace the temporary semver comment with one line stating that this is the
first supported beta for the Loop features used by Batuta.

- [ ] **Step 7: Register the repository when missing, then verify GREEN**

Read first: `compozy workspace list -o json`.

If the exact repository root is absent, run:

```bash
compozy workspace add /home/franciscpd/Projects/batuta-compozy -o json
```

Do not remove the registration afterward; `workspace remove` also removes
stopped session history.

Run:

```bash
tests/contract/test_01_validate.sh
tests/contract/test_02_routing_dryrun.sh
```

Expected: both PASS; no `_routing_probe_*` directory remains.

- [ ] **Step 8: Commit**

```bash
git add extension.toml tests/contract/lib.sh tests/contract/test_01_validate.sh \
  tests/contract/test_02_routing_dryrun.sh \
  tests/contract/test_04_deliver_validate.sh \
  tests/contract/test_05_watch_validate.sh
git commit -m "test: harden workspace and compatibility contracts"
```

---

### Task 2: Composite delivery preference and truthful preflight

**Files:**
- Modify: `tests/contract/test_04_deliver_validate.sh`
- Create: `tests/contract/test_06_agent_contract.sh`
- Modify: `loops/batuta-deliver/loop.yaml`
- Modify: `agents/batuta/AGENT.md`

**Interfaces:**
- `batuta-deliver` inputs become `slug: string`, `origin_session_id: string`, and `auto_commit: boolean`.
- Workspace preference path becomes `loops.inputs.batuta-deliver.auto_commit`.
- The agent must refuse real submission after a task-set dry-run error.

- [ ] **Step 1: Add failing delivery-definition assertions**

Extend the Python assertion block in `test_04_deliver_validate.sh`:

```python
assert "origin_session_id:" in text, "origin session input missing"
assert "required: true" in text.split("origin_session_id:", 1)[1].split("contract:", 1)[0]
assert "on_error:" not in text, "missing task errors must not become success routes"
assert "id: no_tasks" not in text
assert "id: has_tasks" not in text
assert text.count('auto_commit: "{{ .inputs.auto_commit }}"') == 2
```

- [ ] **Step 2: Add failing agent contract test**

Create `tests/contract/test_06_agent_contract.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

python3 - agents/batuta/AGENT.md <<'PY'
import sys

text = open(sys.argv[1]).read()
assert "loops.inputs.batuta-deliver.auto_commit" in text
assert "loops.inputs.implement-tasks.auto_commit" not in text
assert "loops.inputs.review-and-fix.auto_commit" not in text
assert "origin_session_id" in text
assert "task set" in text.lower() and "dry-run" in text
assert "independent" in text.lower()
print("OK: batuta bootstrap and dispatch contracts are explicit")
PY
```

- [ ] **Step 3: Run focused tests and verify RED**

Run:

```bash
tests/contract/test_04_deliver_validate.sh
tests/contract/test_06_agent_contract.sh
```

Expected: both FAIL on missing new contracts.

- [ ] **Step 4: Simplify `batuta-deliver` graph**

Add input:

```yaml
  origin_session_id:
    type: string
    required: true
```

Remove `load_check.on_error`, `no_tasks`, `has_tasks`, and their edges. Keep:

```yaml
  edges:
    - from: slug_input
      to: load_check
    - from: load_check
      to: implement
    - from: implement
      to: review
```

Keep explicit `auto_commit` input mapping on both child nodes. Update comments to
state that missing task sets fail at `import_tasks` and never become successful
delivery.

- [ ] **Step 5: Rewrite bootstrap as independent checks**

In `AGENT.md`, replace the single configured/not-configured shortcut with these
binding rules:

```markdown
Bootstrap checks are independent; one populated value never proves the others:

1. Read the stored `implement-tasks` runtime rules. Derive, confirm, and store
   them only when absent. Apply any confirmed `critical` choice before marking
   routing configured.
2. Read `loops.inputs.batuta-deliver.auto_commit`. When absent, ask once and
   persist the answer at workspace scope. Do not write child Loop input defaults;
   `batuta-deliver` passes this value explicitly to both children.
3. List `batuta-watch` runs. Start one only when no run is `watching` or `running`.
```

Keep live catalog derivation and exact terminal reporting. Remove duplicated
bootstrap prose that no longer changes behavior.

- [ ] **Step 6: Bind dispatch to current session and fail dry-run closed**

Update dispatch instructions:

```markdown
- Inputs: `slug=<feature-slug>` and
  `origin_session_id=<this CompozyOS session ID>`; `auto_commit` resolves from
  `loops.inputs.batuta-deliver.auto_commit`.
- Always dry-run first. If task import reports no matching task set, stop before
  real submission and tell the operator that authored tasks are required.
```

State that terminal wake prompts return to this same session.

- [ ] **Step 7: Verify GREEN**

Run:

```bash
tests/contract/test_04_deliver_validate.sh
tests/contract/test_06_agent_contract.sh
compozy loop validate --file loops/batuta-deliver/loop.yaml \
  --workspace "$(source tests/contract/lib.sh; require_test_workspace)" -o json
```

Expected: all PASS; Loop validation returns `valid: true`.

- [ ] **Step 8: Commit**

```bash
git add agents/batuta/AGENT.md loops/batuta-deliver/loop.yaml \
  tests/contract/test_04_deliver_validate.sh \
  tests/contract/test_06_agent_contract.sh
git commit -m "fix: preserve delivery preference and reject invalid task sets"
```

---

### Task 3: Deterministic return-to-session watcher

**Files:**
- Modify: `tests/contract/test_05_watch_validate.sh`
- Modify: `loops/batuta-watch/loop.yaml`

**Interfaces:**
- Consumes: terminal `watch-events.output.events[]` items.
- Consumes: `batuta-deliver.run.inputs.origin_session_id` from `compozy__loop_status`.
- Produces: one queued prompt admission per terminal run using deterministic identities.

- [ ] **Step 1: Replace watcher assertions with behavioral structure checks**

Keep daemon validation, then assert:

```python
assert 'stop_when: "false"' in text
assert "iteration_cap: 0" in text
assert "kind: run-agent" not in text
assert "kind: fan-out" in text
assert "kind: compozy__loop_status" in text
assert "kind: compozy__session_prompt" in text
assert "kind: collect" in text
assert "mode: queue" in text
assert "origin_session_id" in text
assert text.count("batuta-terminal-{{ .nodes.read_deliver.output.run.id }}") == 2
```

- [ ] **Step 2: Run watcher test and verify RED**

Run: `tests/contract/test_05_watch_validate.sh`

Expected: FAIL because current watcher contains `run-agent` and lacks native prompt delivery.

- [ ] **Step 3: Make watcher contract persistent**

Add:

```yaml
  stop_when: "false"
  iteration_cap: 0
```

Update goal and definition of done to say each observed terminal is queued to its
origin session and the Loop returns to watching. Keep `concurrency: forbid`.

- [ ] **Step 4: Replace isolated agent with deterministic graph**

Use this graph shape:

```yaml
graph:
  nodes:
    - id: deliver_terminal
      class: source
      kind: watch-events
      events:
        - kind: loop.terminal
          filter: "event.loop_name == 'batuta-deliver'"

    - id: terminal_events
      class: control
      kind: fan-out
      collection: "{{ .nodes.deliver_terminal.output.events }}"
      batch_size: 1
      max_parallel: 1
      max_fan_out: 64

    - id: read_deliver
      class: action
      kind: compozy__loop_status
      params:
        run_id: "{{ .item.loop_run_id }}"

    - id: notify_origin
      class: action
      kind: compozy__session_prompt
      params:
        session_id: "{{ .nodes.read_deliver.output.run.inputs.origin_session_id }}"
        message_id: "batuta-terminal-{{ .nodes.read_deliver.output.run.id }}"
        idempotency_key: "batuta-terminal-{{ .nodes.read_deliver.output.run.id }}"
        mode: queue
        message: |
          Batuta delivery run {{ .nodes.read_deliver.output.run.id }} reached
          terminal state {{ .nodes.read_deliver.output.run.status }}. Inspect the
          run with `compozy__loop_status`, then report its exact terminal outcome,
          child run IDs, commits, and blocker in this conversation. Decide any
          redispatch or escalation with the operator. Never approve a gate, edit
          code, push, or mutate routing from this wake prompt.

    - id: delivered
      class: control
      kind: collect

  edges:
    - from: deliver_terminal
      to: terminal_events
    - from: terminal_events
      to: read_deliver
    - from: read_deliver
      to: notify_origin
    - from: notify_origin
      to: delivered
```

- [ ] **Step 5: Validate native tool schemas against current daemon**

Run:

```bash
compozy tool info compozy__loop_status \
  --workspace "$(source tests/contract/lib.sh; require_test_workspace)" -o json
compozy tool info compozy__session_prompt \
  --workspace "$(source tests/contract/lib.sh; require_test_workspace)" -o json
```

Confirm `run_id`; `session_id`, `message`, `message_id`, `idempotency_key`, and
`mode=queue` remain accepted. If schema differs, change YAML and test together to
the live descriptor; do not guess or suppress validation.

- [ ] **Step 6: Verify GREEN**

Run:

```bash
tests/contract/test_05_watch_validate.sh
```

Expected: PASS; daemon validation returns `valid: true`.

- [ ] **Step 7: Commit**

```bash
git add loops/batuta-watch/loop.yaml tests/contract/test_05_watch_validate.sh
git commit -m "fix: return terminal delivery to originating session"
```

---

### Task 4: Documentation and guided behavioral proof

**Files:**
- Modify: `README.md`
- Modify: `README.pt-BR.md`
- Modify: `tests/e2e/SMOKE.md`
- Modify: `docs/superpowers/specs/2026-08-11-batuta-compozy-design.md`
- Modify: `docs/superpowers/plans/2026-08-11-batuta-compozy.md`

**Interfaces:**
- Documents the public `batuta-deliver(slug, origin_session_id, auto_commit)` contract.
- Documents the registered-workspace prerequisite and current extension inventory.

- [ ] **Step 1: Add failing documentation assertions to agent contract test**

Extend `test_06_agent_contract.sh`:

```python
for path in ("README.md", "README.pt-BR.md"):
    doc = open(path).read()
    assert "batuta-deliver" in doc, path
    assert "origin_session_id" in doc, path
    assert "batuta-watch" in doc, path
    assert "0.3.0-beta.13" in doc, path
```

- [ ] **Step 2: Run and verify RED**

Run: `tests/contract/test_06_agent_contract.sh`

Expected: FAIL because current READMEs describe separate child dispatches and omit session binding.

- [ ] **Step 3: Rewrite both README flows**

Document:

- extension inventory contains `batuta`, `batuta-routing`, `batuta-deliver`, and `batuta-watch`;
- providers are derived from `compozy provider models list`, not hard-coded by lane;
- bootstrap stores `auto_commit` on `batuta-deliver`;
- one composite delivery receives the current session ID;
- watcher queues a terminal prompt to that conversation with no reporting agent;
- contract tests require `compozy workspace add <repo>` once;
- minimum supported daemon is `0.3.0-beta.13`.

- [ ] **Step 4: Expand guided E2E smoke**

Add explicit checks:

```markdown
- Run once with `auto_commit=false`; both child inputs must show `false` and no
  implementation/review commit may be created.
- After terminal delivery, the original Batuta session must receive exactly one
  new turn; the watcher must return to `watching`.
- Replaying/observing the same terminal must not create a duplicate message.
- Watcher run detail must contain no isolated `session_id` or `resolved_runtime`
  output for a reporting agent and no watcher model token spend.
- A missing slug must be rejected by dry-run before submission. A deliberate
  direct invalid submission must never end `done`.
```

Record exact run IDs, session ID, prompt admission delivery, watcher status, and
token count when the operator executes the smoke.

- [ ] **Step 5: Mark historical documents superseded**

Add a note after each old title:

```markdown
> Superseded for runtime behavior by
> `docs/superpowers/specs/2026-08-12-batuta-reliability-design.md`.
> Retained as implementation history.
```

Do not rewrite historical decisions as if they were originally current.

- [ ] **Step 6: Verify GREEN and documentation consistency**

Run:

```bash
tests/contract/test_06_agent_contract.sh
rg -n "batuta-deliver|origin_session_id|batuta-watch|0.3.0-beta.13" \
  README.md README.pt-BR.md tests/e2e/SMOKE.md
```

Expected: test PASS; both language variants name the same public contract.

- [ ] **Step 7: Commit**

```bash
git add README.md README.pt-BR.md tests/e2e/SMOKE.md \
  docs/superpowers/specs/2026-08-11-batuta-compozy-design.md \
  docs/superpowers/plans/2026-08-11-batuta-compozy.md \
  tests/contract/test_06_agent_contract.sh
git commit -m "docs: align batuta flow with return-to-session delivery"
```

---

### Task 5: Full verification and local publication

**Files:**
- Review: all changed files
- No source file should change unless verification exposes a root-cause defect.

**Interfaces:**
- Produces a validated, locally active extension and records exact remaining E2E operator work.

- [ ] **Step 1: Run static and contract verification**

```bash
bash -n scripts/republish.sh tests/contract/*.sh
git diff --check
tests/contract/run.sh
```

Expected: all contract tests PASS. `test_03_lifecycle.sh` may report its intentional
SKIP while Batuta is installed; inventory is verified after republication below.

- [ ] **Step 2: Run slop review**

Use the `deslop` skill. Remove duplicated prompt prose, stale comments, weak text
assertions, accidental compatibility language, and unrelated edits. Re-run Step 1
after every change.

- [ ] **Step 3: Republish exact local extension**

Run:

```bash
scripts/republish.sh
```

Expected: extension state `active`; inventory lists all four resources live. This
script removes and reinstalls only the global `batuta` extension and is the repository's
declared local publication workflow.

- [ ] **Step 4: Confirm live definitions**

```bash
compozy extension inventory batuta -o json
compozy loop inspect --workspace /home/franciscpd/Projects/batuta-compozy \
  --name batuta-deliver -o json
compozy loop inspect --workspace /home/franciscpd/Projects/batuta-compozy \
  --name batuta-watch -o json
```

Expected: inventory items all `live: true`; live definitions contain the new
session input and native watcher graph.

- [ ] **Step 5: Run verification-before-completion**

Use `superpowers:verification-before-completion`. Capture fresh outputs for manifest
validation, both Loop validations, shell syntax, contract suite, inventory, and clean
worktree/diff state. Do not claim the guided token-spending E2E was executed unless an
operator actually performs it.

- [ ] **Step 6: Commit any verification-driven correction**

Only when Step 1–5 required a source correction:

```bash
git add <exact-corrected-files>
git commit -m "fix: resolve batuta reliability verification findings"
```

Otherwise create no empty verification commit.
