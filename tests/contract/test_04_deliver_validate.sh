#!/usr/bin/env bash
# Valida a definição do Loop batuta-deliver sem publicar (lint + compile no daemon).
set -euo pipefail
cd "$(dirname "$0")/../.."
source tests/contract/lib.sh
REPO_ROOT=$PWD
REPO_WORKSPACE_PREEXISTED=false
if workspace_marker_present "$REPO_ROOT"; then
  REPO_WORKSPACE_PREEXISTED=true
fi
WS=$(require_test_workspace)
TMP=$(mktemp)
cleanup() {
  local original_status=$?
  local cleanup_failed=false
  trap - EXIT

  if ! rm -f -- "$TMP"; then
    cleanup_failed=true
  fi
  if ! reject_new_repository_marker \
    "$REPO_ROOT" "$REPO_WORKSPACE_PREEXISTED"; then
    cleanup_failed=true
  fi
  if [[ $cleanup_failed == true ]]; then
    exit 1
  fi
  exit "$original_status"
}
trap cleanup EXIT

out=$(compozy loop validate --file loops/batuta-deliver/loop.yaml --workspace "$WS" -o json)
printf '%s' "$out" > "$TMP"
python3 - "$TMP" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
assert d.get("valid") is True, f"batuta-deliver invalido: {d}"
print("OK: batuta-deliver valido (lint+compile)")
PY

# O grafo deve encadear implement -> review via run-loop, sem tocar nos bundled.
python3 - loops/batuta-deliver/loop.yaml <<'PY'
import sys
text = open(sys.argv[1]).read()
assert "kind: run-loop" in text, "nós run-loop ausentes"
assert "loop: implement-tasks" in text and "loop: review-and-fix" in text, "encadeamento incompleto"
assert text.count("kind: run-loop") == 2, "esperados exatamente 2 nós run-loop"
assert "kind: ext__spec_cycle__import_tasks" in text, (
    "spec-cycle import action absent"
)
assert "ext__dev_cycle__import_tasks" not in text, (
    "retired dev-cycle import action remains"
)
print("OK: encadeamento implement-tasks -> review-and-fix por composição")
PY
