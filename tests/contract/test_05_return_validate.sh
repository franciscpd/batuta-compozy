#!/usr/bin/env bash
# Valida o contrato público necessário para o retorno terminal de batuta-deliver.
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

schema=$(compozy tool info compozy__session_prompt --workspace "$WS" -o json)
printf '%s' "$schema" | python3 -c '
import json, sys
d = json.load(sys.stdin)["tool"]["descriptor"]["input_schema"]
required = set(d["required"])
expected = {"session_id", "message", "message_id", "idempotency_key"}
assert required == expected, f"campos obrigatorios inesperados: {required}"
assert "queue" in d["properties"]["mode"]["enum"], "mode=queue indisponivel"
print("OK: session_prompt aceita identidade idempotente e mode=queue")
'

python3 - <<'PY'
from pathlib import Path
import re

agent = Path("agents/batuta/AGENT.md").read_text()
required_clauses = (
    "successful real dispatch is a hard turn boundary",
    "end the turn without another tool call",
    "first operational tool call is compozy__loop_status",
    "one compozy__loop_status read",
)
for clause in required_clauses:
    assert clause in agent, f"missing Batuta return-boundary clause: {clause!r}"

assert "While a run is live: observe with" not in agent, (
    "live-run watcher instruction must not remain after accepted dispatch"
)
accepted_dispatch = r"(?:after|following)\s+(?:an?\s+)?accepted(?:\s+real)?\s+dispatch"
watcher_instruction = r"(?:poll|keep\s+watching)"
assert not re.search(
    rf"{accepted_dispatch}[^.\n]*{watcher_instruction}", agent, re.IGNORECASE
), "accepted dispatch must not instruct Batuta to poll or keep watching"
assert not re.search(
    rf"{watcher_instruction}[^.\n]*{accepted_dispatch}", agent, re.IGNORECASE
), "accepted dispatch must not instruct Batuta to poll or keep watching"
print("OK: Batuta dispatch boundary is authored")
PY

out=$(compozy loop validate --file loops/batuta-deliver/loop.yaml --workspace "$WS" -o json)
printf '%s' "$out" | python3 -c '
import json, sys
d = json.load(sys.stdin)
assert d.get("valid") is True, f"batuta-deliver invalido: {d}"
print("OK: batuta-deliver valido (efeitos terminais compilados)")
'
