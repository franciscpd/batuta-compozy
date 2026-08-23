#!/usr/bin/env bash
# Local dev install: stage the six package files, validate, reinstall, enable, verify.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd -P)
cd "$ROOT"

scripts/check-compozy-version.sh >/dev/null

STAGE=$(mktemp -d /tmp/batuta-republish.XXXXXX)
cleanup() {
  case "$STAGE" in
    /tmp/batuta-republish.*) rm -rf -- "$STAGE" ;;
    *)
      printf 'refusing to clean unexpected staging path: %s\n' "$STAGE" >&2
      return 1
      ;;
  esac
}
trap cleanup EXIT
scripts/stage-extension.sh "$STAGE"

compozy extension validate "$STAGE" -o json | python3 -c '
import json, sys
d = json.load(sys.stdin)
errors = [i for i in d.get("issues", []) if i.get("severity") == "error"]
assert not errors, f"pacote invalido: {errors}"
'

if compozy extension list -o json | python3 -c '
import json, sys
rows = json.load(sys.stdin)
raise SystemExit(0 if any(row.get("name") == "batuta" for row in rows) else 1)
'; then
  compozy extension remove batuta --global -o json >/dev/null
fi

compozy extension install "$STAGE" --allow-unverified --yes -o json >/dev/null
compozy extension enable batuta -o json | python3 -c "
import json,sys
d=json.load(sys.stdin)
state=d.get('extension',{}).get('state')
assert state=='active', f'enable falhou: {d}'
print('extensao ativa')"

compozy extension inventory batuta -o json | python3 -c "
import json,sys
items=json.load(sys.stdin)['items']
actual={(it['kind'], it['name']) for it in items}
expected={('agent','batuta'), ('agent','batuta-publisher'), ('loop','batuta-deliver'), ('skill','batuta-routing')}
assert actual==expected, f'inventario inesperado: {sorted(actual)}'
assert all(it['live'] for it in items), f'recursos nao-live: {items}'
print('recursos live:', ', '.join(it['name'] for it in items))"
