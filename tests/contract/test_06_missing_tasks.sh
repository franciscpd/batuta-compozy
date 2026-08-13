#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
source tests/contract/lib.sh
WS=$(require_test_workspace)

slug="_batuta_missing_contract_$(date +%s%N)_${RANDOM}_$$"
if [[ -e .compozy/tasks/$slug ]]; then
  printf 'nao foi possivel obter slug ausente unico: %s\n' "$slug" >&2
  exit 1
fi
pattern=".compozy/tasks/$slug/task_*.md"
OUT=$(mktemp)
ERR=$(mktemp)
cleanup() {
  rm -f -- "$OUT" "$ERR"
}
trap cleanup EXIT

if compozy tool invoke ext__dev_cycle__import_tasks \
  --workspace "$WS" --input "{\"pattern\":\"$pattern\"}" -o json \
  >"$OUT" 2>"$ERR"; then
  printf 'import_tasks aceitou task set inexistente\n' >&2
  exit 1
fi

if ! python3 - "$OUT" "$pattern" <<'PY'
import json
import sys

data = json.load(open(sys.argv[1]))
error = data["error"]
details = error["details"]
assert error["code"] == "tool_invalid_input", error
assert "dependency_missing" in error["reason_codes"], error
assert sys.argv[2] in details["operator_cause"], details
assert "Create the matching task set" in details["operator_recovery"], details
print("OK: import_tasks rejeita task set ausente com recuperacao publica")
PY
then
  while IFS= read -r diagnostic; do
    printf 'import_tasks stderr: %s\n' "$diagnostic" >&2
  done < "$ERR"
  exit 1
fi
