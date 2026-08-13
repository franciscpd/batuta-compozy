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

STAGE=$(mktemp -d /tmp/batuta-lifecycle.XXXXXX)
INV_OUT=$(mktemp)
MUTATION_STARTED=false
cleanup() {
  local original_status=$?
  local cleanup_failed=false
  trap - EXIT

  if [[ $MUTATION_STARTED == true ]]; then
    if ! compozy extension remove batuta --global -o json >/dev/null; then
      printf 'cleanup failed to remove global batuta after lifecycle mutation\n' >&2
      cleanup_failed=true
    fi
  fi
  rm -f -- "$INV_OUT"
  case "$STAGE" in
    /tmp/batuta-lifecycle.*) rm -rf -- "$STAGE" ;;
    *)
      printf 'refusing to clean unexpected staging path: %s\n' "$STAGE" >&2
      cleanup_failed=true
      ;;
  esac

  if [[ $cleanup_failed == true ]]; then
    exit 1
  fi
  exit "$original_status"
}
trap cleanup EXIT

scripts/stage-extension.sh "$STAGE"
MUTATION_STARTED=true
compozy extension install "$STAGE" --allow-unverified --yes -o json \
  | python3 -c 'import json,sys; d=json.load(sys.stdin); print("install:", json.dumps(d)[:200])'

compozy extension enable batuta -o json | python3 -c '
import json, sys
d = json.load(sys.stdin)
assert d.get("extension", {}).get("state") == "active", d
'

# Nota: nao usar `cmd | python3 - <<PY`; `python3 -` le o proprio script do
# stdin via heredoc, o que sobrescreve o stdin herdado do pipe antes que o
# script possa ler o JSON de sys.stdin (mesma armadilha documentada em
# test_02_routing_dryrun.sh). Passa o JSON por arquivo temporario + argv.
compozy extension inventory batuta -o json > "$INV_OUT"

python3 - "$INV_OUT" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
items = d["items"]
actual = {(item["kind"], item["name"]) for item in items}
expected = {
    ("agent", "batuta"),
    ("loop", "batuta-deliver"),
    ("skill", "batuta-routing"),
}
assert actual == expected, f"inventory inesperado: {sorted(actual)}"
assert all(item["live"] for item in items), f"recursos nao-live: {items}"
print("OK: inventory publica exatamente os tres recursos live")
PY
rm -f "$INV_OUT"

compozy extension remove batuta --global -o json | python3 -c '
import json,sys
d=json.load(sys.stdin)
assert d.get("status") in ("removed", None) or "removed" in json.dumps(d), d
print("OK: remocao limpa")'
MUTATION_STARTED=false
