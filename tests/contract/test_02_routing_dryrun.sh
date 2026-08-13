#!/usr/bin/env bash
# Extrai runtime_rules da skill batuta-routing e valida por dry-run do implement-tasks.
set -euo pipefail
cd "$(dirname "$0")/../.."
source tests/contract/lib.sh
WS=$(require_test_workspace)
SKILL="resources/skills/batuta-routing/SKILL.md"

RULES_JSON=$(python3 - "$SKILL" <<'PY'
import re, sys, json
text = open(sys.argv[1]).read()
m = re.search(r"```json runtime_rules\n(.*?)```", text, re.S)
assert m, "bloco '```json runtime_rules' nao encontrado na skill"
rules = json.loads(m.group(1))
assert isinstance(rules, list) and rules, "runtime_rules deve ser lista nao vazia"
lanes = [r["match"]["complexity"] for r in rules]
assert lanes == ["low", "medium", "high", "critical"], f"lanes erradas: {lanes}"
print(json.dumps(rules))
PY
)

# Fixture descartavel: uma task minima para o import do dry-run resolver.
mkdir -p .compozy/tasks
PROBE_DIR=$(mktemp -d .compozy/tasks/_routing_probe_XXXXXX)
PROBE_SLUG=$(basename "$PROBE_DIR")
TMP_OUT=$(mktemp)
cleanup() {
  rm -rf -- "$PROBE_DIR"
  rm -f -- "$TMP_OUT"
}
trap cleanup EXIT
cat > "$PROBE_DIR/task_01.md" <<'TASK'
---
status: pending
title: Routing probe
type: chore
complexity: low
---
# Routing probe
Dry-run probe only.
TASK
cat > "$PROBE_DIR/_tasks.md" <<MANIFEST
---
schema_version: "compozy.tasks/v2"
workflow: $PROBE_SLUG
graph:
  nodes:
    - id: task_01
      file: task_01.md
  edges: []
---
# Routing Probe Task List
MANIFEST

# Monta os --runtime a partir do JSON da skill (expressao provider/model@reasoning).
mapfile -t RUNTIME_FLAGS < <(python3 - <<PY
import json
for r in json.loads('''$RULES_JSON'''):
    rt = r["runtime"]
    expr = f"{rt['provider']}/{rt['model']}"
    if rt.get("reasoning"):
        expr += "@" + rt["reasoning"]
    print(f"complexity={r['match']['complexity']}:{expr}")
PY
)
ARGS=()
for f in "${RUNTIME_FLAGS[@]}"; do ARGS+=(--runtime "$f"); done

# Nota: o dry-run valida providers, mas IDs exatos de modelo passam adiante sem validação (typo em model nao falha aqui).
out=$(compozy loop run --workspace "$WS" --name implement-tasks \
  --input slug=$PROBE_SLUG "${ARGS[@]}" --dry-run -o json)

# Nota: nao usar `echo "$out" | python3 - <<PY`; `python3 -` le o proprio
# script do stdin, consumindo-o antes que o script possa ler $out de sys.stdin.
# Passa o JSON por arquivo temporario + argv em vez disso.
printf '%s' "$out" > "$TMP_OUT"

python3 - "$TMP_OUT" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))["dry_run"]
rules = d["effective_config"]["run_runtime_rules"]
lanes = {r["match"]["complexity"]: r["runtime"] for r in rules}
assert set(lanes) == {"low", "medium", "high", "critical"}, f"lanes no dry-run: {sorted(lanes)}"
for lane, rt in lanes.items():
    assert rt.get("provider") and rt.get("model"), f"lane {lane} sem provider/model resolvido: {rt}"
print("OK: dry-run aceitou as 4 lanes em run_runtime_rules")
PY
