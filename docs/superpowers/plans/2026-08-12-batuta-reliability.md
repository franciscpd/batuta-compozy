# Batuta Reliability Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Batuta preserve delivery preferences, return terminal results to the originating conversation through native effects, reject missing task sets truthfully, and verify those contracts.

**Architecture:** Keep the extension resource-only. Pass the originating CompozyOS session ID explicitly through `batuta-deliver`; use all seven contract terminal effects to queue one idempotent prompt to that session. Keep routing in the stored `implement-tasks` runtime rules, but move the commit preference to the composite Loop that owns child inputs. No watcher or reporting agent remains.

**Tech Stack:** CompozyOS manifest grammar floor `0.3.0-beta.13` with a
post-tag operational guard, resource-only extension TOML, Compozy Loop YAML,
AGENT.md, Bash contract tests, Python 3 JSON assertions.

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
- Modify: `tests/contract/test_05_watch_validate.sh` (renamed in Task 3)
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

At the top of tests 02, 04, and the then-current 05 watcher test, after changing
to the repository root:

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
- Modify: `loops/batuta-deliver/loop.yaml`
- Modify: `agents/batuta/AGENT.md`
- Modify: `tests/e2e/SMOKE.md`

**Interfaces:**
- `batuta-deliver` inputs become `slug: string`, `origin_session_id: string`, and `auto_commit: boolean`.
- Workspace preference path becomes `loops.inputs.batuta-deliver.auto_commit`.
- The agent must refuse real submission after a task-set dry-run error.

- [ ] **Step 1: Record RED behavior from public surfaces**

Run:

```bash
compozy config get loops.inputs.batuta-deliver.auto_commit \
  --workspace "$(source tests/contract/lib.sh; require_test_workspace)" -o json
compozy loop status --workspace ws_13d8f64cc29e9d5c \
  --run-id looprun-0e0226eee2324cea -o json
```

Expected RED evidence: input default path is missing; historical invalid task-set
run reports `status: done`. Save the bounded command outputs in the task report.

- [ ] **Step 2: Simplify `batuta-deliver` graph**

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

- [ ] **Step 3: Rewrite bootstrap as independent checks**

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
```

Keep live catalog derivation and exact terminal reporting. Remove duplicated
bootstrap prose that no longer changes behavior.

- [ ] **Step 4: Bind dispatch to current session and fail dry-run closed**

Update dispatch instructions:

```markdown
- Inputs: `slug=<feature-slug>` and
  `origin_session_id=<this CompozyOS session ID>`; `auto_commit` resolves from
  `loops.inputs.batuta-deliver.auto_commit`.
- Always dry-run first. If task import reports no matching task set, stop before
  real submission and tell the operator that authored tasks are required.
```

State that terminal effects return prompts to this same session.

- [ ] **Step 5: Add guided behavioral cases before running implementation**

Add these acceptance cases to `tests/e2e/SMOKE.md` before live verification:

```markdown
- Configure `auto_commit=false`, dispatch through Batuta, and confirm both child
  runs persist `inputs.auto_commit=false`.
- Ask Batuta to dispatch a missing slug. Confirm dry-run fails and no real
  `batuta-deliver` run is created.
- Submit one deliberate direct invalid run. Confirm its terminal is not `done`.
```

These cases test consuming-agent and daemon behavior. Do not replace them with
grep assertions against YAML or AGENT.md.

- [ ] **Step 6: Verify compiled configuration**

Run:

```bash
compozy loop validate --file loops/batuta-deliver/loop.yaml \
  --workspace "$(source tests/contract/lib.sh; require_test_workspace)" -o json
```

Expected: Loop validation returns `valid: true`. The live behavioral cases remain
pending until Task 5 republication; do not claim them from source inspection.

- [ ] **Step 7: Commit**

```bash
git add agents/batuta/AGENT.md loops/batuta-deliver/loop.yaml tests/e2e/SMOKE.md
git commit -m "fix: preserve delivery preference and reject invalid task sets"
```

---

### Task 3: Deterministic return-to-session terminal effects

> **Root-cause amendment — 2026-08-12:** The live output descriptor and Loop
> compiler rejected the planned watcher data paths. The steps below supersede
> that graph and record the implemented terminal-effect design.

**Files:**
- Modify: `loops/batuta-deliver/loop.yaml`
- Delete: `loops/batuta-watch/loop.yaml`
- Modify: `agents/batuta/AGENT.md`
- Delete: `tests/contract/test_05_watch_validate.sh`
- Create: `tests/contract/test_05_return_validate.sh`
- Modify: `tests/e2e/SMOKE.md`

**Interfaces:**
- Consumes: `inputs.origin_session_id` in `batuta-deliver`.
- Consumes: terminal effect context
  `effect.identity.loop_run_id` and `effect.identity.trigger`.
- Produces: one queued `compozy__session_prompt` admission per terminal run,
  using run-derived message and idempotency identities.

- [ ] **Step 1: Record RED behavior from historical live watcher state**

Run:

```bash
compozy loop status --workspace ws_13d8f64cc29e9d5c \
  --run-id looprun-1eef4226a7d3b11b -o json
```

Expected bounded RED evidence: the historical watcher is terminal `done`, its
`conduct` output has an isolated reporting `session_id` and `resolved_runtime`,
and `run.tokens_used` is non-zero. Save only those fields in the task report.

- [ ] **Step 2: Validate live schemas and record the watcher-graph conflict**

Run:

```bash
compozy tool info compozy__loop_status \
  --workspace "$(source tests/contract/lib.sh; require_test_workspace)" -o json
compozy tool info compozy__session_prompt \
  --workspace "$(source tests/contract/lib.sh; require_test_workspace)" -o json
```

`compozy__loop_status` accepts `run_id`, but its declared output schema exposes
only `run.id`, `run.best_generation`, and `run.best_score`. Record the compiler's
`unresolvable_path` diagnostics for the rejected
`run.inputs.origin_session_id` and `run.status` watcher templates. Do not hide the
conflict with a dynamic accessor. Confirm `compozy__session_prompt` requires
`session_id`, `message`, `message_id`, and `idempotency_key`, and accepts
`mode=queue`.

- [ ] **Step 3: Add all seven native terminal effects**

Under `batuta-deliver.contract`, include `canceled` in `terminal_states` and add
`on_done`, `on_noop`, `on_blocked`, `on_failed`, `on_exhausted`, `on_stalled`,
and `on_canceled`. Each list contains the same effect:

```yaml
tool: compozy__session_prompt
with:
  session_id: "{{ .inputs.origin_session_id }}"
  message_id: "batuta-terminal-{{ .effect.identity.loop_run_id }}"
  idempotency_key: "batuta-terminal-{{ .effect.identity.loop_run_id }}"
  mode: queue
  message: |
    Batuta delivery run {{ .effect.identity.loop_run_id }} reached terminal
    trigger {{ .effect.identity.trigger }}. Inspect the run with
    `compozy__loop_status`, then report its exact terminal outcome, child run
    IDs, commits, and blocker in this conversation. Decide any redispatch or
    escalation with the operator. Never approve a gate, edit code, push, or
    mutate routing from this return prompt.
```

A shared YAML anchor is acceptable only when the live compiler accepts it.
Terminal effects persist with lifecycle settlement and relay tool calls with
stable delivery IDs; the run-derived prompt identities provide admission replay
deduplication.

- [ ] **Step 4: Remove the watcher resource and bootstrap behavior**

Delete `loops/batuta-watch/loop.yaml`. Remove watcher start/monitor instructions
from `agents/batuta/AGENT.md`; bootstrap retains only routing and `auto_commit`
checks. Document that the originating Batuta session receives the effect prompt,
inspects the exact run, and reports the outcome without a reporting agent.

- [ ] **Step 5: Replace the watcher contract test**

Delete `tests/contract/test_05_watch_validate.sh` and create executable
`tests/contract/test_05_return_validate.sh`. It must inspect the live
`compozy__session_prompt` descriptor and compile the candidate
`batuta-deliver` definition. `compozy loop validate` exposes only `valid` and
diagnostics before publication, not a compiled definition, so do not add an
undeclared YAML parser or source-text assertions. Review exact seven-hook
coverage separately; guided E2E owns admission and replay behavior.

- [ ] **Step 6: Add guided behavioral cases**

Add to `tests/e2e/SMOKE.md` before live execution:

```markdown
- After one terminal delivery, the original session receives exactly one
  queued/direct turn.
- Replaying the same terminal effect identity does not add another turn.
- No watcher or reporting-agent runtime exists, and returning the terminal
  spends no reporting-agent model tokens.
```

- [ ] **Step 7: Verify public contracts**

```bash
tests/contract/test_05_return_validate.sh
tests/contract/run.sh
git diff --check
```

Expected: the live prompt schema is accepted, `batuta-deliver` compiles with
`valid: true`, all contract tests pass apart from the documented installed-state
lifecycle skip, and the diff check is clean.

- [ ] **Step 8: Commit**

```bash
git add agents/batuta/AGENT.md loops/batuta-deliver/loop.yaml \
  loops/batuta-watch/loop.yaml tests/contract/test_05_watch_validate.sh \
  tests/contract/test_05_return_validate.sh tests/e2e/SMOKE.md
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

- [ ] **Step 1: Rewrite both README flows**

Document:

- extension inventory contains exactly `batuta`, `batuta-routing`, and
  `batuta-deliver`;
- providers are derived from `compozy provider models list`, not hard-coded by lane;
- bootstrap stores `auto_commit` on `batuta-deliver`;
- one composite delivery receives the current session ID;
- all seven terminal effects queue one idempotent prompt to that conversation,
  with no background or reporting agent;
- contract tests require `compozy workspace add <repo>` once;
- minimum supported daemon is `0.3.0-beta.13`.

- [ ] **Step 2: Consolidate guided E2E smoke**

Add explicit checks:

```markdown
- Run once with `auto_commit=false`; both child inputs must show `false` and no
  implementation/review commit may be created.
- After terminal delivery, the original Batuta session must receive exactly one
  new queued/direct turn.
- Replaying the same terminal effect identity must not create a duplicate turn.
- Extension inventory must contain no watcher resource; delivery run detail must
  contain no reporting-agent `session_id` or `resolved_runtime` output, and the
  terminal return must spend no reporting-agent model tokens.
- A missing slug must be rejected by dry-run before submission. A deliberate
  direct invalid submission must never end `done`.
```

Record the exact delivery run ID, origin session ID, effect trigger, prompt
admission delivery/replay result, and relevant token count when the operator
executes the smoke.

- [ ] **Step 3: Mark historical documents superseded**

Add a note after each old title:

```markdown
> Superseded for runtime behavior by
> `docs/superpowers/specs/2026-08-12-batuta-reliability-design.md`.
> Retained as implementation history.
```

Do not rewrite historical decisions as if they were originally current.

- [ ] **Step 4: Review documentation against live public surfaces**

Run:

```bash
compozy extension inventory batuta -o json
compozy loop inspect --workspace "$(source tests/contract/lib.sh; require_test_workspace)" \
  --name batuta-deliver -o json
```

Before republication this read shows the prior live definition. Compare public field
names only; verify new live behavior after Task 5. Human prose receives editorial
review, not change-detector tests.

- [ ] **Step 5: Commit**

```bash
git add README.md README.pt-BR.md tests/e2e/SMOKE.md \
  docs/superpowers/specs/2026-08-11-batuta-compozy-design.md \
  docs/superpowers/plans/2026-08-11-batuta-compozy.md
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

Expected: extension state `active`; inventory lists exactly three resources live:
`batuta`, `batuta-routing`, and `batuta-deliver`. This
script removes and reinstalls only the global `batuta` extension and is the repository's
declared local publication workflow.

- [ ] **Step 4: Confirm live definitions**

```bash
WS=$(bash -c 'source tests/contract/lib.sh; require_test_workspace')
compozy extension inventory batuta -o json
compozy loop inspect --workspace "$WS" --name batuta-deliver -o json
```

Expected: all three inventory items are `live: true`; the live delivery definition
contains the required session input and all seven native terminal effects with
queued session prompting and run-derived identities.

Use guided E2E—not source inspection—to verify that the original session receives
exactly one turn and replaying the same effect identity is deduplicated. Confirm
inventory has no watcher resource, delivery detail has no reporting-agent runtime,
and the return has no reporting-agent token use.

- [ ] **Step 5: Run verification-before-completion**

Use `superpowers:verification-before-completion`. Capture fresh outputs for manifest
validation, delivery Loop validation, shell syntax, contract suite, inventory, and clean
worktree/diff state. Do not claim the guided token-spending E2E was executed unless an
operator actually performs it.

- [ ] **Step 6: Commit any verification-driven correction**

Only when Step 1–5 required a source correction:

```bash
git add <exact-corrected-files>
git commit -m "fix: resolve batuta reliability verification findings"
```

Otherwise create no empty verification commit.

---

### Task 6: Final-review root-cause corrections — 2026-08-12

This dated amendment supersedes Task 2's dry-run task-existence claim and Task
5's repository-root publication workflow while preserving their review history.

- Require a direct read-only `import_tasks` call with positive count before
  dry-run; document that dry-run plans nodes without executing them.
- Keep beta.13 as the manifest grammar floor and enforce the operational floor
  with a version guard: beta.13 post-tag `Version` and `Commit` must resolve to
  the same canonical full hash in the official descendant allowlist from
  `594d9fdf` through current. Accept only exact full commit hashes or the
  official eight-character emitted abbreviation; later beta/stable releases
  remain accepted.
- Derive the routing dry-run pair from live provider/model catalogs, preferring
  live availability and falling back to non-explicitly-unavailable catalog
  models on providers whose authentication is not missing; create no task
  fixture.
- Assert the exact live `(kind,name)` inventory.
- Stage only the manifest and three declared resources; promote them through a
  temporary sibling into a content-addressed package under user data, make
  files read-only, verify exact tree and bytes before reuse, and install from
  the retained final directory.
- Hold one stable per-user global-Batuta lock, independent of package root,
  across package creation, validation, revalidation, removal, installation,
  enabling, and final inventory verification. Keep the package-root lock for
  local construction only. Revalidate the retained package under the global
  lock immediately before changing installed extension state.
- Keep the lifecycle mutation window cleanup-guarded even when install output
  parsing fails after the daemon has committed the install.
- Validate the guard before removal, then publish from the retained digest
  package and verify the managed installation contains no repository metadata
  or development artifacts.
- Preserve the pending operator-conversation E2E boundary; structural and
  contract verification do not prove it.
