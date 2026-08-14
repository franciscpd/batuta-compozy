#!/usr/bin/env bash
set -euo pipefail

marker=../../.compozy
mkdir -p "$marker"
: > "$marker/workspace.toml"
if [[ ${FIXTURE_MARKER_CLEANUP_FAILURE:-} == 1 ]]; then
  : > "$marker/unexpected"
fi
exit 42
