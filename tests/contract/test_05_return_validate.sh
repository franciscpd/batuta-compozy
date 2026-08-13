#!/usr/bin/env bash
# Valida o contrato público necessário para o retorno terminal de batuta-deliver.
set -euo pipefail
cd "$(dirname "$0")/../.."
source tests/contract/lib.sh
REPO_ROOT=$PWD
REPO_WORKSPACE_PREEXISTED=false
if [[ -e .compozy ]]; then
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

out=$(compozy loop validate --file loops/batuta-deliver/loop.yaml --workspace "$WS" -o json)
printf '%s' "$out" | python3 -c '
import json, sys
d = json.load(sys.stdin)
assert d.get("valid") is True, f"batuta-deliver invalido: {d}"
print("OK: batuta-deliver valido (efeitos terminais compilados)")
'
