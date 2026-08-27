#!/usr/bin/env bash
# Verifies Batuta's public documentation and contribution contract.
set -euo pipefail
cd "$(dirname "$0")/../.."

require_file() {
  [[ -f $1 && ! -L $1 ]] || {
    printf 'missing regular public documentation file: %s\n' "$1" >&2
    exit 1
  }
}

require_text() {
  local document=$1 text=$2
  if ! grep -qF -- "$text" "$document"; then
    printf 'missing public documentation text in %s: %s\n' "$document" "$text" >&2
    exit 1
  fi
}

require_file docs/architecture.md
require_file docs/case-studies/version-subcommand.md
require_file CONTRIBUTING.md
require_file docs/releases/0.1.0-beta.5.md

for text in \
  'independent community project' \
  'docs/architecture.md' \
  'docs/case-studies/version-subcommand.md' \
  'CONTRIBUTING.md' \
  'LICENSE' \
  'docs/how-it-works.md' \
  'docs/verify.md' \
  'https://www.compozy.com/docs/' \
  'https://github.com/compozy/compozy' \
  'automatic executor inventory' \
  'domain × complexity' \
  '`auto_commit=true`' \
  'bounded fresh-run fallback' \
  'no human publication gate' \
  'merge remains manual'; do
  require_text README.md "$text"
done

for text in \
  'projeto independente da comunidade' \
  'docs/architecture.md' \
  'docs/case-studies/version-subcommand.md' \
  'CONTRIBUTING.md' \
  'LICENSE' \
  'docs/how-it-works.md' \
  'docs/verify.md' \
  'https://www.compozy.com/docs/' \
  'https://github.com/compozy/compozy' \
  'inventário automático de executores' \
  'domínio × complexidade' \
  '`auto_commit=true`' \
  'fallback limitado em novo run' \
  'sem gate humano de publicação' \
  'merge continua manual'; do
  require_text README.pt-BR.md "$text"
done

for document in README.md README.pt-BR.md; do
  for obsolete in 'waits behind a human publication gate' 'gate humano de publicação que você aprova' 'Should I enable automatic commits' 'Devo habilitar commits automáticos'; do
    if grep -Fq -- "$obsolete" "$document"; then
      printf '%s ainda descreve comportamento beta.4 removido: %s\n' "$document" "$obsolete" >&2
      exit 1
    fi
  done
done

for text in \
  'Operator' \
  'Batuta session' \
  'spec-cycle' \
  'ext__spec_cycle__import_tasks' \
  'batuta-deliver' \
  'implement-tasks' \
  'review-and-fix' \
  'compozy__session_prompt' \
  'Resource and authority boundaries' \
  'ext__batuta__publish_worktree' \
  'delivery_id' \
  'fresh-run recovery' \
  'one PR per delivery phase'; do
  require_text docs/architecture.md "$text"
done

if grep -Fq -- 'batuta-publisher' docs/architecture.md; then
  printf 'docs/architecture.md ainda descreve o agente publicador removido\n' >&2
  exit 1
fi

require_file agents/batuta/AGENT.md

for text in \
  'full Compozy tool scope' \
  'write the SDD artifacts' \
  'Never implement feature code' \
  '`cy-create-spec`' \
  '`cy-create-tasks`' \
  '`ext__batuta__executor_inventory`' \
  '`ext__batuta__routing_plan`' \
  '`ext__batuta__routing_apply`' \
  '`ext__spec_cycle__import_tasks`' \
  '`batuta-deliver`' \
  'one PR per delivery phase' \
  'domain × complexity' \
  'Never run concurrent writers in one worktree.' \
  'merge remains manual'; do
  require_text agents/batuta/AGENT.md "$text"
done

require_file docs/how-it-works.md
require_file docs/verify.md
require_file tests/e2e/SMOKE.md

for text in \
  'cy-create-spec' \
  'cy-create-tasks' \
  'full workspace' \
  'SDD' \
  'ext__batuta__executor_inventory' \
  'ext__batuta__routing_plan' \
  'ext__batuta__routing_apply' \
  'ext__spec_cycle__import_tasks' \
  'batuta-deliver' \
  'origin_session_id' \
  'compozy__loop_status' \
  'agents/batuta/AGENT.md' \
  'resources/skills/batuta-routing/SKILL.md' \
  'ext__batuta__publish_worktree' \
  'publication_verify' \
  'batuta/<slug>' \
  'wall-clock budget' \
  'domain × complexity' \
  'backend/low' \
  'frontend/medium' \
  'delivery_id' \
  'fresh parent run' \
  'stored Compozy Loop configuration is never mutated' \
  'one commit per task' \
  'one PR per delivery phase' \
  'merge remains manual'; do
  require_text docs/how-it-works.md "$text"
done

for document in docs/how-it-works.md tests/e2e/SMOKE.md; do
  if grep -Fq -- 'batuta-publisher' "$document"; then
    printf '%s ainda descreve o agente publicador removido\n' "$document" >&2
    exit 1
  fi
  if grep -Fq -- 'auto_commit=false' "$document"; then
    printf '%s ainda descreve auto_commit como preferencia do operador\n' "$document" >&2
    exit 1
  fi
done

for text in \
  'cy-create-spec' \
  'cy-create-tasks' \
  'executor_inventory' \
  'routing_plan' \
  'routing_apply' \
  'start_delivery' \
  'delivery_id' \
  'one commit per task' \
  'one PR per delivery phase' \
  'no human publication gate' \
  'merge remains manual'; do
  require_text tests/e2e/SMOKE.md "$text"
done

for document in README.md README.pt-BR.md docs/architecture.md docs/how-it-works.md tests/e2e/SMOKE.md; do
  for obsolete in 'same-lineage fallback' 'fallback limitado na mesma linhagem' 'revisioned CAS' 'read-back revisionado'; do
    if grep -Fq -- "$obsolete" "$document"; then
      printf '%s ainda descreve o desenho substituido: %s\n' "$document" "$obsolete" >&2
      exit 1
    fi
  done
done

for text in \
  '--allow-unverified' \
  'unverified' \
  'digest_matched' \
  'compozy extension provenance batuta' \
  'batuta-v0.1.0-beta.4.tar.gz' \
  'batuta-v0.1.0-beta.4.tar.gz.sha256' \
  'sha256sum --check' \
  'compozy extension validate' \
  'compozy extension install github:franciscpd/batuta-compozy --allow-unverified --yes' \
  'compozy extension enable batuta' \
  'scripts/republish.sh'; do
  require_text docs/verify.md "$text"
done
require_text docs/verify.md 'all eight hosted Batuta tools'

for text in \
  'bash -n scripts/*.sh tests/contract/*.sh' \
  "python3 -m unittest discover -s tests/e2e -p 'test_*.py' -v" \
  'tests/contract/run.sh' \
  'git diff --check' \
  '^(build|ci|docs|feat|fix|perf|refactor|test): [a-z].+$' \
  '.compozy/' \
  'gh workflow run release.yml' \
  'gh release delete' \
  '--cleanup-tag' \
  'release.yml' \
  'scripts/republish.sh' \
  'docs/internal/specs' \
  'docs/internal/plans'; do
  require_text CONTRIBUTING.md "$text"
done

if [[ -e docs/superpowers ]]; then
  printf 'internal planning docs must live under docs/internal, not docs/superpowers\n' >&2
  exit 1
fi
require_file CLAUDE.md
require_text CLAUDE.md 'docs/internal/specs/'
require_text CLAUDE.md 'docs/internal/plans/'

for text in \
  'https://github.com/franciscpd/batuta-compozy/blob/v0.1.0-beta.4/LICENSE' \
  'https://github.com/franciscpd/batuta-compozy/blob/v0.1.0-beta.4/docs/architecture.md' \
  'https://github.com/franciscpd/batuta-compozy/blob/v0.1.0-beta.4/docs/case-studies/version-subcommand.md'; do
  require_text docs/releases/0.1.0-beta.4.md "$text"
done

python3 - README.md README.pt-BR.md docs/architecture.md CONTRIBUTING.md \
  docs/case-studies/version-subcommand.md docs/releases/0.1.0-beta.2.md \
  docs/releases/0.1.0-beta.3.md docs/releases/0.1.0-beta.4.md \
  docs/releases/0.1.0-beta.5.md <<'PY'
import re
import sys


def unsupported_affiliation(contents):
    sentences = re.split(r"(?<=[.!?])\s+", contents)
    for sentence in sentences:
        lowered = sentence.lower()
        prohibited = (
            "batuta" in lowered
            and any(term in lowered for term in (
                "official", "endorsed", "compozyos component",
                "oficial", "endossad", "componente do compozyos",
                "componente oficial",
            ))
        )
        denial = any(phrase in lowered for phrase in (
            "not official", "not an official", "not endorsed", "not an endorsed",
            "not a compozyos component", "não oficial", "não é oficial",
            "não endossado", "não é endossado", "não um componente oficial",
            "não é um componente oficial", "não é um componente do compozyos",
        ))
        if prohibited and not denial:
            return sentence
    return None


for statement in (
    "Batuta é um componente oficial do CompozyOS.",
    "Batuta é endossado pelo CompozyOS.",
):
    if unsupported_affiliation(statement) is None:
        raise SystemExit(
            f"affiliation scanner accepted affirmative Portuguese claim: {statement}"
        )

for statement in (
    "Batuta is not an official or endorsed CompozyOS component.",
    "O Batuta não é um componente oficial ou endossado do CompozyOS.",
):
    if unsupported_affiliation(statement) is not None:
        raise SystemExit(f"affiliation scanner rejected explicit denial: {statement}")

for name in sys.argv[1:]:
    contents = open(name, encoding="utf-8").read()
    unsupported = unsupported_affiliation(contents)
    if unsupported is not None:
        raise SystemExit(f"unsupported affiliation claim in {name}: {unsupported}")
PY

printf 'OK: public documentation exposes guides, contribution workflow, and independent status\n'
