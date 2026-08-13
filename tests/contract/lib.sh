#!/usr/bin/env bash

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
