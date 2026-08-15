#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

BUILDER=scripts/build-preview-assets.sh
if [[ ! -d .compozy || -L .compozy ]]; then
  printf 'expected the pre-existing .compozy marker directory\n' >&2
  exit 1
fi

PREVIEW_ROOT=$(mktemp -d /tmp/batuta-preview-assets.XXXXXX)
cleanup() {
  local root=$PREVIEW_ROOT
  PREVIEW_ROOT=
  if [[ -z $root ]]; then
    return 0
  fi
  if [[ $root != /tmp/batuta-preview-assets.* || ${root%/*} != /tmp || -L $root || ! -d $root ]]; then
    printf 'refusing to clean unexpected preview asset path: %s\n' "$root" >&2
    return 1
  fi
  chmod -R u+w -- "$root"
  rm -rf -- "$root"
}
trap cleanup EXIT

marker_snapshot="$PREVIEW_ROOT/compozy-marker.tar"
tar --sort=name --mtime='@0' --owner=0 --group=0 --numeric-owner \
  -cf "$marker_snapshot" -C . .compozy

version=$(python3 - extension.toml <<'PY'
import sys
import tomllib

with open(sys.argv[1], "rb") as manifest_file:
    print(tomllib.load(manifest_file)["extension"]["version"])
PY
)

first_output="$PREVIEW_ROOT/first"
second_output="$PREVIEW_ROOT/second"
mkdir "$first_output" "$second_output"

epoch=1700000000
first_archive=$(SOURCE_DATE_EPOCH=$epoch "$BUILDER" "$version" "$first_output")
second_archive=$(SOURCE_DATE_EPOCH=$epoch "$BUILDER" "$version" "$second_output")

[[ $first_archive == "$first_output/batuta-compozy_${version}.tar.gz" ]]
[[ $second_archive == "$second_output/batuta-compozy_${version}.tar.gz" ]]
first_sha=$(sha256sum "$first_archive" | cut -d' ' -f1)
second_sha=$(sha256sum "$second_archive" | cut -d' ' -f1)
[[ $first_sha == "$second_sha" ]]
(cd "$first_output" && sha256sum --check SHA256SUMS)

extracted="$PREVIEW_ROOT/extracted"
mkdir "$extracted"
tar -xzf "$first_archive" -C "$extracted"
actual=$(cd "$extracted" && find . -type f -print | LC_ALL=C sort)
expected=$(printf '%s\n' \
  './agents/batuta/AGENT.md' \
  './extension.toml' \
  './loops/batuta-deliver/loop.yaml' \
  './resources/skills/batuta-routing/SKILL.md')
if [[ $actual != "$expected" ]]; then
  printf 'preview archive tree mismatch:\n%s\n' "$actual" >&2
  exit 1
fi

for archive in "$first_archive" "$second_archive"; do
  if tar -tzf "$archive" | grep -qF '.compozy'; then
    printf 'preview archive unexpectedly contains .compozy: %s\n' "$archive" >&2
    exit 1
  fi
done

expect_failure() {
  if "$@" >/dev/null 2>&1; then
    printf 'expected command to fail: %q\n' "$*" >&2
    exit 1
  fi
}

mismatch_output="$PREVIEW_ROOT/mismatch"
relative_output="$PREVIEW_ROOT/relative"
nonempty_output="$PREVIEW_ROOT/nonempty"
symlink_output="$PREVIEW_ROOT/symlink"
symlink_target="$PREVIEW_ROOT/symlink-target"
mkdir "$mismatch_output" "$nonempty_output" "$symlink_target"
touch "$nonempty_output/occupied"
ln -s "$symlink_target" "$symlink_output"

expect_failure "$BUILDER" "${version}-mismatch" "$mismatch_output"
expect_failure "$BUILDER" "$version" "relative-preview-output"
expect_failure "$BUILDER" "$version" "$nonempty_output"
expect_failure "$BUILDER" "$version" "$symlink_output"

marker_after="$PREVIEW_ROOT/compozy-marker-after.tar"
tar --sort=name --mtime='@0' --owner=0 --group=0 --numeric-owner \
  -cf "$marker_after" -C . .compozy
cmp -s "$marker_snapshot" "$marker_after"

printf 'OK: deterministic preview assets validate, reject unsafe outputs, and exclude .compozy\n'
