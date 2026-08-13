#!/usr/bin/env bash
# Republica somente o pacote declarado da extensão batuta.
set -euo pipefail
cd "$(dirname "$0")/.."

scripts/check-compozy-version.sh >/dev/null
PACKAGE_DIR=$(scripts/package-extension.sh)
compozy extension validate "$PACKAGE_DIR" -o json | python3 -c '
import json, sys
d = json.load(sys.stdin)
errors = [i for i in d.get("issues", []) if i.get("severity") == "error"]
assert not errors, f"pacote invalido: {errors}"
'

if compozy extension list -o json | python3 -c '
import json, sys
rows = json.load(sys.stdin)
raise SystemExit(0 if any(row["name"] == "batuta" for row in rows) else 1)
'; then
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
