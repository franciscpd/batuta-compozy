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

schema=$(compozy tool info compozy__session_prompt --workspace "$WS" -o json)
printf '%s' "$schema" | python3 -c '
import json, sys
d = json.load(sys.stdin)["tool"]["descriptor"]["input_schema"]
required = set(d["required"])
assert {"session_id", "message_id", "idempotency_key"}.issubset(required)
assert {"message", "attachments"}.issubset(d["properties"])
assert "not" in d, "session_prompt must require message or attachments"
assert "queue" in d["properties"]["mode"]["enum"]
print("OK: session_prompt idempotent queue contract holds")
'

python3 - agents/batuta/AGENT.md loops/batuta-deliver/loop.yaml <<'PY'
from pathlib import Path
import sys

agent = Path(sys.argv[1]).read_text(encoding="utf-8")
loop = Path(sys.argv[2]).read_text(encoding="utf-8")
agent_flat = " ".join(agent.split())

assert "durable acceptance is a hard turn boundary" in agent_flat
assert "end the turn" in agent_flat
assert "For an explicit progress question, make one `compozy__loop_status` read" in agent_flat
assert "reconcile_fallbacks" in agent_flat and "recover_delivery" in agent_flat
status_index = agent_flat.index("Call `compozy__loop_status`")
reconcile_index = agent_flat.index("reconcile_fallbacks", status_index)
recover_index = agent_flat.index("recover_delivery", reconcile_index)
assert status_index < reconcile_index < recover_index
for forbidden in ("poll until", "keep watching", "watcher agent"):
    assert forbidden not in agent_flat.lower(), forbidden

identity = "batuta-terminal-{{ .inputs.delivery_id }}-{{ .effect.identity.loop_run_id }}-g{{ .effect.identity.generation }}-{{ .effect.identity.trigger }}"
assert loop.count("message_id: \"" + identity + "\"") == 1
assert loop.count("idempotency_key: \"" + identity + "\"") == 1
for terminal in ("on_done", "on_noop", "on_blocked", "on_failed", "on_exhausted", "on_stalled", "on_canceled"):
    assert terminal + ":" in loop
print("OK: terminal returns are generation-scoped and reconciliation-first")
PY
