#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

BUILDER=scripts/build-preview-assets.sh
MARKER_PRESENT=false
if [[ -e .compozy || -L .compozy ]]; then
  if [[ ! -d .compozy || -L .compozy ]]; then
    printf 'expected .compozy to be an absent entry or a real directory\n' >&2
    exit 1
  fi
  MARKER_PRESENT=true
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

if [[ $MARKER_PRESENT == true ]]; then
  marker_snapshot="$PREVIEW_ROOT/compozy-marker.manifest"
  snapshot_marker .compozy "$marker_snapshot"
fi

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
  './LICENSE' \
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

if [[ $MARKER_PRESENT == true ]]; then
  marker_after="$PREVIEW_ROOT/compozy-marker-after.manifest"
  snapshot_marker .compozy "$marker_after"
  cmp -s "$marker_snapshot" "$marker_after"
else
  [[ ! -e .compozy && ! -L .compozy ]]
fi

tampered_marker="$PREVIEW_ROOT/tampered-compozy"
mkdir "$tampered_marker"
printf 'synthetic marker\n' > "$tampered_marker/workspace.toml"
synthetic_marker_before="$PREVIEW_ROOT/tampered-compozy-before.manifest"
snapshot_marker "$tampered_marker" "$synthetic_marker_before"
python3 - "$tampered_marker/workspace.toml" <<'PY'
import os
import sys

path = sys.argv[1]
metadata = os.stat(path)
os.utime(path, ns=(metadata.st_atime_ns, metadata.st_mtime_ns + 1))
PY
tampered_snapshot="$PREVIEW_ROOT/tampered-compozy.manifest"
snapshot_marker "$tampered_marker" "$tampered_snapshot"
if cmp -s "$synthetic_marker_before" "$tampered_snapshot"; then
  printf 'marker snapshot failed to detect a timestamp-only change\n' >&2
  exit 1
fi

printf 'OK: deterministic preview assets validate, reject unsafe outputs, and exclude .compozy\n'
