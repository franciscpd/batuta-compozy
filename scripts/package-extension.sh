#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd -P)
lock_fd=${BATUTA_PACKAGE_LOCK_FD:-}
lock_path=${BATUTA_PACKAGE_LOCK_PATH:-}
if ! python3 "$ROOT/scripts/with-package-lock.py" \
  --check "$lock_fd" "$lock_path"; then
  exec python3 "$ROOT/scripts/with-package-lock.py" \
    "$ROOT/scripts/package-extension.sh" "$@"
fi

if [[ -n ${BATUTA_PACKAGE_ROOT:-} ]]; then
  requested_root=$BATUTA_PACKAGE_ROOT
elif [[ -n ${XDG_DATA_HOME:-} ]]; then
  requested_root=$XDG_DATA_HOME/batuta-compozy/packages
else
  requested_root=$(python3 -c '
from pathlib import Path
print(Path.home() / ".local" / "share" / "batuta-compozy" / "packages")
')
fi

if [[ $requested_root != /* ]]; then
  printf 'package root must be absolute: %s\n' "$requested_root" >&2
  exit 2
fi
mkdir -p "$requested_root"
if [[ -L $requested_root || ! -d $requested_root ]]; then
  printf 'package root must be a directory, not a symlink: %s\n' "$requested_root" >&2
  exit 2
fi
PACKAGE_ROOT=$(cd "$requested_root" && pwd -P)
if [[ $lock_path != "$PACKAGE_ROOT/.publication.lock" ]]; then
  printf 'package lock does not protect package root: %s\n' "$PACKAGE_ROOT" >&2
  exit 2
fi

PACKAGE_TEMP=$(mktemp -d "$PACKAGE_ROOT/.staging.XXXXXX")
cleanup() {
  if [[ -n $PACKAGE_TEMP ]]; then
    case "$PACKAGE_TEMP" in
      "$PACKAGE_ROOT"/.staging.*)
        chmod -R u+w -- "$PACKAGE_TEMP"
        rm -rf -- "$PACKAGE_TEMP"
        ;;
      *)
        printf 'refusing to clean unexpected package staging path: %s\n' "$PACKAGE_TEMP" >&2
        return 1
        ;;
    esac
  fi
}
trap cleanup EXIT

"$ROOT/scripts/stage-extension.sh" "$PACKAGE_TEMP"
digest=$(python3 - "$PACKAGE_TEMP" <<'PY'
import hashlib
from pathlib import Path
import sys

root = Path(sys.argv[1])
files = sorted(path for path in root.rglob("*") if path.is_file())
digest = hashlib.sha256()
for path in files:
    relative = path.relative_to(root).as_posix().encode()
    content = path.read_bytes()
    digest.update(len(relative).to_bytes(4, "big"))
    digest.update(relative)
    digest.update(len(content).to_bytes(8, "big"))
    digest.update(content)
print(digest.hexdigest())
PY
)
PACKAGE_DIR=$PACKAGE_ROOT/$digest

verify_exact() {
  python3 - "$PACKAGE_TEMP" "$PACKAGE_DIR" <<'PY'
from pathlib import Path
import stat
import sys

expected_root = Path(sys.argv[1])
actual_root = Path(sys.argv[2])


def snapshot(root):
    entries = {}
    for path in root.rglob("*"):
        relative = path.relative_to(root).as_posix()
        if path.is_symlink():
            raise SystemExit(f"package contains symlink: {path}")
        entries[relative] = (
            ("dir", None) if path.is_dir() else ("file", path.read_bytes())
        )
    return entries


expected = snapshot(expected_root)
actual = snapshot(actual_root)
if actual != expected:
    raise SystemExit(f"existing content-addressed package is not exact: {actual_root}")
for path in [actual_root, *actual_root.rglob("*")]:
    expected_mode = 0o755 if path.is_dir() else 0o444
    actual_mode = stat.S_IMODE(path.stat().st_mode)
    if actual_mode != expected_mode:
        raise SystemExit(
            f"existing content-addressed package has mode {actual_mode:o}, "
            f"expected {expected_mode:o}: {path}"
        )
PY
}

python3 - "$PACKAGE_TEMP" <<'PY'
import os
from pathlib import Path
import sys

root = Path(sys.argv[1])
for path in root.rglob("*"):
    os.chmod(path, 0o755 if path.is_dir() else 0o444)
os.chmod(root, 0o755)
PY

if [[ -e $PACKAGE_DIR ]]; then
  if [[ -L $PACKAGE_DIR || ! -d $PACKAGE_DIR ]]; then
    printf 'package digest path is not a directory: %s\n' "$PACKAGE_DIR" >&2
    exit 1
  fi
  verify_exact
else
  rename_result=$(python3 - "$PACKAGE_TEMP" "$PACKAGE_DIR" <<'PY'
import errno
import os
import sys

source, destination = sys.argv[1:]
try:
    os.rename(source, destination)
except OSError as error:
    if error.errno not in {errno.EEXIST, errno.ENOTEMPTY}:
        raise
    print("exists")
else:
    print("created")
PY
  )
  if [[ $rename_result == created ]]; then
    PACKAGE_TEMP=""
  else
    if [[ -L $PACKAGE_DIR || ! -d $PACKAGE_DIR ]]; then
      printf 'raced package digest path is not a directory: %s\n' "$PACKAGE_DIR" >&2
      exit 1
    fi
    verify_exact
  fi
fi

printf '%s\n' "$PACKAGE_DIR"
