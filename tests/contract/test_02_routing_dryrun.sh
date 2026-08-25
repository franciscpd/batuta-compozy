#!/usr/bin/env bash
# Deriva um provider/modelo utilizavel do catalogo vivo e valida runtime_rules.
set -euo pipefail
cd "$(dirname "$0")/../.."
source tests/contract/lib.sh
REPO_ROOT=$PWD
REPO_WORKSPACE_PREEXISTED=false
if workspace_marker_present "$REPO_ROOT"; then
  REPO_WORKSPACE_PREEXISTED=true
fi
WS=$(require_test_workspace)
PROVIDERS=$(mktemp)
MODELS=$(mktemp)
PAIR_FILE=$(mktemp)
TMP_OUT=$(mktemp)
RULES_FILE=$(mktemp)
cleanup() {
  local original_status=$?
  local cleanup_failed=false
  trap - EXIT

  if ! reject_new_repository_marker \
    "$REPO_ROOT" "$REPO_WORKSPACE_PREEXISTED"; then
    cleanup_failed=true
  fi
  if ! rm -f -- "$PROVIDERS" "$MODELS" "$PAIR_FILE" "$TMP_OUT" "$RULES_FILE"; then
    cleanup_failed=true
  fi

  if [[ $cleanup_failed == true ]]; then
    exit 1
  fi
  exit "$original_status"
}
trap cleanup EXIT

compozy provider list -o json > "$PROVIDERS"
compozy provider models list -o json > "$MODELS"

python3 tests/contract/select_routing_pair.py \
  "$PROVIDERS" "$MODELS" > "$PAIR_FILE"
mapfile -t PAIR < "$PAIR_FILE"

PROVIDER=${PAIR[0]}
MODEL=${PAIR[1]}

python3 - "$RULES_FILE" "$PROVIDER" "$MODEL" <<'PY'
import json, sys

path, provider, model = sys.argv[1:]
cells = [
    ("backend", "low"),
    ("frontend", "medium"),
    ("infra", "high"),
    ("security", "critical"),
]
config = {
    "runtime_rules": [
        {
            "match": {"type": domain, "complexity": complexity},
            "runtime": {"provider": provider, "model": model},
        }
        for domain, complexity in cells
    ]
}
with open(path, "w", encoding="utf-8") as handle:
    json.dump(config, handle)
PY

# Dry-run planeja o grafo e nao executa import_tasks; nenhum fixture e necessario.
out=$(compozy loop run --workspace "$WS" --name implement-tasks \
  --input slug=_batuta_routing_shape_probe --config-file "$RULES_FILE" \
  --dry-run -o json)

# Nota: nao usar `echo "$out" | python3 - <<PY`; `python3 -` le o proprio
# script do stdin, consumindo-o antes que o script possa ler $out de sys.stdin.
# Passa o JSON por arquivo temporario + argv em vez disso.
printf '%s' "$out" > "$TMP_OUT"

python3 - "$TMP_OUT" "$PROVIDER" "$MODEL" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))["dry_run"]
rules = d["effective_config"]["run_runtime_rules"]
lanes = {
    (r["match"].get("type"), r["match"].get("complexity")): r["runtime"]
    for r in rules
}
expected = {
    ("backend", "low"),
    ("frontend", "medium"),
    ("infra", "high"),
    ("security", "critical"),
}
assert set(lanes) == expected, f"lanes no dry-run: {sorted(lanes)}"
for lane, rt in lanes.items():
    assert rt.get("provider") == sys.argv[2], f"provider errado na lane {lane}: {rt}"
    assert rt.get("model") == sys.argv[3], f"modelo errado na lane {lane}: {rt}"
print(f"OK: dry-run aceitou 4 lanes type+complexity com {sys.argv[2]}/{sys.argv[3]}")
PY
