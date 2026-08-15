#!/usr/bin/env bash
# Verifica o contrato revisado de documentação do preview beta.2.
set -euo pipefail
cd "$(dirname "$0")/../.."

release_notes=docs/releases/0.1.0-beta.2.md
documents=(README.md README.pt-BR.md "$release_notes")

require() {
  local document=$1 text=$2
  if ! grep -qF -- "$text" "$document"; then
    printf 'missing preview documentation text in %s: %s\n' "$document" "$text" >&2
    exit 1
  fi
}

require_wrapped() {
  local document=$1 text=$2
  python3 - "$document" "$text" <<'PY'
import re
import sys

document, text = sys.argv[1:]
contents = re.sub(r"\s+", " ", open(document, encoding="utf-8").read())
if text not in contents:
    raise SystemExit(f"missing preview documentation text in {document}: {text}")
PY
}

for document in "${documents[@]}"; do
  [[ -f $document && ! -L $document ]]
  require "$document" 'franciscpd/batuta-compozy'
  require "$document" 'v0.1.0-beta.2'
  require "$document" 'batuta-compozy_0.1.0-beta.2.tar.gz'
  require "$document" 'SHA256SUMS'
  require "$document" 'gh release download'
  require "$document" 'sha256sum --check'
  require "$document" 'a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c'
  require "$document" 'compozy extension remove batuta --global'
done

for document in README.md "$release_notes"; do
  require_wrapped "$document" 'executor sessions are not visually nested'
  require_wrapped "$document" 'remain active/idle after normal terminal completion'
done
require_wrapped README.pt-BR.md 'sessões dos executores não são visualmente aninhadas'
require_wrapped README.pt-BR.md 'permanecem active/idle após a conclusão terminal normal'

printf 'OK: preview documentation identifies beta.2 assets, trust boundary, and upstream limitations\n'
