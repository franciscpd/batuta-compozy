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
require_file docs/releases/0.1.0-beta.6.md
require_file docs/architecture.pt-BR.md
require_file docs/case-studies/version-subcommand.pt-BR.md
require_file CONTRIBUTING.pt-BR.md
require_file docs/how-it-works.pt-BR.md
require_file docs/verify.pt-BR.md
require_file docs/releases/0.1.0-beta.3.pt-BR.md
require_file docs/releases/0.1.0-beta.6.pt-BR.md

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
  'max-four dependency-safe parallelism' \
  'task worktrees' \
  'canonical conflict reexecution' \
  'one final review' \
  '34208e9990622ee62e9a5cf114386273ae6abfa0' \
  'no human publication gate' \
  'merge remains manual'; do
  require_text README.md "$text"
done

for text in \
  'projeto independente da comunidade' \
  'docs/architecture.pt-BR.md' \
  'docs/case-studies/version-subcommand.pt-BR.md' \
  'CONTRIBUTING.pt-BR.md' \
  'LICENSE' \
  'docs/how-it-works.pt-BR.md' \
  'docs/verify.pt-BR.md' \
  'https://www.compozy.com/docs/' \
  'https://github.com/compozy/compozy' \
  'inventário automático de executores' \
  'domínio × complexidade' \
  'paralelismo seguro de no máximo quatro' \
  'worktrees de task' \
  'reexecução canônica de conflito' \
  'uma revisão final' \
  '34208e9990622ee62e9a5cf114386273ae6abfa0' \
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
  'three Loops' \
  'batuta-deliver' \
  'batuta-deliver-core' \
  'batuta-task' \
  'nine hosted Batuta tools' \
  'ext__batuta__delivery_graph' \
  'review-and-fix' \
  'compozy__session_prompt' \
  'ext__batuta__publish_worktree' \
  'delivery_id' \
  'canonical conflict reexecution' \
  'retained diagnostic worktree' \
  'merge remains manual'; do
  require_text docs/architecture.md "$text"
done

for text in \
  'três Loops' \
  'batuta-deliver-core'; do
  require_text docs/architecture.pt-BR.md "$text"
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
  '`batuta-deliver`' \
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
  'SDD' \
  'ext__batuta__executor_inventory' \
  'ext__batuta__routing_plan' \
  'ext__batuta__routing_apply' \
  'batuta-deliver' \
  'batuta-task' \
  'origin_session_id' \
  'compozy__loop_status' \
  'agents/batuta/AGENT.md' \
  'resources/skills/batuta-routing/SKILL.md' \
  'ext__batuta__publish_worktree' \
  'publication_verify' \
  'active wall-clock' \
  'domain × complexity' \
  'backend/low' \
  'frontend/medium' \
  'delivery_id' \
  'canonical conflict reexecution' \
  'max-four dependency-safe parallelism' \
  'task worktree' \
  'integration worktree' \
  'Stored Compozy Loop configuration is never mutated' \
  'one commit' \
  'one final review' \
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
  'one final review' \
  'max-four dependency-safe parallelism' \
  'batuta-task' \
  'Merge remains manual'; do
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
  'compozy extension validate' \
  'compozy extension install github:batuta-ai/compozy --allow-unverified --yes' \
  'compozy extension enable batuta' \
  'scripts/republish.sh'; do
  require_text docs/verify.md "$text"
done
require_text docs/verify.md 'all nine hosted Batuta tools'
require_text docs/verify.md 'loops/batuta-task/loop.yaml'
require_text docs/verify.md 'loops/batuta-deliver-core/loop.yaml'
require_text docs/verify.pt-BR.md 'loops/batuta-deliver-core/loop.yaml'
require_text docs/verify.md 'staged Go sources'
require_text docs/verify.md '34208e9990622ee62e9a5cf114386273ae6abfa0'

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
  'https://github.com/batuta-ai/compozy/blob/v0.1.0-beta.4/LICENSE' \
  'https://github.com/batuta-ai/compozy/blob/v0.1.0-beta.4/docs/architecture.md' \
  'https://github.com/batuta-ai/compozy/blob/v0.1.0-beta.4/docs/case-studies/version-subcommand.md'; do
  require_text docs/releases/0.1.0-beta.4.md "$text"
done

python3 - README.md README.pt-BR.md docs/architecture.md CONTRIBUTING.md \
  docs/case-studies/version-subcommand.md docs/releases/0.1.0-beta.2.md \
  docs/releases/0.1.0-beta.3.md docs/releases/0.1.0-beta.4.md \
  docs/releases/0.1.0-beta.5.md docs/releases/0.1.0-beta.6.md <<'PY'
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
