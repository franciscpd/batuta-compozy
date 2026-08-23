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
assert "worktree_ref:" in text, "worktree_ref input ausente"
assert 'mode: worktree' in text, "Loop-default worktree environment ausente"
assert 'worktree_ref: "{{ .inputs.worktree_ref }}"' in text, (
    "environment nao referencia o input worktree_ref"
)
print("OK: encadeamento implement-tasks -> review-and-fix por composição")
PY

# O grafo pos-review deve inspecionar o worktree, decidir por branch, abrir o
# gate humano e disparar a publicação via agente batuta-publisher.
python3 - loops/batuta-deliver/loop.yaml <<'PY'
import sys
text = open(sys.argv[1]).read()
assert "id: worktree_state" in text, "no worktree_state inspect node"
assert "kind: compozy__worktree_inspect" in text, "inspect action ausente"
assert "id: publish_check" in text and "kind: branch" in text, "branch node ausente"
assert "id: publish_gate" in text and "kind: gate" in text, "human gate ausente"
assert "kind: human" in text, "criterio human ausente"
assert "id: publish" in text and "kind: goal" in text, "publish goal ausente"
assert "agent: batuta-publisher" in text, "publisher agent nao referenciado"
print("OK: grafo pos-review inspeciona, decide, aprova e publica")
PY
