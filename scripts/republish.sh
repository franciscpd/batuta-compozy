#!/usr/bin/env bash
# Republica somente o pacote declarado da extensão batuta.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd -P)
lock_fd=${BATUTA_PACKAGE_LOCK_FD:-}
lock_path=${BATUTA_PACKAGE_LOCK_PATH:-}
if ! python3 "$ROOT/scripts/with-package-lock.py" \
  --check "$lock_fd" "$lock_path"; then
  exec python3 "$ROOT/scripts/with-package-lock.py" \
    "$ROOT/scripts/republish.sh" "$@"
fi
cd "$ROOT"

scripts/check-compozy-version.sh >/dev/null
PACKAGE_DIR=$(scripts/package-extension.sh)
compozy extension validate "$PACKAGE_DIR" -o json | python3 -c '
import json, sys
d = json.load(sys.stdin)
errors = [i for i in d.get("issues", []) if i.get("severity") == "error"]
assert not errors, f"pacote invalido: {errors}"
'

installed=false
if compozy extension list -o json | python3 -c '
import json, sys
rows = json.load(sys.stdin)
raise SystemExit(0 if any(row["name"] == "batuta" for row in rows) else 1)
'; then
  installed=true
fi

REVALIDATED_PACKAGE_DIR=$(scripts/package-extension.sh)
if [[ $REVALIDATED_PACKAGE_DIR != "$PACKAGE_DIR" ]]; then
  printf 'package digest changed during publication: %s -> %s\n' \
    "$PACKAGE_DIR" "$REVALIDATED_PACKAGE_DIR" >&2
  exit 1
fi

if [[ $installed == true ]]; then
  compozy extension remove batuta --global -o json >/dev/null
fi

compozy extension install "$PACKAGE_DIR" --allow-unverified --yes -o json >/dev/null
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
expected={('agent','batuta'), ('loop','batuta-deliver'), ('skill','batuta-routing')}
assert actual==expected, f'inventario inesperado: {sorted(actual)}'
assert all(it['live'] for it in items), f'recursos nao-live: {items}'
print('recursos live:', ', '.join(it['name'] for it in items))"
