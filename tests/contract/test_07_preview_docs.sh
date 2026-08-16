#!/usr/bin/env bash
# Verifica o contrato revisado de documentação do preview beta.2.
set -euo pipefail
cd "$(dirname "$0")/../.."

release_notes=docs/releases/0.1.0-beta.2.md
documents=(README.md README.pt-BR.md "$release_notes")
preview_design=docs/superpowers/specs/2026-08-15-batuta-preview-release-design.md
preview_plan=docs/superpowers/plans/2026-08-15-batuta-preview-release.md

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
  require "$document" 'tar -xzf "$preview_dir/batuta-compozy_0.1.0-beta.2.tar.gz" -C "$extracted_directory"'
  require "$document" 'compozy extension validate "$extracted_directory" -o json'
  require "$document" 'compozy extension install "$extracted_directory" --allow-unverified --yes'
  require "$document" 'a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c'
  require "$document" 'compozy extension remove batuta --global'
done

require_wrapped README.md 'This is a preview trust boundary'
require_wrapped README.md 'unverified preview source'
require_wrapped README.pt-BR.md 'Este é um limite de confiança do preview'
require_wrapped README.pt-BR.md 'fonte de preview não verificada'
require_wrapped "$release_notes" 'explicit trust boundary'
require_wrapped "$release_notes" 'unverified preview source'

for document in README.md "$release_notes"; do
  require_wrapped "$document" 'executor sessions are not visually nested'
  require_wrapped "$document" 'remain active/idle after normal terminal completion'
done
require_wrapped README.pt-BR.md 'sessões dos executores não são visualmente aninhadas'
require_wrapped README.pt-BR.md 'permanecem active/idle após a conclusão terminal normal'

for superseded_document in "$preview_design" "$preview_plan"; do
  require_wrapped "$superseded_document" \
    'The earlier four-file, no-license package decision is superseded by the approved public documentation and publication plan.'
  require_wrapped "$superseded_document" \
    'Copyright (c) 2026 Francisross Soares de Oliveira'
  for package_file in \
    './LICENSE' \
    './agents/batuta/AGENT.md' \
    './extension.toml' \
    './loops/batuta-deliver/loop.yaml' \
    './resources/skills/batuta-routing/SKILL.md'; do
    require "$superseded_document" "$package_file"
  done
done

for obsolete_contract in \
  'Do not add a license automatically; license selection remains a separate explicit repository decision.' \
  'the package contains only the four expected files' \
  'assert the exact four-file package tree'; do
  if grep -qF -- "$obsolete_contract" "$preview_design" "$preview_plan"; then
    printf 'obsolete preview package contract remains active: %s\n' "$obsolete_contract" >&2
    exit 1
  fi
done

printf 'OK: preview documentation identifies beta.2 assets, trust boundary, and upstream limitations\n'
