#!/usr/bin/env bash

workspace_marker_present() {
  local marker=$1/.compozy
  [[ -e $marker || -L $marker ]]
}

preflight_contract_workspace() {
  local repo_root=$1
  if workspace_marker_present "$repo_root"; then
    printf 'contract suite requires a clean repository without %s/.compozy\n' \
      "$repo_root" >&2
    return 1
  fi
}

reject_new_repository_marker() {
  local repo_root=$1 marker_preexisted=$2
  if [[ $marker_preexisted == false ]] && \
    workspace_marker_present "$repo_root"; then
    printf 'contract generated foreign repository state: %s/.compozy\n' \
      "$repo_root" >&2
    return 1
  fi
}

workspace_id_for_canonical_root() {
  local repo_root=$1 expected_name=${2:-} workspaces_json resolved
  workspaces_json=$(mktemp)

  if ! compozy workspace list -o json > "$workspaces_json"; then
    rm -f -- "$workspaces_json"
    return 1
  fi

  if ! resolved=$(python3 - "$repo_root" "$expected_name" "$workspaces_json" <<'PY'
import json
import os
import sys

repo_root = os.path.realpath(sys.argv[1])
expected_name = sys.argv[2]
rows = json.load(open(sys.argv[3]))
matches = [
    row["id"]
    for row in rows
    if isinstance(row.get("id"), str)
    and os.path.realpath(row["root_dir"]) == repo_root
    and (not expected_name or row.get("name") == expected_name)
]
if len(matches) > 1:
    raise SystemExit(
        f"ambiguous workspace registrations for {repo_root}"
    )
if matches:
    print(matches[0])
PY
  ); then
    rm -f -- "$workspaces_json"
    return 1
  fi

  rm -f -- "$workspaces_json"
  printf '%s\n' "$resolved"
}

require_test_workspace() {
  local repo_root expected_input expected_root candidate workspaces_json resolved
  repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
  expected_input=${BATUTA_TEST_WORKSPACE_ROOT:-$repo_root}
  candidate=${BATUTA_TEST_WORKSPACE:-$repo_root}
  if [[ -n ${BATUTA_TEST_WORKSPACE_ROOT:-} ]]; then
    if [[ -z ${BATUTA_TEST_WORKSPACE:-} ]]; then
      printf 'external contract workspace requires explicit root and selector\n' >&2
      return 1
    fi
    if [[ -L $expected_input || ! -d $expected_input ]]; then
      printf 'external contract workspace must be a real directory: %s\n' \
        "$expected_input" >&2
      return 1
    fi
    expected_root=$(cd "$expected_input" && pwd -P)
    if [[ $expected_root == "$repo_root" ]]; then
      printf 'refusing repository alias as external contract workspace: %s\n' \
        "$expected_input" >&2
      return 1
    fi
    case $expected_root in
      /tmp/batuta-contract-workspace.*) ;;
      *)
        printf 'refusing unguarded external contract workspace: %s\n' \
          "$expected_root" >&2
        return 1
        ;;
    esac
  else
    expected_root=$repo_root
  fi
  workspaces_json=$(mktemp)

  if ! compozy workspace list -o json > "$workspaces_json"; then
    rm -f -- "$workspaces_json"
    return 1
  fi

  if ! resolved=$(python3 - "$expected_root" "$candidate" "$workspaces_json" <<'PY'
import json
import os
import sys

expected_root = os.path.realpath(sys.argv[1])
candidate = sys.argv[2]
rows = json.load(open(sys.argv[3]))

for row in rows:
    root = os.path.realpath(row["root_dir"])
    if candidate in (row["id"], row.get("name"), row["root_dir"], root):
        if root != expected_root:
            raise SystemExit(
                f"BATUTA_TEST_WORKSPACE resolves to {root}, expected {expected_root}"
            )
        print(row["id"])
        break
else:
    raise SystemExit(
        f"workspace not registered: {expected_root}; "
        f"run `compozy workspace add {expected_root}`"
    )
PY
  ); then
    rm -f -- "$workspaces_json"
    return 1
  fi

  rm -f -- "$workspaces_json"
  printf '%s\n' "$resolved"
}
