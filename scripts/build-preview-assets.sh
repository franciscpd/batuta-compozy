#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  printf 'usage: %s VERSION ABSOLUTE_EMPTY_OUTPUT_DIRECTORY\n' "$0" >&2
  exit 2
fi

version=$1
output=$2
semver_re='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-beta\.(0|[1-9][0-9]*)$'
if [[ ! $version =~ $semver_re ]]; then
  printf 'version must be strict beta SemVer: %s\n' "$version" >&2
  exit 2
fi

if [[ $output != /* || ! -d $output || -L $output ]]; then
  printf 'output directory must be an existing absolute directory, not a symlink: %s\n' "$output" >&2
  exit 2
fi
if [[ -n $(find "$output" -mindepth 1 -print -quit) ]]; then
  printf 'output directory must be empty: %s\n' "$output" >&2
  exit 2
fi
output=$(cd "$output" && pwd -P)

ROOT=$(cd "$(dirname "$0")/.." && pwd -P)
manifest_version=$(python3 - "$ROOT/extension.toml" <<'PY'
import sys
import tomllib

with open(sys.argv[1], "rb") as manifest_file:
    print(tomllib.load(manifest_file)["extension"]["version"])
PY
)
if [[ $version != "$manifest_version" ]]; then
  printf 'version does not match extension.toml: %s != %s\n' "$version" "$manifest_version" >&2
  exit 2
fi

package_root=$(mktemp -d /tmp/batuta-preview-package.XXXXXX)
cleanup() {
  local root=$package_root
  package_root=
  if [[ -z $root ]]; then
    return 0
  fi
  if [[ $root != /tmp/batuta-preview-package.* || ${root%/*} != /tmp || -L $root || ! -d $root ]]; then
    printf 'refusing to clean unexpected preview package path: %s\n' "$root" >&2
    return 1
  fi
  chmod -R u+w -- "$root"
  rm -rf -- "$root"
}
trap cleanup EXIT

first_package=$(BATUTA_PACKAGE_ROOT="$package_root" "$ROOT/scripts/package-extension.sh")
second_package=$(BATUTA_PACKAGE_ROOT="$package_root" "$ROOT/scripts/package-extension.sh")
if [[ $first_package != "$second_package" || ! -d $first_package || -L $first_package ]]; then
  printf 'content-addressed package changed between builds: %s / %s\n' \
    "$first_package" "$second_package" >&2
  exit 1
fi

archive="$output/batuta-compozy_${version}.tar.gz"
epoch=${SOURCE_DATE_EPOCH:-$(git -C "$ROOT" show -s --format=%ct HEAD)}
tar --sort=name --mtime="@$epoch" --owner=0 --group=0 --numeric-owner \
  -cf - -C "$first_package" . | gzip -n -9 > "$archive"
(cd "$output" && sha256sum "${archive##*/}" > SHA256SUMS)
printf '%s\n' "$archive"
