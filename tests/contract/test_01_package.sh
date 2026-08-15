#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

PACKAGE_ROOT=$(mktemp -d /tmp/batuta-package-test.XXXXXX)
cleanup() {
  case "$PACKAGE_ROOT" in
    /tmp/batuta-package-test.*)
      chmod -R u+w "$PACKAGE_ROOT"
      rm -rf -- "$PACKAGE_ROOT"
      ;;
  esac
}
trap cleanup EXIT

first=$(BATUTA_PACKAGE_ROOT="$PACKAGE_ROOT" scripts/package-extension.sh)
second=$(BATUTA_PACKAGE_ROOT="$PACKAGE_ROOT" scripts/package-extension.sh)

if [[ $first != "$second" || ! -d $first ]]; then
  printf 'content-addressed package was not retained/reused: %s / %s\n' \
    "$first" "$second" >&2
  exit 1
fi

digest=${first##*/}
if [[ ${first%/*} != "$PACKAGE_ROOT" || ! $digest =~ ^[0-9a-f]{64}$ ]]; then
  printf 'package path is not rooted by digest: %s\n' "$first" >&2
  exit 1
fi

actual=$(cd "$first" && find . -type f -print | LC_ALL=C sort)
expected=$(printf '%s\n' \
  './LICENSE' \
  './agents/batuta/AGENT.md' \
  './extension.toml' \
  './loops/batuta-deliver/loop.yaml' \
  './resources/skills/batuta-routing/SKILL.md')
if [[ $actual != "$expected" ]]; then
  printf 'retained package tree mismatch:\n%s\n' "$actual" >&2
  exit 1
fi

python3 - "$first" <<'PY'
import os
from pathlib import Path
import stat
import sys

root = Path(sys.argv[1])
for path in [root, *root.rglob("*")]:
    expected = 0o755 if path.is_dir() else 0o444
    actual = stat.S_IMODE(os.stat(path).st_mode)
    assert actual == expected, f"unexpected package mode {actual:o}: {path}"
PY

leftover=$(find "$PACKAGE_ROOT" -mindepth 1 -maxdepth 1 -type d \
  -name '.staging.*' -print -quit)
if [[ -n $leftover ]]; then
  printf 'package staging directory leaked\n' >&2
  exit 1
fi

printf 'OK: retained package is minimal, content-addressed, read-only-file, and reusable\n'
