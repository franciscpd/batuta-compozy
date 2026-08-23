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

# O contrato deve mencionar publicação e a rota nothing-to-publish.
python3 - loops/batuta-deliver/loop.yaml <<'PY'
import sys
text = open(sys.argv[1]).read()
assert "publication" in text.lower(), "contract nao menciona publicacao"
assert "nothing to publish" in text, "rota nothing-to-publish ausente do contrato"
print("OK: contrato cobre publicação e rota nothing-to-publish")
PY

# O contrato declara o orçamento de 4h e documenta que a aplicação real
# ocorre via override de compozy__loop_configure (Batuta Bootstrap/Dispatch),
# não pelo literal do contrato sozinho.
python3 - loops/batuta-deliver/loop.yaml <<'PY'
import sys
text = open(sys.argv[1]).read()
assert "14400" in text, "default de 4h ausente"
assert "wall_clock_sec: 0" not in text, "budget ilimitado permanece"
assert "compozy__loop_configure" in text, (
    "comentario sobre o override real de compozy__loop_configure ausente"
)
assert "effective_config.budget_wall_sec" in text, (
    "comentario sobre o campo enforced effective_config.budget_wall_sec ausente"
)
print("OK: budget wall_clock_sec declarado e mecanismo real documentado")
PY

# A cadeia de edges pos-review deve encadear exatamente
# review -> worktree_state -> publish_check e publish_gate -> publish, sem
# atalhos que pulem o gate humano.
python3 - loops/batuta-deliver/loop.yaml <<'PY'
import re
import sys
text = open(sys.argv[1]).read()
edges_block = text.split("\n  edges:\n", 1)[1]
pairs = re.findall(
    r"- from:\s*(\S+)\s*\n\s*to:\s*(\S+)", edges_block
)
assert pairs, "nenhuma edge encontrada"
edge_set = set(pairs)
required_edges = {
    ("review", "worktree_state"),
    ("worktree_state", "publish_check"),
    ("publish_gate", "publish"),
}
missing = required_edges - edge_set
assert not missing, f"edges obrigatorias ausentes: {missing}"
# publish_check so pode levar a publish via publish_gate - nunca direto.
assert ("publish_check", "publish") not in edge_set, (
    "publish_check pula o gate humano e vai direto para publish"
)
print("OK: cadeia de edges review -> worktree_state -> publish_check e publish_gate -> publish presente")
PY

# O predicado de publish_check deve ser fail-closed: como o daemon nao expoe
# refresh no node compozy__worktree_inspect nem now() no CEL de branch (ambos
# confirmados via `compozy tool info` / `compozy loop validate` durante o
# fix), a unica rota honesta e sempre encaminhar ao gate humano - nunca
# decidir "nothing to publish" a partir de uma leitura potencialmente
# obsoleta de ahead_of_base.
python3 - loops/batuta-deliver/loop.yaml <<'PY'
import sys
text = open(sys.argv[1]).read()
assert 'condition: "true"' in text, (
    "publish_check nao esta fail-closed (esperado condition: \"true\", sempre rotea ao gate)"
)
assert "ahead_of_base > 0" not in text, (
    "predicado antigo (ahead_of_base > 0) ainda presente - reintroduz o skip silencioso"
)
assert "no `refresh` flag" in text, (
    "comentario sobre ausencia de refresh no worktree_inspect ausente"
)
assert "undeclared reference to 'now'" in text, (
    "comentario sobre ausencia de now() no CEL de branch ausente"
)
print("OK: publish_check fail-closed (sempre rotea ao publish_gate) e documentado")
PY

# O output_schema do node publish deve exigir head_sha e op_ids, mais pelo
# menos uma URL de publicacao (pr_url ou compare_url) via anyOf - uma
# "success" sem evidencia de push nao pode satisfazer o contrato.
python3 - loops/batuta-deliver/loop.yaml <<'PY'
import sys
text = open(sys.argv[1]).read()
assert "required: [status, summary, head_sha, op_ids]" in text, (
    "output_schema do publish nao exige head_sha/op_ids"
)
assert "compare_url:" in text, "propriedade compare_url ausente do output_schema"
assert "anyOf:" in text, "anyOf (pr_url ou compare_url) ausente do output_schema"
assert "required: [pr_url]" in text and "required: [compare_url]" in text, (
    "anyOf nao cobre pr_url e compare_url"
)
print("OK: output_schema do publish exige head_sha, op_ids e ao menos uma URL de publicacao")
PY
