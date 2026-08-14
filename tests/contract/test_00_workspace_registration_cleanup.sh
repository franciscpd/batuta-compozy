#!/usr/bin/env bash
# Prova que bootstrap de workspace limpa apenas o registro criado ao falhar.
set -euo pipefail
cd "$(dirname "$0")/../.."

RUNNER=$PWD/tests/contract/run.sh
LIB=$PWD/tests/contract/lib.sh
CASE_DIR=
STATE_DIR=

cleanup() {
  local status=$?
  trap - EXIT
  if [[ -n $CASE_DIR && -d $CASE_DIR ]]; then
    rm -rf -- "$CASE_DIR"
  fi
  if [[ -n $STATE_DIR && -d $STATE_DIR ]]; then
    rm -rf -- "$STATE_DIR"
  fi
  exit "$status"
}
trap cleanup EXIT

compozy() {
  local root name id
  case "$1 $2" in
    "workspace list")
      if [[ ! -f $FAKE_COMPOZY_STATE/registration ]]; then
        printf '[]\n'
        return
      fi
      IFS= read -r root < "$FAKE_COMPOZY_STATE/registration"
      IFS= read -r name < <(sed -n '2p' "$FAKE_COMPOZY_STATE/registration")
      IFS= read -r id < <(sed -n '3p' "$FAKE_COMPOZY_STATE/registration")
      printf '[{"id":"%s","root_dir":"%s","name":"%s"}]\n' \
        "$id" "$root" "$name"
      ;;
    "workspace add")
      root=$3
      name=
      shift 3
      while (($#)); do
        if [[ $1 == --name ]]; then
          name=$2
          shift 2
          continue
        fi
        shift
      done
      printf '%s\n%s\n%s\n' "$root" "$name" "ws-created" \
        > "$FAKE_COMPOZY_STATE/registration"
      mkdir -p "$root/.compozy"
      : > "$root/.compozy/workspace.toml"
      if [[ $FAKE_COMPOZY_SCENARIO == marker-cleanup-failure ]]; then
        : > "$root/.compozy/unexpected"
      fi
      printf 'add:%s:%s\n' "$root" "$name" >> "$FAKE_COMPOZY_STATE/log"
      case $FAKE_COMPOZY_SCENARIO in
        malformed-add-output) printf '{invalid json\n' ;;
        missing-add-id) printf '{"root_dir":"%s"}\n' "$root" ;;
        *) printf '{"id":"ws-created","root_dir":"%s","name":"%s"}\n' "$root" "$name" ;;
      esac
      ;;
    "workspace remove")
      id=$3
      printf 'remove:%s\n' "$id" >> "$FAKE_COMPOZY_STATE/log"
      IFS= read -r root < "$FAKE_COMPOZY_STATE/registration"
      IFS= read -r name < <(sed -n '2p' "$FAKE_COMPOZY_STATE/registration")
      IFS= read -r expected_id < <(sed -n '3p' "$FAKE_COMPOZY_STATE/registration")
      [[ $id == "$expected_id" ]]
      rm -f -- "$FAKE_COMPOZY_STATE/registration"
      mkdir -p "$root/.compozy"
      : > "$root/.compozy/workspace.toml"
      printf '{"id":"%s"}\n' "$id"
      ;;
    *)
      printf 'unexpected fake compozy invocation: %s\n' "$*" >&2
      return 64
      ;;
  esac
}
export -f compozy

prepare_case() {
  CASE_DIR=$(mktemp -d /tmp/batuta-contract-registration.XXXXXX)
  STATE_DIR=$(mktemp -d /tmp/batuta-contract-registration-state.XXXXXX)
  mkdir -p "$CASE_DIR/tests/contract"
  cp "$RUNNER" "$LIB" "$CASE_DIR/tests/contract/"
  : > "$STATE_DIR/log"
}

assert_created_registration_removed() {
  local label=$1
  if [[ -e $STATE_DIR/registration ]]; then
    printf '%s leaked its created workspace registration\n' "$label" >&2
    return 1
  fi
  if ! grep -Fx 'remove:ws-created' "$STATE_DIR/log" >/dev/null; then
    printf '%s did not remove the exact created registration\n' "$label" >&2
    return 1
  fi
}

run_parse_case() {
  local scenario=$1 output status
  prepare_case
  output=$(mktemp)
  set +e
  FAKE_COMPOZY_STATE=$STATE_DIR \
    FAKE_COMPOZY_SCENARIO=$scenario \
    BATUTA_CONTRACT_STOP_AFTER_WORKSPACE_SETUP=1 \
    "$CASE_DIR/tests/contract/run.sh" >"$output" 2>&1
  status=$?
  set -e
  cat "$output"
  if grep -F 'Traceback (most recent call last):' "$output" >/dev/null; then
    printf '%s exposed a parser traceback\n' "$scenario" >&2
    rm -f -- "$output"
    return 1
  fi
  rm -f -- "$output"
  assert_created_registration_removed "$scenario"
  [[ $status -eq 0 ]] || {
    printf '%s exited %s after recovering add output\n' "$scenario" "$status" >&2
    return 1
  }
  [[ ! -e $CASE_DIR/.compozy ]] || {
    printf '%s left a workspace marker\n' "$scenario" >&2
    return 1
  }
  rm -rf -- "$CASE_DIR" "$STATE_DIR"
  CASE_DIR=
  STATE_DIR=
}

run_marker_cleanup_failure_case() {
  local output status
  prepare_case
  output=$(mktemp)
  set +e
  FAKE_COMPOZY_STATE=$STATE_DIR \
    FAKE_COMPOZY_SCENARIO=marker-cleanup-failure \
    BATUTA_CONTRACT_STOP_AFTER_WORKSPACE_SETUP=1 \
    "$CASE_DIR/tests/contract/run.sh" >"$output" 2>&1
  status=$?
  set -e
  cat "$output"
  rm -f -- "$output"
  assert_created_registration_removed marker-cleanup-failure
  [[ $status -eq 1 ]] || {
    printf 'marker-cleanup-failure exited %s, expected cleanup failure\n' "$status" >&2
    return 1
  }
  rm -rf -- "$CASE_DIR" "$STATE_DIR"
  CASE_DIR=
  STATE_DIR=
}

run_preexisting_registration_case() {
  local output status
  prepare_case
  printf '%s\n%s\n%s\n' "$CASE_DIR" "operator-owned" "ws-existing" \
    > "$STATE_DIR/registration"
  output=$(mktemp)
  set +e
  FAKE_COMPOZY_STATE=$STATE_DIR \
    FAKE_COMPOZY_SCENARIO=preexisting-registration \
    BATUTA_CONTRACT_STOP_AFTER_WORKSPACE_SETUP=1 \
    "$CASE_DIR/tests/contract/run.sh" >"$output" 2>&1
  status=$?
  set -e
  cat "$output"
  rm -f -- "$output"
  [[ $status -eq 0 ]] || {
    printf 'preexisting-registration exited %s\n' "$status" >&2
    return 1
  }
  [[ -e $STATE_DIR/registration ]] || {
    printf 'preexisting-registration was removed\n' >&2
    return 1
  }
  if grep -Eq '^(add|remove):' "$STATE_DIR/log"; then
    printf 'preexisting-registration was mutated\n' >&2
    return 1
  fi
  rm -rf -- "$CASE_DIR" "$STATE_DIR"
  CASE_DIR=
  STATE_DIR=
}

run_parse_case malformed-add-output
run_parse_case missing-add-id
run_marker_cleanup_failure_case
run_preexisting_registration_case
printf 'OK: workspace bootstrap cleanup survives add-output and marker-cleanup failures\n'
