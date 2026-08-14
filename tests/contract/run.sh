#!/usr/bin/env bash
# Roda todos os testes de contrato em ordem.
set -euo pipefail
CONTRACT_DIR=$(cd "$(dirname "$0")" && pwd -P)
REPO_ROOT=$(cd "$CONTRACT_DIR/../.." && pwd -P)
GENERATED_WORKSPACE="$REPO_ROOT/.compozy"
source "$CONTRACT_DIR/lib.sh"

WORKSPACE_ID=
WORKSPACE_NAME="batuta-contract-$$"
WORKSPACE_CREATION_ATTEMPTED=false
WORKSPACE_ADD_OUTPUT=

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
    if [[ $original_status -eq 0 ]]; then
      original_status=1
    fi
  fi

  if [[ $WORKSPACE_CREATION_ATTEMPTED == true ]]; then
    local created_workspace_id
    if ! created_workspace_id=$(workspace_id_for_canonical_root \
      "$REPO_ROOT" "$WORKSPACE_NAME"); then
      printf 'cleanup failed to resolve generated workspace registration\n' >&2
      cleanup_failed=true
    elif [[ -n $created_workspace_id ]] && \
      ! compozy workspace remove "$created_workspace_id" -o json >/dev/null; then
      printf 'cleanup failed to remove generated workspace registration: %s\n' \
        "$created_workspace_id" >&2
      cleanup_failed=true
    fi
  fi

  if workspace_marker_present "$REPO_ROOT" && \
    ! cleanup_generated_workspace_marker "$REPO_ROOT"; then
    cleanup_failed=true
  fi

  if [[ -n $WORKSPACE_ADD_OUTPUT ]] && ! rm -f -- "$WORKSPACE_ADD_OUTPUT"; then
    cleanup_failed=true
  fi

  if [[ $cleanup_failed == true ]]; then
    exit 1
  fi
  exit "$original_status"
}
trap cleanup EXIT

preflight_contract_workspace "$REPO_ROOT"
if ! WORKSPACE_ID=$(workspace_id_for_canonical_root "$REPO_ROOT"); then
  exit 1
fi
if [[ -z $WORKSPACE_ID ]]; then
  WORKSPACE_ADD_OUTPUT=$(mktemp)
  WORKSPACE_CREATION_ATTEMPTED=true
  compozy workspace add "$REPO_ROOT" --name "$WORKSPACE_NAME" -o json \
    > "$WORKSPACE_ADD_OUTPUT"

  reported_workspace_id=
  if ! reported_workspace_id=$(python3 - "$WORKSPACE_ADD_OUTPUT" <<'PY'
import json
import sys

try:
    workspace = json.load(open(sys.argv[1]))
    workspace_id = workspace["id"]
except (json.JSONDecodeError, KeyError, OSError) as error:
    raise SystemExit(f"workspace add result is unreadable: {error}") from None
if not isinstance(workspace_id, str):
    raise SystemExit("workspace add result has invalid id")
print(workspace_id)
PY
  ); then
    printf 'workspace add result could not be parsed; resolving by canonical root\n' >&2
  fi

  if ! WORKSPACE_ID=$(workspace_id_for_canonical_root \
    "$REPO_ROOT" "$WORKSPACE_NAME"); then
    exit 1
  fi
  if [[ -z $WORKSPACE_ID ]]; then
    printf 'workspace add did not register the expected canonical root\n' >&2
    exit 1
  fi
  if [[ -n $reported_workspace_id && $reported_workspace_id != "$WORKSPACE_ID" ]]; then
    printf 'workspace add result id does not match its canonical registration\n' >&2
    exit 1
  fi
  cleanup_generated_workspace_marker "$REPO_ROOT"
fi

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
