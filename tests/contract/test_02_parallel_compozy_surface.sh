#!/usr/bin/env bash
# Prova que a fonte e o binário do Compozy expõem o contrato usado pelo Batuta paralelo.
set -euo pipefail

SOURCE_ROOT=${1:-${COMPOZY_SOURCE_ROOT:-}}
COMPOZY_BINARY=${2:-${COMPOZY_BINARY:-compozy}}

fail() {
  printf 'parallel Compozy contract unavailable: %s\n' "$1" >&2
  exit 1
}

if [[ -z $SOURCE_ROOT ]]; then
  fail 'source root argument is required'
fi
if [[ $SOURCE_ROOT != /* || ! -d $SOURCE_ROOT || -L $SOURCE_ROOT ]]; then
  fail 'source root must be an absolute regular directory'
fi
SOURCE_ROOT=$(cd "$SOURCE_ROOT" && pwd -P)
if [[ ! -f $SOURCE_ROOT/go.mod || -L $SOURCE_ROOT/go.mod ]]; then
  fail 'source root does not contain a regular go.mod'
fi
if ! grep -qF 'module github.com/compozy/compozy' "$SOURCE_ROOT/go.mod"; then
  fail 'source root is not the Compozy module'
fi

require_test() {
  local package=$1 test_name=$2 listed
  if ! listed=$(cd "$SOURCE_ROOT" && go test "$package" -list "^${test_name}$" 2>&1); then
    printf '%s\n' "$listed" >&2
    fail "could not compile canonical test ${package}:${test_name}"
  fi
  if ! grep -qxF "$test_name" <<<"$listed"; then
    fail "canonical test missing: ${package}:${test_name}"
  fi
}

run_tests() {
  local package=$1 expression=$2
  if ! (cd "$SOURCE_ROOT" && go test "$package" -run "$expression" -count=1); then
    fail "canonical tests failed: ${package}:${expression}"
  fi
}

require_doc_field() {
  local package=$1 type_name=$2 field_name=$3 json_name=$4 documentation
  if ! documentation=$(cd "$SOURCE_ROOT" && go doc "$package" "$type_name" 2>&1); then
    printf '%s\n' "$documentation" >&2
    fail "public type is unavailable: ${package}.${type_name}"
  fi
  if ! grep -qE "^[[:space:]]*${field_name}[[:space:]].*json:\"${json_name}" <<<"$documentation"; then
    fail "public field is unavailable: ${package}.${type_name}.${field_name} (${json_name})"
  fi
}

# The forwarding subtest is the executable owner of child-scoped overrides.
if ! forwarding_output=$(
  cd "$SOURCE_ROOT"
  go test ./internal/loop \
    -run '^TestReservedActionExecutorsShouldRunAgentLoopAndTransform$/^Should_pass_materialized_config_overrides_to_child_loop$' \
    -count=1 -v
); then
  printf '%s\n' "$forwarding_output" >&2
  fail 'child run-loop params.config_overrides forwarding tests failed'
fi
forwarding_test='TestReservedActionExecutorsShouldRunAgentLoopAndTransform/Should_pass_materialized_config_overrides_to_child_loop'
if ! grep -qF -- "--- PASS: $forwarding_test" <<<"$forwarding_output"; then
  fail 'child run-loop params.config_overrides forwarding is missing'
fi

require_test ./internal/loop TestRunLoopActionShouldMatchPublicConfigOverrideFields
require_test ./internal/loop TestLinterShouldValidateAskControls
require_test ./internal/loop TestCoordinatorRunnerShouldParkAskRequests
require_test ./internal/loop TestCoordinatorRunnerShouldDriveFanOutAndCollectControls
require_test ./internal/api/core TestLoopRequestHandlersShouldPreserveTransportParity
require_test ./internal/daemon TestLoopRequestPayloadShouldExposeOnlyPublicRequestState
require_test ./internal/daemon TestLoopRunListOrderingAndCursorContract
require_test ./internal/cli TestWorktreeListCommand
require_test ./internal/cli TestWorktreeRemovalRefusalOutput
require_test ./internal/cli TestWorktreeMutationNameReferences

run_tests ./internal/loop \
  '^(TestRunLoopActionShouldMatchPublicConfigOverrideFields|TestLinterShouldValidateAskControls|TestCoordinatorRunnerShouldParkAskRequests|TestCoordinatorRunnerShouldDriveFanOutAndCollectControls)$'
run_tests ./internal/api/core '^TestLoopRequestHandlersShouldPreserveTransportParity$'
run_tests ./internal/daemon \
  '^(TestLoopRequestPayloadShouldExposeOnlyPublicRequestState|TestLoopRunListOrderingAndCursorContract)$'
run_tests ./internal/cli \
  '^(TestWorktreeListCommand|TestWorktreeRemovalRefusalOutput|TestWorktreeMutationNameReferences)$'

require_doc_field ./internal/api/contract LoopRunPayload Inputs inputs
require_doc_field ./internal/api/contract LoopGenerationOutput ChildLoopRunID child_loop_run_id
require_doc_field ./internal/api/contract LoopRequestPayload LoopRunID loop_run_id
require_doc_field ./internal/api/contract LoopRequestPayload Generation generation
require_doc_field ./internal/api/contract LoopRequestPayload NodeID node_id
require_doc_field ./internal/api/contract LoopRequestPayload ItemIndex item_index
require_doc_field ./internal/api/contract RespondLoopRequestResponse RunID run_id
require_doc_field ./internal/api/contract RespondLoopRequestResponse NodeID node_id

if [[ $COMPOZY_BINARY == */* ]]; then
  if [[ $COMPOZY_BINARY != /* || ! -f $COMPOZY_BINARY || -L $COMPOZY_BINARY || ! -x $COMPOZY_BINARY ]]; then
    fail 'binary path must name an absolute executable regular file'
  fi
else
  COMPOZY_BINARY=$(command -v -- "$COMPOZY_BINARY") || fail 'Compozy binary was not found'
fi

source_commit=$(git -C "$SOURCE_ROOT" rev-parse HEAD) || fail 'source commit is unreadable'
expected_binary_commit=$(git -C "$SOURCE_ROOT" rev-parse --short HEAD) || \
  fail 'source abbreviation is unreadable'
version_json=$(
  cd /
  "$COMPOZY_BINARY" version -o json
) || fail 'Compozy binary identity is unreadable'
actual_binary_commit=$(python3 - "$version_json" <<'PY'
import json
import sys

try:
    payload = json.loads(sys.argv[1])
    commit = payload["Commit"]
except (json.JSONDecodeError, KeyError, TypeError) as error:
    raise SystemExit(f"invalid Compozy version payload: {error}") from None
if not isinstance(commit, str) or not commit:
    raise SystemExit("invalid Compozy version commit")
print(commit)
PY
) || fail 'Compozy binary identity payload is invalid'
if [[ $actual_binary_commit != "$expected_binary_commit" ]]; then
  fail "binary commit ${actual_binary_commit} does not equal source ${expected_binary_commit} (${source_commit})"
fi
for worktree_command in create inspect status remove; do
  if ! "$COMPOZY_BINARY" worktree "$worktree_command" --help >/dev/null; then
    fail "managed worktree CLI command is unavailable: ${worktree_command}"
  fi
done

printf 'OK: parallel Compozy contract is executable at %s\n' "$source_commit"
