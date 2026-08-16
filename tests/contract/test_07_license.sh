#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

MARKER_PRESENT=false
if [[ -e .compozy || -L .compozy ]]; then
  if [[ ! -d .compozy || -L .compozy ]]; then
    printf 'expected .compozy to be an absent entry or a real directory\n' >&2
    exit 1
  fi
  MARKER_PRESENT=true
fi

TEST_ROOT=$(mktemp -d /tmp/batuta-license-contract.XXXXXX)
cleanup() {
  local root=$TEST_ROOT
  TEST_ROOT=
  if [[ -z $root ]]; then
    return 0
  fi
  if [[ $root != /tmp/batuta-license-contract.* || ${root%/*} != /tmp || -L $root || ! -d $root ]]; then
    printf 'refusing to clean unexpected license contract path: %s\n' "$root" >&2
    return 1
  fi
  chmod -R u+w -- "$root"
  rm -rf -- "$root"
}
trap cleanup EXIT

snapshot_marker() {
  local marker_root=$1
  local snapshot=$2
  python3 - "$marker_root" "$snapshot" <<'PY'
import hashlib
import json
import os
from pathlib import Path
import stat
import sys

root = Path(sys.argv[1])
snapshot = Path(sys.argv[2])
noatime = getattr(os, "O_NOATIME", None)
directory = getattr(os, "O_DIRECTORY", None)
if noatime is None or directory is None:
    raise SystemExit("refusing to read marker without Linux O_NOATIME support")


def open_noatime(path, *, is_directory=False):
    flags = os.O_RDONLY | noatime
    if is_directory:
        flags |= directory
    try:
        return os.open(path, flags)
    except OSError as error:
        raise SystemExit(f"refusing marker read without O_NOATIME: {path}: {error}")


def sorted_paths(path):
    paths = [path]
    fd = open_noatime(path, is_directory=True)
    try:
        with os.scandir(fd) as entries:
            children = sorted(entries, key=lambda entry: entry.name)
    finally:
        os.close(fd)
    for entry in children:
        child = path / entry.name
        paths.extend(sorted_paths(child) if entry.is_dir(follow_symlinks=False) else [child])
    return paths


def sha256_noatime(path):
    digest = hashlib.sha256()
    fd = open_noatime(path)
    try:
        while chunk := os.read(fd, 1024 * 1024):
            digest.update(chunk)
    finally:
        os.close(fd)
    return digest.hexdigest()


paths = sorted_paths(root)

with snapshot.open("w", encoding="utf-8") as output:
    for path in paths:
        metadata = path.lstat()
        relative = "." if path == root else path.relative_to(root).as_posix()
        if stat.S_ISREG(metadata.st_mode):
            digest = sha256_noatime(path)
            kind = "file"
        elif stat.S_ISDIR(metadata.st_mode):
            digest = None
            kind = "directory"
        elif stat.S_ISLNK(metadata.st_mode):
            digest = os.readlink(path)
            kind = "symlink"
        else:
            raise SystemExit(f"unsupported marker entry: {path}")
        record = {
            "digest_or_target": digest,
            "gid": metadata.st_gid,
            "kind": kind,
            "mode": stat.S_IMODE(metadata.st_mode),
            "atime_ns": metadata.st_atime_ns,
            "mtime_ns": metadata.st_mtime_ns,
            "path": relative,
            "size": metadata.st_size,
            "uid": metadata.st_uid,
        }
        output.write(json.dumps(record, sort_keys=True, separators=(",", ":")))
        output.write("\n")
PY
}

expected_license="$TEST_ROOT/expected-LICENSE"
cat > "$expected_license" <<'EOF'
MIT License

Copyright (c) 2026 Francisross Soares de Oliveira

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
EOF

if [[ $MARKER_PRESENT == true ]]; then
  marker_snapshot="$TEST_ROOT/compozy-marker.manifest"
  snapshot_marker .compozy "$marker_snapshot"
fi

if [[ -L LICENSE || ! -f LICENSE ]]; then
  printf 'LICENSE must be a regular file, not a symlink\n' >&2
  exit 1
fi
if ! cmp -s -- "$expected_license" LICENSE; then
  printf 'LICENSE does not match the expected MIT text\n' >&2
  exit 1
fi

python3 - extension.toml <<'PY'
import sys
import tomllib

with open(sys.argv[1], "rb") as manifest_file:
    manifest = tomllib.load(manifest_file)


def contains_license(value):
    if isinstance(value, dict):
        return "license" in value or any(contains_license(item) for item in value.values())
    if isinstance(value, list):
        return any(contains_license(item) for item in value)
    return False


if contains_license(manifest):
    raise SystemExit("extension.toml must not contain a license key")
PY

assert_inventory() {
  local tree=$1 label=$2 actual expected
  actual=$(cd "$tree" && find . -type f -print | LC_ALL=C sort)
  expected=$(printf '%s\n' \
    './LICENSE' \
    './agents/batuta/AGENT.md' \
    './extension.toml' \
    './loops/batuta-deliver/loop.yaml' \
    './resources/skills/batuta-routing/SKILL.md')
  if [[ $actual != "$expected" ]]; then
    printf '%s package tree mismatch:\n%s\n' "$label" "$actual" >&2
    exit 1
  fi
}

stage="$TEST_ROOT/stage"
mkdir "$stage"
scripts/stage-extension.sh "$stage"
if ! cmp -s -- LICENSE "$stage/LICENSE"; then
  printf 'staged LICENSE differs from the repository LICENSE\n' >&2
  exit 1
fi
assert_inventory "$stage" staged

package_root="$TEST_ROOT/packages"
mkdir "$package_root"
package_dir=$(BATUTA_PACKAGE_ROOT="$package_root" scripts/package-extension.sh)
if ! cmp -s -- LICENSE "$package_dir/LICENSE"; then
  printf 'content-addressed package LICENSE differs from the repository LICENSE\n' >&2
  exit 1
fi
assert_inventory "$package_dir" content-addressed

version=$(python3 - extension.toml <<'PY'
import sys
import tomllib

with open(sys.argv[1], "rb") as manifest_file:
    print(tomllib.load(manifest_file)["extension"]["version"])
PY
)
preview_output="$TEST_ROOT/preview"
extracted="$TEST_ROOT/extracted"
mkdir "$preview_output" "$extracted"
archive=$(scripts/build-preview-assets.sh "$version" "$preview_output")
tar -xzf "$archive" -C "$extracted"
if ! cmp -s -- LICENSE "$extracted/LICENSE"; then
  printf 'preview archive LICENSE differs from the repository LICENSE\n' >&2
  exit 1
fi
assert_inventory "$extracted" preview

if [[ $MARKER_PRESENT == true ]]; then
  marker_after="$TEST_ROOT/compozy-marker-after.manifest"
  snapshot_marker .compozy "$marker_after"
  if ! cmp -s -- "$marker_snapshot" "$marker_after"; then
    printf '.compozy changed during license contract\n' >&2
    exit 1
  fi
else
  [[ ! -e .compozy && ! -L .compozy ]]
fi

printf 'OK: MIT license is exact and preserved in every package artifact\n'
