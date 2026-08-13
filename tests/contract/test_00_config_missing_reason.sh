#!/usr/bin/env bash
# Prova que caminho de configuracao ausente nao se mascara como ferramenta ausente.
set -euo pipefail
cd "$(dirname "$0")/../.."
source tests/contract/lib.sh

REPO_ROOT=$PWD
REPO_WORKSPACE_PREEXISTED=false
if workspace_marker_present "$REPO_ROOT"; then
  REPO_WORKSPACE_PREEXISTED=true
fi
WS=$(require_test_workspace)
OUT=$(mktemp)
ERR=$(mktemp)
cleanup() {
  local original_status=$?
  local cleanup_failed=false
  trap - EXIT

  if ! rm -f -- "$OUT" "$ERR"; then
    cleanup_failed=true
  fi
  if [[ $REPO_WORKSPACE_PREEXISTED == false ]] && \
    ! cleanup_generated_workspace_marker "$REPO_ROOT"; then
    cleanup_failed=true
  fi
  if [[ $cleanup_failed == true ]]; then
    exit 1
  fi
  exit "$original_status"
}
trap cleanup EXIT

path="loops.inputs.batuta-deliver.__missing_contract_probe_$$"
if compozy tool invoke compozy__config_get --workspace "$WS" \
  --input "{\"path\":\"$path\"}" -o json >"$OUT" 2>"$ERR"; then
  printf 'config_get aceitou caminho ausente: %s\n' "$path" >&2
  exit 1
fi

if ! python3 - "$OUT" <<'PY'
import json
import sys

data = json.load(open(sys.argv[1]))
error = data["error"]
assert error["tool_id"] == "compozy__config_get", error
assert "config_path_not_found" in error["reason_codes"], error
assert "tool_unknown" not in error["reason_codes"], error
print("OK: config_get distingue caminho ausente de ferramenta ausente")
PY
then
  while IFS= read -r diagnostic; do
    printf 'config_get stderr: %s\n' "$diagnostic" >&2
  done < "$ERR"
  exit 1
fi
