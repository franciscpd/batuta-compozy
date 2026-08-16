#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "$0")/../.." && pwd -P)
supplied_root=$BATUTA_TEST_WORKSPACE_ROOT
if [[ -L $supplied_root || ! -d $supplied_root ]]; then
  printf 'refusing non-directory substitution root: %s\n' "$supplied_root" >&2
  exit 1
fi
canonical_root=$(cd "$supplied_root" && pwd -P)
if [[ $canonical_root != "$supplied_root" || $canonical_root == "$repo_root" ]]; then
  printf 'refusing redirected substitution root: %s\n' "$supplied_root" >&2
  exit 1
fi
case $canonical_root in
  /tmp/batuta-contract-workspace.*) ;;
  *)
    printf 'refusing unguarded substitution root: %s\n' "$canonical_root" >&2
    exit 1
    ;;
esac
read -r current_device current_inode < <(stat -c '%d %i' "$canonical_root")
read -r expected_device expected_inode < \
  "$FAKE_COMPOZY_STATE/workspace-identity"
if [[ $current_device != "$expected_device" || \
  $current_inode != "$expected_inode" ]]; then
  printf 'refusing unowned substitution root: %s\n' "$canonical_root" >&2
  exit 1
fi
IFS= read -r recorded_root < "$FAKE_COMPOZY_STATE/workspace-root"
if [[ $recorded_root != "$canonical_root" ]]; then
  printf 'refusing unexpected substitution root: %s\n' "$canonical_root" >&2
  exit 1
fi

target=$(mktemp -d /tmp/batuta-contract-substitution-target.XXXXXX)
printf '%s\n' "$target" > "$FAKE_COMPOZY_STATE/substitution-target"
find "$canonical_root" -depth -delete
ln -s "$target" "$canonical_root"
printf 'substitution evidence\n' > "$target/workspace.toml"
exit "${FIXTURE_EXIT_STATUS:-0}"
