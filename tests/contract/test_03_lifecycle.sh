#!/usr/bin/env bash
# Ciclo de vida: install local_path -> enable -> inventory -> remove.
set -euo pipefail
cd "$(dirname "$0")/../.."

if compozy extension list -o json | python3 -c '
import json,sys
rows=json.load(sys.stdin)
sys.exit(0 if any(r["name"]=="batuta" for r in rows) else 1)'; then
  echo "SKIP: extensao batuta instalada (estado intencional); ciclo de vida requer estado desinstalado"
  exit 0
fi

cleanup() { compozy extension remove batuta --global -o json >/dev/null 2>&1 || true; }
trap cleanup EXIT

compozy extension install "$PWD" --allow-unverified --yes -o json \
  | python3 -c 'import json,sys; d=json.load(sys.stdin); print("install:", json.dumps(d)[:200])'

compozy extension enable batuta -o json >/dev/null 2>&1 || true  # install pode ja habilitar

# Nota: nao usar `cmd | python3 - <<PY`; `python3 -` le o proprio script do
# stdin via heredoc, o que sobrescreve o stdin herdado do pipe antes que o
# script possa ler o JSON de sys.stdin (mesma armadilha documentada em
# test_02_routing_dryrun.sh). Passa o JSON por arquivo temporario + argv.
INV_OUT=$(mktemp)
trap 'compozy extension remove batuta --global -o json >/dev/null 2>&1 || true; rm -f "$INV_OUT"' EXIT
compozy extension inventory batuta -o json > "$INV_OUT"

python3 - "$INV_OUT" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
blob = json.dumps(d)
assert "batuta" in blob, "agente batuta ausente do inventory"
assert "batuta-routing" in blob, "skill batuta-routing ausente do inventory"
print("OK: inventory publica agente batuta e skill batuta-routing")
PY
rm -f "$INV_OUT"

compozy extension remove batuta --global -o json | python3 -c '
import json,sys
d=json.load(sys.stdin)
assert d.get("status") in ("removed", None) or "removed" in json.dumps(d), d
print("OK: remocao limpa")'
trap - EXIT
