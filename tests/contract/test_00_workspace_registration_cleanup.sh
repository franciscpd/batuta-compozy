#!/usr/bin/env bash
# Prova que bootstrap de workspace limpa apenas o registro criado ao falhar.
set -euo pipefail
cd "$(dirname "$0")/../.."

RUNNER=$PWD/tests/contract/run.sh
LIB=$PWD/tests/contract/lib.sh
NOOP_FIXTURE=$PWD/tests/contract/fixtures/noop-contract-test.sh
EXIT_42_FIXTURE=$PWD/tests/contract/fixtures/exit-42-with-marker.sh
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
  local root name id expected_id
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
  cp "$NOOP_FIXTURE" "$CASE_DIR/tests/contract/test_99_noop.sh"
  : > "$STATE_DIR/log"
}

if grep -F 'BATUTA_CONTRACT_STOP_AFTER_WORKSPACE_SETUP' "$RUNNER" >/dev/null; then
  printf 'production runner retains a contract-suite stop hook\n' >&2
  exit 1
fi

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
    "$CASE_DIR/tests/contract/run.sh" >"$output" 2>&1
  status=$?
  set -e
  cat "$output"
  if grep -F 'Traceback (most recent call last):' "$output" >/dev/null; then
    printf '%s exposed a parser traceback\n' "$scenario" >&2
    rm -f -- "$output"
    return 1
  fi
  if ! grep -Fx 'OK: isolated contract fixture ran' "$output" >/dev/null; then
    printf '%s did not run the normal contract-test loop\n' "$scenario" >&2
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

run_nonzero_status_case() {
  local marker_failure=$1 output status expected
  prepare_case
  cp "$EXIT_42_FIXTURE" "$CASE_DIR/tests/contract/test_99_exit_42.sh"
  rm -f -- "$CASE_DIR/tests/contract/test_99_noop.sh"
  output=$(mktemp)
  if [[ $marker_failure == true ]]; then
    expected=1
  else
    expected=42
  fi
  set +e
  FAKE_COMPOZY_STATE=$STATE_DIR \
    FAKE_COMPOZY_SCENARIO=normal \
    FIXTURE_MARKER_CLEANUP_FAILURE=$([[ $marker_failure == true ]] && printf 1) \
    "$CASE_DIR/tests/contract/run.sh" >"$output" 2>&1
  status=$?
  set -e
  cat "$output"
  rm -f -- "$output"
  assert_created_registration_removed "nonzero-status-$marker_failure"
  [[ $status -eq $expected ]] || {
    printf 'nonzero-status-%s exited %s, expected %s\n' \
      "$marker_failure" "$status" "$expected" >&2
    return 1
  }
  if [[ $marker_failure == false && -e $CASE_DIR/.compozy ]]; then
    printf 'nonzero-status-success left a workspace marker\n' >&2
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
run_nonzero_status_case false
run_nonzero_status_case true
printf 'OK: workspace bootstrap cleanup survives add-output and marker-cleanup failures\n'
