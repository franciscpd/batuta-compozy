#!/usr/bin/env bash
# Deriva um provider/modelo utilizavel do catalogo vivo e valida runtime_rules.
set -euo pipefail
cd "$(dirname "$0")/../.."
source tests/contract/lib.sh
WS=$(require_test_workspace)
PROVIDERS=$(mktemp)
MODELS=$(mktemp)
PAIR_FILE=$(mktemp)
TMP_OUT=$(mktemp)
cleanup() {
  rm -f -- "$PROVIDERS" "$MODELS" "$PAIR_FILE" "$TMP_OUT"
}
trap cleanup EXIT

compozy provider list -o json > "$PROVIDERS"
compozy provider models list -o json > "$MODELS"

python3 tests/contract/select_routing_pair.py \
  "$PROVIDERS" "$MODELS" > "$PAIR_FILE"
mapfile -t PAIR < "$PAIR_FILE"

PROVIDER=${PAIR[0]}
MODEL=${PAIR[1]}
ARGS=()
for lane in low medium high critical; do
  ARGS+=(--runtime "complexity=$lane:$PROVIDER/$MODEL")
done

# Dry-run planeja o grafo e nao executa import_tasks; nenhum fixture e necessario.
out=$(compozy loop run --workspace "$WS" --name implement-tasks \
  --input slug=_batuta_routing_shape_probe "${ARGS[@]}" --dry-run -o json)

# Nota: nao usar `echo "$out" | python3 - <<PY`; `python3 -` le o proprio
# script do stdin, consumindo-o antes que o script possa ler $out de sys.stdin.
# Passa o JSON por arquivo temporario + argv em vez disso.
printf '%s' "$out" > "$TMP_OUT"

python3 - "$TMP_OUT" "$PROVIDER" "$MODEL" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))["dry_run"]
rules = d["effective_config"]["run_runtime_rules"]
lanes = {r["match"]["complexity"]: r["runtime"] for r in rules}
assert set(lanes) == {"low", "medium", "high", "critical"}, f"lanes no dry-run: {sorted(lanes)}"
for lane, rt in lanes.items():
    assert rt.get("provider") == sys.argv[2], f"provider errado na lane {lane}: {rt}"
    assert rt.get("model") == sys.argv[3], f"modelo errado na lane {lane}: {rt}"
print(f"OK: dry-run aceitou 4 lanes com {sys.argv[2]}/{sys.argv[3]}")
PY
