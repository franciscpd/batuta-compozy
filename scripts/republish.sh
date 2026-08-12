#!/usr/bin/env bash
# Republica a extensão batuta e re-sincroniza a cópia autorada do agente.
#
# Necessário enquanto o CompozyOS (<= 0.3.0-beta.13) tiver o bug em que agentes
# publicados por extensão são invisíveis ao catálogo de skills das sessões
# (500 "agent not found" no prompt). Workaround: manter uma cópia AUTORADA
# global do agente. Ordem obrigatória: o enable recusa se a cópia autorada
# existir (extension_agent_conflict), então deleta -> enable -> recria.
set -euo pipefail
cd "$(dirname "$0")/.."

compozy agent delete batuta --yes >/dev/null 2>&1 || true
compozy extension remove batuta --global -o json >/dev/null 2>&1 || true
compozy extension install "$PWD" --allow-unverified --yes -o json >/dev/null
compozy extension enable batuta -o json | python3 -c "
import json,sys
d=json.load(sys.stdin)
state=d.get('extension',{}).get('state')
assert state=='active', f'enable falhou: {d}'
print('extensao ativa')"

python3 - <<'PY'
import re
body = re.sub(r'^---\n.*?\n---\n', '', open('agents/batuta/AGENT.md').read(), flags=re.S).strip()
fm = '---\nname: batuta\nprovider: ""\ncategory_path:\n- Batuta\n---\n\n'
import pathlib
pathlib.Path('/home/franciscpd/.compozy/agents/batuta').mkdir(parents=True, exist_ok=True)
open('/home/franciscpd/.compozy/agents/batuta/AGENT.md','w').write(fm + body + '\n')
print('copia autorada sincronizada')
PY

compozy extension inventory batuta -o json | python3 -c "
import json,sys
items=json.load(sys.stdin)['items']
assert all(it['live'] for it in items), f'recursos nao-live: {items}'
print('recursos live:', ', '.join(it['name'] for it in items))"
