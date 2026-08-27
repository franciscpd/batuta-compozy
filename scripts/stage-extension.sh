#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  printf 'usage: %s EMPTY_STAGING_DIRECTORY\n' "$0" >&2
  exit 2
fi

ROOT=$(cd "$(dirname "$0")/.." && pwd -P)
if [[ ! -d $1 || -L $1 ]]; then
  printf 'staging destination must be an existing directory, not a symlink: %s\n' "$1" >&2
  exit 2
fi
STAGE=$(cd "$1" && pwd -P)

if [[ -n $(find "$STAGE" -mindepth 1 -print -quit) ]]; then
  printf 'staging destination must be empty: %s\n' "$STAGE" >&2
  exit 2
fi

mkdir -p "$STAGE/agents" "$STAGE/resources/skills" "$STAGE/loops" "$STAGE/internal"
cp -- "$ROOT/LICENSE" "$STAGE/LICENSE"
cp -- "$ROOT/go.mod" "$ROOT/go.sum" "$ROOT/main.go" "$STAGE/"
cp -R -- "$ROOT/agents/batuta" "$STAGE/agents/"
cp -R -- "$ROOT/resources/skills/batuta-routing" "$STAGE/resources/skills/"
cp -R -- "$ROOT/loops/batuta-deliver" "$STAGE/loops/"
cp -R -- "$ROOT/loops/batuta-task" "$STAGE/loops/"

while IFS= read -r source; do
  relative=${source#"$ROOT/"}
  mkdir -p "$STAGE/$(dirname "$relative")"
  cp -- "$source" "$STAGE/$relative"
done < <(find "$ROOT/internal" -type f -name '*.go' ! -name '*_test.go' -print)
