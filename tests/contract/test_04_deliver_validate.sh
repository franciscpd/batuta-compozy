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
cleanup() {
  local original_status=$?
  trap - EXIT
  reject_new_repository_marker "$REPO_ROOT" "$REPO_WORKSPACE_PREEXISTED" || exit 1
  exit "$original_status"
}
trap cleanup EXIT

out=$(compozy loop validate --file loops/batuta-deliver/loop.yaml --workspace "$WS" -o json)
printf '%s' "$out" | python3 -c '
import json, sys
d = json.load(sys.stdin)
assert d.get("valid") is True, f"batuta-deliver invalido: {d}"
print("OK: batuta-deliver valido (lint+compile)")
'

python3 - loops/batuta-deliver/loop.yaml agents/batuta/AGENT.md <<'PY'
from pathlib import Path
import re
import sys

loop = open(sys.argv[1], encoding="utf-8").read()
conductor = open(sys.argv[2], encoding="utf-8").read()

assert "\n  auto_commit:\n" not in loop
assert loop.count("auto_commit: true") == 2
assert loop.count("kind: run-loop") == 2
for required_input in ("delivery_id:", "attempt:", "absolute_deadline:", "token_ceiling:", "recovery_operation_id:", "routing_generation:"):
    assert required_input in loop, required_input
assert "iteration_cap: 4" in loop
assert "id: routing_context" in loop and "kind: ext__batuta__routing_context" in loop
assert "id: delivery_budget_context" in loop and "kind: ext__batuta__delivery_budget_context" in loop
assert "runtime_rules: \"{{ .nodes.routing_context.output.runtime_rules }}\"" in loop
assert "budget_tokens: \"{{ .nodes.routing_context.output.remaining_tokens }}\"" in loop
assert "budget_wall_sec: \"{{ .nodes.routing_context.output.remaining_wall_seconds }}\"" in loop
assert "budget_tokens: \"{{ .nodes.delivery_budget_context.output.remaining_tokens }}\"" in loop
assert "budget_wall_sec: \"{{ .nodes.delivery_budget_context.output.remaining_wall_seconds }}\"" in loop
assert "implementation_run_id:" not in loop
assert ".nodes.implement.output.child_loop_run_id" not in loop
assert "delivery_id {{ .inputs.delivery_id }}" in loop
assert "delivery_id and delivery_run_id" in loop
assert "id: recovery_gate" not in loop and "kind: human" not in loop
assert "id: publish_gate" not in loop
assert "id: publish" in loop and "kind: ext__batuta__publish_worktree" in loop
assert "kind: goal" not in loop and "agent: batuta-publisher" not in loop
assert "kind: ext__batuta__publication_verify" in loop
assert "publisher_result: \"{{ .nodes.publish.output }}\"" in loop
assert "compare_url" not in loop

edges = loop.split("\n  edges:\n", 1)[1]
pairs = set(re.findall(r"- from:\s*(\S+)\s*\n\s*to:\s*(\S+)", edges))
required = {
    ("load_check", "routing_context"),
    ("routing_context", "implement"),
    ("implement", "delivery_budget_context"),
    ("delivery_budget_context", "review"),
    ("review", "publication_plan"),
    ("publication_plan", "publication_route"),
    ("publication_route", "publish"),
    ("publication_route", "publication_verify_nothing"),
    ("publication_route", "publication_blocked_stop"),
    ("publish", "publication_verify"),
}
assert not (required - pairs), required - pairs

assert not Path("agents/batuta-publisher").exists()

assert "auto_commit=true" in conductor
assert "publication" in conductor.lower() and "automatic" in conductor.lower()
assert "merge remains manual" in conductor
assert "one PR per delivery phase" in conductor
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
