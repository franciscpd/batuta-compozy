#!/usr/bin/env bash
# Verifica o contrato revisado da documentação da release atual.
set -euo pipefail
cd "$(dirname "$0")/../.."

release_notes=docs/releases/0.1.0-beta.6.md
documents=(README.md README.pt-BR.md "$release_notes")
preview_design=docs/internal/specs/2026-08-15-batuta-preview-release-design.md
preview_plan=docs/internal/plans/2026-08-15-batuta-preview-release.md

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

install_command='compozy extension install github:batuta-ai/compozy --allow-unverified --yes'
update_command='compozy extension update batuta --allow-unverified --yes'

for document in "${documents[@]}"; do
  [[ -f $document && ! -L $document ]]
  require "$document" 'batuta-ai/compozy'
  require "$document" 'v0.1.0-beta.6'
  require "$document" '34208e9990622ee62e9a5cf114386273ae6abfa0'
  require "$document" "$install_command"
  require "$document" 'compozy extension enable batuta'
  require "$document" "$update_command"
  require "$document" 'compozy extension remove batuta --global'
  for obsolete in \
    'SHA256SUMS' \
    'gh release download' \
    'batuta-compozy_0.1.0-beta.6.tar.gz' \
    'a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c' \
    '382976d4b43274630a4b67445812fd4a0216dbcc' \
    'check-compozy-version.sh' \
    'batuta-republish.lock'; do
    if grep -qF -- "$obsolete" "$document"; then
      printf 'obsolete install text in %s: %s\n' "$document" "$obsolete" >&2
      exit 1
    fi
  done
  if grep -oE '[0-9a-f]{40}' "$document" | grep -qvFx '34208e9990622ee62e9a5cf114386273ae6abfa0'; then
    printf 'unexpected 40-hex commit hash in %s\n' "$document" >&2
    exit 1
  fi
done

for document in README.md "$release_notes"; do
  require "$document" 'docs/verify.md'
done
require README.pt-BR.md 'docs/verify.pt-BR.md'

for readme in README.md README.pt-BR.md; do
  require "$readme" 'compozy provider models list'
  require "$readme" 'v0.3.0-beta.21'
  first_code_block=$(awk '/^```bash$/{n=1; next} n==1{print; exit}' "$readme")
  if [[ $first_code_block != "$install_command" ]]; then
    printf 'first bash code block in %s is not the install command: %s\n' \
      "$readme" "$first_code_block" >&2
    exit 1
  fi
  usage_line=$(grep -nE '^## (Use|Uso)$' "$readme" | head -n 1 | cut -d: -f1)
  if [[ -z $usage_line || $usage_line -gt 60 ]]; then
    printf 'usage section in %s starts after line 60 (line %s)\n' \
      "$readme" "${usage_line:-none}" >&2
    exit 1
  fi
done

require README.md 'docs/how-it-works.md'
require README.pt-BR.md 'docs/how-it-works.pt-BR.md'

require_wrapped README.md 'independent community project'
require_wrapped README.pt-BR.md 'projeto independente da comunidade'
require_wrapped "$release_notes" 'temporary staging directory'

require_wrapped README.md \
  'Current Compozy renders executor sessions in their parent/child hierarchy and stops run-agent sessions after terminal settlement.'
require_wrapped README.pt-BR.md \
  'O Compozy atual exibe as sessões dos executores na hierarquia pai/filho e encerra as sessões run-agent após o assentamento terminal.'
require_wrapped "$release_notes" \
  'Current Compozy renders executor sessions in their parent/child hierarchy and stops run-agent sessions after terminal settlement.'

for document in README.md README.pt-BR.md "$release_notes"; do
  if grep -qF -- 'executor sessions are not visually nested' "$document" ||
    grep -qF -- 'sessões dos executores não são visualmente aninhadas' "$document" ||
    grep -qF -- 'remain active/idle after normal terminal completion' "$document" ||
    grep -qF -- 'permanecem active/idle após a conclusão terminal normal' "$document"; then
    printf 'obsolete Compozy session limitation remains active in %s\n' "$document" >&2
    exit 1
  fi
done

pt_documents=(
  CONTRIBUTING.pt-BR.md
  docs/architecture.pt-BR.md
  docs/how-it-works.pt-BR.md
  docs/verify.pt-BR.md
  docs/case-studies/version-subcommand.pt-BR.md
  docs/releases/0.1.0-beta.3.pt-BR.md
  docs/releases/0.1.0-beta.6.pt-BR.md
)

for document in "${pt_documents[@]}"; do
  [[ -f $document && ! -L $document ]] || {
    printf 'missing regular Portuguese documentation file: %s\n' "$document" >&2
    exit 1
  }
  require_wrapped "$document" 'Versão em inglês'
done

for target in "${pt_documents[@]}"; do
  require README.pt-BR.md "($target)"
done

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

aggregate_plans=("$preview_plan" docs/internal/plans/2026-08-15-batuta-public-documentation-and-publication.md)
for aggregate_plan in "${aggregate_plans[@]}"; do
  require_wrapped "$aggregate_plan" \
    'Run the aggregate only from a disposable detached worktree at the exact candidate commit; never move, hide, or restore the live repository `.compozy/` tree.'
  require_wrapped "$aggregate_plan" \
    'Use separate isolated `HOME` and `COMPOZY_HOME`, an absolute isolated UDS path, and a unique OS-assigned HTTP port.'
  require_wrapped "$aggregate_plan" \
    'Before and after the suite, require the live daemon PID, process start identity, and socket path/inode to match their read-only snapshots.'
  require_wrapped "$aggregate_plan" \
    'Teardown must stop only the isolated daemon and remove only the exact guarded isolated home and detached worktree, then prove their PID, UDS, port, home, and worktree state absent.'
done

for obsolete_aggregate_contract in \
  'guarded `.compozy` hold/restore procedure' \
  'Move the pre-existing `.compozy` directory' \
  'preserve the exact pre-existing `.compozy/` tree outside the runner-visible path'; do
  if grep -qF -- "$obsolete_aggregate_contract" "${aggregate_plans[@]}"; then
    printf 'obsolete aggregate isolation contract remains active: %s\n' \
      "$obsolete_aggregate_contract" >&2
    exit 1
  fi
done

printf 'OK: current documentation reflects Compozy session lifecycle and exposes complete PT-BR links\n'
