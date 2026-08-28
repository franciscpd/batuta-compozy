#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

fixture=tests/fixtures/parallel-delivery/.compozy/tasks/parallel-demo
manifest="$fixture/_tasks.md"

[[ -f $manifest ]]
[[ ! -e "$fixture/_manifest.json" ]]
for task in task_01 task_02 task_03 task_04 task_05; do
  [[ -f "$fixture/$task.md" ]]
  grep -Fx -- "    - id: $task" "$manifest" >/dev/null
done
grep -Fx -- '    - from: task_01' "$manifest" >/dev/null
grep -Fx -- '      to: task_05' "$manifest" >/dev/null

cache_dir=$(mktemp -d -p /tmp batuta-task8-pycache.XXXXXX)
trap 'rm -rf -- "$cache_dir"' EXIT
PYTHONPYCACHEPREFIX="$cache_dir" python3 -m unittest tests.e2e.test_assert_parallel_delivery -v
printf 'OK: parallel delivery fixture and Go-harness evidence contract passed\n'
