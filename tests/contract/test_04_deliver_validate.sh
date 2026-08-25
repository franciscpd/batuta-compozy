#!/usr/bin/env bash
# Valida o contrato do Loop batuta-deliver sem publicar.
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
  rm -f -- "$TMP" || cleanup_failed=true
  reject_new_repository_marker "$REPO_ROOT" "$REPO_WORKSPACE_PREEXISTED" || cleanup_failed=true
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

python3 - loops/batuta-deliver/loop.yaml agents/batuta-publisher/AGENT.md agents/batuta/AGENT.md <<'PY'
import re
import sys

loop = open(sys.argv[1], encoding="utf-8").read()
publisher = open(sys.argv[2], encoding="utf-8").read()
conductor = open(sys.argv[3], encoding="utf-8").read()

assert "\n  auto_commit:\n" not in loop, "auto_commit ainda e input do operador"
assert loop.count("auto_commit: true") == 2, "filhos nao recebem dois commits literais true"
assert loop.count("kind: run-loop") == 2, "esperados exatamente dois run-loop filhos"
assert "loop: implement-tasks" in loop and "loop: review-and-fix" in loop

assert "id: publication_plan" in loop
assert "kind: ext__batuta__publication_plan" in loop
assert "id: publication_route" in loop and "kind: route" in loop
for disposition in ("publishable", "nothing_to_publish", "blocked"):
    assert f"nodes.publication_plan.output.disposition == '{disposition}'" in loop

assert "id: publish_gate" not in loop, "gate humano permanece no caminho saudavel"
assert "id: recovery_gate" in loop and "kind: human" in loop
assert "id: publish" in loop and "agent: batuta-publisher" in loop
assert "id: publication_verify" in loop
assert "kind: ext__batuta__publication_verify" in loop
assert "publisher_result: \"{{ .nodes.publish.output.publication_result }}\"" in loop
assert "compare_url" not in loop, "compare URL ainda e aceito como sucesso"
assert "merge" not in loop.lower() or "merge manual" in loop.lower(), "merge automatico apareceu no Loop"

edges = loop.split("\n  edges:\n", 1)[1]
pairs = set(re.findall(r"- from:\s*(\S+)\s*\n\s*to:\s*(\S+)", edges))
required = {
    ("review", "publication_plan"),
    ("publication_plan", "publication_route"),
    ("publication_route", "publish"),
    ("publication_route", "publication_verify_nothing"),
    ("publication_route", "recovery_gate"),
    ("publish", "publication_verify"),
}
assert not (required - pairs), f"edges automaticas ausentes: {required - pairs}"
assert ("recovery_gate", "publish") not in pairs, "recovery gate publica sem replanejar"

assert "permissions: approve-all" in publisher
assert re.search(r"tools:\s*\n\s*- ext__batuta__publish_worktree", publisher)
assert "ext__batuta__publication_plan" not in publisher
assert "ext__batuta__publication_verify" not in publisher
for forbidden in ("shell", "filesystem", "merge"):
    assert forbidden in publisher.lower(), f"publisher nao proibe {forbidden}"

assert "auto_commit=true" in conductor
assert "automatically publishes" in conductor.lower()
assert "merge manual" in conductor.lower()
print("OK: grafo publica e abre PR automaticamente; operador aparece apenas em recovery")
PY

python3 - loops/batuta-deliver/loop.yaml <<'PY'
import sys
text = open(sys.argv[1], encoding="utf-8").read()
assert "14400" in text, "default de 4h ausente"
assert "wall_clock_sec: 0" not in text, "budget ilimitado permanece"
assert "compozy__loop_configure" in text
assert "effective_config.budget_wall_sec" in text
assert "publication_verify" in text
assert "real PR" in text or "PR URL" in text
print("OK: budget e verificacao independente permanecem no contrato")
PY
