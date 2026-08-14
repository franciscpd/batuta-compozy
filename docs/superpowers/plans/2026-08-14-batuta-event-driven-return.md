# Batuta Event-Driven Return Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Batuta end its provider turn immediately after an accepted `batuta-deliver` dispatch and resume only through CompozyOS's existing terminal prompt or one explicitly requested progress read.

**Architecture:** Keep `batuta-deliver` and its idempotent terminal effects unchanged. Tighten the authored Batuta agent contract, prove event ordering through the public session-events API, and add the exact current Compozy build to Batuta's trusted-build guard so the candidate can be exercised in an isolated lab before publication.

**Tech Stack:** Markdown agent/extension resources, Bash contract tests, Python 3 `unittest`, CompozyOS CLI and hosted MCP session events.

## Global Constraints

- Work only in `/home/franciscpd/Projects/batuta-compozy/.worktrees/batuta-reliability` on `feat/batuta-reliability`.
- Preserve `.compozy/` as untracked operator/generated state; never stage it. Run the aggregate contract suite in a clean temporary worktree/package clone rather than deleting or committing that directory.
- Do not modify `loops/batuta-deliver/loop.yaml`, `extension.toml`, the three-resource inventory, CompozyOS source, provider prices, routing candidates, session lineage, or session lifecycle behavior.
- Treat a real dispatch as accepted only after the correlated `compozy__loop_run` result contains the exact parent run ID under `structuredContent.run.id`.
- Dry-runs and failed submissions do not create the hard turn boundary.
- After an accepted dispatch, no further tool call is allowed in that turn. Later prose fragments and the normal `done` event are allowed because they complete the acknowledgement.
- A terminal-effect prompt is only a wake signal. Its turn must inspect the exact parent with `compozy__loop_status` before reporting a terminal result.
- An explicit progress request permits exactly one `compozy__loop_status` read in that separate turn and never authorizes sleep, shell wait, repeated reads, or a watcher.
- Add only the full trusted Compozy commit `c88b3e5274e86103215fbf900faf742d6593b7dd`, paired with its exact version `v0.3.0-beta.15-26-gc88b3e52`; do not accept arbitrary beta.15 histories, partial prefixes shorter than the existing eight-character rule, or count-only ancestry claims.
- Use TDD for every behavior change, run `deslop` before the implementation commit, and preserve exact RED/GREEN output in QA evidence.
- No push, PR, upstream issue, or production republication is authorized by this implementation plan. Stop after isolated QA and a clean local commit; production publication remains an explicit follow-up decision.

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `tests/contract/test_00_runtime_guard.sh` | Modify | Prove the current merged Compozy build is accepted only by exact version/commit identity. |
| `scripts/check-compozy-version.sh` | Modify | Add the one exact current merged build to `trusted_post_tag_builds`. |
| `tests/e2e/assert_event_driven_return.py` | Create | Validate dispatch, terminal-return, and optional progress-turn ordering from public session events. |
| `tests/e2e/test_assert_event_driven_return.py` | Create | Unit-test the validator with synthetic event streams, including the observed polling failure. |
| `tests/contract/test_05_return_validate.sh` | Modify | Enforce the authored Batuta hard-boundary contract and continue validating terminal effects. |
| `agents/batuta/AGENT.md` | Modify | Replace ambiguous live-run observation guidance with the hard turn boundary and bounded later-turn rules. |
| `README.md` | Modify | Document the event-driven dispatch/return contract and validator invocation in English. |
| `README.pt-BR.md` | Modify | Document the same public contract in Portuguese. |
| `tests/e2e/SMOKE.md` | Modify | Add observable acceptance steps for no polling, terminal wake, and one-read progress behavior. |
| `docs/superpowers/specs/2026-08-14-batuta-event-driven-return-design.md` | Preserve | Approved design source; no implementation edits expected. |
| `loops/batuta-deliver/loop.yaml` | Preserve | Existing seven terminal effects and identities remain byte-for-byte unchanged. |

---

### Task 1: Trust the exact merged Compozy test build

**Files:**
- Modify: `tests/contract/test_00_runtime_guard.sh`
- Modify: `scripts/check-compozy-version.sh`

- [ ] **Step 1: Add the failing exact-build contract**

Extend `tests/contract/test_00_runtime_guard.sh` with:

```bash
CURRENT_COMPOZY_COMMIT=c88b3e5274e86103215fbf900faf742d6593b7dd

expect_accept \
  "v0.3.0-beta.15-26-gc88b3e52" \
  "$CURRENT_COMPOZY_COMMIT"
expect_accept "v0.3.0-beta.15-26-gc88b3e52" "c88b3e52"
expect_reject "v0.3.0-beta.15-26-gc88b3e52" "c88b3e5"
expect_reject \
  "v0.3.0-beta.15-25-gc88b3e52" \
  "$CURRENT_COMPOZY_COMMIT"
expect_reject \
  "v0.3.0-beta.15-26-gc88b3e52" \
  "c88b3e5274e86103215fbf900faf742d6593b7de"
```

- [ ] **Step 2: Run the focused RED**

Run:

```bash
tests/contract/test_00_runtime_guard.sh
```

Expected: nonzero because `v0.3.0-beta.15-26-gc88b3e52` is not yet present in `trusted_post_tag_builds`.

- [ ] **Step 3: Add only the exact trusted identity**

Add this entry to `trusted_post_tag_builds` in `scripts/check-compozy-version.sh`:

```python
"c88b3e5274e86103215fbf900faf742d6593b7dd":
    "v0.3.0-beta.15-26-gc88b3e52",
```

Do not change `resolve_trusted_post_tag`, release-floor logic, or the error text.

- [ ] **Step 4: Run the focused GREEN and real-binary guard**

Run:

```bash
tests/contract/test_00_runtime_guard.sh
scripts/check-compozy-version.sh
```

Expected: both exit zero; the second prints that `v0.3.0-beta.15-26-gc88b3e52 (c88b3e52)` is an exact trusted test build.

- [ ] **Step 5: Commit the isolated compatibility update**

```bash
git add scripts/check-compozy-version.sh tests/contract/test_00_runtime_guard.sh
git diff --cached --check
git commit -m "fix: trust current Compozy test build"
```

Expected: one local conventional commit; `.compozy/` remains unstaged.

---

### Task 2: Build the public-event ordering validator with TDD

**Files:**
- Create: `tests/e2e/assert_event_driven_return.py`
- Create: `tests/e2e/test_assert_event_driven_return.py`

- [ ] **Step 1: Write synthetic event builders and failing tests**

In `tests/e2e/test_assert_event_driven_return.py`, load the sibling validator module and add `unittest.TestCase` coverage for these exact cases:

1. Accepted `batuta-deliver` result followed by `compozy__loop_status` in the same `turn_id` fails and reports both sequences.
2. Accepted result followed by a shell `sleep` tool in the same turn fails.
3. Dry-run and failed real submission do not establish a dispatch boundary.
4. Accepted result, acknowledgement/done, later terminal prompt containing `Batuta delivery run $DELIVERY_RUN_ID reached terminal`, then first later-turn tool `compozy__loop_status` with the exact `run_id` passes.
5. A terminal-return turn whose first operational tool is another tool fails.
6. A terminal-return status read for a different run ID fails.
7. Two terminal prompts with the same run-derived identity fail as duplicate logical returns.
8. An explicitly identified progress turn with exactly one matching status read passes; two status reads or a wait tool fail.
9. A truncated event window whose minimum sequence is greater than one fails closed instead of claiming complete ordering evidence.

Use production-shaped records: `type`, `sequence`, `content.turn_id`, `content.tool_call_id`, `content.tool_input.tool`, `content.tool_input.arguments`, and correlated `content.tool_result.raw_output.result.structuredContent`.

- [ ] **Step 2: Run the validator unit RED**

Run:

```bash
python3 -m unittest -v tests/e2e/test_assert_event_driven_return.py
```

Expected: import/module failure because `assert_event_driven_return.py` does not exist.

- [ ] **Step 3: Implement the validator interfaces**

Create `tests/e2e/assert_event_driven_return.py` with fully implemented bodies
for these concrete interfaces:

```text
@dataclass(frozen=True)
class ValidationResult:
    dispatch_sequence: int
    dispatch_turn_id: str
    terminal_prompt_sequence: int
    terminal_status_sequence: int

- fetch_events(compozy: str, session_id: str) -> list[dict]
- validate_delivery(events: list[dict], run_id: str) -> ValidationResult
- validate_progress_turn(events: list[dict], run_id: str, turn_id: str) -> int
```

The CLI is:

```text
assert_event_driven_return.py \
  --compozy "$COMPOZY_BIN" \
  --session "$SESSION_ID" \
  --run-id "$DELIVERY_RUN_ID" \
  [--progress-turn "$PROGRESS_TURN_ID"]
```

Implementation rules:

- `fetch_events` calls `compozy session events "$SESSION_ID" --archive all --last 10000 -o json`, requires a JSON list, sorts by `sequence`, rejects duplicate sequences, and fails closed when the first sequence is not `1`.
- Locate the real dispatch call by `tool == "compozy__loop_run"`, `arguments.name == "batuta-deliver"`, and `arguments.dry` absent or `false`.
- Correlate its result with `tool_call_id`; accept only when `structuredContent.run.id == run_id`.
- Reject every `tool_call` after that result with the same `turn_id`, regardless of tool name. This covers status, shell sleep, session wait/prompt, and future polling aliases without maintaining a bypassable denylist.
- Locate exactly one later prompt by joining text fragments in each turn and matching the stable prefix formed from `Batuta delivery run `, the exact `run_id`, and ` reached terminal`.
- Require the terminal prompt's turn to differ from the dispatch turn. Its first `tool_call` must be `compozy__loop_status` with `arguments.run_id == run_id`.
- When `--progress-turn` is supplied, require exactly one tool call in that turn and require it to be the matching `compozy__loop_status` call.
- Catch `AssertionError`, `subprocess.CalledProcessError`, and `json.JSONDecodeError`; prefix the exact diagnostic with `event-driven return violation:` on stderr and exit one.
- Print deterministic success fields: dispatch sequence, dispatch turn, terminal prompt sequence, terminal status sequence, and optional progress sequence.

- [ ] **Step 4: Run the validator unit GREEN**

Run:

```bash
python3 -m unittest -v tests/e2e/test_assert_event_driven_return.py
python3 -m py_compile \
  tests/e2e/assert_event_driven_return.py \
  tests/e2e/test_assert_event_driven_return.py
```

Expected: every synthetic case passes; compilation exits zero.

- [ ] **Step 5: Prove the historical session is RED for the intended reason**

Run:

```bash
python3 tests/e2e/assert_event_driven_return.py \
  --compozy /home/franciscpd/.local/bin/compozy \
  --session sess-22de2cc93e324ddc \
  --run-id looprun-c3275372773ef7c4
```

Expected: nonzero with a deterministic violation naming accepted result sequence `377`, later same-turn tool sequence `383`, and turn `turn-00b043f42f6cf655`. This is the behavioral RED fixture; do not edit or replay that historical session.

- [ ] **Step 6: Commit the reusable regression harness**

```bash
git add tests/e2e/assert_event_driven_return.py \
  tests/e2e/test_assert_event_driven_return.py
git diff --cached --check
git commit -m "test: cover event-driven Batuta return"
```

---

### Task 3: Make accepted dispatch a hard Batuta turn boundary

**Files:**
- Modify: `tests/contract/test_05_return_validate.sh`
- Modify: `agents/batuta/AGENT.md`

- [ ] **Step 1: Add the authored-contract RED**

Extend `tests/contract/test_05_return_validate.sh` with a Python assertion over `agents/batuta/AGENT.md` that requires these concepts as literal stable clauses:

- `successful real dispatch is a hard turn boundary`
- `end the turn without another tool call`
- `first operational tool call is compozy__loop_status`
- `one compozy__loop_status read`

It must also reject the old ambiguous sentence `While a run is live: observe with` and reject any instruction to poll or keep watching after accepted dispatch. Keep the existing `session_prompt` schema and Loop validation checks unchanged.

- [ ] **Step 2: Run the focused contract RED**

Run:

```bash
tests/contract/test_05_return_validate.sh
```

Expected: nonzero because the current AGENT still contains `While a run is live: observe with` and lacks the hard boundary.

- [ ] **Step 3: Rewrite only the dispatch/return guidance**

In `agents/batuta/AGENT.md`:

- Keep preflight, dry-run, exact inputs, failure handling, terminal literalness, and routing audit rules.
- Replace Dispatch step 4 with: after the successful real result, retain `run_id`/`web_url`, acknowledge durable acceptance, tell the operator the daemon will return here, and end the turn without another tool call.
- Split later behavior into two explicit clauses:
  - terminal-effect turn: first operational tool call is exact-parent `compozy__loop_status`, then report literal parent/child/commit/blocker evidence;
  - explicit operator progress turn: one matching `compozy__loop_status` read, report snapshot, end turn.
- Remove the clause beginning `While a run is live: observe with` entirely.
- State that failed terminal-effect delivery never authorizes a watcher or polling fallback.
- Preserve `todo 1.0.0`, no direct code edits, no push, no self-approval, and no silent routing mutation.

- [ ] **Step 4: Run the focused contract GREEN**

Run:

```bash
tests/contract/test_05_return_validate.sh
```

Expected: the authored-contract assertions, hosted `session_prompt` descriptor check, and `batuta-deliver` validation all pass.

---

### Task 4: Document the observable behavior

**Files:**
- Modify: `README.md`
- Modify: `README.pt-BR.md`
- Modify: `tests/e2e/SMOKE.md`

- [ ] **Step 1: Update both public READMEs symmetrically**

Immediately after the delivery-flow description, document:

- accepted dispatch returns `run_id`/optional `web_url` and ends the Batuta turn;
- CompozyOS's existing idempotent terminal effect starts the reporting turn;
- that later turn verifies the exact run before reporting;
- an explicit progress request performs one status snapshot and does not poll;
- no watcher resource or reporting agent is added.

Keep the English and Portuguese claims semantically identical.

- [ ] **Step 2: Strengthen the guided smoke**

Update `tests/e2e/SMOKE.md` so acceptance records:

1. the real dispatch's tool-result sequence and turn ID;
2. the absence of any later tool call in that turn;
3. a later terminal prompt with the exact run identity;
4. exact-parent `loop_status` as that turn's first operational tool;
5. an optional separate progress-request turn with exactly one status call;
6. no sleep, wait, watcher, extra logical terminal prompt, push, or Batuta-authored code.

Add the exact validator command:

```bash
COMPOZY_BIN=/absolute/path/to/compozy
SESSION_ID=sess-example
DELIVERY_RUN_ID=looprun-example
python3 tests/e2e/assert_event_driven_return.py \
  --compozy "$COMPOZY_BIN" \
  --session "$SESSION_ID" \
  --run-id "$DELIVERY_RUN_ID"
```

Document repeatable `--progress-turn "$PROGRESS_TURN_ID"` only when that explicit-progress case was actually exercised.

- [ ] **Step 3: Check documentation and shell formatting**

Run:

```bash
git diff --check
bash -n scripts/*.sh tests/contract/*.sh
python3 -m unittest -v tests/e2e/test_assert_event_driven_return.py
tests/contract/test_05_return_validate.sh
```

Expected: all exit zero and the English/Portuguese docs describe the same boundary.

- [ ] **Step 4: Commit the authored behavior and public docs**

```bash
git add agents/batuta/AGENT.md README.md README.pt-BR.md \
  tests/contract/test_05_return_validate.sh tests/e2e/SMOKE.md
git diff --cached --check
git diff --cached --name-only
git commit -m "fix: end Batuta turn after delivery dispatch"
```

Expected: the staged list excludes `.compozy/`, `loop.yaml`, and `extension.toml`.

---

### Task 5: Run the complete local verification in a clean clone

**Files:**
- Verify only: all changed files
- Preserve: repository `.compozy/`

- [ ] **Step 1: Run focused and static checks in the source worktree**

```bash
tests/contract/test_00_runtime_guard.sh
tests/contract/test_05_return_validate.sh
python3 -m unittest -v tests/e2e/test_assert_event_driven_return.py
bash -n scripts/*.sh tests/contract/*.sh
git diff --check
```

Expected: all pass.

- [ ] **Step 2: Run the aggregate contract suite without touching operator state**

Create a temporary detached worktree from the candidate commit, run the suite there, and remove that worktree with normal Git cleanup:

```bash
VERIFY_DIR=$(mktemp -d /tmp/batuta-event-return.XXXXXX)
git worktree add --detach "$VERIFY_DIR" HEAD
(cd "$VERIFY_DIR" && tests/contract/run.sh)
git worktree remove "$VERIFY_DIR"
```

Expected: every `test_*.sh` passes and no `.compozy/` remains in the temporary worktree. If the suite fails, preserve the output and fix the root cause; do not hide cleanliness failures.

- [ ] **Step 3: Run package validation without publication**

```bash
PACKAGE_DIR=$(scripts/package-extension.sh)
compozy extension validate "$PACKAGE_DIR" -o json
```

Expected: no severity `error`; the package contains only the manifest plus `agents/batuta`, `skills/batuta-routing`, and `loops/batuta-deliver` resources.

- [ ] **Step 4: Run deslop and review scope**

Invoke the `deslop` skill over the branch diff, then run:

```bash
git diff 04cc0e3798a83119edcf34b2c0ee19022e6176f1 --stat
git diff 04cc0e3798a83119edcf34b2c0ee19022e6176f1 -- \
  agents/batuta/AGENT.md scripts/check-compozy-version.sh \
  tests/contract/test_00_runtime_guard.sh \
  tests/contract/test_05_return_validate.sh \
  tests/e2e/assert_event_driven_return.py \
  tests/e2e/test_assert_event_driven_return.py \
  tests/e2e/SMOKE.md README.md README.pt-BR.md
git status --short
```

Expected: no unrelated file, no Loop/manifest change, and only `.compozy/` remains untracked outside the intended diff.

- [ ] **Step 5: Commit only verification-driven corrections, if any**

If Steps 1–4 uncover a real defect, fix it with a focused RED/GREEN cycle and
commit only the corrected files using an unscoped conventional `fix:` or
`test:` message. Do not create an empty verification commit and do not amend
the already reviewed task commits merely to hide the correction history.

---

### Task 6: Prove the behavior in a fresh isolated Compozy lab

**Files:**
- Runtime evidence only under the fresh lab's `qa-artifacts/qa/`
- Verify live package sourced from the Batuta candidate commit

- [ ] **Step 1: Rebuild and identify the clean merged Compozy binary**

From `/home/franciscpd/Projects/compozy`, verify the repository is clean and at
the exact merged commit, then build:

```bash
test "$(git rev-parse HEAD)" = c88b3e5274e86103215fbf900faf742d6593b7dd
test -z "$(git status --porcelain)"
make build
./bin/compozy version -o json
sha256sum ./bin/compozy
```

Expected: the JSON identifies `v0.3.0-beta.15-26-gc88b3e52` and commit
`c88b3e52`; record the full SHA-256 before starting the lab.

- [ ] **Step 2: Bootstrap a fresh targeted lab**

From `/home/franciscpd/Projects/compozy`, run the canonical bootstrap with the required runtime/provider/CLI surfaces:

```bash
python3 .agents/skills/eng/eng-qa-bootstrap/scripts/bootstrap-qa-env.py \
  --scenario "batuta-event-driven-return" \
  --repo-root . \
  --profile targeted \
  --required-surface runtime \
  --required-surface provider \
  --required-surface cli
```

Record the emitted `BOOTSTRAP_MANIFEST`, source only its `bootstrap.env`, register every process in `qa-artifacts/qa/pids/`, and use the clean Compozy binary at commit `c88b3e5274e86103215fbf900faf742d6593b7dd`.

- [ ] **Step 3: Publish only the candidate Batuta package inside the lab**

Build the package from this branch, validate it, install it under the manifest-derived `COMPOZY_HOME`, enable it, and assert the exact live inventory:

```text
(agent, batuta)
(skill, batuta-routing)
(loop, batuta-deliver)
```

Do not remove, replace, or republish the operator's global Batuta installation during isolated QA.

- [ ] **Step 4: Prepare the smallest real delivery fixture**

In the lab runtime workspace:

- create a small repository with no remote;
- author one valid low-complexity task under `.compozy/tasks/event-return/task_01.md`;
- set workspace `loops.inputs.batuta-deliver.auto_commit=false` to avoid candidate code commits;
- configure only valid live routing for the lab;
- create a fresh Batuta session using the live provider lane;
- issue one operator request that approves the authored task and asks Batuta to dispatch it.

Capture the session ID and real `batuta-deliver` run ID from public CLI output/events.

- [ ] **Step 5: Validate the accepted-dispatch turn before completion**

As soon as the real run is accepted, read public events and assert:

- the dispatch result contains the exact run ID;
- no tool call follows it in the same turn;
- the Batuta acknowledgement says the daemon will return to the conversation;
- the session is not burning a provider turn on sleep/status calls while the Loop remains live.

Do not manually prompt the session to make the terminal return happen.

- [ ] **Step 6: Validate terminal wake and exact first read**

After CompozyOS queues the terminal effect and the later turn completes, run:

```bash
python3 tests/e2e/assert_event_driven_return.py \
  --compozy "$COMPOZY_BIN" \
  --session "$SESSION_ID" \
  --run-id "$DELIVERY_RUN_ID"
```

Expected: PASS with distinct dispatch/terminal turns, no post-dispatch tool, one logical terminal prompt, and the exact parent status as the first terminal-turn tool.

- [ ] **Step 7: Exercise one explicit progress turn separately**

Use a second fresh delivery that remains live long enough for one operator message asking for progress. Record that turn ID and run:

```bash
python3 tests/e2e/assert_event_driven_return.py \
  --compozy "$COMPOZY_BIN" \
  --session "$SESSION_ID" \
  --run-id "$DELIVERY_RUN_ID" \
  --progress-turn "$PROGRESS_TURN_ID"
```

Expected: exactly one matching status call in the progress turn and no wait/repeated read. If timing makes the run terminal before the request, record the case as not exercised and do not manufacture a PASS.

- [ ] **Step 8: Audit and tear down on every path**

Append actual actions to `journey-log.jsonl`, record provider proof, run the manifest's strict audit, then execute its exact teardown command whether the verdict is PASS, FAIL, BLOCKED, or aborted:

```bash
python3 "$AUDIT_COMMAND" --qa-output-path "$QA_OUTPUT_PATH" --strict
eval "$TEARDOWN_COMMAND"
```

Expected: `qa-audit-report.json` has no blocker for the declared targeted surfaces and `teardown.json` contains `"clean": true` with zero survivors.

- [ ] **Step 9: Record the final local handoff**

Report:

- Batuta commit IDs and total changed-file count;
- Compozy version, full commit, and checksum used by the lab;
- historical RED sequences `377 -> 383`;
- fresh GREEN session/run/turn/sequence IDs;
- package digest and exact three-resource inventory;
- contract-suite result and any honest SKIP/BLOCKED case;
- manifest, audit, and clean teardown paths;
- confirmation that no push, PR, global republication, or production change occurred.

---

## Final Self-Review Checklist

- [ ] Every approved design rule maps to an authored instruction and at least one automated or guided check.
- [ ] The validator distinguishes accepted real dispatch, dry-run, and failed submission.
- [ ] The validator uses only public CLI event data and fails closed on incomplete history.
- [ ] No tool-specific allowlist weakens the universal post-dispatch boundary.
- [ ] Terminal and progress turns are distinct and independently bounded.
- [ ] The full Compozy hash/version pair is exact and the existing trust logic is unchanged.
- [ ] `loops/batuta-deliver/loop.yaml` and `extension.toml` are unchanged.
- [ ] `.compozy/` is absent from every commit and package.
- [ ] No unfinished marker remains in implementation or tests.
- [ ] Isolated teardown evidence is `clean: true` before any completion claim.
