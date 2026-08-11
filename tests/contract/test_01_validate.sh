#!/usr/bin/env bash
# Valida o manifest da extensão sem executar código.
set -euo pipefail
cd "$(dirname "$0")/../.."

compozy extension validate . -o json | python3 -c "
import json, sys
d = json.load(sys.stdin)
issues = d.get('issues') or []
errors = [i for i in issues if i.get('severity') == 'error']
assert not errors, f'validate retornou erros: {errors}'
print('OK: manifest valido, sem issues de severidade error')
"
