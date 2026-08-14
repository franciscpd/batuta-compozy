#!/usr/bin/env bash
# Roda todos os testes de contrato em ordem.
set -euo pipefail
CONTRACT_DIR=$(cd "$(dirname "$0")" && pwd -P)
REPO_ROOT=$(cd "$CONTRACT_DIR/../.." && pwd -P)
GENERATED_WORKSPACE="$REPO_ROOT/.compozy"
source "$CONTRACT_DIR/lib.sh"

preflight_contract_workspace "$REPO_ROOT"
WORKSPACE_ID=$(compozy workspace list -o json | python3 -c '
import json
import os
import sys

repo_root = os.path.realpath(sys.argv[1])
for workspace in json.load(sys.stdin):
    if os.path.realpath(workspace["root_dir"]) == repo_root:
        print(workspace["id"])
        break
' "$REPO_ROOT")
WORKSPACE_CREATED=false
if [[ -z $WORKSPACE_ID ]]; then
  WORKSPACE_ID=$(compozy workspace add "$REPO_ROOT" -o json | python3 -c \
    'import json, sys; print(json.load(sys.stdin)["id"])')
  WORKSPACE_CREATED=true
  cleanup_generated_workspace_marker "$REPO_ROOT"
fi

cleanup() {
  local original_status=$?
  local cleanup_failed=false
  trap - EXIT

  if workspace_marker_present "$REPO_ROOT"; then
    printf 'contract suite generated repository state: %s\n' \
      "$GENERATED_WORKSPACE" >&2
    if ! cleanup_generated_workspace_marker "$REPO_ROOT"; then
      cleanup_failed=true
    fi
    original_status=1
  fi

  if [[ $WORKSPACE_CREATED == true ]] && \
    ! compozy workspace remove "$WORKSPACE_ID" -o json >/dev/null; then
    printf 'cleanup failed to remove generated workspace registration: %s\n' \
      "$WORKSPACE_ID" >&2
    cleanup_failed=true
  fi

  if workspace_marker_present "$REPO_ROOT" && \
    ! cleanup_generated_workspace_marker "$REPO_ROOT"; then
    cleanup_failed=true
  fi

  if [[ $cleanup_failed == true ]]; then
    exit 1
  fi
  exit "$original_status"
}
trap cleanup EXIT

cd "$CONTRACT_DIR"
for t in test_*.sh; do
  echo "=== $t ==="
  "./$t"
  if workspace_marker_present "$REPO_ROOT"; then
    printf '%s generated repository state\n' "$t" >&2
    exit 1
  fi
done
echo "=== todos os testes de contrato passaram ==="
