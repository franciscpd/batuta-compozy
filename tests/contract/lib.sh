#!/usr/bin/env bash

cleanup_generated_workspace_marker() {
  local repo_root=$1 marker=$1/.compozy unexpected
  if [[ ! -e $marker ]]; then
    return 0
  fi
  if [[ ! -d $marker || -L $marker || ! -f $marker/workspace.toml || \
    -L $marker/workspace.toml ]]; then
    printf 'refusing to clean unexpected workspace marker: %s\n' "$marker" >&2
    return 1
  fi
  unexpected=$(find "$marker" -mindepth 1 \
    ! -path "$marker/workspace.toml" -print -quit)
  if [[ -n $unexpected ]]; then
    printf 'refusing to clean non-minimal workspace marker: %s\n' \
      "$unexpected" >&2
    return 1
  fi
  rm -f -- "$marker/workspace.toml"
  rmdir "$marker"
}

require_test_workspace() {
  local repo_root candidate workspaces_json resolved
  repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
  candidate=${BATUTA_TEST_WORKSPACE:-$repo_root}
  workspaces_json=$(mktemp)

  if ! compozy workspace list -o json > "$workspaces_json"; then
    rm -f -- "$workspaces_json"
    return 1
  fi

  if ! resolved=$(python3 - "$repo_root" "$candidate" "$workspaces_json" <<'PY'
import json
import os
import sys

repo_root = os.path.realpath(sys.argv[1])
candidate = sys.argv[2]
rows = json.load(open(sys.argv[3]))

for row in rows:
    root = os.path.realpath(row["root_dir"])
    if candidate in (row["id"], row.get("name"), row["root_dir"], root):
        if root != repo_root:
            raise SystemExit(
                f"BATUTA_TEST_WORKSPACE resolves to {root}, expected {repo_root}"
            )
        print(row["id"])
        break
else:
    raise SystemExit(
        f"workspace not registered: {repo_root}; run `compozy workspace add {repo_root}`"
    )
PY
  ); then
    rm -f -- "$workspaces_json"
    return 1
  fi

  rm -f -- "$workspaces_json"
  printf '%s\n' "$resolved"
}
