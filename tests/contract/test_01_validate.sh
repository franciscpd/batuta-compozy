#!/usr/bin/env bash
# Valida o manifest da extensão sem executar código.
set -euo pipefail
cd "$(dirname "$0")/../.."

compozy extension validate "$PWD" -o json | python3 -c "
import json, sys
d = json.load(sys.stdin)
issues = d.get('issues') or []
errors = [i for i in issues if i.get('severity') == 'error']
assert not errors, f'validate retornou erros: {errors}'
manifest = d.get(\"manifest\") or {}
assert manifest.get(\"version\") == \"0.1.0-beta.4\", manifest
assert manifest.get(\"min_compozy_version\") == \"0.3.0-beta.13\", manifest
print('OK: manifest valido, sem issues de severidade error')
"
