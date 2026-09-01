#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

TMP=$(mktemp -d)
remove_tmp() {
  rm -rf -- "$TMP"
}
trap remove_tmp EXIT

CLEANUP_SOURCE=$(python3 - tests/contract/test_04_deliver_validate.sh <<'PY'
from pathlib import Path
import sys

source = Path(sys.argv[1]).read_text(encoding="utf-8")
start = source.index("cleanup() {")
end = source.index("\n}\ntrap cleanup EXIT", start) + 3
print(source[start:end])
PY
)

run_case() {
  local original_status=$1 expected_status=$2
  local case_root log output status
  case_root=$(mktemp -d "$TMP/case.XXXXXX")
  mkdir -p "$case_root/smoke"
  log="$case_root/calls"

  set +e
  output=$(BATUTA_CLEANUP_SOURCE="$CLEANUP_SOURCE" \
    BATUTA_ORIGINAL_STATUS="$original_status" \
    BATUTA_CASE_ROOT="$case_root" \
    BATUTA_FAKE_LOG="$log" \
    bash -c '
set -euo pipefail
eval "$BATUTA_CLEANUP_SOURCE"
REPO_ROOT=/fake/repository
REPO_WORKSPACE_PREEXISTED=false
WS=workspace
INSTALLED_HERE=true
SMOKE_ROOT="$BATUTA_CASE_ROOT/smoke"
SMOKE_LAUNCHER_STATUS="$BATUTA_CASE_ROOT/launcher-status"
SMOKE_STATUS="$BATUTA_CASE_ROOT/core-status"
SMOKE_CORE_RUN_ID=core-run
SMOKE_RUN_ID=launcher-run
timeout() {
  printf "timeout %s\n" "$*" >> "$BATUTA_FAKE_LOG"
  if [[ $1 == 30s ]]; then
    return 7
  fi
  return 0
}
rm() {
  printf "rm %s\n" "$*" >> "$BATUTA_FAKE_LOG"
  if [[ $1 == -rf ]]; then
    return 8
  fi
  return 9
}
reject_new_repository_marker() {
  printf "marker %s %s\n" "$1" "$2" >> "$BATUTA_FAKE_LOG"
  return 10
}
trap cleanup EXIT
exit "$BATUTA_ORIGINAL_STATUS"
' 2>&1)
  status=$?
  set -e

  if [[ $status -ne $expected_status ]]; then
    printf 'cleanup status mismatch for original %s: got %s, want %s: %s\n' \
      "$original_status" "$status" "$expected_status" "$output" >&2
    exit 1
  fi

  local -a expected=(
    "timeout 15s compozy loop cancel --workspace workspace --run-id core-run -o json"
    "timeout 15s compozy loop cancel --workspace workspace --run-id launcher-run -o json"
    "rm -rf -- $case_root/smoke"
    "timeout 30s compozy extension remove batuta --global -o json"
    "rm -f -- $case_root/launcher-status $case_root/core-status"
    "marker /fake/repository false"
  )
  local -a actual=()
  mapfile -t actual < "$log"
  if [[ ${#actual[@]} -ne ${#expected[@]} ]]; then
    printf 'cleanup call count mismatch for original %s: %s\n' \
      "$original_status" "${actual[*]}" >&2
    exit 1
  fi
  local index
  for index in "${!expected[@]}"; do
    if [[ ${actual[$index]} != "${expected[$index]}" ]]; then
      printf 'cleanup call %s mismatch for original %s: got %s, want %s\n' \
        "$index" "$original_status" "${actual[$index]}" "${expected[$index]}" >&2
      exit 1
    fi
  done
}

run_case 23 23
run_case 124 124
run_case 0 1

printf 'OK: delivery smoke cleanup preserves existing failures and reports clean-run cleanup failure\n'
