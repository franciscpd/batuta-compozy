#!/usr/bin/env bash
# Roda todos os testes de contrato em ordem.
set -euo pipefail
CONTRACT_DIR=$(cd "$(dirname "$0")" && pwd -P)
REPO_ROOT=$(cd "$CONTRACT_DIR/../.." && pwd -P)
GENERATED_WORKSPACE="$REPO_ROOT/.compozy"
source "$CONTRACT_DIR/lib.sh"

if [[ -e $GENERATED_WORKSPACE ]]; then
  printf 'contract suite requires a clean repository without %s\n' \
    "$GENERATED_WORKSPACE" >&2
  exit 1
fi

cleanup() {
  local original_status=$?
  local cleanup_failed=false
  trap - EXIT

  if [[ -e $GENERATED_WORKSPACE ]]; then
    printf 'contract suite generated repository state: %s\n' \
      "$GENERATED_WORKSPACE" >&2
    if ! cleanup_generated_workspace_marker "$REPO_ROOT"; then
      cleanup_failed=true
    fi
    original_status=1
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
  if [[ -e $GENERATED_WORKSPACE ]]; then
    printf '%s generated repository state\n' "$t" >&2
    exit 1
  fi
done
echo "=== todos os testes de contrato passaram ==="
