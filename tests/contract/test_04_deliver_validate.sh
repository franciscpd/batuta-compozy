#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
source tests/contract/lib.sh
REPO_ROOT=$PWD
REPO_WORKSPACE_PREEXISTED=false
if workspace_marker_present "$REPO_ROOT"; then
  REPO_WORKSPACE_PREEXISTED=true
fi
WS=$(require_test_workspace)
INSTALLED_HERE=false
SMOKE_SLUG=_batuta_public_action_smoke
SMOKE_ROOT="$BATUTA_TEST_WORKSPACE_ROOT/.compozy/tasks/$SMOKE_SLUG"
SMOKE_LAUNCHER_STATUS=$(mktemp)
SMOKE_STATUS=$(mktemp)
SMOKE_RUN_ID=
SMOKE_CORE_RUN_ID=
cleanup() {
  local original_status=$?
  trap - EXIT
  if [[ -n $SMOKE_CORE_RUN_ID ]]; then
    timeout 15s compozy loop cancel --workspace "$WS" --run-id "$SMOKE_CORE_RUN_ID" -o json >/dev/null 2>&1 || true
  fi
  if [[ -n $SMOKE_RUN_ID ]]; then
    timeout 15s compozy loop cancel --workspace "$WS" --run-id "$SMOKE_RUN_ID" -o json >/dev/null 2>&1 || true
  fi
  if [[ -d $SMOKE_ROOT && ! -L $SMOKE_ROOT ]]; then
    rm -rf -- "$SMOKE_ROOT"
  fi
  if [[ $INSTALLED_HERE == true ]]; then
    timeout 30s compozy extension remove batuta --global -o json >/dev/null || exit 1
  fi
  rm -f -- "$SMOKE_LAUNCHER_STATUS" "$SMOKE_STATUS"
  reject_new_repository_marker "$REPO_ROOT" "$REPO_WORKSPACE_PREEXISTED" || exit 1
  exit "$original_status"
}
trap cleanup EXIT

if timeout 15s compozy extension list -o json | python3 -c 'import json,sys; raise SystemExit(0 if any(row.get("name") == "batuta" for row in json.load(sys.stdin)) else 1)'; then
  printf 'public action smoke requires an isolated runtime without a preinstalled Batuta\n' >&2
  exit 1
fi

for loop_file in loops/batuta-deliver/loop.yaml loops/batuta-deliver-core/loop.yaml; do
  out=$(timeout 60s compozy loop validate --file "$loop_file" --workspace "$WS" -o json)
  printf '%s' "$out" | python3 - "$loop_file" <<'PY'
import json, sys
d = json.load(sys.stdin)
assert d.get("valid") is True, f"{sys.argv[1]} invalido: {d}"
print(f"OK: {sys.argv[1]} valido (lint+compile)")
PY
done

INSTALLED_HERE=true
timeout 180s scripts/republish.sh

mkdir -p "$(dirname "$SMOKE_ROOT")"
cp -R tests/fixtures/parallel-delivery/.compozy/tasks/parallel-demo "$SMOKE_ROOT"
if ! run=$(timeout 30s compozy loop run --workspace "$WS" --name batuta-deliver \
  --input delivery_envelope_version=1 \
  --input delivery_id=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
  --input attempt=1 --input slug="$SMOKE_SLUG" --input origin_session_id=sess-public-smoke \
  --input worktree_ref="$BATUTA_TEST_WORKSPACE_ROOT" \
  --input routing_generation=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb \
  --input absolute_deadline=2099-01-01T00:00:00Z --input token_ceiling=1000 \
  --input recovery_operation_id=sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc \
  --input iteration_cap=1 --input budget_tokens=1000 --input budget_wall_seconds=30 \
  --no-prompt -o json 2>&1); then
  if grep -q 'unknown_action_kind' <<<"$run"; then
    printf 'installed public Batuta action is unknown_action_kind: %s\n' "$run" >&2
  else
    printf 'installed public Batuta Loop did not start: %s\n' "$run" >&2
  fi
  exit 1
fi
SMOKE_RUN_ID=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["run"]["id"])' <<<"$run")

core_started=false
for _ in {1..100}; do
  if timeout 10s compozy loop status --workspace "$WS" --run-id "$SMOKE_RUN_ID" -o json > "$SMOKE_LAUNCHER_STATUS"; then
    if grep -q 'unknown_action_kind' "$SMOKE_LAUNCHER_STATUS"; then
      printf 'installed public Batuta action became unknown_action_kind\n' >&2
      exit 1
    fi
    if SMOKE_CORE_RUN_ID=$(python3 - "$SMOKE_LAUNCHER_STATUS" <<'PY'
import json, sys
payload = json.load(open(sys.argv[1], encoding="utf-8"))
outputs = [row for generation in payload.get("generations", []) for row in generation.get("outputs", [])]
children = [
    row["child_loop_run_id"]
    for row in outputs
    if row.get("node_id") == "delivery_core"
    and isinstance(row.get("child_loop_run_id"), str)
    and row["child_loop_run_id"]
]
assert len(children) <= 1, children
if len(children) != 1:
    raise SystemExit(1)
print(children[0])
PY
    ); then
      core_started=true
      break
    fi
  fi
  sleep 0.1
done
if [[ $core_started != true ]]; then
  printf 'installed public Batuta launcher never produced exactly one delivery_core child: %s\n' "$(cat "$SMOKE_LAUNCHER_STATUS")" >&2
  exit 1
fi
if grep -q 'unknown_action_kind' <<<"$run"; then
  printf 'installed public Batuta action is unknown_action_kind: %s\n' "$run" >&2
  exit 1
fi
core_ready=false
for _ in {1..100}; do
  if timeout 10s compozy loop status --workspace "$WS" --run-id "$SMOKE_CORE_RUN_ID" -o json > "$SMOKE_STATUS"; then
    if grep -q 'unknown_action_kind' "$SMOKE_STATUS"; then
      printf 'installed Batuta core action became unknown_action_kind\n' >&2
      exit 1
    fi
    if python3 - "$SMOKE_STATUS" "$SMOKE_LAUNCHER_STATUS" "$SMOKE_RUN_ID" "$SMOKE_CORE_RUN_ID" <<'PY'
import json, sys
payload = json.load(open(sys.argv[1], encoding="utf-8"))
launcher_payload = json.load(open(sys.argv[2], encoding="utf-8"))
launcher_id = sys.argv[3]
launcher_run = launcher_payload["run"]
core_run = payload["run"]
assert launcher_run["id"] == launcher_id, launcher_run
assert core_run["id"] == sys.argv[4], core_run
assert core_run["loop_name"] == "batuta-deliver-core", core_run
assert core_run["parent_loop_run_id"] == launcher_id, core_run
assert core_run["workspace_id"] == launcher_run["workspace_id"], core_run
for key in (
    "delivery_envelope_version", "delivery_id", "attempt", "slug",
    "origin_session_id", "worktree_ref", "routing_generation",
    "absolute_deadline", "token_ceiling", "recovery_operation_id",
    "iteration_cap", "budget_tokens", "budget_wall_seconds",
):
    assert core_run["inputs"][key] == launcher_run["inputs"][key], key
outputs = [row for generation in payload.get("generations", []) for row in generation.get("outputs", [])]
load = [row for row in outputs if row.get("node_id") == "load_check"]
routing = [row for row in outputs if row.get("node_id") == "routing_context"]
if not (
    load and load[-1].get("status") == "succeeded"
    and routing and routing[-1].get("status") in {"retrying", "failed", "blocked"}
    and isinstance(routing[-1].get("task_run_id"), str) and routing[-1]["task_run_id"]
):
    raise SystemExit(1)
print("OK: launcher/core lineage is exact and core invoked routing_context")
PY
    then
      core_ready=true
      break
    fi
  fi
  sleep 0.1
done
if [[ $core_ready != true ]]; then
  printf 'installed Batuta core never resolved load_check and invoked routing_context: %s\n' "$(cat "$SMOKE_STATUS")" >&2
  exit 1
fi

python3 - loops/batuta-deliver/loop.yaml loops/batuta-deliver-core/loop.yaml agents/batuta/AGENT.md <<'PY'
from pathlib import Path
import re
import sys

launcher = open(sys.argv[1], encoding="utf-8").read()
core = open(sys.argv[2], encoding="utf-8").read()
conductor = open(sys.argv[3], encoding="utf-8").read()

assert launcher.count("kind: run-loop") == 1
assert "kind: ext__" not in launcher
assert "loop: batuta-deliver-core" in launcher
assert "\n  auto_commit:\n" not in core
assert core.count("auto_commit: true") == 1
assert core.count("kind: run-loop") == 2
for required_input in ("delivery_id:", "attempt:", "absolute_deadline:", "token_ceiling:", "recovery_operation_id:", "routing_generation:"):
    assert required_input in core, required_input
assert "iteration_cap: 64" in core
assert "max_parallel: 4" in core and "max_fan_out: 4" in core
assert "id: routing_context" in core and "kind: ext__batuta__routing_context" in core
assert "id: prepare_wave" in core and "kind: ext__batuta__delivery_graph" in core
assert "id: task_wave" in core and "kind: fan-out" in core
assert "id: record_candidate" in core and "kind: ext__batuta__delivery_graph" in core
assert "id: settle_wave" in core and "kind: ext__batuta__delivery_graph" in core
assert "id: delivery_budget_context" in core and "kind: ext__batuta__delivery_budget_context" in core
assert "runtime_rules: \"{{ .nodes.routing_context.output.runtime_rules }}\"" in core
assert "budget_tokens: \"{{ .item.remaining_tokens }}\"" in core
assert "budget_wall_sec: \"{{ .item.remaining_active_wall_seconds }}\"" in core
assert "budget_tokens: \"{{ .nodes.delivery_budget_context.output.remaining_tokens }}\"" in core
assert "budget_wall_sec: \"{{ .nodes.delivery_budget_context.output.remaining_wall_seconds }}\"" in core
assert "implementation_run_id:" not in core
assert ".nodes.implement.output.child_loop_run_id" not in core
assert "id: recovery_gate" not in core and "kind: human" not in core
assert "id: publish_gate" not in core
assert "id: publish" in core and "kind: ext__batuta__publish_worktree" in core
assert "kind: goal" not in core and "agent: batuta-publisher" not in core
assert "kind: ext__batuta__publication_verify" in core
assert "publisher_result: \"{{ .nodes.publish.output }}\"" in core
assert "compare_url" not in core

edges = core.split("\n  edges:\n", 1)[1]
pairs = set(re.findall(r"-\s*\{?from:\s*([^,\s}]+),?\s*to:\s*([^,\s}]+)", edges))
required = {
    ("load_check", "routing_context"),
    ("routing_context", "prepare_wave"),
    ("prepare_wave", "wave_route"),
    ("wave_route", "task_wave"),
    ("task_wave", "run_task"),
    ("run_task", "record_candidate"),
    ("record_candidate", "collect_wave"),
    ("collect_wave", "settle_wave"),
    ("delivery_budget_context", "review"),
    ("review", "publication_plan"),
    ("publication_plan", "publication_route"),
    ("publication_route", "publish"),
    ("publication_route", "publication_verify_nothing"),
    ("publication_route", "publication_blocked_stop"),
    ("publish", "publication_verify"),
    ("publication_verify", "cleanup"),
    ("cleanup", "cleanup_route"),
}
assert not (required - pairs), required - pairs

assert not Path("agents/batuta-publisher").exists()

assert "auto_commit=true" in conductor
assert "publication" in conductor.lower() and "automatic" in conductor.lower()
assert "merge remains manual" in conductor
assert "reviewed delivery boundary for one PR" in conductor
assert "Never call `compozy__loop_recover_nested`" in conductor
assert "operation `start_delivery`" in conductor
assert "operation `reconcile_fallbacks`" in conductor
assert "operation `recover_delivery`" in conductor
assert "fresh parent run" in conductor
for forbidden in ("compozy__loop_configure", "compozy__loop_run", "expected_revision", "config_revision", "same-lineage"):
    assert forbidden not in conductor, forbidden

routing_skill = Path("resources/skills/batuta-routing/SKILL.md").read_text(encoding="utf-8")
assert "start_delivery" in routing_skill
assert "fresh Compozy parent run" in routing_skill
for forbidden in ("revision CAS", "same-lineage", "stored Loop rules"):
    assert forbidden not in routing_skill, forbidden
print("OK: delivery is automatic, bounded, exact-head verified, and human-gate free")
PY
