#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
source tests/contract/lib.sh

TMP=$(mktemp -d /tmp/batuta-workspace-clean-test.XXXXXX)
REPO_ROOT=$PWD
MARKER=$REPO_ROOT/.compozy
TARGET=$TMP/missing-workspace
cleanup() {
  local original_status=$?
  trap - EXIT
  if [[ -L $MARKER && $(readlink "$MARKER") == "$TARGET" ]]; then
    rm -f -- "$MARKER"
  elif [[ -e $MARKER || -L $MARKER ]]; then
    printf 'refusing to remove unexpected workspace fixture: %s\n' \
      "$MARKER" >&2
    exit 1
  fi
  case "$TMP" in
    /tmp/batuta-workspace-clean-test.*)
      rm -f -- "$TMP/out"
      rmdir "$TMP"
      ;;
  esac
  exit "$original_status"
}
trap cleanup EXIT

if [[ -e $MARKER || -L $MARKER ]]; then
  printf 'workspace cleanliness fixture requires a clean repository\n' >&2
  exit 1
fi
ln -s "$TARGET" "$MARKER"

if bash tests/contract/run.sh > "$TMP/out" 2>&1; then
  printf 'contract preflight accepted a dangling .compozy symlink\n' >&2
  exit 1
fi
reported=false
while IFS= read -r diagnostic; do
  if [[ $diagnostic == *"requires a clean repository"* ]]; then
    reported=true
  fi
done < "$TMP/out"
if [[ $reported == false ]]; then
  printf 'contract preflight did not report repository state\n' >&2
  exit 1
fi
if [[ ! -L $MARKER ]]; then
  printf 'contract preflight removed the dangling .compozy symlink\n' >&2
  exit 1
fi

rm -f -- "$MARKER"

printf 'OK: dangling workspace symlink is rejected and preserved\n'
