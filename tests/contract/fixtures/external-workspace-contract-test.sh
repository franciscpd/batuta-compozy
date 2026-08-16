#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "$0")/../.." && pwd -P)
expected_root=$(cd "$BATUTA_TEST_WORKSPACE_ROOT" && pwd -P)
[[ $expected_root != "$repo_root" ]]
[[ $BATUTA_TEST_WORKSPACE == ws-created ]]
[[ -f $expected_root/.compozy/workspace.toml ]]

rm -f -- "$expected_root/.compozy/workspace.toml"
rmdir "$expected_root/.compozy"
mkdir "$expected_root/.compozy"
printf 'lifecycle-recreated external marker\n' \
  > "$expected_root/.compozy/workspace.toml"
[[ ! -e $repo_root/.compozy && ! -L $repo_root/.compozy ]]
printf 'OK: contracts use only the external runner workspace\n'
