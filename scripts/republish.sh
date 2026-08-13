#!/usr/bin/env bash
# Republica a extensão batuta (install + enable + verificação de recursos live).
#
# Desde o fix upstream do catálogo de skills (compozy/compozy#350, na main após
# v0.3.0-beta.13), agentes publicados por extensão funcionam direto no prompt —
# a extensão é a única fonte, sem cópia autorada.
set -euo pipefail
cd "$(dirname "$0")/.."

compozy extension remove batuta --global -o json >/dev/null 2>&1 || true
compozy extension install "$PWD" --allow-unverified --yes -o json >/dev/null
compozy extension enable batuta -o json | python3 -c "
import json,sys
d=json.load(sys.stdin)
state=d.get('extension',{}).get('state')
assert state=='active', f'enable falhou: {d}'
print('extensao ativa')"

compozy extension inventory batuta -o json | python3 -c "
import json,sys
items=json.load(sys.stdin)['items']
assert all(it['live'] for it in items), f'recursos nao-live: {items}'
print('recursos live:', ', '.join(it['name'] for it in items))"
