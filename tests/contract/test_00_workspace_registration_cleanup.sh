#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

RUNNER=$PWD/tests/contract/run.sh
LIB=$PWD/tests/contract/lib.sh
EXTERNAL_FIXTURE=$PWD/tests/contract/fixtures/external-workspace-contract-test.sh
EXIT_42_FIXTURE=$PWD/tests/contract/fixtures/exit-42.sh
FOREIGN_MARKER_FIXTURE=$PWD/tests/contract/fixtures/exit-42-with-marker.sh
SUBSTITUTE_ROOT_FIXTURE=$PWD/tests/contract/fixtures/substitute-external-workspace-root.sh
REAL_CLEANUP_CHILD=$PWD/tests/contract/test_00_config_missing_reason.sh
CASE_DIR=
STATE_DIR=
ALIAS_PATH=
SUBSTITUTED_ROOT=
SUBSTITUTION_TARGET=
SUBSTITUTION_TARGET_CANONICAL=
SUBSTITUTION_TARGET_DEVICE=
SUBSTITUTION_TARGET_INODE=
ADVERSARIAL_GUARD_ROOT=

source "$LIB"

if ! git check-ignore -q --no-index -- .compozy/workspace.toml; then
  printf '.compozy must be ignored without touching the live marker\n' >&2
  exit 1
fi

cleanup() {
  local status=$?
  trap - EXIT
  if [[ -n $SUBSTITUTION_TARGET ]] && ! cleanup_substitution_target; then
    status=1
  fi
  if [[ -n $CASE_DIR && -d $CASE_DIR ]]; then
    rm -rf -- "$CASE_DIR"
  fi
  if [[ -n $STATE_DIR && -d $STATE_DIR ]]; then
    rm -rf -- "$STATE_DIR"
  fi
  if [[ -n $ALIAS_PATH && -L $ALIAS_PATH ]]; then
    rm -f -- "$ALIAS_PATH"
  fi
  if [[ -z $SUBSTITUTION_TARGET && -n $SUBSTITUTED_ROOT && \
    -L $SUBSTITUTED_ROOT ]]; then
    rm -f -- "$SUBSTITUTED_ROOT"
  fi
  if [[ -n $ADVERSARIAL_GUARD_ROOT ]]; then
    case $ADVERSARIAL_GUARD_ROOT in
      /tmp/batuta-contract-workspace.*)
        if [[ ! -L $ADVERSARIAL_GUARD_ROOT && \
          -d $ADVERSARIAL_GUARD_ROOT ]]; then
          rmdir "$ADVERSARIAL_GUARD_ROOT" || status=1
        else
          status=1
        fi
        ;;
      *) status=1 ;;
    esac
  fi
  exit "$status"
}
trap cleanup EXIT

capture_substitution_target() {
  local candidate=$1 canonical
  if [[ -L $candidate || ! -d $candidate ]]; then
    printf 'refusing invalid substitution target: %s\n' "$candidate" >&2
    return 1
  fi
  canonical=$(cd "$candidate" && pwd -P)
  if [[ $canonical != "$candidate" ]]; then
    printf 'refusing redirected substitution target: %s\n' "$candidate" >&2
    return 1
  fi
  case $canonical in
    /tmp/batuta-contract-substitution-target.*) ;;
    *)
      printf 'refusing unguarded substitution target: %s\n' "$canonical" >&2
      return 1
      ;;
  esac
  SUBSTITUTION_TARGET=$candidate
  SUBSTITUTION_TARGET_CANONICAL=$canonical
  read -r SUBSTITUTION_TARGET_DEVICE SUBSTITUTION_TARGET_INODE < <(
    stat -c '%d %i' "$candidate"
  )
}

cleanup_substitution_target() {
  local current_canonical current_device current_inode
  if [[ -L $SUBSTITUTION_TARGET || ! -d $SUBSTITUTION_TARGET ]]; then
    printf 'refusing substituted cleanup target: %s\n' \
      "$SUBSTITUTION_TARGET" >&2
    return 1
  fi
  current_canonical=$(cd "$SUBSTITUTION_TARGET" && pwd -P)
  if [[ $current_canonical != "$SUBSTITUTION_TARGET_CANONICAL" || \
    $current_canonical != "$SUBSTITUTION_TARGET" ]]; then
    printf 'refusing redirected cleanup target: %s\n' \
      "$SUBSTITUTION_TARGET" >&2
    return 1
  fi
  case $current_canonical in
    /tmp/batuta-contract-substitution-target.*) ;;
    *)
      printf 'refusing unguarded cleanup target: %s\n' \
        "$current_canonical" >&2
      return 1
      ;;
  esac
  read -r current_device current_inode < <(
    stat -c '%d %i' "$SUBSTITUTION_TARGET"
  )
  if [[ $current_device != "$SUBSTITUTION_TARGET_DEVICE" || \
    $current_inode != "$SUBSTITUTION_TARGET_INODE" ]]; then
    printf 'refusing replaced cleanup target: %s\n' \
      "$SUBSTITUTION_TARGET" >&2
    return 1
  fi
  if ! find "$SUBSTITUTION_TARGET_CANONICAL" -depth -delete; then
    return 1
  fi
  SUBSTITUTION_TARGET=
  SUBSTITUTION_TARGET_CANONICAL=
  SUBSTITUTION_TARGET_DEVICE=
  SUBSTITUTION_TARGET_INODE=
}

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
      printf '%s\n' "$root" > "$FAKE_COMPOZY_STATE/workspace-root"
      stat -c '%d %i' "$root" > "$FAKE_COMPOZY_STATE/workspace-identity"
      mkdir -p "$root/.compozy"
      printf 'workspace add external marker\n' > "$root/.compozy/workspace.toml"
      printf 'add:%s:%s\n' "$root" "$name" >> "$FAKE_COMPOZY_STATE/log"
      case ${FAKE_COMPOZY_SCENARIO:-normal} in
        malformed-add-output) printf '{invalid json\n' ;;
        missing-add-id) printf '{"root_dir":"%s"}\n' "$root" ;;
        *) printf '{"id":"ws-created","root_dir":"%s","name":"%s"}\n' "$root" "$name" ;;
      esac
      ;;
    "workspace remove")
      id=$3
      printf 'remove:%s\n' "$id" >> "$FAKE_COMPOZY_STATE/log"
      IFS= read -r expected_id < <(sed -n '3p' "$FAKE_COMPOZY_STATE/registration")
      [[ $id == "$expected_id" ]]
      if [[ ${FAKE_COMPOZY_SCENARIO:-normal} == remove-failure ]]; then
        return 1
      fi
      rm -f -- "$FAKE_COMPOZY_STATE/registration"
      printf '{"id":"%s"}\n' "$id"
      ;;
    "tool invoke")
      if [[ ${FAKE_COMPOZY_SCENARIO:-normal} != child-foreign-marker ]]; then
        printf 'unexpected fake compozy invocation: %s\n' "$*" >&2
        return 64
      fi
      mkdir -p "$PWD/.compozy"
      printf 'child-generated foreign marker\n' \
        > "$PWD/.compozy/workspace.toml"
      printf 'child-generated foreign marker\n' \
        > "$FAKE_COMPOZY_STATE/child-marker.expected"
      printf '%s\n' \
        '{"error":{"tool_id":"compozy__config_get","reason_codes":["config_path_not_found"]}}'
      return 1
      ;;
    *)
      printf 'unexpected fake compozy invocation: %s\n' "$*" >&2
      return 64
      ;;
  esac
}
export -f compozy

prepare_case() {
  local fixture=$1
  CASE_DIR=$(mktemp -d /tmp/batuta-contract-registration.XXXXXX)
  STATE_DIR=$(mktemp -d /tmp/batuta-contract-registration-state.XXXXXX)
  mkdir -p "$CASE_DIR/tests/contract"
  cp "$RUNNER" "$LIB" "$CASE_DIR/tests/contract/"
  cp "$fixture" "$CASE_DIR/tests/contract/test_99_fixture.sh"
  : > "$STATE_DIR/log"
}

finish_case() {
  rm -rf -- "$CASE_DIR" "$STATE_DIR"
  CASE_DIR=
  STATE_DIR=
}

prepare_real_cleanup_case() {
  CASE_DIR=$(mktemp -d /tmp/batuta-contract-registration.XXXXXX)
  STATE_DIR=$(mktemp -d /tmp/batuta-contract-registration-state.XXXXXX)
  mkdir -p "$CASE_DIR/tests/contract"
  cp "$RUNNER" "$LIB" "$CASE_DIR/tests/contract/"
  cp "$REAL_CLEANUP_CHILD" "$CASE_DIR/tests/contract/test_99_fixture.sh"
  : > "$STATE_DIR/log"
}

assert_external_cleanup() {
  local label=$1 root
  if [[ -e $STATE_DIR/registration ]]; then
    printf '%s leaked its external workspace registration\n' "$label" >&2
    return 1
  fi
  if ! grep -Fx 'remove:ws-created' "$STATE_DIR/log" >/dev/null; then
    printf '%s did not remove the exact external registration\n' "$label" >&2
    return 1
  fi
  IFS= read -r root < "$STATE_DIR/workspace-root"
  [[ $root == /tmp/batuta-contract-workspace.* ]] || {
    printf '%s registered unguarded workspace root: %s\n' "$label" "$root" >&2
    return 1
  }
  if [[ -e $root || -L $root ]]; then
    printf '%s leaked its external workspace root: %s\n' "$label" "$root" >&2
    return 1
  fi
}

run_external_case() {
  local scenario=${1:-normal} output status
  prepare_case "$EXTERNAL_FIXTURE"
  output=$(mktemp)
  set +e
  FAKE_COMPOZY_STATE=$STATE_DIR \
    FAKE_COMPOZY_SCENARIO=$scenario \
    "$CASE_DIR/tests/contract/run.sh" >"$output" 2>&1
  status=$?
  set -e
  cat "$output"
  if ! grep -Fx 'OK: contracts use only the external runner workspace' \
    "$output" >/dev/null; then
    printf '%s did not run against the external workspace\n' "$scenario" >&2
    rm -f -- "$output"
    return 1
  fi
  rm -f -- "$output"
  [[ $status -eq 0 ]] || {
    printf '%s exited %s, expected success\n' "$scenario" "$status" >&2
    return 1
  }
  [[ ! -e $CASE_DIR/.compozy && ! -L $CASE_DIR/.compozy ]] || {
    printf '%s generated repository marker state\n' "$scenario" >&2
    return 1
  }
  assert_external_cleanup "$scenario"
  finish_case
}

run_foreign_marker_case() {
  local output status
  prepare_case "$FOREIGN_MARKER_FIXTURE"
  printf 'foreign repository marker\n' > "$STATE_DIR/foreign-marker.expected"
  output=$(mktemp)
  set +e
  FAKE_COMPOZY_STATE=$STATE_DIR \
    "$CASE_DIR/tests/contract/run.sh" >"$output" 2>&1
  status=$?
  set -e
  cat "$output"
  rm -f -- "$output"
  [[ $status -eq 1 ]] || {
    printf 'foreign-marker exited %s, expected ownership failure\n' "$status" >&2
    return 1
  }
  if [[ ! -f $CASE_DIR/.compozy/workspace.toml ]] ||
    ! cmp -s -- "$STATE_DIR/foreign-marker.expected" \
      "$CASE_DIR/.compozy/workspace.toml"; then
    printf 'foreign repository marker evidence was not preserved\n' >&2
    return 1
  fi
  assert_external_cleanup foreign-marker
  finish_case
}

run_child_cleanup_marker_case() {
  local output status
  prepare_real_cleanup_case
  output=$(mktemp)
  set +e
  FAKE_COMPOZY_STATE=$STATE_DIR \
    FAKE_COMPOZY_SCENARIO=child-foreign-marker \
    "$CASE_DIR/tests/contract/run.sh" >"$output" 2>&1
  status=$?
  set -e
  cat "$output"
  rm -f -- "$output"
  [[ $status -eq 1 ]] || {
    printf 'real-child-marker exited %s, expected ownership failure\n' \
      "$status" >&2
    return 1
  }
  if [[ ! -f $CASE_DIR/.compozy/workspace.toml ]] ||
    ! cmp -s -- "$STATE_DIR/child-marker.expected" \
      "$CASE_DIR/.compozy/workspace.toml"; then
    printf 'real child cleanup erased foreign marker evidence\n' >&2
    return 1
  fi
  assert_external_cleanup real-child-marker
  finish_case
}

run_preexisting_marker_case() {
  local output status
  prepare_case "$EXTERNAL_FIXTURE"
  mkdir "$CASE_DIR/.compozy"
  printf 'operator-owned marker\n' > "$CASE_DIR/.compozy/workspace.toml"
  cp -p -- "$CASE_DIR/.compozy/workspace.toml" \
    "$STATE_DIR/workspace.toml.before"
  output=$(mktemp)
  set +e
  FAKE_COMPOZY_STATE=$STATE_DIR \
    "$CASE_DIR/tests/contract/run.sh" >"$output" 2>&1
  status=$?
  set -e
  cat "$output"
  rm -f -- "$output"
  [[ $status -eq 1 ]] || {
    printf 'preexisting-marker exited %s, expected preflight rejection\n' \
      "$status" >&2
    return 1
  }
  if [[ ! -f $CASE_DIR/.compozy/workspace.toml ]] ||
    ! cmp -s -- "$STATE_DIR/workspace.toml.before" \
      "$CASE_DIR/.compozy/workspace.toml"; then
    printf 'preexisting marker changed during preflight rejection\n' >&2
    return 1
  fi
  if grep -Eq '^(add|remove):' "$STATE_DIR/log"; then
    printf 'preexisting marker triggered workspace mutation\n' >&2
    return 1
  fi
  finish_case
}

run_nonzero_status_case() {
  local output status
  prepare_case "$EXIT_42_FIXTURE"
  output=$(mktemp)
  set +e
  FAKE_COMPOZY_STATE=$STATE_DIR \
    "$CASE_DIR/tests/contract/run.sh" >"$output" 2>&1
  status=$?
  set -e
  cat "$output"
  rm -f -- "$output"
  [[ $status -eq 42 ]] || {
    printf 'nonzero-status exited %s, expected 42\n' "$status" >&2
    return 1
  }
  assert_external_cleanup nonzero-status
  finish_case
}

run_remove_failure_case() {
  local fixture=$1 expected_label=$2 output status root
  prepare_case "$fixture"
  output=$(mktemp)
  set +e
  FAKE_COMPOZY_STATE=$STATE_DIR \
    FAKE_COMPOZY_SCENARIO=remove-failure \
    "$CASE_DIR/tests/contract/run.sh" >"$output" 2>&1
  status=$?
  set -e
  cat "$output"
  rm -f -- "$output"
  [[ $status -eq 1 ]] || {
    printf '%s remove failure exited %s, expected 1\n' \
      "$expected_label" "$status" >&2
    return 1
  }
  [[ -f $STATE_DIR/registration ]] || {
    printf '%s remove failure lost registration evidence\n' \
      "$expected_label" >&2
    return 1
  }
  IFS= read -r root < "$STATE_DIR/workspace-root"
  [[ ! -e $root && ! -L $root ]] || {
    printf '%s remove failure leaked owned external root\n' \
      "$expected_label" >&2
    return 1
  }
  finish_case
}

run_root_substitution_case() {
  local child_status=$1 output status root target
  prepare_case "$SUBSTITUTE_ROOT_FIXTURE"
  output=$(mktemp)
  set +e
  FAKE_COMPOZY_STATE=$STATE_DIR \
    FIXTURE_EXIT_STATUS=$child_status \
    "$CASE_DIR/tests/contract/run.sh" >"$output" 2>&1
  status=$?
  set -e
  cat "$output"
  rm -f -- "$output"
  IFS= read -r root < "$STATE_DIR/workspace-root"
  IFS= read -r target < "$STATE_DIR/substitution-target"
  case $root in
    /tmp/batuta-contract-workspace.*) ;;
    *)
      printf 'root-substitution-%s reported unguarded root: %s\n' \
        "$child_status" "$root" >&2
      return 1
      ;;
  esac
  capture_substitution_target "$target"
  SUBSTITUTED_ROOT=$root
  [[ $status -eq 1 ]] || {
    printf 'root-substitution-%s exited %s, expected cleanup failure\n' \
      "$child_status" "$status" >&2
    return 1
  }
  if [[ ! -L $root || $(readlink "$root") != "$target" ]] ||
    [[ ! -f $target/workspace.toml ]]; then
    printf 'root-substitution-%s evidence was not preserved\n' \
      "$child_status" >&2
    return 1
  fi
  [[ ! -e $STATE_DIR/registration ]] || {
    printf 'root-substitution-%s leaked registration\n' \
      "$child_status" >&2
    return 1
  }
  cleanup_substitution_target
  rm -f -- "$root"
  SUBSTITUTED_ROOT=
  finish_case
}

run_substitution_fixture_guard_case() {
  local scenario=$1 supplied_root victim output status target
  CASE_DIR=$(mktemp -d /tmp/batuta-contract-registration.XXXXXX)
  STATE_DIR=$(mktemp -d /tmp/batuta-contract-registration-state.XXXXXX)
  victim=$STATE_DIR/victim
  mkdir "$victim"
  printf 'preserve fixture victim\n' > "$victim/sentinel"
  case $scenario in
    unguarded)
      supplied_root=$victim
      ;;
    traversal)
      ADVERSARIAL_GUARD_ROOT=$(mktemp -d \
        /tmp/batuta-contract-workspace.XXXXXX)
      supplied_root=$ADVERSARIAL_GUARD_ROOT/../../${victim#/}
      ;;
    symlink)
      ALIAS_PATH=$(mktemp -d /tmp/batuta-contract-workspace.XXXXXX)
      rmdir "$ALIAS_PATH"
      ln -s "$victim" "$ALIAS_PATH"
      supplied_root=$ALIAS_PATH
      ;;
    *)
      printf 'unknown substitution fixture guard scenario: %s\n' \
        "$scenario" >&2
      return 64
      ;;
  esac
  output=$(mktemp)
  set +e
  FAKE_COMPOZY_STATE=$STATE_DIR \
    BATUTA_TEST_WORKSPACE_ROOT=$supplied_root \
    "$SUBSTITUTE_ROOT_FIXTURE" >"$output" 2>&1
  status=$?
  set -e
  cat "$output"
  rm -f -- "$output"
  if [[ -f $STATE_DIR/substitution-target ]]; then
    IFS= read -r target < "$STATE_DIR/substitution-target"
    capture_substitution_target "$target"
  fi
  [[ $status -ne 0 ]] || {
    printf '%s substitution fixture accepted unsafe root\n' "$scenario" >&2
    return 1
  }
  if [[ ! -f $victim/sentinel ]] ||
    ! grep -Fx 'preserve fixture victim' "$victim/sentinel" >/dev/null; then
    printf '%s substitution fixture deleted victim evidence\n' \
      "$scenario" >&2
    return 1
  fi
  if [[ $scenario == symlink ]] && \
    { [[ ! -L $ALIAS_PATH ]] || [[ $(readlink "$ALIAS_PATH") != "$victim" ]]; }; then
    printf 'symlink substitution fixture replaced the supplied alias\n' >&2
    return 1
  fi
  if [[ -n $ALIAS_PATH ]]; then
    rm -f -- "$ALIAS_PATH"
    ALIAS_PATH=
  fi
  if [[ -n $ADVERSARIAL_GUARD_ROOT ]]; then
    rmdir "$ADVERSARIAL_GUARD_ROOT"
    ADVERSARIAL_GUARD_ROOT=
  fi
  if [[ -n $SUBSTITUTION_TARGET ]]; then
    cleanup_substitution_target
  fi
  finish_case
}

run_substitution_cleanup_failure_case() {
  local expected_target expected_canonical expected_device expected_inode
  local cleanup_failed=false evidence_preserved=false identity_preserved=false
  expected_target=$(mktemp -d \
    /tmp/batuta-contract-substitution-target.XXXXXX)
  printf 'preserve failed cleanup evidence\n' > "$expected_target/sentinel"
  capture_substitution_target "$expected_target"
  expected_canonical=$SUBSTITUTION_TARGET_CANONICAL
  expected_device=$SUBSTITUTION_TARGET_DEVICE
  expected_inode=$SUBSTITUTION_TARGET_INODE
  chmod 500 "$expected_target"
  if ! cleanup_substitution_target; then
    cleanup_failed=true
  fi
  if [[ -f $expected_target/sentinel ]]; then
    evidence_preserved=true
  fi
  if [[ $SUBSTITUTION_TARGET == "$expected_target" && \
    $SUBSTITUTION_TARGET_CANONICAL == "$expected_canonical" && \
    $SUBSTITUTION_TARGET_DEVICE == "$expected_device" && \
    $SUBSTITUTION_TARGET_INODE == "$expected_inode" ]]; then
    identity_preserved=true
  fi
  chmod 700 "$expected_target"
  if [[ -z $SUBSTITUTION_TARGET ]]; then
    capture_substitution_target "$expected_target"
  fi
  cleanup_substitution_target
  [[ $cleanup_failed == true ]] || {
    printf 'substitution target deletion failure was swallowed\n' >&2
    return 1
  }
  [[ $evidence_preserved == true && $identity_preserved == true ]] || {
    printf 'failed substitution cleanup cleared ownership evidence\n' >&2
    return 1
  }
}

run_selection_alias_case() {
  local scenario=$1 selected_root registered_root
  CASE_DIR=$(mktemp -d /tmp/batuta-contract-workspace.XXXXXX)
  STATE_DIR=$(mktemp -d /tmp/batuta-contract-registration-state.XXXXXX)
  : > "$STATE_DIR/log"
  case $scenario in
    traversal)
      selected_root=$CASE_DIR/../../etc
      registered_root=/etc
      ;;
    symlink-alias)
      registered_root=$STATE_DIR/actual
      mkdir "$registered_root"
      ALIAS_PATH=$(mktemp -d /tmp/batuta-contract-workspace.alias.XXXXXX)
      rmdir "$ALIAS_PATH"
      ln -s "$registered_root" "$ALIAS_PATH"
      selected_root=$ALIAS_PATH
      ;;
    repo-alias)
      registered_root=$PWD
      selected_root=$CASE_DIR/../../${PWD#/}
      ;;
    *)
      printf 'unknown alias scenario: %s\n' "$scenario" >&2
      return 64
      ;;
  esac
  printf '%s\n%s\n%s\n' "$registered_root" external ws-existing \
    > "$STATE_DIR/registration"
  if FAKE_COMPOZY_STATE=$STATE_DIR \
    BATUTA_TEST_WORKSPACE_ROOT=$selected_root \
    BATUTA_TEST_WORKSPACE=ws-existing \
    require_test_workspace >/dev/null 2>&1; then
    printf '%s external workspace alias was accepted\n' "$scenario" >&2
    return 1
  fi
  if [[ -n $ALIAS_PATH && -L $ALIAS_PATH ]]; then
    rm -f -- "$ALIAS_PATH"
    ALIAS_PATH=
  fi
  finish_case
}

run_workspace_selection_case() {
  local actual expected unguarded resolved
  CASE_DIR=$(mktemp -d /tmp/batuta-contract-workspace.XXXXXX)
  STATE_DIR=$(mktemp -d /tmp/batuta-contract-registration-state.XXXXXX)
  actual=$CASE_DIR
  expected=$CASE_DIR/expected
  unguarded=$STATE_DIR/unguarded
  mkdir "$expected" "$unguarded"
  printf '%s\n%s\n%s\n' "$actual" external ws-existing \
    > "$STATE_DIR/registration"
  : > "$STATE_DIR/log"

  if FAKE_COMPOZY_STATE=$STATE_DIR \
    BATUTA_TEST_WORKSPACE_ROOT=$actual \
    require_test_workspace >/dev/null 2>&1; then
    printf 'external root was accepted without an explicit workspace selector\n' >&2
    return 1
  fi
  if FAKE_COMPOZY_STATE=$STATE_DIR \
    BATUTA_TEST_WORKSPACE=ws-existing \
    require_test_workspace >/dev/null 2>&1; then
    printf 'external workspace was accepted without its expected root\n' >&2
    return 1
  fi
  if FAKE_COMPOZY_STATE=$STATE_DIR \
    BATUTA_TEST_WORKSPACE_ROOT=$expected \
    BATUTA_TEST_WORKSPACE=ws-existing \
    require_test_workspace >/dev/null 2>&1; then
    printf 'external workspace was accepted with a mismatched root\n' >&2
    return 1
  fi
  printf '%s\n%s\n%s\n' "$unguarded" external ws-existing \
    > "$STATE_DIR/registration"
  if FAKE_COMPOZY_STATE=$STATE_DIR \
    BATUTA_TEST_WORKSPACE_ROOT=$unguarded \
    BATUTA_TEST_WORKSPACE=ws-existing \
    require_test_workspace >/dev/null 2>&1; then
    printf 'unguarded external workspace was accepted\n' >&2
    return 1
  fi
  printf '%s\n%s\n%s\n' "$actual" external ws-existing \
    > "$STATE_DIR/registration"
  resolved=$(FAKE_COMPOZY_STATE=$STATE_DIR \
    BATUTA_TEST_WORKSPACE_ROOT=$actual \
    BATUTA_TEST_WORKSPACE=ws-existing \
    require_test_workspace)
  [[ $resolved == ws-existing ]] || {
    printf 'explicit external workspace resolved as %s\n' "$resolved" >&2
    return 1
  }
  finish_case
}

case ${1:-all} in
  external) run_external_case ;;
  foreign) run_foreign_marker_case ;;
  child-cleanup) run_child_cleanup_marker_case ;;
  preexisting) run_preexisting_marker_case ;;
  selection) run_workspace_selection_case ;;
  traversal) run_selection_alias_case traversal ;;
  symlink-alias) run_selection_alias_case symlink-alias ;;
  repo-alias) run_selection_alias_case repo-alias ;;
  remove-failure)
    run_remove_failure_case "$EXTERNAL_FIXTURE" success
    run_remove_failure_case "$EXIT_42_FIXTURE" exit-42
    ;;
  root-substitution)
    run_root_substitution_case 0
    run_root_substitution_case 42
    ;;
  fixture-guard)
    run_substitution_fixture_guard_case unguarded
    run_substitution_fixture_guard_case traversal
    run_substitution_fixture_guard_case symlink
    ;;
  target-delete-failure)
    run_substitution_cleanup_failure_case
    ;;
  all)
    run_external_case
    run_external_case malformed-add-output
    run_external_case missing-add-id
    run_foreign_marker_case
    run_child_cleanup_marker_case
    run_preexisting_marker_case
    run_nonzero_status_case
    run_workspace_selection_case
    run_selection_alias_case traversal
    run_selection_alias_case symlink-alias
    run_selection_alias_case repo-alias
    run_remove_failure_case "$EXTERNAL_FIXTURE" success
    run_remove_failure_case "$EXIT_42_FIXTURE" exit-42
    run_root_substitution_case 0
    run_root_substitution_case 42
    run_substitution_fixture_guard_case unguarded
    run_substitution_fixture_guard_case traversal
    run_substitution_fixture_guard_case symlink
    run_substitution_cleanup_failure_case
    printf 'OK: contract runner isolates workspace registration from repository state\n'
    ;;
  *)
    printf 'unknown registration cleanup case: %s\n' "$1" >&2
    exit 64
    ;;
esac
